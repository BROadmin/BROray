package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// WriteFileAtomic writes data to a temporary file in the same directory, fsyncs
// it, renames it over path and fsyncs the directory. On UBIFS this is the only
// sequence that survives a power loss with either the old or the new content.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err = tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return SyncDir(dir)
}

func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// MkdirAllDurable creates missing directory components and syncs each new
// entry's parent before creating a child. Existing components must be real
// directories; a symlink or other type is an error.
func MkdirAllDurable(path string, mode os.FileMode) error {
	return mkdirAllDurableWithSync(path, mode, SyncDir)
}

func mkdirAllDurableWithSync(path string, mode os.FileMode, syncDir func(string) error) error {
	if path == "" || syncDir == nil {
		return fmt.Errorf("durable mkdir: empty path or sync function")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// Keep every ancestor open. Path checks followed by path-based Mkdir can
	// write through a parent that is replaced with a symlink between calls.
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	rootFD, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return err
	}
	fds := []int{rootFD}
	defer func() {
		for i := len(fds) - 1; i >= 0; i-- {
			_ = unix.Close(fds[i])
		}
	}()
	parentFD := rootFD
	parentPath := string(filepath.Separator)
	for _, name := range strings.Split(strings.TrimPrefix(filepath.Clean(abs), parentPath), parentPath) {
		if name == "" {
			continue
		}
		childFD, openErr := unix.Openat(parentFD, name, flags, 0)
		if errors.Is(openErr, unix.ENOENT) {
			if err := unix.Mkdirat(parentFD, name, uint32(mode.Perm())); err != nil {
				return fmt.Errorf("durable mkdir %s: %w", filepath.Join(parentPath, name), err)
			}
			if err := unix.Fsync(parentFD); err != nil {
				return fmt.Errorf("durable mkdir parent sync %s: %w", parentPath, err)
			}
			if err := syncDir(parentPath); err != nil {
				return fmt.Errorf("durable mkdir parent sync %s: %w", parentPath, err)
			}
			childFD, openErr = unix.Openat(parentFD, name, flags, 0)
		}
		if openErr != nil {
			return fmt.Errorf("durable mkdir open %s: %w", filepath.Join(parentPath, name), openErr)
		}
		fds = append(fds, childFD)
		parentFD = childFD
		parentPath = filepath.Join(parentPath, name)
	}
	return nil
}

// OpenDirNoFollow returns a descriptor for the named directory. Every path
// component is opened relative to the previous descriptor without following
// a symlink, so replacing an ancestor cannot redirect later operations.
func OpenDirNoFollow(path string) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("open directory: empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, err
	}
	current := string(filepath.Separator)
	for _, name := range strings.Split(strings.TrimPrefix(filepath.Clean(abs), current), current) {
		if name == "" {
			continue
		}
		next, openErr := unix.Openat(fd, name, flags, 0)
		if openErr != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("open directory %s: %w", filepath.Join(current, name), openErr)
		}
		_ = unix.Close(fd)
		fd = next
		current = filepath.Join(current, name)
	}
	return os.NewFile(uintptr(fd), abs), nil
}

// SyncTree persists regular file content and each directory entry bottom-up.
// Symlinks and special files make a backup stage ambiguous and are refused.
func SyncTree(path string) error {
	return syncTreeWith(path, syncFile, SyncDir)
}

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func syncTreeWith(path string, syncRegular, syncDirectory func(string) error) error {
	if path == "" || syncRegular == nil || syncDirectory == nil {
		return fmt.Errorf("backup tree sync: invalid path or sync function")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch {
	case st.Mode().IsRegular():
		if err := syncRegular(path); err != nil {
			return fmt.Errorf("backup file sync %s: %w", path, err)
		}
	case st.IsDir():
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := syncTreeWith(filepath.Join(path, entry.Name()), syncRegular, syncDirectory); err != nil {
				return err
			}
		}
		if err := syncDirectory(path); err != nil {
			return fmt.Errorf("backup directory sync %s: %w", path, err)
		}
	default:
		return fmt.Errorf("backup tree sync: %s is not a regular file or directory", path)
	}
	return nil
}

// SyncPathAndParents persists a selected live path and its existing directory
// chain through root. For an absent path, syncing the nearest existing parent
// persists the absence. Ancestors are validated before opening the path.
func SyncPathAndParents(root, rel string) error {
	return syncPathAndParentsWith(root, rel, SyncTree, SyncDir)
}

func syncPathAndParentsWith(root, rel string, syncTree, syncDirectory func(string) error) error {
	if root == "" || rel == "" || syncTree == nil || syncDirectory == nil {
		return fmt.Errorf("backup path sync: invalid root, path or sync function")
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("backup path sync: invalid relative path %q", rel)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absRoot = filepath.Clean(absRoot)
	path := filepath.Join(absRoot, clean)
	var existing []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		st, err := os.Lstat(dir)
		switch {
		case err == nil && !st.IsDir():
			return fmt.Errorf("backup path sync: %s is not a directory", dir)
		case err == nil:
			existing = append(existing, dir)
		case errors.Is(err, os.ErrNotExist):
			// A missing intermediate directory is valid for an absent path.
		case err != nil:
			return err
		}
		if dir == absRoot {
			break
		}
		if filepath.Dir(dir) == dir {
			return fmt.Errorf("backup path sync: %s escaped root %s", path, absRoot)
		}
	}
	if len(existing) == 0 || existing[len(existing)-1] != absRoot {
		return fmt.Errorf("backup path sync: root %s is not a directory", absRoot)
	}
	st, err := os.Lstat(path)
	switch {
	case err == nil && (st.Mode().IsRegular() || st.IsDir()):
		if err := syncTree(path); err != nil {
			return err
		}
	case err == nil:
		return fmt.Errorf("backup path sync: %s is not a regular file or directory", path)
	case errors.Is(err, os.ErrNotExist):
		// No selected path exists; its parent chain still needs to be durable.
	case err != nil:
		return err
	}
	for _, dir := range existing {
		if err := syncDirectory(dir); err != nil {
			return fmt.Errorf("backup ancestor sync %s: %w", dir, err)
		}
	}
	return nil
}

// WriteJSONAtomic marshals v with indentation and writes it atomically.
func WriteJSONAtomic(path string, v any, mode os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(b, '\n'), mode)
}

// ReadJSON reads a regular (non-symlink) file into v. Missing file → os.ErrNotExist.
func ReadJSON(path string, v any) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Lock is an exclusive advisory flock held for the life of the process. The
// kernel releases it when the holder dies, which is how "is the daemon alive"
// is answered without trusting a PID.
type Lock struct{ f *os.File }

// Owns checks the live descriptor against the expected lock inode. Used only
// while the caller retains the Lock; a pathname or PID is not proof of ownership.
func (l *Lock) Owns(path string) bool {
	if l == nil || l.f == nil {
		return false
	}
	a, err := l.f.Stat()
	if err != nil {
		return false
	}
	b, err := os.Lstat(path)
	return err == nil && b.Mode().IsRegular() && os.SameFile(a, b)
}

var ErrLocked = errors.New("LOCK_HELD")

func TryLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close() // closing releases the flock
	l.f = nil
	return err
}

// Held reports whether another process currently holds the lock at path.
func Held(path string) bool {
	l, err := TryLock(path)
	if err != nil {
		return errors.Is(err, ErrLocked)
	}
	_ = l.Release()
	return false
}

// HeldChecked distinguishes an unheld lock from a lock that cannot be probed.
// Callers making destructive decisions must stop on an ambiguous lock state.
func HeldChecked(path string) (bool, error) {
	l, err := TryLock(path)
	if errors.Is(err, ErrLocked) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err := l.Release(); err != nil {
		return false, err
	}
	return false, nil
}

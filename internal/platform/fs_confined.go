package platform

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func confinedName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("invalid file name %q", name)
	}
	return nil
}

// OpenDirAtNoFollow opens one child of an already held directory. It cannot
// follow a replacement symlink or a renamed ancestor into another tree.
func OpenDirAtNoFollow(parent *os.File, name string) (*os.File, error) {
	if parent == nil {
		return nil, errors.New("nil parent directory")
	}
	if err := confinedName(name); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

// MkdirTempAtNoFollow allocates a fresh directory under a held parent. The
// caller retains the parent descriptor for subsequent operations and cleanup.
func MkdirTempAtNoFollow(parent *os.File, prefix string, mode os.FileMode) (string, error) {
	if parent == nil {
		return "", errors.New("nil parent directory")
	}
	if prefix == "" || strings.ContainsAny(prefix, "/\\") {
		return "", fmt.Errorf("invalid directory prefix %q", prefix)
	}
	for attempt := 0; attempt < 16; attempt++ {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", err
		}
		name := prefix + hex.EncodeToString(nonce[:])
		if err := unix.Mkdirat(int(parent.Fd()), name, uint32(mode.Perm())); errors.Is(err, unix.EEXIST) {
			continue
		} else if err != nil {
			return "", err
		}
		if err := parent.Sync(); err != nil {
			return name, err
		}
		return name, nil
	}
	return "", errors.New("unable to allocate unique directory name")
}

// RemoveDirTreeAtNoFollow removes one owned directory below a held parent.
// Every child is inspected without following links; a replaced directory
// entry is rejected before its final unlink.
func RemoveDirTreeAtNoFollow(parent *os.File, name string) error {
	return removeDirTreeAtNoFollow(parent, name, nil)
}

// RemoveOwnedDirTreeAtNoFollow refuses to traverse a child that has been
// replaced since the caller opened it.
func RemoveOwnedDirTreeAtNoFollow(parent *os.File, name string, expected *os.File) error {
	if expected == nil {
		return errors.New("nil expected directory")
	}
	return removeDirTreeAtNoFollow(parent, name, expected)
}

func removeDirTreeAtNoFollow(parent *os.File, name string, expected *os.File) error {
	directory, err := OpenDirAtNoFollow(parent, name)
	if err != nil {
		return err
	}
	defer directory.Close()
	if expected != nil {
		ownedInfo, err := expected.Stat()
		if err != nil {
			return err
		}
		currentInfo, err := directory.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(ownedInfo, currentInfo) {
			return fmt.Errorf("directory entry changed before traversal: %s", name)
		}
	}
	var held unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &held); err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := entry.Name()
		if err := confinedName(child); err != nil {
			return err
		}
		var st unix.Stat_t
		if err := unix.Fstatat(int(directory.Fd()), child, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			if err := removeDirTreeAtNoFollow(directory, child, nil); err != nil {
				return err
			}
		case unix.S_IFREG, unix.S_IFLNK:
			if err := unix.Unlinkat(int(directory.Fd()), child, 0); err != nil {
				return err
			}
		default:
			return fmt.Errorf("refuse to remove unusual directory entry %q", child)
		}
	}
	if err := directory.Sync(); err != nil {
		return err
	}
	var current unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Mode&unix.S_IFMT != unix.S_IFDIR || current.Dev != held.Dev || current.Ino != held.Ino {
		return fmt.Errorf("directory entry changed before removal: %s", name)
	}
	if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	return parent.Sync()
}

// ReadRegularAtNoFollow reads a regular file under an already held directory.
func ReadRegularAtNoFollow(parent *os.File, name string) ([]byte, error) {
	if parent == nil {
		return nil, errors.New("nil parent directory")
	}
	if err := confinedName(name); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if statErr != nil {
			return nil, statErr
		}
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	b, readErr := io.ReadAll(f)
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return b, nil
}

// WriteNewRegularAtNoFollow creates one file beneath a held directory. It
// refuses to replace any existing entry, including a symlink.
func WriteNewRegularAtNoFollow(parent *os.File, name string, data []byte, mode os.FileMode) error {
	if parent == nil {
		return errors.New("nil parent directory")
	}
	if err := confinedName(name); err != nil {
		return err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return parent.Sync()
}

func ReadRegularNoFollow(path string) ([]byte, error) {
	parent, err := OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return ReadRegularAtNoFollow(parent, filepath.Base(path))
}

// RemoveRegularNoFollow removes a regular file without following any path
// component and syncs its parent. Missing files return ENOENT to the caller.
func RemoveRegularNoFollow(path string) error {
	parent, err := OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(path)
	if err := confinedName(name); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if err := unix.Unlinkat(int(parent.Fd()), name, 0); err != nil {
		return err
	}
	return parent.Sync()
}

// WriteFileAtomicNoFollow atomically replaces a regular file through a held
// parent descriptor. The caller must create and validate the parent first.
func WriteFileAtomicNoFollow(path string, data []byte, mode os.FileMode) error {
	return writeFileAtomicNoFollow(path, data, mode, true)
}

// WriteFileAtomicLightNoFollow preserves atomic replacement without syncing
// progress files on every update.
func WriteFileAtomicLightNoFollow(path string, data []byte, mode os.FileMode) error {
	return writeFileAtomicNoFollow(path, data, mode, false)
}

func writeFileAtomicNoFollow(path string, data []byte, mode os.FileMode, durable bool) error {
	parent, err := OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(path)
	if err := confinedName(name); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if st.Mode&unix.S_IFMT != unix.S_IFREG {
			return fmt.Errorf("%s is not a regular file", path)
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmpName := "." + name + "." + hex.EncodeToString(nonce[:]) + ".tmp"
	fd, err := unix.Openat(int(parent.Fd()), tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = unix.Unlinkat(int(parent.Fd()), tmpName, 0)
		}
	}()
	f := os.NewFile(uintptr(fd), tmpName)
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Chmod(mode)
	}
	if writeErr == nil && durable {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := unix.Renameat(int(parent.Fd()), tmpName, int(parent.Fd()), name); err != nil {
		return err
	}
	renamed = true
	if durable {
		return parent.Sync()
	}
	return nil
}

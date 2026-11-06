package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

// All cleanup reads and mutations stay relative to held, no-follow directory
// descriptors. A lexical allowlist alone cannot establish ownership.
type cleanupHandle struct {
	root, parent, file *os.File
	name               string
}

func (h *cleanupHandle) close() {
	if h.file != nil {
		_ = h.file.Close()
	}
	if h.parent != nil {
		_ = h.parent.Close()
	}
	if h.root != nil {
		_ = h.root.Close()
	}
}

func cleanupRelative(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("cleanup path escapes installation")
	}
	return rel, nil
}

func (a *area) openCleanup(rel string) (*cleanupHandle, error) {
	path := filepath.Join(a.d.Root.Dir, rel)
	checked, err := cleanupRelative(a.d.Root.Dir, path)
	if err != nil || checked != rel {
		return nil, errors.New("invalid cleanup path")
	}
	root, err := platform.OpenDirNoFollow(a.d.Root.Dir)
	if err != nil {
		return nil, err
	}
	h := &cleanupHandle{root: root, name: filepath.Base(rel)}
	// A separate descriptor keeps root identity available even when parent=root.
	fd, err := unix.Dup(int(root.Fd()))
	if err != nil {
		h.close()
		return nil, err
	}
	unix.CloseOnExec(fd)
	h.parent = os.NewFile(uintptr(fd), a.d.Root.Dir)
	parentRel := filepath.Dir(rel)
	if parentRel != "." {
		for _, component := range strings.Split(parentRel, string(filepath.Separator)) {
			next, e := platform.OpenDirAtNoFollow(h.parent, component)
			if e != nil {
				h.close()
				return nil, e
			}
			_ = h.parent.Close()
			h.parent = next
		}
	}
	fd, err = unix.Openat(int(h.parent.Fd()), h.name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		h.close()
		return nil, err
	}
	h.file = os.NewFile(uintptr(fd), h.name)
	st, err := h.file.Stat()
	if err != nil || (!st.Mode().IsRegular() && !st.IsDir()) {
		h.close()
		return nil, errors.New("cleanup entry is not regular file/directory")
	}
	return h, nil
}

func cleanupObjectIdentity(st *unix.Stat_t) string {
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%d", st.Dev, st.Ino, st.Mode, st.Size,
		st.Mtim.Sec, st.Mtim.Nsec, st.Ctim.Sec, st.Ctim.Nsec)
}

func cleanupTree(f *os.File, prefix string, rows *[]string) (int64, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return 0, err
	}
	*rows = append(*rows, prefix+"="+cleanupObjectIdentity(&st))
	blocks := int64(st.Blocks)
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return blocks, nil
	}
	entries, err := f.ReadDir(-1)
	if err != nil {
		return 0, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		var child unix.Stat_t
		if err := unix.Fstatat(int(f.Fd()), name, &child, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return 0, err
		}
		childPath := prefix + "/" + name
		if child.Mode&unix.S_IFMT == unix.S_IFDIR {
			dir, err := platform.OpenDirAtNoFollow(f, name)
			if err != nil {
				return 0, err
			}
			n, err := cleanupTree(dir, childPath, rows)
			_ = dir.Close()
			if err != nil {
				return 0, err
			}
			blocks += n
		} else if child.Mode&unix.S_IFMT == unix.S_IFREG || child.Mode&unix.S_IFMT == unix.S_IFLNK {
			*rows = append(*rows, childPath+"="+cleanupObjectIdentity(&child))
			blocks += int64(child.Blocks)
		} else {
			return 0, errors.New("unusual cleanup directory entry")
		}
	}
	return blocks, nil
}

func (h *cleanupHandle) describe() (os.FileInfo, int64, string, error) {
	st, err := h.file.Stat()
	if err != nil {
		return nil, 0, "", err
	}
	var root, parent unix.Stat_t
	if err = unix.Fstat(int(h.root.Fd()), &root); err != nil {
		return nil, 0, "", err
	}
	if err = unix.Fstat(int(h.parent.Fd()), &parent); err != nil {
		return nil, 0, "", err
	}
	rows := []string{fmt.Sprintf("root=%d:%d;parent=%d:%d", root.Dev, root.Ino, parent.Dev, parent.Ino)}
	blocks, err := cleanupTree(h.file, "", &rows)
	if err != nil {
		return nil, 0, "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	size := st.Size()
	if st.IsDir() {
		size = ((blocks*512 + 1023) / 1024) * 1024
	}
	return st, size, hex.EncodeToString(sum[:]), nil
}

func (a *area) removeCleanup(l cleanupLine) error {
	h, err := a.openCleanup(l.rel)
	if err != nil {
		return err
	}
	defer h.close()
	st, size, identity, err := h.describe()
	if err != nil {
		return err
	}
	if identity != l.identity || size != l.size || (l.kind == "file") != st.Mode().IsRegular() {
		return errors.New("cleanup entry changed")
	}
	if st.IsDir() {
		return platform.RemoveOwnedDirTreeAtNoFollow(h.parent, h.name, h.file)
	}
	var current, held unix.Stat_t
	if err = unix.Fstat(int(h.file.Fd()), &held); err != nil {
		return err
	}
	if err = unix.Fstatat(int(h.parent.Fd()), h.name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if cleanupObjectIdentity(&current) != cleanupObjectIdentity(&held) {
		return errors.New("cleanup file replaced before removal")
	}
	if err = unix.Unlinkat(int(h.parent.Fd()), h.name, 0); err != nil {
		return err
	}
	return h.parent.Sync()
}

func (a *area) openCleanupPlans(create bool) (*os.File, error) {
	rel, err := cleanupRelative(a.d.Root.Dir, a.cleanupPlanDir())
	if err != nil {
		return nil, err
	}
	dir, err := platform.OpenDirNoFollow(a.d.Root.Dir)
	if err != nil {
		return nil, err
	}
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		next, e := platform.OpenDirAtNoFollow(dir, name)
		if e != nil && create && errors.Is(e, unix.ENOENT) {
			e = unix.Mkdirat(int(dir.Fd()), name, 0700)
			if e == nil || errors.Is(e, unix.EEXIST) {
				next, e = platform.OpenDirAtNoFollow(dir, name)
			}
		}
		_ = dir.Close()
		if e != nil {
			return nil, e
		}
		dir = next
	}
	return dir, nil
}

func cleanupRemovePlanFile(dir *os.File, name string) {
	var st unix.Stat_t
	if unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW) == nil && st.Mode&unix.S_IFMT == unix.S_IFREG {
		_ = unix.Unlinkat(int(dir.Fd()), name, 0)
	}
}

func (a *area) removeExpiredPlans(dir *os.File) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return
	}
	for _, e := range entries {
		token := strings.TrimSuffix(e.Name(), ".json")
		if !strings.HasSuffix(e.Name(), ".json") || !reHex32Token.MatchString(token) {
			continue
		}
		raw, err := platform.ReadRegularAtNoFollow(dir, e.Name())
		if err != nil {
			continue
		}
		var plan struct {
			ExpiresEpoch int64 `json:"expiresEpoch"`
		}
		if json.Unmarshal(raw, &plan) == nil && plan.ExpiresEpoch < a.now().Unix() {
			cleanupRemovePlanFile(dir, e.Name())
			cleanupRemovePlanFile(dir, token+".list")
		}
	}
}

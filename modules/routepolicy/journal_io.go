package routepolicy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

var ErrJournalUncertain = errors.New("policy journal durability unknown; retain fence and read back")

// RecoveryPending only inspects a bounded regular journal; it never repairs it.
func RecoveryPending(ctx context.Context, root platform.Root) (bool, error) {
	j, err := OpenJournal(root)
	if err != nil {
		return false, err
	}
	defer j.Close()
	var pending bool
	err = j.locked(ctx, func() error {
		image, err := readImage(j.dirs[root.Routes()], JournalFile, maxJournalBytes)
		if err != nil {
			return err
		}
		pending = image.Exists
		return j.checkRoots()
	})
	return pending, err
}

func makeImage(data []byte) fileImage {
	if data == nil {
		return fileImage{}
	}
	sum := sha256.Sum256(data)
	return fileImage{Exists: true, Data: append([]byte(nil), data...), SHA256: hex.EncodeToString(sum[:])}
}
func (f fileImage) equal(other fileImage) bool {
	return f.Exists == other.Exists && f.SHA256 == other.SHA256
}
func (f fileImage) valid(limit int) bool {
	if !f.Exists {
		return len(f.Data) == 0 && f.SHA256 == ""
	}
	if len(f.Data) > limit {
		return false
	}
	sum := sha256.Sum256(f.Data)
	return f.SHA256 == hex.EncodeToString(sum[:])
}

func readImage(parent *os.File, name string, limit int) (fileImage, error) {
	image, file, err := openImage(parent, name, limit)
	if file != nil {
		defer file.Close()
	}
	return image, err
}

func openImage(parent *os.File, name string, limit int) (fileImage, *os.File, error) {
	if parent == nil || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || limit <= 0 {
		return fileImage{}, nil, errors.New("invalid journal file input")
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return fileImage{}, nil, nil
	}
	if err != nil {
		return fileImage{}, nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fileImage{}, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		f.Close()
		return fileImage{}, nil, errors.New("journal input is not bounded regular data")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		f.Close()
		return fileImage{}, nil, errors.New("journal read failed or exceeded limit")
	}
	return makeImage(data), f, nil
}

func sameJournalEntry(parent *os.File, name string, held *os.File) error {
	var visible unix.Stat_t
	err := unix.Fstatat(int(parent.Fd()), name, &visible, unix.AT_SYMLINK_NOFOLLOW)
	if held == nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.New("previously absent journal entry appeared")
	}
	if err != nil {
		return err
	}
	var own unix.Stat_t
	if err := unix.Fstat(int(held.Fd()), &own); err != nil {
		return err
	}
	if visible.Mode&unix.S_IFMT != unix.S_IFREG || visible.Dev != own.Dev || visible.Ino != own.Ino {
		return errors.New("journal file identity changed")
	}
	return nil
}

// All mutations are relative to retained parent descriptors. A temporary file
// is cleaned only while its visible entry still names our original descriptor.
func replaceImage(parent *os.File, name string, next, expected fileImage, limit int) error {
	if !next.valid(limit) || !expected.valid(limit) {
		return errors.New("invalid replacement image")
	}
	current, held, err := openImage(parent, name, limit)
	if held != nil {
		defer held.Close()
	}
	if err != nil {
		return err
	}
	if !current.equal(expected) {
		return errors.New("journal replacement compare failed")
	}
	if current.equal(next) {
		return nil
	}
	if !next.Exists {
		if err := sameJournalEntry(parent, name, held); err != nil {
			return err
		}
		if err := unix.Unlinkat(int(parent.Fd()), name, 0); err != nil {
			return err
		}
		if err := parent.Sync(); err != nil {
			return fmt.Errorf("%w: %v", ErrJournalUncertain, err)
		}
		return nil
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := ".policy-" + hex.EncodeToString(nonce[:]) + ".tmp"
	fd, err := unix.Openat(int(parent.Fd()), temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), temp)
	defer f.Close()
	defer func() {
		if sameJournalEntry(parent, temp, f) == nil {
			_ = unix.Unlinkat(int(parent.Fd()), temp, 0)
		}
	}()
	if n, err := f.Write(next.Data); err != nil {
		return err
	} else if n != len(next.Data) {
		return io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := sameJournalEntry(parent, name, held); err != nil {
		return err
	}
	if err := sameJournalEntry(parent, temp, f); err != nil {
		return err
	}
	if err := unix.Renameat(int(parent.Fd()), temp, int(parent.Fd()), name); err != nil {
		return err
	}
	if err := parent.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrJournalUncertain, err)
	}
	return nil
}

func journalGuard(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("recovery guard is not regular")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	if err := sameJournalEntry(parent, name, f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// RecoverPolicy is cold-start recovery only. It proves daemon ownership and
// holds child/installer/launcher exclusions throughout all recovery writes.
// A live runtime instead uses Rollback with its real lifecycle restore callback.
func RecoverPolicy(ctx context.Context, root platform.Root, guard *platform.Lock) error {
	if !guard.Owns(root.DaemonLock()) {
		return errors.New("policy recovery requires daemon kernel lock")
	}
	j, err := OpenJournal(root)
	if err != nil {
		return err
	}
	defer j.Close()
	return j.locked(ctx, func() error {
		_, _, err := j.load()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		j.daemonGuard = guard
		j.exclusions = map[string]*os.File{}
		for _, name := range []string{"xray-child.lock", filepath.Base(root.InstallerLock()), filepath.Base(root.LauncherLock())} {
			lock, err := journalGuard(j.dirs[root.Run()], name)
			if err != nil {
				return fmt.Errorf("policy recovery owner %s: %w", name, err)
			}
			defer lock.Close()
			j.exclusions[name] = lock
		}
		if err := j.checkRoots(); err != nil {
			return err
		}
		return j.rollback(ctx, func(_ context.Context, data []byte, exists bool) error {
			r, _, err := j.load()
			if err != nil {
				return err
			}
			if err := j.checkRoots(); err != nil {
				return err
			}
			old, err := readImage(j.dirs[root.Config()], filepath.Base(root.XrayConfig()), MaxStateBytes)
			if err != nil {
				return err
			}
			if !old.equal(r.PreviousConfig) && !old.equal(r.NextConfig) {
				return errors.New("config changed during cold recovery")
			}
			return replaceImage(j.dirs[root.Config()], filepath.Base(root.XrayConfig()), fileImage{Exists: exists, Data: data, SHA256: r.PreviousConfig.SHA256}, old, MaxStateBytes)
		})
	})
}

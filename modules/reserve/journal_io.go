package reserve

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strings"
)

// Adapted without changing ownership/durability checks from routepolicy's
// verified retained-parent file image implementation.
var ErrTransactionUncertain = errors.New("reserve transaction durability unknown; retain fence and read back")

func transactionImage(data []byte) TransactionImage {
	if data == nil {
		return TransactionImage{}
	}
	sum := sha256.Sum256(data)
	return TransactionImage{Exists: true, Data: append([]byte(nil), data...), SHA256: hex.EncodeToString(sum[:])}
}
func (f TransactionImage) equal(other TransactionImage) bool {
	return f.Exists == other.Exists && f.SHA256 == other.SHA256
}
func (f TransactionImage) valid(limit int) bool {
	if !f.Exists {
		return len(f.Data) == 0 && f.SHA256 == ""
	}
	if len(f.Data) > limit {
		return false
	}
	sum := sha256.Sum256(f.Data)
	return f.SHA256 == hex.EncodeToString(sum[:])
}

func readTransactionImage(parent *os.File, name string, limit int) (TransactionImage, error) {
	image, file, err := openTransactionImage(parent, name, limit)
	if file != nil {
		defer file.Close()
	}
	return image, err
}

func openTransactionImage(parent *os.File, name string, limit int) (TransactionImage, *os.File, error) {
	if parent == nil || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || limit <= 0 {
		return TransactionImage{}, nil, errors.New("invalid journal file input")
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return TransactionImage{}, nil, nil
	}
	if err != nil {
		return TransactionImage{}, nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return TransactionImage{}, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		f.Close()
		return TransactionImage{}, nil, errors.New("journal input is not bounded regular data")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		f.Close()
		return TransactionImage{}, nil, errors.New("journal read failed or exceeded limit")
	}
	return transactionImage(data), f, nil
}

func sameTransactionEntry(parent *os.File, name string, held *os.File) error {
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
func replaceTransactionImage(parent *os.File, name string, next, expected TransactionImage, limit int) error {
	if !next.valid(limit) || !expected.valid(limit) {
		return errors.New("invalid replacement image")
	}
	current, held, err := openTransactionImage(parent, name, limit)
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
		if err := sameTransactionEntry(parent, name, held); err != nil {
			return err
		}
		if err := unix.Unlinkat(int(parent.Fd()), name, 0); err != nil {
			return err
		}
		if err := parent.Sync(); err != nil {
			return fmt.Errorf("%w: %v", ErrTransactionUncertain, err)
		}
		return nil
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := ".reserve-" + hex.EncodeToString(nonce[:]) + ".tmp"
	fd, err := unix.Openat(int(parent.Fd()), temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), temp)
	defer f.Close()
	defer func() {
		if sameTransactionEntry(parent, temp, f) == nil {
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
	if err := sameTransactionEntry(parent, name, held); err != nil {
		return err
	}
	if err := sameTransactionEntry(parent, temp, f); err != nil {
		return err
	}
	if err := unix.Renameat(int(parent.Fd()), temp, int(parent.Fd()), name); err != nil {
		return err
	}
	if err := parent.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrTransactionUncertain, err)
	}
	return nil
}

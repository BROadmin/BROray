//go:build linux

package compat

import (
	"os"
	"path/filepath"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

// exchange atomically swaps two paths (renameat2 RENAME_EXCHANGE, Linux
// 3.15+ on ext4/tmpfs/…); callers fall back only when the operation is unsupported.
func exchange(a, b string) error {
	return backupRenameNoFollow(a, b, unix.RENAME_EXCHANGE)
}

// renameBackupNoReplace moves a path only when the destination is still
// absent. Unsupported kernels/filesystems fail closed: os.Rename may replace
// an entry created after preflight, including one owned by another actor.
func renameBackupNoReplace(a, b string) error {
	return backupRenameNoFollow(a, b, unix.RENAME_NOREPLACE)
}

func exchangeBackupAt(aDir *os.File, aName string, bDir *os.File, bName string) error {
	return unix.Renameat2(int(aDir.Fd()), aName, int(bDir.Fd()), bName, unix.RENAME_EXCHANGE)
}

func renameBackupNoReplaceAt(aDir *os.File, aName string, bDir *os.File, bName string) error {
	return unix.Renameat2(int(aDir.Fd()), aName, int(bDir.Fd()), bName, unix.RENAME_NOREPLACE)
}

func backupRenameNoFollow(a, b string, flags uint) error {
	aParent, bParent := filepath.Dir(a), filepath.Dir(b)
	aDir, err := platform.OpenDirNoFollow(aParent)
	if err != nil {
		return err
	}
	defer aDir.Close()
	bDir, err := platform.OpenDirNoFollow(bParent)
	if err != nil {
		return err
	}
	defer bDir.Close()
	if err := backupSwapParentUnchanged(aParent, aDir); err != nil {
		return err
	}
	if err := backupSwapParentUnchanged(bParent, bDir); err != nil {
		return err
	}
	if err := unix.Renameat2(int(aDir.Fd()), filepath.Base(a), int(bDir.Fd()), filepath.Base(b), flags); err != nil {
		return err
	}
	if err := aDir.Sync(); err != nil {
		return err
	}
	if aParent != bParent {
		if err := bDir.Sync(); err != nil {
			return err
		}
	}
	if err := backupSwapParentUnchanged(aParent, aDir); err != nil {
		return err
	}
	return backupSwapParentUnchanged(bParent, bDir)
}

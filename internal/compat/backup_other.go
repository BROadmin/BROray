//go:build !linux

package compat

import (
	"os"
	"syscall"
)

func exchange(a, b string) error { return syscall.ENOSYS }

// BROray backup swaps require an atomic no-replace rename. Linux is the
// supported router target; other kernels must fail closed.
func renameBackupNoReplace(a, b string) error { return syscall.ENOSYS }

func exchangeBackupAt(aDir *os.File, aName string, bDir *os.File, bName string) error {
	return syscall.ENOSYS
}

func renameBackupNoReplaceAt(aDir *os.File, aName string, bDir *os.File, bName string) error {
	return syscall.ENOSYS
}

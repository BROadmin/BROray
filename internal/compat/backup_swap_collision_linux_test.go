//go:build linux

package compat

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func TestSwapExchangeDoesNotReplaceLatePreCopyCollision(t *testing.T) {
	liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
	live := filepath.Join(liveRoot, filepath.FromSlash(rel))
	stage := filepath.Join(stageRoot, filepath.FromSlash(rel))
	pre := filepath.Join(preRoot, filepath.FromSlash(rel))
	_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, backupSwapHooks{
		exchange: func(a, b string) error {
			if err := exchange(a, b); err != nil {
				return err
			}
			writeT(t, pre, "foreign")
			return nil
		},
		syncDir: platform.SyncDir,
	})
	if err == nil {
		t.Fatal("accepted late pre-copy collision after exchange")
	}
	if got := readT(t, pre); got != "foreign" {
		t.Fatalf("foreign pre-copy overwritten: %q", got)
	}
	if got := readT(t, live); got != "new" {
		t.Fatalf("unexpected live state: %q", got)
	}
	if got := readT(t, stage); got != "old" {
		t.Fatalf("original stage state lost: %q", got)
	}
}

func TestSwapFallbackDoesNotReplaceLatePreCopyCollision(t *testing.T) {
	liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
	live := filepath.Join(liveRoot, filepath.FromSlash(rel))
	stage := filepath.Join(stageRoot, filepath.FromSlash(rel))
	pre := filepath.Join(preRoot, filepath.FromSlash(rel))
	_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, backupSwapHooks{
		exchange: func(string, string) error {
			writeT(t, pre, "foreign")
			return syscall.ENOSYS
		},
		syncDir: platform.SyncDir,
	})
	if err == nil {
		t.Fatal("accepted late pre-copy collision in fallback")
	}
	if got := readT(t, pre); got != "foreign" {
		t.Fatalf("foreign pre-copy overwritten: %q", got)
	}
	if got := readT(t, live); got != "old" {
		t.Fatalf("original live state lost: %q", got)
	}
	if got := readT(t, stage); got != "new" {
		t.Fatalf("staged state changed: %q", got)
	}
}

func TestSwapStageOnlyDoesNotReplaceLateLiveCollision(t *testing.T) {
	liveRoot, stageRoot, preRoot, rel := t.TempDir(), t.TempDir(), t.TempDir(), "data/item.json"
	live := filepath.Join(liveRoot, filepath.FromSlash(rel))
	stage := filepath.Join(stageRoot, filepath.FromSlash(rel))
	pre := filepath.Join(preRoot, filepath.FromSlash(rel))
	writeT(t, stage, "new")
	checks := 0
	_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, backupSwapHooks{
		exchange: exchange,
		syncDir:  platform.SyncDir,
		lstat: func(path string) (os.FileInfo, error) {
			st, err := os.Lstat(path)
			if path == live {
				checks++
				if checks == 2 && os.IsNotExist(err) {
					writeT(t, live, "foreign")
				}
			}
			return st, err
		},
	})
	if checks != 2 || err == nil {
		t.Fatalf("late live collision fixture failed: checks=%d err=%v", checks, err)
	}
	if got := readT(t, live); got != "foreign" {
		t.Fatalf("foreign live file overwritten: %q", got)
	}
	if got := readT(t, stage); got != "new" {
		t.Fatalf("staged file lost: %q", got)
	}
	if got := readT(t, pre); got != "<missing>" {
		t.Fatalf("pre-copy unexpectedly created: %q", got)
	}
}

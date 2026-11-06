//go:build linux

package compat

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSwapRejectsParentSwapAfterFinalCheck(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fallback bool
	}{
		{name: "exchange"},
		{name: "unsupported_exchange_fallback", fallback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
			live := filepath.Join(liveRoot, filepath.FromSlash(rel))
			stage := filepath.Join(stageRoot, filepath.FromSlash(rel))
			liveParent := filepath.Dir(live)
			ownedParent := liveParent + "-owned"
			outside := t.TempDir()
			foreign := filepath.Join(outside, filepath.Base(live))
			writeT(t, foreign, "foreign")

			stageChecks := 0
			swapped := false
			hooks := backupSwapHooks{
				exchange: exchange,
				lstat: func(path string) (os.FileInfo, error) {
					if path == stage {
						stageChecks++
						if stageChecks == 2 {
							if err := os.Rename(liveParent, ownedParent); err != nil {
								t.Fatal(err)
							}
							if err := os.Symlink(outside, liveParent); err != nil {
								t.Fatal(err)
							}
							swapped = true
						}
					}
					return os.Lstat(path)
				},
			}
			if tc.fallback {
				hooks.exchange = func(string, string) error { return syscall.ENOSYS }
			}
			_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, hooks)
			if !swapped {
				t.Fatal("fixture did not swap parent after final path check")
			}
			if got := readT(t, foreign); got != "foreign" {
				t.Errorf("foreign file changed after parent swap: %q", got)
			}
			if got := readT(t, filepath.Join(ownedParent, filepath.Base(live))); got != "old" {
				t.Errorf("owned original changed after parent swap: %q", got)
			}
			if err == nil {
				t.Error("accepted parent swap after final path check")
			}
		})
	}
}

func TestSwapRejectsRealDirectoryParentSwapAfterFinalCheck(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fallback bool
	}{
		{name: "exchange"},
		{name: "unsupported_exchange_fallback", fallback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
			live := filepath.Join(liveRoot, filepath.FromSlash(rel))
			stage := filepath.Join(stageRoot, filepath.FromSlash(rel))
			liveParent := filepath.Dir(live)
			ownedParent := liveParent + "-owned"
			outside := t.TempDir()
			writeT(t, filepath.Join(outside, filepath.Base(live)), "foreign")

			stageChecks := 0
			swapped := false
			hooks := backupSwapHooks{
				exchange: exchange,
				lstat: func(path string) (os.FileInfo, error) {
					if path == stage {
						stageChecks++
						if stageChecks == 2 {
							if err := os.Rename(liveParent, ownedParent); err != nil {
								t.Fatal(err)
							}
							if err := os.Rename(outside, liveParent); err != nil {
								t.Fatal(err)
							}
							swapped = true
						}
					}
					return os.Lstat(path)
				},
			}
			if tc.fallback {
				hooks.exchange = func(string, string) error { return syscall.ENOSYS }
			}
			_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, hooks)
			if !swapped {
				t.Fatal("fixture did not replace parent after final path check")
			}
			if got := readT(t, filepath.Join(liveParent, filepath.Base(live))); got != "foreign" {
				t.Errorf("foreign file changed after real-directory parent swap: %q", got)
			}
			if got := readT(t, filepath.Join(ownedParent, filepath.Base(live))); got != "old" {
				t.Errorf("owned original changed after real-directory parent swap: %q", got)
			}
			if err == nil {
				t.Error("accepted real-directory parent swap after final path check")
			}
		})
	}
}

func TestUndoBackupExchangeRejectsSymlinkParent(t *testing.T) {
	base := t.TempDir()
	stageParent := filepath.Join(base, "stage")
	outside := filepath.Join(base, "outside")
	liveParent := filepath.Join(base, "live")
	for _, dir := range []string{stageParent, outside} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stage := filepath.Join(stageParent, "item.json")
	foreign := filepath.Join(outside, "item.json")
	writeT(t, stage, "staged")
	writeT(t, foreign, "foreign")
	if err := os.Symlink(outside, liveParent); err != nil {
		t.Fatal(err)
	}
	if err := UndoBackupExchange(stage, filepath.Join(liveParent, "item.json")); err == nil {
		t.Error("accepted symlink parent in backup undo")
	}
	if got := readT(t, foreign); got != "foreign" {
		t.Errorf("foreign file changed by backup undo: %q", got)
	}
	if got := readT(t, stage); got != "staged" {
		t.Errorf("staged file changed by backup undo: %q", got)
	}
}

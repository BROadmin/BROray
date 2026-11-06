package compat

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func swapSyncFixture(t *testing.T) (liveRoot, stageRoot, preRoot, rel string) {
	t.Helper()
	liveRoot, stageRoot, preRoot = t.TempDir(), t.TempDir(), t.TempDir()
	rel = "data/item.json"
	writeT(t, filepath.Join(liveRoot, filepath.FromSlash(rel)), "old")
	writeT(t, filepath.Join(stageRoot, filepath.FromSlash(rel)), "new")
	return liveRoot, stageRoot, preRoot, rel
}

func TestSwapStopsAfterExchangeSyncFailure(t *testing.T) {
	liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
	wantErr := errors.New("injected directory sync failure")
	calls := 0
	_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, backupSwapHooks{
		exchange: exchange,
		syncDir:  func(string) error { calls++; return wantErr },
	})
	if !errors.Is(err, wantErr) || calls != 1 {
		t.Fatalf("sync failure not propagated immediately: calls=%d err=%v", calls, err)
	}
	if got := readT(t, filepath.Join(liveRoot, filepath.FromSlash(rel))); got != "new" {
		t.Fatalf("live after exchange: %q", got)
	}
	if got := readT(t, filepath.Join(stageRoot, filepath.FromSlash(rel))); got != "old" {
		t.Fatalf("original stage changed before sync: %q", got)
	}
	if got := readT(t, filepath.Join(preRoot, filepath.FromSlash(rel))); got != "<missing>" {
		t.Fatalf("original moved to pre-copy before sync: %q", got)
	}
}

func TestSwapStopsAfterFallbackFirstRenameSyncFailure(t *testing.T) {
	liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
	wantErr := errors.New("injected directory sync failure")
	calls := 0
	_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, backupSwapHooks{
		exchange: func(string, string) error { return syscall.ENOSYS },
		syncDir:  func(string) error { calls++; return wantErr },
	})
	if !errors.Is(err, wantErr) || calls != 1 {
		t.Fatalf("sync failure not propagated immediately: calls=%d err=%v", calls, err)
	}
	if got := readT(t, filepath.Join(liveRoot, filepath.FromSlash(rel))); got != "<missing>" {
		t.Fatalf("stage moved to live before first rename sync: %q", got)
	}
	if got := readT(t, filepath.Join(stageRoot, filepath.FromSlash(rel))); got != "new" {
		t.Fatalf("stage changed before first rename sync: %q", got)
	}
	if got := readT(t, filepath.Join(preRoot, filepath.FromSlash(rel))); got != "old" {
		t.Fatalf("original not retained in pre-copy: %q", got)
	}
}

func TestSwapSyncsParentsAfterEveryMetadataPhase(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		name := "exchange"
		if fallback {
			name = "fallback"
		}
		t.Run(name, func(t *testing.T) {
			liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
			live := filepath.Join(liveRoot, filepath.FromSlash(rel))
			stage := filepath.Join(stageRoot, filepath.FromSlash(rel))
			pre := filepath.Join(preRoot, filepath.FromSlash(rel))
			var dirs, states []string
			hooks := backupSwapHooks{
				exchange: exchange,
				syncDir: func(dir string) error {
					dirs = append(dirs, dir)
					states = append(states, readT(t, live)+"/"+readT(t, stage)+"/"+readT(t, pre))
					return platform.SyncDir(dir)
				},
			}
			if fallback {
				hooks.exchange = func(string, string) error { return syscall.ENOSYS }
			}
			if _, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, hooks); err != nil {
				t.Fatal(err)
			}
			if len(dirs) != 4 {
				t.Fatalf("expected two directory syncs per metadata phase, got %v", dirs)
			}
			first := "new/old/<missing>"
			firstDirs := []string{filepath.Dir(stage), filepath.Dir(live)}
			secondDirs := []string{filepath.Dir(stage), filepath.Dir(pre)}
			if fallback {
				first = "<missing>/new/old"
				firstDirs = []string{filepath.Dir(live), filepath.Dir(pre)}
				secondDirs = []string{filepath.Dir(stage), filepath.Dir(live)}
			}
			if states[0] != first || states[1] != first || states[2] != "new/<missing>/old" || states[3] != "new/<missing>/old" {
				t.Fatalf("directory sync happened in wrong phase: %v", states)
			}
			if !sameSwapDirs(dirs[:2], firstDirs) || !sameSwapDirs(dirs[2:], secondDirs) {
				t.Fatalf("wrong directories synced: %v", dirs)
			}
		})
	}
}

func sameSwapDirs(got, want []string) bool {
	if len(got) != 2 || len(want) != 2 {
		return false
	}
	return got[0] == want[0] && got[1] == want[1] || got[0] == want[1] && got[1] == want[0]
}

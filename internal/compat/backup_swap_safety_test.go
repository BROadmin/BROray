package compat

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func TestSwapUnexpectedExchangeErrorDoesNotFallback(t *testing.T) {
	liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
	fault := syscall.EIO
	_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, backupSwapHooks{
		exchange: func(string, string) error { return fault },
		syncDir:  platform.SyncDir,
	})
	if !errors.Is(err, fault) {
		t.Fatalf("unexpected exchange error was ignored: %v", err)
	}
	if got := readT(t, filepath.Join(liveRoot, filepath.FromSlash(rel))); got != "old" {
		t.Fatalf("live changed after EIO: %q", got)
	}
	if got := readT(t, filepath.Join(stageRoot, filepath.FromSlash(rel))); got != "new" {
		t.Fatalf("stage changed after EIO: %q", got)
	}
	if got := readT(t, filepath.Join(preRoot, filepath.FromSlash(rel))); got != "<missing>" {
		t.Fatalf("pre-copy changed after EIO: %q", got)
	}
}

func TestSwapPreservesForeignPreCopyCollision(t *testing.T) {
	liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
	pre := filepath.Join(preRoot, filepath.FromSlash(rel))
	writeT(t, pre, "foreign")
	if _, err := SwapBackupPaths(liveRoot, stageRoot, preRoot, []string{rel}); err == nil {
		t.Fatal("accepted occupied pre-copy destination")
	}
	if got := readT(t, pre); got != "foreign" {
		t.Fatalf("foreign pre-copy overwritten: %q", got)
	}
	if got := readT(t, filepath.Join(liveRoot, filepath.FromSlash(rel))); got != "old" {
		t.Fatalf("live changed before collision check: %q", got)
	}
	if got := readT(t, filepath.Join(stageRoot, filepath.FromSlash(rel))); got != "new" {
		t.Fatalf("stage changed before collision check: %q", got)
	}
}

func TestSwapDoesNotOverwriteForeignStageOnLaterFailure(t *testing.T) {
	liveRoot, stageRoot, preRoot := t.TempDir(), t.TempDir(), t.TempDir()
	liveA, stageA, preA := filepath.Join(liveRoot, "data", "a"), filepath.Join(stageRoot, "data", "a"), filepath.Join(preRoot, "data", "a")
	liveB, stageB := filepath.Join(liveRoot, "other", "b"), filepath.Join(stageRoot, "other", "b")
	preBParent := filepath.Join(preRoot, "other")
	writeT(t, liveA, "old-a")
	writeT(t, stageA, "new-a")
	writeT(t, liveB, "old-b")
	writeT(t, stageB, "new-b")
	injected := false
	hooks := backupSwapHooks{
		exchange: exchange,
		syncDir: func(dir string) error {
			if !injected && readT(t, liveA) == "new-a" && readT(t, preA) == "old-a" && readT(t, stageA) == "<missing>" {
				writeT(t, stageA, "foreign-stage")
				if st, err := os.Lstat(preBParent); err == nil && st.IsDir() {
					if err := os.Remove(preBParent); err != nil {
						t.Fatal(err)
					}
				}
				writeT(t, preBParent, "foreign-parent")
				injected = true
			}
			return platform.SyncDir(dir)
		},
	}
	_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{"data/a", "other/b"}, hooks)
	if !injected || err == nil {
		t.Fatalf("fixture did not reach later failure: injected=%v err=%v", injected, err)
	}
	if got := readT(t, stageA); got != "foreign-stage" {
		t.Fatalf("foreign stage overwritten by rollback: %q", got)
	}
	if got := readT(t, preA); got != "old-a" {
		t.Fatalf("original pre-copy lost: %q", got)
	}
	if got := readT(t, liveA); got != "new-a" {
		t.Fatalf("partial live state unexpectedly changed: %q", got)
	}
}

func TestSwapRefusesLstatErrorBeforeMutation(t *testing.T) {
	liveRoot, stageRoot, preRoot, rel := swapSyncFixture(t)
	live := filepath.Join(liveRoot, filepath.FromSlash(rel))
	fault := syscall.EIO
	_, err := swapBackupPathsWithHooks(liveRoot, stageRoot, preRoot, []string{rel}, backupSwapHooks{
		exchange: exchange,
		syncDir:  platform.SyncDir,
		lstat: func(path string) (os.FileInfo, error) {
			if path == live {
				return nil, fault
			}
			return os.Lstat(path)
		},
	})
	if !errors.Is(err, fault) {
		t.Fatalf("Lstat EIO was treated as absent: %v", err)
	}
	if got := readT(t, live); got != "old" {
		t.Fatalf("live changed after Lstat EIO: %q", got)
	}
	if got := readT(t, filepath.Join(stageRoot, filepath.FromSlash(rel))); got != "new" {
		t.Fatalf("stage changed after Lstat EIO: %q", got)
	}
	if got := readT(t, filepath.Join(preRoot, filepath.FromSlash(rel))); got != "<missing>" {
		t.Fatalf("pre-copy changed after Lstat EIO: %q", got)
	}
}

func TestSwapRefusesENOTDIRInsteadOfTreatingAbsent(t *testing.T) {
	dir := t.TempDir()
	liveRoot := filepath.Join(dir, "live")
	stageRoot := filepath.Join(dir, "stage")
	preRoot := filepath.Join(dir, "pre")
	for _, root := range []string{liveRoot, stageRoot} {
		if err := os.WriteFile(root, []byte("foreign-parent"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := os.Lstat(filepath.Join(root, "item.json"))
		if !errors.Is(err, syscall.ENOTDIR) {
			t.Fatalf("fixture must produce ENOTDIR below %s, got %v", root, err)
		}
	}
	if _, err := SwapBackupPaths(liveRoot, stageRoot, preRoot, []string{"item.json"}); err == nil {
		t.Fatal("non-ENOENT Lstat errors were treated as absence")
	}
	for _, root := range []string{liveRoot, stageRoot} {
		b, err := os.ReadFile(root)
		if err != nil || string(b) != "foreign-parent" {
			t.Fatalf("foreign parent %s changed: %q, %v", root, b, err)
		}
	}
}

package compat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSwapRejectsSymlinkDestinationParentBeforeLiveMutation(t *testing.T) {
	base := t.TempDir()
	liveRoot := filepath.Join(base, "live")
	stageRoot := filepath.Join(base, "stage")
	preRoot := filepath.Join(base, "pre")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{liveRoot, stageRoot, preRoot, outside} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	rel := "data/item.json"
	live := filepath.Join(liveRoot, filepath.FromSlash(rel))
	stage := filepath.Join(stageRoot, filepath.FromSlash(rel))
	writeT(t, live, "old")
	writeT(t, stage, "new")
	if err := os.Symlink(outside, filepath.Join(preRoot, "data")); err != nil {
		t.Fatal(err)
	}
	if _, err := SwapBackupPaths(liveRoot, stageRoot, preRoot, []string{rel}); err == nil {
		t.Fatal("accepted symlink destination parent")
	}
	if got := readT(t, live); got != "old" {
		t.Fatalf("live changed to %q", got)
	}
	if got := readT(t, stage); got != "new" {
		t.Fatalf("stage changed to %q", got)
	}
	if _, err := os.Lstat(filepath.Join(outside, "item.json")); !os.IsNotExist(err) {
		t.Fatalf("foreign destination changed: %v", err)
	}
}

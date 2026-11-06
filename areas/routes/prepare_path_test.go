package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoutePrepareRejectsForeignRootBeforeCreatingLayout(t *testing.T) {
	base := t.TempDir()
	foreign := t.TempDir()
	root := filepath.Join(base, "routes")
	if err := os.Symlink(foreign, root); err != nil {
		t.Fatal(err)
	}
	e := &engine{root: root}
	if err := e.prepare(); err == nil {
		t.Error("prepare accepted a foreign route root")
	}
	entries, err := os.ReadDir(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("prepare created %d entries in a foreign directory", len(entries))
	}
}

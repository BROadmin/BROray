package routes

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRouteLightWriteRejectsForeignAncestor(t *testing.T) {
	base := t.TempDir()
	foreign := t.TempDir()
	root := filepath.Join(base, "routes")
	if err := os.Symlink(foreign, root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "counter.txt")
	foreignTarget := filepath.Join(foreign, "counter.txt")
	sentinel := []byte("foreign sentinel")
	if err := os.WriteFile(foreignTarget, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeLight(target, []byte("replacement")); err == nil {
		t.Error("light write accepted a foreign ancestor")
	}
	got, err := os.ReadFile(foreignTarget)
	if err != nil || !bytes.Equal(got, sentinel) {
		t.Errorf("foreign target changed: %q, %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(foreign, "counter.txt.new")); !os.IsNotExist(err) {
		t.Errorf("temporary file appeared in foreign directory: %v", err)
	}
}

func TestRouteLightWriteIgnoresOccupiedLegacyTempSymlink(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	target := filepath.Join(root, "counter.txt")
	foreignTarget := filepath.Join(foreign, "sentinel.txt")
	sentinel := []byte("foreign sentinel")
	if err := os.WriteFile(foreignTarget, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreignTarget, target+".new"); err != nil {
		t.Fatal(err)
	}
	if err := writeLight(target, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(foreignTarget)
	if err != nil || !bytes.Equal(got, sentinel) {
		t.Errorf("foreign target changed: %q, %v", got, err)
	}
	got, err = os.ReadFile(target)
	if err != nil || !bytes.Equal(got, []byte("replacement")) {
		t.Errorf("target was not updated: %q, %v", got, err)
	}
	info, err := os.Lstat(target + ".new")
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("legacy temporary symlink changed: %v", err)
	}
}

func TestRouteLightWriteRejectsTargetSymlink(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	target := filepath.Join(root, "counter.txt")
	foreignTarget := filepath.Join(foreign, "sentinel.txt")
	if err := os.WriteFile(foreignTarget, []byte("foreign sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreignTarget, target); err != nil {
		t.Fatal(err)
	}
	if err := writeLight(target, []byte("replacement")); err == nil {
		t.Error("light write replaced an existing target symlink")
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("target symlink was removed: %v", err)
	}
}

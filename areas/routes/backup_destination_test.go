package routes

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRouteBackupSaveRejectsForeignSymlinks(t *testing.T) {
	source := filepath.Join(t.TempDir(), "routes", "state.json")
	b := &fileBackup{files: map[string][]byte{source: []byte("original")}}
	name := "routes-state.json"

	t.Run("occupied_file_link", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "original")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(t.TempDir(), "foreign")
		if err := os.WriteFile(foreign, []byte("foreign sentinel"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, name)
		if err := os.Symlink(foreign, link); err != nil {
			t.Fatal(err)
		}
		if err := b.save(dir); err == nil {
			t.Error("backup accepted a foreign file symlink")
		}
		got, err := os.ReadFile(foreign)
		if err != nil || !bytes.Equal(got, []byte("foreign sentinel")) {
			t.Errorf("foreign file changed: %q, %v", got, err)
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("foreign symlink changed: %v, %v", info, err)
		}
	})

	t.Run("directory_link", func(t *testing.T) {
		foreign := filepath.Join(t.TempDir(), "foreign-dir")
		if err := os.Mkdir(foreign, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(t.TempDir(), "original")
		if err := os.Symlink(foreign, link); err != nil {
			t.Fatal(err)
		}
		if err := b.save(link); err == nil {
			t.Error("backup accepted a foreign directory symlink")
		}
		if _, err := os.Lstat(filepath.Join(foreign, name)); !os.IsNotExist(err) {
			t.Errorf("foreign directory received a backup file: %v", err)
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("foreign directory symlink changed: %v, %v", info, err)
		}
	})
}

func TestRouteBackupSaveRejectsNameCollisionBeforeWriting(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a", "routes", "state.json")
	b := filepath.Join(base, "b", "routes", "state.json")
	backup := &fileBackup{files: map[string][]byte{a: []byte("a"), b: []byte("b")}}
	dir := filepath.Join(base, "original")
	if err := backup.save(dir); err == nil {
		t.Error("backup accepted two sources with the same destination name")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("backup wrote files before detecting destination collision: %v", entries)
	}
}

func TestRouteBackupSaveRejectsSymlinkedAncestor(t *testing.T) {
	base := t.TempDir()
	foreign := filepath.Join(base, "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "routes")
	if err := os.Symlink(foreign, root); err != nil {
		t.Fatal(err)
	}
	backup := &fileBackup{files: map[string][]byte{
		filepath.Join(base, "config", "interface.json"): []byte("original"),
	}}
	dir := filepath.Join(root, "transactions", "sync-test", "original")
	if err := backup.save(dir); err == nil {
		t.Error("backup accepted a symlinked routes ancestor")
	}
	if _, err := os.Lstat(filepath.Join(foreign, "transactions")); !os.IsNotExist(err) {
		t.Errorf("foreign tree changed before backup was rejected: %v", err)
	}
}

func TestRouteBackupSaveRejectsParentSwapAfterMkdir(t *testing.T) {
	base := t.TempDir()
	foreign := t.TempDir()
	root := filepath.Join(base, "routes")
	moved := filepath.Join(base, "routes-moved")
	dir := filepath.Join(root, "transactions", "sync-test", "original")
	foreignDir := filepath.Join(foreign, "transactions", "sync-test", "original")
	if err := os.MkdirAll(foreignDir, 0o700); err != nil {
		t.Fatal(err)
	}
	b := &fileBackup{files: map[string][]byte{
		filepath.Join(base, "config", "interface.json"): []byte("original"),
	}}
	swapped := false
	err := b.saveWithMkdir(dir, func(path string, mode os.FileMode) error {
		if err := os.MkdirAll(path, mode); err != nil {
			return err
		}
		if err := os.Rename(root, moved); err != nil {
			return err
		}
		if err := os.Symlink(foreign, root); err != nil {
			return err
		}
		swapped = true
		return nil
	})
	if !swapped {
		t.Fatalf("parent swap was not reached: %v", err)
	}
	if err == nil {
		t.Error("backup accepted a replacement parent")
	}
	entries, readErr := os.ReadDir(foreignDir)
	if readErr != nil || len(entries) != 0 {
		t.Errorf("backup wrote to foreign tree: entries=%v err=%v", entries, readErr)
	}
}

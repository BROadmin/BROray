//go:build linux

package ops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRenamePreservesLateForeignDestination(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "live", "item")
	dst := filepath.Join(base, "stage", "item")
	for _, parent := range []string{filepath.Dir(src), filepath.Dir(dst)} {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(src, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The first pass of rollback saw an absent destination. Another actor
	// creates it before the rename performed by the second pass.
	if exists, err := backupPathExists(dst); err != nil || exists {
		t.Fatalf("fixture destination is not absent: exists=%v err=%v", exists, err)
	}
	if err := os.WriteFile(dst, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backupRename(src, dst); err == nil {
		t.Fatal("backupRename replaced a foreign destination")
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "foreign" {
		t.Fatalf("foreign destination changed: %q, %v", data, err)
	}
	if data, err := os.ReadFile(src); err != nil || string(data) != "original" {
		t.Fatalf("source changed after refusal: %q, %v", data, err)
	}
}

func TestBackupRenameMovesToAbsentDestination(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "live", "item")
	dst := filepath.Join(base, "stage", "nested", "item")
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backupRename(src, dst); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "original" {
		t.Fatalf("destination missing after rename: %q, %v", data, err)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("source remains after rename: %v", err)
	}
}

func TestBackupRenamePreservesLateForeignEmptyDirectory(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "live", "servers")
	dst := filepath.Join(base, "stage", "servers")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "server.json"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if exists, err := backupPathExists(dst); err != nil || exists {
		t.Fatalf("fixture destination is not absent: exists=%v err=%v", exists, err)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	err = backupRename(src, dst)
	if err == nil {
		t.Fatal("backupRename replaced a foreign empty directory")
	}
	t.Logf("directory collision refusal: %v", err)
	retained, err := os.Lstat(dst)
	if err != nil || !os.SameFile(foreign, retained) {
		t.Fatalf("foreign directory was replaced: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(src, "server.json")); err != nil || string(data) != "original" {
		t.Fatalf("source directory changed after refusal: %q, %v", data, err)
	}
}

func TestBackupRenameRejectsSymlinkSourceParent(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	link := filepath.Join(base, "live")
	dst := filepath.Join(base, "stage", "item")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(outside, "item")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := backupRename(filepath.Join(link, "item"), dst)
	if err == nil {
		t.Error("backupRename accepted a symlink source parent")
	}
	if data, readErr := os.ReadFile(foreign); readErr != nil || string(data) != "foreign" {
		t.Errorf("foreign source changed: data=%q err=%v", data, readErr)
	}
	if _, statErr := os.Lstat(dst); !os.IsNotExist(statErr) {
		t.Errorf("destination changed after refusal: %v", statErr)
	}
}

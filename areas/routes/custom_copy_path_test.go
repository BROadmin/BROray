package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomCopyRejectsForeignDestinationAncestor(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "source")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "01.bat"), []byte("route add 1.2.3.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	catalog := filepath.Join(base, "catalog")
	if err := os.Symlink(foreign, catalog); err != nil {
		t.Fatal(err)
	}
	if err := copyDir(src, filepath.Join(catalog, "user.new", "source")); err == nil {
		t.Error("copy accepted foreign destination ancestor")
	}
	entries, err := os.ReadDir(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("copy created %d entries in foreign destination", len(entries))
	}
}

func TestRouteCustomCopyRejectsSourceSymlink(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "source")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign.bat")
	if err := os.WriteFile(foreign, []byte("foreign private content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(src, "01.bat")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(base, "destination")
	if err := copyDir(src, dst); err == nil {
		t.Error("copy accepted source symlink")
	}
	if _, err := os.Lstat(filepath.Join(dst, "01.bat")); !os.IsNotExist(err) {
		t.Errorf("copy materialized foreign source: %v", err)
	}
}

func TestRouteCustomCopyPreservesRegularFile(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "source")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("route add 1.2.3.0\n")
	if err := os.WriteFile(filepath.Join(src, "01.bat"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(base, "destination")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "01.bat"))
	if err != nil || string(got) != string(content) {
		t.Errorf("regular copy changed bytes: content=%q error=%v", got, err)
	}
}

func TestRouteCustomCopyRejectsDestinationSwapAfterOpen(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "source")
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "01.bat"), []byte("route add 1.2.3.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(base, "destination")
	held := filepath.Join(base, "destination-held")
	foreign := t.TempDir()
	beforeCopy := func() {
		if err := os.Rename(dst, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, dst); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyDirWithHook(src, dst, beforeCopy); err == nil {
		t.Error("copy accepted destination swap after opening it")
	}
	entries, err := os.ReadDir(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("copy created %d entries in foreign destination", len(entries))
	}
}

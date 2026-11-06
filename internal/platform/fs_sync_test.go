package platform

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSyncTreeFilesBeforeDirectories(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "a")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "item"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	var got []string
	err := syncTreeWith(root, func(path string) error {
		got = append(got, "file:"+path)
		return nil
	}, func(path string) error {
		got = append(got, "dir:"+path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"file:" + filepath.Join(child, "item"), "dir:" + child, "dir:" + filepath.Join(root, "b"), "dir:" + root}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sync order=%v, want %v", got, want)
	}
}

func TestSyncTreeStopsOnFileSyncFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "item"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("injected file sync failure")
	dirs := 0
	err := syncTreeWith(root, func(string) error { return fault }, func(string) error { dirs++; return nil })
	if !errors.Is(err, fault) || dirs != 0 {
		t.Fatalf("err=%v dirs synced=%d", err, dirs)
	}
}

func TestSyncTreeRejectsSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "foreign")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := syncTreeWith(root, func(string) error { calls++; return nil }, func(string) error { calls++; return nil })
	if err == nil || calls != 0 {
		t.Fatalf("symlink accepted: err=%v calls=%d", err, calls)
	}
}

func TestSyncPathAndParentsSyncsExistingAncestorsForMissingPath(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	var got []string
	err := syncPathAndParentsWith(root, filepath.Join("existing", "missing", "leaf"),
		func(string) error { t.Fatal("synced nonexistent tree"); return nil },
		func(path string) error { got = append(got, path); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{existing, root}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ancestors=%v, want %v", got, want)
	}
}

func TestSyncPathAndParentsRejectsSymlinkAncestorBeforeTree(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "item"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := syncPathAndParentsWith(root, filepath.Join("link", "item"),
		func(string) error { calls++; return nil },
		func(string) error { calls++; return nil })
	if err == nil || calls != 0 {
		t.Fatalf("symlink ancestor accepted: err=%v calls=%d", err, calls)
	}
}

func TestSyncPathAndParentsSyncsTreeBeforeParents(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "a")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parent, "item")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []string
	err := syncPathAndParentsWith(root, filepath.Join("a", "item"),
		func(path string) error { got = append(got, "tree:"+path); return nil },
		func(path string) error { got = append(got, "dir:"+path); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tree:" + file, "dir:" + parent, "dir:" + root}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sync order=%v, want %v", got, want)
	}
}

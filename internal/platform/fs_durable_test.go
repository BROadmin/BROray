package platform

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMkdirAllDurableSyncsEachCreatedParent(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "a")
	target := filepath.Join(first, "b")
	var synced []string
	err := mkdirAllDurableWithSync(target, 0o700, func(dir string) error {
		synced = append(synced, dir)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{base, first}; !reflect.DeepEqual(synced, want) {
		t.Fatalf("synced parents = %v, want %v", synced, want)
	}
	if st, err := os.Lstat(target); err != nil || !st.IsDir() {
		t.Fatalf("target directory missing: stat=%v err=%v", st, err)
	}
}

func TestMkdirAllDurableStopsAtFirstSyncFailure(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "a", "b")
	fault := errors.New("injected directory sync failure")
	calls := 0
	err := mkdirAllDurableWithSync(target, 0o700, func(dir string) error {
		calls++
		if dir != base {
			t.Fatalf("unexpected sync target: %s", dir)
		}
		return fault
	})
	if !errors.Is(err, fault) || calls != 1 {
		t.Fatalf("err=%v sync calls=%d, want injected fault after first mkdir", err, calls)
	}
	if _, err := os.Lstat(filepath.Join(base, "a")); err != nil {
		t.Fatalf("first mkdir should be retained for recovery: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("child created after parent sync failure: %v", err)
	}
}

func TestMkdirAllDurableRejectsAmbiguousAncestors(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	if err := mkdirAllDurableWithSync(filepath.Join(base, "link", "child"), 0o700, func(string) error { return nil }); err == nil {
		t.Fatal("accepted symlink ancestor")
	}
	if _, err := os.Lstat(filepath.Join(outside, "child")); !os.IsNotExist(err) {
		t.Fatalf("changed symlink target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "file"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mkdirAllDurableWithSync(filepath.Join(base, "file", "child"), 0o700, func(string) error { return nil }); err == nil {
		t.Fatal("accepted file ancestor")
	}
}

func TestMkdirAllDurableRejectsParentSwapBeforeChild(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	parent := filepath.Join(base, "a")
	moved := filepath.Join(base, "a-moved")
	target := filepath.Join(parent, "child")
	swapped := false
	err := mkdirAllDurableWithSync(target, 0o700, func(dir string) error {
		if dir != base || swapped {
			return nil
		}
		if err := os.Rename(parent, moved); err != nil {
			return err
		}
		if err := os.Symlink(outside, parent); err != nil {
			return err
		}
		swapped = true
		return nil
	})
	if !swapped {
		t.Fatalf("parent swap was not reached: %v", err)
	}
	if err == nil {
		t.Error("accepted a replacement parent after validation")
	}
	if _, err := os.Lstat(filepath.Join(outside, "child")); !os.IsNotExist(err) {
		t.Errorf("created a child in the foreign tree: %v", err)
	}
}

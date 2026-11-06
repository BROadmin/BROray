package routes

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRoutePathSwapTransactionWriteRejectsForeignAncestor(t *testing.T) {
	base := t.TempDir()
	foreign := t.TempDir()
	root := filepath.Join(base, "routes")
	if err := os.Symlink(foreign, root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "transactions", "sync-test", "transaction.json")
	if err := writeObj(path, obj{"phase": "captured"}); err == nil {
		t.Error("transaction write accepted a foreign ancestor")
	}
	if _, err := os.Lstat(filepath.Join(foreign, "transactions")); !os.IsNotExist(err) {
		t.Errorf("transaction write changed foreign tree: %v", err)
	}
}

func TestRoutePathSwapUnfinishedTransactionFailsClosed(t *testing.T) {
	base := t.TempDir()
	foreign := t.TempDir()
	root := filepath.Join(base, "routes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(root, "transactions")); err != nil {
		t.Fatal(err)
	}
	e := &engine{root: root, liveTransactions: map[string]bool{}}
	if !e.unfinishedTransaction() {
		t.Error("a foreign transaction directory did not block route writes")
	}
}

func TestRoutePathSwapMissingTransactionDirectoryFailsClosed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "routes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	e := &engine{root: root, liveTransactions: map[string]bool{}}
	if !e.unfinishedTransaction() {
		t.Error("a missing transaction directory did not block route writes")
	}
}

func TestRoutePathSwapRestoreRejectsForeignAncestor(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "existing"
		if missing {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			foreign := t.TempDir()
			root := filepath.Join(base, "routes")
			if err := os.Symlink(foreign, root); err != nil {
				t.Fatal(err)
			}
			foreignFile := filepath.Join(foreign, "state.json")
			sentinel := []byte("foreign sentinel")
			if err := os.WriteFile(foreignFile, sentinel, 0o600); err != nil {
				t.Fatal(err)
			}
			var original []byte
			if !missing {
				original = []byte("original")
			}
			b := &fileBackup{files: map[string][]byte{filepath.Join(root, "state.json"): original}}
			if b.restore() {
				t.Error("restore accepted a foreign ancestor")
			}
			got, err := os.ReadFile(foreignFile)
			if err != nil || !bytes.Equal(got, sentinel) {
				t.Errorf("restore changed foreign file: %q, %v", got, err)
			}
		})
	}
}

func TestRoutePathSwapBackupReadRejectsForeignAncestor(t *testing.T) {
	base := t.TempDir()
	foreign := t.TempDir()
	root := filepath.Join(base, "routes")
	if err := os.Symlink(foreign, root); err != nil {
		t.Fatal(err)
	}
	secret := []byte("foreign route data")
	if err := os.WriteFile(filepath.Join(foreign, "state.json"), secret, 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := backupFilesChecked(filepath.Join(root, "state.json"))
	if err == nil {
		t.Errorf("backup accepted a foreign ancestor and captured %d bytes", len(backup.files[filepath.Join(root, "state.json")]))
	}
}

func TestRoutePathSwapReadObjRejectsForeignAncestor(t *testing.T) {
	base := t.TempDir()
	foreign := t.TempDir()
	root := filepath.Join(base, "routes")
	if err := os.Symlink(foreign, root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "state.json"), []byte(`{"status":"foreign"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := readObj(filepath.Join(root, "state.json")); ok {
		t.Errorf("JSON reader accepted a foreign ancestor: %v", got)
	}
}

func TestRoutePathSwapReadObjRejectsTargetSymlink(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	foreignFile := filepath.Join(foreign, "state.json")
	if err := os.WriteFile(foreignFile, []byte(`{"status":"foreign"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "state.json")
	if err := os.Symlink(foreignFile, target); err != nil {
		t.Fatal(err)
	}
	if got, ok := readObj(target); ok {
		t.Errorf("JSON reader accepted a target symlink: %v", got)
	}
}

func TestRouteTransactionAllocationRejectsSwappedParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "transactions")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	held := filepath.Join(base, "transactions-held")
	beforeCreate := func() {
		if err := os.Rename(parent, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, parent); err != nil {
			t.Fatal(err)
		}
	}
	if dir, err := createRouteTransactionDir(parent, "sync-test-", beforeCreate); err == nil {
		t.Errorf("allocation accepted swapped parent: %s", dir)
	}
	entries, err := os.ReadDir(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("allocation created %d entries in foreign parent", len(entries))
	}
}

func TestRouteTransactionAllocationCreatesOwnedDirectory(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "transactions")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := createRouteTransactionDir(parent, "sync-test-", nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != parent {
		t.Fatalf("unexpected transaction parent: %s", dir)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("transaction directory missing or invalid: info=%v error=%v", info, err)
	}
}

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAllocationRejectsCollisions(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		id       string
		allocate func(string, string) (string, error)
	}{
		{".stage-owned", createBackupStageDir},
		{"settings-before-import-owned", createBackupCopyDir},
	} {
		dir := filepath.Join(root, tc.id)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(dir, "foreign.txt")
		if err := os.WriteFile(marker, []byte("foreign"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := tc.allocate(root, tc.id); err == nil {
			t.Fatalf("reused existing directory %s", tc.id)
		}
		got, err := os.ReadFile(marker)
		if err != nil || string(got) != "foreign" {
			t.Fatalf("foreign directory changed: data=%q err=%v", got, err)
		}
	}
}

func TestBackupAllocationCreatesOwnedStageAndCopy(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "data", "backup")
	stage, err := createBackupStageDir(parent, ".stage-new")
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(stage, backupStageOwnerFile))
	if err != nil || string(marker) != backupStageOwnerText {
		t.Fatalf("owner marker missing: data=%q err=%v", marker, err)
	}
	copy, err := createBackupCopyDir(parent, "settings-before-import-new")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(copy, backupCopyRoot))
	if err != nil || !info.IsDir() {
		t.Fatalf("copy root missing: info=%v err=%v", info, err)
	}
}

func TestBackupStageAllocationStopsBeforeMarkerOnParentSyncFailure(t *testing.T) {
	parent := t.TempDir()
	fault := errors.New("injected parent sync failure")
	calls := 0
	_, err := createBackupStageDirWithSync(parent, ".stage-fault", func(dir string) error {
		calls++
		if dir != parent {
			t.Fatalf("unexpected sync target: %s", dir)
		}
		return fault
	})
	if !errors.Is(err, fault) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want first parent sync failure", err, calls)
	}
	dir := filepath.Join(parent, ".stage-fault")
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		t.Fatalf("created stage not retained: info=%v err=%v", st, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, backupStageOwnerFile)); !os.IsNotExist(err) {
		t.Fatalf("owner marker written after sync failure: %v", err)
	}
}

func TestBackupCopyAllocationStopsBeforeRootOnParentSyncFailure(t *testing.T) {
	parent := t.TempDir()
	fault := errors.New("injected parent sync failure")
	calls := 0
	_, err := createBackupCopyDirWithSync(parent, "copy-fault", func(dir string) error {
		calls++
		if dir != parent {
			t.Fatalf("unexpected sync target: %s", dir)
		}
		return fault
	})
	if !errors.Is(err, fault) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want first parent sync failure", err, calls)
	}
	dir := filepath.Join(parent, "copy-fault")
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		t.Fatalf("created copy not retained: info=%v err=%v", st, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, backupCopyRoot)); !os.IsNotExist(err) {
		t.Fatalf("copy root created after sync failure: %v", err)
	}
}

package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupSwapRejectsUnselectedStageSymlinkBeforeLiveMutation(t *testing.T) {
	a := backupArea(t)
	stage, rel, live := backupPostApplyFixture(t, a)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(stage, "foreign-link")); err != nil {
		t.Fatal(err)
	}
	release, err := a.backupLock("backup-import")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, _, err = a.swapWithCopy(context.Background(), stage, []string{rel}, nil,
		copyManifest{Kind: "pre-import"}, "settings-before-import")
	if err == nil {
		t.Fatal("accepted unselected symlink in stage before swap")
	}
	got, readErr := os.ReadFile(live)
	if readErr != nil || string(got) != "old" {
		t.Fatalf("live changed before stage was fully validated: %q %v", got, readErr)
	}
}

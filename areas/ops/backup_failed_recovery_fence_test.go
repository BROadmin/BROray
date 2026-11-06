package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestBackupFailedRecoveryRetainsPendingFence(t *testing.T) {
	a := backupArea(t)
	release, err := a.backupLock("backup-import")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	copyDir, stageRoot := t.TempDir(), t.TempDir()
	copySentinel := filepath.Join(copyDir, "sentinel")
	stageSentinel := filepath.Join(stageRoot, "sentinel")
	for _, p := range []string{copySentinel, stageSentinel} {
		if err := os.WriteFile(p, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	copy := incompleteBackupCopy{
		dir:       copyDir,
		stageRoot: stageRoot,
		manifest: copyManifest{Paths: []compat.SwapEntry{{
			Path: "data/missing", Existed: true, Staged: true,
			OriginalSHA256: strings.Repeat("a", 64), StageSHA256: strings.Repeat("b", 64),
		}}},
	}
	got := a.recoverFailedBackupSwap(copy, errors.New("injected swap failure"))
	if got == nil || !strings.Contains(got.Error(), "BACKUP_RECOVERY_REQUIRED") {
		t.Fatalf("failed rollback did not report recovery requirement: %v", got)
	}
	release()
	holder, held := a.d.Lock.Current()
	if !held || !holder.Pending || holder.Resumable || holder.Action != "system:backup-import" {
		t.Fatalf("failed recovery did not retain non-resumable fence: held=%v holder=%+v", held, holder)
	}
	restored := compat.NewGlobalLock(a.d.RunPath("global-operation.json"))
	holder, held = restored.Current()
	if !held || !holder.Pending || holder.Resumable {
		t.Fatalf("pending fence did not survive reload: held=%v holder=%+v", held, holder)
	}
	for _, p := range []string{copySentinel, stageSentinel} {
		b, err := os.ReadFile(p)
		if err != nil || string(b) != "keep" {
			t.Fatalf("recovery input changed: %s %q %v", p, b, err)
		}
	}
}

func TestBackupSuccessfulRecoveryReleasesFence(t *testing.T) {
	a := backupArea(t)
	release, err := a.backupLock("backup-import")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cause := errors.New("injected swap failure")
	copy := incompleteBackupCopy{
		dir:       t.TempDir(),
		stageRoot: t.TempDir(),
		manifest:  copyManifest{Paths: nil},
	}
	got := a.recoverFailedBackupSwap(copy, cause)
	if !errors.Is(got, cause) {
		t.Fatalf("original swap error lost: %v", got)
	}
	release()
	if holder, held := a.d.Lock.Current(); held {
		t.Fatalf("successful recovery left fence held: %+v", holder)
	}
}

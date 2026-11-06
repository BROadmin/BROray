package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func writeInterruptedBackupFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The first path has been replaced, while the second still contains its old
// value. This is the durable state after a process stops between two renames.
func interruptedBackupFixture(t *testing.T, a *area) (stage, liveA, liveB string) {
	t.Helper()
	stageRel := ".stage-1234567890abcdef"
	stage = a.backupDir(stageRel)
	copyID := "settings-before-import-20261104-120000-deadbeef"
	copyDir := a.backupDir(copyID)
	liveA = filepath.Join(a.d.Root.Dir, "data", "recovery", "a.json")
	liveB = filepath.Join(a.d.Root.Dir, "data", "recovery", "b.json")
	writeInterruptedBackupFile(t, filepath.Join(stage, backupStageOwnerFile), backupStageOwnerText)
	writeInterruptedBackupFile(t, filepath.Join(stage, "data", "recovery", "b.json"), "new-b")
	writeInterruptedBackupFile(t, liveA, "new-a")
	writeInterruptedBackupFile(t, liveB, "old-b")
	writeInterruptedBackupFile(t, filepath.Join(copyDir, backupCopyRoot, "data", "recovery", "a.json"), "old-a")
	shaA, err := compat.BackupDigest(a.d.Root.Dir, "data/recovery/a.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	shaB, err := compat.BackupDigest(stage, "data/recovery/b.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	preRoot := filepath.Join(copyDir, backupCopyRoot)
	oldA, err := compat.BackupDigest(preRoot, "data/recovery/a.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	oldB, err := compat.BackupDigest(a.d.Root.Dir, "data/recovery/b.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schemaVersion": 1, "kind": "pre-import", "complete": false,
		"stageRel": stageRel,
		"paths": []compat.SwapEntry{
			{Path: "data/recovery/a.json", Existed: true, Staged: true, StageSHA256: shaA, OriginalSHA256: oldA},
			{Path: "data/recovery/b.json", Existed: true, Staged: true, StageSHA256: shaB, OriginalSHA256: oldB},
		},
	}
	if err := compat.WriteJSON(filepath.Join(copyDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	return stage, liveA, liveB
}

func TestBackupInterruptedSwapRecoveredOnStartup(t *testing.T) {
	a := backupArea(t)
	a.d.Maintenance = true
	_, liveA, liveB := interruptedBackupFixture(t, a)
	if err := setup(a.d); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ path, want string }{
		{liveA, "old-a"}, {liveB, "old-b"},
	} {
		got, err := os.ReadFile(item.path)
		if err != nil || string(got) != item.want {
			t.Errorf("interrupted import left mixed live settings at %s: %q, %v", item.path, got, err)
		}
	}
}

func TestBackupMissingManifestWithOriginalBlocksStartup(t *testing.T) {
	a := backupArea(t)
	a.d.Maintenance = true
	_, liveA, _ := interruptedBackupFixture(t, a)
	manifest := a.backupDir("settings-before-import-20261104-120000-deadbeef", "manifest.json")
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := setup(a.d); err == nil {
		t.Fatal("missing copy manifest allowed startup with mixed live settings")
	}
	got, err := os.ReadFile(liveA)
	if err != nil || string(got) != "new-a" {
		t.Fatalf("uncertain live setting changed during failed recovery: %q, %v", got, err)
	}
}

func TestBackupMissingManifestWithEmptyCopyBlocksStartup(t *testing.T) {
	a := backupArea(t)
	a.d.Maintenance = true
	if _, _, err := a.newCopyDir("settings-before-import"); err != nil {
		t.Fatal(err)
	}
	if err := setup(a.d); err == nil {
		t.Fatal("missing copy manifest with empty original root allowed startup")
	}
}

func TestBackupClearStagesKeepsIncompleteImport(t *testing.T) {
	a := backupArea(t)
	stage, _, _ := interruptedBackupFixture(t, a)
	a.clearStages()
	if _, err := os.Lstat(stage); err != nil {
		t.Fatalf("incomplete import stage removed before recovery: %v", err)
	}
}

func TestBackupSwapWritesRecoveryPlan(t *testing.T) {
	a := backupArea(t)
	stageRel := ".stage-abcdef0123456789"
	stage := a.backupDir(stageRel)
	writeInterruptedBackupFile(t, filepath.Join(stage, backupStageOwnerFile), backupStageOwnerText)
	rel := "data/recovery/value.json"
	writeInterruptedBackupFile(t, filepath.Join(stage, filepath.FromSlash(rel)), "new")
	writeInterruptedBackupFile(t, filepath.Join(a.d.Root.Dir, filepath.FromSlash(rel)), "old")
	release, err := a.backupLock("backup-import")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	id, _, err := a.swapWithCopy(context.Background(), stage, []string{rel}, nil,
		copyManifest{Kind: "pre-import"}, "settings-before-import")
	if err != nil {
		t.Fatal(err)
	}
	var man copyManifest
	if err := compat.ReadJSON(a.backupDir(id, "manifest.json"), &man); err != nil {
		t.Fatal(err)
	}
	if !man.Complete || man.StageRel != stageRel || len(man.Paths) != 1 ||
		!man.Paths[0].Existed || !man.Paths[0].Staged || man.Paths[0].StageSHA256 == "" {
		t.Fatalf("backup manifest lacks the pre-swap recovery plan: %+v", man)
	}
	var raw map[string]any
	if err := compat.ReadJSON(a.backupDir(id, "manifest.json"), &raw); err != nil {
		t.Fatal(err)
	}
	path := raw["paths"].([]any)[0].(map[string]any)
	if path["originalSha256"] == nil || path["originalSha256"] == "" {
		t.Fatalf("backup manifest lacks original content digest: %+v", path)
	}
}

func TestBackupRecoveryRefusesChangedOriginalCopy(t *testing.T) {
	a := backupArea(t)
	a.d.Maintenance = true
	_, liveA, _ := interruptedBackupFixture(t, a)
	copyID := "settings-before-import-20261104-120000-deadbeef"
	preRoot := a.backupDir(copyID, backupCopyRoot)
	preA := filepath.Join(preRoot, "data", "recovery", "a.json")
	originalDigest, err := compat.BackupDigest(preRoot, "data/recovery/a.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := a.backupDir(copyID, "manifest.json")
	var man map[string]any
	if err := compat.ReadJSON(manifestPath, &man); err != nil {
		t.Fatal(err)
	}
	man["paths"].([]any)[0].(map[string]any)["originalSha256"] = originalDigest
	if err := compat.WriteJSON(manifestPath, man); err != nil {
		t.Fatal(err)
	}
	writeInterruptedBackupFile(t, preA, "foreign-a")
	if err := setup(a.d); err == nil {
		t.Fatal("recovery accepted changed original content")
	}
	if got, err := os.ReadFile(liveA); err != nil || string(got) != "new-a" {
		t.Fatalf("ambiguous recovery replaced live content: %q %v", got, err)
	}
	if got, err := os.ReadFile(preA); err != nil || string(got) != "foreign-a" {
		t.Fatalf("ambiguous recovery removed changed pre-copy: %q %v", got, err)
	}
}

func TestBackupRecoveryChecksAllPathsBeforeMutation(t *testing.T) {
	a := backupArea(t)
	a.d.Maintenance = true
	stage, liveA, liveB := interruptedBackupFixture(t, a)
	copyID := "settings-before-import-20261104-120000-deadbeef"
	preA := a.backupDir(copyID, backupCopyRoot, "data", "recovery", "a.json")
	preB := a.backupDir(copyID, backupCopyRoot, "data", "recovery", "b.json")
	stageB := filepath.Join(stage, "data", "recovery", "b.json")
	if err := os.Rename(liveB, preB); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stageB, liveB); err != nil {
		t.Fatal(err)
	}
	writeInterruptedBackupFile(t, preA, "foreign-a")
	if err := setup(a.d); err == nil {
		t.Fatal("recovery accepted a changed original")
	}
	for _, item := range []struct{ path, want string }{
		{liveA, "new-a"}, {liveB, "new-b"}, {preA, "foreign-a"}, {preB, "old-b"},
	} {
		got, err := os.ReadFile(item.path)
		if err != nil || string(got) != item.want {
			t.Errorf("recovery changed %s before validating every path: %q %v", item.path, got, err)
		}
	}
}

func TestBackupInterruptedAfterExchangeRecovered(t *testing.T) {
	a := backupArea(t)
	a.d.Maintenance = true
	stage, liveA, liveB := interruptedBackupFixture(t, a)
	copyID := "settings-before-import-20261104-120000-deadbeef"
	manifestPath := a.backupDir(copyID, "manifest.json")
	var man copyManifest
	if err := compat.ReadJSON(manifestPath, &man); err != nil {
		t.Fatal(err)
	}
	newDigest, err := compat.BackupDigest(a.d.Root.Dir, man.Paths[0].Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	man.Paths[0].StageSHA256 = newDigest
	if err := os.Remove(a.backupDir(copyID, backupCopyRoot, filepath.FromSlash(man.Paths[0].Path))); err != nil {
		t.Fatal(err)
	}
	writeInterruptedBackupFile(t, filepath.Join(stage, filepath.FromSlash(man.Paths[0].Path)), "old-a")
	if err := compat.WriteJSON(manifestPath, man); err != nil {
		t.Fatal(err)
	}
	if err := setup(a.d); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ path, want string }{{liveA, "old-a"}, {liveB, "old-b"}} {
		got, err := os.ReadFile(item.path)
		if err != nil || string(got) != item.want {
			t.Errorf("exchange interruption left mixed live settings at %s: %q, %v", item.path, got, err)
		}
	}
}

func TestBackupRecoveryRefusesChangedOriginalAfterExchange(t *testing.T) {
	a := backupArea(t)
	a.d.Maintenance = true
	stage, liveA, _ := interruptedBackupFixture(t, a)
	copyID := "settings-before-import-20261104-120000-deadbeef"
	rel := filepath.Join("data", "recovery", "a.json")
	if err := os.Remove(a.backupDir(copyID, backupCopyRoot, rel)); err != nil {
		t.Fatal(err)
	}
	stageA := filepath.Join(stage, rel)
	writeInterruptedBackupFile(t, stageA, "foreign-a")
	if err := setup(a.d); err == nil {
		t.Fatal("recovery accepted changed original after exchange")
	}
	if got, err := os.ReadFile(liveA); err != nil || string(got) != "new-a" {
		t.Fatalf("ambiguous exchange replaced live content: %q %v", got, err)
	}
	if got, err := os.ReadFile(stageA); err != nil || string(got) != "foreign-a" {
		t.Fatalf("ambiguous exchange removed changed original: %q %v", got, err)
	}
}

func backupPostApplyFixture(t *testing.T, a *area) (stage, rel, live string) {
	t.Helper()
	stage = a.backupDir(".stage-fedcba9876543210")
	writeInterruptedBackupFile(t, filepath.Join(stage, backupStageOwnerFile), backupStageOwnerText)
	rel = "data/recovery/post-apply.json"
	live = filepath.Join(a.d.Root.Dir, filepath.FromSlash(rel))
	writeInterruptedBackupFile(t, filepath.Join(stage, filepath.FromSlash(rel)), "new")
	writeInterruptedBackupFile(t, live, "old")
	return stage, rel, live
}

func TestBackupCompleteManifestHasDigestBeforeAfter(t *testing.T) {
	a := backupArea(t)
	stage, rel, _ := backupPostApplyFixture(t, a)
	sawAfter := false
	part := fakePart(a)
	part.After = func(context.Context, any) compat.BackupPlan {
		sawAfter = true
		copies, err := filepath.Glob(a.backupDir("settings-before-import-*"))
		if err != nil || len(copies) != 1 {
			t.Fatalf("copy at After: %v, %v", copies, err)
		}
		var man copyManifest
		if err := compat.ReadJSON(filepath.Join(copies[0], "manifest.json"), &man); err != nil {
			t.Fatal(err)
		}
		if !man.Complete || len(man.Paths) != 1 || man.Paths[0].SHA256 == "" {
			t.Errorf("completed manifest lacks live digest before After: %+v", man)
		}
		return compat.BackupPlan{}
	}
	a.parts = func() []compat.BackupPart { return []compat.BackupPart{part} }
	id, _, err := a.swapWithCopy(context.Background(), stage, []string{rel}, nil,
		copyManifest{Kind: "pre-import"}, "settings-before-import")
	if err != nil || id == "" || !sawAfter {
		t.Fatalf("swap=%q after=%v err=%v", id, sawAfter, err)
	}
}

func TestBackupPostApplyManifestWriteFailureIsReported(t *testing.T) {
	a := backupArea(t)
	stage, rel, live := backupPostApplyFixture(t, a)
	part := fakePart(a)
	var afterErr error
	part.After = func(context.Context, any) compat.BackupPlan {
		copies, err := filepath.Glob(a.backupDir("settings-before-import-*"))
		if err != nil || len(copies) != 1 {
			if err == nil {
				err = os.ErrNotExist
			}
			afterErr = err
			return compat.BackupPlan{}
		}
		if err := os.WriteFile(live, []byte("post-hook"), 0o600); err != nil {
			afterErr = err
			return compat.BackupPlan{}
		}
		manifest := filepath.Join(copies[0], "manifest.json")
		if err := os.Rename(manifest, manifest+".saved"); err != nil {
			afterErr = err
			return compat.BackupPlan{}
		}
		afterErr = os.Mkdir(manifest, 0o700)
		return compat.BackupPlan{}
	}
	a.parts = func() []compat.BackupPart { return []compat.BackupPart{part} }
	_, _, err := a.swapWithCopy(context.Background(), stage, []string{rel}, nil,
		copyManifest{Kind: "pre-import"}, "settings-before-import")
	if afterErr != nil {
		t.Fatal(afterErr)
	}
	if err == nil || !strings.Contains(err.Error(), "BACKUP_APPLIED_MANIFEST_INCOMPLETE") {
		t.Fatalf("post-apply manifest write fault was reported as success: %v", err)
	}
	if got, readErr := os.ReadFile(live); readErr != nil || string(got) != "post-hook" {
		t.Fatalf("post-hook state was silently rolled back: %q %v", got, readErr)
	}
}

package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

type incompleteBackupCopy struct {
	dir       string
	stageRoot string
	manifest  copyManifest
}

func (a *area) relativeBackupStage(stageRoot, kind string) (string, error) {
	rel, err := filepath.Rel(a.backupDir(), stageRoot)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	checked, err := a.backupStageRoot(copyManifest{Kind: kind, StageRel: rel})
	if err != nil {
		return "", err
	}
	if filepath.Clean(checked) != filepath.Clean(stageRoot) {
		return "", fmt.Errorf("backup stage path changed")
	}
	return rel, nil
}

// incompleteBackupCopies inspects only our backup copy names. A missing or
// malformed manifest is ambiguous: the copy may have lost its recovery plan
// after a live-path swap, even when its original root is empty.
func (a *area) incompleteBackupCopies() ([]incompleteBackupCopy, error) {
	entries, err := os.ReadDir(a.backupDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var copies []incompleteBackupCopy
	for _, item := range entries {
		id := item.Name()
		if !reBackupPruneCopyID.MatchString(id) {
			continue
		}
		dir := a.backupDir(id)
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("backup recovery: ambiguous copy %s: %v", id, err)
		}
		path := filepath.Join(dir, "manifest.json")
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return nil, fmt.Errorf("backup recovery: missing manifest for owned copy %s", id)
		} else if err != nil {
			return nil, err
		}
		var man copyManifest
		if err := compat.ReadJSON(path, &man); err != nil {
			return nil, fmt.Errorf("backup recovery: manifest %s: %w", id, err)
		}
		if man.Complete || man.Recovered {
			continue
		}
		kind := "pre-import"
		if strings.HasPrefix(id, "settings-before-undo-") {
			kind = "pre-undo"
		}
		if man.SchemaVersion != 1 || man.Kind != kind {
			return nil, fmt.Errorf("backup recovery: invalid manifest for %s", id)
		}
		stageRoot, err := a.backupStageRoot(man)
		if err != nil {
			return nil, fmt.Errorf("backup recovery: %s: %w", id, err)
		}
		seen := map[string]bool{}
		for _, entry := range man.Paths {
			if !copyPathAllowed(entry.Path) || seen[entry.Path] {
				return nil, fmt.Errorf("backup recovery: invalid path in %s", id)
			}
			seen[entry.Path] = true
		}
		copies = append(copies, incompleteBackupCopy{dir: dir, stageRoot: stageRoot, manifest: man})
	}
	return copies, nil
}

// backupStageRoot validates the source of an interrupted operation. Import
// stages have an owner marker; undo stages are roots of completed owned copies.
func (a *area) backupStageRoot(man copyManifest) (string, error) {
	rel := man.StageRel
	if rel == "" || filepath.ToSlash(filepath.Clean(rel)) != rel || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid stage path")
	}
	var stage string
	switch man.Kind {
	case "pre-import":
		if !reBackupStageID.MatchString(rel) {
			return "", fmt.Errorf("invalid import stage")
		}
		stage = a.backupDir(rel)
		info, err := os.Lstat(stage)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("missing import stage: %v", err)
		}
		owner, err := os.ReadFile(filepath.Join(stage, backupStageOwnerFile))
		if err != nil || string(owner) != backupStageOwnerText {
			return "", fmt.Errorf("import stage owner is uncertain")
		}
	case "pre-undo":
		parts := strings.Split(rel, "/")
		if len(parts) != 2 || !reBackupCopyID.MatchString(parts[0]) || parts[1] != backupCopyRoot {
			return "", fmt.Errorf("invalid undo stage")
		}
		if _, ok := a.readCopy(parts[0]); !ok {
			return "", fmt.Errorf("undo source copy is unavailable")
		}
		stage = a.backupDir(parts[0], backupCopyRoot)
		info, err := os.Lstat(stage)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("missing undo stage: %v", err)
		}
	default:
		return "", fmt.Errorf("invalid backup operation")
	}
	return stage, nil
}

func (a *area) incompleteStageRefs() (map[string]bool, error) {
	copies, err := a.incompleteBackupCopies()
	if err != nil {
		return nil, err
	}
	refs := map[string]bool{}
	for _, copy := range copies {
		if copy.manifest.Kind == "pre-import" {
			refs[copy.manifest.StageRel] = true
		}
	}
	return refs, nil
}

// recoverIncompleteBackups runs during area setup, before its handlers and
// loops are installed. The global fence remains pending after an ambiguous
// rollback, so other areas cannot change the live settings silently.
func (a *area) recoverIncompleteBackups() error {
	copies, err := a.incompleteBackupCopies()
	if err != nil || len(copies) == 0 {
		return err
	}
	var release compat.Release
	var ok bool
	if h, held := a.d.Lock.Current(); held && h.Pending && h.Scope == "system" &&
		(h.Action == "system:backup-recovery" || h.Action == "system:backup-import" || h.Action == "system:backup-undo") {
		release, ok = a.d.Lock.TakePending("system", "", "system:backup-recovery")
	} else {
		release, ok = a.d.Lock.TryAcquire("system", "system:backup-recovery", "")
	}
	if !ok {
		return fmt.Errorf("backup recovery: global fence is busy")
	}
	defer release()
	for _, copy := range copies {
		if err := a.rollbackIncompleteCopy(copy); err != nil {
			pendingErr := a.d.Lock.MarkPending(false)
			return fmt.Errorf("backup recovery: %s: %w", filepath.Base(copy.dir), errors.Join(err, pendingErr))
		}
	}
	return nil
}

func backupPathExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return false, fmt.Errorf("ambiguous path type at %s", path)
	}
	return true, nil
}

func backupRename(src, dst string) error {
	srcParent, dstParent := filepath.Dir(src), filepath.Dir(dst)
	if err := platform.MkdirAllDurable(dstParent, 0o700); err != nil {
		return err
	}
	srcDir, err := platform.OpenDirNoFollow(srcParent)
	if err != nil {
		return err
	}
	defer srcDir.Close()
	dstDir, err := platform.OpenDirNoFollow(dstParent)
	if err != nil {
		return err
	}
	defer dstDir.Close()
	if err := backupParentUnchanged(srcParent, srcDir); err != nil {
		return err
	}
	if err := backupParentUnchanged(dstParent, dstDir); err != nil {
		return err
	}
	if err := unix.Renameat2(int(srcDir.Fd()), filepath.Base(src), int(dstDir.Fd()), filepath.Base(dst), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := srcDir.Sync(); err != nil {
		return err
	}
	if srcParent != dstParent {
		if err := dstDir.Sync(); err != nil {
			return err
		}
	}
	if err := backupParentUnchanged(srcParent, srcDir); err != nil {
		return err
	}
	return backupParentUnchanged(dstParent, dstDir)
}

func backupParentUnchanged(path string, held *os.File) error {
	current, err := platform.OpenDirNoFollow(path)
	if err != nil {
		return err
	}
	defer current.Close()
	before, err := held.Stat()
	if err != nil {
		return err
	}
	after, err := current.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return fmt.Errorf("backup parent changed: %s", path)
	}
	return nil
}
func backupMatches(root string, entry compat.SwapEntry) (bool, error) {
	if entry.StageSHA256 == "" {
		return false, fmt.Errorf("staged content digest is missing")
	}
	digest, err := compat.BackupDigest(root, entry.Path, nil)
	return digest == entry.StageSHA256, err
}

func backupOriginalMatches(root string, entry compat.SwapEntry) error {
	if entry.OriginalSHA256 == "" {
		return fmt.Errorf("original content digest is missing")
	}
	digest, err := compat.BackupDigest(root, entry.Path, nil)
	if err != nil {
		return err
	}
	if digest != entry.OriginalSHA256 {
		return fmt.Errorf("original content changed")
	}
	return nil
}

func (a *area) rollbackIncompleteCopy(copy incompleteBackupCopy) error {
	// Validate every path before changing any live setting. The second pass
	// rechecks each path because another process could alter the filesystem.
	for _, apply := range []bool{false, true} {
		for i := len(copy.manifest.Paths) - 1; i >= 0; i-- {
			entry := copy.manifest.Paths[i]
			if err := a.rollbackBackupPath(copy, entry, apply); err != nil {
				return fmt.Errorf("%s: %w", entry.Path, err)
			}
		}
	}
	copy.manifest.Recovered = true
	return compat.WriteJSON(filepath.Join(copy.dir, "manifest.json"), copy.manifest)
}

func (a *area) rollbackBackupPath(copy incompleteBackupCopy, entry compat.SwapEntry, apply bool) error {
	rel := filepath.FromSlash(entry.Path)
	live := filepath.Join(a.d.Root.Dir, rel)
	stage := filepath.Join(copy.stageRoot, rel)
	pre := filepath.Join(copy.dir, backupCopyRoot, rel)
	liveExists, err := backupPathExists(live)
	if err != nil {
		return err
	}
	stageExists, err := backupPathExists(stage)
	if err != nil {
		return err
	}
	preExists, err := backupPathExists(pre)
	if err != nil {
		return err
	}
	if preExists {
		if !entry.Existed || (liveExists && stageExists) {
			return fmt.Errorf("ambiguous original and stage")
		}
		if err := backupOriginalMatches(filepath.Join(copy.dir, backupCopyRoot), entry); err != nil {
			return err
		}
		if liveExists {
			if !entry.Staged {
				return fmt.Errorf("unexpected live replacement")
			}
			matches, err := backupMatches(a.d.Root.Dir, entry)
			if err != nil || !matches {
				return fmt.Errorf("live replacement does not match the staged plan: %v", err)
			}
		} else if stageExists {
			if !entry.Staged {
				return fmt.Errorf("unexpected staged path")
			}
			matches, err := backupMatches(copy.stageRoot, entry)
			if err != nil || !matches {
				return fmt.Errorf("staged replacement changed: %v", err)
			}
		}
		if !apply {
			return nil
		}
		if liveExists {
			if err := backupRename(live, stage); err != nil {
				return err
			}
		}
		return backupRename(pre, live)
	}
	if entry.Existed {
		if !liveExists || (entry.Staged && !stageExists) {
			return fmt.Errorf("original path missing or stage lost")
		}
		if entry.Staged {
			stageMatches, err := backupMatches(copy.stageRoot, entry)
			if err != nil {
				return err
			}
			if stageMatches {
				return backupOriginalMatches(a.d.Root.Dir, entry) // path was not swapped
			}
			liveMatches, err := backupMatches(a.d.Root.Dir, entry)
			if err != nil || !liveMatches {
				return fmt.Errorf("stage and live do not match an exchange: %v", err)
			}
			if err := backupOriginalMatches(copy.stageRoot, entry); err != nil {
				return err
			}
			if !apply {
				return nil
			}
			return compat.UndoBackupExchange(stage, live)
		}
		if stageExists {
			return fmt.Errorf("unexpected staged path")
		}
		return backupOriginalMatches(a.d.Root.Dir, entry)
	}
	if !entry.Staged {
		if liveExists || stageExists {
			return fmt.Errorf("unexpected path without staged content")
		}
		return nil
	}
	if liveExists && !stageExists {
		matches, err := backupMatches(a.d.Root.Dir, entry)
		if err != nil || !matches {
			return fmt.Errorf("inserted path changed: %v", err)
		}
		if !apply {
			return nil
		}
		return backupRename(live, stage)
	}
	if !liveExists && stageExists {
		matches, err := backupMatches(copy.stageRoot, entry)
		if err != nil || !matches {
			return fmt.Errorf("staged insertion changed: %v", err)
		}
		return nil
	}
	return fmt.Errorf("ambiguous inserted path")
}

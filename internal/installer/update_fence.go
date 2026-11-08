package installer

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// Published 3.5.0/3.5.1 can exit before their update watcher's deferred
// release runs. Transfer only this live update's exact fence, after the
// authenticated stop and acquisition of the daemon's kernel lock. An arbitrary
// stale/pending/foreign record is never evidence of ownership.
type updateFence struct {
	path string
	info os.FileInfo
	hash [sha256.Size]byte
}

var handoffOperationID = regexp.MustCompile(`^(update|reinstall)-[0-9]{14}-[0-9]+(?:-[0-9a-f]{16})?$`)

func readHandoffFile(path string) (os.FileInfo, []byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > 256<<10 {
		return nil, nil, errors.New("unbounded or nonregular handoff record")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(st, opened) {
		return nil, nil, errors.New("handoff record changed on open")
	}
	b, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if err != nil || int64(len(b)) != st.Size() {
		return nil, nil, errors.New("handoff record changed on read")
	}
	now, err := os.Lstat(path)
	if err != nil || !os.SameFile(st, now) || now.Size() != st.Size() || !now.ModTime().Equal(st.ModTime()) {
		return nil, nil, errors.New("handoff record replaced")
	}
	return st, b, nil
}

func captureUpdateFence(root platform.Root) *updateFence {
	for _, path := range []string{root.LauncherLock(), root.InstallerLock(), root.DaemonLock()} {
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() {
			return nil
		}
		held, err := platform.HeldChecked(path)
		if err != nil || !held {
			return nil
		}
	}
	_, raw, err := readHandoffFile(root.UpdateJournal())
	if err != nil {
		return nil
	}
	var j Journal
	if json.Unmarshal(raw, &j) != nil || j.SchemaVersion != 1 || j.State != JStopping || !handoffOperationID.MatchString(j.OperationID) || (j.Operation != "update" && j.Operation != "reinstall") {
		return nil
	}
	var page struct {
		OperationID  string    `json:"operationId"`
		Operation    string    `json:"operation"`
		Running      bool      `json:"running"`
		WatchLock    string    `json:"watchLock"`
		ProgressFile string    `json:"progressFile"`
		UpdatedAt    time.Time `json:"updatedAt"`
	}
	_, raw, err = readHandoffFile(filepath.Join(root.Data(), "state", "broray-operation.json"))
	if err != nil || json.Unmarshal(raw, &page) != nil || !page.Running || page.OperationID != j.OperationID || page.Operation != j.Operation || page.WatchLock != root.LauncherLock() || page.ProgressFile != filepath.Join(root.Data(), "state", "update", j.OperationID+".jsonl") {
		return nil
	}
	path := filepath.Join(root.Run(), "global-operation.json")
	st, raw, err := readHandoffFile(path)
	if err != nil {
		return nil
	}
	var h struct {
		Scope     string    `json:"scope"`
		Action    string    `json:"action"`
		BundleID  string    `json:"bundleId"`
		Pending   bool      `json:"pending"`
		Resumable bool      `json:"resumable"`
		StartedAt time.Time `json:"startedAt"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if json.Unmarshal(raw, &h) != nil || h.Scope != "system" || h.Action != j.Operation || h.BundleID != "" || h.Pending || h.Resumable || h.StartedAt.IsZero() || h.UpdatedAt.Before(h.StartedAt) || page.UpdatedAt.Before(h.StartedAt) {
		return nil
	}
	return &updateFence{path: path, info: st, hash: sha256.Sum256(raw)}
}

func (f *updateFence) releaseStopped() error {
	if f == nil {
		return nil
	}
	st, b, err := readHandoffFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} // old watcher released it itself
	if err != nil || !os.SameFile(f.info, st) || sha256.Sum256(b) != f.hash {
		return fmt.Errorf("UPDATE_HANDOFF_FENCE_CHANGED: updater fence changed during shutdown; preserved")
	}
	if err := os.Remove(f.path); err != nil {
		return fmt.Errorf("UPDATE_HANDOFF_FENCE_REMOVE: %w", err)
	}
	return platform.SyncDir(filepath.Dir(f.path))
}

func stopUpdateDaemon(root platform.Root, timeout time.Duration) (*platform.Lock, bool, error) {
	fence := captureUpdateFence(root)
	l, was, err := stopDaemon(root, timeout)
	if err != nil || !was {
		return l, was, err
	}
	// stopDaemon returns a held lock; no old daemon or new process can still
	// own this record. The detached launcher/installer continue fencing writes.
	return l, was, fence.releaseStopped()
}

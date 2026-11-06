package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateWatchKeepsOperationRunningWhenLockCannotBeProbed(t *testing.T) {
	a := testArea(t)
	lock := filepath.Join(t.TempDir(), "ambiguous.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	progress := filepath.Join(t.TempDir(), "progress.jsonl")
	if err := os.WriteFile(progress, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id := "ambiguous-launcher-lock"
	if !a.page.begin(&pageOp{OperationID: id, Operation: "update", State: "running", Running: true,
		ProgressFile: progress, WatchLock: lock}) {
		t.Fatal("could not begin page operation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		a.watch(ctx, id, func() {}, time.Now().Add(-spawnGrace-time.Second))
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("watcher treated an unreadable launcher lock as a confirmed exit")
	case <-time.After(2 * watchPollPeriod):
	}
	if !a.page.running() {
		t.Fatal("operation was marked terminal while launcher lock state was unknown")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop after context cancellation")
	}
}

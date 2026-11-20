package compat

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func reserveQueueDeps(t *testing.T) *Deps {
	t.Helper()
	d := testDeps(t)
	guard, err := platform.TryLock(d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Release(); err != nil {
			t.Error(err)
		}
	})
	if err := d.Lock.ReconcileDaemonReserve(d.Root, guard, false, nil); err != nil {
		t.Fatal(err)
	}
	return d
}

func runReserveQueue(t *testing.T, d *Deps, ctx context.Context, action, target string, stages []Stage) *Request {
	t.Helper()
	d.Queue.Define(ActionSpec{Name: action, Type: "server_operation", Priority: map[string]int{"*": 1}, Stages: stages})
	r, _, err := d.Queue.Submit(action, target, "USER", "revision", RandHex(16))
	if err != nil {
		t.Fatal(err)
	}
	running, spec, op := d.Queue.next()
	d.Queue.execute(ctx, running, spec, op)
	got, ok := d.Queue.Request(r.RequestID)
	if !ok {
		t.Fatal("receipt missing")
	}
	return got
}

func reserveQueueJournal(d *Deps) string {
	return filepath.Join(d.Root.Config(), "server-reserve-pending.v1.json")
}

// Losing the post-stage journal check lets another mutation run over a
// partially installed reserve transaction, even when the callback panics.
func TestReserveQueueRetainsUnfinishedTransaction(t *testing.T) {
	for _, kind := range []string{"error", "panic", "success-with-journal", "cancel", "linked-journal"} {
		t.Run(kind, func(t *testing.T) {
			d := reserveQueueDeps(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			foreign := filepath.Join(d.Root.Dir, "foreign")
			got := runReserveQueue(t, d, ctx, ReserveSwitchAction, ReserveOperationTarget, []Stage{{Name: "switch", Global: true, Protected: true, Run: func(task *Task) error {
				if kind == "linked-journal" {
					if err := os.WriteFile(foreign, []byte("untouched"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(foreign, reserveQueueJournal(d)); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(reserveQueueJournal(d), []byte("uncertain transaction"), 0600); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "panic":
					panic("after durable begin")
				case "success-with-journal", "linked-journal":
					return nil
				case "cancel":
					cancel()
					return context.Canceled
				}
				return errors.New("router verification failed")
			}}})
			if got.State != "failed" || got.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" {
				t.Errorf("recovery receipt: %+v", got)
			}
			owner, ok := d.Lock.Current()
			if !ok || !owner.Pending || owner.Resumable || !owner.DaemonOwnedReserve {
				t.Errorf("recovery fence lost: %+v %v", owner, ok)
			}
			if release, ok := d.Lock.TryAcquire("operations", "servers:activate", "other"); ok {
				release()
				t.Error("competing mutation acquired")
			}
			restarted := NewGlobalLock(d.RunPath("global-operation.json"))
			if !restarted.Busy() {
				t.Error("restart lost recovery fence")
			}
			if _, err := os.Lstat(reserveQueueJournal(d)); err != nil {
				t.Error("journal removed", err)
			}
			if kind == "linked-journal" {
				b, err := os.ReadFile(foreign)
				if err != nil || string(b) != "untouched" {
					t.Error("foreign journal altered", err)
				}
			}
		})
	}
}

func TestReserveQueueRequiresProtectedOwnedSingleStage(t *testing.T) {
	for _, kind := range []string{"unmarked", "wrong-target", "cooperative", "nonglobal", "two-stages", "preexisting-journal"} {
		t.Run(kind, func(t *testing.T) {
			d := reserveQueueDeps(t)
			if kind == "unmarked" {
				d.Lock = NewGlobalLock(d.RunPath("global-operation.json"))
			}
			target := ReserveOperationTarget
			if kind == "wrong-target" {
				target = "foreign"
			}
			if kind == "preexisting-journal" {
				if err := os.WriteFile(reserveQueueJournal(d), []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ran := false
			stages := []Stage{{Name: "mode", Global: kind != "nonglobal", Protected: kind != "cooperative", Run: func(*Task) error { ran = true; return nil }}}
			if kind == "two-stages" {
				stages = append(stages, stages[0])
			}
			got := runReserveQueue(t, d, context.Background(), ReserveModeAction, target, stages)
			if ran || got.State != "failed" {
				t.Fatalf("unsafe reserve callback executed: ran=%v receipt=%+v", ran, got)
			}
		})
	}
}

func TestReserveQueueSuccessfulCommitReleasesFence(t *testing.T) {
	for _, action := range []string{ReserveModeAction, ReserveSwitchAction} {
		t.Run(action, func(t *testing.T) {
			d := reserveQueueDeps(t)
			got := runReserveQueue(t, d, context.Background(), action, ReserveOperationTarget, []Stage{{Name: "commit", Global: true, Protected: true, Run: func(task *Task) error {
				if !FenceHeld(task.Ctx) {
					return errors.New("reserve executor received no fence authority")
				}
				owner, ok := d.Lock.Current()
				if !ok || !owner.DaemonOwnedReserve || owner.Action != action {
					return errors.New("wrong owner")
				}
				return nil
			}}})
			if got.State != "completed" || d.Lock.Busy() {
				t.Fatalf("clean commit: %+v busy=%v", got, d.Lock.Busy())
			}
		})
	}
}

func TestReserveQueueOwnerReplacementIsNotSuccess(t *testing.T) {
	d := reserveQueueDeps(t)
	path := d.RunPath("global-operation.json")
	foreign := []byte(`{"foreign":"owner"}`)
	got := runReserveQueue(t, d, context.Background(), ReserveModeAction, ReserveOperationTarget, []Stage{{Name: "mode", Global: true, Protected: true, Run: func(*Task) error {
		return os.WriteFile(path, foreign, 0600)
	}}})
	if got.State != "failed" || !d.Lock.Busy() {
		t.Errorf("ambiguous owner reported success: %+v", got)
	}
	b, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(b, foreign) {
		t.Fatal("foreign owner removed/rewritten", err)
	}
}

func TestReserveQueuePreBeginFailureReleasesFence(t *testing.T) {
	d := reserveQueueDeps(t)
	got := runReserveQueue(t, d, context.Background(), ReserveModeAction, ReserveOperationTarget, []Stage{{Name: "mode", Global: true, Protected: true, Run: func(*Task) error { return errors.New("stale revision") }}})
	if got.State != "failed" || d.Lock.Busy() {
		t.Fatalf("pre-begin refusal retained fence: %+v", got)
	}
}

func TestReserveQueueRetainsOrphanProbeWorkspace(t *testing.T) {
	d := reserveQueueDeps(t)
	workspace := filepath.Join(d.Root.Tmp(), "reserve-probe-uncertain")
	got := runReserveQueue(t, d, context.Background(), ReserveSwitchAction, ReserveOperationTarget, []Stage{{Name: "monitor", Global: true, Protected: true, Run: func(*Task) error {
		return os.Mkdir(workspace, 0700)
	}}})
	if got.State != "failed" || got.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" || !d.Lock.Busy() {
		t.Fatalf("uncertain probe released mutation fence: %+v", got)
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatal("orphan removed", err)
	}
}

func TestReserveQueueRetainsExplicitUncertainCommit(t *testing.T) {
	d := reserveQueueDeps(t)
	got := runReserveQueue(t, d, context.Background(), ReserveModeAction, ReserveOperationTarget, []Stage{{Name: "mode", Global: true, Protected: true, Run: func(task *Task) error {
		// Confirm can unlink its journal and then fail to sync the directory.
		task.Result("misleading earlier result", "OTHER_ERROR")
		return errors.Join(errors.New("directory sync failed"), ErrReserveTransactionPending)
	}}})
	if got.State != "failed" || got.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" {
		t.Fatalf("uncertain commit receipt: %+v", got)
	}
	owner, ok := d.Lock.Current()
	if !ok || !owner.Pending || owner.Resumable {
		t.Fatalf("uncertain commit released: %+v", owner)
	}
}

func TestReserveQueueFailedPendingWriteKeepsOriginalFence(t *testing.T) {
	d := reserveQueueDeps(t)
	path := d.Lock.file
	var before []byte
	got := runReserveQueue(t, d, context.Background(), ReserveSwitchAction, ReserveOperationTarget, []Stage{{Name: "switch", Global: true, Protected: true, Run: func(*Task) error {
		var err error
		before, err = os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(reserveQueueJournal(d), []byte("pending"), 0600); err != nil {
			return err
		}
		// A vanished parent makes persistence impossible; the saved original
		// owner must survive the failed check and ordinary Release.
		d.Lock.file = unwritableLockPath(d)
		return errors.New("installation uncertain")
	}}})
	d.Lock.file = path
	if got.State != "failed" || !d.Lock.Busy() {
		t.Fatalf("lost failed-persistence fence: %+v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("original owner changed", err)
	}
	if !NewGlobalLock(path).Busy() {
		t.Fatal("restart accepted uncertain owner")
	}
}

func TestReserveQueueRetainedFenceReconcilesBeforeNextMutation(t *testing.T) {
	d := testDeps(t)
	guard, err := platform.TryLock(d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	if err := d.Lock.ReconcileDaemonReserve(d.Root, guard, false, nil); err != nil {
		t.Fatal(err)
	}
	got := runReserveQueue(t, d, context.Background(), ReserveSwitchAction, ReserveOperationTarget, []Stage{{Name: "switch", Global: true, Protected: true, Run: func(*Task) error {
		if err := os.WriteFile(reserveQueueJournal(d), []byte("pending"), 0600); err != nil {
			return err
		}
		return errors.New("router unavailable")
	}}})
	if got.State != "failed" {
		t.Fatal(got)
	}
	path := d.RunPath("global-operation.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewGlobalLock(path)
	called := false
	if err := restarted.ReconcileDaemonReserve(d.Root, guard, true, func() error {
		called = true
		now, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(now, before) {
			return errors.New("recovery lost its owner")
		}
		return os.Remove(reserveQueueJournal(d))
	}); err != nil || !called {
		t.Fatalf("recovery: called=%v err=%v", called, err)
	}
	release, ok := restarted.TryAcquire("operations", "servers:activate", "next")
	if !ok {
		t.Fatal("recovered queue still blocked")
	}
	release()
}

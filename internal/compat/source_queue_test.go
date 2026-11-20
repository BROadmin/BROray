package compat

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

const sourceRemovalTestAction = "subscriptions:remove-source"

func TestSourceRemovalQueueReceivesOwnedFence(t *testing.T) {
	d := reserveQueueDeps(t)
	got := runReserveQueue(t, d, context.Background(), sourceRemovalTestAction, ReserveOperationTarget, []Stage{{Name: "remove", Global: true, Protected: true, Run: func(task *Task) error {
		owner, ok := d.Lock.Current()
		if !FenceHeld(task.Ctx) || !ok || !owner.DaemonOwnedReserve || owner.Action != sourceRemovalTestAction {
			return errors.New("source removal lacks protected owner")
		}
		return nil
	}}})
	if got.State != "completed" || d.Lock.Busy() {
		t.Fatalf("clean removal fence: %+v busy=%v", got, d.Lock.Busy())
	}
}
func TestSourceRemovalQueueRetainsPendingAndUncertainOutcome(t *testing.T) {
	for _, kind := range []string{"error", "panic", "cancel", "success-with-journal", "uncertain-after-unlink"} {
		t.Run(kind, func(t *testing.T) {
			d := reserveQueueDeps(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got := runReserveQueue(t, d, ctx, sourceRemovalTestAction, ReserveOperationTarget, []Stage{{Name: "remove", Global: true, Protected: true, Run: func(task *Task) error {
				if kind == "uncertain-after-unlink" {
					return ErrReserveTransactionPending
				}
				if err := os.WriteFile(reserveQueueJournal(d), []byte("unconfirmed source transition"), 0600); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "panic":
					panic("source transition interrupted")
				case "cancel":
					cancel()
					return context.Canceled
				case "success-with-journal":
					return nil
				}
				return errors.New("source replacement not verified")
			}}})
			if got.State != "failed" || got.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" {
				t.Errorf("recovery receipt: %+v", got)
			}
			owner, ok := d.Lock.Current()
			if !ok || !owner.Pending || owner.Resumable || !owner.DaemonOwnedReserve {
				t.Errorf("source fence lost: %+v %v", owner, ok)
			}
			if release, ok := d.Lock.TryAcquire("operations", "servers:activate", "other"); ok {
				release()
				t.Error("competing mutation acquired")
			}
			if !NewGlobalLock(d.RunPath("global-operation.json")).Busy() {
				t.Error("restart lost source fence")
			}
		})
	}
}
func TestSourceRemovalQueueRefusesUnsafeStage(t *testing.T) {
	for _, kind := range []string{"unmarked", "wrong-target", "cooperative", "nonglobal", "two-stages", "preexisting-journal"} {
		t.Run(kind, func(t *testing.T) {
			d := reserveQueueDeps(t)
			if kind == "unmarked" {
				d.Lock = NewGlobalLock(d.RunPath("global-operation.json"))
			}
			target := ReserveOperationTarget
			if kind == "wrong-target" {
				target = "source-id"
			}
			if kind == "preexisting-journal" {
				if err := os.WriteFile(reserveQueueJournal(d), []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ran := false
			stages := []Stage{{Name: "remove", Global: kind != "nonglobal", Protected: kind != "cooperative", Run: func(*Task) error { ran = true; return nil }}}
			if kind == "two-stages" {
				stages = append(stages, stages[0])
			}
			got := runReserveQueue(t, d, context.Background(), sourceRemovalTestAction, target, stages)
			if ran || got.State != "failed" {
				t.Fatalf("unsafe source callback: ran=%v request=%+v", ran, got)
			}
		})
	}
}
func TestSourceRemovalRecoveryKeepsOwnerUntilConfirmed(t *testing.T) {
	for _, fails := range []bool{false, true} {
		root := platform.Root{Dir: t.TempDir()}
		if err := root.EnsureDirs(); err != nil {
			t.Fatal(err)
		}
		guard, err := platform.TryLock(root.DaemonLock())
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Release()
		path := filepath.Join(root.Run(), "global-operation.json")
		now := time.Now()
		h := Holder{Scope: "operations", Action: sourceRemovalTestAction, BundleID: ReserveOperationTarget, StartedAt: now, UpdatedAt: now, Pending: true, DaemonOwnedReserve: true}
		if err := platform.WriteJSONAtomic(path, h, 0600); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		g := NewGlobalLock(path, root.InstallerLock(), root.LauncherLock())
		called := false
		err = g.ReconcileDaemonReserve(root, guard, true, func() error {
			called = true
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, before) {
				t.Fatal("owner released before source recovery")
			}
			if fails {
				return errors.New("source restore failed")
			}
			return nil
		})
		if !called || (err != nil) != fails {
			t.Fatal("wrong source recovery outcome", called, err)
		}
		if fails {
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, before) || !g.Busy() {
				t.Fatal("failed restore lost ownership", err)
			}
		} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("confirmed owner retained", err)
		}
	}
}

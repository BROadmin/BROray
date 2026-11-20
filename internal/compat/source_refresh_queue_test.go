package compat

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/platform"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sourceRefreshTestAction = "subscriptions:refresh"

func refreshQueueStages(prepare, apply func(*Task) error) []Stage {
	return []Stage{{Name: "fetch", Run: prepare}, {Name: "parse", Run: prepare}, {Name: "apply", Global: true, Protected: true, Run: apply}}
}
func TestSourceRefreshQueueStageOwnership(t *testing.T) {
	d := reserveQueueDeps(t)
	prepared := 0
	got := runReserveQueue(t, d, context.Background(), sourceRefreshTestAction, "source-a", refreshQueueStages(func(task *Task) error {
		prepared++
		if FenceHeld(task.Ctx) || d.Lock.Busy() {
			return errors.New("preparation unexpectedly fenced")
		}
		return nil
	}, func(task *Task) error {
		owner, ok := d.Lock.Current()
		if !FenceHeld(task.Ctx) || !ok || !owner.DaemonOwnedReserve || owner.Action != sourceRefreshTestAction || owner.BundleID != ReserveOperationTarget || task.Target != "source-a" {
			return errors.New("refresh apply lacks exact durable source owner")
		}
		return nil
	}))
	if prepared != 2 || got.State != "completed" || d.Lock.Busy() {
		t.Fatalf("ownership outcome prepared=%d request=%+v busy=%v", prepared, got, d.Lock.Busy())
	}
}
func TestSourceRefreshQueueRetainsUncertainApply(t *testing.T) {
	for _, kind := range []string{"error", "panic", "success-with-journal", "uncertain-after-unlink"} {
		t.Run(kind, func(t *testing.T) {
			d := reserveQueueDeps(t)
			got := runReserveQueue(t, d, context.Background(), sourceRefreshTestAction, "source-a", refreshQueueStages(func(*Task) error { return nil }, func(*Task) error {
				if kind == "uncertain-after-unlink" {
					return ErrReserveTransactionPending
				}
				if err := os.WriteFile(reserveQueueJournal(d), []byte("pending refresh transition"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "panic" {
					panic("refresh interrupted")
				}
				if kind == "success-with-journal" {
					return nil
				}
				return errors.New("unconfirmed apply")
			}))
			owner, ok := d.Lock.Current()
			if got.State != "failed" || got.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" || !ok || !owner.Pending || !owner.DaemonOwnedReserve || owner.Resumable {
				t.Fatalf("owner lost: %+v %+v", got, owner)
			}
			if release, ok := d.Lock.TryAcquire("operations", "servers:activate", "other"); ok {
				release()
				t.Fatal("competing operation admitted")
			}
			if !NewGlobalLock(d.RunPath("global-operation.json")).Busy() {
				t.Fatal("persisted ownership lost")
			}
		})
	}
}
func TestSourceRefreshQueueRejectsUnsafeShape(t *testing.T) {
	for _, kind := range []string{"empty-target", "invalid-target", "nonglobal-apply", "cooperative-apply", "protected-fetch", "global-parse", "wrong-order", "extra-stage", "unmarked"} {
		t.Run(kind, func(t *testing.T) {
			d := reserveQueueDeps(t)
			target := "source-a"
			ran := 0
			apply := false
			stages := refreshQueueStages(func(*Task) error { ran++; return nil }, func(*Task) error { apply = true; return nil })
			switch kind {
			case "empty-target":
				target = ""
			case "invalid-target":
				target = "../source"
			case "nonglobal-apply":
				stages[2].Global = false
			case "cooperative-apply":
				stages[2].Protected = false
			case "protected-fetch":
				stages[0].Protected = true
			case "global-parse":
				stages[1].Global = true
			case "wrong-order":
				stages[0], stages[1] = stages[1], stages[0]
			case "extra-stage":
				stages = append(stages, stages[2])
			case "unmarked":
				d.Lock = NewGlobalLock(d.RunPath("global-operation.json"))
			}
			got := runReserveQueue(t, d, context.Background(), sourceRefreshTestAction, target, stages)
			if got.State != "failed" || apply || (kind != "unmarked" && ran != 0) {
				t.Fatalf("unsafe shape ran=%d apply=%v request=%+v", ran, apply, got)
			}
		})
	}
}
func TestSourceRefreshRecoveryKeepsOwnerUntilConfirmed(t *testing.T) {
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
		h := Holder{Scope: "operations", Action: sourceRefreshTestAction, BundleID: ReserveOperationTarget, StartedAt: now, UpdatedAt: now, Pending: true, DaemonOwnedReserve: true}
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

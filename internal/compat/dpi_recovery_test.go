package compat

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type dpiReconciler interface {
	ReconcileDaemonDPI(platform.Root, *platform.Lock, bool, func() error) error
}

func TestDPIRecoveryOwnerClass(t *testing.T) {
	for _, action := range []string{"servers:dpi-profile", ReserveModeAction, ReserveSwitchAction, SourceRemovalAction, SourceRefreshAction, "servers:activate"} {
		for _, dpi := range []bool{false, true} {
			for _, pending := range []bool{false, true} {
				t.Run(action+"/"+map[bool]string{false: "selection", true: "dpi"}[dpi]+"/"+map[bool]string{false: "idle", true: "pending"}[pending], func(t *testing.T) {
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
					h := Holder{Scope: "operations", Action: action, BundleID: ReserveOperationTarget, StartedAt: now, UpdatedAt: now, Pending: pending, DaemonOwnedReserve: true}
					if err := platform.WriteJSONAtomic(path, h, 0600); err != nil {
						t.Fatal(err)
					}
					before, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					g := NewGlobalLock(path)
					api, ok := any(g).(dpiReconciler)
					if !ok {
						t.Fatal("distinct DPI reconciliation absent")
					}
					called := false
					callback := func() error {
						called = true
						got, e := os.ReadFile(path)
						if e != nil || !bytes.Equal(got, before) {
							t.Fatal("owner released before callback", e)
						}
						return nil
					}
					if dpi {
						err = api.ReconcileDaemonDPI(root, guard, pending, callback)
					} else {
						err = g.ReconcileDaemonReserve(root, guard, pending, callback)
					}
					matches := (action == "servers:dpi-profile") == dpi
					known := action != "servers:activate"
					if matches && known {
						if err != nil || !called {
							t.Fatal("matching recovery refused", err)
						}
						if _, err := os.Lstat(path); !os.IsNotExist(err) {
							t.Fatal("confirmed owner retained", err)
						}
					} else {
						if called {
							t.Fatal("wrong recovery callback invoked")
						}
						if pending && err == nil {
							t.Fatal("pending journal borrowed wrong owner")
						}
						got, e := os.ReadFile(path)
						if e != nil || !bytes.Equal(got, before) {
							t.Fatal("other owner changed", e)
						}
					}
				})
			}
		}
	}
}

func TestDPIQueueUsesProtectedOwner(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "pending"}[pending], func(t *testing.T) {
			d := reserveQueueDeps(t)
			called := false
			got := runReserveQueue(t, d, context.Background(), "servers:dpi-profile", ReserveOperationTarget, []Stage{{Name: "profile", Global: true, Protected: true, Run: func(task *Task) error {
				called = true
				owner, ok := d.Lock.Current()
				if !FenceHeld(task.Ctx) || !ok || !owner.DaemonOwnedReserve || owner.Action != "servers:dpi-profile" {
					t.Fatal("DPI queue lacks exact protected owner")
				}
				if pending {
					return os.WriteFile(filepath.Join(d.Root.Config(), reserve.ProfileTransactionJournalFile), []byte(`{"unfinished":true}`), 0600)
				}
				return nil
			}}})
			owner, held := d.Lock.Current()
			if !called {
				t.Fatal("DPI stage not called", got)
			}
			if pending {
				if got.State != "failed" || got.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" || !held || !owner.Pending || !owner.DaemonOwnedReserve {
					t.Fatal("DPI pending owner lost", got, owner)
				}
			} else if got.State != "completed" || held {
				t.Fatal("confirmed clean DPI queue failed", got, owner)
			}
		})
	}
}

func TestDPIRecoveryRejectsOwnershipDrift(t *testing.T) {
	for _, mode := range []string{"callback-error", "foreign-owner", "lost-daemon", "external", "source-workspace", "unmarked", "mixed-policy"} {
		t.Run(mode, func(t *testing.T) {
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
			h := Holder{Scope: "operations", Action: "servers:dpi-profile", BundleID: ReserveOperationTarget, StartedAt: now, UpdatedAt: now, DaemonOwnedReserve: true}
			if mode == "unmarked" {
				h.DaemonOwnedReserve = false
			}
			if mode == "mixed-policy" {
				h.DaemonOwnedPolicy = true
			}
			if err := platform.WriteJSONAtomic(path, h, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			g := NewGlobalLock(path, root.InstallerLock(), root.LauncherLock())
			api, ok := any(g).(dpiReconciler)
			if !ok {
				t.Fatal("DPI reconciliation absent")
			}
			if mode == "external" {
				held, err := platform.TryLock(root.InstallerLock())
				if err != nil {
					t.Fatal(err)
				}
				defer held.Release()
			}
			if mode == "source-workspace" {
				sourceSyncFixtureMarker(t, root, "marker")
			}
			called := false
			err = api.ReconcileDaemonDPI(root, guard, true, func() error {
				called = true
				switch mode {
				case "callback-error":
					return errors.New("uncertain native proof")
				case "foreign-owner":
					return os.WriteFile(path, []byte(`{"foreign":true}`), 0600)
				case "lost-daemon":
					return guard.Release()
				}
				return nil
			})
			if err == nil {
				t.Fatal("uncertain recovery accepted")
			}
			got, e := os.ReadFile(path)
			if e != nil {
				t.Fatal("owner removed", e)
			}
			if mode != "foreign-owner" && !bytes.Equal(got, before) {
				t.Fatal("owner modified")
			}
			if called && (mode == "external" || mode == "source-workspace" || mode == "unmarked" || mode == "mixed-policy") {
				t.Fatal("untrusted callback invoked")
			}
		})
	}
}

func TestDPIRecoveryWithoutVolatileOwner(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "journal"}[pending], func(t *testing.T) {
			root := platform.Root{Dir: t.TempDir()}
			if err := root.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			guard, err := platform.TryLock(root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			g := NewGlobalLock(filepath.Join(root.Run(), "global-operation.json"))
			sourceSyncFixtureMarker(t, root, "empty")
			called := false
			if err := g.ReconcileDaemonDPI(root, guard, pending, func() error { called = true; return nil }); err != nil || called != pending {
				t.Fatal("wrong absent owner recovery", called, err)
			}
			release, ok := g.TryAcquire("operations", DPIProfileAction, ReserveOperationTarget)
			if !ok {
				t.Fatal("reconciled fence unavailable")
			}
			owner, ok := g.Current()
			if !ok || !owner.DaemonOwnedReserve || owner.Action != DPIProfileAction {
				t.Fatal("new DPI owner not classified", owner)
			}
			release()
		})
	}
}

func TestDPIQueueRefusesUnsafeShape(t *testing.T) {
	for _, mode := range []string{"unprotected", "nonglobal", "wrong-target", "multiple"} {
		t.Run(mode, func(t *testing.T) {
			d := reserveQueueDeps(t)
			called := false
			stage := Stage{Name: "profile", Global: true, Protected: true, Run: func(*Task) error { called = true; return nil }}
			target := ReserveOperationTarget
			if mode == "unprotected" {
				stage.Protected = false
			}
			if mode == "nonglobal" {
				stage.Global = false
			}
			if mode == "wrong-target" {
				target = "server-a"
			}
			stages := []Stage{stage}
			if mode == "multiple" {
				stages = append(stages, stage)
			}
			got := runReserveQueue(t, d, context.Background(), DPIProfileAction, target, stages)
			if called || got.State != "failed" || d.Lock.Busy() {
				t.Fatal("unsafe DPI queue admitted", got, called)
			}
		})
	}
}

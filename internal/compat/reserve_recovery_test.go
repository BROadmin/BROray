package compat

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

func TestReserveDaemonRecoveryRetainsFenceThroughCallback(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "pending"}[pending], func(t *testing.T) {
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
			h := Holder{Scope: "operations", Action: ReserveModeAction, BundleID: ReserveOperationTarget, StartedAt: now, UpdatedAt: now, Pending: pending, DaemonOwnedReserve: true}
			if err := platform.WriteJSONAtomic(path, h, 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			g := NewGlobalLock(path, root.InstallerLock(), root.LauncherLock())
			called := false
			if err := g.ReconcileDaemonReserve(root, guard, true, func() error {
				called = true
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, before) {
					t.Fatal("fence released before recovery")
				}
				return nil
			}); err != nil || !called {
				t.Fatal("owned recovery", err)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("recovered fence retained", err)
			}
			release, ok := g.TryAcquire("operations", ReserveSwitchAction, ReserveOperationTarget)
			if !ok {
				t.Fatal("recovered fence unavailable")
			}
			owner, ok := g.Current()
			if !ok || !owner.DaemonOwnedReserve || owner.DaemonOwnedPolicy || owner.DaemonOwnedXray {
				t.Fatal("new reserve owner not marked", owner)
			}
			release()
		})
	}
}

func TestReserveDaemonRecoveryRejectsUntrustedOwnership(t *testing.T) {
	for _, kind := range []string{"unmarked", "wrong-action", "mixed-policy", "mixed-xray", "resumable", "callback-error", "owner-replaced", "guard-lost", "external", "missing-callback"} {
		t.Run(kind, func(t *testing.T) {
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
			h := Holder{Scope: "operations", Action: ReserveSwitchAction, BundleID: ReserveOperationTarget, StartedAt: now, UpdatedAt: now, Pending: true, DaemonOwnedReserve: true}
			switch kind {
			case "unmarked":
				h.DaemonOwnedReserve = false
			case "wrong-action":
				h.Action = "servers:activate"
			case "mixed-policy":
				h.DaemonOwnedPolicy = true
			case "mixed-xray":
				h.DaemonOwnedXray = true
			case "resumable":
				h.Resumable = true
			}
			if err := platform.WriteJSONAtomic(path, h, 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			g := NewGlobalLock(path, root.InstallerLock(), root.LauncherLock())
			if kind == "external" {
				lock, err := platform.TryLock(root.InstallerLock())
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Release()
			}
			called := false
			callback := func() error {
				called = true
				switch kind {
				case "callback-error":
					return errors.New("uncertain restore")
				case "owner-replaced":
					return os.WriteFile(path, []byte(`{"foreign":true}`), 0600)
				case "guard-lost":
					guard.Release()
				}
				return nil
			}
			if kind == "missing-callback" {
				callback = nil
			}
			if err := g.ReconcileDaemonReserve(root, guard, true, callback); err == nil {
				t.Fatal("untrusted recovery accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("fence removed", err)
			}
			if kind != "owner-replaced" && !bytes.Equal(got, before) {
				t.Fatal("owner altered")
			}
			if called && (kind == "unmarked" || kind == "wrong-action" || kind == "mixed-policy" || kind == "mixed-xray" || kind == "resumable" || kind == "external") {
				t.Fatal("untrusted callback invoked")
			}
		})
	}
}

func TestReserveDaemonRecoveryLeavesOtherOwnersAndHandlesAbsentRecord(t *testing.T) {
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
	g := NewGlobalLock(path)
	called := false
	if err := g.ReconcileDaemonReserve(root, guard, true, func() error { called = true; return nil }); err != nil || !called {
		t.Fatal("journal without volatile record", err)
	}
	now := time.Now()
	h := Holder{Scope: "routes", Action: "routes:policy", BundleID: "policy", StartedAt: now, UpdatedAt: now, DaemonOwnedPolicy: true}
	if err := platform.WriteJSONAtomic(path, h, 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	g = NewGlobalLock(path)
	called = false
	if err := g.ReconcileDaemonReserve(root, guard, false, func() error { called = true; return nil }); err != nil || called {
		t.Fatal("other owner intercepted", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatal("policy owner changed")
	}
}

package compat

import (
	"github.com/BROadmin/broray/internal/platform"
	"os"
	"testing"
	"time"
)

func TestDaemonXrayRecoveryPreservesOtherOwners(t *testing.T) {
	for _, tc := range []struct {
		name, scope, action       string
		marked, external, recover bool
	}{
		{"owned", "xray", "xray:install", true, false, true},
		{"legacy", "xray", "xray:install", false, false, false},
		{"foreign", "routes", "apply", true, false, false},
		{"unknown-action", "xray", "other", true, false, false},
		{"external-live", "xray", "xray:install", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDeps(t)
			path := d.RunPath("global-operation.json")
			now := time.Now().UTC()
			if err := platform.WriteJSONAtomic(path, Holder{Scope: tc.scope, Action: tc.action, BundleID: "xray", StartedAt: now, UpdatedAt: now, DaemonOwnedXray: tc.marked}, 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			guard, err := platform.TryLock(d.Root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			if tc.external {
				l, err := platform.TryLock(d.Root.InstallerLock())
				if err != nil {
					t.Fatal(err)
				}
				defer l.Release()
			}
			g := NewGlobalLock(path, d.Root.InstallerLock())
			err = g.ReconcileDaemonXray(d.Root, guard)
			if (err == nil) != tc.recover {
				t.Fatalf("recovery=%v error=%v", tc.recover, err)
			}
			if !tc.recover {
				after, _ := os.ReadFile(path)
				if string(after) != string(before) {
					t.Fatal("uncertain record changed")
				}
			}
		})
	}
}

func TestDaemonXrayRecoveryRequiresOwnKernelLock(t *testing.T) {
	d := testDeps(t)
	g := d.Lock
	if err := g.ReconcileDaemonXray(d.Root, nil); err == nil {
		t.Fatal("recovery accepted without daemon kernel lock")
	}
	guard, err := platform.TryLock(d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	if err := g.ReconcileDaemonXray(d.Root, guard); err != nil {
		t.Fatal(err)
	}
	rel, ok := g.TryAcquire("xray", "xray:install", "xray")
	if !ok {
		t.Fatal("acquire")
	}
	defer rel()
	h, ok := g.Current()
	if !ok || !h.DaemonOwnedXray {
		t.Fatal("new Xray worker lacks lifetime binding")
	}
	guard.Release()
	if err := g.ReconcileDaemonXray(d.Root, guard); err == nil {
		t.Fatal("released descriptor accepted")
	}
}

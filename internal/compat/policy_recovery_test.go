package compat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/routepolicy"
)

func TestDaemonPolicyRecoveryOwnerContract(t *testing.T) {
	for _, tc := range []struct {
		name, action                                          string
		marked, pending, journal, external, success, callback bool
	}{
		{"owned-active", "routes:policy", true, false, true, false, true, true},
		{"owned-pending", "routes:geodata", true, true, true, false, true, true},
		{"completed-before-release", "routes:policy", true, false, false, false, true, true},
		{"legacy", "routes:policy", false, false, true, false, false, false},
		{"unknown", "other", true, false, true, false, false, false},
		{"external", "routes:policy", true, false, true, true, false, false},
		{"unrelated-pending-no-journal", "other", false, true, false, false, true, false},
		{"unrelated-pending-with-journal", "other", false, true, true, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDeps(t)
			now := time.Now().UTC()
			path := d.RunPath("global-operation.json")
			h := Holder{Scope: "routes", Action: tc.action, BundleID: "policy", Pending: tc.pending, StartedAt: now, UpdatedAt: now, DaemonOwnedPolicy: tc.marked}
			if err := platform.WriteJSONAtomic(path, h, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			guard, err := platform.TryLock(d.Root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			if tc.external {
				lock, err := platform.TryLock(d.Root.InstallerLock())
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Release()
			}
			g := NewGlobalLock(path, d.Root.InstallerLock())
			called := false
			err = g.ReconcileDaemonPolicy(d.Root, guard, tc.journal, func() error { called = true; return nil })
			if (err == nil) != tc.success || called != tc.callback {
				t.Fatalf("error=%v callback=%v", err, called)
			}
			if !tc.success || !tc.callback {
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(before) {
					t.Fatal("unrelated owner changed")
				}
			}
			if tc.success && tc.callback && g.Busy() {
				t.Fatal("completed recovery did not release fence")
			}
		})
	}
}

func TestDaemonPolicyRecoveryRequiresProofAndRetainsFailures(t *testing.T) {
	d := testDeps(t)
	g := d.Lock
	if err := g.ReconcileDaemonPolicy(d.Root, nil, true, func() error { t.Fatal("unguarded recovery ran"); return nil }); err == nil {
		t.Fatal("nil guard accepted")
	}
	guard, err := platform.TryLock(d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	if err := g.ReconcileDaemonPolicy(d.Root, guard, false, nil); err != nil {
		t.Fatal(err)
	}
	release, ok := g.TryAcquire("routes", "routes:policy", "policy")
	if !ok {
		t.Fatal("acquire")
	}
	defer release()
	h, ok := g.Current()
	if !ok || !h.DaemonOwnedPolicy {
		t.Fatal("new policy operation lacks daemon lifetime marker")
	}
	path := d.RunPath("global-operation.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewGlobalLock(path)
	if err := restarted.ReconcileDaemonPolicy(d.Root, guard, true, func() error { return errors.New("rollback unconfirmed") }); err == nil {
		t.Fatal("failed recovery accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("failed recovery cleared owner")
	}
}

func TestDaemonPolicyRecoveryWithRealJournal(t *testing.T) {
	d := testDeps(t)
	ctx := context.Background()
	guard, err := platform.TryLock(d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	old := []byte(`{"old":true}`)
	next := []byte(`{"new":true}`)
	if err := os.WriteFile(d.Root.XrayConfig(), old, 0600); err != nil {
		t.Fatal(err)
	}
	j, err := routepolicy.OpenJournal(d.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	state := routepolicy.State{SchemaVersion: 1, Rules: []routepolicy.Rule{}}
	if err := j.Begin(ctx, state, next, ""); err != nil {
		t.Fatal(err)
	}
	if err := platform.WriteFileAtomicNoFollow(d.Root.XrayConfig(), next, 0600); err != nil {
		t.Fatal(err)
	}
	if err := j.InstallState(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	path := d.RunPath("global-operation.json")
	if err := platform.WriteJSONAtomic(path, Holder{Scope: "routes", Action: "routes:policy", BundleID: "policy", StartedAt: now, UpdatedAt: now, DaemonOwnedPolicy: true}, 0600); err != nil {
		t.Fatal(err)
	}
	g := NewGlobalLock(path, d.Root.InstallerLock(), d.Root.LauncherLock())
	if err := g.ReconcileDaemonPolicy(d.Root, guard, true, func() error { return routepolicy.RecoverPolicy(ctx, d.Root, guard) }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(d.Root.XrayConfig())
	if err != nil || string(got) != string(old) {
		t.Fatal("actual journal did not restore config")
	}
	if _, err := os.Lstat(filepath.Join(d.Root.Routes(), routepolicy.JournalFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal not cleared")
	}
	if g.Busy() || g.Ambiguous() {
		t.Fatal("fence still blocks startup")
	}
}

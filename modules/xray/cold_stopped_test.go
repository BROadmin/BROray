package xray

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
)

func TestColdInstallPreservesIntentAndExcludesStart(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	path := profileBootFixture(t, p)
	guard, err := platform.TryLock(p.root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	ctx := context.Background()
	lease, err := reserve.OpenStoppedProfile(ctx, p.root, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	before := p.RuntimeSnapshot()
	if !before.ColdStopped() {
		t.Fatal("not cold")
	}
	for _, start := range []func(context.Context) error{p.Start, p.StartAtBoot} {
		done := make(chan error, 1)
		go func() { done <- start(ctx) }()
		select {
		case err := <-done:
			if err == nil || p.RuntimeSnapshot() != before {
				t.Fatal("competing start changed intent", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("competing start blocked")
		}
	}
	called := false
	if err := p.InstallIfCold(ctx, before, lease, func() error { called = true; return nil }); err != nil || !called {
		t.Fatal("cold install", err)
	}
	if p.RuntimeSnapshot() != before {
		t.Fatal("manufactured generation or intent")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("hold consumed", err)
	}
	if err := p.Start(ctx); err != nil {
		t.Fatal("explicit start not released", err)
	}
}

func TestColdInstallRejectsStaleOrForeignAuthority(t *testing.T) {
	for _, mode := range []string{"foreign-snapshot", "nil-lease", "closed", "manual-stop", "cancelled", "hold-drift", "callback-error"} {
		t.Run(mode, func(t *testing.T) {
			p := testProcess(t, "exec sleep 60")
			path := profileBootFixture(t, p)
			guard, err := platform.TryLock(p.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lease, err := reserve.OpenStoppedProfile(ctx, p.root, guard)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			expected := p.RuntimeSnapshot()
			var stopDone chan error
			sentinel := errors.New("install failed")
			switch mode {
			case "foreign-snapshot":
				expected = NewProcess(p.root).RuntimeSnapshot()
			case "nil-lease":
				lease = nil
			case "closed":
				lease.Close()
			case "manual-stop":
				stopDone = make(chan error, 1)
				go func() { stopDone <- p.Stop(ctx) }()
				deadline := time.Now().Add(2 * time.Second)
				for p.RuntimeSnapshot().Intent == expected.Intent {
					if time.Now().After(deadline) {
						t.Fatal("manual stop intent not observed")
					}
					time.Sleep(time.Millisecond)
				}
			case "cancelled":
				cancel()
			case "hold-drift":
				if err := os.WriteFile(path, []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := p.RuntimeSnapshot()
			called := false
			err = p.InstallIfCold(ctx, expected, lease, func() error { called = true; return sentinel })
			if err == nil || called != (mode == "callback-error") || p.RuntimeSnapshot() != before {
				t.Fatal("invalid cold install accepted", mode, err, called)
			}
			if stopDone != nil {
				if err := lease.Close(); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-stopDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("manual stop failed to confirm after release")
				}
			}
			if mode == "callback-error" && !errors.Is(err, sentinel) {
				t.Fatal("callback error lost", err)
			}
		})
	}
}

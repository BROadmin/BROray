package xray

import (
	"context"
	"errors"
	"syscall"
	"testing"
)

func TestGuardedReplaceObservedUnexpectedExit(t *testing.T) {
	for _, kind := range []string{"unexpected", "manual-stop", "new-generation", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			p := testProcess(t, "exec sleep 60")
			stopObservedProcess(t, p)
			if err := p.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			if kind == "manual-stop" {
				if err := p.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				p.mu.Lock()
				p.signalLocked(p.cur, syscall.SIGKILL)
				p.mu.Unlock()
			}
			observed := waitObservedExit(t, p)
			if kind == "foreign" {
				observed.owner = NewProcess(p.root)
			}
			if kind == "new-generation" {
				p.mu.Lock()
				err := p.launchLocked()
				p.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			before := p.RuntimeSnapshot()
			called := false
			after, err := p.ReplaceIfCurrent(context.Background(), observed, func() error { called = true; return nil })
			if kind != "unexpected" {
				if !errors.Is(err, ErrRuntimeChanged) || called || p.RuntimeSnapshot() != before {
					t.Fatal("invalid resume authority mutated process", err)
				}
				return
			}
			if err != nil || !called || !after.Wanted || !after.Running || after.Generation != observed.Generation+1 || after.Intent != observed.Intent+1 {
				t.Fatal("owned unexpected exit not replaced", after, err)
			}
			if _, err := p.ReplaceIfCurrent(context.Background(), observed, func() error { t.Error("replayed resume installed"); return nil }); !errors.Is(err, ErrRuntimeChanged) || p.RuntimeSnapshot() != after {
				t.Fatal("old exit replay changed process", err)
			}
		})
	}
}

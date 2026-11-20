package xray

import (
	"context"
	"errors"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestGuardedChangeWaitsForChildGuard(t *testing.T) {
	for _, restart := range []bool{false, true} {
		for _, cancelWait := range []bool{false, true} {
			name := "stop"
			if restart {
				name = "replace"
			}
			if cancelWait {
				name += "-cancelled"
			}
			t.Run(name, func(t *testing.T) {
				p := testProcess(t, "exec sleep 60")
				stopObservedProcess(t, p)
				if err := p.Start(context.Background()); err != nil {
					t.Fatal(err)
				}
				before := p.RuntimeSnapshot()
				p.mu.Lock()
				p.signalLocked(p.cur, syscall.SIGKILL)
				p.mu.Unlock()
				if !waitObservedExit(t, p).ConfirmsUnexpectedExit(before) {
					t.Fatal("fixture has no confirmed owned exit")
				}
				// Deterministically retain the same ownership lock after leader
				// reap, as an inherited descriptor in a terminating descendant can.
				guard, err := p.acquireChildGuard()
				if err != nil {
					t.Fatal(err)
				}
				defer guard.Close()
				var called atomic.Bool
				install := func() error { called.Store(true); return nil }
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				type outcome struct {
					snapshot RuntimeSnapshot
					err      error
				}
				done := make(chan outcome, 1)
				go func() {
					snapshot, err := p.changeIfCurrent(ctx, before, install, restart)
					done <- outcome{snapshot, err}
				}()
				deadline := time.Now().Add(time.Second)
				for p.RuntimeSnapshot().Intent == before.Intent && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if p.RuntimeSnapshot().Intent == before.Intent {
					t.Fatal("guarded claim did not start")
				}
				if called.Load() {
					t.Error("configuration changed while child ownership lock remained held")
				}
				if cancelWait {
					cancel()
				} else if err := guard.Close(); err != nil {
					t.Fatal(err)
				}
				select {
				case result := <-done:
					if cancelWait {
						if !errors.Is(result.err, context.Canceled) || called.Load() || p.RuntimeSnapshot().Running {
							t.Fatal("cancelled ownership wait installed or restarted", result.err, called.Load())
						}
					} else if result.err != nil || !called.Load() || result.snapshot.Running != restart {
						t.Fatal("released ownership did not complete guarded change", result.err, called.Load())
					}
				case <-time.After(2 * time.Second):
					t.Fatal("guarded change did not finish")
				}
			})
		}
	}
}

package xray

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type guardedStopper interface {
	StopIfCurrent(context.Context, RuntimeSnapshot, func() error) (RuntimeSnapshot, error)
}

func requireGuardedStop(t *testing.T, p *Process) guardedStopper {
	t.Helper()
	stop, ok := any(p).(guardedStopper)
	if !ok {
		t.Fatal("SOURCE_STOP_GUARD_ABSENT: exact-snapshot disconnect is unavailable")
	}
	return stop
}
func TestGuardedStopReapsThenDisconnectsWithoutRestart(t *testing.T) {
	for _, state := range []string{"running", "manual-stop", "unexpected-exit", "observed-exit"} {
		t.Run(state, func(t *testing.T) {
			p := testProcess(t, "exec sleep 60")
			stopObservedProcess(t, p)
			stop := requireGuardedStop(t, p)
			if err := p.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			expected := p.RuntimeSnapshot()
			p.mu.Lock()
			old := p.cur
			p.mu.Unlock()
			if state == "manual-stop" {
				if err := p.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				expected = waitObservedExit(t, p)
			} else if state != "running" {
				p.mu.Lock()
				p.signalLocked(p.cur, syscall.SIGKILL)
				p.mu.Unlock()
				exited := waitObservedExit(t, p)
				if !exited.ConfirmsUnexpectedExit(expected) {
					t.Fatal("fixture lacks owned exit")
				}
				if state == "observed-exit" {
					expected = exited
				}
			}
			called := 0
			after, err := stop.StopIfCurrent(context.Background(), expected, func() error {
				select {
				case <-old.done:
				default:
					t.Error("disconnect before owned child reap")
				}
				if p.cur != nil || p.wanted || !p.exitConfirmed {
					t.Error("disconnect before stopped bookkeeping")
				}
				called++
				return nil
			})
			if err != nil || called != 1 || after.Wanted || after.Running || !after.ExitConfirmed || after.PID != 0 || after.LastPID != expected.LastPID || after.Generation != expected.Generation || after.Intent != expected.Intent+1 || after != p.RuntimeSnapshot() {
				t.Fatalf("disconnect: calls=%d before=%+v after=%+v err=%v", called, expected, after, err)
			}
			if _, err := stop.StopIfCurrent(context.Background(), expected, func() error { t.Error("replayed disconnect"); return nil }); !errors.Is(err, ErrRuntimeChanged) || p.RuntimeSnapshot() != after {
				t.Fatal("stale replay changed runtime", err)
			}
		})
	}
}
func TestGuardedStopRejectsInvalidAuthorityWithoutMutation(t *testing.T) {
	for _, state := range []string{"running", "stopped", "unknown"} {
		t.Run(state, func(t *testing.T) {
			p := testProcess(t, "exec sleep 60")
			stopObservedProcess(t, p)
			stop := requireGuardedStop(t, p)
			if state != "unknown" {
				if err := p.Start(context.Background()); err != nil {
					t.Fatal(err)
				}
				if state == "stopped" {
					if err := p.Stop(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := p.RuntimeSnapshot()
			for _, kind := range []string{"foreign", "serialized", "generation", "intent", "exhausted", "unconfirmed", "cancelled", "nil", "unknown"} {
				if kind == "unknown" && state != "unknown" {
					continue
				}
				t.Run(kind, func(t *testing.T) {
					expected := before
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					install := func() error { t.Error("invalid authority called disconnect"); return nil }
					switch kind {
					case "foreign":
						expected.owner = NewProcess(p.root)
					case "serialized":
						raw, err := json.Marshal(before)
						if err != nil {
							t.Fatal(err)
						}
						expected = RuntimeSnapshot{}
						if err := json.Unmarshal(raw, &expected); err != nil {
							t.Fatal(err)
						}
					case "generation":
						expected.Generation++
					case "intent":
						expected.Intent++
					case "exhausted":
						expected.Intent = ^uint64(0)
					case "unconfirmed":
						expected.ExitConfirmed = !expected.ExitConfirmed
					case "cancelled":
						cancel()
					case "nil":
						install = nil
					}
					if _, err := stop.StopIfCurrent(ctx, expected, install); err == nil || p.RuntimeSnapshot() != before {
						t.Fatal("invalid authority mutated process", err)
					}
				})
			}
		})
	}
}
func TestGuardedStopRejectsNewGeneration(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	stop := requireGuardedStop(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	p.mu.Lock()
	p.signalLocked(p.cur, syscall.SIGKILL)
	p.mu.Unlock()
	waitObservedExit(t, p)
	p.mu.Lock()
	err := p.launchLocked()
	p.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	current := p.RuntimeSnapshot()
	if _, err := stop.StopIfCurrent(context.Background(), before, func() error { t.Error("new generation disconnected"); return nil }); !errors.Is(err, ErrRuntimeChanged) || p.RuntimeSnapshot() != current {
		t.Fatal("new child not preserved", err)
	}
}
func TestGuardedStopInstallErrorAndCancellationStayStopped(t *testing.T) {
	for _, kind := range []string{"failure", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			p := testProcess(t, "exec sleep 60")
			stopObservedProcess(t, p)
			stop := requireGuardedStop(t, p)
			if err := p.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := p.RuntimeSnapshot()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("fixture disconnect failure")
			_, err := stop.StopIfCurrent(ctx, before, func() error {
				if kind == "cancel" {
					cancel()
					return nil
				}
				return failure
			})
			want := failure
			if kind == "cancel" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatal("lost callback error", err)
			}
			after := p.RuntimeSnapshot()
			if after.Running || after.Wanted || !after.ExitConfirmed || after.Generation != before.Generation || after.Intent != before.Intent+1 {
				t.Fatal("failure resumed child", after)
			}
		})
	}
}
func TestGuardedStopCompetingClaimsInstallOnce(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	stop := requireGuardedStop(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	start := make(chan struct{})
	done := make(chan error, 2)
	var calls atomic.Int32
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := stop.StopIfCurrent(context.Background(), before, func() error { calls.Add(1); return nil })
			done <- err
		}()
	}
	close(start)
	success, refused := 0, 0
	for i := 0; i < 2; i++ {
		err := <-done
		if err == nil {
			success++
		} else if errors.Is(err, ErrRuntimeChanged) {
			refused++
		} else {
			t.Fatal(err)
		}
	}
	if after := p.RuntimeSnapshot(); success != 1 || refused != 1 || calls.Load() != 1 || after.Running || after.Wanted || after.Generation != before.Generation {
		t.Fatal("claims not serialized", success, refused, calls.Load(), after)
	}
}
func TestGuardedStopManualIntentDuringReapWins(t *testing.T) {
	dir := t.TempDir()
	ready, term, release := filepath.Join(dir, "ready"), filepath.Join(dir, "term"), filepath.Join(dir, "release")
	p := testProcess(t, "trap \"touch '"+term+"'; while [ ! -f '"+release+"' ]; do sleep 1; done; exit 0\" TERM; touch '"+ready+"'; while :; do sleep 1; done")
	stopObservedProcess(t, p)
	stop := requireGuardedStop(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitGuardedMarker(t, ready)
	before := p.RuntimeSnapshot()
	done := make(chan error, 1)
	go func() {
		_, err := stop.StopIfCurrent(context.Background(), before, func() error { t.Error("superseded claim disconnected"); return nil })
		done <- err
	}()
	waitGuardedMarker(t, term)
	stopped := make(chan error, 1)
	go func() { stopped <- p.Stop(context.Background()) }()
	deadline := time.Now().Add(3 * time.Second)
	for p.RuntimeSnapshot().Intent == before.Intent+1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.RuntimeSnapshot().Intent != before.Intent+2 {
		t.Fatal("manual intent not observed")
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrRuntimeChanged) {
		t.Fatal("superseded claim accepted", err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if after := p.RuntimeSnapshot(); after.Running || after.Wanted {
		t.Fatal("manual stop undone", after)
	}
}
func TestGuardedStopEscalatesOnlyOwnedChild(t *testing.T) {
	oldTimeouts := StopTimeouts
	StopTimeouts = [2]time.Duration{100 * time.Millisecond, time.Second}
	defer func() { StopTimeouts = oldTimeouts }()
	ready := filepath.Join(t.TempDir(), "ready")
	p := testProcess(t, "trap '' TERM; touch '"+ready+"'; while :; do sleep 1; done")
	stopObservedProcess(t, p)
	stop := requireGuardedStop(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitGuardedMarker(t, ready)
	before := p.RuntimeSnapshot()
	after, err := stop.StopIfCurrent(context.Background(), before, func() error { return nil })
	if err != nil || after.Running || after.Wanted || !after.ExitConfirmed || after.Generation != before.Generation {
		t.Fatal("owned escalation failed", after, err)
	}
}

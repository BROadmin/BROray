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

func TestGuardedReplaceStopsBeforeInstallAndLaunchesOnce(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	p.mu.Lock()
	old := p.cur
	p.mu.Unlock()
	called := 0
	after, err := p.ReplaceIfCurrent(context.Background(), before, func() error {
		select {
		case <-old.done:
		default:
			t.Error("installation before owned child reap")
		}
		called++
		return nil
	})
	if err != nil || called != 1 || !after.Running || !after.Wanted || after.Generation != before.Generation+1 || after.Intent != before.Intent+1 || after == before || after != p.RuntimeSnapshot() {
		t.Fatalf("replacement: calls=%d before=%+v after=%+v err=%v", called, before, after, err)
	}
	if _, err := p.ReplaceIfCurrent(context.Background(), before, func() error { t.Error("replayed install"); return nil }); !errors.Is(err, ErrRuntimeChanged) || p.RuntimeSnapshot() != after {
		t.Fatal("stale replay changed replacement", err)
	}
}

func TestGuardedReplaceRejectsInvalidAuthorityWithoutMutation(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	for _, kind := range []string{"foreign", "serialized", "generation", "intent", "exhausted", "stopped", "cancelled", "nil"} {
		t.Run(kind, func(t *testing.T) {
			expected := before
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			install := func() error { t.Error("invalid authority called install"); return nil }
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
			case "stopped":
				expected.Wanted = false
			case "cancelled":
				cancel()
			case "nil":
				install = nil
			}
			if _, err := p.ReplaceIfCurrent(ctx, expected, install); err == nil || p.RuntimeSnapshot() != before {
				t.Fatal("invalid authority mutated process", err)
			}
		})
	}
}

func TestGuardedReplaceAcceptsOwnedExitRejectsNewGeneration(t *testing.T) {
	for _, newer := range []bool{false, true} {
		p := testProcess(t, "exec sleep 60")
		stopObservedProcess(t, p)
		if err := p.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		before := p.RuntimeSnapshot()
		p.mu.Lock()
		p.signalLocked(p.cur, syscall.SIGKILL)
		p.mu.Unlock()
		exited := waitObservedExit(t, p)
		if !exited.ConfirmsUnexpectedExit(before) {
			t.Fatal("fixture lacks owned exit")
		}
		if newer {
			// Same desired intent, genuinely new child, as in the automatic restart.
			p.mu.Lock()
			err := p.launchLocked()
			p.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
		}
		current := p.RuntimeSnapshot()
		called := false
		after, err := p.ReplaceIfCurrent(context.Background(), before, func() error { called = true; return nil })
		if newer {
			if !errors.Is(err, ErrRuntimeChanged) || called || p.RuntimeSnapshot() != current {
				t.Fatal("new automatic generation replaced", err)
			}
		} else if err != nil || !called || !after.Running {
			t.Fatal("owned exit not replaced", err)
		}
	}
}

func TestGuardedReplaceFailedInstallAndCancellationStayStopped(t *testing.T) {
	for _, kind := range []string{"failure", "cancel"} {
		p := testProcess(t, "exec sleep 60")
		stopObservedProcess(t, p)
		if err := p.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		failure := errors.New("fixture install failed")
		_, err := p.ReplaceIfCurrent(ctx, p.RuntimeSnapshot(), func() error {
			if kind == "cancel" {
				cancel()
				return nil
			}
			return failure
		})
		cancel()
		if kind == "failure" && !errors.Is(err, failure) {
			t.Fatal("lost install error", err)
		}
		if kind == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatal("lost cancellation", err)
		}
		if after := p.RuntimeSnapshot(); after.Wanted || after.Running {
			t.Fatal("unconfirmed install resumed", after)
		}
	}
}

func waitGuardedMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fixture marker not observed: ", path)
}

func TestGuardedReplaceManualStopDuringReapWins(t *testing.T) {
	dir := t.TempDir()
	ready, term, release := filepath.Join(dir, "ready"), filepath.Join(dir, "term"), filepath.Join(dir, "release")
	p := testProcess(t, "trap \"touch '"+term+"'; while [ ! -f '"+release+"' ]; do sleep 1; done; exit 0\" TERM; touch '"+ready+"'; while :; do sleep 1; done")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitGuardedMarker(t, ready)
	before := p.RuntimeSnapshot()
	done := make(chan error, 1)
	go func() {
		_, err := p.ReplaceIfCurrent(context.Background(), before, func() error { t.Error("superseded claim installed"); return nil })
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
		t.Fatal("manual stop did not invalidate claim")
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
	if after := p.RuntimeSnapshot(); after.Wanted || after.Running {
		t.Fatal("manual stop undone", after)
	}
}

func TestGuardedReplaceConcurrentClaimsInstallOnce(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	start := make(chan struct{})
	done := make(chan error, 2)
	var installs atomic.Int32
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := p.ReplaceIfCurrent(context.Background(), before, func() error { installs.Add(1); return nil })
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
	if after := p.RuntimeSnapshot(); success != 1 || refused != 1 || installs.Load() != 1 || !after.Running || after.Generation != before.Generation+1 {
		t.Fatal("competing claims not serialized", success, refused, installs.Load(), after)
	}
}

func TestGuardedReplaceEscalatesOnlyOwnedChild(t *testing.T) {
	oldTimeouts := StopTimeouts
	StopTimeouts = [2]time.Duration{100 * time.Millisecond, time.Second}
	defer func() { StopTimeouts = oldTimeouts }()
	ready := filepath.Join(t.TempDir(), "ready")
	p := testProcess(t, "trap '' TERM; touch '"+ready+"'; while :; do sleep 1; done")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitGuardedMarker(t, ready)
	before := p.RuntimeSnapshot()
	after, err := p.ReplaceIfCurrent(context.Background(), before, func() error { return nil })
	if err != nil || !after.Running || after.Generation != before.Generation+1 {
		t.Fatal("owned escalation failed", after, err)
	}
}

func TestGuardedReplaceLaunchFailureDoesNotScheduleRetry(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := p.ReplaceIfCurrent(context.Background(), p.RuntimeSnapshot(), func() error {
		// install already holds mu; force a deterministic launch failure.
		p.Binary = "/missing/guarded-replacement-core"
		return nil
	})
	if err == nil {
		t.Fatal("missing replacement launched")
	}
	if after := p.RuntimeSnapshot(); after.Wanted || after.Running {
		t.Fatal("failed launch can restart later", after)
	}
}

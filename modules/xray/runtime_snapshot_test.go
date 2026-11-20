package xray

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func stopObservedProcess(t *testing.T, p *Process) {
	t.Helper()
	t.Cleanup(func() {
		if err := p.Stop(context.Background()); err != nil {
			t.Error("owned fixture cleanup", err)
		}
	})
}

func waitObservedExit(t *testing.T, p *Process) RuntimeSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := p.RuntimeSnapshot()
		if s.ExitConfirmed && !s.Running {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("owned exit was not confirmed")
	return RuntimeSnapshot{}
}

func TestRuntimeSnapshotStableAndNoVersionIO(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	fresh := p.RuntimeSnapshot()
	if fresh.Wanted || fresh.Running || fresh.Generation != 0 || fresh.ConfirmsUnexpectedExit(fresh) {
		t.Fatal("fresh supervisor claimed running/outage")
	}
	marker := filepath.Join(t.TempDir(), "version-called")
	if err := os.WriteFile(p.root.XrayBinary(), []byte("#!/bin/sh\nprintf called > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := p.RuntimeSnapshot()
	if !s.Wanted || !s.Running || s.PID <= 0 || s.LastPID != s.PID || s.Generation == 0 || s.Intent == 0 || s.ExitConfirmed {
		t.Fatal("incomplete live observation", s)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if got := p.RuntimeSnapshot(); got != s {
			t.Fatal("read/idempotent start changed observation", got, s)
		}
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatal("snapshot invoked Version")
	}
}

func TestRuntimeSnapshotConfirmsOnlyOwnedUnexpectedExit(t *testing.T) {
	for _, kind := range []string{"normal", "signal"} {
		t.Run(kind, func(t *testing.T) {
			trigger := filepath.Join(t.TempDir(), "exit")
			script := "while [ ! -f '" + trigger + "' ]; do sleep 1; done; exit 7"
			if kind == "signal" {
				script = "exec sleep 60"
			}
			p := testProcess(t, script)
			stopObservedProcess(t, p)
			if err := p.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := p.RuntimeSnapshot()
			if !before.Running {
				t.Fatal("child not observed running")
			}
			if kind == "signal" {
				p.mu.Lock()
				p.signalLocked(p.cur, syscall.SIGKILL)
				p.mu.Unlock()
			} else {
				if err := os.WriteFile(trigger, []byte("exit"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			after := waitObservedExit(t, p)
			if !after.ConfirmsUnexpectedExit(before) || after.PID != 0 || after.LastPID != before.PID || !after.Wanted {
				t.Fatal("real owned exit not proven", before, after)
			}
			other := testProcess(t, "exec sleep 60").RuntimeSnapshot()
			other.Generation, other.Intent, other.PID, other.LastPID, other.Running, other.Wanted = before.Generation, before.Intent, before.PID, before.LastPID, true, true
			if after.ConfirmsUnexpectedExit(other) {
				t.Fatal("other supervisor accepted")
			}
			data, err := json.Marshal(before)
			if err != nil {
				t.Fatal(err)
			}
			var restored RuntimeSnapshot
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			if after.ConfirmsUnexpectedExit(restored) {
				t.Fatal("serialized observation accepted")
			}
			for _, field := range []string{"generation", "intent"} {
				bad := before
				if field == "generation" {
					bad.Generation = ^uint64(0)
				} else {
					bad.Intent = ^uint64(0)
				}
				if after.ConfirmsUnexpectedExit(bad) {
					t.Fatal("exhausted observation accepted")
				}
			}
		})
	}
}

func TestRuntimeSnapshotManualStopAndFailedRestartInvalidate(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := waitObservedExit(t, p)
	if after.Wanted || after.Intent == before.Intent || after.ConfirmsUnexpectedExit(before) {
		t.Fatal("deliberate stop treated as outage", after)
	}
	p.Binary = "/missing/broray-test-core"
	if err := p.Start(context.Background()); err == nil {
		t.Fatal("fixture unexpectedly started")
	}
	after = p.RuntimeSnapshot()
	if !after.Wanted || after.Generation != before.Generation || after.ConfirmsUnexpectedExit(before) {
		t.Fatal("failed explicit restart reused old outage proof", after)
	}
}

func TestRuntimeSnapshotNewChildInvalidatesGeneration(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = waitObservedExit(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := p.RuntimeSnapshot()
	if !next.Running || next.Generation <= before.Generation || next.Intent == before.Intent {
		t.Fatal("new child retained old observation", before, next)
	}
	p.mu.Lock()
	p.signalLocked(p.cur, syscall.SIGKILL)
	p.mu.Unlock()
	after := waitObservedExit(t, p)
	if after.ConfirmsUnexpectedExit(before) || !after.ConfirmsUnexpectedExit(next) {
		t.Fatal("stale child proof reused", after)
	}
}

func TestRuntimeSnapshotAutomaticLaunchChangesGenerationOnly(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	p.mu.Lock()
	p.signalLocked(p.cur, syscall.SIGKILL)
	p.mu.Unlock()
	if got := waitObservedExit(t, p); !got.ConfirmsUnexpectedExit(before) {
		t.Fatal("first owned exit not confirmed")
	}
	// Exercise the exact locked launch path used by the delayed restart,
	// without changing that existing timer or waiting for it in this test.
	p.mu.Lock()
	err := p.launchLocked()
	p.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	next := p.RuntimeSnapshot()
	if !next.Running || next.Generation <= before.Generation || next.Intent != before.Intent || next.ExitConfirmed {
		t.Fatal("automatic restart observation invalid", next)
	}
	p.mu.Lock()
	p.signalLocked(p.cur, syscall.SIGKILL)
	p.mu.Unlock()
	after := waitObservedExit(t, p)
	if after.ConfirmsUnexpectedExit(before) || !after.ConfirmsUnexpectedExit(next) {
		t.Fatal("automatic restart reused stale generation")
	}
}

func TestRuntimeSnapshotExhaustionDoesNotStopLifecycle(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	p.mu.Lock()
	p.generation = ^uint64(0) - 1
	p.lifecycleIntent = ^uint64(0) - 1
	p.mu.Unlock()
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := p.RuntimeSnapshot()
	if !before.Running || before.Generation != ^uint64(0) || before.Intent != ^uint64(0) {
		t.Fatal("counter fixture did not reach limit")
	}
	p.mu.Lock()
	p.signalLocked(p.cur, syscall.SIGKILL)
	p.mu.Unlock()
	after := waitObservedExit(t, p)
	if after.ConfirmsUnexpectedExit(before) {
		t.Fatal("exhausted counters authorized outage")
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if next := p.RuntimeSnapshot(); !next.Running || next.Generation != ^uint64(0) || next.Intent != ^uint64(0) {
		t.Fatal("exhaustion recycled counter or stopped VPN", next)
	}
}

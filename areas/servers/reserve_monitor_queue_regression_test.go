package servers

import (
	"context"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

func TestReserveMonitorUnconfirmedRetryBackoff(t *testing.T) {
	for _, changeRevision := range []bool{false, true} {
		name := "same-revision"
		if changeRevision {
			name = "changed-revision"
		}
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			a := e.addServer(t, "trojan://pw@a.example:443#A")
			b := e.addServer(t, "trojan://pw@b.example:443#B")
			writeReserveAutomatic(t, e, a, b, true)
			now := time.Now()
			e.s.now = func() time.Time { return now }
			guard, err := platform.TryLock(e.d.Root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			if err := e.d.Lock.ReconcileDaemonReserve(e.d.Root, guard, false, nil); err != nil {
				t.Fatal(err)
			}
			if err := e.s.enqueueReserveDue(); err != nil {
				t.Fatal(err)
			}
			first, ok := e.d.Queue.FindLive(compat.ReserveSwitchAction, compat.ReserveOperationTarget)
			if !ok {
				t.Fatal("initial check not queued")
			}
			ctx, cancel := context.WithCancel(e.d.Ctx)
			done := make(chan struct{})
			go func() { defer close(done); e.d.Queue.Run(ctx) }()
			t.Cleanup(func() { cancel(); <-done })
			waitReq(t, e, first.RequestID, "failed")
			cancel()
			<-done
			if e.d.Lock.Busy() {
				t.Fatal("clean unconfirmed check retained fence")
			}
			now = now.Add(14 * time.Second)
			if err := e.s.enqueueReserveDue(); err != nil {
				t.Fatal(err)
			}
			if _, ok := e.d.Queue.FindLive(compat.ReserveSwitchAction, compat.ReserveOperationTarget); ok {
				t.Fatal("unconfirmed monitor queued again before retry interval")
			}
			if changeRevision {
				writeReserveAutomatic(t, e, b, a, true)
			} else {
				now = now.Add(time.Second)
			}
			if err := e.s.enqueueReserveDue(); err != nil {
				t.Fatal(err)
			}
			next, ok := e.d.Queue.FindLive(compat.ReserveSwitchAction, compat.ReserveOperationTarget)
			if !ok || next.RequestID == first.RequestID {
				t.Fatal("due or revised monitor was not queued")
			}
			prefs, err := readReservePreferences(e.d)
			if err != nil || next.Context != prefs.Revision {
				t.Fatal("queued stale revision", err)
			}
		})
	}
}

func TestReserveStatusDoesNotWaitForProbe(t *testing.T) {
	e := newEnv(t)
	m := e.s.reserveMon
	m.mu.Lock()
	type result struct {
		value any
		err   error
	}
	done := make(chan result, 1)
	go func() { value, err := e.s.reserveStatus(); done <- result{value, err} }()
	select {
	case got := <-done:
		m.mu.Unlock()
		if got.err != nil {
			t.Fatal(got.err)
		}
		status := got.value.(map[string]any)
		if status["checking"] != true || status["state"] != nil {
			t.Fatal("busy monitor exposed stale state", status)
		}
	case <-time.After(250 * time.Millisecond):
		m.mu.Unlock()
		<-done
		t.Fatal("status waited for probe mutex")
	}
	value, err := e.s.reserveStatus()
	if err != nil {
		t.Fatal(err)
	}
	if value.(map[string]any)["checking"] != false {
		t.Fatal("idle status still checking")
	}
}

func TestReserveVerifiedSampleFeedsActiveSummary(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	s := f.e.s
	s.d.Svc.XrayRunning = func() bool { return true }
	s.socksListening = func(string, int) bool { return true }
	m := s.reserveMon
	probe, err := s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome)
	if err != nil || !probe.Sample.Verified || probe.Sample.Verdict != "healthy" {
		t.Fatal("active reserve probe did not complete", err, probe.Sample)
	}
	now := probe.Sample.FinishedAt
	s.now = func() time.Time { return now }
	m.mu.Lock()
	defer m.mu.Unlock()
	check := func(state string, available, up bool) {
		t.Helper()
		summary, err := s.summary(f.ctx)
		if err != nil || summary["connectionState"] != state ||
			get(summary, "activeConnection", "available") != available || get(summary, "activeConnection", "up") != up {
			t.Fatal("unexpected active connection", err, summary["connectionState"], summary["activeConnection"])
		}
	}
	check("connected", true, true)
	now = now.Add(31 * time.Second)
	check("degraded", false, false)
	now = probe.Sample.FinishedAt
	writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
	check("degraded", false, false)
}

package servers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/modules/reserve"
)

func TestReserveMonitorPublishesIntentWithExactGuard(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	m := &reserveMonitor{}
	if _, err := f.e.s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome); err != nil {
		t.Fatal(err)
	}
	// Prior verified policy history is synthetic; the third failure below uses
	// the production proxy/TLS probe against the unavailable owned upstream.
	now := time.Now()
	m.state.LastSwitchAt = now.Add(-time.Minute)
	m.state.EvidenceSince = m.state.LastSwitchAt
	m.state.ActiveFailures = 2
	m.state.Samples[m.state.ActiveID] = reserve.Sample{ServerID: m.state.ActiveID, Fingerprint: m.state.Settings.Primary.Fingerprint, Identity: m.state.ActiveIdentity, Verdict: "unhealthy", Verified: true, StartedAt: now.Add(-15 * time.Second), FinishedAt: now.Add(-14 * time.Second)}
	b := m.state.Settings.Backups[0]
	m.state.Samples[b.ID] = reserve.Sample{ServerID: b.ID, Fingerprint: b.Fingerprint, Identity: "fixture-previous-backup-proof", Verdict: "healthy", Verified: true, StartedAt: now.Add(-2 * time.Second), FinishedAt: now.Add(-time.Second)}
	failed, err := f.e.s.monitorReserve(f.ctx, m)
	if err != nil || failed.Intent != nil || m.state.ActiveFailures != reserve.DefaultPolicy().Failures {
		t.Fatalf("used old readiness after active failure: %+v %v", failed, err)
	}
	got, err := f.e.s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome)
	if err != nil || got.Sample.ServerID != f.backup || got.Intent == nil || got.Intent.To != f.backup || got.Intent.Reason != "active-failed" || got.Guard.snapshot != f.process.RuntimeSnapshot() || got.Guard.identity != failed.Sample.Identity || got.Guard.plan.node.ID != f.plan.node.ID {
		t.Fatalf("intent guard: %+v %v", got, err)
	}
	if m.state.Pending == nil || ActiveID(f.e.d) != f.plan.node.ID {
		t.Fatal("intention applied before transaction")
	}
	called := false
	got, err = f.e.s.monitorReserveWith(f.ctx, m, func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome {
		called = true
		return reserve.ProbeOutcome{}
	})
	if err != nil || called || got.Probed {
		t.Fatal("pending transaction started another probe", err)
	}
}

func TestReserveMonitorUsesOwnedProbesAndPreservesActive(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	m := &reserveMonitor{}
	before := f.process.RuntimeSnapshot()
	config, err := os.ReadFile(f.e.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.e.s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome)
	if err != nil || !got.Probed || !got.Sample.Verified || got.Sample.ServerID != f.plan.node.ID || got.Sample.Verdict != "healthy" || got.Intent != nil {
		t.Fatalf("active round: %+v %v", got, err)
	}
	got, err = f.e.s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome)
	if err != nil || !got.Probed || !got.Sample.Verified || got.Sample.ServerID != f.backup || got.Sample.Verdict != "healthy" {
		t.Fatalf("backup round: %+v %v", got, err)
	}
	got, err = f.e.s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome)
	if err != nil || got.Probed {
		t.Fatalf("rate limit: %+v %v", got, err)
	}
	after, err := os.ReadFile(f.e.d.Root.XrayConfig())
	if err != nil || !bytes.Equal(config, after) || f.process.RuntimeSnapshot() != before || ActiveID(f.e.d) != f.plan.node.ID {
		t.Fatal("monitor changed active runtime", err)
	}
}

func TestReserveMonitorRetainsExactFailedBinding(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	m := &reserveMonitor{}
	got, err := f.e.s.monitorReserve(f.ctx, m)
	if err != nil || !got.Sample.Verified || got.Sample.Verdict != "unhealthy" || m.state.ActiveFailures != 1 {
		t.Fatalf("production probe: %+v %v", got, err)
	}
	if m.lastFailure == nil || m.lastFailure.snapshot != f.process.RuntimeSnapshot() || m.lastFailure.identity != got.Sample.Identity {
		t.Fatal("exact failed binding lost")
	}
}

func TestReserveMonitorCountsConfirmedOwnedExit(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	m := &reserveMonitor{binding: &f.binding}
	reserveTerminateOwnedFixture(t, f)
	called := false
	got, err := f.e.s.monitorReserveWith(f.ctx, m, func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome {
		called = true
		return reserve.ProbeOutcome{}
	})
	if err != nil || called || !got.Sample.Verified || got.Sample.Verdict != "unhealthy" || m.state.ActiveFailures != 1 || m.lastFailure == nil || m.lastFailure.snapshot != f.binding.snapshot {
		t.Fatalf("owned exit: %+v %v called%v", got, err, called)
	}
}

func TestReserveMonitorRejectsUnfencedOffAndChangedAuthority(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	m := &reserveMonitor{}
	called := false
	probe := func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
		called = true
		return reserveVerifiedOutcome(ctx, p)
	}
	if _, err := f.e.s.monitorReserveWith(context.Background(), m, probe); err == nil || called {
		t.Fatal("unfenced monitor admitted")
	}
	got, err := f.e.s.monitorReserveWith(f.ctx, m, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
		out := reserveVerifiedOutcome(ctx, p)
		writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
		return out
	})
	if err == nil || got.Sample.Verified || got.Intent != nil || m.state.ActiveFailures != 0 {
		t.Fatalf("changed authority trusted: %+v %v", got, err)
	}
	called = false
	got, err = f.e.s.monitorReserveWith(f.ctx, m, probe)
	if err != nil || got.Probed || called || m.state.Settings.Enabled {
		t.Fatal("OFF monitor probed", got, err)
	}
}

func TestReserveMonitorRetainsUnconfirmedCleanup(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	m := &reserveMonitor{}
	if _, err := f.e.s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome); err != nil {
		t.Fatal(err)
	}
	var work string
	got, err := f.e.s.monitorReserveWith(f.ctx, m, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
		out := reserveVerifiedOutcome(ctx, p)
		work = filepath.Dir(filepath.Dir(p.ConfigPath))
		if e := os.Rename(work, work+"-retained"); e != nil {
			t.Fatal(e)
		}
		if e := os.Mkdir(work, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(work, "foreign"), []byte("keep"), 0600); e != nil {
			t.Fatal(e)
		}
		return out
	})
	if !errors.Is(err, errReserveProbeCleanup) || !m.blocked || got.Sample.Verified || got.Intent != nil {
		t.Fatalf("cleanup was released: %+v %v", got, err)
	}
	called := false
	_, err = f.e.s.monitorReserveWith(f.ctx, m, func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome {
		called = true
		return reserve.ProbeOutcome{}
	})
	if !errors.Is(err, errReserveProbeCleanup) || called {
		t.Fatal("blocked monitor restarted", err)
	}
	b, e := os.ReadFile(filepath.Join(work, "foreign"))
	if e != nil || string(b) != "keep" {
		t.Fatal("foreign cleanup removed", e)
	}
}

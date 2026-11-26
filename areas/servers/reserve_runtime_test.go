package servers

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
	"golang.org/x/sys/unix"
)

type reserveRuntimeFixture struct {
	e       *fakeEnv
	ctx     context.Context
	plan    reserveRuntimePlan
	binding reserveActiveBinding
	process *xray.Process
	backup  string
	release compat.Release
}

func newReserveRuntimeFixture(t *testing.T) reserveRuntimeFixture {
	t.Helper()
	return newReserveRuntimeFixtureWithLinks(t, "trojan://pw@127.0.0.1:1#A", "trojan://pw@127.0.0.1:2#B")
}

func newReserveRuntimeFixtureWithLinks(t *testing.T, primary, backup string) reserveRuntimeFixture {
	t.Helper()
	e := newEnv(t)
	a := e.addServer(t, primary)
	b := e.addServer(t, backup)
	if err := compat.WriteJSON(settingsFile(e.d), map[string]any{"listenAddress": "127.0.0.1", "socksPort": freeTCP(t), "logLevel": "none"}); err != nil {
		t.Fatal(err)
	}
	writeReserveAutomatic(t, e, a, b, true)
	core, err := os.ReadFile(os.Getenv("BRORAY_XRAY_BIN"))
	if err != nil {
		t.Fatal("pinned official core fixture required: ", err)
	}
	if err := os.WriteFile(e.d.Root.XrayBinary(), core, 0700); err != nil {
		t.Fatal(err)
	}
	p := xray.NewProcess(e.d.Root)
	e.d.App = &app.App{Root: e.d.Root, Xray: p}
	release, ok := e.d.Lock.TryAcquire("test", "reserve-binding", a)
	if !ok {
		t.Fatal("fixture fence")
	}
	t.Cleanup(release)
	ctx := compat.WithFence(e.d.Ctx)
	endpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(freeTCP(t)))
	plan, err := e.s.prepareReserveRuntime(ctx, a, endpoint)
	if err != nil {
		t.Fatal("prepare: ", err)
	}
	if err := platform.WriteFileAtomic(e.d.Root.XrayConfig(), plan.config, 0600); err != nil {
		t.Fatal(err)
	}
	if err := setActiveID(e.d, a); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	var binding reserveActiveBinding
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		binding, err = e.s.bindReserveRuntime(ctx, plan, nil)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		snapshot := p.RuntimeSnapshot()
		identity, captureErr := reserve.CaptureProcess(snapshot.PID, e.d.Root.XrayBinary())
		_, proofErr := reserve.VerifyProbeTarget(reserve.ProbeTarget{Process: identity, ConfigPath: e.d.Root.XrayConfig(), ConfigSHA256: plan.configSHA, Endpoint: plan.endpoint})
		raw, logErr := os.ReadFile(filepath.Join(e.d.Root.Logs(), "xray.log"))
		if len(raw) > 4096 {
			raw = raw[len(raw)-4096:]
		}
		t.Logf("fixture snapshot=%+v capture=%v proof=%v coreLogError=%v coreLog=%s", snapshot, captureErr, proofErr, logErr, raw)
		t.Fatal("owned binding: ", err)
	}
	return reserveRuntimeFixture{e, ctx, plan, binding, p, b, release}
}

func reserveVerifiedOutcome(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
	started := time.Now()
	identity, err := reserve.VerifyProbeTarget(target)
	if err != nil {
		return reserve.ProbeOutcome{Verdict: "unknown", StartedAt: started, FinishedAt: time.Now()}
	}
	return reserve.ProbeOutcome{Verdict: "healthy", Identity: identity, StartedAt: started, FinishedAt: time.Now()}
}

func TestReserveRuntimeOwnsRenderedProfileAndMapsEvidence(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	got := f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, reserveVerifiedOutcome)
	if !got.Verified || got.Verdict != "healthy" || got.ServerID != f.plan.node.ID || got.Fingerprint != f.plan.node.Fingerprint || got.Identity != f.binding.identity {
		t.Fatalf("sample: %+v", got)
	}
	// The public production path uses genuine TLS/proxy probing. This fixture
	// has an unavailable upstream; ownership does not imply healthy traffic.
	got = f.e.s.probeReserveRuntime(f.ctx, f.binding)
	if !got.Verified || got.Verdict != "unhealthy" {
		t.Fatalf("unavailable upstream: %+v", got)
	}
	if _, err := f.e.s.bindReserveRuntime(context.Background(), f.plan, nil); err == nil {
		t.Fatal("unfenced binding")
	}
	if got := f.e.s.probeReserveRuntimeWith(context.Background(), f.binding, reserveVerifiedOutcome); got.Verified {
		t.Fatal("unfenced sample")
	}
}

func TestReserveRuntimeRejectsChangedAuthority(t *testing.T) {
	for _, mutation := range []string{"record", "settings", "roster", "config", "pointer", "paused", "maintenance", "supervisor", "disabled"} {
		t.Run(mutation, func(t *testing.T) {
			f := newReserveRuntimeFixture(t)
			switch mutation {
			case "record":
				r, err := ReadRecord(f.e.d, f.plan.node.ID)
				if err != nil {
					t.Fatal(err)
				}
				r["password"] = "changed"
				if err := WriteRecord(f.e.d, r); err != nil {
					t.Fatal(err)
				}
			case "settings":
				if err := compat.WriteJSON(settingsFile(f.e.d), map[string]any{"socksPort": freeTCP(t)}); err != nil {
					t.Fatal(err)
				}
			case "roster":
				writeReserveAutomatic(t, f.e, f.backup, f.plan.node.ID, true)
			case "config":
				if err := os.WriteFile(f.e.d.Root.XrayConfig(), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "pointer":
				if err := setActiveID(f.e.d, f.backup); err != nil {
					t.Fatal(err)
				}
			case "paused":
				if err := f.e.d.Queue.SetPaused(true); err != nil {
					t.Fatal(err)
				}
			case "maintenance":
				if err := os.WriteFile(filepath.Join(f.e.d.Root.State(), "xray-maintenance.json"), []byte(`{"schemaVersion":1,"status":"removed"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "supervisor":
				f.e.d.App.Xray = xray.NewProcess(f.e.d.Root)
			case "disabled":
				writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
			}
			calls := 0
			got := f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
				calls++
				return reserveVerifiedOutcome(ctx, target)
			})
			if got.Verified || got.Verdict != "unknown" || got.Identity != "" || calls != 0 {
				t.Fatalf("changed %s accepted: %+v calls%d", mutation, got, calls)
			}
		})
	}
}

func TestReserveRuntimeRejectsMutationDuringProbe(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	got := f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		out := reserveVerifiedOutcome(ctx, target)
		if err := os.WriteFile(f.e.d.Root.XrayConfig(), []byte(`{}`), 0600); err != nil {
			t.Error(err)
		}
		return out
	})
	if got.Verified || got.Identity != "" || got.Verdict != "unknown" {
		t.Fatalf("stale result accepted: %+v", got)
	}
}

func reserveTerminateOwnedFixture(t *testing.T, f reserveRuntimeFixture) {
	t.Helper()
	fd, err := unix.PidfdOpen(f.binding.target.Process.PID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	// Revalidate exact owned listener/process after opening the handle. The
	// handle cannot retarget a reused PID between this proof and the signal.
	if _, err := reserve.VerifyProbeTarget(f.binding.target); err != nil {
		t.Fatal(err)
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f.process.RuntimeSnapshot().ConfirmsUnexpectedExit(f.binding.snapshot) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("owned exit not confirmed")
}

func TestReserveRuntimeConfirmedExitAndAutomaticGeneration(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	reserveTerminateOwnedFixture(t, f)
	got := f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome {
		t.Error("probe called for reaped child")
		return reserve.ProbeOutcome{}
	})
	if !got.Verified || got.Verdict != "unhealthy" || got.Identity != f.binding.identity {
		t.Fatalf("owned exit: %+v", got)
	}
	var next reserveActiveBinding
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		next, err = f.e.s.bindReserveRuntime(f.ctx, f.plan, &f.binding)
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil || next.snapshot.Generation <= f.binding.snapshot.Generation || next.identity != f.binding.identity {
		t.Fatalf("automatic relaunch lost desired lifecycle: %v", err)
	}
	if old := f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, reserveVerifiedOutcome); old.Verified {
		t.Fatal("old child accepted after relaunch")
	}
	if fresh := f.e.s.probeReserveRuntimeWith(f.ctx, next, reserveVerifiedOutcome); !fresh.Verified {
		t.Fatal("new child not accepted after fresh ownership proof")
	}
	if err := f.process.Restart(f.ctx); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		next, err = f.e.s.bindReserveRuntime(f.ctx, f.plan, &f.binding)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || next.identity == f.binding.identity {
		t.Fatal("manual restart reused policy identity")
	}
}

func TestReserveRuntimeManualStopAndUntrustedOutcomeAreUnknown(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	got := f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		out := reserveVerifiedOutcome(ctx, target)
		out.Identity = "foreign"
		return out
	})
	if got.Verified {
		t.Fatal("foreign outcome accepted")
	}
	if err := f.process.Stop(f.ctx); err != nil {
		t.Fatal(err)
	}
	got = f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, reserveVerifiedOutcome)
	if got.Verified || got.Verdict != "unknown" {
		t.Fatal("manual stop treated as outage")
	}
}

func TestReserveRuntimeRejectsLinkedInputsAndReplacedCore(t *testing.T) {
	for _, input := range []string{"record", "settings", "pointer", "config", "core", "core-replaced"} {
		t.Run(input, func(t *testing.T) {
			f := newReserveRuntimeFixture(t)
			paths := map[string]string{"record": RecordPath(f.e.d, f.plan.node.ID), "settings": settingsFile(f.e.d),
				"pointer": ActiveFile(f.e.d), "config": f.e.d.Root.XrayConfig(), "core": f.e.d.Root.XrayBinary(), "core-replaced": f.e.d.Root.XrayBinary()}
			path := paths[input]
			if err := os.Rename(path, path+".saved"); err != nil {
				t.Fatal(err)
			}
			if input == "core-replaced" {
				data, err := os.ReadFile(path + ".saved")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(path+".saved", path); err != nil {
				t.Fatal(err)
			}
			called := false
			out := f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
				called = true
				return reserveVerifiedOutcome(ctx, target)
			})
			if out.Verified || out.Identity != "" || called {
				t.Fatalf("untrusted %s used: %+v", input, out)
			}
		})
	}
}

func TestReserveRuntimeRejectsInvalidProbeResultAndCancellation(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	for _, mutation := range []string{"identity", "verdict", "started", "finished", "future", "cancelled"} {
		t.Run(mutation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			out := f.e.s.probeReserveRuntimeWith(ctx, f.binding, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
				result := reserveVerifiedOutcome(ctx, target)
				switch mutation {
				case "identity":
					result.Identity = "other"
				case "verdict":
					result.Verdict = "unknown"
				case "started":
					result.StartedAt = time.Now().Add(-time.Hour)
				case "finished":
					result.FinishedAt = time.Time{}
				case "future":
					result.FinishedAt = time.Now().Add(time.Hour)
				case "cancelled":
					cancel()
				}
				return result
			})
			if out.Verified || out.Identity != "" || out.Verdict != "unknown" {
				t.Fatalf("invalid outcome accepted: %+v", out)
			}
		})
	}
}

func TestReserveRuntimeOwnedExitDuringProbeIsNotHealthy(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	out := f.e.s.probeReserveRuntimeWith(f.ctx, f.binding, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		result := reserveVerifiedOutcome(ctx, target)
		reserveTerminateOwnedFixture(t, f)
		return result
	})
	if !out.Verified || out.Verdict != "unhealthy" || out.Identity != f.binding.identity {
		t.Fatalf("owned exit after HTTPS reported healthy: %+v", out)
	}
}

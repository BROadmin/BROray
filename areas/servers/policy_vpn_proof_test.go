package servers

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
)

func TestPolicyVPNProbeBindsPayloadToCurrentRuntime(t *testing.T) {
	f := newReserveRuntimeFixtureWithLinks(t, reserveHotPeer(t, "A"), reserveHotPeer(t, "B"))
	config := f.plan.config
	calls := 0
	probe := func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		calls++
		start := time.Now()
		identity, err := reserve.VerifyProbeTarget(target)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := reserveHotPayload(ctx, target.Endpoint)
		if err != nil || payload != "A" {
			t.Fatalf("bound health path did not pass the known VPN payload: %q, %v", payload, err)
		}
		return reserve.ProbeOutcome{Verdict: "healthy", Identity: identity, StartedAt: start, FinishedAt: time.Now()}
	}
	if got := f.e.s.probeAppliedPolicyVPNWith(f.ctx, config, probe); got != compat.VPNVerified || calls != 1 {
		t.Fatalf("valid owned payload: %s, calls=%d", got, calls)
	}
	if got := f.e.s.probeAppliedPolicyVPNWith(context.Background(), config, probe); got != compat.VPNUnconfirmed || calls != 1 {
		t.Fatalf("unfenced proof accepted: %s, calls=%d", got, calls)
	}
	stale := func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		outcome := probe(ctx, target)
		outcome.StartedAt = outcome.StartedAt.Add(-time.Hour)
		return outcome
	}
	if got := f.e.s.probeAppliedPolicyVPNWith(f.ctx, config, stale); got != compat.VPNUnconfirmed {
		t.Fatalf("old payload was reported as current: %s", got)
	}
	probeCtx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	cancelled := func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		outcome := probe(ctx, target)
		cancel()
		return outcome
	}
	if got := f.e.s.probeAppliedPolicyVPNWith(probeCtx, config, cancelled); got != compat.VPNUnconfirmed {
		t.Fatalf("cancelled check was reported as current: %s", got)
	}
	drift := func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		outcome := probe(ctx, target)
		changed := append(append([]byte(nil), config...), '\n')
		if err := platform.WriteFileAtomicNoFollow(f.e.d.Root.XrayConfig(), changed, 0600); err != nil {
			t.Fatal(err)
		}
		return outcome
	}
	if got := f.e.s.probeAppliedPolicyVPNWith(f.ctx, config, drift); got != compat.VPNUnconfirmed {
		t.Fatalf("changed live bytes accepted after probe: %s", got)
	}
}

func TestGeoApplyPreparesOwnedDiagnosticForOrdinaryConfig(t *testing.T) {
	f, _ := reserveModeQueueFromRuntime(t, newReserveRuntimeFixtureWithLinks(t, reserveHotPeer(t, "A"), reserveHotPeer(t, "B")))
	path := f.e.d.Root.XrayConfig()
	ordinary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reserve.DiagnosticEndpoint(ordinary); ok {
		t.Fatal("expected an ordinary starting layout")
	}
	preparer, ok := any(f.e.s).(interface {
		preparePolicyVPN(context.Context, []byte) ([]byte, io.Closer)
	})
	if !ok {
		t.Fatal("changed Geo policy cannot prepare a diagnostic path for the ordinary layout")
	}
	incoming := append(append([]byte(nil), ordinary...), '\n')
	before := f.process.RuntimeSnapshot()
	prepared, lease := preparer.preparePolicyVPN(f.ctx, incoming)
	if lease == nil {
		t.Fatal("no lease for ordinary profile")
	}
	t.Cleanup(func() { _ = lease.Close() })
	endpoint, ok := reserve.DiagnosticEndpoint(prepared)
	if !ok || !endpoint.Addr().IsLoopback() {
		t.Fatalf("missing private canonical diagnostic path: %v", endpoint)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, ordinary) || f.process.RuntimeSnapshot() != before {
		t.Fatalf("preparing changed working configuration or process: %v", err)
	}
	conflict, err := net.Listen("tcp4", endpoint.String())
	if err == nil {
		_ = conflict.Close()
		t.Fatal("port was not leased during preparation")
	}
	existing, extra := preparer.preparePolicyVPN(f.ctx, f.plan.config)
	if extra != nil || !bytes.Equal(existing, f.plan.config) {
		t.Fatal("existing canonical layout was changed")
	}
	unfenced, extra := preparer.preparePolicyVPN(context.Background(), incoming)
	if extra != nil || !bytes.Equal(unfenced, incoming) {
		t.Fatal("unfenced input was decorated")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := f.process.ReplaceIfCurrent(f.ctx, before, func() error {
		return platform.WriteFileAtomicNoFollow(path, prepared, 0600)
	})
	if err != nil || after.Generation != before.Generation+1 {
		t.Fatalf("wanted one new generation: %v, %d -> %d", err, before.Generation, after.Generation)
	}
	called := false
	probe := func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		called = true
		started := time.Now()
		identity, err := reserve.VerifyProbeTarget(target)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := reserveHotPayload(ctx, target.Endpoint)
		if err != nil || payload != "A" {
			t.Fatalf("new ordinary process did not carry the expected VPN payload: %q, %v", payload, err)
		}
		return reserve.ProbeOutcome{Verdict: "healthy", Identity: identity, StartedAt: started, FinishedAt: time.Now()}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := f.e.s.probeAppliedPolicyVPNWith(f.ctx, prepared, probe)
		if got == compat.VPNVerified && called {
			break
		}
		if called || time.Now().After(deadline) {
			t.Fatalf("ordinary VPN proof was %s, control called=%t", got, called)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

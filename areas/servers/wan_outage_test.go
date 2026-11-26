package servers

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/BROadmin/broray/modules/health"
)

func TestActiveWANOutagePausesAutomaticSelection(t *testing.T) {
	e, id, _ := reserveAutomaticEnv(t)
	e.health.Store("unhealthy")
	probe := e.s.activeProbe
	e.s.activeProbe = func(ctx context.Context, path, id string) map[string]any {
		result := probe(ctx, path, id)
		result["attempts"] = 2
		return result
	}
	checks := 0
	e.s.readWANOutage = func(context.Context) bool {
		checks++
		return true
	}
	runReserveQueue(t, e)
	runCheck := func() map[string]any {
		t.Helper()
		op, err := e.d.Queue.Enqueue(actionHealth, id, "AUTO_SWITCH", e.s.runtimeContext(id))
		if err != nil {
			t.Fatal(err)
		}
		waitReq(t, e, op.RequestID, "completed")
		state, ok := e.s.readState()
		if !ok {
			t.Fatal("missing automatic state")
		}
		return state
	}
	paused := runCheck()
	if count, ok := isInt(paused["consecutiveFailures"]); !ok || count != 0 ||
		paused["status"] != "paused" || e.s.cachedActiveHealth(id)["errorCode"] != wanUnavailable {
		t.Fatalf("WAN failure counted against VPN: %+v", paused)
	}
	if _, queued := e.d.Queue.FindLive(actionFailover, id); ActiveID(e.d) != id || queued || checks != 1 {
		t.Fatal("outage started selection")
	}
	e.health.Store("healthy")
	recovered := runCheck()
	if recovered["status"] != "healthy" || ActiveID(e.d) != id || checks != 1 {
		t.Fatalf("VPN did not recover in place: %+v", recovered)
	}
}

func TestActiveWANAmbiguousAndCancelledRemainDistinct(t *testing.T) {
	e, id, _ := reserveAutomaticEnv(t)
	e.health.Store("unhealthy")
	checks := 0
	e.s.readWANOutage = func(context.Context) bool { checks++; return false }
	probe := e.s.activeProbe
	e.s.activeProbe = func(ctx context.Context, path, id string) map[string]any {
		result := probe(ctx, path, id)
		result["attempts"] = 2
		return result
	}
	if verdict, result, _ := e.s.activeMeasure(context.Background(), id); verdict != "false" || result["errorCode"] != "PROXY_HTTPS_FAILED" || checks != 1 {
		t.Fatalf("indeterminate WAN blocked genuine probe failure: %s %+v", verdict, result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if verdict, result, _ := e.s.activeMeasure(ctx, id); verdict != "unknown" || result["errorCode"] != "ACTIVE_PROBE_CANCELLED" || checks != 1 {
		t.Fatalf("cancelled probe counted: %s %+v", verdict, result)
	}
}

func TestActiveControlsKeepBothObservations(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	config := `{"inbounds":[{"protocol":"socks","port":` + strconv.Itoa(port) + `}]}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	r := (&svc{now: time.Now}).socksProbe(context.Background(), path, "fixture")
	controls, ok := r["controls"].([]health.ControlObservation)
	if !ok || len(controls) != 2 || controls[0].Target != "cloudflare" || controls[1].Target != "google" ||
		controls[0].CurlCode != 7 || controls[1].CurlCode != 7 || r["curlCode"] != 7 {
		t.Fatalf("control attempts: %+v", r)
	}
}

func TestReserveMonitorHoldsVerifiedWANOutage(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	m := f.e.s.reserveMon
	before := f.process.RuntimeSnapshot()
	checks := 0
	f.e.s.readWANOutage = func(context.Context) bool { checks++; return true }
	r, err := f.e.s.monitorReserve(f.ctx, m)
	if err != nil || r.Intent != nil || !r.Sample.Verified || !r.Sample.WANUnavailable ||
		r.Sample.Verdict != "unknown" || m.state.ActiveFailures != 0 || checks != 1 {
		t.Fatalf("outage result: %+v %v", r, err)
	}
	if ActiveID(f.e.d) != f.plan.node.ID || f.process.RuntimeSnapshot() != before || m.lastFailure != nil {
		t.Fatal("WAN outage changed active binding")
	}
	r, err = f.e.s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome)
	if err != nil || r.Probed {
		t.Fatalf("standby probe ran during hold: %+v %v", r, err)
	}
}

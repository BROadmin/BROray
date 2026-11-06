package keenetic

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	kn "github.com/BROadmin/broray/internal/keenetic"
)

// Recorded-shape samples (ASSUMED KeeneticOS formats, see
// internal/keenetic/ipv6_test.go): global address on the WAN, a delegated
// global address on the home segment.
const sampleAddressesGlobal = `{"address":[
 {"address":"fe80::52ff:20ff:fe00:1","interface":"GigabitEthernet1","valid-lifetime":"infinite"},
 {"address":"2a02:2698:a002:1d::5","interface":"GigabitEthernet1","valid-lifetime":"86400"},
 {"address":"2a02:2698:a400:7e01::1","interface":"Bridge0","valid-lifetime":"86400"}]}`

func ipv6Of(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	v, ok := data["ipv6"].(map[string]any)
	if !ok {
		t.Fatalf("no ipv6 fact in %v", data)
	}
	return v
}

// waitIPv6 observes the background refresh without making a status request
// wait on a router read. Unknown needs an explicit completed-refresh check.
func waitIPv6(t *testing.T, s *service, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v := s.ipv6Fact(context.Background())
		s.ipv6Mu.Lock()
		ready := !s.ipv6At.IsZero() && !s.ipv6Refreshing
		s.ipv6Mu.Unlock()
		if ready && v["state"] == want {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("IPv6 refresh did not settle at %q", want)
	return nil
}

func TestIPv6FirstStatusDoesNotWaitForRouter(t *testing.T) {
	h := newHarness(t)
	h.r.rci = map[string]string{"show/ipv6/addresses": sampleAddressesGlobal}
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	entered := make(chan struct{}, 1)
	h.r.ipv6Block, h.r.ipv6Entered = release, entered
	type statusResult struct {
		code int
		body []byte
	}
	first := make(chan statusResult, 1)
	go func() {
		req := httptest.NewRequest("GET", "/api/keenetic/status.cgi?force=1", nil)
		w := httptest.NewRecorder()
		cgi.Serve(w, req, req.URL.Path, nil)
		first <- statusResult{w.Code, w.Body.Bytes()}
	}()
	select {
	case result := <-first:
		var v struct {
			Data struct {
				IPv6 map[string]any `json:"ipv6"`
			} `json:"data"`
		}
		if result.code != 200 || json.Unmarshal(result.body, &v) != nil || v.Data.IPv6["state"] != "unknown" {
			t.Fatalf("first status should return unknown IPv6: status=%d body=%s", result.code, result.body)
		}
	case <-time.After(2 * time.Second):
		close(release)
		<-first
		t.Fatal("status.cgi blocked on IPv6 router read")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("background IPv6 refresh did not start")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if v := h.s.ipv6Fact(context.Background()); v["state"] == "global" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background IPv6 fact was not cached")
}

func TestStatusIPv6UnknownByDefault(t *testing.T) {
	h := newHarness(t)
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	if !st.Success {
		t.Fatalf("%+v", st.Error)
	}
	v := ipv6Of(t, st.Data)
	if v["state"] != "unknown" || v["warning"] != nil || v["lanPrefix"] != false || v["checkedAt"] != nil {
		t.Fatalf("ipv6 %v", v)
	}
	if wan, ok := v["wan"].([]any); !ok || len(wan) != 0 {
		t.Fatalf("wan %#v", v["wan"])
	}
	if got := h.r.commands(); len(got) != 0 {
		t.Fatalf("IPv6 detection must be read-only, wrote %q", got)
	}
}

func TestStatusIPv6GlobalWarnsWithoutChangingHealth(t *testing.T) {
	h := newHarness(t)
	if r := h.call("POST", "/api/keenetic/create.cgi"); !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	before := h.call("GET", "/api/keenetic/status.cgi?force=1").Data["health"].(map[string]any)
	waitIPv6(t, h.s, "unknown")
	h.r.resetCommands()

	// Same router, fresh service (fresh IPv6 cache) with IPv6 answers.
	h.s = newService(h.d)
	current.Store(h.s)
	h.r.mu.Lock()
	h.r.rci = map[string]string{"show/ipv6/addresses": sampleAddressesGlobal, "show/ipv6/prefixes": `{}`}
	h.r.mu.Unlock()
	waitIPv6(t, h.s, "global")
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	v := ipv6Of(t, st.Data)
	if v["state"] != "global" || v["lanPrefix"] != true || v["warning"] != kn.IPv6WarningText || v["checkedAt"] == nil {
		t.Fatalf("ipv6 %v", v)
	}
	if !reflect.DeepEqual(v["wan"], []any{"GigabitEthernet1"}) {
		t.Fatalf("wan %v", v["wan"])
	}
	after := st.Data["health"].(map[string]any)
	if after["severity"] != before["severity"] || after["severity"] != "ok" || after["actionRequired"] != false {
		t.Fatalf("IPv6 must not change health: before %v after %v", before, after)
	}
	if got := h.r.commands(); len(got) != 0 {
		t.Fatalf("IPv6 detection must be read-only, wrote %q", got)
	}
}

func TestIPv6TextFallbackAndMalformed(t *testing.T) {
	h := newHarness(t)
	// No RCI JSON: ndmc-style text, link-local only → none, no warning.
	h.r.text = map[string][]string{"show ipv6 addresses": {
		"          address: ",
		"               address: fe80::1",
		"             interface: Bridge0",
	}}
	v := waitIPv6(t, h.s, "none")
	if v["state"] != "none" || v["warning"] != nil {
		t.Fatalf("none expected: %v", v)
	}

	// Malformed answers everywhere → unknown, never a warning.
	h = newHarness(t)
	h.r.rci = map[string]string{"show/ipv6/addresses": `{"status":[{"status":"error","message":"no such command"}]}`}
	h.r.text = map[string][]string{"show ipv6 prefixes": {"Command::Base error[7405602]: no such command: ipv6."}}
	v = waitIPv6(t, h.s, "unknown")
	if v["state"] != "unknown" || v["warning"] != nil {
		t.Fatalf("unknown expected: %v", v)
	}
}

func TestIPv6Cache(t *testing.T) {
	h := newHarness(t)
	h.r.rci = map[string]string{"show/ipv6/addresses": sampleAddressesGlobal}
	waitIPv6(t, h.s, "global")
	for i := 0; i < 5; i++ {
		if v := h.s.ipv6Fact(context.Background()); v["state"] != "global" {
			t.Fatal(v)
		}
		_ = h.call("GET", "/api/keenetic/status.cgi?force=1")
	}
	h.r.mu.Lock()
	shows := h.r.shows
	h.r.mu.Unlock()
	if shows != 1 {
		t.Fatalf("router asked %d times within the cache TTL", shows)
	}
	// The ISP turns IPv6 off: seen after the TTL.
	h.r.mu.Lock()
	h.r.rci["show/ipv6/addresses"] = `{"address":[{"address":"fe80::1","interface":"Bridge0"}]}`
	h.r.mu.Unlock()
	if v := h.s.ipv6Fact(context.Background()); v["state"] != "global" {
		t.Fatal("cached answer expected", v)
	}
	h.s.ipv6Mu.Lock()
	h.s.ipv6At = time.Now().Add(-IPv6CacheTTL - time.Second)
	h.s.ipv6Mu.Unlock()
	if v := waitIPv6(t, h.s, "none"); v["warning"] != nil {
		t.Fatal("refresh expected", v)
	}

	// Unknown is retried after the shorter delay, not the full TTL.
	h = newHarness(t)
	if v := waitIPv6(t, h.s, "unknown"); v["state"] != "unknown" {
		t.Fatal(v)
	}
	h.r.mu.Lock()
	h.r.rci = map[string]string{"show/ipv6/addresses": sampleAddressesGlobal}
	h.r.mu.Unlock()
	h.s.ipv6Mu.Lock()
	h.s.ipv6At = time.Now().Add(-ipv6UnknownRetryTTL - time.Second)
	h.s.ipv6Mu.Unlock()
	if v := waitIPv6(t, h.s, "global"); v["state"] != "global" {
		t.Fatal("retry expected", v)
	}
}

func TestIPv6CancelledRequestDoesNotCancelSharedRefresh(t *testing.T) {
	h := newHarness(t)
	h.r.rci = map[string]string{"show/ipv6/addresses": sampleAddressesGlobal}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v := h.s.ipv6Fact(ctx); v["state"] != "unknown" {
		t.Fatal(v)
	}
	if v := waitIPv6(t, h.s, "global"); v["state"] != "global" {
		t.Fatal(v)
	}
}

func TestIPv6ServiceHook(t *testing.T) {
	h := newHarness(t)
	// Without the keenetic area: unknown, never a warning.
	if v := h.d.Svc.IPv6(context.Background()); v["state"] != "unknown" || v["warning"] != nil {
		t.Fatal(v)
	}
	h.d.Maintenance = true // no startup reconcile goroutine in this test
	if err := setup(h.d); err != nil {
		t.Fatal(err)
	}
	h.r.rci = map[string]string{"show/ipv6/addresses": sampleAddressesGlobal}
	waitIPv6(t, current.Load(), "global")
	v := h.d.Svc.IPv6(context.Background())
	if v["state"] != "global" || !strings.Contains(v["warning"].(string), "IPv6") {
		t.Fatal(v)
	}
}

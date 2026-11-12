package keenetic

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	kn "github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/udpprobe"
)

// «Проверить UDP»: STUN responders on distinct loopback addresses stand for
// the Internet STUN servers; the injected probes emulate the router:
//
//	routed  — 127.0.0.10/11: candidates for the server/ProxyN parts;
//	direct  — 127.0.0.20: the WAN comparison;
//	exits   — WAN = 127.0.0.1 (default source), VPN = 127.0.0.3.
//
// fakeDirect looks at the fake router's running configuration like the
// KeeneticOS kernel would: a /32 to Proxy0 sends the datagram "through
// ProxyN" (mode vpn: from 127.0.0.3; wan: a broken ProxyN that leaks to
// WAN; drop: ProxyN without UDP).

type udpEnv struct {
	h            *harness
	routed       []string
	direct       string
	proxyMode    string // vpn | wan | drop
	socksOK      bool
	mu           sync.Mutex
	routedProbes []string // targets probed while a /32 → Proxy0 existed
	onRouted     func()
}

func stunAt(t *testing.T, ip string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp4", ip+":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go udpprobe.Responder(pc)
	return pc.LocalAddr().String()
}

func newUDPEnv(t *testing.T) *udpEnv {
	h := newHarness(t)
	if r := h.call("POST", "/api/keenetic/create.cgi"); !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	h.r.resetCommands()
	e := &udpEnv{h: h, proxyMode: "vpn", socksOK: true}
	e.routed = []string{stunAt(t, "127.0.0.10"), stunAt(t, "127.0.0.11")}
	e.direct = stunAt(t, "127.0.0.20")
	saved := []any{STUNRoutedCandidates, STUNDirectCandidates, STUNAllowLocal, STUNDirect, STUNViaSOCKS, udpProbeTimeout, udpRoutedTimeout, udpRouteSettle, udpRecoverDelay}
	t.Cleanup(func() {
		STUNRoutedCandidates, STUNDirectCandidates, STUNAllowLocal = saved[0].([]string), saved[1].([]string), saved[2].(bool)
		STUNDirect, STUNViaSOCKS = saved[3].(func(string, time.Duration) (*net.UDPAddr, error)), saved[4].(func(string, string, time.Duration) (*net.UDPAddr, error))
		udpProbeTimeout, udpRoutedTimeout, udpRouteSettle, udpRecoverDelay = saved[5].(time.Duration), saved[6].(time.Duration), saved[7].(time.Duration), saved[8].(time.Duration)
	})
	STUNRoutedCandidates, STUNDirectCandidates, STUNAllowLocal = e.routed, []string{e.direct}, true
	udpProbeTimeout, udpRoutedTimeout, udpRouteSettle, udpRecoverDelay = 600*time.Millisecond, 600*time.Millisecond, 0, 0
	STUNDirect = e.fakeDirect
	STUNViaSOCKS = func(socks, server string, timeout time.Duration) (*net.UDPAddr, error) {
		if socks != "192.168.1.1:2080" {
			return nil, fmt.Errorf("unexpected SOCKS endpoint %s", socks)
		}
		if !e.socksOK {
			return nil, errors.New("SOCKS5_UDP_REFUSED: UDP ASSOCIATE rejected (reply 7)")
		}
		return udpprobe.DirectFrom("127.0.0.3:0", server, timeout)
	}
	return e
}

func (e *udpEnv) fakeDirect(server string, timeout time.Duration) (*net.UDPAddr, error) {
	ap := netip.MustParseAddrPort(server)
	running, _ := e.h.r.RunningConfig(context.Background())
	if routeCoverage(mustRoutes(running), ap.Addr(), "Proxy0") == routeOwned {
		e.mu.Lock()
		e.routedProbes = append(e.routedProbes, server)
		e.mu.Unlock()
		if e.onRouted != nil {
			e.onRouted()
		}
		switch e.proxyMode {
		case "vpn":
			return udpprobe.DirectFrom("127.0.0.3:0", server, timeout)
		case "drop":
			return nil, errors.New("UDP_TIMEOUT: no STUN response")
		}
	}
	return udpprobe.Direct(server, timeout)
}

func mustRoutes(lines []string) []cfgRoute { r, _ := configRoutes(lines); return r }

func (e *udpEnv) run(t *testing.T) *udpCheckResult {
	t.Helper()
	release, ok := e.h.d.Lock.TryAcquire("keenetic", "keenetic:check-udp", "keenetic")
	if !ok {
		t.Fatal("fence busy")
	}
	defer release()
	return e.h.s.udpCheck(context.Background(), nil)
}

func ipOf(addr string) string { return netip.MustParseAddrPort(addr).Addr().String() }

func noSave(t *testing.T, cmds []string) {
	t.Helper()
	for _, c := range cmds {
		if strings.Contains(c, "save") {
			t.Fatalf("the UDP check must never save the configuration: %q", cmds)
		}
	}
}

func TestUDPCheckThroughBROray(t *testing.T) {
	e := newUDPEnv(t)
	startup := strings.Join(e.h.r.startup, "\n")
	r := e.run(t)
	if r.Verdict != "UDP_VIA_BROray" || r.Message != "UDP идёт через BROray (выход 127.0.0.3)" || r.Severity != "ok" {
		t.Fatalf("%+v", r)
	}
	if r.Server.Exit != "127.0.0.3" || r.Proxy.Exit != "127.0.0.3" || r.Direct.Exit != "127.0.0.1" {
		t.Fatalf("exits server=%s proxy=%s direct=%s", r.Server.Exit, r.Proxy.Exit, r.Direct.Exit)
	}
	ip := ipOf(e.routed[0])
	want := []string{"ip route " + ip + " 255.255.255.255 Proxy0 1200", "no ip route " + ip + " 255.255.255.255 Proxy0"}
	if got := e.h.r.commands(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("commands %q, want %q", got, want)
	}
	if len(e.routedProbes) != 1 || e.routedProbes[0] != e.routed[0] {
		t.Fatalf("the ProxyN probe must run while the temporary route exists: %q", e.routedProbes)
	}
	if r.Route.Mode != "temporary" || !r.Route.Added || !r.Route.Removed || len(r.Warnings) != 0 {
		t.Fatalf("route %+v warnings %q", r.Route, r.Warnings)
	}
	if hostRoutePresent(e.h.r.render(), netip.MustParseAddr(ip), "Proxy0") {
		t.Fatal("temporary route left in running-config")
	}
	if strings.Join(e.h.r.startup, "\n") != startup {
		t.Fatal("startup configuration changed")
	}
	if _, err := os.Lstat(e.h.s.udpReceiptPath()); !os.IsNotExist(err) {
		t.Fatal("receipt must be removed after a verified removal")
	}
}

func TestUDPCheckBypassAndProxyWithoutUDP(t *testing.T) {
	e := newUDPEnv(t)
	e.proxyMode = "wan"
	r := e.run(t)
	if r.Verdict != "BYPASS_WAN" || r.Message != "UDP идёт мимо VPN — через провайдера (127.0.0.1)" {
		t.Fatalf("%+v", r)
	}
	e.proxyMode = "drop"
	e.h.r.resetCommands()
	r = e.run(t)
	if r.Verdict != "PROXY_UDP_FAILED" || r.Message != "Не удалось подтвердить UDP через Proxy0: STUN-проверки не получили ответа" || !r.Route.Removed {
		t.Fatalf("%+v", r)
	}
	noSave(t, e.h.r.commands())
	// UDP switched off on the ProxyN: the verdict names the fix.
	makeTCPOnly(e.h, "Proxy0")
	r = e.run(t)
	if r.Verdict != "PROXY_UDP_FAILED" || !strings.Contains(r.Message, "выключен UDP через SOCKS5") {
		t.Fatalf("%+v", r)
	}
}

func TestUDPCheckServerWithoutUDP(t *testing.T) {
	e := newUDPEnv(t)
	e.socksOK = false
	r := e.run(t)
	if r.Verdict != "SERVER_UDP_FAILED" || r.Message != "Не удалось подтвердить UDP через сервер: STUN-проверки через Xray SOCKS UDP не получили ответа" || r.Proxy.Ran {
		t.Fatalf("%+v", r)
	}
	if len(r.Server.Tried) != 2 {
		t.Fatalf("every routed candidate must be tried: %q", r.Server.Tried)
	}
	if c := e.h.r.commands(); len(c) != 0 {
		t.Fatalf("no route may be added when the server does not pass UDP: %q", c)
	}
}

// A STUN address already routed to the managed ProxyN by a BROray set is
// probed as is: nothing added, nothing removed.
func TestUDPCheckOwnedRouteUntouched(t *testing.T) {
	e := newUDPEnv(t)
	ip := ipOf(e.routed[0])
	line := "ip route " + ip + " 255.255.255.255 Proxy0 1200"
	e.h.r.routes = []string{line}
	r := e.run(t)
	if r.Verdict != "UDP_VIA_BROray" || r.Route.Mode != "existing" || r.Route.Added {
		t.Fatalf("%+v", r)
	}
	if c := e.h.r.commands(); len(c) != 0 {
		t.Fatalf("owned route must be left alone: %q", c)
	}
	if !contains(e.h.r.routes, line) {
		t.Fatal("owned route removed")
	}
	// covered by a wider owned set route: same
	e.h.r.routes = []string{"ip route 127.0.0.0 255.255.255.0 Proxy0 1200"}
	r = e.run(t)
	if r.Route.Mode != "existing" || len(e.h.r.commands()) != 0 {
		t.Fatalf("%+v %q", r.Route, e.h.r.commands())
	}
	// the direct comparison never uses an address inside a route set
	if r.Direct.Ran && len(r.Direct.Tried) != 0 {
		t.Fatalf("direct candidate inside a route set was probed: %q", r.Direct.Tried)
	}
}

// A STUN address routed elsewhere (foreign route) is skipped; the next
// candidate is used. Nothing about the foreign route is touched.
func TestUDPCheckForeignRouteSkipped(t *testing.T) {
	e := newUDPEnv(t)
	first, second := ipOf(e.routed[0]), ipOf(e.routed[1])
	foreign := "ip route " + first + " 255.255.255.255 Wireguard0"
	e.h.r.routes = []string{foreign}
	r := e.run(t)
	if r.Verdict != "UDP_VIA_BROray" || !strings.HasPrefix(r.Proxy.Target, e.routed[1]) {
		t.Fatalf("%+v", r)
	}
	for _, c := range e.h.r.commands() {
		if strings.Contains(c, first) || !strings.Contains(c, second) {
			t.Fatalf("unexpected command %q", c)
		}
	}
	if !contains(e.h.r.routes, foreign) || len(e.h.r.routes) != 1 {
		t.Fatalf("routes %q", e.h.r.routes)
	}
	// every candidate routed elsewhere → cannot check, nothing changed
	e.h.r.routes = []string{"ip route 127.0.0.0 255.255.255.0 Wireguard0"}
	e.h.r.resetCommands()
	r = e.run(t)
	if r.Verdict != "CHECK_FAILED" || !strings.HasPrefix(r.Message, "Не удалось проверить: ") || len(e.h.r.commands()) != 0 {
		t.Fatalf("%+v %q", r, e.h.r.commands())
	}
	// an unparsable route line: no temporary route at all
	e.h.r.routes = []string{"ip route weird-token Wireguard0"}
	r = e.run(t)
	if r.Proxy.Ran || r.Verdict != "CHECK_FAILED" || len(e.h.r.commands()) != 0 {
		t.Fatalf("%+v %q", r, e.h.r.commands())
	}
}

func TestUDPCheckRequiresOwnedProxy(t *testing.T) {
	e := newUDPEnv(t)
	_ = os.Remove(e.h.s.receiptPath())
	r := e.run(t)
	if r.Verdict != "CHECK_FAILED" || !strings.Contains(r.Message, "не подтверждён") || len(e.h.r.commands()) != 0 {
		t.Fatalf("%+v %q", r, e.h.r.commands())
	}
}

// Removal happens on every path: cancel during the probe, a rejected add,
// a rejected removal (receipt kept → recovered later and at daemon start).
func TestUDPCheckRemovalOnFailure(t *testing.T) {
	e := newUDPEnv(t)
	ip := netip.MustParseAddr(ipOf(e.routed[0]))
	add, rm := udpRouteCommands("Proxy0", ip)

	// cancelled while the route exists
	ctx, cancel := context.WithCancel(context.Background())
	e.onRouted = cancel
	release, _ := e.h.d.Lock.TryAcquire("keenetic", "t", "keenetic")
	r := e.h.s.udpCheck(ctx, nil)
	release()
	e.onRouted = nil
	if !r.Route.Removed || hostRoutePresent(e.h.r.render(), ip, "Proxy0") {
		t.Fatalf("route must be removed after cancel: %+v %q", r.Route, e.h.r.routes)
	}

	// add rejected → nothing left, receipt gone
	e.h.r.resetCommands()
	e.h.r.reject[add] = true
	r = e.run(t)
	delete(e.h.r.reject, add)
	if r.Verdict != "CHECK_FAILED" || r.Proxy.Ran || len(e.h.r.routes) != 0 {
		t.Fatalf("%+v %q", r, e.h.r.routes)
	}
	if _, err := os.Lstat(e.h.s.udpReceiptPath()); !os.IsNotExist(err) {
		t.Fatal("receipt kept after a rejected add with nothing to remove")
	}

	// removal rejected → warning, receipt kept
	e.h.r.reject[rm] = true
	r = e.run(t)
	if r.Route.Removed || len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], rm) {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Lstat(e.h.s.udpReceiptPath()); err != nil {
		t.Fatal("receipt must stay while the route exists")
	}
	delete(e.h.r.reject, rm)

	// daemon start: a fresh service removes the leftover
	s2 := newService(e.h.d)
	s2.recoverUDPRouteAtStart(context.Background())
	if hostRoutePresent(e.h.r.render(), ip, "Proxy0") {
		t.Fatal("leftover temporary route not removed at start")
	}
	if _, err := os.Lstat(e.h.s.udpReceiptPath()); !os.IsNotExist(err) {
		t.Fatal("receipt not removed after recovery")
	}
	noSave(t, e.h.r.commands())

	// a receipt whose /32 is now owned by a route set is dropped, the route kept
	e.h.r.routes = []string{add}
	_ = compat.WriteJSON(e.h.s.udpReceiptPath(), udpRouteReceipt{SchemaVersion: 1, Purpose: udpReceiptPurpose, Network: ip.String(), Mask: udpHostMask, Interface: "Proxy0", Metric: 1200})
	_ = os.MkdirAll(e.h.d.Path("routes", "installed"), 0o700)
	_ = compat.WriteJSON(e.h.d.Path("routes", "installed", "routes.json"), map[string]any{"routes": []any{map[string]any{"key": "ipv4|" + ip.String() + "/32|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified"}}})
	if err := e.h.s.recoverUDPRoute(context.Background()); err != nil || !contains(e.h.r.routes, add) {
		t.Fatalf("owned route removed by recovery: %v %q", err, e.h.r.routes)
	}
}

func TestUDPRecoveryPreservesRouteWhenRegistryUnreadable(t *testing.T) {
	e := newUDPEnv(t)
	ip := netip.MustParseAddr(ipOf(e.routed[0]))
	add, _ := udpRouteCommands("Proxy0", ip)
	e.h.r.routes = []string{add}
	path := e.h.s.udpReceiptPath()
	if err := compat.WriteJSON(path, udpRouteReceipt{SchemaVersion: 1, Purpose: udpReceiptPurpose, Network: ip.String(), Mask: udpHostMask, Interface: "Proxy0", Metric: 1200}); err != nil {
		t.Fatal(err)
	}
	installed := e.h.d.Path("routes", "installed")
	if err := os.MkdirAll(installed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "routes.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.h.s.recoverUDPRoute(context.Background()); err == nil {
		t.Fatal("unreadable route registry accepted as proof that /32 is unowned")
	}
	if !hostRoutePresent(e.h.r.render(), ip, "Proxy0") {
		t.Fatal("ambiguous route removed")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("receipt lost while ownership ambiguous: %v", err)
	}
}

func TestUDPRecoveryPreservesMalformedReceipt(t *testing.T) {
	e := newUDPEnv(t)
	path := e.h.s.udpReceiptPath()
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.h.s.recoverUDPRoute(context.Background()); err == nil {
		t.Fatal("malformed receipt was silently accepted")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "{" {
		t.Fatalf("malformed receipt changed: %d bytes, error %v", len(got), err)
	}
}

// The endpoint: 202 + operationId, background run under the fence, status
// polled until done; busy fence → 409.
func TestUDPShutdownWaitsForTemporaryRouteCleanup(t *testing.T) {
	e := newUDPEnv(t)
	entered := make(chan struct{})
	releaseProbe := make(chan struct{})
	defer func() {
		select {
		case <-releaseProbe:
		default:
			close(releaseProbe)
		}
	}()
	var once sync.Once
	e.onRouted = func() {
		once.Do(func() { close(entered) })
		<-releaseProbe
	}
	if r := e.h.call("POST", "/api/keenetic/check-udp.cgi"); r.Status != 202 {
		t.Fatalf("start UDP check: %+v", r)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not reach temporary route")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.h.s.waitUDPJobs(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("shutdown finished before route cleanup: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseProbe)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("UDP cleanup exceeded shutdown deadline")
	}
	if _, err := os.Lstat(e.h.s.udpReceiptPath()); !os.IsNotExist(err) {
		t.Fatalf("temporary route receipt remains after shutdown wait: %v", err)
	}
	running, err := e.h.r.RunningConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range e.routed {
		if ip := netip.MustParseAddrPort(server).Addr(); hostRoutePresent(running, ip, "Proxy0") {
			t.Fatalf("temporary /32 for %s remains after shutdown wait", ip)
		}
	}
	e.h.expectErr(e.h.call("POST", "/api/keenetic/check-udp.cgi"), 503, "SERVICE_STOPPING")
}
func TestUDPShutdownWaitRespectsDeadline(t *testing.T) {
	h := newHarness(t)
	h.s.udpJobs.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := h.s.waitUDPJobs(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting past deadline: %v", err)
	}
	h.s.udpJobs.Done()
	if err := h.s.waitUDPJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestUDPCheckEndpoint(t *testing.T) {
	e := newUDPEnv(t)
	r := e.h.call("POST", "/api/keenetic/check-udp.cgi")
	if r.Status != 202 || r.Data["accepted"] != true || r.Data["operationId"] == "" {
		t.Fatalf("%+v", r)
	}
	var st resp
	for i := 0; i < 100; i++ {
		st = e.h.call("GET", "/api/keenetic/check-udp-status.cgi")
		if st.Data["running"] == false {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	res, _ := st.Data["result"].(map[string]any)
	if st.Data["running"] != false || res["verdict"] != "UDP_VIA_BROray" || st.Data["operationId"] != r.Data["operationId"] {
		t.Fatalf("%v", st.Data)
	}
	e.h.s.udpJobs.Wait()
	if e.h.d.Lock.Busy() {
		t.Fatal("fence not released")
	}
	op, ok := e.h.d.Queue.Operation(r.Data["operationId"].(string))
	if !ok || op.Public()["state"] != "completed" {
		t.Fatalf("operation %v", op)
	}
	release, _ := e.h.d.Lock.TryAcquire("routes", "export", "x")
	e.h.expectErr(e.h.call("POST", "/api/keenetic/check-udp.cgi"), 409, "OPERATION_BUSY")
	release()
}

func TestUDPCheckRejectsCrossOriginPOST(t *testing.T) {
	h := newHarness(t)
	// Keep the global fence occupied so the pre-fix endpoint cannot start a
	// network probe; the guard must reject before checking the fence.
	release, ok := h.d.Lock.TryAcquire("routes", "export", "csrf-test")
	if !ok {
		t.Fatal("could not occupy operation fence")
	}
	defer release()
	for _, tc := range []struct {
		name, code string
		headers    map[string]string
	}{
		{"missing header", "CSRF_REJECTED", map[string]string{"Origin": "http://example.com"}},
		{"missing origin", "ORIGIN_REJECTED", map[string]string{"X-BROray-Request": "operations"}},
		{"foreign origin", "ORIGIN_REJECTED", map[string]string{"X-BROray-Request": "operations", "Origin": "http://evil.example"}},
	} {
		r := h.callH("POST", "/api/keenetic/check-udp.cgi", tc.headers)
		if r.Status != 403 || r.Error.Code != tc.code {
			t.Errorf("%s: status=%d error=%s", tc.name, r.Status, r.Error.Code)
		}
	}
}

func TestUDPRouteCommandAllowlist(t *testing.T) {
	saved := STUNAllowLocal
	defer func() { STUNAllowLocal = saved }()
	STUNAllowLocal = false
	ok := [][3]string{
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy0 1200"},
		{"Proxy0", "162.159.207.0", "no ip route 162.159.207.0 255.255.255.255 Proxy0"},
		{"Proxy12", "74.125.250.129", "ip route 74.125.250.129 255.255.255.255 Proxy12 1200"},
	}
	for _, c := range ok {
		if !udpRouteCommandAllowed(c[0], c[1], c[2]) {
			t.Errorf("refused %q", c[2])
		}
	}
	bad := [][3]string{
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.0 Proxy0 1200"},       // not /32
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Wireguard0 1200"}, // foreign interface
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy1 1200"},     // not the managed ProxyN
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy0 100"},      // other metric
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy0"},          // no metric
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.1 255.255.255.255 Proxy0 1200"},     // other address
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy0 1200; system reboot"},
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy0 1200 ; system reboot"},
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy0 1200\nsystem reboot"},
		{"Proxy0", "162.159.207.0", "ip route 162.159.207.0  255.255.255.255 Proxy0 1200"},
		{"Proxy0", "162.159.207.0", "no ip route 162.159.207.0 255.255.255.255 Proxy0 1200 auto"},
		{"Proxy0", "162.159.207.0", "system configuration save"},
		{"Proxy0", "162.159.207.0", "system reboot"},
		{"Proxy256", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy256 1200"}, // out of range
		{"Proxy00", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Proxy00 1200"},
		{"Wireguard0", "162.159.207.0", "ip route 162.159.207.0 255.255.255.255 Wireguard0 1200"},
		{"Proxy0", "162.159.207.00", "ip route 162.159.207.00 255.255.255.255 Proxy0 1200"}, // non-canonical
	}
	for _, ip := range []string{"192.168.1.1", "10.0.0.1", "172.16.0.1", "127.0.0.1", "100.64.0.1", "198.18.0.1", "0.0.0.0", "224.0.0.1", "255.255.255.255", "169.254.1.1"} {
		bad = append(bad, [3]string{"Proxy0", ip, "ip route " + ip + " 255.255.255.255 Proxy0 1200"})
	}
	for _, c := range bad {
		if udpRouteCommandAllowed(c[0], c[1], c[2]) {
			t.Errorf("accepted %q (%s, %s)", c[2], c[0], c[1])
		}
	}
	// the shared routes allowlist refuses injections too
	for _, c := range []string{"ip route 1.2.3.4 255.255.255.255 Proxy0 1200; system reboot", "ip route 1.2.3.4 255.255.255.255 Wireguard0 1200",
		"no ip route 1.2.3.4 255.255.255.255 Proxy0\nsystem reboot", "ip route 1.2.3.4 255.255.255.255 Proxy256 1200"} {
		if kn.Allow(kn.AreaRoutes, c) == nil {
			t.Errorf("routes allowlist accepted %q", c)
		}
	}
	// the refused command is never dispatched
	h := newHarness(t)
	if err := h.s.runUDPRoute(context.Background(), "Proxy0", netip.MustParseAddr("10.0.0.1"), "ip route 10.0.0.1 255.255.255.255 Proxy0 1200"); err == nil || len(h.r.commands()) != 0 {
		t.Fatalf("private address dispatched: %v %q", err, h.r.commands())
	}
}

func TestUDPVerdictMapping(t *testing.T) {
	ok := func(exit string) udpPart { return udpPart{Ran: true, OK: true, Exit: exit} }
	failed := udpPart{Ran: true}
	for _, c := range []struct {
		name                  string
		server, proxy, direct udpPart
		udp                   bool
		verdict, message      string
	}{
		{"via", ok("5.5.5.5"), ok("5.5.5.5"), ok("1.1.1.1"), true, "UDP_VIA_BROray", "UDP идёт через BROray (выход 5.5.5.5)"},
		{"via without wan", ok("5.5.5.5"), ok("5.5.5.5"), failed, true, "UDP_VIA_BROray", "UDP идёт через BROray (выход 5.5.5.5)"},
		{"bypass", ok("5.5.5.5"), ok("1.1.1.1"), ok("1.1.1.1"), true, "BYPASS_WAN", "UDP идёт мимо VPN — через провайдера (1.1.1.1)"},
		{"proxy timeout", ok("5.5.5.5"), failed, ok("1.1.1.1"), true, "PROXY_UDP_FAILED", "Не удалось подтвердить UDP через Proxy0: STUN-проверки не получили ответа"},
		{"server", failed, udpPart{}, ok("1.1.1.1"), true, "SERVER_UDP_FAILED", "Не удалось подтвердить UDP через сервер: STUN-проверки через Xray SOCKS UDP не получили ответа"},
		{"same exits", ok("1.1.1.1"), ok("1.1.1.1"), ok("1.1.1.1"), true, "CHECK_FAILED", "Не удалось проверить: выход сервера 1.1.1.1 совпадает с адресом провайдера — пути не различить"},
		{"unexpected", ok("5.5.5.5"), ok("9.9.9.9"), ok("1.1.1.1"), true, "CHECK_FAILED", "Не удалось проверить: адрес выхода 9.9.9.9 не совпадает ни с выходом сервера 5.5.5.5, ни с адресом провайдера 1.1.1.1"},
		{"unexpected no wan", ok("5.5.5.5"), ok("9.9.9.9"), failed, true, "CHECK_FAILED", "Не удалось проверить: адрес выхода 9.9.9.9 не совпадает с выходом сервера 5.5.5.5, а адрес провайдера определить не удалось"},
		{"not run", ok("5.5.5.5"), udpPart{Error: "временный маршрут не появился в конфигурации KeeneticOS"}, ok("1.1.1.1"), true, "CHECK_FAILED", "Не удалось проверить: временный маршрут не появился в конфигурации KeeneticOS"},
	} {
		r := &udpCheckResult{InterfaceName: "Proxy0", Server: c.server, Proxy: c.proxy, Direct: c.direct, Socks5UDP: c.udp}
		udpVerdict(r)
		if r.Verdict != c.verdict || r.Message != c.message {
			t.Errorf("%s: %s %q", c.name, r.Verdict, r.Message)
		}
	}
}

func TestConfigRoutesParse(t *testing.T) {
	routes, complete := configRoutes([]string{
		"ip route 1.2.3.4 Proxy0 1200",                       // KeeneticOS short /32 form
		"ip route 10.0.0.0 255.0.0.0 192.168.1.5 Wireguard0", // gateway + interface
		"ip route 20.0.0.0 255.255.0.0 192.168.1.5 auto",     // gateway only
		"ip route 30.0.0.0 255.255.255.0 Proxy0 1200 auto",
		"interface Proxy0",
	})
	if !complete || len(routes) != 4 {
		t.Fatalf("%v %v", routes, complete)
	}
	a := func(s string) netip.Addr { return netip.MustParseAddr(s) }
	for _, c := range []struct{ ip, want string }{
		{"1.2.3.4", routeOwned}, {"1.2.3.5", routeFree}, {"10.9.9.9", routeForeign}, {"20.0.1.1", routeForeign}, {"30.0.0.7", routeOwned},
	} {
		if got := routeCoverage(routes, a(c.ip), "Proxy0"); got != c.want {
			t.Errorf("%s: %s, want %s", c.ip, got, c.want)
		}
	}
	// a static default route (backup WAN) does not block the temporary /32
	def, _ := configRoutes([]string{"ip route 0.0.0.0 0.0.0.0 192.168.2.1 ISP auto"})
	if got := routeCoverage(def, a("162.159.207.0"), "Proxy0"); got != routeFree {
		t.Errorf("default route: %s, want free", got)
	}
	wordDef, complete := configRoutes([]string{"ip route default 192.168.2.1 ISP auto"})
	if !complete || len(wordDef) != 1 || wordDef[0].prefix.String() != "0.0.0.0/0" {
		t.Fatalf("word default route was not parsed as 0/0: %v complete=%v", wordDef, complete)
	}
	if got := routeCoverage(wordDef, a("162.159.207.0"), "Proxy0"); got != routeFree {
		t.Errorf("word default route: %s, want free", got)
	}
	if !hostRoutePresent([]string{"ip route 1.2.3.4 255.255.255.255 Proxy0 1200"}, a("1.2.3.4"), "Proxy0") ||
		hostRoutePresent([]string{"ip route 1.2.3.0 255.255.255.0 Proxy0 1200"}, a("1.2.3.4"), "Proxy0") {
		t.Fatal("hostRoutePresent")
	}
	if _, complete := configRoutes([]string{"ip route 1.2.3.0 255.0.255.0 Proxy0"}); complete {
		t.Fatal("invalid mask accepted")
	}
}

// Host E2E: the server part through a real Xray SOCKS inbound (freedom from
// 127.0.0.3); the router's ProxyN is emulated by relaying routed datagrams
// through the same SOCKS (Keenetic ProxyN → SOCKS5 UDP → Xray).
func TestUDPCheckRealXraySOCKS(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN not set")
	}
	e := newUDPEnv(t)
	l, _ := net.Listen("tcp4", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cfg := fmt.Sprintf(`{"log":{"loglevel":"warning"},"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth","udp":true,"ip":"127.0.0.1"}}],
"outbounds":[{"protocol":"freedom","sendThrough":"127.0.0.3","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.0/8"]}]}}]}`, port)
	p := filepath.Join(t.TempDir(), "x.json")
	_ = os.WriteFile(p, []byte(cfg), 0o600)
	cmd := exec.Command(bin, "run", "-c", p)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	socks := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", socks); err == nil {
			c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// The ProxyN keeps its receipt endpoint; only the SOCKS inbound moves.
	_ = os.WriteFile(e.h.d.Root.XrayConfig(), []byte(fmt.Sprintf(`{"inbounds":[{"protocol":"socks","listen":"127.0.0.1","port":%d}]}`, port)), 0o600)
	STUNViaSOCKS = udpprobe.ViaSOCKS5
	STUNDirect = func(server string, timeout time.Duration) (*net.UDPAddr, error) {
		if hostRoutePresent(e.h.r.render(), netip.MustParseAddrPort(server).Addr(), "Proxy0") {
			return udpprobe.ViaSOCKS5(socks, server, timeout)
		}
		return udpprobe.Direct(server, timeout)
	}
	udpProbeTimeout, udpRoutedTimeout = 3*time.Second, 3*time.Second
	r := e.run(t)
	if r.Verdict != "UDP_VIA_BROray" || r.Server.Exit != "127.0.0.3" || r.Socks != socks {
		t.Fatalf("%+v", r)
	}
	t.Logf("%s | %s | %s | %s", r.Message, r.Server.Text, r.Proxy.Text, r.Direct.Text)
}

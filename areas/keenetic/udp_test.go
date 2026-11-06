package keenetic

import (
	"context"
	"strings"
	"testing"

	kn "github.com/BROadmin/broray/internal/keenetic"
)

// UDP through ProxyN (SOCKS5 UDP, KeeneticOS "proxy socks5-udp").
// RED before the fix, GREEN after; see docs/udp/KEENETIC-UDP-CONTRACT.md.

func udpOf(h *harness, name string) (running, startup bool) {
	for _, l := range h.r.render() {
		_ = l
	}
	rb, _ := extractBlock(h.r.render(), name)
	sb, _ := extractBlock(h.r.startup, name)
	return contains(rb, "    proxy socks5-udp"), contains(sb, "    proxy socks5-udp")
}

// RED-1: a new ProxyN is created with SOCKS5 UDP, protocol first.
func TestUDP1CreateEnablesSocks5UDP(t *testing.T) {
	h := newHarness(t)
	r := h.call("POST", "/api/keenetic/create.cgi")
	if !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	cmds := h.r.commands()
	iProto, iUDP := -1, -1
	for i, c := range cmds {
		switch c {
		case "interface Proxy0 proxy protocol socks5":
			iProto = i
		case "interface Proxy0 proxy socks5-udp":
			iUDP = i
		}
	}
	if iUDP < 0 || iProto < 0 || iUDP < iProto {
		t.Fatalf("socks5-udp must follow proxy protocol socks5: %q", cmds)
	}
	if run, start := udpOf(h, "Proxy0"); !run || !start {
		t.Fatalf("UDP running=%v startup=%v", run, start)
	}
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	if st.Data["checks"].(map[string]any)["socks5Udp"] != true || st.Data["actual"].(map[string]any)["socks5UdpEnabled"] != true ||
		st.Data["healthy"] != true || st.Data["matchesExpected"] != true {
		t.Fatalf("status %v", st.Data["checks"])
	}
}

// RED-2 / RED-7: the allowlists accept exactly "interface <ProxyN> proxy socks5-udp".
func TestUDP2Allowlist(t *testing.T) {
	ok := []string{"interface Proxy0 proxy socks5-udp", "interface Proxy255 proxy socks5-udp"}
	for _, c := range ok {
		if err := kn.Allow(kn.AreaInterface, c); err != nil {
			t.Errorf("allow %q: %v", c, err)
		}
	}
	if !stageAllowed("Proxy0", "socks5-udp", "interface Proxy0 proxy socks5-udp") ||
		!stageAllowed("Proxy0", "rollback-socks5-udp", "interface Proxy0 no proxy socks5-udp") {
		t.Error("stage grammar must accept the UDP stage and its rollback")
	}
	bad := []string{
		"interface Proxy0 proxy socks5-udp ; system reboot",
		"interface Proxy0 proxy socks5-udp;system reboot",
		"interface Wireguard0 proxy socks5-udp",
		"interface Proxy999 proxy socks5-udp",
		"interface Proxy0 proxy socks5-udp something",
		"interface Proxy0 proxy socks5-udp\nsystem reboot",
		"interface Proxy0  proxy socks5-udp",
		"interface proxy0 proxy socks5-udp",
		"interface Proxy00 proxy socks5-udp",
		"interface Proxy0 proxy socks5-UDP",
		"interface Proxy0 proxy socks5-udp ",
		"interface Proxy0 proxy udpgw-upstream 127.0.0.1 7300",
	}
	for _, c := range bad {
		if kn.Allow(kn.AreaInterface, c) == nil {
			t.Errorf("allowlist accepted %q", c)
		}
		if stageAllowed("Proxy0", "socks5-udp", c) || stageAllowed("Proxy0", "rollback-socks5-udp", c) {
			t.Errorf("stage grammar accepted %q", c)
		}
	}
	if stageAllowed("Proxy0", "socks5-udp", "interface Proxy1 proxy socks5-udp") {
		t.Error("stage grammar must bind the interface name")
	}
}

// makeTCPOnly turns the owned ProxyN into what BROray 3.x / dev1-dev3 created.
func makeTCPOnly(h *harness, name string) {
	h.r.mu.Lock()
	h.r.ifaces[name].udp = false
	h.r.mu.Unlock()
	h.r.Parse(context.Background(), []string{"system configuration save"})
	h.r.resetCommands()
}

// RED-3 / RED-6: an owned TCP-only ProxyN is not "fine": status names the
// problem, ownership stays confirmed (old receipt), repair adds UDP in place.
func TestUDP3RepairTCPOnlyOwned(t *testing.T) {
	h := newHarness(t)
	if r := h.call("POST", "/api/keenetic/create.cgi"); !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	rec0, _ := h.s.loadReceipt()
	makeTCPOnly(h, "Proxy0")
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	if st.Data["ownershipConfirmed"] != true {
		t.Fatal("an old receipt for a TCP-only ProxyN must stay valid")
	}
	if st.Data["checks"].(map[string]any)["socks5Udp"] != false || st.Data["matchesExpected"] != false ||
		st.Data["actual"].(map[string]any)["socks5UdpEnabled"] != false {
		t.Fatalf("TCP-only must not match: %v", st.Data["checks"])
	}
	codes := []string{}
	for _, x := range st.Data["health"].(map[string]any)["reasons"].([]any) {
		codes = append(codes, x.(map[string]any)["code"].(string))
	}
	if !contains(codes, "PROXY_SOCKS5_UDP_DISABLED") {
		t.Fatalf("reasons %v", codes)
	}
	r := h.call("POST", "/api/keenetic/repair.cgi")
	if !r.Success || r.Data["checks"].(map[string]any)["socks5Udp"] != true || r.Data["healthy"] != true {
		t.Fatalf("repair %+v %v", r.Error, r.Data["checks"])
	}
	got := strings.Join(h.r.commands(), "|")
	if got != "interface Proxy0 proxy socks5-udp|system configuration save" {
		t.Fatalf("repair must be minimal (no delete/recreate): %q", got)
	}
	rec1, _ := h.s.loadReceipt()
	if rec1.RunningBlockSHA256 != rec0.RunningBlockSHA256 {
		t.Fatal("UDP must not change the receipt block SHA (backward compatible receipts)")
	}
	if run, start := udpOf(h, "Proxy0"); !run || !start {
		t.Fatal("UDP must be in running and startup")
	}
}

// RED-4: startup reconcile restores a missing UDP mode.
func TestUDP4ReconcileRestoresUDP(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.s.EnsureInterface(ctx); err != nil {
		t.Fatal(err)
	}
	makeTCPOnly(h, "Proxy0")
	op := h.d.Queue.StartOperation("", "router_operation", "keenetic:reconcile", "SYSTEM_RECOVERY", "cooperative", "")
	if s, m, err := h.s.reconcileOnce(ctx, op); err != nil || s != "success" {
		t.Fatalf("%s %s %v", s, m, err)
	}
	if run, start := udpOf(h, "Proxy0"); !run || !start {
		t.Fatalf("reconcile must restore UDP: %q", h.r.commands())
	}
	// EnsureInterface (server activation) also restores it
	makeTCPOnly(h, "Proxy0")
	if err := h.s.EnsureInterface(ctx); err != nil {
		t.Fatal(err)
	}
	if run, _ := udpOf(h, "Proxy0"); !run {
		t.Fatal("EnsureInterface must restore UDP")
	}
}

// RED-5: failure injection around the UDP stage.
func TestUDP5Rollback(t *testing.T) {
	// create: UDP rejected → nothing partial is left, no receipt
	h := newHarness(t)
	h.r.reject["interface Proxy0 proxy socks5-udp"] = true
	r := h.call("POST", "/api/keenetic/create.cgi")
	h.expectErr(r, 500, "KEENETIC_ACTION_FAILED")
	if len(h.r.ifaces) != 0 {
		t.Fatalf("partial ProxyN left: %v", h.r.ifaces)
	}
	if _, ok := h.s.loadReceipt(); ok {
		t.Fatal("no receipt after a failed create")
	}

	// create: save after UDP fails → removed
	h = newHarness(t)
	h.r.noSave = true
	h.expectErr(h.call("POST", "/api/keenetic/create.cgi"), 500, "KEENETIC_ACTION_FAILED")
	h.r.noSave = false
	h.r.mu.Lock()
	n := len(h.r.ifaces)
	h.r.mu.Unlock()
	if n != 0 {
		t.Fatal("partial ProxyN left after a failed save")
	}

	// repair of TCP-only: UDP rejected → the previous TCP-only state stays owned and working
	h = newHarness(t)
	if r := h.call("POST", "/api/keenetic/create.cgi"); !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	makeTCPOnly(h, "Proxy0")
	h.r.reject["interface Proxy0 proxy socks5-udp"] = true
	r = h.call("POST", "/api/keenetic/repair.cgi")
	h.expectErr(r, 500, "KEENETIC_ACTION_FAILED")
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	if st.Data["exists"] != true || st.Data["ownershipConfirmed"] != true || st.Data["healthy"] != true {
		t.Fatalf("after failed UDP repair %v", st.Data)
	}
	for _, c := range h.r.commands() {
		if strings.HasPrefix(c, "no interface") {
			t.Fatalf("a failed UDP repair must not delete the interface: %q", h.r.commands())
		}
	}

	// repair of TCP-only: save after UDP fails → rolled back to TCP-only (no half state)
	h = newHarness(t)
	if r := h.call("POST", "/api/keenetic/create.cgi"); !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	makeTCPOnly(h, "Proxy0")
	h.r.noSave = true
	h.expectErr(h.call("POST", "/api/keenetic/repair.cgi"), 500, "KEENETIC_ACTION_FAILED")
	h.r.noSave = false
	run, start := udpOf(h, "Proxy0")
	if run != start {
		t.Fatalf("running/startup UDP diverge after rollback: running=%v startup=%v", run, start)
	}
}

// A foreign ProxyN is never given UDP.
func TestUDPForeignUntouched(t *testing.T) {
	h := newHarness(t)
	h.r.ifaces["Proxy0"] = &fakeIface{desc: `    description "Other"`, security: true, protocol: "socks5", upstream: "192.168.1.1 2080", up: true}
	h.r.Parse(context.Background(), []string{"system configuration save"})
	h.r.resetCommands()
	_ = h.call("POST", "/api/keenetic/repair.cgi")
	for _, c := range h.r.commands() {
		if strings.Contains(c, "Proxy0") {
			t.Fatalf("foreign Proxy0 touched: %s", c)
		}
	}
}

// Grammar: the UDP line is accepted only once, after the protocol line.
func TestUDPGrammar(t *testing.T) {
	base := []string{"interface Proxy3", `    description "BROray - NL"`, "    security-level public", "    proxy protocol socks5"}
	withUDP := append(append([]string{}, base...), "    proxy socks5-udp", "    proxy upstream 192.168.1.1 2080", "    up")
	afterUp := append(append([]string{}, base...), "    proxy upstream 192.168.1.1 2080", "    proxy socks5-udp", "    up")
	plain := append(append([]string{}, base...), "    proxy upstream 192.168.1.1 2080", "    up")
	for _, b := range [][]string{withUDP, afterUp, plain} {
		if !signatureExact(b, "Proxy3", "192.168.1.1", 2080, "BROray - NL") {
			t.Fatalf("rejected %q", b)
		}
	}
	a, _ := blockSHA256(withUDP)
	p, _ := blockSHA256(plain)
	if a != p {
		t.Fatal("the UDP line must not change the block SHA")
	}
	twice := append(append([]string{}, base...), "    proxy socks5-udp", "    proxy socks5-udp", "    proxy upstream 192.168.1.1 2080", "    up")
	before := []string{"interface Proxy3", `    description "BROray - NL"`, "    security-level public", "    proxy socks5-udp", "    proxy protocol socks5",
		"    proxy upstream 192.168.1.1 2080", "    up"}
	other := append(append([]string{}, base...), "    proxy socks5-udp enable", "    proxy upstream 192.168.1.1 2080", "    up")
	for _, b := range [][]string{twice, before, other} {
		if signatureExact(b, "Proxy3", "192.168.1.1", 2080, "BROray - NL") {
			t.Fatalf("accepted %q", b)
		}
	}
	if !blockUDP(withUDP) || blockUDP(plain) {
		t.Fatal("blockUDP")
	}
}

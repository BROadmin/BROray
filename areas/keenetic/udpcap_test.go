package keenetic

import (
	"context"
	"strings"
	"testing"

	kn "github.com/BROadmin/broray/internal/keenetic"
)

// KeeneticOS before 4.1 rejects "proxy socks5-udp": BROray keeps ProxyN
// TCP-only with a distinct warning instead of failing create/repair.

func oldFirmware(h *harness, release string) {
	h.r.mu.Lock()
	h.r.noUDP = true
	if h.r.rci == nil {
		h.r.rci = map[string]string{}
	}
	h.r.rci["show/version"] = `{"release":"` + release + `"}`
	h.r.mu.Unlock()
}

func reasonCodes(st resp) []string {
	codes := []string{}
	for _, x := range st.Data["health"].(map[string]any)["reasons"].([]any) {
		codes = append(codes, x.(map[string]any)["code"].(string))
	}
	return codes
}

func hasCmd(cmds []string, sub string) bool {
	for _, c := range cmds {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func TestUDPUnsupportedClassifier(t *testing.T) {
	yes := [][]kn.Status{
		{{Status: "error", Code: "7405600", Ident: "Command::Base", Message: "no such command: socks5-udp"}},
		{{Status: "error", Message: "Command::Base error[7405600]: no such command: socks5-udp."}},
		{{Status: "error", Message: "% Unknown command"}},
	}
	no := [][]kn.Status{
		nil,
		{{Status: "error", Message: "rejected"}},
		{{Status: "error", Message: "proxy protocol is not socks5"}},
		{{Status: "error", Code: "7405602", Message: "Command::Base error[7405602]: argument parse error."}},
		{{Status: "message", Message: "no such command"}}, // not an error status
	}
	for _, st := range yes {
		if !unsupportedCommand(st) {
			t.Errorf("not classified as unsupported: %v", st)
		}
	}
	for _, st := range no {
		if unsupportedCommand(st) {
			t.Errorf("classified as unsupported: %v", st)
		}
	}
}

func TestUDPUnsupportedCreateKeepsTCPOnly(t *testing.T) {
	h := newHarness(t)
	oldFirmware(h, "4.0.5")
	r := h.call("POST", "/api/keenetic/create.cgi")
	if !r.Success {
		t.Fatalf("create on KeeneticOS 4.0 must succeed TCP-only: %+v", r.Error)
	}
	if run, start := udpOf(h, "Proxy0"); run || start {
		t.Fatal("UDP line on a firmware without it")
	}
	if _, ok := h.s.loadReceipt(); !ok {
		t.Fatal("no receipt")
	}
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	codes := reasonCodes(st)
	if !contains(codes, "PROXY_SOCKS5_UDP_UNSUPPORTED") || contains(codes, "PROXY_SOCKS5_UDP_DISABLED") {
		t.Fatalf("reasons %v", codes)
	}
	health := st.Data["health"].(map[string]any)
	if st.Data["matchesExpected"] != true || st.Data["healthy"] != true || st.Data["socks5UdpSupported"] != false ||
		health["severity"] != "warning" || health["actionRequired"] != false || st.Data["checks"].(map[string]any)["socks5Udp"] != nil {
		t.Fatalf("status %v %v", st.Data["matchesExpected"], health)
	}
	found := false
	for _, p := range st.Data["problems"].([]any) {
		found = found || p == udpUnsupportedMsg
	}
	if !found {
		t.Fatalf("problems %v", st.Data["problems"])
	}
	// repair: nothing to do, no socks5-udp attempt
	h.r.resetCommands()
	if r := h.call("POST", "/api/keenetic/repair.cgi"); !r.Success {
		t.Fatalf("repair %+v", r.Error)
	}
	if hasCmd(h.r.commands(), "socks5-udp") {
		t.Fatalf("repair retried socks5-udp: %q", h.r.commands())
	}
	// reconcile: success, no repair loop
	op := h.d.Queue.StartOperation("", "router_operation", "keenetic:reconcile", "SYSTEM_RECOVERY", "cooperative", "")
	if s, m, err := h.s.reconcileOnce(context.Background(), op); err != nil || s != "success" {
		t.Fatalf("reconcile %s %s %v", s, m, err)
	}
	if hasCmd(h.r.commands(), "socks5-udp") || hasCmd(h.r.commands(), "configuration save") {
		t.Fatalf("reconcile repaired: %q", h.r.commands())
	}
}

func TestUDPUnsupportedRepairAndRollback(t *testing.T) {
	// an owned TCP-only ProxyN (3.x) on old firmware: repair is not a failure
	h := newHarness(t)
	if r := h.call("POST", "/api/keenetic/create.cgi"); !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	makeTCPOnly(h, "Proxy0")
	oldFirmware(h, "4.0.5")
	if r := h.call("POST", "/api/keenetic/repair.cgi"); !r.Success {
		t.Fatalf("repair %+v", r.Error)
	}
	if !h.s.udpUnsupported(context.Background()) {
		t.Fatal("capability not recorded")
	}
	// a failed upstream change is rolled back without "no proxy socks5-udp"
	h.setSocks("192.168.1.1", 2081)
	h.r.resetCommands()
	h.r.noSave = true
	h.expectErr(h.call("POST", "/api/keenetic/repair.cgi"), 500, "KEENETIC_ACTION_FAILED")
	h.r.noSave = false
	if hasCmd(h.r.commands(), "socks5-udp") {
		t.Fatalf("rollback sent a socks5-udp command: %q", h.r.commands())
	}
	if !hasCmd(h.r.commands(), "proxy upstream 192.168.1.1 2080") {
		t.Fatalf("no rollback: %q", h.r.commands())
	}
}

func TestUDPUnsupportedReprobedAfterFirmwareChange(t *testing.T) {
	h := newHarness(t)
	oldFirmware(h, "4.0.5")
	if r := h.call("POST", "/api/keenetic/create.cgi"); !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	// firmware update (reboot → new daemon run): the record no longer applies
	h.r.mu.Lock()
	h.r.noUDP = false
	h.r.rci["show/version"] = `{"release":"4.1.2"}`
	h.r.mu.Unlock()
	h.s = newService(h.d)
	current.Store(h.s)
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	if codes := reasonCodes(st); !contains(codes, "PROXY_SOCKS5_UDP_DISABLED") || st.Data["socks5UdpSupported"] != true {
		t.Fatalf("after a firmware change: %v", codes)
	}
	op := h.d.Queue.StartOperation("", "router_operation", "keenetic:reconcile", "SYSTEM_RECOVERY", "cooperative", "")
	if s, m, err := h.s.reconcileOnce(context.Background(), op); err != nil || s != "success" {
		t.Fatalf("reconcile %s %s %v", s, m, err)
	}
	if run, start := udpOf(h, "Proxy0"); !run || !start {
		t.Fatalf("UDP not enabled after the firmware update: %q", h.r.commands())
	}
	if h.s.udpUnsupported(context.Background()) {
		t.Fatal("stale record kept")
	}
}

func TestUDPUnsupportedUnknownFirmwareIsPerRun(t *testing.T) {
	h := newHarness(t)
	h.r.noUDP = true // no "show version" answer
	if r := h.call("POST", "/api/keenetic/create.cgi"); !r.Success {
		t.Fatalf("create %+v", r.Error)
	}
	if !h.s.udpUnsupported(context.Background()) {
		t.Fatal("not recorded for this run")
	}
	s2 := newService(h.d) // daemon restart
	if s2.udpUnsupported(context.Background()) {
		t.Fatal("a record without a firmware release must be re-probed on daemon start")
	}
}

package keenetic

import (
	"context"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestBlockGrammar(t *testing.T) {
	block := []string{"interface Proxy3", `    description "BROray - \xd0\x9d\xd0\xbb"`, "    security-level public", "    ip global 65000",
		"    proxy protocol socks5", "    proxy upstream 192.168.1.1 2080", "    proxy connect via ISP", "    up"}
	if !signatureExact(block, "Proxy3", "192.168.1.1", 2080, "BROray - Нл") {
		t.Fatal("exact signature with ip global, binding and hex description")
	}
	plain := []string{"interface Proxy3", `    description "BROray - Нл"`, "    security-level public", "    proxy protocol socks5",
		"    proxy upstream 192.168.1.1 2080", "    up"}
	a, _ := blockSHA256(block)
	b, _ := blockSHA256(append([]string{plain[0], block[1]}, plain[2:]...))
	if a != b {
		t.Fatal("ip global / proxy connect via must not change the SHA")
	}
	for _, bad := range [][]string{
		append(append([]string{}, plain...), "    up"),
		{plain[0], plain[1], plain[3], plain[2], plain[4], plain[5]},
		{plain[0], `    description "Other"`, plain[2], plain[3], plain[4], plain[5]},
	} {
		if signatureExact(bad, "Proxy3", "192.168.1.1", 2080, "BROray - Нл") {
			t.Fatalf("accepted %v", bad)
		}
	}
	cfg := []string{"interface Proxy3", "    up", "interface Proxy4", "!"}
	if _, rc := extractBlock(cfg, "Proxy3"); rc != blockMalformed {
		t.Fatal("nested interface must be malformed")
	}
	if _, rc := extractBlock(cfg, "Proxy9"); rc != blockAbsent {
		t.Fatal("absent")
	}
	if !validDescription("BROray") || validDescription("BROray - a;b") || validDescription("BROrayX") || validDescription("BROray - a>") {
		t.Fatal("description rules")
	}
	if normalizeDescription("BROray — NL") != normalizeDescription("BROray - NL") {
		t.Fatal("dash normalisation")
	}
}

func TestStatusMissingAndNotReady(t *testing.T) {
	h := newHarness(t)
	r := h.call("GET", "/api/keenetic/status.cgi?force=1")
	if !r.Success || r.Data["exists"] != false || r.Data["expectedReady"] != true || r.Data["interfaceName"] != "Proxy0" {
		t.Fatalf("%v", r.Data)
	}
	health := r.Data["health"].(map[string]any)
	if health["severity"] != "error" || health["reasons"].([]any)[0].(map[string]any)["code"] != "PROXY0_MISSING" {
		t.Fatal(health)
	}
	if p := r.Data["problems"].([]any); len(p) != 1 || p[0] != "Интерфейс BROray - Netherlands не создан." {
		t.Fatalf("problems %v", p)
	}
	h.setServer("", "")
	r = h.call("POST", "/api/keenetic/create.cgi")
	h.expectErr(r, 409, "KEENETIC_EXPECTED_STATE_UNAVAILABLE")
	if r.Error.Details != "Невозможно настроить Proxy0 без SOCKS Xray и активного сервера." {
		t.Fatal(r.Error.Details)
	}
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	if !contains(toStrings(st.Data["problems"]), "Интерфейс  не создан.") {
		t.Fatalf("3.x double-space text expected: %v", st.Data["problems"])
	}
	h.expectErr(h.call("GET", "/api/keenetic/create.cgi"), 405, "METHOD_NOT_ALLOWED")
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestCreateSyncRepairDelete(t *testing.T) {
	h := newHarness(t)
	r := h.call("POST", "/api/keenetic/create.cgi")
	if !r.Success || r.Data["healthy"] != true || r.Data["ownershipConfirmed"] != true || r.Data["matchesExpected"] != true {
		t.Fatalf("create %+v %v", r.Error, r.Data)
	}
	want := []string{"interface Proxy0", "interface Proxy0 proxy protocol socks5", "interface Proxy0 proxy socks5-udp", "interface Proxy0 proxy upstream 192.168.1.1 2080",
		"interface Proxy0 proxy connect", `interface Proxy0 description "BROray - Netherlands"`, "interface Proxy0 security-level public",
		"interface Proxy0 up", "system configuration save"}
	if got := h.r.commands(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("commands %q", got)
	}
	rec, ok := h.s.loadReceipt()
	if !ok || rec.SchemaVersion != 2 || rec.Contract != ownerContract || rec.SelectionMode != "created" || rec.Upstream.Port != 2080 ||
		len(rec.RunningBlockSHA256) != 64 || rec.WriteProtocolSHA256 != writePolicySHA256 {
		t.Fatalf("receipt %+v", rec)
	}
	if h.call("GET", "/api/keenetic/status.cgi?force=1").Data["health"].(map[string]any)["severity"] != "ok" {
		t.Fatal("status must be ok")
	}
	r = h.call("POST", "/api/keenetic/create.cgi")
	h.expectErr(r, 500, "KEENETIC_ACTION_FAILED")
	if !strings.Contains(r.Error.Details.(string), "уже существует") {
		t.Fatal(r.Error.Details)
	}

	// server renamed → sync-description
	h.setServer("de", "Germany")
	st := h.call("GET", "/api/keenetic/status.cgi?force=1")
	if st.Data["checks"].(map[string]any)["description"] != false {
		t.Fatal("description drift expected")
	}
	h.r.resetCommands()
	r = h.call("POST", "/api/keenetic/sync-description.cgi")
	if !r.Success || r.Data["description"] != "BROray - Germany" {
		t.Fatalf("sync %+v", r)
	}
	if got := h.r.commands(); strings.Join(got, "|") != `interface Proxy0 description "BROray - Germany"|system configuration save` {
		t.Fatalf("sync commands %q", got)
	}

	// SOCKS endpoint moved → ownership stays, repair moves the upstream
	h.setSocks("192.168.1.1", 2081)
	st = h.call("GET", "/api/keenetic/status.cgi?force=1")
	if st.Data["ownershipConfirmed"] != true || st.Data["checks"].(map[string]any)["upstream"] != false {
		t.Fatalf("after socks change %v", st.Data["checks"])
	}
	h.r.resetCommands()
	r = h.call("POST", "/api/keenetic/repair.cgi")
	if !r.Success || r.Data["upstream"] != "192.168.1.1:2081" || r.Data["healthy"] != true {
		t.Fatalf("repair %+v %v", r.Error, r.Data["upstream"])
	}
	if got := h.r.commands(); strings.Join(got, "|") != "interface Proxy0 proxy upstream 192.168.1.1 2081|system configuration save" {
		t.Fatalf("repair commands %q", got)
	}
	if rec, _ := h.s.loadReceipt(); rec.Upstream.Port != 2081 || rec.SelectionMode != "repaired" {
		t.Fatalf("receipt %+v", rec)
	}

	r = h.call("POST", "/api/keenetic/delete.cgi")
	if !r.Success || r.Data["exists"] != false || r.Data["ownershipReceiptPresent"] != true {
		t.Fatalf("delete %+v %v", r.Error, r.Data)
	}
	h.expectErr(h.call("POST", "/api/keenetic/delete.cgi"), 500, "KEENETIC_ACTION_FAILED")
}

func TestForeignAndAdoption(t *testing.T) {
	h := newHarness(t)
	// a foreign proxy that loosely looks like BROray stays untouched
	h.r.ifaces["Proxy0"] = &fakeIface{desc: `    description "BROray - Old"`, security: true, protocol: "socks5", upstream: "192.168.1.1 2080", up: true,
		extra: []string{"    ip mtu 1400"}}
	h.r.Parse(context.Background(), []string{"system configuration save"})
	h.r.resetCommands()
	r := h.call("POST", "/api/keenetic/create.cgi")
	if !r.Success || r.Data["interfaceName"] != "Proxy1" {
		t.Fatalf("%+v %v", r.Error, r.Data["interfaceName"])
	}
	for _, c := range h.r.commands() {
		if strings.Contains(c, "Proxy0") {
			t.Fatalf("foreign interface touched: %s", c)
		}
	}

	// an exact BROray block without receipt is adopted
	h2 := newHarness(t)
	h2.r.ifaces["Proxy2"] = &fakeIface{desc: `    description "BROray - Old"`, security: true, protocol: "socks5", upstream: "192.168.1.1 2080", up: true}
	h2.r.Parse(context.Background(), []string{"system configuration save"})
	h2.r.resetCommands()
	r = h2.call("POST", "/api/keenetic/repair.cgi")
	if !r.Success || r.Data["interfaceName"] != "Proxy2" || r.Data["description"] != "BROray - Netherlands" {
		t.Fatalf("%+v %v", r.Error, r.Data)
	}
	if rec, _ := h2.s.loadReceipt(); rec.InterfaceName != "Proxy2" {
		t.Fatal("receipt")
	}
}

func TestCreateRollbackAndBusy(t *testing.T) {
	h := newHarness(t)
	h.r.connected = false
	r := h.call("POST", "/api/keenetic/create.cgi")
	h.expectErr(r, 500, "KEENETIC_ACTION_FAILED")
	if !strings.Contains(r.Error.Details.(string), "PROXY_CREATE_CONVERGENCE_TIMEOUT") || len(h.r.ifaces) != 0 {
		t.Fatalf("%v %v", r.Error.Details, h.r.ifaces)
	}
	if _, ok := h.s.loadReceipt(); ok {
		t.Fatal("no receipt after a failed create")
	}
	rel, _ := h.d.Lock.TryAcquire("routes", "export", "x")
	h.expectErr(h.call("POST", "/api/keenetic/create.cgi"), 409, "OPERATION_BUSY")
	h.expectErr(h.call("POST", "/api/keenetic/web-access-enable.cgi"), 409, "OPERATION_BUSY")
	rel()
}

func TestEnsureInterfaceUninstallReconcile(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.s.EnsureInterface(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.s.EnsureInterface(ctx); err != nil {
		t.Fatal("idempotent:", err)
	}
	if st, _ := h.s.status(ctx); st["healthy"] != true {
		t.Fatal(st["problems"])
	}
	if s, m, err := h.s.reconcileOnce(ctx, h.d.Queue.StartOperation("", "router_operation", "keenetic:reconcile", "SYSTEM_RECOVERY", "cooperative", "")); err != nil || s != "success" {
		t.Fatalf("%s %s %v", s, m, err)
	}
	if err := h.s.uninstallInterface(ctx); err != nil || len(h.r.ifaces) != 0 {
		t.Fatalf("%v %v", err, h.r.ifaces)
	}
	if err := h.s.uninstallInterface(ctx); err != nil {
		t.Fatal("nothing owned → nil:", err)
	}
}

func TestWebAccess(t *testing.T) {
	h := newHarness(t)
	r := h.call("GET", "/api/keenetic/web-access-status.cgi")
	if !r.Success || r.Data["state"] != "disabled" || r.Data["canEnable"] != true || r.Data["address"] != "https://broray.myrouter.keenetic.pro/" ||
		r.Data["localAddress"] != "http://192.168.1.1:8080/" {
		t.Fatalf("%v", r.Data)
	}
	r = h.call("POST", "/api/keenetic/web-access-enable.cgi")
	if !r.Success || r.Data["state"] != "enabled" || r.Data["consistent"] != true {
		t.Fatalf("%+v %v", r.Error, r.Data)
	}
	if o, ok := h.s.keenDNSOrigin(context.Background()); !ok || o != "https://broray.myrouter.keenetic.pro" {
		t.Fatalf("origin %q %v", o, ok)
	}
	// a foreign change makes it a conflict; disable refuses
	h.r.web = append(h.r.web, "foreign line")
	r = h.call("GET", "/api/keenetic/web-access-status.cgi")
	if r.Data["state"] != "conflict" {
		t.Fatal(r.Data["state"])
	}
	r = h.call("POST", "/api/keenetic/web-access-disable.cgi")
	h.expectErr(r, 500, "WEB_PUBLISH_DELETE_AUTHORITY_REFUSED")
	if err := h.s.uninstallWeb(context.Background()); err != nil || h.r.web == nil {
		t.Fatal("uninstall must leave a foreign object alone")
	}
	h.r.web = h.r.web[:len(h.r.web)-1]
	r = h.call("POST", "/api/keenetic/web-access-disable.cgi")
	if !r.Success || r.Data["state"] != "disabled" || h.r.web != nil {
		t.Fatalf("%+v %v", r.Error, r.Data)
	}
	// receipt without live object
	_ = compat.WriteJSON(h.s.webReceiptPath(), map[string]any{"schemaVersion": 3})
	h.expectErr(h.call("POST", "/api/keenetic/web-access-enable.cgi"), 409, "WEB_PUBLISH_RECEIPT_INVALID")
}

func TestWebEnableRollbackMarksRecovery(t *testing.T) {
	h := newHarness(t)
	h.r.noSave = true
	r := h.call("POST", "/api/keenetic/web-access-enable.cgi")
	// rollback also needs a save → fails → last line wins
	h.expectErr(r, 500, "WEB_PUBLISH_ROLLBACK_FAILED")
	st := h.call("GET", "/api/keenetic/web-access-status.cgi")
	if st.Data["state"] != "recovery" {
		t.Fatal(st.Data["state"])
	}
}

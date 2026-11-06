package dns

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func TestParseRunningStrict(t *testing.T) {
	lines := []string{
		"dns-proxy tls upstream 1.1.1.1 853 sni cloudflare-dns.com",
		"dns-proxy tls upstream 9.9.9.9 sni dns.quad9.net",
		"dns-proxy https upstream https://dns.google/dns-query",
		"interface Bridge0",
		"    tls upstream 8.8.8.8 853 sni dns.google", // not inside dns-proxy
		"!",
		"dns-proxy",
		"    tls upstream 8.8.4.4 853 sni dns.google on ISP",
		"    tls upstream 77.88.8.8 853 sni x bogus y",
		"    https upstream https://x/dns-query",
		"    tls upstream 1.1.1.1 sni other.example",
		"!",
	}
	o := parseRunning(lines)
	if len(o.Dot) != 5 || o.DohCount != 2 || o.TotalSecure != 7 {
		t.Fatalf("records %d doh %d total %d", len(o.Dot), o.DohCount, o.TotalSecure)
	}
	if o.Dot[1].PortState != "omitted" || o.Dot[1].EffectivePort != 853 || o.Dot[1].PortRaw != "" {
		t.Fatalf("omitted port: %+v", o.Dot[1])
	}
	if o.Dot[2].Interface != "ISP" || o.Dot[2].On != "ISP" || o.Dot[2].ExtraAttributeCount != 1 {
		t.Fatalf("on: %+v", o.Dot[2])
	}
	if o.Dot[3].Valid || o.Dot[3].UnknownTokenCount != 2 {
		t.Fatalf("unknown tokens: %+v", o.Dot[3])
	}
	if o.SelectorCollisionCount != 1 || o.Determinate {
		t.Fatalf("collision %d det %v", o.SelectorCollisionCount, o.Determinate)
	}
}

func TestParseRuntimeSystemOnly(t *testing.T) {
	lines := []string{
		"proxy-status:", "    proxy-name: System", "    proxy-tls:",
		"        server-tls:", "            address: 1.1.1.1", "            port: 853", "            sni: cloudflare-dns.com",
		"proxy-status:", "    proxy-name: Policy0", "    proxy-tls:",
		"        server-tls:", "            address: 8.8.8.8", "            sni: dns.google",
	}
	r := parseRuntime(lines)
	if !r.Determinate || len(r.Dot) != 1 || r.SystemProxyTLSSections != 1 || r.PolicyProxyTLSSections != 1 {
		t.Fatalf("%+v", r)
	}
	r = parseRuntime(append(lines[:7:7], "            weird: 1"))
	if r.Determinate || r.UnknownSchemaCount != 1 {
		t.Fatalf("unknown schema must break determinacy: %+v", r)
	}
}

func TestRenderRCI(t *testing.T) {
	raw := []byte(`{"proxy-status":[{"proxy-name":"System","proxy-tls":{"server-tls":[{"address":"1.1.1.1","port":"853","sni":"cloudflare-dns.com"}]}}]}`)
	lines, err := renderRCI(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := parseRuntime(lines)
	if !r.Determinate || len(r.Dot) != 1 || r.Dot[0].SNI != "cloudflare-dns.com" {
		t.Fatalf("%v → %+v", lines, r)
	}
}

func TestStatusDefaultsAndCache(t *testing.T) {
	h := newHarness(t)
	r := h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil)
	if !r.Success || r.Status != 200 {
		t.Fatalf("%+v", r)
	}
	if r.Data["configurationState"] != "test-required" || r.Data["selectedCount"].(float64) != 3 {
		t.Fatalf("state %v", r.Data["configurationState"])
	}
	health := r.Data["health"].(map[string]any)
	reasons := health["reasons"].([]any)
	if health["severity"] != "warning" || reasons[0].(map[string]any)["message"] != "Проверено 0 из 3 выбранных серверов." {
		t.Fatalf("health %v", health)
	}
	if _, ok := r.Data["autoCheck"].(map[string]any); !ok {
		t.Fatal("autoCheck missing")
	}
	servers := r.Data["servers"].([]any)
	if len(servers) != 8 || servers[0].(map[string]any)["id"] != "google-primary" {
		t.Fatal("catalog order")
	}
	// cached read writes the cache file
	r = h.call("GET", "/api/routes/dot-status.cgi", nil, nil)
	if !r.Success {
		t.Fatal(r)
	}
	if _, err := os.Stat(h.s.cachePath()); err != nil {
		t.Fatal("cache not written")
	}
	// router unavailable: still 200
	h.k.failRead = true
	r = h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil)
	if !r.Success || r.Data["runningConfigAvailable"] != false || r.Data["mutationBlockedReason"] != "keenetic-unavailable" {
		t.Fatalf("%+v", r.Data["mutationBlockedReason"])
	}
}

func TestMethodAndBodyValidation(t *testing.T) {
	h := newHarness(t)
	h.expectErr(h.call("GET", "/api/routes/dot-test.cgi", nil, nil), 405, "METHOD_NOT_ALLOWED")
	h.expectErr(h.call("POST", "/api/routes/dot-status.cgi", nil, nil), 405, "METHOD_NOT_ALLOWED")
	h.expectErr(h.call("POST", "/api/routes/dot-test.cgi", "", nil), 400, "REQUEST_BODY_REQUIRED")
	h.expectErr(h.call("POST", "/api/routes/dot-test.cgi", "[1]", nil), 400, "REQUEST_JSON_INVALID")
	h.expectErr(h.call("POST", "/api/routes/dot-test.cgi", strings.Repeat(" ", 70000), nil), 413, "REQUEST_TOO_LARGE")
	for _, body := range []string{`{"serverIds":[]}`, `{"serverIds":["nope"]}`, `{"serverIds":["quad9","quad9"]}`,
		`{"serverIds":["quad9"],"allowUntested":true}`, `{"serverIds":"quad9"}`} {
		r := h.call("POST", "/api/routes/dot-test.cgi", body, nil)
		h.expectErr(r, 400, "REQUEST_INVALID")
		if r.Error.Message != "Выбран некорректный список DNS-over-TLS серверов." {
			t.Fatal(r.Error.Message)
		}
	}
	rel, _ := h.d.Lock.TryAcquire("routes", "export", "x")
	r := h.call("POST", "/api/routes/dot-test.cgi", `{"serverIds":["quad9"]}`, nil)
	h.expectErr(r, 409, "ROUTES_OPERATION_BUSY")
	if r.Error.Message != "Другая конфликтующая операция уже выполняется." {
		t.Fatal(r.Error.Message)
	}
	h.expectErr(h.call("POST", "/api/routes/dot-delete-preview.cgi", nil, nil), 409, "ROUTES_OPERATION_BUSY")
	rel()
}

func TestTestApplyDeleteFlow(t *testing.T) {
	h := newHarness(t)
	h.k.dot = []string{"dns-proxy tls upstream 208.67.222.222 sni dns.opendns.com"} // external, kept
	ids := map[string]any{"serverIds": []string{"cloudflare-primary", "quad9"}, "allowUntested": false}

	h.expectErr(h.call("POST", "/api/routes/dot-apply.cgi", ids, nil), 409, "DOT_TEST_REQUIRED")

	r := h.call("POST", "/api/routes/dot-test.cgi", ids, nil)
	if !r.Success || r.Data["selectedTestedCount"].(float64) != 2 {
		t.Fatalf("%+v %v", r.Error, r.Data["selectedTestedCount"])
	}
	if sel := r.Data["selectedIds"].([]any); len(sel) != 2 {
		t.Fatal("test must commit the selection")
	}
	if exists(h.s.markerPath()) {
		t.Fatal("marker left after test")
	}

	r = h.call("POST", "/api/routes/dot-apply.cgi", ids, nil)
	if !r.Success {
		t.Fatalf("apply: %+v", r.Error)
	}
	want := []string{"dns-proxy tls upstream 1.1.1.1 853 sni cloudflare-dns.com", "dns-proxy tls upstream 9.9.9.9 853 sni dns.quad9.net", "system configuration save"}
	if got := h.k.cmds(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("commands %v", got)
	}
	if r.Data["configurationState"] != "matches-selection" || len(r.Data["managed"].([]any)) != 2 || r.Data["externalDotCount"].(float64) != 1 {
		t.Fatalf("after apply %v managed %v", r.Data["configurationState"], r.Data["managed"])
	}
	if r.Data["health"].(map[string]any)["severity"] != "ok" {
		t.Fatalf("health %v", r.Data["health"])
	}

	// delete with a stale fingerprint
	pv := h.call("POST", "/api/routes/dot-delete-preview.cgi", nil, nil)
	if !pv.Success || len(pv.Data["entries"].([]any)) != 2 || len(pv.Data["expectedFingerprint"].(string)) != 64 {
		t.Fatalf("preview %+v", pv)
	}
	bad := map[string]any{"schemaVersion": 1, "serverIds": pv.Data["serverIds"], "expectedFingerprint": strings.Repeat("0", 64)}
	h.expectErr(h.call("POST", "/api/routes/dot-delete.cgi", bad, nil), 409, "DOT_DELETE_CONFIRMATION_STALE")
	h.expectErr(h.call("POST", "/api/routes/dot-delete.cgi", map[string]any{"serverIds": pv.Data["serverIds"]}, nil), 400, "REQUEST_INVALID")
	r = h.call("POST", "/api/routes/dot-delete.cgi", map[string]any{"schemaVersion": 1, "serverIds": pv.Data["serverIds"], "expectedFingerprint": "XYZ"}, nil)
	h.expectErr(r, 400, "REQUEST_INVALID")
	if r.Error.Message != "Контрольная сумма подтверждения удаления некорректна." {
		t.Fatal(r.Error.Message)
	}
	good := map[string]any{"schemaVersion": 1, "serverIds": pv.Data["serverIds"], "expectedFingerprint": pv.Data["expectedFingerprint"]}
	r = h.call("POST", "/api/routes/dot-delete.cgi", good, nil)
	if !r.Success {
		t.Fatalf("delete %+v", r.Error)
	}
	if len(r.Data["selectedIds"].([]any)) != 0 || len(h.k.dot) != 1 {
		t.Fatalf("after delete sel=%v live=%v", r.Data["selectedIds"], h.k.dot)
	}
}

func TestApplyRollbackOnRejectedAdd(t *testing.T) {
	h := newHarness(t)
	ids := map[string]any{"serverIds": []string{"cloudflare-primary", "quad9"}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	h.k.reject["dns-proxy tls upstream 9.9.9.9 853 sni dns.quad9.net"] = true
	r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil)
	h.expectErr(r, 502, "DOT_APPLY_FAILED")
	if r.Error.Message != "Keenetic отклонил добавление DNS-over-TLS. Выполнен и проверен откат." || r.Error.Details != "add-command-rejected" {
		t.Fatalf("%q %v", r.Error.Message, r.Error.Details)
	}
	if len(h.k.dot) != 0 || exists(h.s.markerPath()) {
		t.Fatalf("rollback incomplete: %v", h.k.dot)
	}
}

func TestApplyRollbackFailureLeavesMarker(t *testing.T) {
	h := newHarness(t)
	ids := map[string]any{"serverIds": []string{"quad9"}}
	h.call("POST", "/api/routes/dot-test.cgi", ids, nil)
	h.k.reject["system configuration save"] = true
	r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil)
	h.expectErr(r, 502, "DOT_APPLY_FAILED")
	if !strings.HasSuffix(r.Error.Message, "Откат не подтверждён; дальнейшие изменения заблокированы recovery-marker.") {
		t.Fatal(r.Error.Message)
	}
	h.expectErr(h.call("POST", "/api/routes/dot-test.cgi", ids, nil), 409, "DOT_RECOVERY_REQUIRED")
	st := h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil)
	if st.Data["recoveryRequired"] != true || st.Data["configurationState"] != "recovery-required" {
		t.Fatal("status must report recovery")
	}
	// the daemon-start recovery retries the rollback once the router accepts it
	delete(h.k.reject, "system configuration save")
	h.s.recoverMarker(context.Background())
	if exists(h.s.markerPath()) || len(h.k.dot) != 0 {
		t.Fatalf("recovery failed: %v", h.k.dot)
	}
}

func TestLimitAndConflict(t *testing.T) {
	h := newHarness(t)
	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5", "10.0.0.6", "10.0.0.7"} {
		h.k.dot = append(h.k.dot, "dns-proxy tls upstream "+ip+" 853 sni x.example")
	}
	ids := map[string]any{"serverIds": []string{"cloudflare-primary", "quad9"}}
	h.call("POST", "/api/routes/dot-test.cgi", ids, nil)
	r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil)
	h.expectErr(r, 409, "DOT_LIMIT_EXCEEDED")
	if r.Error.Message != "После экспорта будет 9 из 8 DoT/DoH-серверов." {
		t.Fatal(r.Error.Message)
	}
	h.k.dot = []string{"dns-proxy tls upstream 1.1.1.1 853 sni other.example"}
	r = h.call("POST", "/api/routes/dot-apply.cgi", ids, nil)
	h.expectErr(r, 409, "DOT_SELECTOR_CONFLICT")
	if r.Error.Details != `["cloudflare-primary"]` {
		t.Fatal(r.Error.Details)
	}
	h.k.failShow = true
	h.expectErr(h.call("POST", "/api/routes/dot-apply.cgi", ids, nil), 502, "KEENETIC_RUNTIME_UNAVAILABLE")
}

func TestDeletePreviewRefused(t *testing.T) {
	h := newHarness(t)
	r := h.call("POST", "/api/routes/dot-delete-preview.cgi", nil, nil)
	h.expectErr(r, 409, "DOT_DELETE_AUTHORITY_REFUSED")
	if r.Error.Details != "selected-dot-record-not-present" {
		t.Fatal(r.Error.Details)
	}
}

func TestAutoSettings(t *testing.T) {
	h := newHarness(t)
	h.call("GET", "/api/routes/dot-status.cgi", nil, nil) // creates the default selection
	hdr := map[string]string{"X-BROray-Request": "operations", "Origin": "http://192.168.1.1:8080", "Content-Type": "application/json"}
	h.expectErr(h.call("POST", "/api/routes/dot-auto-settings.cgi", `{"enabled":true}`, nil), 403, "CSRF_REJECTED")
	h.expectErr(h.call("POST", "/api/routes/dot-auto-settings.cgi", `{"enabled":true}`, map[string]string{"X-BROray-Request": "operations", "Origin": "http://evil.example"}), 403, "ORIGIN_REJECTED")
	h.expectErr(h.call("POST", "/api/routes/dot-auto-settings.cgi", `{"enabled":1}`, hdr), 400, "REQUEST_INVALID")
	h.expectErr(h.call("POST", "/api/routes/dot-auto-settings.cgi", `{"enabled":true,"x":1}`, hdr), 400, "REQUEST_INVALID")
	r := h.call("POST", "/api/routes/dot-auto-settings.cgi", `{"enabled":true}`, hdr)
	if !r.Success || r.Data["enabled"] != true || r.Data["settingsValid"] != true || r.Data["intervalSeconds"].(float64) != 300 {
		t.Fatalf("%+v", r)
	}
	if r.Data["paused"] != false || len(r.Data["savedIds"].([]any)) != 3 {
		t.Fatalf("view %v", r.Data)
	}
}

func TestAutoCheckQueue(t *testing.T) {
	h := newHarness(t)
	if err := h.s.autoSave(true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.ensureFiles(); err != nil {
		t.Fatal(err)
	}
	if !h.s.autoDue() {
		t.Fatal("must be due without tests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.d.Queue.Run(ctx)
	h.s.enqueueDue()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := readMap(h.s.statePath())
		if ac, ok := st["autoCheck"].(map[string]any); ok && ac["status"] == "success" {
			if len(st["tests"].([]any)) != 3 {
				t.Fatalf("tests %v", st["tests"])
			}
			if h.s.autoDue() {
				t.Fatal("must not be due right after a run")
			}
			view := h.s.autoView()
			if view["last"].(map[string]any)["status"] != "success" {
				t.Fatal(view)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("auto-check did not complete")
}

func TestMigrationAndInvalidConfig(t *testing.T) {
	h := newHarness(t)
	_ = os.MkdirAll(h.s.dotDir(), 0o700)
	_ = compat.WriteJSON(h.s.configPath(), map[string]any{"schemaVersion": 2, "selectedIds": []string{"quad9"},
		"managed": []any{map[string]any{"id": "quad9", "address": "9.9.9.9"}}})
	f, err := h.s.ensureFiles()
	if err != nil {
		t.Fatal(err)
	}
	if f.config["migrationState"] != "legacy-receipts-quarantined" || len(anyList(f.config["quarantinedReceipts"])) != 1 {
		t.Fatalf("%v", f.config)
	}
	_ = compat.WriteJSON(h.s.configPath(), map[string]any{"schemaVersion": 7})
	h.expectErr(h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil), 500, "CONFIG_INVALID")
}

func TestUninstallStep(t *testing.T) {
	h := newHarness(t)
	h.k.dot = []string{"dns-proxy tls upstream 208.67.222.222 sni dns.opendns.com"}
	if err := h.s.uninstall(context.Background()); err != nil {
		t.Fatal("nothing owned must be nil:", err)
	}
	ids := map[string]any{"serverIds": []string{"quad9"}}
	h.call("POST", "/api/routes/dot-test.cgi", ids, nil)
	if r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	if err := h.s.uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.k.dot) != 1 || !strings.Contains(h.k.dot[0], "208.67.222.222") {
		t.Fatalf("live after uninstall %v", h.k.dot)
	}
	if err := h.s.uninstall(context.Background()); err != nil {
		t.Fatal("idempotent:", err)
	}
}

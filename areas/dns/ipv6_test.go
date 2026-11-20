package dns

import (
	"context"
	"strings"
	"testing"
)

func TestIPv6CatalogKeepsExistingSelection(t *testing.T) {
	old := []struct{ id, address, sni string }{
		{"google-primary", "8.8.8.8", "dns.google"}, {"google-secondary", "8.8.4.4", "dns.google"},
		{"yandex-primary", "77.88.8.8", "common.dot.dns.yandex.net"},
		{"cloudflare-primary", "1.1.1.1", "cloudflare-dns.com"}, {"cloudflare-secondary", "1.0.0.1", "cloudflare-dns.com"},
		{"quad9", "9.9.9.9", "dns.quad9.net"}, {"adguard-primary", "94.140.14.14", "dns.adguard-dns.com"},
		{"adguard-secondary", "94.140.15.15", "dns.adguard-dns.com"},
	}
	for i, want := range old {
		if len(catalog) <= i || catalog[i].ID != want.id || catalog[i].Address != want.address || catalog[i].SNI != want.sni {
			t.Fatalf("legacy catalog changed at %d", i)
		}
	}
	h := newHarness(t)
	status := h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil)
	if !status.Success {
		t.Fatal(status.Error)
	}
	seen := map[string]bool{}
	for _, value := range status.Data["servers"].([]any) {
		s := value.(map[string]any)
		if strings.Contains(s["address"].(string), ":") {
			if s["addressFamily"] != "IPv6" || s["selected"] != false || s["present"] != false {
				t.Fatalf("IPv6 must be explicit and OFF: %v", s)
			}
			seen[s["id"].(string)] = true
		} else if s["addressFamily"] != "IPv4" {
			t.Errorf("IPv4 family absent: %v", s["id"])
		}
	}
	for _, id := range []string{"google-primary-ipv6", "google-secondary-ipv6", "cloudflare-primary-ipv6", "cloudflare-secondary-ipv6", "quad9-primary-ipv6", "quad9-secondary-ipv6", "adguard-primary-ipv6", "adguard-secondary-ipv6"} {
		if !seen[id] {
			t.Errorf("missing IPv6 variant %s", id)
		}
	}
	if status.Data["selectedCount"] != float64(3) || len(h.k.cmds()) != 0 {
		t.Fatal("catalog read changed defaults or router")
	}
}

func TestIPv6ObservationCanonicalIdentityAndCollision(t *testing.T) {
	config := []string{"dns-proxy tls upstream 2001:4860:4860::8888 853 sni dns.google"}
	runtime := []string{"proxy-status:", "    proxy-name: System", "    proxy-tls:", "        server-tls:",
		"            address: 2001:4860:4860:0000:0000:0000:0000:8888", "            port: 853", "            sni: dns.google"}
	o := reconcile(parseRunning(config), parseRuntime(runtime))
	if !o.Determinate || !o.RuntimeReconciled {
		t.Fatalf("equivalent IPv6 must reconcile: %+v", o)
	}
	if o.Dot[0].Address != "2001:4860:4860::8888" || o.Runtime.(runtimeObs).Dot[0].Address != "2001:4860:4860:0000:0000:0000:0000:8888" {
		t.Fatalf("observation must preserve raw address spelling: %+v", o)
	}
	config = append(config, "dns-proxy tls upstream 2001:4860:4860:0:0:0:0:8888 853 sni other.example")
	o = parseRunning(config)
	if o.Determinate || o.SelectorCollisionCount != 1 {
		t.Fatalf("equivalent selectors must collide: %+v", o)
	}
}

func TestIPv6StrictAddressGrammar(t *testing.T) {
	for _, address := range []string{"2606:4700:4700::1111", "2606:4700:4700:0:0:0:0:1111"} {
		add := "dns-proxy tls upstream " + address + " 853 sni one.one.one.one"
		del, ok := deleteCommand(liveRec{Address: address, EffectivePort: 853})
		if !commandAllowed(add) || !ok || !commandAllowed(del) {
			t.Errorf("valid IPv6 commands rejected: %s", address)
		}
	}
	for _, address := range []string{"[::1]", "fe80::1%eth0", "::ffff:1.1.1.1", "1::2::3", "2001:db8::/32", "2001:db8::1;reboot", "hostname"} {
		line := "dns-proxy tls upstream " + address + " 853 sni x.example"
		if commandAllowed(line) || parseRunning([]string{line}).Determinate {
			t.Errorf("invalid address accepted: %q", address)
		}
	}
}

func TestIPv6ReceiptExpandedAddress(t *testing.T) {
	c, ok := findCatalog("google-primary-ipv6")
	if !ok {
		t.Fatal("IPv6 preset missing")
	}
	live := liveRec{Address: "2001:4860:4860:0:0:0:0:8888", EffectivePort: 853, PortRaw: "853", PortState: "explicit", SNI: "dns.google", Valid: true}
	r, ok := parseReceipt(receiptObject(c.ID, c.Provider, live, "2026-11-17T00:00:00Z"))
	if !ok || !sameFull(r.rec(), c.endpoint()) {
		t.Fatal("expanded observed receipt cannot be read against catalog")
	}
	// endpoint is a plan record, not an observed record: classify requires Valid.
	actual := c.endpoint()
	actual.Valid = true
	classified := classify(observed{Dot: []liveRec{actual}}, []receipt{r}, nil)
	if len(classified) != 1 || !classified[0].Managed {
		t.Fatalf("semantic address lost ownership: %+v", classified)
	}
}

func TestIPv6ApplyPreservesForeignOnUninstall(t *testing.T) {
	h := newHarness(t)
	foreign := "dns-proxy tls upstream 2606:4700:4700:0:0:0:0:1111 853 sni one.one.one.one"
	h.k.dot = []string{foreign}
	ids := map[string]any{"serverIds": []string{"google-primary-ipv6", "cloudflare-primary-ipv6"}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil)
	if !r.Success || len(r.Data["managed"].([]any)) != 1 {
		t.Fatalf("mixed IPv6 ownership: %+v", r)
	}
	if err := h.s.uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(h.k.dot, "\n") != foreign {
		t.Fatalf("foreign IPv6 changed: %v", h.k.dot)
	}
}

func TestIPv6RollbackAndConfirmedDelete(t *testing.T) {
	h := newHarness(t)
	foreign := "dns-proxy tls upstream 9.9.9.9 853 sni dns.quad9.net"
	h.k.dot = []string{foreign}
	ids := map[string]any{"serverIds": []string{"google-primary-ipv6", "google-secondary-ipv6"}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	rejected := "dns-proxy tls upstream 2001:4860:4860::8844 853 sni dns.google"
	h.k.reject[rejected] = true
	r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil)
	h.expectErr(r, 502, "DOT_APPLY_FAILED")
	if strings.Join(h.k.dot, "\n") != foreign || exists(h.s.markerPath()) {
		t.Fatalf("IPv6 rollback incomplete: %v", h.k.dot)
	}
	delete(h.k.reject, rejected)
	if r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	pv := h.call("POST", "/api/routes/dot-delete-preview.cgi", nil, nil)
	if !pv.Success {
		t.Fatal(pv.Error)
	}
	request := map[string]any{"schemaVersion": 1, "serverIds": pv.Data["serverIds"], "expectedFingerprint": strings.Repeat("0", 64)}
	h.expectErr(h.call("POST", "/api/routes/dot-delete.cgi", request, nil), 409, "DOT_DELETE_CONFIRMATION_STALE")
	request["expectedFingerprint"] = pv.Data["expectedFingerprint"]
	if r := h.call("POST", "/api/routes/dot-delete.cgi", request, nil); !r.Success {
		t.Fatal(r.Error)
	}
	if strings.Join(h.k.dot, "\n") != foreign {
		t.Fatalf("confirmed delete changed foreign: %v", h.k.dot)
	}
}

func TestUnknownReceiptProtocolCannotAuthorizeApply(t *testing.T) {
	for _, keep := range []bool{false, true} {
		name := "drop"
		if keep {
			name = "keep"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			initial := map[string]any{"serverIds": []string{"google-primary"}}
			if r := h.call("POST", "/api/routes/dot-test.cgi", initial, nil); !r.Success {
				t.Fatal(r.Error)
			}
			if r := h.call("POST", "/api/routes/dot-apply.cgi", initial, nil); !r.Success {
				t.Fatal(r.Error)
			}
			cfg, err := readMap(h.s.configPath())
			if err != nil {
				t.Fatal(err)
			}
			cfg["managed"].([]any)[0].(map[string]any)["writeProtocolSha256"] = strings.Repeat("0", 64)
			if err := writeMap(h.s.configPath(), cfg); err != nil {
				t.Fatal(err)
			}
			id := "quad9"
			if keep {
				id = "google-primary"
			}
			selection := map[string]any{"serverIds": []string{id}}
			if r := h.call("POST", "/api/routes/dot-test.cgi", selection, nil); !r.Success {
				t.Fatal(r.Error)
			}
			if r := h.call("POST", "/api/routes/dot-apply.cgi", selection, nil); !r.Success {
				t.Fatal(r.Error)
			}
			if err := h.s.uninstall(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := "dns-proxy tls upstream 8.8.8.8 853 sni dns.google"
			if strings.Join(h.k.dot, "\n") != want {
				t.Fatalf("unknown protocol authorized delete/adoption: %v", h.k.dot)
			}
		})
	}
}

func TestUninstallRequiresReceiptAuthorityAndDeterminateObservation(t *testing.T) {
	for _, mode := range []string{"unknown-protocol", "unknown-token", "runtime-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			ids := map[string]any{"serverIds": []string{"google-primary"}}
			if r := h.call("POST", "/api/routes/dot-test.cgi", ids, nil); !r.Success {
				t.Fatal(r.Error)
			}
			if r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil); !r.Success {
				t.Fatal(r.Error)
			}
			switch mode {
			case "unknown-protocol":
				cfg, err := readMap(h.s.configPath())
				if err != nil {
					t.Fatal(err)
				}
				cfg["managed"].([]any)[0].(map[string]any)["writeProtocolSha256"] = strings.Repeat("0", 64)
				if err := writeMap(h.s.configPath(), cfg); err != nil {
					t.Fatal(err)
				}
			case "unknown-token":
				h.k.dot[0] += " unknown value"
			case "runtime-mismatch":
				h.k.runtimeFn = func([]string) []string { return []string{"proxy-status:", "    proxy-name: System", "    proxy-tls:"} }
			}
			before, commands := strings.Join(h.k.dot, "\n"), len(h.k.cmds())
			err := h.s.uninstall(context.Background())
			if mode != "unknown-protocol" && err == nil {
				t.Error("ambiguous observation must stop uninstall")
			}
			if strings.Join(h.k.dot, "\n") != before || len(h.k.cmds()) != commands {
				t.Fatalf("uninstall mutated without authority: live=%v, error=%v", h.k.dot, err)
			}
		})
	}
}

func TestUninstallKeepsReceiptWhenReadbackIsAmbiguous(t *testing.T) {
	h := newHarness(t)
	ids := map[string]any{"serverIds": []string{"google-primary-ipv6"}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	if r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	h.k.runtimeFn = func(dot []string) []string {
		lines := []string{"proxy-status:", "    proxy-name: System", "    proxy-tls:"}
		if len(dot) > 0 {
			return append(lines, "        server-tls:", "            address: 2001:4860:4860::8888", "            port: 853", "            sni: dns.google")
		}
		return append(lines, "        unexpected-schema: unknown")
	}
	if err := h.s.uninstall(context.Background()); err == nil {
		t.Error("ambiguous post-delete readback reported success")
	}
	cfg, err := readMap(h.s.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg["managed"].([]any)) != 1 {
		t.Fatal("unconfirmed removal cleared its ownership receipt")
	}
}

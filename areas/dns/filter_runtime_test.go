package dns

import (
	"context"
	"strings"
	"testing"
)

func filterRecord(address, sni string) []string {
	return []string{"        server-tls:", "            address: " + address, "            port: 853", "            sni: " + sni, "            spki:", "            interface:", "            domain:"}
}

func filterRuntime(config []string) []string {
	lines := []string{"proxy-status:", "    proxy-name: System", "    proxy-tls:"}
	lines = append(lines, filterRecord("192.0.2.53", "filter.example")...)
	for _, c := range config {
		f := strings.Fields(c)
		if f[3] != "192.0.2.53" {
			lines = append(lines, filterRecord(f[3], f[len(f)-1])...)
		}
	}
	lines = append(lines, "    proxy-tls-filters:")
	return append(lines, filterRecord("192.0.2.53", "filter.example")...)
}

func TestFilterRuntimeReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		explicit []string
	}{
		{"filter-only", nil},
		{"mixed", []string{"dns-proxy tls upstream 8.8.8.8 853 sni dns.google"}},
		{"shared-endpoint", []string{"dns-proxy tls upstream 192.0.2.53 853 sni filter.example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reconcile(parseRunning(tc.explicit), parseRuntime(filterRuntime(tc.explicit)))
			if !got.Determinate {
				t.Fatalf("valid filter ownership must reconcile: %+v", got)
			}
			if len(got.Dot) != len(tc.explicit) {
				t.Fatal("filter-only entry became an explicit upstream")
			}
		})
	}
}

func TestFilterRuntimeRejectsUnexplainedOrMalformed(t *testing.T) {
	base := filterRuntime(nil)
	cases := map[string][]string{
		"missing-runtime":                     append([]string{"proxy-status:", "proxy-name: System", "proxy-tls:", "proxy-tls-filters:"}, filterRecord("192.0.2.53", "filter.example")...),
		"wrong-filter-identity":               strings.Split(strings.Replace(strings.Join(base, "\n"), "proxy-tls-filters:\n        server-tls:\n            address: 192.0.2.53", "proxy-tls-filters:\n        server-tls:\n            address: 192.0.2.54", 1), "\n"),
		"unknown-filter-field":                append(append([]string{}, base...), "            unexpected: value"),
		"duplicate-filter":                    append(append([]string{}, base...), filterRecord("192.0.2.53", "filter.example")...),
		"policy-filter-cannot-explain-system": append([]string{"proxy-status:", "proxy-name: System", "proxy-tls:"}, append(filterRecord("192.0.2.53", "filter.example"), append([]string{"proxy-status:", "proxy-name: Policy0", "proxy-tls-filters:"}, filterRecord("192.0.2.53", "filter.example")...)...)...),
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			if reconcile(parseRunning(nil), parseRuntime(lines)).Determinate {
				t.Fatal("unsafe observation accepted")
			}
		})
	}
}

func TestFilterRuntimeApplyUninstallPreservesFilter(t *testing.T) {
	h := newHarness(t)
	h.k.runtimeFn = filterRuntime
	h.k.extra = []string{"dns-proxy", "    filter assign interface preset Bridge1 yandex-safe", "    filter engine public"}
	selected := map[string]any{"serverIds": []string{"google-primary"}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", selected, nil); !r.Success {
		t.Fatal(r.Error)
	}
	if r := h.call("POST", "/api/routes/dot-apply.cgi", selected, nil); !r.Success {
		t.Fatal(r.Error)
	}
	if err := h.s.uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.k.dot) != 0 {
		t.Fatalf("owned upstream not removed: %v", h.k.dot)
	}
	for _, c := range h.k.cmds() {
		if strings.Contains(c, "filter") || strings.Contains(c, "192.0.2.53") {
			t.Fatalf("touched filter: %s", c)
		}
	}
	if got := reconcile(parseRunning(nil), parseRuntime(filterRuntime(nil))); !got.Determinate {
		t.Fatal("filter observation broken after uninstall")
	}
}

func TestFilterRuntimeRCIRendering(t *testing.T) {
	raw := []byte(`{"proxy-status":[{"proxy-name":"System","proxy-tls":{"server-tls":[{"address":"192.0.2.53","port":853,"sni":"filter.example","spki":"","interface":"","domain":""}]},"proxy-tls-filters":{"server-tls":[{"address":"192.0.2.53","port":853,"sni":"filter.example","spki":"","interface":"","domain":""}]},"proxy-https-filters":{"server-https":[{"uri":"https://filter.example/dns-query"}]}}]}`)
	lines, err := renderRCI(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := parseRuntime(lines)
	if len(r.FilterDot) != 1 || !reconcile(parseRunning(nil), r).Determinate {
		t.Fatalf("RCI filter ownership lost: %+v", r)
	}
}

func TestFilterRuntimeExplicitDeletePreservesFilter(t *testing.T) {
	for _, shared := range []bool{false, true} {
		name := "mixed"
		if shared {
			name = "shared-endpoint"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.k.runtimeFn = filterRuntime
			if shared {
				h.k.runtimeFn = func(_ []string) []string {
					lines := filterRuntime(nil)
					for i, l := range lines {
						lines[i] = strings.ReplaceAll(strings.ReplaceAll(l, "192.0.2.53", "8.8.8.8"), "filter.example", "dns.google")
					}
					return lines
				}
			}
			selected := map[string]any{"serverIds": []string{"google-primary"}}
			if r := h.call("POST", "/api/routes/dot-test.cgi", selected, nil); !r.Success {
				t.Fatal(r.Error)
			}
			// A filter-only catalog match is not an explicit record to delete.
			pv := h.call("POST", "/api/routes/dot-delete-preview.cgi", nil, nil)
			if pv.Success && len(pv.Data["entries"].([]any)) != 0 {
				t.Fatal("filter-only entry entered deletion preview")
			}
			if r := h.call("POST", "/api/routes/dot-apply.cgi", selected, nil); !r.Success {
				t.Fatal(r.Error)
			}
			pv = h.call("POST", "/api/routes/dot-delete-preview.cgi", nil, nil)
			if !pv.Success || len(pv.Data["entries"].([]any)) != 1 {
				t.Fatalf("explicit preview: %+v", pv)
			}
			r := h.call("POST", "/api/routes/dot-delete.cgi", map[string]any{"schemaVersion": 1, "serverIds": pv.Data["serverIds"], "expectedFingerprint": pv.Data["expectedFingerprint"]}, nil)
			if !r.Success {
				t.Fatal(r.Error)
			}
			if len(h.k.dot) != 0 {
				t.Fatal("explicit record remains")
			}
			obs := reconcile(parseRunning(nil), parseRuntime(h.k.runtimeFn(nil)))
			if !obs.Determinate || len(obs.Runtime.(runtimeObs).FilterDot) != 1 {
				t.Fatal("filter lost after explicit delete")
			}
			for _, c := range h.k.cmds() {
				if strings.Contains(c, "filter") || strings.Contains(c, "192.0.2.53") {
					t.Fatalf("filter mutation: %s", c)
				}
			}
		})
	}
}

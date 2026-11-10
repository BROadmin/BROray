package servers

import (
	"context"
	"os"
	"strings"
	"testing"
)

// A newly named fingerprint must not silently degrade to the core's fallback.
// Ordinary chrome remains a dynamic core alias, including on older cores.
func TestCoreCapabilityGate(t *testing.T) {
	for _, tc := range []struct {
		name, version, fingerprint string
		wantOK                     bool
	}{
		{"old-core-new-profile", "26.10.31", "hellochrome_155", false},
		{"minimum-core", "26.11.9", "hellochrome_155", true},
		{"later-core", "26.12.1", "hellochrome_155", true},
		{"unidentified-core", "unknown", "hellochrome_155", false},
		{"dynamic-chrome", "26.10.31", "chrome", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			if err := os.WriteFile(e.d.Root.XrayBinary(), []byte("#!/bin/sh\necho 'Xray "+tc.version+" (test core)'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			id := e.addServer(t, "trojan://synthetic@example.test:443?fp="+tc.fingerprint)
			r := call(t, "POST", "/api/servers/activate.cgi", `{"id":"`+id+`"}`, nil)
			if tc.wantOK {
				if r.Code != 200 || ActiveID(e.d) != id || len(e.applied) != 1 {
					t.Fatalf("activation: %s", r.Raw)
				}
			} else {
				if r.Code != 400 || errCode(r) != "XRAY_CORE_UNSUPPORTED" || !strings.Contains(r.Raw, "26.11.9") {
					t.Errorf("expected a minimum-core error, got %s", r.Raw)
				}
				if ActiveID(e.d) != "" || len(e.applied) != 0 || e.ensured.Load() != 0 {
					t.Error("unsupported profile mutated runtime")
				}
			}
		})
	}
}

func TestCoreCapabilityQualityDoesNotProbeOrOverwrite(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(e.d.Root.XrayBinary(), []byte("#!/bin/sh\necho 'Xray 26.10.31 (test core)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := e.addServer(t, "trojan://synthetic@example.test:443?fp=hellochrome_155")
	called := false
	e.s.measure = func(context.Context, Record, []byte) probeResult { called = true; return probeResult{Success: true} }
	_, err := e.s.checkServer(context.Background(), id, "manual", "")
	if err == nil || !strings.Contains(err.Error(), "26.11.9") {
		t.Errorf("missing version rejection: %v", err)
	}
	if called {
		t.Error("probe started on unsupported core")
	}
	if _, err := os.Stat(QualityPath(e.d, id)); !os.IsNotExist(err) {
		t.Error("quality overwritten despite incompatible core")
	}
}

func TestCoreCapabilityUsesEffectiveSettings(t *testing.T) {
	for _, tc := range []struct {
		security, tls, reality string
		wantNew                bool
	}{
		{"none", "hellochrome_155", "hellochrome_155", false},
		{"tls", "chrome", "hellochrome_155", false},
		{"reality", "hellochrome_155", "chrome", false},
		{"tls", "", "hellochrome_155", false},
		{"reality", "chrome", "hellochrome_155", true},
	} {
		r := Record{"protocol": "vless", "network": "raw", "security": tc.security, "tls": map[string]any{"fingerprint": tc.tls}, "reality": map[string]any{"fingerprint": tc.reality}}
		if got := RequiredCore(r) != ""; got != tc.wantNew {
			t.Errorf("effective %s fingerprint gate=%v want=%v", tc.security, got, tc.wantNew)
		}
	}
	r := Record{"protocol": "vless", "network": "raw", "security": "none", "transport": map[string]any{"extra": map[string]any{"downloadSettings": map[string]any{"security": "tls", "tlsSettings": map[string]any{"fingerprint": "hellochrome_155"}}}}}
	if RequiredCore(r) != "" {
		t.Error("unused transport block gated old stored record")
	}
	r["security"], r["tls"], r["reality"] = "tls", map[string]any{}, map[string]any{"fingerprint": "hellochrome_155"}
	if RequiredCore(r) == "" {
		t.Error("absent TLS fingerprint must follow generator's legacy REALITY fallback")
	}
}

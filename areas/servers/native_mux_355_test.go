package servers

import (
	"encoding/json"
	"strings"
	"testing"
)

func missingMux355Fixture() map[string]any {
	out := nativeTestOutbound("xdrive")
	delete(out, "mux")
	out["settings"] = map[string]any{"address": "provider.example.invalid", "port": 443, "id": "11111111-1111-4111-8111-111111111111", "encryption": "mlkem768x25519plus.native.0rtt." + strings.Repeat("A", 43), "level": 8}
	out["streamSettings"] = map[string]any{"network": "xdrive", "security": "none", "xdriveSettings": map[string]any{"service": "google-drive", "remoteFolder": "invented-folder", "secrets": []any{"invented-client", "invented-secret", "invented-refresh"}}}
	return out
}
func TestYour355MissingXDriveMux(t *testing.T) {
	out := missingMux355Fixture()
	before, _ := json.Marshal(out)
	r, e := ParseNativeOutbound(out, "invented", "mux-test", nil)
	if e != nil {
		t.Fatal(e)
	}
	after, _ := json.Marshal(out)
	if string(before) != string(after) {
		t.Fatal("caller mutated")
	}
	if !jsonValueEqual(r["mux"], map[string]any{"enabled": true}) {
		t.Fatal("missing mandatory client mux normalization")
	}
	warnings := r["importWarnings"].([]any)
	if !strings.Contains(strings.Join(func() []string {
		a := []string{}
		for _, w := range warnings {
			a = append(a, w.(string))
		}
		return a
	}(), " "), "mux.enabled=true") {
		t.Fatal("missing explicit client-policy advisory")
	}
	if !jsonValueEqual(r["xdrive"], out["streamSettings"].(map[string]any)["xdriveSettings"]) || r["uuid"] != out["settings"].(map[string]any)["id"] || r["encryption"] != out["settings"].(map[string]any)["encryption"] {
		t.Fatal("provider credentials/settings changed")
	}
	explicit := missingMux355Fixture()
	explicit["mux"] = map[string]any{"enabled": true}
	r2, e := ParseNativeOutbound(explicit, "invented", "mux-test", nil)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := CanonicalKey(r)
	b, _ := CanonicalKey(r2)
	if a != b {
		t.Fatal("default and explicit mux identity differs")
	}
	coverageRoundTrip(t, r, true)
}
func TestYour355ExplicitXDriveMuxRemainsStrict(t *testing.T) {
	for _, mux := range []any{nil, false, map[string]any{}, map[string]any{"enabled": false}, map[string]any{"enabled": "true"}, map[string]any{"enabled": true, "concurrency": -1}, map[string]any{"enabled": true, "xudpProxyUDP443": "skip"}} {
		out := missingMux355Fixture()
		out["mux"] = mux
		if _, e := ParseNativeOutbound(out, "invented", "mux-test", nil); e == nil {
			t.Fatalf("explicit invalid mux accepted: %T", mux)
		}
	}
	masque := nativeTestOutbound("masque")
	r, e := ParseNativeOutbound(masque, "invented", "mux-test", nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := r["mux"]; ok {
		t.Fatal("MASQUE mux invented")
	}
}

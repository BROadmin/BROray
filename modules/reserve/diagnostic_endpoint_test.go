package reserve

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"testing"
)

func TestReserveDiagnosticEndpointExactShape(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:32080", "[::1]:32080"} {
		ep := netip.MustParseAddrPort(addr)
		config, err := AttachHealthInbound([]byte(healthBase), ep)
		if err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), config...)
		if got, ok := DiagnosticEndpoint(config); !ok || got != ep {
			t.Fatalf("exact diagnostic input not recognized: %v", got)
		}
		if !bytes.Equal(before, config) {
			t.Fatal("recognizer changed config")
		}
		for _, mutation := range []string{"route", "auth", "tag", "port", "reorder", "duplicate"} {
			cfg := configObject(t, config)
			input := cfg["inbounds"].([]any)[1].(map[string]any)
			switch mutation {
			case "route":
				cfg["routing"].(map[string]any)["rules"].([]any)[0].(map[string]any)["outboundTag"] = "direct"
			case "auth":
				input["settings"].(map[string]any)["auth"] = "password"
			case "tag":
				input["tag"] = "other"
			case "port":
				input["port"] = "32080"
			case "reorder":
				ins := cfg["inbounds"].([]any)
				ins[0], ins[1] = ins[1], ins[0]
			}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "duplicate" {
				raw = bytes.Replace(raw, []byte(`"inbounds":`), []byte(`"inbounds":[],"inbounds":`), 1)
			}
			if _, ok := DiagnosticEndpoint(raw); ok {
				t.Fatalf("tampered %s recognized", mutation)
			}
		}
	}
	if _, ok := DiagnosticEndpoint([]byte(healthBase)); ok {
		t.Fatal("ordinary config recognized as diagnostic")
	}
}

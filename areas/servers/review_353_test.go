package servers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReview353CloudCredentialContinuity(t *testing.T) {
	for _, kind := range []string{"static", "basic", "oauth2", "warp"} {
		t.Run(kind, func(t *testing.T) {
			backend := "template"
			if kind == "warp" {
				backend = "masque-warp"
			}
			out := nativeBackendFixture(t, backend)
			if kind != "warp" {
				auth := map[string]any{"type": kind}
				switch kind {
				case "static":
					auth["header"] = map[string]any{"Authorization": "Bearer synthetic-old"}
				case "basic":
					auth["username"], auth["password"] = "synthetic", "synthetic-old"
				case "oauth2":
					auth["tokenUrl"], auth["form"] = "https://storage.example.test/token", map[string]any{"refresh_token": "synthetic-old"}
				}
				get(out, "streamSettings", "xdriveSettings", "template").(map[string]any)["auth"] = auth
			}
			old := parseNativeFixture(t, out)
			if kind == "warp" {
				get(out, "streamSettings", "masqueSettings", "warp").(map[string]any)["privateKey"] = get(nativeBackendFixture(t, "masque-warp"), "streamSettings", "masqueSettings", "warp", "privateKey")
			} else {
				auth := get(out, "streamSettings", "xdriveSettings", "template", "auth").(map[string]any)
				switch kind {
				case "static":
					auth["header"].(map[string]any)["Authorization"] = "Bearer synthetic-new"
				case "basic":
					auth["password"] = "synthetic-new"
				case "oauth2":
					auth["form"].(map[string]any)["refresh_token"] = "synthetic-new"
				}
			}
			changed := parseNativeFixture(t, out)
			a, _ := ContinuityKey(old)
			b, _ := ContinuityKey(changed)
			if a != b {
				t.Error("credential-only rotation changed endpoint identity")
			}
			a, _ = CanonicalKey(old)
			b, _ = CanonicalKey(changed)
			if a == b {
				t.Error("new credentials not detected as configuration change")
			}
		})
	}
}

func TestReview353LegacyGRPCPathAlias(t *testing.T) {
	for _, key := range []string{"serviceName", "service_name"} {
		for _, same := range []bool{false, true} {
			path := "private-rpc-two"
			if same {
				path = "private-rpc-one"
			}
			link := legacyContractLink(t, map[string]any{"net": "grpc", key: "private-rpc-one", "path": path})
			rec, err := ParseLink(link, "test", nil)
			if !same {
				if err == nil || !strings.Contains(err.Error(), "path") || strings.Contains(err.Error(), "private-rpc") {
					t.Errorf("conflicting alias silently discarded: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				out, err := Outbound(rec)
				if err != nil || get(out, "streamSettings", "grpcSettings", "serviceName") != path {
					t.Fatal("equal aliases changed", err)
				}
			}
		}
	}
}

func TestReview353NativeEnumType(t *testing.T) {
	for _, field := range []string{"mux.xudpProxyUDP443", "template.auth.type"} {
		for _, value := range []any{false, true, nil, 0, []any{}, map[string]any{}} {
			out := nativeBackendFixture(t, "template")
			if field == "mux.xudpProxyUDP443" {
				out["mux"].(map[string]any)["xudpProxyUDP443"] = value
			} else {
				get(out, "streamSettings", "xdriveSettings", "template").(map[string]any)["auth"] = map[string]any{"type": value}
			}
			b, _ := json.Marshal(out)
			_, err := ParseLink(string(b), "test", nil)
			if err == nil || !strings.Contains(err.Error(), field) {
				t.Errorf("wrong enum type accepted: %s %T: %v", field, value, err)
			}
		}
	}
}

package xrayimport

import (
	"encoding/json"
	"reflect"
	"testing"
)

func unavailable357Fixture() map[string]any {
	return map[string]any{"protocol": "vless", "tag": "synthetic-private-tag", "settings": map[string]any{
		"address": "edge.example.test", "port": 443, "id": "00000000-0000-4000-8000-000000000357", "encryption": "none"},
		"streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"serverName": "edge.example.test"}}}
}

func unavailable357Decode(t *testing.T, source map[string]any) Input {
	t.Helper()
	raw, _ := json.Marshal(source)
	result, err := DecodeJSON(raw, 10, Checks{VlessEncryptionValid: func(string) bool { return true },
		ValidateStreamOptions: func(string, map[string]any) error { return nil }, ValidateClientFields: func(string, string, string, map[string]any) error { return nil }})
	if err != nil || len(result.Inputs) != 1 {
		t.Fatal("source framing failed")
	}
	return result.Inputs[0]
}

func TestFullXray357UnavailableConverterSourceCarrier(t *testing.T) {
	for _, kind := range []string{"reverse", "policy-extension"} {
		t.Run(kind, func(t *testing.T) {
			source := unavailable357Fixture()
			settings := source["settings"].(map[string]any)
			if kind == "reverse" {
				settings["reverse"] = map[string]any{"tag": "synthetic-private-reverse"}
			} else {
				settings["level"] = 7
				source["providerExtension"] = map[string]any{"secret": "synthetic-private-value"}
			}
			node := unavailable357Decode(t, source)
			if node.Rejected || node.Link == "" && node.NativeOutbound == nil {
				t.Fatal("typed unavailable source needs a validated ordinary projection")
			}
			field := reflect.ValueOf(node).FieldByName("UnavailableSource")
			if !field.IsValid() || !field.CanInterface() {
				t.Fatal("private unavailable source carrier missing")
			}
			got, _ := json.Marshal(field.Interface())
			want, _ := json.Marshal(source)
			if string(got) != string(want) {
				t.Fatal("source intent lost or altered by projection")
			}
		})
	}
}

func TestFullXray357UnavailableConverterRejectsMalformedProjection(t *testing.T) {
	for _, field := range []string{"port", "tls"} {
		source := unavailable357Fixture()
		source["providerExtension"] = true
		if field == "port" {
			source["settings"].(map[string]any)["port"] = "synthetic-private-value"
		} else {
			source["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["enableSessionResumption"] = "synthetic-private-value"
		}
		if !unavailable357Decode(t, source).Rejected {
			t.Fatal("malformed declared field absorbed into unavailable source")
		}
	}
}

func TestFullXray357UnavailableConverterNarrowBridge(t *testing.T) {
	checks := Checks{VlessEncryptionValid: func(string) bool { return true },
		ValidateStreamOptions: func(string, map[string]any) error { return nil }, ValidateClientFields: func(string, string, string, map[string]any) error { return nil }}
	for _, kind := range []string{"ordinary", "email", "reverse"} {
		source := unavailable357Fixture()
		settings := source["settings"].(map[string]any)
		switch kind {
		case "email":
			settings["email"] = "synthetic-private-email"
		case "reverse":
			settings["reverse"] = map[string]any{"tag": "synthetic-private-reverse"}
		}
		lines, err := OutboundURIs(map[string]any{}, source, 1, false, checks)
		if kind == "ordinary" {
			if err != nil || len(lines) != 1 {
				t.Fatal("ordinary URI bridge rejected")
			}
		} else if err == nil || len(lines) != 0 {
			t.Fatal("URI-only bridge silently lost private sidecar")
		}
	}
}

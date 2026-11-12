package servers

import (
	"encoding/json"
	"net/url"
	"testing"
)

// Client socket options must survive URI -> private record -> generated Xray.
// The values below are synthetic and never contact an external server.
func TestFullImport357ClientSocket(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"tcpCongestion", "cubic"},
		{"tcpWindowClamp", json.Number("65536")},
		{"tcpMaxSeg", json.Number("1200")},
		{"tcpUserTimeout", json.Number("0")},
		{"tcpMptcp", false},
		{"addressPortStrategy", "srvportonly"},
		{"happyEyeballs", map[string]any{"prioritizeIPv6": true, "tryDelayMs": json.Number("0"), "interleave": json.Number("1"), "maxConcurrentTry": json.Number("4")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := map[string]any{"sockopt": map[string]any{tc.name: tc.value}}
			data, _ := json.Marshal(options)
			link := "vless://11111111-1111-4111-8111-111111111111@edge.example.test:443?encryption=none&security=none&streamOptions=" + url.QueryEscape(string(data))
			r, err := ParseLink(link, "full-import", nil)
			if err != nil {
				t.Fatal(err)
			}
			out, err := Outbound(r)
			if err != nil {
				t.Fatal(err)
			}
			actual := get(out, "streamSettings", "sockopt", tc.name)
			if !jsonValueEqual(actual, tc.value) {
				t.Fatalf("socket field %s lost or changed", tc.name)
			}
		})
	}
}

func TestFullImport357ClientSocketInvalid(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"tcpMptcp", "true"}, {"tcpUserTimeout", json.Number("2147483648")},
		{"tcpWindowClamp", json.Number("1.5")}, {"happyEyeballs", nil},
		{"happyEyeballs", map[string]any{"tryDelayMs": json.Number("-1")}},
		{"happyEyeballs", map[string]any{"unexpected": true}},
		{"addressPortStrategy", "unknown"}, {"tcpCongestion", []any{}},
	} {
		data, _ := json.Marshal(map[string]any{"sockopt": map[string]any{tc.name: tc.value}})
		link := "vless://11111111-1111-4111-8111-111111111111@edge.example.test:443?encryption=none&streamOptions=" + url.QueryEscape(string(data))
		if _, err := ParseLink(link, "invalid", nil); err == nil {
			t.Fatalf("invalid %s accepted", tc.name)
		}
	}
}

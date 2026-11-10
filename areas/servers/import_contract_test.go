package servers

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestTrojanRejectsVlessOnlyFields(t *testing.T) {
	for _, field := range []string{"flow", "encryption"} {
		t.Run(field, func(t *testing.T) {
			_, err := ParseLink("trojan://synthetic@example.test:443?"+field+"=private-unused-value", "test", nil)
			if err == nil || !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), "private-unused-value") {
				t.Fatalf("field must be rejected without its value: %v", err)
			}
		})
	}
}

func TestURIFieldContext(t *testing.T) {
	for _, tc := range []struct{ query, field string }{
		{"type=raw&authority=private-unused-value", "authority"},
		{"type=ws&authority=private-unused-value", "authority"},
		{"type=raw&serviceName=private-unused-value", "serviceName"},
		{"type=ws&service_name=private-unused-value", "service_name"},
		{"type=raw&mode=packet-up", "mode"},
		{"type=raw&security=tls&sid=abcd", "sid"},
		{"type=ws&security=tls&pbk=private-unused-value", "pbk"},
		{"type=ws&security=none&sni=private-unused-value", "sni"},
		{"type=raw&security=reality&sni=example.test&pbk=test&sid=abcd&alpn=h2", "alpn"},
		{"type=raw&security=tls&host=private-unused-value", "host"},
		{"type=raw&security=tls&path=/private-unused-value", "path"},
		{"type=grpc&security=tls&path=/private-unused-value", "path"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			_, err := ParseLink("vless://test@example.test:443?"+tc.query, "test", nil)
			if err == nil || !strings.Contains(err.Error(), tc.field) || strings.Contains(err.Error(), "private-unused-value") {
				t.Fatalf("context field silently unused: %v", err)
			}
		})
	}
	rec, err := ParseLink("vless://test@example.test:443?type=grpc&security=tls&authority=grpc.example.test&serviceName=rpc&mode=multi", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Outbound(rec)
	if err != nil {
		t.Fatal(err)
	}
	if get(out, "streamSettings", "grpcSettings", "authority") != "grpc.example.test" || get(out, "streamSettings", "grpcSettings", "multiMode") != true {
		t.Fatal("valid grpc settings changed")
	}
}

func TestLegacyVMessFieldContext(t *testing.T) {
	for _, fields := range []map[string]any{
		{"host": "unused.example.test"}, {"path": "/unused"},
		{"serviceName": "unused"}, {"tls": "none", "sni": "unused.example.test"},
		{"tls": "tls", "sid": "abcd"}, {"tls": "tls", "pbk": "unused"},
		{"mode": "packet-up"}, {"mtu": 1400}, {"seed": "unused"},
		{"host": 123}, {"path": false}, {"alpn": 0},
		{"tls": "none", "alpn": []any{"h2", "h3"}},
		{"type": "none", "headerType": "http"},
	} {
		link := legacyContractLink(t, fields)
		if _, err := ParseLink(link, "test", nil); err == nil {
			t.Errorf("unused or invalid legacy fields accepted: keys=%v", sortedKeys(fields))
		}
	}
	for _, fields := range []map[string]any{
		{"net": "grpc", "path": "legacy-rpc"},
		{"net": "kcp", "tls": "none", "path": "legacy-seed"},
		{"type": "http", "host": "header.example.test", "path": "/hello"},
	} {
		if _, err := ParseLink(legacyContractLink(t, fields), "test", nil); err != nil {
			t.Fatal("valid legacy transport rejected", err)
		}
	}
}

func TestHysteriaRejectsRealityOnlyField(t *testing.T) {
	if _, err := ParseLink("hy2://test@example.test:443?pqv=private-unused-value", "test", nil); err == nil {
		t.Fatal("REALITY verification parameter lost in Hysteria")
	}
}

func legacyContractLink(t *testing.T, fields map[string]any) string {
	t.Helper()
	m := map[string]any{"v": "2", "add": "example.test", "port": "443", "id": "test", "net": "tcp", "tls": "tls"}
	for k, v := range fields {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

func TestLegacyVMessAliasConflict(t *testing.T) {
	for _, tc := range []struct {
		a, b   string
		av, bv any
	}{
		{"add", "address", "one.example.test", "two.example.test"},
		{"id", "uuid", "private-first-id", "private-second-id"},
		{"sni", "serverName", "one.example.test", "two.example.test"},
		{"fp", "fingerprint", "chrome", "firefox"},
		{"net", "network", "tcp", "ws"},
		{"tls", "security", "tls", "none"},
		{"pbk", "publicKey", "private-first-key", "private-second-key"},
		{"sid", "shortId", "abcd", "1234"},
		{"spx", "spiderX", "/one", "/two"},
		{"aid", "alterId", 0, 1},
		{"scy", "securityCipher", "auto", "none"},
		{"securityCipher", "cipher", "auto", "none"},
		{"serviceName", "service_name", "one", "two"},
	} {
		t.Run(tc.a+"-"+tc.b, func(t *testing.T) {
			_, err := ParseLink(legacyContractLink(t, map[string]any{tc.a: tc.av, tc.b: tc.bv}), "test", nil)
			if err == nil || !strings.Contains(err.Error(), tc.a) || !strings.Contains(err.Error(), tc.b) {
				t.Errorf("conflicting aliases accepted: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private-") {
				t.Error("alias error leaks value")
			}
		})
	}
	for _, fields := range []map[string]any{
		{"add": "example.test", "address": "example.test"},
		{"aid": 0, "alterId": "0"},
		{"net": "tcp", "network": "raw"},
		{"net": "websocket", "network": "ws"},
	} {
		if _, err := ParseLink(legacyContractLink(t, fields), "test", nil); err != nil {
			t.Errorf("equivalent aliases rejected: %v", err)
		}
	}
}

func TestHysteriaIdleTimeoutBounds(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{"", true}, {"0", true}, {"2", true}, {"600", true}, {"1", false}, {"601", false}, {"-1", false}, {"false", false},
	} {
		t.Run("value-"+tc.value, func(t *testing.T) {
			link := "hysteria2://synthetic@example.test:443"
			if tc.value != "" {
				link += "?udpIdleTimeout=" + url.QueryEscape(tc.value)
			}
			rec, err := ParseLink(link, "test", nil)
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), "udpIdleTimeout") {
					t.Errorf("invalid timeout accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			out, err := Outbound(rec)
			if err != nil {
				t.Fatal(err)
			}
			v := get(out, "streamSettings", "hysteriaSettings", "udpIdleTimeout")
			if tc.value == "" {
				if v != nil {
					t.Errorf("absence became explicit: %v", v)
				}
			} else if jqRaw(v) != tc.value {
				t.Errorf("timeout changed: %v", v)
			}
		})
	}
}

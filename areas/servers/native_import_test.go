package servers

import (
	"encoding/json"
	"strings"
	"testing"
)

func nativeTestOutbound(kind string) map[string]any {
	tls := map[string]any{"serverName": "front.example.test", "fingerprint": "chrome", "alpn": []any{"h3"}}
	if kind == "masque" {
		return map[string]any{"protocol": "masque", "tag": "Профиль", "settings": map[string]any{"address": "proxy.example.test", "port": 443, "remoteDNS": []any{"1.1.1.1", "2606:4700:4700::1111"}}, "streamSettings": map[string]any{"network": "masque", "security": "tls", "tlsSettings": tls, "masqueSettings": map[string]any{"host": "proxy.example.test", "path": "/masque{?target,ipproto}", "user": "synthetic-private-user", "pass": "synthetic-private-pass", "headers": map[string]any{"X-Test": "synthetic-private-header"}, "xmux": map[string]any{"maxConnections": 1, "hKeepAlivePeriod": 0}}}}
	}
	return map[string]any{"protocol": "vless", "tag": "Профиль", "settings": map[string]any{"vnext": []any{map[string]any{"address": "proxy.example.test", "port": 443, "users": []any{map[string]any{"id": "00000000-0000-4000-8000-000000000001", "encryption": "mlkem768x25519plus.native.0rtt." + strings.Repeat("A", 43)}}}}}, "mux": map[string]any{"enabled": true, "concurrency": 0, "xudpConcurrency": 0, "xudpProxyUDP443": "allow"}, "streamSettings": map[string]any{"network": "xdrive", "security": "tls", "tlsSettings": tls, "xdriveSettings": map[string]any{"service": "google-drive", "remoteFolder": "synthetic-folder", "secrets": []any{"synthetic-private-client", "synthetic-private-secret", "synthetic-private-refresh"}, "segmentBytes": 0, "flushIntervalMs": 0, "pollIntervalMs": 20, "maxPollIntervalMs": 200, "sessionTtlSeconds": 3600, "concurrency": 0, "eagerWindowMs": 0, "holeTimeoutMs": 10, "xmux": map[string]any{"maxConnections": "1-2", "hKeepAlivePeriod": 0}}}}
}

func parseNativeFixture(t *testing.T, out map[string]any) Record {
	t.Helper()
	b, _ := json.Marshal(out)
	r, err := ParseLink(string(b), "native-test", nil)
	if err != nil {
		t.Fatal("native import rejected:", err)
	}
	return r
}

func TestImportRoundTrip(t *testing.T) {
	for _, kind := range []string{"xdrive", "masque"} {
		t.Run(kind, func(t *testing.T) {
			want := nativeTestOutbound(kind)
			r := parseNativeFixture(t, want)
			if RequiredCore(r) != "26.11.9" {
				t.Fatal("minimum core missing")
			}
			e := newEnv(t)
			if err := WriteRecord(e.d, r); err != nil {
				t.Fatal(err)
			}
			read, err := ReadRecord(e.d, "native-test")
			if err != nil {
				t.Fatal(err)
			}
			b, _ := Marshal(read)
			restored, err := DecodeBackupRecord(b, "native-test")
			if err != nil {
				t.Fatal(err)
			}
			out, err := Outbound(restored)
			if err != nil {
				t.Fatal(err)
			}
			want["tag"] = "proxy"
			if !jsonValueEqual(out, want) {
				t.Fatal("native outbound changed during import/storage/backup")
			}
			masked, _ := Marshal(MaskRecord(restored))
			if strings.Contains(string(masked), "synthetic-private") {
				t.Fatal("private field exposed")
			}
			key, _ := CanonicalKey(r)
			changed := nativeTestOutbound(kind)
			if kind == "xdrive" {
				get(changed, "streamSettings", "xdriveSettings").(map[string]any)["secrets"] = []any{"other", "other", "other"}
			} else {
				get(changed, "streamSettings", "masqueSettings").(map[string]any)["pass"] = "other"
			}
			r2 := parseNativeFixture(t, changed)
			key2, _ := CanonicalKey(r2)
			if key == key2 {
				t.Fatal("private credentials missing from identity")
			}
		})
	}
}

func TestXDriveInvalidCombinations(t *testing.T) {
	cases := []struct {
		name string
		edit func(map[string]any)
	}{
		{"disabled-mux", func(o map[string]any) { o["mux"].(map[string]any)["enabled"] = false }},
		{"negative-mux", func(o map[string]any) { o["mux"].(map[string]any)["concurrency"] = -1 }},
		{"skip", func(o map[string]any) { o["mux"].(map[string]any)["xudpProxyUDP443"] = "skip" }},
		{"no-encryption", func(o map[string]any) {
			get(o, "settings", "vnext").([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)["encryption"] = "none"
		}},
		{"flow", func(o map[string]any) {
			get(o, "settings", "vnext").([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)["flow"] = "xtls-rprx-vision"
		}},
		{"reality", func(o map[string]any) { o["streamSettings"].(map[string]any)["security"] = "reality" }},
		{"secrets", func(o map[string]any) {
			get(o, "streamSettings", "xdriveSettings").(map[string]any)["secrets"] = []any{"synthetic-private-only-one"}
		}},
		{"xmux-conflict", func(o map[string]any) {
			get(o, "streamSettings", "xdriveSettings", "xmux").(map[string]any)["maxConcurrency"] = 1
		}},
		{"local-path", func(o map[string]any) {
			s := get(o, "streamSettings", "xdriveSettings").(map[string]any)
			s["service"] = "local"
			s["remoteFolder"] = "/etc"
			delete(s, "secrets")
		}},
		{"uint-overflow", func(o map[string]any) {
			get(o, "streamSettings", "xdriveSettings").(map[string]any)["segmentBytes"] = uint64(1) << 32
		}},
		{"unknown", func(o map[string]any) {
			get(o, "streamSettings", "xdriveSettings").(map[string]any)["command"] = "synthetic-private-command"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := nativeTestOutbound("xdrive")
			c.edit(o)
			b, _ := json.Marshal(o)
			_, err := ParseLink(string(b), "test", nil)
			if err == nil {
				t.Fatal("invalid native profile accepted")
			}
			if strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("error exposes value")
			}
		})
	}
}

func TestMasqueInvalidCombinations(t *testing.T) {
	for _, change := range []map[string]any{
		{"path": "/bad/{unsupported}"}, {"user": "bad:user"}, {"headers": map[string]any{"Host": "other"}},
		{"headers": map[string]any{"Capsule-Protocol": "?1"}}, {"headers": map[string]any{"Authorization": "synthetic-private-token"}},
		{"warp": map[string]any{"privateKey": "synthetic-private-invalid", "publicKey": "invalid", "address": []any{"10.0.0.1"}}},
		{"xmux": map[string]any{"maxConnections": 1, "maxConcurrency": 2}},
	} {
		o := nativeTestOutbound("masque")
		s := get(o, "streamSettings", "masqueSettings").(map[string]any)
		for k, v := range change {
			s[k] = v
		}
		b, _ := json.Marshal(o)
		_, err := ParseLink(string(b), "test", nil)
		if err == nil {
			t.Fatal("invalid MASQUE accepted")
		}
		if strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("private value in error")
		}
	}
}

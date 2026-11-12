package clashschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPinnedKnownProtocolFields(t *testing.T) {
	for _, typ := range []string{"ss", "ssr", "socks5", "http", "vmess", "vless", "snell", "trojan", "hysteria", "hysteria2", "wireguard", "tuic", "shadowquic", "gost-relay", "ssh", "mieru", "anytls", "sudoku", "masque", "trusttunnel", "openvpn", "tailscale", "zerotier", "easytier"} {
		t.Run(typ, func(t *testing.T) {
			// Structural validation must not invent a universal endpoint shape:
			// mesh nodes do not have server/port; runtime adapters validate it.
			n, err := Parse(map[string]any{"type": typ, "name": "synthetic"})
			if err != nil || n.Type != typ || n.Fields["name"] != "synthetic" {
				t.Fatalf("known type not parsed: %v", err)
			}
		})
	}
}

func TestPinnedTypedNestedFieldsAndPresence(t *testing.T) {
	input := map[string]any{"type": "vless", "name": "synthetic", "port": "443", "mptcp": false,
		"xhttp-opts": map[string]any{"download-settings": map[string]any{"tls": false, "port": json.Number("0"), "headers": map[string]any{}, "alpn": []any{}, "sni-new": "private-extension"}},
		"smux":       map[string]any{"enabled": false, "brutal-opts": map[string]any{"up": json.Number("20")}}}
	n, e := Parse(input)
	if e != nil {
		t.Fatal(e)
	}
	if n.Fields["port"] != json.Number("443") || n.Fields["mptcp"] != false {
		t.Fatal("scalar conversion/presence lost")
	}
	d := n.Fields["xhttp-opts"].(map[string]any)["download-settings"].(map[string]any)
	if d["tls"] != false || d["port"] != json.Number("0") || len(d["headers"].(map[string]any)) != 0 || len(d["alpn"].([]any)) != 0 {
		t.Fatal("explicit zero/false/empty lost")
	}
	if d["sni-new"] != "private-extension" || len(n.UnknownPaths) != 1 || strings.Contains(n.UnknownPaths[0], "sni-new") {
		t.Fatal("extension preservation/privacy")
	}
	if n.Fields["smux"].(map[string]any)["brutal-opts"].(map[string]any)["up"] != "20" {
		t.Fatal("numeric bandwidth spelling")
	}
	d["tls"] = true
	if input["xhttp-opts"].(map[string]any)["download-settings"].(map[string]any)["tls"] != false {
		t.Fatal("caller input mutated")
	}
}

func TestPinnedSchemaRejectsInvalidFields(t *testing.T) {
	for _, fields := range []map[string]any{
		{"type": "tuic", "port": json.Number("1.5")}, {"type": "tuic", "port": json.Number("9223372036854775808")},
		{"type": "vless", "tls": "true"}, {"type": "vless", "xhttp-opts": []any{}},
		{"type": "vmess", "mkcp-opts": map[string]any{"mtu": json.Number("-1")}},
		{"type": "http", "headers": map[string]any{"X-Private": "ok", "X-Wrong": true}},
		{"type": "tuic", "alpn": []any{true}}, {"type": "tuic", "sni": nil},
		{"type": "vless", "client-fingerprint": "chrome", "client_fingerprint": "firefox"},
		{"type": "unknown-private-type"}, {"type": "direct"},
	} {
		if _, e := Parse(fields); e == nil {
			t.Fatal("malformed/policy node admitted")
		} else if strings.Contains(e.Error(), "private") || strings.Contains(e.Error(), "firefox") {
			t.Fatal("private value in diagnostic")
		}
	}
}

func TestPinnedAliasesBytesAndUnknownFields(t *testing.T) {
	n, e := Parse(map[string]any{"type": "wireguard", "remote_dns_resolve": false, "reserved": "AQID", "private-key": "synthetic", "unknown-secret-key": map[string]any{"payload": "secret"}})
	if e != nil {
		t.Fatal(e)
	}
	if n.Fields["remote-dns-resolve"] != false || len(n.Fields["reserved"].([]any)) != 3 {
		t.Fatal("alias/byte-array")
	}
	if len(n.UnknownPaths) != 1 || strings.Contains(n.UnknownPaths[0], "secret") {
		t.Fatal("unknown field privacy")
	}
}

func TestPinnedCaseInsensitiveAliases(t *testing.T) {
	n, e := Parse(map[string]any{"type": "vless", "SERVERNAME": "test.invalid", "Client_Fingerprint": "chrome", "Tls": false})
	if e != nil {
		t.Fatal(e)
	}
	if n.Fields["servername"] != "test.invalid" || n.Fields["client-fingerprint"] != "chrome" || n.Fields["tls"] != false || len(n.UnknownPaths) != 0 {
		t.Fatal("Mihomo case-insensitive keys lost")
	}
	if _, e := Parse(map[string]any{"type": "vless", "tls": false, "TLS": true}); e == nil {
		t.Fatal("conflicting aliases accepted")
	}
}

func TestPinnedPrivateExtensionsBounded(t *testing.T) {
	m := map[string]any{"type": "vless"}
	m["loop"] = m
	if _, e := Parse(m); e == nil {
		t.Fatal("cyclic extension")
	}
	m = map[string]any{"type": "vless", "extension": strings.Repeat("x", 256*1024+1)}
	if _, e := Parse(m); e == nil {
		t.Fatal("oversize extension")
	}
	if _, e := Parse(map[string]any{"type": "ss", "plugin": "v2ray-plugin", "plugin-opts": map[string]any{"tls": "true"}}); e == nil {
		t.Fatal("untyped known plugin")
	}
	if _, e := Parse(map[string]any{"type": "ss", "plugin": "v2ray-plugin", "plugin-opts": map[string]any{"tls": true, "mode": "websocket"}}); e != nil {
		t.Fatal(e)
	}
}

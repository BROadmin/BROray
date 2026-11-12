package subscriptions

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/clashimport"
)

// Source pins: Mihomo 0455d0d28c4526937380505153e65a843d8612e8,
// Xray 676eaf66f8f05d4d558ee7a74b4736623d3feeb9 (v26.11.9).
// Each positive contract below names an existing target implementation; JSON
// and private backup checks do not by themselves establish live interoperability.
func clashEquivalent357Clone(t *testing.T, p map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func clashEquivalent357Run(t *testing.T, p, want map[string]any, variant func(map[string]any), privateValues ...string) {
	t.Helper()
	r := clashMapped357Roundtrip(t, p, want)
	config, err := servers.XrayConfig(r, servers.GenSettings{Listen: "127.0.0.1"}, "synthetic-equivalent-357")
	if err != nil {
		t.Fatalf("mapped client cannot generate Xray configuration: %v", err)
	}
	var generated map[string]any
	if err := json.Unmarshal(config, &generated); err != nil {
		t.Fatal("invalid generated Xray JSON")
	}
	for path, expected := range want {
		actual, exists := clashMapped357At(generated, "outbounds.0."+path)
		if !exists || !jsonEqual(actual, expected) {
			t.Errorf("generated Xray lost equivalent setting %s", path)
		}
	}
	key, err := servers.CanonicalKey(r)
	if err != nil {
		t.Fatal(err)
	}
	renamed := clashEquivalent357Clone(t, p)
	renamed["name"] = "synthetic-renamed-357"
	renameRecord, _ := clashFull357Record(t, renamed)
	renameKey, err := servers.CanonicalKey(renameRecord)
	if err != nil || key != renameKey {
		t.Fatal("display name changed connection identity")
	}
	changed := clashEquivalent357Clone(t, p)
	variant(changed)
	changedRecord, _ := clashFull357Record(t, changed)
	if _, err := servers.Outbound(changedRecord); err != nil {
		t.Fatal("semantic identity variant unexpectedly became unavailable")
	}
	changedKey, err := servers.CanonicalKey(changedRecord)
	if err != nil || changedKey == key {
		t.Fatal("effective imported parameter is missing from connection identity")
	}
	private, err := servers.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(servers.MaskRecord(r))
	for _, value := range privateValues {
		if !bytes.Contains(private, []byte(value)) || bytes.Contains(public, []byte(value)) {
			t.Fatal("imported credential/header did not retain its private-only boundary")
		}
	}
}

func clashEquivalent357Unavailable(t *testing.T, p map[string]any) {
	t.Helper()
	in, err := clashimport.ConvertNode(p, importChecks())
	if err != nil || in.Unavailable == nil || in.Capability == nil || in.Capability.Kind == "" {
		t.Fatalf("valid contextual capability requires explicit retention: %v", err)
	}
	if in.Link != "" || len(in.ClientFields) != 0 || !jsonEqual(in.Unavailable.Fields, p) {
		t.Fatal("contextual constraint was stripped while mapping its otherwise compatible fields")
	}
	clashCapability357Persist(t, p)
}

func clashEquivalent357ECH(t *testing.T, id byte) string {
	t.Helper()
	// RFC ECHConfigList: X25519, HKDF-SHA256/AES-128-GCM, synthetic public name.
	key, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x35}, 32))
	if err != nil {
		t.Fatal(err)
	}
	lengthPrefix := func(b []byte) []byte {
		return append(binary.BigEndian.AppendUint16(nil, uint16(len(b))), b...)
	}
	content := []byte{id, 0, 0x20}
	content = append(content, lengthPrefix(key.PublicKey().Bytes())...)
	content = append(content, 0, 4, 0, 1, 0, 1, 0)
	name := "public.example.test"
	content = append(content, byte(len(name)))
	content = append(content, name...)
	content = append(content, 0, 0)
	config := append([]byte{0xfe, 0x0d}, lengthPrefix(content)...)
	return base64.StdEncoding.EncodeToString(lengthPrefix(config))
}

func TestClashEquivalent357SSXChaCha(t *testing.T) {
	// sing-shadowsocks2 v0.2.8 shadowaead/method.go and pinned Xray
	// infra/conf/shadowsocks.go cipherFromString select the same XChaCha AEAD.
	p := clashMapped357Base("ss")
	p["cipher"] = "xchacha20-ietf-poly1305"
	clashEquivalent357Run(t, p, map[string]any{
		"settings.servers.0.method":   "xchacha20-poly1305",
		"settings.servers.0.password": p["password"],
	}, func(p map[string]any) { p["cipher"] = "aes-256-gcm" }, p["password"].(string))
}

func TestClashEquivalent357StaticECH(t *testing.T) {
	// Mihomo adapter/outbound/ech.go Parse uses Config verbatim when enabled;
	// Xray infra/conf/transport_security.go maps echConfigList to TLS runtime.
	p := clashMapped357Base("vless")
	p["tls"] = true
	config := clashEquivalent357ECH(t, 0x35)
	p["ech-opts"] = map[string]any{"enable": true, "config": config}
	clashEquivalent357Run(t, p, map[string]any{"streamSettings.tlsSettings.echConfigList": config},
		func(p map[string]any) { p["ech-opts"].(map[string]any)["config"] = clashEquivalent357ECH(t, 0x36) })
}

func clashEquivalent357XHTTP() map[string]any {
	p := clashMapped357Base("vless")
	p["tls"], p["servername"], p["network"] = true, "upload.example.test", "xhttp"
	p["alpn"] = []any{"h2"}
	p["xhttp-opts"] = map[string]any{"host": "upload.example.test", "path": "/upload", "mode": "packet-up",
		"headers":           map[string]any{"X-Token": "synthetic-download-secret-357"},
		"download-settings": map[string]any{"server": "download.example.test", "port": 8443, "host": "download-front.example.test", "path": "/download"}}
	return p
}

func TestClashEquivalent357XHTTPDownload(t *testing.T) {
	// Mihomo adapter/outbound/vless.go lines720+: absent pointer fields inherit
	// upload TLS/ALPN/headers. Xray SplitHTTPConfig.DownloadSettings is a stream;
	// explicit inherited values are necessary, not just renamed download keys.
	p := clashEquivalent357XHTTP()
	const prefix = "streamSettings.xhttpSettings.extra.downloadSettings."
	clashEquivalent357Run(t, p, map[string]any{
		prefix + "address": "download.example.test", prefix + "port": 8443,
		prefix + "network": "xhttp", prefix + "security": "tls",
		prefix + "tlsSettings.serverName": "upload.example.test", prefix + "tlsSettings.alpn": []any{"h2"},
		prefix + "xhttpSettings.host": "download-front.example.test", prefix + "xhttpSettings.path": "/download",
		prefix + "xhttpSettings.extra.headers": map[string]any{"X-Token": "synthetic-download-secret-357"},
	}, func(p map[string]any) {
		p["xhttp-opts"].(map[string]any)["download-settings"].(map[string]any)["port"] = 9443
	}, "synthetic-download-secret-357")
}

func clashEquivalent357HY2() map[string]any {
	p := clashMapped357Base("hysteria2")
	p["password"] = "synthetic-secret-357"
	return p
}

func TestClashEquivalent357HY2Settings(t *testing.T) {
	// Mihomo adapter/outbound/hysteria2.go ClientOptions/QUICConfig and Xray
	// infra/conf/transport_finalmask.go QuicParamsConfig, Salamander and UDPHop.
	t.Run("quic", func(t *testing.T) {
		p := clashEquivalent357HY2()
		p["up"], p["down"], p["bbr-profile"] = "8 Mbps", "16 Mbps", "standard"
		p["initial-stream-receive-window"], p["max-stream-receive-window"] = 65536, 131072
		p["initial-connection-receive-window"], p["max-connection-receive-window"] = 262144, 524288
		const prefix = "streamSettings.finalmask.quicParams."
		clashEquivalent357Run(t, p, map[string]any{
			"streamSettings.tlsSettings.serverName": "edge.example.test",
			prefix + "brutalUp":                     "8 Mbps", prefix + "brutalDown": "16 Mbps", prefix + "bbrProfile": "standard",
			prefix + "initStreamReceiveWindow": 65536, prefix + "maxStreamReceiveWindow": 131072,
			prefix + "initConnectionReceiveWindow": 262144, prefix + "maxConnectionReceiveWindow": 524288,
		}, func(p map[string]any) { p["max-stream-receive-window"] = 196608 }, "synthetic-secret-357")
	})
	for _, obfs := range []string{"salamander", "gecko"} {
		t.Run(obfs, func(t *testing.T) {
			p := clashEquivalent357HY2()
			p["obfs"], p["obfs-password"] = obfs, "synthetic-obfs-secret-357"
			want := map[string]any{"streamSettings.tlsSettings.serverName": "edge.example.test", "streamSettings.finalmask.udp.0.type": "salamander", "streamSettings.finalmask.udp.0.settings.password": "synthetic-obfs-secret-357"}
			if obfs == "gecko" {
				p["obfs-min-packet-size"], p["obfs-max-packet-size"] = 512, 1200
				want["streamSettings.finalmask.udp.0.settings.packetSize"] = "512-1200"
			}
			clashEquivalent357Run(t, p, want, func(p map[string]any) { p["obfs-password"] = "synthetic-obfs-variant-357" }, "synthetic-obfs-secret-357")
		})
	}
	t.Run("port-hop", func(t *testing.T) {
		p := clashEquivalent357HY2()
		p["ports"], p["hop-interval"] = "443,8443-8444", "5-10"
		clashEquivalent357Run(t, p, map[string]any{
			"streamSettings.tlsSettings.serverName": "edge.example.test",
			"streamSettings.finalmask.udp.0.type":   "udphop", "streamSettings.finalmask.udp.0.settings.mode": "intervalremote",
			"streamSettings.finalmask.udp.0.settings.remotePorts": "443,8443-8444", "streamSettings.finalmask.udp.0.settings.interval": "5-10",
		}, func(p map[string]any) { p["hop-interval"] = "6-10" })
	})
}

func TestClashEquivalent357MASQUEBasicWARP(t *testing.T) {
	// Mihomo adapter/outbound/masque.go uses P-256 DER keys/IP prefixes;
	// Xray infra/conf/transport_method.go MasqueWarpConfig accepts the same forms.
	p := clashCapability357Remote(t)["masque"]
	p["sni"] = "warp.example.test"
	clashEquivalent357Run(t, p, map[string]any{
		"protocol": "masque", "settings.address": "edge.example.test", "settings.port": 443,
		"streamSettings.network": "masque", "streamSettings.security": "tls", "streamSettings.tlsSettings.alpn": []any{"h3"},
		"streamSettings.tlsSettings.serverName":         "warp.example.test",
		"streamSettings.masqueSettings.warp.privateKey": p["private-key"], "streamSettings.masqueSettings.warp.publicKey": p["public-key"],
		"streamSettings.masqueSettings.warp.address": []any{"10.8.0.2/32"},
	}, func(p map[string]any) { p["ip"] = "10.8.0.3/32" }, p["private-key"].(string))
}

func clashEquivalent357WS() map[string]any {
	p := clashMapped357Base("vless")
	p["network"] = "ws"
	p["ws-opts"] = map[string]any{"path": "/socket?token=357", "max-early-data": 1024, "early-data-header-name": "Sec-WebSocket-Protocol"}
	return p
}

func TestClashEquivalent357WSEarlyData(t *testing.T) {
	// Mihomo transport/vmess/websocket.go and Xray websocket/dialer.go both
	// use RawURLEncoding with Sec-WebSocket-Protocol. Xray's conf builder takes
	// ed from the path query, removes it on the wire and retains other query keys.
	p := clashEquivalent357WS()
	clashEquivalent357Run(t, p, map[string]any{"streamSettings.wsSettings.path": "/socket?ed=1024&token=357"},
		func(p map[string]any) { p["ws-opts"].(map[string]any)["max-early-data"] = 2048 })
}

func clashEquivalent357Plugin() map[string]any {
	p := clashMapped357Base("ss")
	p["plugin"] = "v2ray-plugin"
	p["plugin-opts"] = map[string]any{"mode": "websocket", "mux": false, "host": "plugin.example.test", "path": "/plugin",
		"tls": true, "headers": map[string]any{"X-Token": "synthetic-plugin-secret-357"}}
	return p
}

func TestClashEquivalent357PluginWSMuxFalse(t *testing.T) {
	// Mihomo transport/v2ray-plugin/websocket.go delegates directly to vmess
	// WebSocket when Mux is false. Mux is true by default in the outbound adapter.
	p := clashEquivalent357Plugin()
	clashEquivalent357Run(t, p, map[string]any{
		"protocol": "shadowsocks", "streamSettings.network": "ws", "streamSettings.security": "tls",
		"streamSettings.tlsSettings.serverName": "plugin.example.test", "streamSettings.tlsSettings.alpn": []any{"http/1.1"},
		"streamSettings.wsSettings.host": "plugin.example.test", "streamSettings.wsSettings.path": "/plugin",
		"streamSettings.wsSettings.headers": map[string]any{"X-Token": "synthetic-plugin-secret-357"},
	}, func(p map[string]any) { p["plugin-opts"].(map[string]any)["path"] = "/plugin-variant" }, "synthetic-plugin-secret-357")
}

func TestClashEquivalent357ContextMustRemainUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func() map[string]any
	}{
		{"ws-custom-header", func() map[string]any {
			p := clashEquivalent357WS()
			p["ws-opts"].(map[string]any)["early-data-header-name"] = "X-ED"
			return p
		}},
		{"ws-path-suffix", func() map[string]any {
			p := clashEquivalent357WS()
			p["ws-opts"].(map[string]any)["early-data-header-name"] = ""
			return p
		}},
		{"plugin-mux", func() map[string]any {
			p := clashEquivalent357Plugin()
			p["plugin-opts"].(map[string]any)["mux"] = true
			return p
		}},
		{"plugin-mux-default", func() map[string]any {
			p := clashEquivalent357Plugin()
			delete(p["plugin-opts"].(map[string]any), "mux")
			return p
		}},
		{"ech-dynamic-query", func() map[string]any {
			p := clashMapped357Base("vless")
			p["tls"] = true
			p["ech-opts"] = map[string]any{"enable": true, "query-server-name": "ech.example.test"}
			return p
		}},
		{"download-smux", func() map[string]any {
			p := clashEquivalent357XHTTP()
			p["smux"] = map[string]any{"enabled": true, "protocol": "smux"}
			return p
		}},
		{"hy2-cwnd", func() map[string]any { p := clashEquivalent357HY2(); p["cwnd"] = 16; return p }},
		{"masque-mtu", func() map[string]any { p := clashCapability357Remote(t)["masque"]; p["mtu"] = 1300; return p }},
	} {
		t.Run(tc.name, func(t *testing.T) { clashEquivalent357Unavailable(t, tc.make()) })
	}
}

func TestClashEquivalent357MalformedKnownCannotHide(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func() map[string]any
	}{
		{"cipher", func() map[string]any { p := clashMapped357Base("ss"); p["cipher"] = "bad-cipher"; return p }},
		{"ech", func() map[string]any {
			p := clashMapped357Base("vless")
			p["tls"] = true
			p["ech-opts"] = map[string]any{"enable": true, "config": "%%%"}
			return p
		}},
		{"download-port", func() map[string]any {
			p := clashEquivalent357XHTTP()
			p["xhttp-opts"].(map[string]any)["download-settings"].(map[string]any)["port"] = 70000
			return p
		}},
		{"download-stream-one", func() map[string]any {
			p := clashEquivalent357XHTTP()
			p["xhttp-opts"].(map[string]any)["mode"] = "stream-one"
			return p
		}},
		{"hy2-obfs", func() map[string]any {
			p := clashEquivalent357HY2()
			p["obfs"], p["obfs-password"] = "not-a-method", "synthetic-secret-357"
			return p
		}},
		{"masque-key", func() map[string]any { p := clashCapability357Remote(t)["masque"]; p["private-key"] = "%%%"; return p }},
		{"ws-limit", func() map[string]any {
			p := clashEquivalent357WS()
			p["ws-opts"].(map[string]any)["max-early-data"] = -1
			return p
		}},
		{"plugin-mode", func() map[string]any {
			p := clashEquivalent357Plugin()
			p["plugin-opts"].(map[string]any)["mode"] = "quic"
			return p
		}},
	} {
		for _, extension := range []bool{false, true} {
			t.Run(tc.name+"/unknown="+strings.ToLower(map[bool]string{true: "true", false: "false"}[extension]), func(t *testing.T) {
				p := tc.make()
				if extension {
					p["private-extension-name"] = map[string]any{"enabled": true}
				}
				in, err := clashimport.ConvertNode(p, importChecks())
				if err == nil || in.Unavailable != nil || in.Link != "" {
					t.Fatal("malformed known parameter became a valid unavailable/runnable profile")
				}
			})
		}
	}
}

func TestClashEquivalent357InsecureTLSRemovedByCore(t *testing.T) {
	// Pinned infra/conf/transport_security.go lines362-364 explicitly returns
	// PrintRemovedFeatureError for AllowInsecure, not a BROray-only policy veto.
	p := clashMapped357Base("vless")
	p["tls"], p["skip-cert-verify"] = true, true
	clashCapability357Assert(t, p, "core-removed")
	clashCapability357Persist(t, p)
}

package servers

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/xrayid"
)

// Contract source: XTLS/Xray-core commit 676eaf66f8f05d4d558ee7a74b4736623d3feeb9.
// These tests exercise JSON -> private record -> outbound, not core or network I/O.
func fullXray357Base(protocol string) map[string]any {
	settings := map[string]any{"address": "edge.example.test", "port": 443}
	switch protocol {
	case "vless":
		settings["id"] = "00000000-0000-4000-8000-000000000357"
		settings["encryption"] = "none"
	case "vmess":
		settings["id"] = "00000000-0000-4000-8000-000000000357"
		settings["security"] = "auto"
	case "http", "socks":
		settings["user"], settings["pass"] = "fixture-user", "fixture-password"
	case "shadowsocks":
		settings["method"], settings["password"] = "chacha20-ietf-poly1305", "fixture-password"
	case "trojan":
		settings["password"] = "fixture-password"
	}
	return map[string]any{
		"protocol": protocol, "tag": "full-xray-357", "settings": settings,
		"streamSettings": map[string]any{"network": "raw", "security": "tls",
			"tlsSettings": map[string]any{"serverName": "edge.example.test", "fingerprint": "chrome"}},
	}
}

func fullXray357JSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal("cannot encode synthetic fixture")
	}
	return string(b)
}

func fullXray357Parse(t *testing.T, out map[string]any) Record {
	t.Helper()
	r, err := ParseLink(fullXray357JSON(t, out), "full-xray-357", nil)
	if err != nil {
		t.Fatalf("genuine client JSON rejected: %v", err)
	}
	if err := Validate(r, "full-xray-357"); err != nil {
		t.Fatal("accepted JSON produced an invalid stored record")
	}
	return r
}

func fullXray357At(v any, path string) (any, bool) {
	for _, key := range strings.Split(path, ".") {
		switch m := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = m[key]
			if !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(m) {
				return nil, false
			}
			v = m[i]
		default:
			return nil, false
		}
	}
	return v, true
}

func fullXray357Assert(t *testing.T, out map[string]any, path string, want any) {
	t.Helper()
	got, found := fullXray357At(out, path)
	if !found || fullXray357JSON(t, got) != fullXray357JSON(t, want) {
		t.Fatalf("client field was lost or changed: %s (present=%t)", path, found)
	}
}

func fullXray357Roundtrip(t *testing.T, r Record, want map[string]any) map[string]any {
	t.Helper()
	out, err := Outbound(r)
	if err != nil {
		t.Fatal("accepted client record cannot generate outbound")
	}
	for path, value := range want {
		fullXray357Assert(t, out, path, value)
	}
	b, err := Marshal(r)
	if err != nil {
		t.Fatal("cannot serialize accepted record")
	}
	restored, err := DecodeBackupRecord(b, "full-xray-357")
	if err != nil {
		t.Fatal("backup rejected accepted client record")
	}
	after, err := Outbound(restored)
	if err != nil || fullXray357JSON(t, out) != fullXray357JSON(t, after) {
		t.Fatal("backup changed effective outbound")
	}
	return out
}

func fullXray357Reject(t *testing.T, out map[string]any) {
	t.Helper()
	r, err := ParseLink(fullXray357JSON(t, out), "full-xray-357", nil)
	if err == nil || r != nil {
		t.Fatal("invalid or unresolved client field must explicitly reject the node")
	}
	if strings.Contains(err.Error(), "synthetic-private-value") {
		t.Fatal("rejection disclosed a private field value")
	}
}

func TestFullXray357TLSClientFields(t *testing.T) {
	fields := []struct {
		name  string
		value any
	}{
		{"enableSessionResumption", true}, {"enableSessionResumption", false},
		{"disableSystemRoot", true}, {"disableSystemRoot", false},
		{"useSystemCA", true}, {"useSystemCA", false},
		{"minVersion", "1.2"}, {"maxVersion", "1.3"},
		{"cipherSuites", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
		{"curvePreferences", []any{"X25519", "CurveP256"}},
		{"echSockopt", map[string]any{"tcpFastOpen": false, "tcpKeepAliveIdle": 30, "domainStrategy": "UseIPv4"}},
	}
	for i, tc := range fields {
		t.Run(tc.name+"-"+strconv.Itoa(i), func(t *testing.T) {
			o := fullXray357Base("vless")
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)[tc.name] = tc.value
			fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"streamSettings.tlsSettings." + tc.name: tc.value})
		})
	}
	t.Run("StringList-alpn", func(t *testing.T) {
		o := fullXray357Base("vless")
		o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["alpn"] = "h2,http/1.1"
		r := fullXray357Parse(t, o)
		out, err := Outbound(r)
		if err != nil {
			t.Fatal("cannot render ALPN")
		}
		got, ok := fullXray357At(out, "streamSettings.tlsSettings.alpn")
		if !ok || !(fullXray357JSON(t, got) == fullXray357JSON(t, []any{"h2", "http/1.1"}) || got == "h2,http/1.1") {
			t.Fatal("upstream StringList ALPN changed semantics")
		}
	})
}

func fullXray357Certificate(t *testing.T) (any, string) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x35}, ed25519.SeedSize))
	template := &x509.Certificate{
		SerialNumber: big.NewInt(357), Subject: pkix.Name{CommonName: "synthetic-357.example.test"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal("cannot create synthetic certificate")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal("cannot encode synthetic key")
	}
	lines := func(b []byte) []any {
		var out []any
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			out = append(out, line)
		}
		return out
	}
	cert := lines(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyLines := lines(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	return []any{map[string]any{"certificate": cert, "key": keyLines, "usage": "encipherment",
		"oneTimeLoading": true, "buildChain": false, "ocspStapling": 0}}, keyLines[1].(string)
}

func TestFullXray357InlineTLSCertificatePrivacy(t *testing.T) {
	o := fullXray357Base("vless")
	certs, privateMarker := fullXray357Certificate(t)
	o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"] = certs
	r := fullXray357Parse(t, o)
	// Earlier outbound-preservation PASS was serialization evidence only.
	// The pinned core does not send this certificate during client TLS.
	fullXray357PrivateCertificateRoundtrip(t, r, "tls.certificates", certs, privateMarker)
	if strings.Contains(fullXray357JSON(t, MaskRecord(r)), privateMarker) {
		t.Fatal("public server view exposes inline TLS private key")
	}
}

func TestFullXray357HTTPHeadersAndVMessExperiments(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run("http-legacy-"+strconv.FormatBool(legacy), func(t *testing.T) {
			o := fullXray357Base("http")
			headers := map[string]any{"Authorization": "Bearer synthetic-private-value", "X-Profile": "357"}
			settings := o["settings"].(map[string]any)
			if legacy {
				settings = map[string]any{"servers": []any{map[string]any{"address": "edge.example.test", "port": 443,
					"users": []any{map[string]any{"user": "fixture-user", "pass": "fixture-password"}}}}}
				o["settings"] = settings
			}
			settings["headers"] = headers
			r := fullXray357Parse(t, o)
			fullXray357Roundtrip(t, r, map[string]any{"settings.headers": headers})
			if strings.Contains(fullXray357JSON(t, MaskRecord(r)), "synthetic-private-value") {
				t.Fatal("HTTP authorization header exposed")
			}
		})
	}
	for _, value := range []string{"AuthenticatedLength", "NoTerminationSignal", "AuthenticatedLength|NoTerminationSignal"} {
		t.Run("vmess-"+value, func(t *testing.T) {
			o := fullXray357Base("vmess")
			o["settings"].(map[string]any)["experiments"] = value
			r := fullXray357Parse(t, o)
			out := fullXray357Roundtrip(t, r, nil)
			if _, ok := out["settings"].(map[string]any)["address"]; ok {
				fullXray357Assert(t, out, "settings.experiments", value)
			} else {
				fullXray357Assert(t, out, "settings.vnext.0.users.0.experiments", value)
			}
		})
	}
}

func TestFullXray357OutboundControls(t *testing.T) {
	for i, mux := range []any{
		map[string]any{"enabled": false, "concurrency": 0, "xudpConcurrency": 0, "xudpProxyUDP443": "skip"},
		map[string]any{"enabled": true, "concurrency": 8, "xudpConcurrency": 16, "xudpProxyUDP443": "allow"},
		map[string]any{"enabled": true, "concurrency": -1, "xudpConcurrency": -1, "xudpProxyUDP443": "reject"},
	} {
		t.Run("mux-"+strconv.Itoa(i), func(t *testing.T) {
			o := fullXray357Base("vmess")
			o["mux"] = mux
			fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"mux": mux})
		})
	}
	for _, value := range []string{"AsIs", "UseIP", "UseIPv4", "UseIPv6", "UseIPv4v6", "UseIPv6v4", "ForceIP", "ForceIPv4", "ForceIPv6", "ForceIPv4v6", "ForceIPv6v4"} {
		t.Run("target-"+value, func(t *testing.T) {
			o := fullXray357Base("vless")
			o["targetStrategy"] = value
			out := fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"targetStrategy": value})
			if _, present := fullXray357At(out, "streamSettings.sockopt.domainStrategy"); present {
				t.Fatal("targetStrategy incorrectly translated into proxy-endpoint domainStrategy")
			}
		})
	}
	for _, value := range []string{"192.0.2.10", "2001:db8::10"} {
		t.Run("sendThrough-"+value, func(t *testing.T) {
			o := fullXray357Base("vless")
			o["sendThrough"] = value
			fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"sendThrough": value})
		})
	}
}

func TestFullXray357StreamEndpointAndMethod(t *testing.T) {
	o := fullXray357Base("vless")
	st := o["streamSettings"].(map[string]any)
	st["network"], st["method"] = "raw", "xhttp" // Upstream method wins.
	st["address"], st["port"] = "stream.example.test", 8443
	st["xhttpSettings"] = map[string]any{"path": "/upload", "mode": "packet-up"}
	// Preserve the explicitly shadowed network in disabled private source.
	fullXray357Unavailable(t, o)
	delete(st, "network") // Method alone is an executable transport selection.
	r := fullXray357Parse(t, o)
	out := fullXray357Roundtrip(t, r, map[string]any{"streamSettings.address": "stream.example.test", "streamSettings.port": 8443})
	network, _ := fullXray357At(out, "streamSettings.network")
	if method, present := fullXray357At(out, "streamSettings.method"); present {
		network = method
	}
	if network != "xhttp" && network != "splithttp" {
		t.Fatal("method did not select the effective upstream transport")
	}
	if r["address"] != "edge.example.test" {
		t.Fatal("stream endpoint overwrote protocol endpoint identity")
	}
}

func TestFullXray357RejectInvalidClientFields(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		mutate   func(map[string]any)
	}{
		{"tls-null", "vless", func(o map[string]any) { o["streamSettings"].(map[string]any)["tlsSettings"] = nil }},
		{"tls-bool-type", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["useSystemCA"] = "false"
		}},
		{"tls-bool-null", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["disableSystemRoot"] = nil
		}},
		{"tls-cert-type", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"] = "synthetic-private-value"
		}},
		{"tls-ech-unknown", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["echSockopt"] = map[string]any{"notAField": "synthetic-private-value"}
		}},
		{"tls-ech-invalid-known", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["echSockopt"] = map[string]any{"tcpUserTimeout": "synthetic-private-value"}
		}},
		{"tls-version-conflict", "vless", func(o map[string]any) {
			tls := o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
			tls["minVersion"], tls["maxVersion"] = "1.3", "1.2"
		}},
		{"http-header-null", "http", func(o map[string]any) { o["settings"].(map[string]any)["headers"] = nil }},
		{"http-header-type", "http", func(o map[string]any) {
			o["settings"].(map[string]any)["headers"] = map[string]any{"X-Test": []any{"synthetic-private-value"}}
		}},
		{"http-header-injection", "http", func(o map[string]any) {
			o["settings"].(map[string]any)["headers"] = map[string]any{"X-Test": "synthetic-private-value\r\nInjected: yes"}
		}},
		{"vmess-experiments-null", "vmess", func(o map[string]any) { o["settings"].(map[string]any)["experiments"] = nil }},
		{"vmess-experiments-type", "vmess", func(o map[string]any) { o["settings"].(map[string]any)["experiments"] = []any{"AuthenticatedLength"} }},
		{"mux-null", "vmess", func(o map[string]any) { o["mux"] = nil }},
		{"mux-bool-type", "vmess", func(o map[string]any) { o["mux"] = map[string]any{"enabled": "true"} }},
		{"mux-overflow", "vmess", func(o map[string]any) { o["mux"] = map[string]any{"enabled": true, "concurrency": 32768} }},
		{"mux-udp-policy", "vmess", func(o map[string]any) {
			o["mux"] = map[string]any{"enabled": true, "xudpProxyUDP443": "synthetic-private-value"}
		}},
		{"target-null", "vless", func(o map[string]any) { o["targetStrategy"] = nil }},
		{"target-invalid", "vless", func(o map[string]any) { o["targetStrategy"] = "synthetic-private-value" }},
		{"sendThrough-null", "vless", func(o map[string]any) { o["sendThrough"] = nil }},
		{"sendThrough-domain", "vless", func(o map[string]any) { o["sendThrough"] = "synthetic-private-value.example.test" }},
		{"stream-port-fraction", "vless", func(o map[string]any) { o["streamSettings"].(map[string]any)["port"] = 1.5 }},
		{"stream-port-overflow", "vless", func(o map[string]any) { o["streamSettings"].(map[string]any)["port"] = 65536 }},
		{"stream-address-type", "vless", func(o map[string]any) { o["streamSettings"].(map[string]any)["address"] = 1 }},
		{"stream-method-null", "vless", func(o map[string]any) { o["streamSettings"].(map[string]any)["method"] = nil }},
		{"stream-method-invalid", "vless", func(o map[string]any) { o["streamSettings"].(map[string]any)["method"] = "synthetic-private-value" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := fullXray357Base(tc.protocol)
			tc.mutate(o)
			if tc.name == "tls-ech-unknown" {
				// Integration-six RED used the old rejection-only contract.
				// Unknown source intent is now retained privately and disabled;
				// malformed known socket fields still reject above.
				fullXray357Unavailable(t, o)
			} else {
				fullXray357Reject(t, o)
			}
		})
	}
}

func TestFullXray357RemovedAndUnresolvedCapabilities(t *testing.T) {
	cases := []struct {
		name, protocol string
		mutate         func(map[string]any)
	}{
		{"removed-allowInsecure", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["allowInsecure"] = true
		}},
		{"removed-proxySettings", "vless", func(o map[string]any) { o["proxySettings"] = map[string]any{"tag": "foreign-outbound"} }},
		{"removed-trojan-flow", "trojan", func(o map[string]any) { o["settings"].(map[string]any)["flow"] = "xtls-rprx-vision" }},
		{"unresolved-dialerProxy", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["sockopt"] = map[string]any{"dialerProxy": "foreign-outbound"}
		}},
		{"unresolved-reverse", "vless", func(o map[string]any) {
			o["settings"].(map[string]any)["reverse"] = map[string]any{"tag": "foreign-inbound"}
		}},
		{"unresolved-certificate-path", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"] = []any{map[string]any{"certificateFile": "/synthetic-private-value/ca.pem", "usage": "verify"}}
		}},
		{"unresolved-key-log", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["masterKeyLog"] = "/synthetic-private-value/keys.log"
		}},
		{"unresolved-noise-script", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["finalmask"] = map[string]any{"udp": []any{map[string]any{"type": "noise", "settings": map[string]any{"script": "synthetic-private-value"}}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := fullXray357Base(tc.protocol)
			tc.mutate(o)
			if tc.name == "removed-trojan-flow" {
				// The core removed this protocol value; it is not a valid
				// missing-resource client endpoint.
				fullXray357Reject(t, o)
			} else {
				// The full-import contract now keeps typed unavailable intent;
				// the earlier rejection-only RED/GREEN receipts remain historical.
				fullXray357Unavailable(t, o)
			}
		})
	}
}

func TestFullXray357IdentityIncludesNewClientSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, protocol string
		mutate         func(map[string]any)
	}{
		{"tls-resumption", "vless", func(o map[string]any) {
			o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["enableSessionResumption"] = true
		}},
		{"http-headers", "http", func(o map[string]any) {
			o["settings"].(map[string]any)["headers"] = map[string]any{"X-Test": "variant"}
		}},
		{"vmess-experiments", "vmess", func(o map[string]any) { o["settings"].(map[string]any)["experiments"] = "AuthenticatedLength" }},
		{"mux", "vmess", func(o map[string]any) { o["mux"] = map[string]any{"enabled": true, "concurrency": 8} }},
		{"target-strategy", "vless", func(o map[string]any) { o["targetStrategy"] = "UseIPv4" }},
		{"source-binding", "vless", func(o map[string]any) { o["sendThrough"] = "192.0.2.10" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := fullXray357Parse(t, fullXray357Base(tc.protocol))
			changed := fullXray357Base(tc.protocol)
			tc.mutate(changed)
			variant := fullXray357Parse(t, changed)
			a, ea := CanonicalKey(base)
			b, eb := CanonicalKey(variant)
			if ea != nil || eb != nil || a == b {
				t.Fatal("different effective connection semantics collapsed during deduplication")
			}
		})
	}
}

func fullXray357Download() (map[string]any, map[string]any) {
	o := fullXray357Base("vless")
	d := map[string]any{
		"address": "download.example.test", "port": 8443, "network": "xhttp", "security": "tls",
		"tlsSettings":   map[string]any{"serverName": "download.example.test", "fingerprint": "chrome"},
		"xhttpSettings": map[string]any{"path": "/download", "mode": "stream-one"},
	}
	s := o["streamSettings"].(map[string]any)
	s["network"] = "xhttp"
	s["xhttpSettings"] = map[string]any{"path": "/upload", "mode": "packet-up", "extra": map[string]any{"downloadSettings": d}}
	return o, d
}

func TestFullXray357DownloadClientFields(t *testing.T) {
	const prefix = "streamSettings.xhttpSettings.extra.downloadSettings."
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
		want   map[string]any
	}{
		{"tls", func(d map[string]any) {
			tls := d["tlsSettings"].(map[string]any)
			tls["enableSessionResumption"], tls["minVersion"], tls["useSystemCA"] = true, "1.2", true
			tls["echSockopt"] = map[string]any{"tcpFastOpen": false, "domainStrategy": "UseIPv4"}
		}, map[string]any{"tlsSettings.enableSessionResumption": true, "tlsSettings.minVersion": "1.2", "tlsSettings.useSystemCA": true, "tlsSettings.echSockopt": map[string]any{"tcpFastOpen": false, "domainStrategy": "UseIPv4"}}},
		{"socket", func(d map[string]any) {
			d["sockopt"] = map[string]any{"tcpFastOpen": false, "tcpKeepAliveIdle": 30, "tcpKeepAliveInterval": 15, "domainStrategy": "UseIPv6"}
		}, map[string]any{"sockopt": map[string]any{"tcpFastOpen": false, "tcpKeepAliveIdle": 30, "tcpKeepAliveInterval": 15, "domainStrategy": "UseIPv6"}}},
		{"method", func(d map[string]any) { d["network"], d["method"] = "splithttp", "xhttp" }, map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, d := fullXray357Download()
			tc.mutate(d)
			want := map[string]any{prefix + "address": "download.example.test", prefix + "port": 8443}
			for key, value := range tc.want {
				want[prefix+key] = value
			}
			out := fullXray357Roundtrip(t, fullXray357Parse(t, o), want)
			network, _ := fullXray357At(out, prefix+"network")
			if method, found := fullXray357At(out, prefix+"method"); found {
				network = method
			}
			if network != "xhttp" && network != "splithttp" {
				t.Fatal("download method precedence changed")
			}
		})
	}
}

func TestFullXray357DownloadPrivacyAndIdentity(t *testing.T) {
	o, d := fullXray357Download()
	certs, marker := fullXray357Certificate(t)
	d["tlsSettings"].(map[string]any)["certificates"] = certs
	r := fullXray357Parse(t, o)
	fullXray357PrivateCertificateRoundtrip(t, r, "transport.extra.downloadSettings.tlsSettings.certificates", certs, marker)
	if strings.Contains(fullXray357JSON(t, MaskRecord(r)), marker) {
		t.Fatal("nested download TLS private key exposed")
	}
	first, _ := fullXray357Download()
	second, changed := fullXray357Download()
	changed["tlsSettings"].(map[string]any)["enableSessionResumption"] = true
	a, ea := CanonicalKey(fullXray357Parse(t, first))
	b, eb := CanonicalKey(fullXray357Parse(t, second))
	if ea != nil || eb != nil || a == b {
		t.Fatal("download TLS semantics collapsed during deduplication")
	}
}

func fullXray357PrivateCertificateRoundtrip(t *testing.T, r Record, path string, certs any, marker string) Record {
	t.Helper()
	fullXray357Assert(t, r, path, certs)
	encoded, err := Marshal(r)
	if err != nil {
		t.Fatal("private TLS record could not be saved")
	}
	restored, err := DecodeBackupRecord(encoded, "full-xray-357")
	if err != nil {
		t.Fatal("private TLS backup rejected")
	}
	fullXray357Assert(t, restored, path, certs)
	if strings.Contains(fullXray357JSON(t, MaskRecord(r)), marker) || strings.Contains(fullXray357JSON(t, MaskRecord(restored)), marker) {
		t.Fatal("private client certificate disclosed")
	}
	return restored
}

func TestFullXray357TLSClientCertificateUnavailable(t *testing.T) {
	// Exact-core traffic evidence: trusted server requiring client certificates
	// reports "client didn't provide a certificate". TLSConfig.GetCertificate
	// is a server callback; this core has no outbound client-certificate hook.
	for _, kind := range []string{"root", "download", "realm", "xdns"} {
		t.Run(kind, func(t *testing.T) {
			certs, marker := fullXray357Certificate(t)
			o := fullXray357Base("vless")
			path := "tls.certificates"
			if kind == "root" {
				o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"] = certs
			} else if kind == "download" {
				var d map[string]any
				o, d = fullXray357Download()
				d["tlsSettings"].(map[string]any)["certificates"] = certs
				path = "transport.extra.downloadSettings.tlsSettings.certificates"
			} else {
				var mask map[string]any
				for _, f := range finalMaskSchemaFixtures {
					if f.kind == kind {
						mask = finalMaskFixtureValue(t, f)
						break
					}
				}
				if mask == nil {
					t.Fatal("missing synthetic finalmask fixture")
				}
				settings := mask["udp"].([]any)[0].(map[string]any)["settings"].(map[string]any)
				path = "transport.finalMask.udp.0.settings.tlsConfig.certificates"
				if kind == "xdns" {
					settings = settings["resolvers"].([]any)[0].(map[string]any)
					path = "transport.finalMask.udp.0.settings.resolvers.0.tlsConfig.certificates"
				}
				settings["tlsConfig"].(map[string]any)["certificates"] = certs
				o["streamSettings"] = map[string]any{"network": "kcp", "security": "none", "kcpSettings": map[string]any{}, "finalmask": mask}
			}
			r := fullXray357Parse(t, o)
			restored := fullXray357PrivateCertificateRoundtrip(t, r, path, certs, marker)
			for _, record := range []Record{r, restored} {
				capability := connectionSupport(record)
				if capability["supported"] != false || capability["code"] != "PROTOCOL_NOT_SUPPORTED" {
					t.Fatal("core-unsupported client certificate advertised as runnable")
				}
				if strings.Contains(fullXray357JSON(t, capability), marker) {
					t.Fatal("capability message disclosed private key")
				}
				if out, err := Outbound(record); err == nil || out != nil {
					t.Fatal("required client certificate generated runnable outbound")
				}
				if config, err := XrayConfig(record, GenSettings{}, "certificate"); err == nil || config != nil {
					t.Fatal("required client certificate generated runnable core config")
				}
			}
		})
	}
}

func TestFullXray357TLSVerifyOnlyRemainsRunnable(t *testing.T) {
	for _, kind := range []string{"root", "download"} {
		t.Run(kind, func(t *testing.T) {
			certs, _ := fullXray357Certificate(t)
			cert := certs.([]any)[0].(map[string]any)
			cert["usage"] = "verify"
			delete(cert, "key")
			o := fullXray357Base("vless")
			path := "streamSettings.tlsSettings.certificates"
			if kind == "root" {
				o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"] = certs
			} else {
				var d map[string]any
				o, d = fullXray357Download()
				d["tlsSettings"].(map[string]any)["certificates"] = certs
				path = "streamSettings.xhttpSettings.extra.downloadSettings.tlsSettings.certificates"
			}
			r := fullXray357Parse(t, o)
			if connectionSupport(r)["supported"] != true {
				t.Fatal("supported CA trust incorrectly made unavailable")
			}
			fullXray357Roundtrip(t, r, map[string]any{path: certs})
		})
	}
}

func TestFullXray357DownloadRejectsInvalidOrUnresolved(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any, map[string]any)
	}{
		{"tls-null", func(o, d map[string]any) { d["tlsSettings"] = nil }},
		{"tls-conflict", func(o, d map[string]any) {
			d["tlsSettings"].(map[string]any)["minVersion"] = "1.3"
			d["tlsSettings"].(map[string]any)["maxVersion"] = "1.2"
		}},
		{"tls-file", func(o, d map[string]any) {
			d["tlsSettings"].(map[string]any)["certificates"] = []any{map[string]any{"certificateFile": "/synthetic-private-value/ca.pem", "usage": "verify"}}
		}},
		{"socket-null", func(o, d map[string]any) { d["sockopt"] = nil }},
		{"socket-type", func(o, d map[string]any) { d["sockopt"] = map[string]any{"tcpFastOpen": "synthetic-private-value"} }},
		{"dialer-reference", func(o, d map[string]any) { d["sockopt"] = map[string]any{"dialerProxy": "synthetic-private-value"} }},
		{"method-null", func(o, d map[string]any) { d["method"] = nil }},
		{"method-invalid", func(o, d map[string]any) { d["method"] = "synthetic-private-value" }},
		{"missing-destination", func(o, d map[string]any) { delete(d, "address") }},
		{"outer-stream-one", func(o, d map[string]any) {
			o["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)["mode"] = "stream-one"
		}},
		{"recursive-download", func(o, d map[string]any) {
			d["xhttpSettings"].(map[string]any)["extra"] = map[string]any{"downloadSettings": map[string]any{"network": "xhttp", "address": "nested.example.test", "port": 443}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, d := fullXray357Download()
			tc.mutate(o, d)
			if tc.name == "tls-file" || tc.name == "dialer-reference" {
				fullXray357Unavailable(t, o)
			} else {
				fullXray357Reject(t, o)
			}
		})
	}
}

func TestFullXray357VLESSPreconnect(t *testing.T) {
	// testpre is consumed by the pinned outbound's preconnect pool. These
	// fixtures only render config; they never start the core or preconnect.
	for _, legacy := range []bool{false, true} {
		t.Run(strconv.FormatBool(legacy), func(t *testing.T) {
			o := fullXray357Base("vless")
			settings := o["settings"].(map[string]any)
			settings["testpre"] = 2
			if legacy {
				o["settings"] = map[string]any{"vnext": []any{map[string]any{"address": settings["address"], "port": settings["port"], "users": []any{map[string]any{"id": settings["id"], "encryption": "none", "testpre": 2}}}}}
			}
			r := fullXray357Parse(t, o)
			out := fullXray357Roundtrip(t, r, nil)
			path := "settings.testpre"
			if _, flat := fullXray357At(out, "settings.address"); !flat {
				path = "settings.vnext.0.users.0.testpre"
			}
			fullXray357Assert(t, out, path, 2)
			a, ea := CanonicalKey(fullXray357Parse(t, fullXray357Base("vless")))
			b, eb := CanonicalKey(r)
			if ea != nil || eb != nil || a == b {
				t.Fatal("preconnect semantics collapsed during deduplication")
			}
		})
	}
	for _, bad := range []any{nil, "synthetic-private-value", -1, 1.5, uint64(4294967296)} {
		o := fullXray357Base("vless")
		o["settings"].(map[string]any)["testpre"] = bad
		fullXray357Reject(t, o)
	}
}

func TestFullXray357WireGuardSourceDefaults(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"protocol": "wireguard", "settings": map[string]any{
			"secretKey": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "address": []any{"192.0.2.2/32"}, "noKernelTun": true,
			"peers": []any{map[string]any{"publicKey": "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", "endpoint": "wg.example.test:51820"}},
		}}
	}
	t.Run("omitted-address", func(t *testing.T) {
		o := base()
		delete(o["settings"].(map[string]any), "address")
		out := fullXray357Roundtrip(t, fullXray357Parse(t, o), nil)
		v, found := fullXray357At(out, "settings.address")
		addresses, ok := v.([]any)
		if !found || !ok || len(addresses) != 2 {
			t.Fatal("upstream default WireGuard addresses lost")
		}
		for i, want := range []string{"10.0.0.1", "fd59:7153:2388:b5fd::1"} {
			text, ok := addresses[i].(string)
			got, err := netip.ParseAddr(strings.Split(text, "/")[0])
			if !ok || err != nil || got != netip.MustParseAddr(want) {
				t.Fatal("upstream default WireGuard address changed")
			}
		}
	})
	t.Run("zero-mtu", func(t *testing.T) {
		o := base()
		o["settings"].(map[string]any)["mtu"] = 0
		fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"settings.mtu": 1420})
	})
	t.Run("explicit-empty-allowedIPs", func(t *testing.T) {
		o := base()
		o["settings"].(map[string]any)["peers"].([]any)[0].(map[string]any)["allowedIPs"] = []any{}
		fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"settings.peers.0.allowedIPs": []any{}})
	})
	t.Run("kernel-preference-false", func(t *testing.T) {
		o := base()
		o["settings"].(map[string]any)["noKernelTun"] = false
		fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"settings.noKernelTun": false})
	})
}

func TestFullXray357XHTTPExtraPrecedence(t *testing.T) {
	// SplitHTTPConfig.Build replaces sibling advanced options with Extra,
	// then restores the outer host/path/mode. Both JSON locations are typed.
	o, _ := fullXray357Download()
	x := o["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
	x["host"], x["path"], x["mode"] = "front.example.test", "/outer", "packet-up"
	x["noSSEHeader"] = true
	x["headers"] = map[string]any{"X-Outer": "ignored-outer-value"}
	x["extra"] = map[string]any{"host": "ignored.example.test", "path": "/ignored", "mode": "stream-one", "noSSEHeader": false,
		"headers": map[string]any{"Authorization": "Bearer synthetic-private-value"}, "xPaddingBytes": "10-20"}
	r := fullXray357Parse(t, o)
	fullXray357Roundtrip(t, r, map[string]any{
		"streamSettings.xhttpSettings.host": "front.example.test", "streamSettings.xhttpSettings.path": "/outer", "streamSettings.xhttpSettings.mode": "packet-up",
		"streamSettings.xhttpSettings.extra.noSSEHeader": false, "streamSettings.xhttpSettings.extra.headers": map[string]any{"Authorization": "Bearer synthetic-private-value"},
		"streamSettings.xhttpSettings.extra.xPaddingBytes": "10-20",
	})
	if strings.Contains(fullXray357JSON(t, MaskRecord(r)), "synthetic-private-value") {
		t.Fatal("XHTTP extra authorization exposed")
	}
	// true is a valid bool, already exercised above as the ignored outer
	// value. The original RED receipt used it incorrectly as an invalid type.
	for _, invalid := range []any{nil, 1, "synthetic-private-value"} {
		bad, _ := fullXray357Download()
		bx := bad["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
		bx["noSSEHeader"], bx["extra"] = invalid, map[string]any{"noSSEHeader": false}
		fullXray357Reject(t, bad)
	}
}

func TestFullXray357XHTTPInt64Declaration(t *testing.T) {
	// This is source declaration preservation only. scMaxBufferedPosts is
	// consumed by the server upload queue, not the pinned client dialer.
	for _, value := range []any{int64(0), int64(2147483648), json.Number("9223372036854775807")} {
		o, _ := fullXray357Download()
		x := o["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
		x["extra"] = map[string]any{"scMaxBufferedPosts": value}
		fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"streamSettings.xhttpSettings.extra.scMaxBufferedPosts": value})
	}
	for _, value := range []any{nil, "synthetic-private-value", 1.5, json.Number("9223372036854775808")} {
		o, _ := fullXray357Download()
		x := o["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
		x["extra"] = map[string]any{"scMaxBufferedPosts": value}
		fullXray357Reject(t, o)
	}
}

func TestFullXray357ProtocolStreamCombinations(t *testing.T) {
	// Pinned HTTP/SOCKS/Shadowsocks clients call the shared transport dialer.
	// These are import/output contracts; wire-level success needs exact-core
	// local-peer tests in addition to core configuration validation.
	for _, tc := range []struct{ protocol, network string }{
		{"http", "ws"}, {"http", "xhttp"}, {"socks", "grpc"}, {"socks", "xhttp"},
		{"shadowsocks", "raw"}, {"shadowsocks", "ws"}, {"shadowsocks", "httpupgrade"},
	} {
		t.Run(tc.protocol+"-"+tc.network, func(t *testing.T) {
			o := fullXray357Base(tc.protocol)
			s := o["streamSettings"].(map[string]any)
			s["network"] = tc.network
			want := map[string]any{"streamSettings.network": tc.network, "streamSettings.security": "tls", "streamSettings.tlsSettings.serverName": "edge.example.test"}
			switch tc.network {
			case "ws", "httpupgrade":
				s[tc.network+"Settings"] = map[string]any{"path": "/transport", "host": "front.example.test", "headers": map[string]any{"X-Stream": "357"}}
				want["streamSettings."+tc.network+"Settings.path"] = "/transport"
				want["streamSettings."+tc.network+"Settings.headers.X-Stream"] = "357"
			case "grpc":
				s["grpcSettings"] = map[string]any{"serviceName": "transport", "authority": "front.example.test", "multiMode": false}
				want["streamSettings.grpcSettings.serviceName"] = "transport"
			case "xhttp":
				s["xhttpSettings"] = map[string]any{"path": "/transport", "mode": "packet-up", "extra": map[string]any{"noSSEHeader": false}}
				want["streamSettings.xhttpSettings.path"] = "/transport"
				want["streamSettings.xhttpSettings.extra.noSSEHeader"] = false
			}
			fullXray357Roundtrip(t, fullXray357Parse(t, o), want)
		})
	}
	for _, protocol := range []string{"http", "socks", "shadowsocks"} {
		t.Run(protocol+"-raw-mask", func(t *testing.T) {
			o := fullXray357Base(protocol)
			s := o["streamSettings"].(map[string]any)
			mask := map[string]any{"tcp": []any{map[string]any{"type": "fragment", "settings": map[string]any{"length": 20, "delay": 0}}}}
			s["finalmask"] = mask
			fullXray357Roundtrip(t, fullXray357Parse(t, o), map[string]any{"streamSettings.finalmask": mask})
		})
		for _, network := range []string{"xdrive", "masque"} {
			o := fullXray357Base(protocol)
			o["streamSettings"].(map[string]any)["network"] = network
			fullXray357Reject(t, o)
		}
	}
}

func fullXray357EmailFixture(protocol string, legacy bool, email any) (map[string]any, string) {
	o := fullXray357Base(protocol)
	if protocol == "shadowsocks" {
		delete(o, "streamSettings")
	}
	s := o["settings"].(map[string]any)
	s["email"] = email
	path := "settings.servers.0.email"
	if protocol == "vless" || protocol == "vmess" {
		path = "settings.vnext.0.users.0.email"
		if legacy {
			account := map[string]any{}
			for key, value := range s {
				if key != "address" && key != "port" {
					account[key] = value
				}
			}
			o["settings"] = map[string]any{"vnext": []any{map[string]any{"address": s["address"], "port": s["port"], "users": []any{account}}}}
		}
	} else if protocol == "http" || protocol == "socks" {
		path = "settings.servers.0.users.0.email"
		if legacy {
			o["settings"] = map[string]any{"servers": []any{map[string]any{"address": s["address"], "port": s["port"], "users": []any{map[string]any{"user": s["user"], "pass": s["pass"], "email": email}}}}}
		}
	} else if legacy {
		o["settings"] = map[string]any{"servers": []any{s}}
	}
	return o, path
}

func TestFullXray357AccountEmailPreservation(t *testing.T) {
	const email = "synthetic-private-value@example.test"
	for _, protocol := range []string{"vless", "vmess", "trojan", "shadowsocks", "http", "socks"} {
		for _, legacy := range []bool{false, true} {
			t.Run(protocol+"-"+strconv.FormatBool(legacy), func(t *testing.T) {
				o, path := fullXray357EmailFixture(protocol, legacy, email)
				r := fullXray357Parse(t, o)
				out := fullXray357Roundtrip(t, r, nil)
				if _, flat := fullXray357At(out, "settings.address"); flat {
					path = "settings.email"
				}
				fullXray357Assert(t, out, path, email)
				if strings.Contains(fullXray357JSON(t, MaskRecord(r)), email) {
					t.Fatal("account email exposed in public view")
				}
				other, _ := fullXray357EmailFixture(protocol, legacy, "different-private-value@example.test")
				a, ea := CanonicalKey(r)
				b, eb := CanonicalKey(fullXray357Parse(t, other))
				if ea != nil || eb != nil || a == b {
					t.Fatal("account email lost in connection identity")
				}
			})
		}
		for _, invalid := range []any{nil, 357, "synthetic-private-value\r\nInjected", strings.Repeat("x", 4097)} {
			o, _ := fullXray357EmailFixture(protocol, false, invalid)
			fullXray357Reject(t, o)
		}
	}
	t.Run("native-xdrive", func(t *testing.T) {
		o := nativeTestOutbound("xdrive")
		settings := o["settings"].(map[string]any)
		if _, flat := settings["address"]; flat {
			settings["email"] = email
		} else {
			settings["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)["email"] = email
		}
		r := fullXray357Parse(t, o)
		out := fullXray357Roundtrip(t, r, nil)
		path := "settings.vnext.0.users.0.email"
		if _, flat := fullXray357At(out, "settings.address"); flat {
			path = "settings.email"
		}
		fullXray357Assert(t, out, path, email)
		if strings.Contains(fullXray357JSON(t, MaskRecord(r)), email) {
			t.Fatal("native account email exposed")
		}
	})
}

func TestFullXray357TLSFingerprintTuningWarning(t *testing.T) {
	for _, fingerprint := range []string{"chrome", "unsafe"} {
		for _, nested := range []bool{false, true} {
			t.Run(fingerprint+"-"+strconv.FormatBool(nested), func(t *testing.T) {
				o := fullXray357Base("vless")
				stream := o["streamSettings"].(map[string]any)
				if nested {
					o, stream = fullXray357Download()
				}
				tls := stream["tlsSettings"].(map[string]any)
				tls["fingerprint"], tls["minVersion"], tls["enableSessionResumption"] = fingerprint, "1.2", true
				r := fullXray357Parse(t, o)
				warnings := strings.ToLower(fullXray357JSON(t, r["importWarnings"]))
				hasWarning := strings.Contains(warnings, "utls")
				if hasWarning != (fingerprint == "chrome") {
					t.Fatal("fingerprint-dependent TLS tuning needs an accurate explicit warning")
				}
				path := "streamSettings.tlsSettings.minVersion"
				if nested {
					path = "streamSettings.xhttpSettings.extra.downloadSettings.tlsSettings.minVersion"
				}
				fullXray357Roundtrip(t, r, map[string]any{path: "1.2"})
			})
		}
	}
}

func TestFullXray357ProtocolStreamURIRealityRequiredFields(t *testing.T) {
	const publicKey = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc"
	for _, base := range []string{
		"http://fixture-user:fixture-password@edge.example.test:443",
		"socks://fixture-user:fixture-password@edge.example.test:443",
		"ss://chacha20-ietf-poly1305:fixture-password@edge.example.test:443",
	} {
		for i, query := range []string{
			"type=raw&security=reality&fp=chrome&pbk=" + publicKey,
			"type=raw&security=reality&fp=chrome&sni=front.example.test",
		} {
			t.Run(strings.Split(base, ":")[0]+"-"+strconv.Itoa(i), func(t *testing.T) {
				r, err := ParseLink(base+"?"+query, "full-xray-357", nil)
				if err == nil || r != nil {
					t.Fatal("REALITY URI without SNI or peer public key accepted")
				}
				if strings.Contains(err.Error(), "fixture-password") {
					t.Fatal("REALITY rejection disclosed credentials")
				}
			})
		}
	}
}

func fullXray357Unavailable(t *testing.T, source map[string]any) {
	t.Helper()
	rec := fullXray357Parse(t, source)
	if !jsonValueEqual(get(rec, "importProfile", "node"), source) {
		t.Fatal("typed unavailable source not retained intact")
	}
	if connectionSupport(rec)["supported"] != false {
		t.Fatal("unavailable source is runnable")
	}
	if _, err := Outbound(rec); err == nil {
		t.Fatal("unavailable source emitted a reduced outbound")
	}
	if _, err := XrayConfig(rec, GenSettings{}, "unavailable"); err == nil {
		t.Fatal("unavailable source emitted executable config")
	}
	masked := fullXray357JSON(t, MaskRecord(rec))
	if strings.Contains(masked, "synthetic-private") || strings.Contains(masked, "importProfile") {
		t.Fatal("unavailable source disclosed private data")
	}
	raw, err := Marshal(rec)
	if err != nil {
		t.Fatal("unavailable source serialization failed")
	}
	restored, err := DecodeBackupRecord(raw, "unavailable")
	if err != nil || !jsonValueEqual(get(restored, "importProfile", "node"), source) || connectionSupport(restored)["supported"] != false {
		t.Fatal("unavailable source backup lost intent or execution gate")
	}
}

func TestFullXray357EscapedAccountID(t *testing.T) {
	const id = "клиент:@#?+357"
	canonical, ok := xrayid.Canonical(id)
	if !ok {
		t.Fatal("invalid synthetic ID fixture")
	}
	for _, protocol := range []string{"vless", "vmess"} {
		t.Run(protocol, func(t *testing.T) {
			o := fullXray357Base(protocol)
			o["settings"].(map[string]any)["id"] = id
			rec := fullXray357Parse(t, o)
			if rec["uuid"] != id {
				t.Fatal("escaped account ID was split or changed")
			}
			want, err := CanonicalKey(rec)
			if err != nil {
				t.Fatal("ID identity failed")
			}
			o["settings"].(map[string]any)["id"] = canonical
			other := fullXray357Parse(t, o)
			got, err := CanonicalKey(other)
			if err != nil || got != want {
				t.Fatal("core-equivalent credential aliases split identity")
			}
			if protocol == "vless" {
				link := "vless://" + url.User(id).String() + "@edge.example.test:443?security=tls&sni=edge.example.test&fp=chrome"
				uri, err := ParseLink(link, "escaped-id", nil)
				if err != nil || uri["uuid"] != id {
					t.Fatal("escaped URI ID not preserved")
				}
				key, err := CanonicalKey(uri)
				if err != nil || key != want {
					t.Fatal("JSON/URI escaped ID identity mismatch")
				}
			}
		})
	}
}

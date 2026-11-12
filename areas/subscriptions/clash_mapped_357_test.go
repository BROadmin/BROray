package subscriptions

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/cgi"
)

// Source contracts: MetaCubeX/mihomo 0455d0d28c4526937380505153e65a843d8612e8
// and XTLS/Xray-core 676eaf66f8f05d4d558ee7a74b4736623d3feeb9.
// These are import/persistence/output contracts, not live interoperability tests.
func clashMapped357Base(kind string) map[string]any {
	p := map[string]any{"name": "synthetic-mapped-357", "type": kind, "server": "edge.example.test", "port": 443}
	switch kind {
	case "vless", "vmess":
		p["uuid"] = "00000000-0000-4000-8000-000000000357"
	case "trojan":
		p["password"] = "synthetic-password-357"
	case "http", "socks5":
		p["username"], p["password"] = "synthetic-user", "synthetic-password-357"
	case "ss":
		p["cipher"], p["password"] = "aes-128-gcm", "synthetic-password-357"
	}
	return p
}

func clashMapped357At(v any, path string) (any, bool) {
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

func clashMapped357Roundtrip(t *testing.T, p, want map[string]any) servers.Record {
	t.Helper()
	r, _ := clashFull357Record(t, p)
	out, err := servers.Outbound(r)
	if err != nil {
		t.Fatalf("directly convertible Clash client cannot generate outbound: %v", err)
	}
	for path, expected := range want {
		actual, exists := clashMapped357At(out, path)
		if !exists || !jsonEqual(actual, expected) {
			t.Errorf("Clash setting lost or changed at %s (present=%t)", path, exists)
		}
	}
	b, err := servers.Marshal(r)
	if err != nil {
		t.Fatal("cannot persist imported client")
	}
	restored, err := servers.DecodeBackupRecord(b, r["id"].(string))
	if err != nil {
		t.Fatalf("private backup rejected imported client: %v", err)
	}
	after, err := servers.Outbound(restored)
	if err != nil || !jsonEqual(out, after) {
		t.Fatal("backup changed effective imported client settings")
	}
	return r
}

func TestClashMapped357SocketFlags(t *testing.T) {
	for _, kind := range []string{"vless", "vmess", "trojan", "ss", "http", "socks5"} {
		for _, enabled := range []bool{false, true} {
			t.Run(kind+"/"+strconv.FormatBool(enabled), func(t *testing.T) {
				p := clashMapped357Base(kind)
				p["tfo"], p["mptcp"] = enabled, enabled
				clashMapped357Roundtrip(t, p, map[string]any{
					"streamSettings.sockopt.tcpFastOpen": enabled,
					"streamSettings.sockopt.tcpMptcp":    enabled,
				})
			})
		}
	}
}

func TestClashMapped357IPVersionOnly(t *testing.T) {
	// Family-only selection must not silently fall back to an unconstrained
	// system lookup after a failed family-specific lookup.
	for _, tc := range []struct{ source, target string }{{"ipv4", "ForceIPv4"}, {"ipv6", "ForceIPv6"}} {
		t.Run(tc.source, func(t *testing.T) {
			p := clashMapped357Base("http")
			p["ip-version"] = tc.source
			clashMapped357Roundtrip(t, p, map[string]any{"streamSettings.sockopt.domainStrategy": tc.target})
		})
	}
}

func TestClashMapped357TLSNamesAndPin(t *testing.T) {
	for _, kind := range []string{"vless", "vmess", "trojan"} {
		t.Run(kind, func(t *testing.T) {
			p := clashMapped357Base(kind)
			p["tls"], p["servername"] = true, "sni.example.test"
			p["name-cert-verify"] = "verify.example.test"
			p["fingerprint"] = strings.Repeat("ab", 32)
			p["client-fingerprint"] = "firefox"
			p["alpn"] = []any{"h2", "http/1.1"}
			clashMapped357Roundtrip(t, p, map[string]any{
				"streamSettings.tlsSettings.serverName":           "sni.example.test",
				"streamSettings.tlsSettings.verifyPeerCertByName": "verify.example.test",
				"streamSettings.tlsSettings.pinnedPeerCertSha256": strings.Repeat("ab", 32),
				"streamSettings.tlsSettings.fingerprint":          "firefox",
				"streamSettings.tlsSettings.alpn":                 []any{"h2", "http/1.1"},
			})
		})
	}
}

func clashMapped357Certificate(t *testing.T) (string, string, []any, []any) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x35}, ed25519.SeedSize))
	template := &x509.Certificate{SerialNumber: big.NewInt(357), Subject: pkix.Name{CommonName: "client.example.test"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal("cannot create synthetic certificate")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal("cannot encode synthetic key")
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	lines := func(s string) []any {
		out := []any{}
		for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
			out = append(out, line)
		}
		return out
	}
	return certPEM, keyPEM, lines(certPEM), lines(keyPEM)
}

func TestClashMapped357TLSClientCertificatePrivate(t *testing.T) {
	certPEM, keyPEM, cert, key := clashMapped357Certificate(t)
	p := clashMapped357Base("vless")
	p["tls"], p["certificate"], p["private-key"] = true, certPEM, keyPEM
	// Exact-core evidence: the current TLS client did not send a certificate
	// even when valid encipherment cert/key JSON passed config validation.
	r, _ := clashFull357Record(t, p)
	b, err := servers.Marshal(r)
	if err != nil {
		t.Fatal("cannot persist valid mTLS source")
	}
	restored, err := servers.DecodeBackupRecord(b, r["id"].(string))
	if err != nil || !clashFull357ContainsSource(restored, p) {
		t.Fatal("mTLS certificate/key source must remain privately recoverable")
	}
	var api *cgi.APIError
	if out, err := servers.Outbound(restored); out != nil || !errors.As(err, &api) || api.Code != "PROTOCOL_NOT_SUPPORTED" {
		t.Fatal("mTLS must refuse runtime explicitly rather than emit a client missing its certificate")
	}
	if config, err := servers.XrayConfig(restored, servers.GenSettings{Listen: "127.0.0.1"}, "synthetic-mtls"); len(config) != 0 || !errors.As(err, &api) || api.Code != "PROTOCOL_NOT_SUPPORTED" {
		t.Fatal("mTLS config generation bypassed the runtime capability refusal")
	}
	masked, _ := json.Marshal(servers.MaskRecord(r))
	if bytes.Contains(masked, []byte(key[1].(string))) || bytes.Contains(masked, []byte(cert[1].(string))) {
		t.Fatal("public record exposed imported client certificate material")
	}
	p["private-key"] = "synthetic-invalid-key-material"
	st := clashStage(t, clashBody(p))
	if st.Accepted != 0 || st.Rejected != 1 {
		t.Fatal("invalid mTLS material must reject instead of masquerading as an unavailable valid client")
	}
}

func TestClashMapped357HTTPHeadersPrivate(t *testing.T) {
	p := clashMapped357Base("http")
	headers := map[string]any{"X-Client-Token": "synthetic-header-secret-357", "User-Agent": "synthetic-agent/357"}
	p["headers"] = headers
	r := clashMapped357Roundtrip(t, p, map[string]any{"settings.headers": headers})
	masked, _ := json.Marshal(servers.MaskRecord(r))
	if bytes.Contains(masked, []byte("synthetic-header-secret-357")) {
		t.Fatal("public record exposed imported HTTP header secret")
	}
}

func TestClashMapped357GRPCUserAgent(t *testing.T) {
	for _, kind := range []string{"vless", "vmess", "trojan"} {
		t.Run(kind, func(t *testing.T) {
			p := clashMapped357Base(kind)
			p["network"] = "grpc"
			p["grpc-opts"] = map[string]any{"grpc-service-name": "synthetic-service", "grpc-user-agent": "synthetic-agent/357"}
			clashMapped357Roundtrip(t, p, map[string]any{
				"streamSettings.network": "grpc", "streamSettings.grpcSettings.serviceName": "synthetic-service",
				"streamSettings.grpcSettings.user_agent": "synthetic-agent/357",
			})
		})
	}
}

func TestClashMapped357HTTPCamouflage(t *testing.T) {
	for _, kind := range []string{"vless", "vmess"} {
		t.Run(kind, func(t *testing.T) {
			p := clashMapped357Base(kind)
			p["network"] = "http"
			paths := []any{"/first", "/second?client=357"}
			headers := map[string]any{"Host": []any{"one.example.test", "two.example.test"}, "X-Client": []any{"a", "b"}}
			p["http-opts"] = map[string]any{"method": "POST", "path": paths, "headers": headers}
			clashMapped357Roundtrip(t, p, map[string]any{
				"streamSettings.network": "raw", "streamSettings.rawSettings.header.type": "http",
				"streamSettings.rawSettings.header.request.method":  "POST",
				"streamSettings.rawSettings.header.request.path":    paths,
				"streamSettings.rawSettings.header.request.headers": headers,
			})
		})
	}
}

func TestClashMapped357XHTTPExtra(t *testing.T) {
	p := clashMapped357Base("vless")
	p["network"] = "xhttp"
	opts := map[string]any{"host": "front.example.test", "path": "/upload", "mode": "packet-up",
		"headers": map[string]any{"X-Client": "synthetic-header-357"}, "no-grpc-header": false,
		"x-padding-bytes": "100-200", "x-padding-obfs-mode": true,
		"x-padding-key": "pad", "x-padding-header": "X-Padding", "x-padding-placement": "header", "x-padding-method": "tokenish",
		"uplink-http-method": "POST", "session-placement": "query", "session-key": "session", "session-table": "hex", "session-length": "16-24",
		"seq-placement": "header", "seq-key": "X-Sequence", "uplink-data-placement": "body", "uplink-data-key": "data",
		"uplink-chunk-size": "1024-2048", "sc-max-each-post-bytes": "65536-131072", "sc-min-posts-interval-ms": "10-20",
		"reuse-settings": map[string]any{"max-concurrency": "4-8", "max-connections": "0", "c-max-reuse-times": "10-20", "h-max-request-times": "100-200", "h-max-reusable-secs": "300-600", "h-keep-alive-period": 15}}
	p["xhttp-opts"] = opts
	want := map[string]any{"streamSettings.network": "xhttp", "streamSettings.xhttpSettings.host": "front.example.test", "streamSettings.xhttpSettings.path": "/upload", "streamSettings.xhttpSettings.mode": "packet-up"}
	for source, target := range map[string]string{
		"headers": "headers", "no-grpc-header": "noGRPCHeader", "x-padding-bytes": "xPaddingBytes", "x-padding-obfs-mode": "xPaddingObfsMode",
		"x-padding-key": "xPaddingKey", "x-padding-header": "xPaddingHeader", "x-padding-placement": "xPaddingPlacement", "x-padding-method": "xPaddingMethod",
		"uplink-http-method": "uplinkHTTPMethod", "session-placement": "sessionIDPlacement", "session-key": "sessionIDKey", "session-table": "sessionIDTable", "session-length": "sessionIDLength",
		"seq-placement": "seqPlacement", "seq-key": "seqKey", "uplink-data-placement": "uplinkDataPlacement", "uplink-data-key": "uplinkDataKey",
		"uplink-chunk-size": "uplinkChunkSize", "sc-max-each-post-bytes": "scMaxEachPostBytes", "sc-min-posts-interval-ms": "scMinPostsIntervalMs",
	} {
		want["streamSettings.xhttpSettings.extra."+target] = opts[source]
	}
	want["streamSettings.xhttpSettings.extra.xmux"] = map[string]any{"maxConcurrency": "4-8", "maxConnections": "0", "cMaxReuseTimes": "10-20", "hMaxRequestTimes": "100-200", "hMaxReusableSecs": "300-600", "hKeepAlivePeriod": 15}
	clashMapped357Roundtrip(t, p, want)
}

func TestClashMapped357MKCPDirectFields(t *testing.T) {
	p := clashMapped357Base("vmess")
	p["network"] = "mkcp"
	p["mkcp-opts"] = map[string]any{"mtu": 1200, "tti": 30, "uplink-capacity": 5, "downlink-capacity": 20, "seed": "synthetic-seed-357", "header": "srtp"}
	clashMapped357Roundtrip(t, p, map[string]any{
		"streamSettings.network": "kcp", "streamSettings.kcpSettings.mtu": 1200,
		"streamSettings.kcpSettings.tti": 30, "streamSettings.kcpSettings.uplinkCapacity": 5, "streamSettings.kcpSettings.downlinkCapacity": 20,
		"streamSettings.finalmask.udp": []any{
			map[string]any{"type": "mkcp-legacy", "settings": map[string]any{"value": "synthetic-seed-357"}},
			map[string]any{"type": "mkcp-legacy", "settings": map[string]any{"header": "srtp"}},
		},
	})
}

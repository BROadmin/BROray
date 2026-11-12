package subscriptions

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/clashimport"
)

// These fixtures follow pinned Mihomo 0455d0d constructors, not a count of
// schema entries. No foreign backend, file reference or remote service runs.
func clashCapability357Remote(t *testing.T) map[string]map[string]any {
	t.Helper()
	key := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()}, D: big.NewInt(357)}
	key.X, key.Y = key.Curve.ScalarBaseMult(key.D.Bytes())
	privateDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal("cannot encode synthetic EC key")
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal("cannot encode synthetic EC public key")
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(357), Subject: pkix.Name{CommonName: "synthetic-ca-357"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	if err != nil {
		t.Fatal("cannot create synthetic CA")
	}
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	fixtures := map[string]map[string]any{
		"anytls":      {"password": "synthetic-secret-357", "idle-session-timeout": 30},
		"easytier":    {"network-name": "synthetic-network-357", "network-secret": "synthetic-secret-357", "dhcp": true, "peers": []any{"tcp://peer.example.test:11010"}},
		"gost-relay":  {"username": "synthetic-user", "password": "synthetic-secret-357", "udp": true},
		"hysteria":    {"auth-str": "synthetic-secret-357", "up": "10 Mbps", "down": "50 Mbps", "protocol": "udp"},
		"masque":      {"private-key": base64.StdEncoding.EncodeToString(privateDER), "public-key": base64.StdEncoding.EncodeToString(publicDER), "ip": "10.8.0.2/32", "network": "h3"},
		"mieru":       {"username": "synthetic-user", "password": "synthetic-secret-357", "transport": "TCP"},
		"openvpn":     {"ca": ca, "username": "synthetic-user", "password": "synthetic-secret-357", "proto": "udp", "dev": "tun", "cipher": "AES-256-GCM"},
		"shadowquic":  {"username": "synthetic-user", "password": "synthetic-secret-357", "quic-versions": []any{"v1"}},
		"snell":       {"psk": "synthetic-secret-357", "version": 3},
		"ssh":         {"username": "synthetic-user", "password": "synthetic-secret-357"},
		"ssr":         {"password": "synthetic-secret-357", "cipher": "aes-128-cfb", "obfs": "plain", "protocol": "origin"},
		"sudoku":      {"key": "synthetic-secret-357", "padding-min": 5, "padding-max": 10},
		"tailscale":   {"hostname": "synthetic-357", "auth-key": "synthetic-secret-357", "exit-node": "100.64.0.2"},
		"trusttunnel": {"username": "synthetic-user", "password": "synthetic-secret-357"},
		"tuic":        {"uuid": "00000000-0000-4000-8000-000000000357", "password": "synthetic-secret-357", "udp-relay-mode": "native"},
		"zerotier":    {"network": "8056c2e21c000001", "mtu": 1280},
	}
	for kind, p := range fixtures {
		p["name"], p["type"] = "synthetic-"+kind, kind
		if kind != "easytier" && kind != "tailscale" && kind != "zerotier" {
			p["server"], p["port"] = "edge.example.test", 443
		}
	}
	return fixtures
}

func clashCapability357Assert(t *testing.T, p map[string]any, kind string) clashimport.Input {
	t.Helper()
	input, err := clashimport.ConvertNode(p, importChecks())
	if err != nil || input.Unavailable == nil || input.Capability == nil {
		t.Fatalf("valid retained source needs an explicit typed unavailable result: %v", err)
	}
	if input.Link != "" || len(input.ClientFields) != 0 {
		t.Fatal("unavailable conversion must not expose a stripped runnable projection")
	}
	if input.Unavailable.Type != p["type"] || !jsonEqual(input.Unavailable.Fields, p) {
		t.Fatal("typed unavailable result lost source protocol or semantic fields")
	}
	// Read the exported contract as data so the initial RED observes the
	// missing classification rather than failing compilation on a new field.
	b, _ := json.Marshal(input.Capability)
	var capability map[string]any
	_ = json.Unmarshal(b, &capability)
	actual := capability["kind"]
	if actual == nil {
		actual = capability["Kind"]
	}
	if actual != kind {
		t.Fatalf("capability must distinguish missing mapping, absent core support and unknown extension: got %v, want %s", actual, kind)
	}
	if len(input.Capability.Fields) == 0 {
		t.Fatal("unavailable result must identify the affected field group")
	}
	for _, path := range input.Capability.Fields {
		if strings.Contains(path, "synthetic-secret") || strings.Contains(path, "private-extension-name") {
			t.Fatal("capability diagnostic exposed a private source value or unknown field name")
		}
	}
	return input
}

func clashCapability357Persist(t *testing.T, p map[string]any) {
	t.Helper()
	r, _ := clashFull357Record(t, p)
	b, err := servers.Marshal(r)
	if err != nil {
		t.Fatal("cannot persist valid unavailable source")
	}
	restored, err := servers.DecodeBackupRecord(b, r["id"].(string))
	if err != nil || !clashFull357ContainsSource(restored, p) {
		t.Fatal("private backup lost the retained structured server definition")
	}
	var api *cgi.APIError
	if out, err := servers.Outbound(restored); out != nil || !errors.As(err, &api) || api.Code != "PROTOCOL_NOT_SUPPORTED" {
		t.Fatal("unavailable server emitted an executable outbound")
	}
	public, _ := json.Marshal(servers.MaskRecord(restored))
	if strings.Contains(string(public), "synthetic-secret-357") || strings.Contains(string(public), "private-extension-name") || strings.Contains(string(public), "BEGIN CERTIFICATE") {
		t.Fatal("private retained source escaped public masking")
	}
}

func TestClashCapability357RemoteBackends(t *testing.T) {
	for kind, p := range clashCapability357Remote(t) {
		t.Run(kind, func(t *testing.T) {
			classification := "core-unsupported"
			if kind == "masque" {
				// The pinned core has a separate masqueSettings.warp key path;
				// basic WARP now maps. A source MTU without a target setting is
				// a genuine remaining unavailable context, not missing backend.
				p["mtu"] = 1300
				classification = "conversion-unimplemented"
			}
			clashCapability357Assert(t, p, classification)
			clashCapability357Persist(t, p)
		})
	}
}

func TestClashCapability357RemoteMalformedKnown(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"anytls":      func(p map[string]any) { p["port"] = 0 },
		"easytier":    func(p map[string]any) { p["ipv4"] = "not-an-ip" },
		"gost-relay":  func(p map[string]any) { p["port"] = 65536 },
		"hysteria":    func(p map[string]any) { p["up"] = "not-a-bandwidth" },
		"masque":      func(p map[string]any) { p["private-key"] = "invalid-base64-EC-key" },
		"mieru":       func(p map[string]any) { p["port-range"] = "5000-4000" },
		"openvpn":     func(p map[string]any) { p["tran-window"] = -1 },
		"shadowquic":  func(p map[string]any) { p["quic-versions"] = []any{"not-a-quic-version"} },
		"snell":       func(p map[string]any) { p["version"] = 7 },
		"ssh":         func(p map[string]any) { p["host-key"] = []any{"not-an-ssh-public-key"} },
		"ssr":         func(p map[string]any) { p["cipher"] = "not-a-cipher" },
		"sudoku":      func(p map[string]any) { p["key"] = "" },
		"tailscale":   func(p map[string]any) { p["control-url"] = "http://[broken" },
		"trusttunnel": func(p map[string]any) { p["port"] = -1 },
		"tuic":        func(p map[string]any) { p["udp-over-stream-version"] = 99 },
		"zerotier":    func(p map[string]any) { p["network"] = "not-a-network-id" },
	}
	for kind, p := range clashCapability357Remote(t) {
		t.Run(kind, func(t *testing.T) {
			mutations[kind](p)
			for _, extension := range []bool{false, true} {
				if extension {
					p["private-extension-name"] = "synthetic-secret-357"
				}
				input, err := clashimport.ConvertNode(p, importChecks())
				if err == nil || input.Unavailable != nil || input.Link != "" {
					t.Fatal("malformed known field was hidden by unavailable classification")
				}
				if strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "private-extension-name") {
					t.Fatal("malformed-field diagnostic leaked source data")
				}
			}
		})
	}
}

func TestClashCapability357KnownFeatureGroups(t *testing.T) {
	cases := []struct {
		name, protocol, kind string
		fields               map[string]any
	}{
		{"packet-address", "vless", "core-unsupported", map[string]any{"packet-encoding": "packetaddr", "udp": true}},
		{"packet-address-vmess", "vmess", "core-unsupported", map[string]any{"packet-addr": true, "udp": true}},
		{"family-preference", "http", "conversion-unimplemented", map[string]any{"ip-version": "ipv6-prefer"}},
		{"dialer-reference", "vless", "conversion-unimplemented", map[string]any{"dialer-proxy": "synthetic-upstream"}},
		{"grpc-pool", "vless", "conversion-unimplemented", map[string]any{"network": "grpc", "grpc-opts": map[string]any{"grpc-service-name": "svc", "max-connections": 4, "min-streams": 2, "max-streams": 8}}},
		{"legacy-kcp-congestion", "vmess", "core-unsupported", map[string]any{"network": "mkcp", "mkcp-opts": map[string]any{"congestion": true, "read-buffer": 4, "write-buffer": 4}}},
		{"h2-transport", "vless", "core-unsupported", map[string]any{"network": "h2", "tls": true, "h2-opts": map[string]any{"host": []any{"front.example.test"}, "path": "/h2"}}},
		{"sing-mux", "vless", "core-unsupported", map[string]any{"smux": map[string]any{"enabled": true, "protocol": "smux", "max-connections": 4}}},
		{"ss-simple-obfs", "ss", "core-unsupported", map[string]any{"plugin": "obfs", "plugin-opts": map[string]any{"mode": "tls", "host": "front.example.test"}}},
		{"trojan-ss-wrapper", "trojan", "core-unsupported", map[string]any{"ss-opts": map[string]any{"enabled": true, "method": "aes-128-gcm", "password": "synthetic-secret-357"}}},
		{"ws-fast-open", "vless", "conversion-unimplemented", map[string]any{"network": "ws", "ws-opts": map[string]any{"path": "/ws", "v2ray-http-upgrade": true, "v2ray-http-upgrade-fast-open": true}}},
		// Ordinary download streams now map; resolver-dependent ECH remains
		// an explicit source-only context within the independently keyed stream.
		{"xhttp-download-dynamic-ech", "vless", "conversion-unimplemented", map[string]any{"network": "xhttp", "tls": true, "xhttp-opts": map[string]any{"path": "/up", "download-settings": map[string]any{"server": "download.example.test", "port": 8443, "tls": true, "path": "/down", "ech-opts": map[string]any{"enable": true, "query-server-name": "ech.example.test"}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := clashMapped357Base(tc.protocol)
			for k, v := range tc.fields {
				p[k] = v
			}
			clashCapability357Assert(t, p, tc.kind)
			clashCapability357Persist(t, p)
		})
	}
}

func TestClashCapability357UnknownExtension(t *testing.T) {
	for _, nested := range []bool{false, true} {
		p := clashMapped357Base("vless")
		if nested {
			p["network"] = "ws"
			p["ws-opts"] = map[string]any{"path": "/ws", "private-extension-name": map[string]any{"mode": "synthetic-secret-357"}}
		} else {
			p["private-extension-name"] = map[string]any{"mode": "synthetic-secret-357", "enabled": false, "values": []any{0, "", true}}
		}
		clashCapability357Assert(t, p, "unknown-extension")
		clashCapability357Persist(t, p)
		p["uuid"] = false
		input, err := clashimport.ConvertNode(p, importChecks())
		if err == nil || input.Unavailable != nil {
			t.Fatal("unknown extension bypassed UUID validation")
		}
	}
	p := clashMapped357Base("not-a-registered-backend")
	p["private-extension-name"] = "synthetic-secret-357"
	if input, err := clashimport.ConvertNode(p, importChecks()); err == nil || input.Unavailable != nil {
		t.Fatal("unregistered protocol must not be certified by a known server schema")
	}
}

func TestClashCapability357MappedMalformedCannotFallback(t *testing.T) {
	cases := []struct {
		name, protocol string
		fields         map[string]any
	}{
		{"uuid", "vless", map[string]any{"uuid": []any{"bad-shape"}}},
		{"flow", "vless", map[string]any{"flow": "bad-flow"}},
		{"alterId", "vmess", map[string]any{"alterId": -1}},
		{"cipher", "ss", map[string]any{"cipher": "bad-cipher"}},
		{"tls-pin", "vless", map[string]any{"tls": true, "fingerprint": "not-a-sha256-pin"}},
		{"grpc-shape", "vless", map[string]any{"network": "grpc", "grpc-opts": map[string]any{"max-connections": []any{1}}}},
		{"xhttp-range", "vless", map[string]any{"network": "xhttp", "xhttp-opts": map[string]any{"sc-max-each-post-bytes": "200-100"}}},
		{"plugin-mode", "ss", map[string]any{"plugin": "obfs", "plugin-opts": map[string]any{"mode": "not-an-obfs-mode"}}},
		{"h2-shape", "vless", map[string]any{"network": "h2", "h2-opts": map[string]any{"host": map[string]any{"bad": "shape"}}}},
		{"ip-family", "http", map[string]any{"ip-version": "ipv7"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := clashMapped357Base(tc.protocol)
			for k, v := range tc.fields {
				p[k] = v
			}
			p["private-extension-name"] = "synthetic-secret-357"
			input, err := clashimport.ConvertNode(p, importChecks())
			if err == nil || input.Unavailable != nil || input.Link != "" {
				t.Fatal("invalid known setting was downgraded to an unavailable profile")
			}
		})
	}
}

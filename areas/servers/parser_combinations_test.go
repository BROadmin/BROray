package servers

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Cartesian domain is explicit. These are client URI contracts, not a
// promise that every Xray feature or every remote peer works on real hardware.
func coverageURI(proto, network, security string, extra url.Values) string {
	q := url.Values{"type": {network}, "security": {security}}
	if security == "tls" || security == "reality" {
		q.Set("sni", "example.test")
		q.Set("fp", "chrome")
	}
	if security == "reality" {
		k, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{7}, 32))
		q.Set("pbk", base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()))
		q.Set("sid", "0123456789abcdef")
	}
	if proto == "vless" {
		q.Set("encryption", "none")
	}
	for k, values := range extra {
		q[k] = values
	}
	return proto + "://00000000-0000-4000-8000-000000000001@example.test:443?" + q.Encode() + "#coverage"
}

func coverageRoundTrip(t *testing.T, r Record, checkCore bool) {
	t.Helper()
	before, err := Outbound(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeBackupRecord(b, "coverage")
	if err != nil {
		t.Fatal(err)
	}
	after, err := Outbound(restored)
	if err != nil || !jsonValueEqual(before, after) {
		t.Fatal("backup changed connection", err)
	}
	if !checkCore {
		return
	}
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Log("parser/backup checked; core configuration NOT RUN (BRORAY_XRAY_BIN unset)")
		return
	}
	cfg, err := XrayConfig(restored, GenSettings{Listen: "127.0.0.1", SocksPort: 2080, LogLevel: "none"}, "coverage")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "client.json")
	if err = os.WriteFile(p, cfg, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, bin, "run", "-test", "-config", p).CombinedOutput()
	if err != nil {
		t.Fatalf("official core rejected generated synthetic configuration: %v %s", err, output)
	}
}

func TestClientProtocolTransportSecurityMatrix(t *testing.T) {
	// Trojan intentionally requires TLS/REALITY. XDRIVE and MASQUE have native
	// JSON contracts and are explicitly rejected by these ordinary URI forms.
	for _, proto := range []string{"vless", "vmess", "trojan"} {
		for _, network := range []string{"raw", "ws", "grpc", "httpupgrade", "xhttp", "kcp", "xdrive", "masque"} {
			for _, security := range []string{"none", "tls", "reality", "invalid"} {
				t.Run(proto+"/"+network+"/"+security, func(t *testing.T) {
					want := network != "xdrive" && network != "masque" && security != "invalid"
					if proto == "trojan" && security == "none" {
						want = false
					}
					if security == "reality" && network != "raw" && network != "grpc" && network != "xhttp" {
						want = false
					}
					r, err := ParseLink(coverageURI(proto, network, security, nil), "coverage", nil)
					if !want {
						if err == nil {
							t.Fatal("unsupported combination accepted")
						}
						return
					}
					if err != nil {
						t.Fatal("supported combination rejected", err)
					}
					if r["protocol"] != proto || r["network"] != network || r["security"] != security {
						t.Fatal("connection selectors changed")
					}
					coverageRoundTrip(t, r, true)
				})
			}
		}
	}
}

func TestClientFlowAndModeMatrix(t *testing.T) {
	for _, network := range []string{"raw", "ws", "grpc", "httpupgrade", "xhttp", "kcp"} {
		for _, security := range []string{"none", "tls", "reality"} {
			for _, flow := range []string{"xtls-rprx-vision", "xtls-rprx-vision-udp443"} {
				t.Run("flow/"+network+"/"+security+"/"+flow, func(t *testing.T) {
					r, err := ParseLink(coverageURI("vless", network, security, url.Values{"flow": {flow}}), "coverage", nil)
					want := network == "raw" && (security == "tls" || security == "reality")
					if !want {
						if err == nil {
							t.Fatal("inapplicable Vision flow accepted")
						}
						return
					}
					if err != nil || r["flow"] != flow {
						t.Fatal("valid Vision flow lost", err)
					}
					coverageRoundTrip(t, r, true)
				})
			}
		}
	}
	for _, proto := range []string{"vless", "vmess", "trojan"} {
		for _, mode := range []string{"auto", "packet-up", "stream-up", "stream-one", "gun", "multi", "invalid"} {
			t.Run("xhttp/"+proto+"/"+mode, func(t *testing.T) {
				r, err := ParseLink(coverageURI(proto, "xhttp", "tls", url.Values{"mode": {mode}, "path": {"/synthetic"}}), "coverage", nil)
				want := mode == "auto" || mode == "packet-up" || mode == "stream-up" || mode == "stream-one"
				if !want {
					if err == nil {
						t.Fatal("invalid XHTTP mode accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				o, err := Outbound(r)
				if err != nil || get(o, "streamSettings", "xhttpSettings", "mode") != mode {
					t.Fatal("mode not preserved", err)
				}
				coverageRoundTrip(t, r, true)
			})
		}
	}
}

func TestClientTransportAliasesAndLegacyParity(t *testing.T) {
	for _, alias := range [][2]string{{"tcp", "raw"}, {"websocket", "ws"}, {"httpUpgrade", "httpupgrade"}, {"splithttp", "xhttp"}} {
		for _, proto := range []string{"vless", "vmess", "trojan"} {
			t.Run(proto+"/"+alias[0], func(t *testing.T) {
				a, e := ParseLink(coverageURI(proto, alias[0], "tls", nil), "coverage", nil)
				b, f := ParseLink(coverageURI(proto, alias[1], "tls", nil), "coverage", nil)
				if e != nil || f != nil {
					t.Fatal(e, f)
				}
				x, _ := Outbound(a)
				y, _ := Outbound(b)
				if !jsonValueEqual(x, y) {
					t.Fatal("alias changed outbound")
				}
			})
		}
	}
	for _, network := range []string{"raw", "ws", "grpc", "httpupgrade", "xhttp", "kcp"} {
		t.Run("legacy-vmess/"+network, func(t *testing.T) {
			m := map[string]any{"v": "2", "ps": "coverage", "add": "example.test", "port": "443", "id": "00000000-0000-4000-8000-000000000001", "aid": "0", "net": network, "tls": "tls", "sni": "example.test", "fp": "chrome", "scy": "auto"}
			b, _ := json.Marshal(m)
			a, e := ParseLink("vmess://"+base64.StdEncoding.EncodeToString(b), "coverage", nil)
			options := url.Values{}
			if network == "kcp" {
				// Legacy VMess selects the old mKCP wire format even with an
				// empty seed. Modern URI must request that format explicitly;
				// absent seed selects current mKCP and is not equivalent input.
				options.Set("seed", "")
			}
			modern, f := ParseLink(coverageURI("vmess", network, "tls", options), "coverage", nil)
			if e != nil || f != nil {
				t.Fatal(e, f)
			}
			x, _ := Outbound(a)
			y, _ := Outbound(modern)
			if !jsonValueEqual(x, y) {
				t.Fatal("equivalent legacy and URI inputs differ")
			}
		})
	}
}

func TestClientInactiveStreamBlocks(t *testing.T) {
	for _, network := range []string{"raw", "ws", "httpupgrade", "grpc", "xhttp", "kcp"} {
		for _, block := range []string{"wsSettings", "httpupgradeSettings", "grpcSettings"} {
			if strings.TrimSuffix(block, "Settings") == network {
				continue // positive field matrix owns active blocks
			}
			t.Run(network+"/"+block, func(t *testing.T) {
				opts, _ := json.Marshal(map[string]any{block: map[string]any{}})
				_, err := ParseLink(coverageURI("vless", network, "tls", url.Values{"streamOptions": {string(opts)}}), "coverage", nil)
				if err == nil {
					t.Fatal("inactive transport block accepted")
				}
			})
		}
	}
}

// Cryptographic encodings and fingerprint membership are checked by the pinned
// core before runtime. Keep this separate from JSON types and import acceptance:
// a parsed string is not evidence that it is a valid key or supported TLS profile.
func TestClientProtectionCoreValidation(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN unset: core validation NOT RUN")
	}
	for _, tc := range []struct {
		name, security, key, value string
		valid                      bool
	}{
		{"reality-short-empty", "reality", "sid", "", true},
		{"reality-short-max", "reality", "sid", "0123456789ABCDEF", true},
		{"reality-short-odd", "reality", "sid", "abc", false},
		{"reality-short-nonhex", "reality", "sid", "zz", false},
		{"reality-short-long", "reality", "sid", "001122334455667788", false},
		{"reality-key-short", "reality", "pbk", "AQEBAQ", false},
		{"reality-key-padding", "reality", "pbk", strings.Repeat("A", 43) + "=", false},
		{"reality-spider-path", "reality", "spx", "/synthetic?x=1", true},
		{"reality-spider-nonpath", "reality", "spx", "synthetic", false},
		{"reality-fingerprint-unsafe", "reality", "fp", "unsafe", false},
		{"reality-fingerprint-golang", "reality", "fp", "hellogolang", false},
		{"reality-fingerprint-unknown", "reality", "fp", "nonexistent-profile", false},
		{"tls-fingerprint-latest", "tls", "fp", "hellochrome_155", true},
		{"tls-fingerprint-unknown", "tls", "fp", "nonexistent-profile", false},
		{"reality-ml-dsa-size", "reality", "pqv", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 1952)), true},
		{"reality-ml-dsa-short", "reality", "pqv", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 1951)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := ParseLink(coverageURI("vless", "raw", tc.security, url.Values{tc.key: {tc.value}}), "coverage", nil)
			if err != nil {
				if tc.valid {
					t.Fatal("valid protection rejected at import", err)
				}
				t.Log("rejected at import")
				return
			}
			cfg, err := XrayConfig(r, GenSettings{Listen: "127.0.0.1", SocksPort: 2080, LogLevel: "none"}, "coverage")
			if err != nil {
				if tc.valid {
					t.Fatal(err)
				}
				t.Log("rejected at generation")
				return
			}
			p := filepath.Join(t.TempDir(), "client.json")
			if err = os.WriteFile(p, cfg, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, bin, "run", "-test", "-config", p).CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("core control timed out")
			}
			if tc.valid && err != nil {
				t.Fatalf("valid synthetic control rejected: %v %s", err, output)
			}
			if !tc.valid && err == nil {
				t.Fatal("invalid protection accepted by final core gate")
			}
			if !tc.valid {
				t.Log("accepted as text by importer; rejected by pinned core config validation")
			}
		})
	}
}

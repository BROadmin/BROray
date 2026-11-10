package servers

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/udpprobe"
)

// Two real Xray processes; no NIC, external accounts or router state required.
func TestNativeXrayTraffic(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN not set")
	}
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(15 * time.Second)); io.Copy(c, c) }()
		}
	}()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go udpprobe.Responder(pc)
	cert, key, pin := selfSigned(t)
	for _, kind := range []string{"masque-h3", "masque-h2", "masque-h3-dns", "masque-h2-dns", "masque-h3-ipv6", "masque-h2-ipv6", "masque-h3-unavailable", "xdrive-local", "xdrive-s3", "xdrive-template", "xdrive-s3-retry", "xdrive-template-recovery"} {
		t.Run(kind, func(t *testing.T) {
			serverPort, clientPort := freeTCP(t), freeTCP(t)
			dir := t.TempDir()
			var out, in map[string]any
			var dnsQueries *atomic.Int32
			if strings.HasPrefix(kind, "xdrive-") {
				backend := strings.TrimPrefix(kind, "xdrive-")
				scenario := backend
				backend = strings.Split(backend, "-")[0]
				if backend == "local" {
					// The fixed namespace is created only in this isolated guest. Refuse reuse.
					if err := os.MkdirAll(filepath.Dir(xdriveLocalDirectory), 0700); err != nil {
						t.Fatal(err)
					}
					if err := prepareLocalStorage(platform.Root{Dir: "/opt/broray"}, xdriveLocalDirectory); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.RemoveAll(xdriveLocalDirectory) })
				}
				private, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{17}, 32))
				if err != nil {
					t.Fatal(err)
				}
				enc := base64.RawURLEncoding.EncodeToString
				out = nativeBackendFixture(t, backend)
				ep := get(out, "settings", "vnext").([]any)[0].(map[string]any)
				ep["address"], ep["port"] = "127.0.0.1", serverPort
				user := ep["users"].([]any)[0].(map[string]any)
				user["encryption"] = "mlkem768x25519plus.native.0rtt." + enc(private.PublicKey().Bytes())
				x := get(out, "streamSettings", "xdriveSettings").(map[string]any)
				if backend != "local" {
					endpoint := cloudStorageFixture(t, scenario)
					st := out["streamSettings"].(map[string]any)
					st["security"] = "none"
					delete(st, "tlsSettings")
					x["remoteFolder"] = "profiles/test"
					if backend == "s3" {
						s3 := x["s3"].(map[string]any)
						s3["endpoint"], s3["pathStyle"] = endpoint, true
					} else {
						template := x["template"].(map[string]any)
						for _, op := range []string{"put", "get", "delete"} {
							template[op].(map[string]any)["url"] = endpoint + "/objects/{folder}/{name}"
						}
						template["list"].(map[string]any)["url"] = endpoint + "/list?prefix={prefix}"
						if scenario == "template-recovery" {
							template["auth"] = map[string]any{"type": "oauth2", "tokenUrl": endpoint + "/token", "form": map[string]any{"refresh_token": "synthetic-private-refresh"}, "tokenPath": "access_token", "expiryPath": "expires_in", "header": map[string]any{"Authorization": "Bearer {token}"}}
						}
					}
				}
				x["flushIntervalMs"], x["pollIntervalMs"], x["maxPollIntervalMs"] = 5, 5, 20
				in = map[string]any{"listen": "127.0.0.1", "port": serverPort, "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": user["id"]}}, "decryption": "mlkem768x25519plus.native.600s." + enc(private.Bytes())}, "streamSettings": out["streamSettings"]}
			} else {
				out = nativeTestOutbound("masque")
				get(out, "settings").(map[string]any)["address"] = "127.0.0.1"
				get(out, "settings").(map[string]any)["port"] = serverPort
				alpn := "h3"
				if strings.HasPrefix(kind, "masque-h2") {
					alpn = "h2"
				}
				st := out["streamSettings"].(map[string]any)
				st["tlsSettings"] = map[string]any{"serverName": "localhost", "alpn": []any{alpn}, "pinnedPeerCertSha256": pin}
				st["masqueSettings"] = map[string]any{"user": "test@example.test", "pass": "synthetic-test-password"}
				in = map[string]any{"listen": "127.0.0.1", "port": serverPort, "protocol": "masque", "settings": map[string]any{"users": []any{map[string]any{"email": "test@example.test", "pass": "synthetic-test-password"}}, "address": []any{"192.0.2.1/24"}}, "streamSettings": map[string]any{"network": "masque", "security": "tls", "tlsSettings": map[string]any{"alpn": []any{alpn}, "certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}}}
				if strings.HasSuffix(kind, "-ipv6") {
					in["settings"].(map[string]any)["address"] = []any{"192.0.2.1/24", "2001:db8::1/64"}
				}
				if strings.HasSuffix(kind, "-dns") {
					dnsQueries = nativeDNSFixture(t)
					out["settings"].(map[string]any)["remoteDNS"] = []any{"198.18.0.53"}
				}
			}
			rec := parseNativeFixture(t, out)
			cfg, err := XrayConfig(rec, GenSettings{Listen: "127.0.0.1", SocksPort: clientPort, LogLevel: "debug"}, "test")
			if err != nil {
				t.Fatal(err)
			}
			freedom := map[string]any{
				"protocol": "freedom", "sendThrough": "127.0.0.3",
				"settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "ip": []any{"127.0.0.0/8", "192.0.2.0/24", "198.18.0.0/15", "2001:db8::/32"}}}},
			}
			tcpTarget, udpTarget := target.Addr().String(), pc.LocalAddr().String()
			if strings.HasPrefix(kind, "masque-") {
				// Like the existing WireGuard fixture, address a remote network
				// inside NetTUN; only the server maps it to the loopback responder.
				freedom["settings"].(map[string]any)["redirect"] = "127.0.0.1:0"
				tcpTarget = strings.Replace(tcpTarget, "127.0.0.1", "198.18.0.1", 1)
				udpTarget = strings.Replace(udpTarget, "127.0.0.1", "198.18.0.1", 1)
				if strings.HasSuffix(kind, "-ipv6") {
					// An address inside the assigned pool denotes another MASQUE
					// client; use a separate documentation prefix as the remote exit.
					tcpTarget = strings.Replace(tcpTarget, "198.18.0.1", "[2001:db8:1::123]", 1)
					udpTarget = strings.Replace(udpTarget, "198.18.0.1", "[2001:db8:1::123]", 1)
				}
				if dnsQueries != nil {
					tcpTarget = strings.Replace(tcpTarget, "198.18.0.1", "native.example.test", 1)
				}
			}
			serverCfg, _ := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "debug"}, "inbounds": []any{in}, "outbounds": []any{freedom}})
			if !strings.HasSuffix(kind, "-unavailable") {
				server := startXray(t, bin, filepath.Join(dir, "server.json"), serverCfg, 0, nil)
				nativeResourceReceipt(t, server, "server")
			}
			client := startXray(t, bin, filepath.Join(dir, "client.json"), cfg, clientPort, nil)
			nativeResourceReceipt(t, client, "client")
			start := time.Now()
			c, err := dialTestSOCKS(fmt.Sprintf("127.0.0.1:%d", clientPort), tcpTarget)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(15 * time.Second))
			if strings.HasSuffix(kind, "-unavailable") {
				c.SetDeadline(time.Now().Add(time.Second))
				_, _ = c.Write([]byte("no remote UDP server"))
				var b [1]byte
				if _, err := c.Read(b[:]); err == nil {
					t.Fatal("unavailable MASQUE returned traffic")
				}
				if _, err := udpprobe.ViaSOCKS5(fmt.Sprintf("127.0.0.1:%d", clientPort), udpTarget, time.Second); err == nil {
					t.Fatal("UDP bypassed unavailable MASQUE")
				}
				if err := client.Process.Signal(syscall.Signal(0)); err != nil {
					t.Fatal("client exited on transport failure", err)
				}
				t.Log("Unavailable UDP transport: TCP/UDP fail, client process remains alive")
				return
			}
			payload := []byte("BROray native transport payload")
			if _, err := c.Write(payload); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(c, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, got) {
				t.Fatal("TCP content differs")
			}
			t.Logf("TCP payload returned; establishment+first-transfer=%s", time.Since(start))
			var result *net.UDPAddr
			if strings.HasSuffix(kind, "-ipv6") {
				result = nativeUDPIPv6(t, fmt.Sprintf("127.0.0.1:%d", clientPort), udpTarget)
			} else {
				result, err = udpprobe.ViaSOCKS5(fmt.Sprintf("127.0.0.1:%d", clientPort), udpTarget, 15*time.Second)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !result.IP.Equal(net.ParseIP("127.0.0.3")) {
				t.Fatal("UDP did not cross server exit")
			}
			t.Log("UDP returned through separate server exit")
			if dnsQueries != nil && dnsQueries.Load() == 0 {
				t.Fatal("domain resolved without remote DNS fixture")
			}
		})
	}
}

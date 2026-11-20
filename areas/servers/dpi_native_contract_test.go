package servers

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This characterizes the existing core boundary, not a DPI preference API.
// Removing the mask must remove actual TLS fragmentation; merely accepting
// JSON is insufficient because an older core could ignore unknown fields.
func TestDPINativeTLSFragmentContract(t *testing.T) { testDPINativeTLSFragmentContract(t, false) }

func TestDPINativeREALITYColdDetection(t *testing.T) { testDPINativeTLSFragmentContract(t, true) }

func testDPINativeTLSFragmentContract(t *testing.T, cold bool) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN not set")
	}
	version, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(string(version), "\n", 2)[0]
	t.Log(first)
	if !strings.Contains(first, "26.11.9") {
		t.Fatal("characterization core version differs", first)
	}
	const uuid = "11111111-2222-4333-8444-555555555555"
	for _, family := range []string{"tcp4", "tcp6"} {
		protocols := []string{"trojan", "vless-vision", "vless-reality", "vmess", "vless-tls", "vless-ws", "vless-grpc", "vless-httpupgrade", "vless-reality-no-vision", "trojan-ws", "trojan-grpc", "trojan-httpupgrade", "vmess-ws", "vmess-grpc", "vmess-httpupgrade"}
		if cold {
			protocols = []string{"vless-reality"}
		}
		for _, protocol := range protocols {
			for _, mode := range []string{"off", "on", "off-after"} {
				t.Run(family+"/"+protocol+"/"+mode, func(t *testing.T) {
					host := "127.0.0.1"
					egress := "127.0.0.3"
					allow := "127.0.0.0/8"
					if family == "tcp6" {
						host = "::1"
						egress = "::1"
						allow = "::1/128"
					}
					cert, key, pin := selfSigned(t)
					dir := t.TempDir()
					sport, cport := freeTCP(t), freeTCP(t)
					in := map[string]any{"listen": "127.0.0.1", "port": sport, "protocol": "trojan", "settings": map[string]any{"clients": []any{map[string]any{"password": "synthetic-dpi"}}},
						"streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}}}
					flow := ""
					if protocol == "vless-vision" || protocol == "vless-reality" {
						flow = "xtls-rprx-vision"
					}
					if strings.HasPrefix(protocol, "vless-") {
						client := map[string]any{"id": uuid}
						if flow != "" {
							client["flow"] = flow
						}
						in["protocol"] = "vless"
						in["settings"] = map[string]any{"decryption": "none", "clients": []any{client}}
					}
					if strings.HasPrefix(protocol, "vmess") {
						in["protocol"] = "vmess"
						in["settings"] = map[string]any{"clients": []any{map[string]any{"id": uuid}}}
					}
					network, extra := "tcp", ""
					stream := in["streamSettings"].(map[string]any)
					switch protocol {
					case "vless-ws", "trojan-ws", "vmess-ws":
						network = "ws"
						extra = "&path=/dpi"
						stream["network"] = "ws"
						stream["wsSettings"] = map[string]any{"path": "/dpi"}
					case "vless-grpc", "trojan-grpc", "vmess-grpc":
						network = "grpc"
						extra = "&serviceName=dpi"
						stream["network"] = "grpc"
						stream["grpcSettings"] = map[string]any{"serviceName": "dpi"}
					case "vless-httpupgrade", "trojan-httpupgrade", "vmess-httpupgrade":
						network = "httpupgrade"
						extra = "&path=/dpi"
						stream["network"] = "httpupgrade"
						stream["httpupgradeSettings"] = map[string]any{"path": "/dpi"}
					}
					realityPublic := ""
					var awaitProbes func()
					if strings.HasPrefix(protocol, "vless-reality") {
						private, e := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{9}, 32))
						if e != nil {
							t.Fatal(e)
						}
						realityPublic = base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes())
						certificate, e := tls.LoadX509KeyPair(cert, key)
						if e != nil {
							t.Fatal(e)
						}
						front := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "synthetic-reality-front") }))
						front.EnableHTTP2 = true
						front.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}
						// REALITY can wait five seconds for asynchronous target detection.
						// The synthetic front must not close that authenticated connection
						// before the core completes its first metadata polling interval.
						front.Config.ReadHeaderTimeout = 10 * time.Second
						t.Cleanup(front.Close)
						if cold {
							awaitProbes = dpiHoldREALITYStartupProbes(t, front.TLS)
						}
						front.StartTLS()
						in["streamSettings"] = map[string]any{"network": "raw", "security": "reality", "realitySettings": map[string]any{"target": front.Listener.Addr().String(), "serverNames": []any{"localhost"}, "privateKey": base64.RawURLEncoding.EncodeToString(private.Bytes()), "shortIds": []any{"a1b2c3d4"}}}
					}
					server, err := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{in}, "outbounds": []any{map[string]any{"protocol": "freedom", "sendThrough": egress, "settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "ip": []any{allow}}}}}}})
					if err != nil {
						t.Fatal(err)
					}
					startXray(t, bin, filepath.Join(dir, "server.json"), server, sport, nil)
					if awaitProbes != nil {
						awaitProbes()
					}
					relay, records := dpiTLSRecordRelay(t, family, net.JoinHostPort(host, "0"), net.JoinHostPort("127.0.0.1", strconv.Itoa(sport)))
					link := "trojan://synthetic-dpi@" + relay + "?type=" + network + "&security=tls&sni=localhost&pcs=" + pin + extra + "#dpi"
					if strings.HasPrefix(protocol, "vless-") {
						link = "vless://" + uuid + "@" + relay + "?type=" + network + "&security=tls&sni=localhost&pcs=" + pin + "&flow=" + flow + "&encryption=none" + extra + "#dpi"
					}
					if strings.HasPrefix(protocol, "vless-reality") {
						link = "vless://" + uuid + "@" + relay + "?type=tcp&security=reality&sni=localhost&pbk=" + realityPublic + "&sid=a1b2c3d4&fp=chrome&flow=" + flow + "&encryption=none#dpi"
					}
					if strings.HasPrefix(protocol, "vmess") {
						relayHost, relayPort, e := net.SplitHostPort(relay)
						if e != nil {
							t.Fatal(e)
						}
						vmess := map[string]any{"v": "2", "ps": "dpi", "add": relayHost, "port": relayPort, "id": uuid, "aid": "0", "net": network, "type": "none", "tls": "tls", "sni": "localhost", "pcs": pin}
						switch network {
						case "ws", "httpupgrade":
							vmess["path"] = "/dpi"
						case "grpc":
							vmess["serviceName"] = "dpi"
						}
						raw, e := json.Marshal(vmess)
						if e != nil {
							t.Fatal(e)
						}
						link = "vmess://" + base64.StdEncoding.EncodeToString(raw)
					}
					rec, err := ParseLink(link, "dpi-native", nil)
					if err != nil {
						t.Fatal(err)
					}
					gs := GenSettings{Listen: "127.0.0.1", SocksPort: cport, LogLevel: "warning"}
					before, err := XrayConfig(rec, gs, "dpi-native")
					if err != nil {
						t.Fatal(err)
					}
					if mode != "off" {
						rec, err = prepareDPIRecord(rec, true)
						if err != nil {
							t.Fatal(err)
						}
					}
					if mode == "off-after" {
						rec, err = prepareDPIRecord(rec, false)
						if err != nil {
							t.Fatal(err)
						}
					}
					cfg, err := XrayConfig(rec, gs, "dpi-native")
					if err != nil {
						t.Fatal(err)
					}
					if mode != "on" && !bytes.Equal(cfg, before) {
						t.Fatal("removing mask changed original configuration")
					}
					var generated map[string]any
					if err = json.Unmarshal(cfg, &generated); err != nil {
						t.Fatal(err)
					}
					if out, ok := generated["outbounds"].([]any); !ok || len(out) != 1 || out[0].(map[string]any)["tag"] != "proxy" {
						t.Fatal("implicit fallback outbound")
					}
					target, err := net.Listen(family, net.JoinHostPort(host, "0"))
					if err != nil {
						t.Fatal(err)
					}
					payload := strings.Repeat("BROray-DPI-native\n", 2048)
					httpServer := &http.Server{ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						ip, _, _ := net.SplitHostPort(r.RemoteAddr)
						w.Header().Set("X-DPI-Egress", ip)
						_, _ = io.WriteString(w, payload)
					})}
					go httpServer.Serve(target)
					t.Cleanup(func() { _ = httpServer.Close() })
					startXray(t, bin, filepath.Join(dir, "client.json"), cfg, cport, nil)
					began := time.Now()
					conn, err := dialTestSOCKS(net.JoinHostPort("127.0.0.1", strconv.Itoa(cport)), target.Addr().String())
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					if err = conn.SetDeadline(time.Now().Add(8 * time.Second)); err != nil {
						t.Fatal(err)
					}
					req, err := http.NewRequest("GET", "http://"+target.Addr().String()+"/dpi", nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Close = true
					if err = req.Write(conn); err != nil {
						t.Fatal(err)
					}
					response, err := http.ReadResponse(bufio.NewReader(conn), req)
					if err != nil {
						t.Fatal("real proxied HTTP failed", err)
					}
					defer response.Body.Close()
					body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
					if err != nil || response.StatusCode != 200 || string(body) != payload || response.Header.Get("X-DPI-Egress") != egress {
						t.Fatal("proxy traffic did not exit from server", response.StatusCode, err)
					}
					if cold && time.Since(began) < 4*time.Second {
						t.Fatal("cold target detection did not exercise core metadata wait")
					}
					count := records.Load()
					if mode == "on" {
						if count < 2 || count > 64 {
							t.Fatal("mask was accepted but TLS fragmentation not observed", count)
						}
					} else if count != 1 {
						t.Fatal("default/restored handshake changed", count)
					}
					t.Logf("TLS records=%d bytes=%d connection+transfer=%s", count, len(body), time.Since(began).Round(time.Millisecond))
				})
			}
		}
	}
}

// Forward exact TLS records unchanged. Count first client handshake records per
// connection, never aggregate separate ordinary handshakes into a fake split.
func dpiTLSRecordRelay(t *testing.T, network, listen, upstream string) (string, *atomic.Uint32) {
	t.Helper()
	ln, err := net.Listen(network, listen)
	if err != nil {
		t.Fatal(err)
	}
	max := new(atomic.Uint32)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var connections []net.Conn
	track := func(c net.Conn) { mu.Lock(); connections = append(connections, c); mu.Unlock() }
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, e := ln.Accept()
			if e != nil {
				return
			}
			track(client)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer client.Close()
				server, e := net.DialTimeout("tcp4", upstream, time.Second)
				if e != nil {
					return
				}
				track(server)
				defer server.Close()
				_ = client.SetDeadline(time.Now().Add(15 * time.Second))
				_ = server.SetDeadline(time.Now().Add(15 * time.Second))
				copied := make(chan struct{})
				go func() { _, _ = io.Copy(client, server); _ = client.Close(); close(copied) }()
				defer func() { _ = server.Close(); _ = client.Close(); <-copied }()
				first := true
				var count uint32
				for {
					header := make([]byte, 5)
					if _, e = io.ReadFull(client, header); e != nil {
						return
					}
					n := int(binary.BigEndian.Uint16(header[3:]))
					body := make([]byte, n)
					if _, e = io.ReadFull(client, body); e != nil {
						return
					}
					if first && header[0] == 22 {
						count++
						for {
							old := max.Load()
							if old >= count || max.CompareAndSwap(old, count) {
								break
							}
						}
					} else {
						first = false
					}
					if _, e = server.Write(append(header, body...)); e != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return ln.Addr().String(), max
}

// Hold all six pinned-core startup probes until the actual REALITY target
// handshake begins. A TCP listener alone does not mean target detection is ready.
// This makes the five-second metadata polling path deterministic without
// changing the core, bypassing its handshake or retrying the measured request.
func dpiHoldREALITYStartupProbes(t *testing.T, cfg *tls.Config) func() {
	t.Helper()
	started := make(chan struct{}, 6)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var calls atomic.Int32
	cfg.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		n := calls.Add(1)
		if n <= 6 {
			started <- struct{}{}
			<-release
		} else if n == 7 {
			time.AfterFunc(time.Second, unblock)
		}
		return nil, nil
	}
	return func() {
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for i := 0; i < 6; i++ {
			select {
			case <-started:
			case <-deadline.C:
				t.Fatal("REALITY did not start all six target probes")
			}
		}
	}
}

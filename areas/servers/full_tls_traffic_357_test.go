package servers

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exact-core, loopback-only proof of new inline trust, HTTP CONNECT header and
// source binding. Required outbound mTLS is separately marked unavailable:
// the independent raw-core probe reproduced a missing client certificate.
func TestFullImport357TLSHeaderTraffic(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN not set")
	}
	serverCertPath, serverKeyPath, _ := selfSigned(t)
	serverCert, e := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if e != nil {
		t.Fatal(e)
	}
	serverPEM, e := os.ReadFile(serverCertPath)
	if e != nil {
		t.Fatal(e)
	}
	var trustLines []any
	for _, s := range strings.Split(strings.TrimSpace(string(serverPEM)), "\n") {
		trustLines = append(trustLines, s)
	}
	for _, kind := range []string{"inline-trust", "utls-inline-trust", "missing-trust"} {
		t.Run(kind, func(t *testing.T) {
			ln, e := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
			if e != nil {
				t.Fatal(e)
			}
			defer ln.Close()
			observed := make(chan string, 1)
			go func() {
				conn, e := ln.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(8 * time.Second))
				if e := conn.(*tls.Conn).Handshake(); e != nil {
					observed <- "TLS handshake: " + e.Error()
					return
				}
				br := bufio.NewReader(conn)
				req, e := http.ReadRequest(br)
				if e != nil {
					observed <- "HTTP request: " + e.Error()
					return
				}
				good := req.Method == "CONNECT" && req.Header.Get("X-BROray-Import") == "synthetic-header" && req.Host == "198.18.0.1:443" && strings.HasPrefix(conn.RemoteAddr().String(), "127.0.0.2:")
				if !good {
					observed <- fmt.Sprintf("HTTP observation method=%t header=%t target=%t source=%t", req.Method == "CONNECT", req.Header.Get("X-BROray-Import") == "synthetic-header", req.Host == "198.18.0.1:443", strings.HasPrefix(conn.RemoteAddr().String(), "127.0.0.2:"))
					return
				}
				if _, e := io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n"); e != nil {
					observed <- "HTTP response: " + e.Error()
					return
				}
				observed <- "verified"
				io.Copy(conn, br)
			}()
			o := fullXray357Base("http")
			o["settings"] = map[string]any{"address": "127.0.0.1", "port": ln.Addr().(*net.TCPAddr).Port, "headers": map[string]any{"X-BROray-Import": "synthetic-header"}}
			o["sendThrough"] = "127.0.0.2"
			certs := []any{}
			if kind != "missing-trust" {
				certs = append(certs, map[string]any{"certificate": trustLines, "usage": "verify"})
			}
			o["streamSettings"].(map[string]any)["tlsSettings"] = map[string]any{"serverName": "localhost", "fingerprint": "unsafe", "disableSystemRoot": true, "minVersion": "1.2", "maxVersion": "1.2", "certificates": certs}
			if kind == "utls-inline-trust" {
				o["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["fingerprint"] = "chrome"
			}
			r := fullXray357Parse(t, o)
			port := freeTCP(t)
			cfg, e := XrayConfig(r, GenSettings{Listen: "127.0.0.1", SocksPort: port, LogLevel: "warning"}, "full-import-357")
			if e != nil {
				t.Fatal(e)
			}
			startXray(t, bin, filepath.Join(t.TempDir(), "client.json"), cfg, port, nil)
			c, e := dialTestSOCKS(fmt.Sprintf("127.0.0.1:%d", port), "198.18.0.1:443")
			var got []byte
			payload := bytes.Repeat([]byte("BROray import TLS header payload."), 2048)
			if e == nil {
				defer c.Close()
				c.SetDeadline(time.Now().Add(4 * time.Second))
				_, e = c.Write(payload)
				if e == nil {
					got = make([]byte, len(payload))
					_, e = io.ReadFull(c, got)
				}
			}
			if kind != "missing-trust" {
				if e != nil || !bytes.Equal(payload, got) {
					phase := "no server observation"
					select {
					case phase = <-observed:
					default:
					}
					t.Fatalf("imported TLS/HTTP tunnel failed: %v; %s", e, phase)
				}
				select {
				case valid := <-observed:
					if valid != "verified" {
						t.Fatal("headers/source/identity not observed")
					}
				case <-time.After(time.Second):
					t.Fatal("no server observation")
				}
				t.Logf("Verified inline trust, custom CONNECT header, source binding and %d-byte echo", len(payload))
			} else if e == nil {
				t.Fatal("TLS negative control returned payload")
			}
		})
	}
}

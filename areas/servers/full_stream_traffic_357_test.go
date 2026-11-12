package servers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Both Xray processes and the target stay on loopback. The target verifies
// the server's distinct source address, proving traffic crossed the server.
func TestFullXray357ProtocolStreamTraffic(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN not set")
	}
	cert, key, _ := selfSigned(t)
	pem, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	var trust []any
	for _, line := range strings.Split(strings.TrimSpace(string(pem)), "\n") {
		trust = append(trust, line)
	}
	for _, tc := range []struct {
		protocol, network string
		badAuth, override bool
	}{
		{"http", "ws", false, false}, {"http", "xhttp", false, false}, {"socks", "grpc", false, false}, {"socks", "xhttp", false, false},
		{"shadowsocks", "raw", false, false}, {"shadowsocks", "ws", false, false}, {"shadowsocks", "httpupgrade", false, false},
		{"http", "ws", true, false}, {"shadowsocks", "raw", false, true},
	} {
		name := tc.protocol + "-" + tc.network
		if tc.badAuth {
			name += "-wrong-password"
		}
		if tc.override {
			name += "-root-endpoint-ignored"
		}
		t.Run(name, func(t *testing.T) {
			target, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			observed := make(chan bool, 1)
			go func() {
				conn, err := target.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(8 * time.Second))
				observed <- strings.HasPrefix(conn.RemoteAddr().String(), "127.0.0.3:")
				io.Copy(conn, conn)
			}()
			serverPort, clientPort := freeTCP(t), freeTCP(t)
			dir := t.TempDir()
			stream := map[string]any{"network": tc.network, "security": "tls"}
			switch tc.network {
			case "ws", "httpupgrade":
				stream[tc.network+"Settings"] = map[string]any{"path": "/stream-357"}
			case "grpc":
				stream["grpcSettings"] = map[string]any{"serviceName": "stream357"}
			case "xhttp":
				stream["xhttpSettings"] = map[string]any{"path": "/stream-357", "mode": "packet-up", "extra": map[string]any{"xPaddingBytes": "10-20"}}
			}
			serverStream := normalize(stream)
			alpn := []any{"http/1.1"}
			if tc.network == "grpc" || tc.network == "xhttp" {
				alpn = []any{"h2"}
			}
			serverStream["tlsSettings"] = map[string]any{"alpn": alpn, "certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}
			serverSettings := map[string]any{}
			switch tc.protocol {
			case "http":
				serverSettings["accounts"] = []any{map[string]any{"user": "fixture-user", "pass": "fixture-password"}}
			case "socks":
				serverSettings = map[string]any{"auth": "password", "accounts": []any{map[string]any{"user": "fixture-user", "pass": "fixture-password"}}}
			case "shadowsocks":
				serverSettings = map[string]any{"method": "chacha20-ietf-poly1305", "password": "fixture-password", "network": "tcp"}
			}
			serverConfig, _ := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "warning"},
				"inbounds":  []any{map[string]any{"listen": "127.0.0.1", "port": serverPort, "protocol": tc.protocol, "settings": serverSettings, "streamSettings": serverStream}},
				"outbounds": []any{map[string]any{"protocol": "freedom", "sendThrough": "127.0.0.3", "settings": map[string]any{"redirect": "127.0.0.1:0", "finalRules": []any{map[string]any{"action": "allow", "ip": []any{"127.0.0.0/8"}}}}}}})
			startXray(t, bin, filepath.Join(dir, "server.json"), serverConfig, serverPort, nil)
			o := fullXray357Base(tc.protocol)
			settings := o["settings"].(map[string]any)
			settings["address"], settings["port"] = "127.0.0.1", serverPort
			if tc.override {
				unused, err := net.Listen("tcp4", "127.0.0.2:0")
				if err != nil {
					t.Fatal(err)
				}
				defer unused.Close()
				// Pinned RAW Dial uses settings' destination, never the root
				// MemoryStreamConfig.Destination. A dead override must therefore
				// not divert the successful tunnel through the nominal server.
				// The opposite expectation failed in integration-five-red-v2.
				stream["address"], stream["port"] = "127.0.0.2", unused.Addr().(*net.TCPAddr).Port
			}
			if tc.badAuth {
				settings["pass"] = "wrong-password"
			}
			stream["tlsSettings"] = map[string]any{"serverName": "localhost", "fingerprint": "unsafe", "disableSystemRoot": true, "alpn": alpn,
				"certificates": []any{map[string]any{"certificate": trust, "usage": "verify"}}}
			o["streamSettings"] = stream
			record := fullXray357Parse(t, o)
			if tc.override && !strings.Contains(fullXray357JSON(t, record["importWarnings"]), "streamSettings.address") {
				t.Fatal("core-ignored root endpoint lacks explicit warning")
			}
			config, err := XrayConfig(record, GenSettings{Listen: "127.0.0.1", SocksPort: clientPort, LogLevel: "warning"}, name)
			if err != nil {
				t.Fatal(err)
			}
			startXray(t, bin, filepath.Join(dir, "client.json"), config, clientPort, nil)
			conn, err := dialTestSOCKS(fmt.Sprintf("127.0.0.1:%d", clientPort), target.Addr().String())
			payload := bytes.Repeat([]byte("BROray full import stream 357."), 2048)
			var response []byte
			if err == nil {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(6 * time.Second))
				_, err = conn.Write(payload)
				if err == nil {
					response = make([]byte, len(payload))
					_, err = io.ReadFull(conn, response)
				}
			}
			if tc.badAuth {
				if err == nil {
					t.Fatal("wrong proxy password returned payload")
				}
				return
			}
			if err != nil || !bytes.Equal(payload, response) {
				t.Fatalf("imported protocol/stream tunnel failed: %v", err)
			}
			select {
			case viaServer := <-observed:
				if !viaServer {
					t.Fatal("traffic bypassed the server")
				}
			case <-time.After(time.Second):
				t.Fatal("target did not observe server connection")
			}
			t.Logf("Verified %s over %s TLS and %d-byte echo through server source", tc.protocol, tc.network, len(payload))
		})
	}
}

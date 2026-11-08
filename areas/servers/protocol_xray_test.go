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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/udpprobe"
)

// Real client/server Xray processes, loopback only, no external service.
func TestXrayAddedProtocols(t *testing.T) {
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
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(5 * time.Second)); io.Copy(c, c) }()
		}
	}()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go udpprobe.Responder(pc)
	cert, key, pin := selfSigned(t)
	private1 := bytes.Repeat([]byte{1}, 32)
	private2 := bytes.Repeat([]byte{2}, 32)
	k1, _ := ecdh.X25519().NewPrivateKey(private1)
	k2, _ := ecdh.X25519().NewPrivateKey(private2)
	enc := base64.StdEncoding.EncodeToString
	for _, kind := range []string{"socks", "http", "https", "wireguard", "hysteria2"} {
		t.Run(kind, func(t *testing.T) {
			sport, cport := freeTCP(t), freeTCP(t)
			dir := t.TempDir()
			t.Cleanup(func() {
				if t.Failed() {
					for _, name := range []string{"error", "access"} {
						b, _ := os.ReadFile(filepath.Join(dir, name))
						t.Logf("client %s: %s", name, b)
					}
				}
			})
			in := map[string]any{"listen": "127.0.0.1", "port": sport}
			wait := sport
			udp := kind != "http" && kind != "https"
			link := ""
			switch kind {
			case "socks":
				in["protocol"] = "socks"
				in["settings"] = map[string]any{"auth": "password", "accounts": []any{map[string]any{"user": "user", "pass": "secret"}}, "udp": true, "ip": "127.0.0.1"}
				link = fmt.Sprintf("socks5://user:secret@127.0.0.1:%d", sport)
			case "http", "https":
				in["protocol"] = "http"
				in["settings"] = map[string]any{"accounts": []any{map[string]any{"user": "user", "pass": "secret"}}}
				link = fmt.Sprintf("%s://user:secret@127.0.0.1:%d", kind, sport)
				if kind == "https" {
					in["streamSettings"] = map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"alpn": []string{"http/1.1"}, "certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}}
					link += "?sni=localhost&pcs=" + pin
				}
			case "wireguard":
				wait = 0
				in["protocol"] = "wireguard"
				in["settings"] = map[string]any{"secretKey": enc(private2), "address": []string{"10.10.0.1/32"}, "noKernelTun": true, "peers": []any{map[string]any{"publicKey": enc(k1.PublicKey().Bytes()), "allowedIPs": []string{"10.10.0.2/32"}}}}
				link = fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = 10.10.0.2/32\n[Peer]\nPublicKey = %s\nEndpoint = 127.0.0.1:%d\nAllowedIPs = 0.0.0.0/0\n", enc(private1), enc(k2.PublicKey().Bytes()), sport)
			case "hysteria2":
				wait = 0
				in["protocol"] = "hysteria"
				in["settings"] = map[string]any{"version": 2, "clients": []any{map[string]any{"auth": "secret"}}}
				in["streamSettings"] = map[string]any{"network": "hysteria", "hysteriaSettings": map[string]any{"version": 2}, "security": "tls", "tlsSettings": map[string]any{"alpn": []string{"h3"}, "certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}}
				link = fmt.Sprintf("hy2://secret@127.0.0.1:%d?sni=localhost&pinSHA256=%s&udpIdleTimeout=41", sport, pin)
			}
			rec, err := ParseLink(link, "test", nil)
			if err != nil {
				t.Fatal(err)
			}
			ccfg, err := XrayConfig(rec, GenSettings{Listen: "127.0.0.1", SocksPort: cport, LogLevel: "debug", AccessLog: filepath.Join(dir, "access"), ErrorLog: filepath.Join(dir, "error")}, "test")
			if err != nil {
				t.Fatal(err)
			}
			scfg, _ := json.Marshal(map[string]any{"inbounds": []any{in}, "outbounds": []any{map[string]any{"protocol": "freedom", "sendThrough": "127.0.0.3", "settings": map[string]any{"redirect": "127.0.0.1:0", "finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.0/8", "198.18.0.0/15"}}}}}}})
			startXray(t, bin, filepath.Join(dir, "server.json"), scfg, wait, nil)
			startXray(t, bin, filepath.Join(dir, "client.json"), ccfg, cport, nil)
			tcpTarget, udpTarget := target.Addr().String(), pc.LocalAddr().String()
			if kind == "wireguard" {
				tcpTarget = strings.Replace(tcpTarget, "127.0.0.1", "198.18.0.1", 1)
				udpTarget = strings.Replace(udpTarget, "127.0.0.1", "198.18.0.1", 1)
			}
			c, err := dialTestSOCKS(fmt.Sprintf("127.0.0.1:%d", cport), tcpTarget)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			payload := []byte("BROray protocol roundtrip")
			if _, err = c.Write(payload); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(payload))
			if _, err = io.ReadFull(c, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, got) {
				t.Fatal("TCP payload differs")
			}
			t.Log("TCP payload returned intact")
			if udp {
				got, e := udpprobe.ViaSOCKS5(fmt.Sprintf("127.0.0.1:%d", cport), udpTarget, 5*time.Second)
				if e != nil {
					b, _ := os.ReadFile(filepath.Join(dir, "error"))
					t.Fatalf("UDP: %v\n%s", e, b)
				}
				if !got.IP.Equal(net.ParseIP("127.0.0.3")) {
					t.Fatal("UDP bypassed server")
				}
				t.Log("UDP crossed server exit")
			}
		})
	}
}

func dialTestSOCKS(proxyAddr, target string) (net.Conn, error) {
	c, e := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if e != nil {
		return nil, e
	}
	c.SetDeadline(time.Now().Add(8 * time.Second))
	fail := func(e error) (net.Conn, error) { c.Close(); return nil, e }
	if _, e = c.Write([]byte{5, 1, 0}); e != nil {
		return fail(e)
	}
	var auth [2]byte
	if _, e = io.ReadFull(c, auth[:]); e != nil {
		return fail(e)
	}
	if auth != [2]byte{5, 0} {
		return fail(fmt.Errorf("SOCKS auth refused"))
	}
	host, port, _ := net.SplitHostPort(target)
	p, _ := strconv.Atoi(port)
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return fail(fmt.Errorf("test requires IPv4"))
	}
	req := append([]byte{5, 1, 0, 1}, ip...)
	req = append(req, byte(p>>8), byte(p))
	if _, e = c.Write(req); e != nil {
		return fail(e)
	}
	var head [4]byte
	if _, e = io.ReadFull(c, head[:]); e != nil {
		return fail(e)
	}
	if head[1] != 0 {
		return fail(fmt.Errorf("SOCKS reply %d", head[1]))
	}
	n := 0
	switch head[3] {
	case 1:
		n = 4
	case 4:
		n = 16
	case 3:
		var sz [1]byte
		if _, e = io.ReadFull(c, sz[:]); e != nil {
			return fail(e)
		}
		n = int(sz[0])
	default:
		return fail(fmt.Errorf("bad address"))
	}
	tail := make([]byte, n+2)
	if _, e = io.ReadFull(c, tail); e != nil {
		return fail(e)
	}
	return c, nil
}

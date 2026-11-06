package servers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/udpprobe"
)

// TestXrayUDPPerProtocol is the protocol/transport half of the UDP audit:
// for each supported protocol a share link is imported by ParseLink, rendered
// by XrayConfig exactly as BROray runs it (SOCKS inbound udp:true → outbound
// "proxy"), and a STUN request goes client → BROray Xray (SOCKS5 UDP
// ASSOCIATE) → test server Xray → freedom from 127.0.0.3 → STUN responder.
// The responder must see 127.0.0.3. Set BRORAY_XRAY_BIN to run.
func TestXrayUDPPerProtocol(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("BRORAY_XRAY_BIN not set")
	}
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go udpprobe.Responder(pc)
	stun := pc.LocalAddr().String()
	// UDP/443 (QUIC) is probed only for the audit log: Xray's VLESS Vision
	// flow rejects it by design (xtls-rprx-vision-udp443 allows it).
	stun443 := ""
	if pc443, err := net.ListenPacket("udp4", "127.0.0.1:443"); err == nil {
		defer pc443.Close()
		go udpprobe.Responder(pc443)
		stun443 = pc443.LocalAddr().String()
	}

	const id = "11111111-2222-4333-8444-555555555555"
	vm, _ := json.Marshal(map[string]string{"v": "2", "ps": "t", "add": "127.0.0.1", "port": "%d", "id": id, "aid": "0", "net": "tcp", "type": "none", "tls": ""})
	cases := []struct {
		name, link, inbound string
	}{
		{"vless-raw", "vless://" + id + "@127.0.0.1:%d?type=tcp&security=none&encryption=none#t",
			`{"protocol":"vless","settings":{"clients":[{"id":"` + id + `"}],"decryption":"none"},"streamSettings":{"network":"raw"}}`},
		{"vmess-raw", "vmess://" + base64.StdEncoding.EncodeToString(vm),
			`{"protocol":"vmess","settings":{"clients":[{"id":"` + id + `"}]},"streamSettings":{"network":"raw"}}`},
		{"trojan-tls", "trojan://secret@127.0.0.1:%d?type=tcp&security=tls&sni=localhost&pcs=PIN#t",
			`{"protocol":"trojan","settings":{"clients":[{"password":"secret"}]},"streamSettings":{"network":"raw","security":"tls","tlsSettings":{"certificates":[{"certificateFile":"CERT","keyFile":"KEY"}]}}}`},
		{"vless-tls-vision", "vless://" + id + "@127.0.0.1:%d?type=tcp&security=tls&sni=localhost&pcs=PIN&flow=xtls-rprx-vision&encryption=none#t",
			`{"protocol":"vless","settings":{"clients":[{"id":"` + id + `","flow":"xtls-rprx-vision"}],"decryption":"none"},"streamSettings":{"network":"raw","security":"tls","tlsSettings":{"certificates":[{"certificateFile":"CERT","keyFile":"KEY"}]}}}`},
		{"vless-ws", "vless://" + id + "@127.0.0.1:%d?type=ws&path=/w&security=none&encryption=none#t",
			`{"protocol":"vless","settings":{"clients":[{"id":"` + id + `"}],"decryption":"none"},"streamSettings":{"network":"ws","wsSettings":{"path":"/w"}}}`},
		{"vless-grpc", "vless://" + id + "@127.0.0.1:%d?type=grpc&serviceName=g&security=none&encryption=none#t",
			`{"protocol":"vless","settings":{"clients":[{"id":"` + id + `"}],"decryption":"none"},"streamSettings":{"network":"grpc","grpcSettings":{"serviceName":"g"}}}`},
		{"vless-httpupgrade", "vless://" + id + "@127.0.0.1:%d?type=httpupgrade&path=/h&security=none&encryption=none#t",
			`{"protocol":"vless","settings":{"clients":[{"id":"` + id + `"}],"decryption":"none"},"streamSettings":{"network":"httpupgrade","httpupgradeSettings":{"path":"/h"}}}`},
		{"vless-xhttp", "vless://" + id + "@127.0.0.1:%d?type=xhttp&path=/x&mode=packet-up&security=none&encryption=none#t",
			`{"protocol":"vless","settings":{"clients":[{"id":"` + id + `"}],"decryption":"none"},"streamSettings":{"network":"xhttp","xhttpSettings":{"path":"/x"}}}`},
		{"vless-kcp", "vless://" + id + "@127.0.0.1:%d?type=kcp&security=none&encryption=none#t",
			`{"protocol":"vless","settings":{"clients":[{"id":"` + id + `"}],"decryption":"none"},"streamSettings":{"network":"kcp"}}`},
		{"hysteria2", "hysteria2://secret@127.0.0.1:%d?sni=localhost&pinSHA256=PIN#t",
			`{"protocol":"hysteria","settings":{"version":2,"clients":[{"auth":"secret"}]},"streamSettings":{"network":"hysteria","hysteriaSettings":{"version":2},"security":"tls","tlsSettings":{"alpn":["h3"],"certificates":[{"certificateFile":"CERT","keyFile":"KEY"}]}}}`},
		{"shadowsocks", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:secret")) + "@127.0.0.1:%d#t",
			`{"protocol":"shadowsocks","settings":{"method":"chacha20-ietf-poly1305","password":"secret","network":"tcp,udp"}}`},
	}
	certFile, keyFile, pin := selfSigned(t)
	env := os.Environ()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sport, cport := freeTCP(t), freeTCP(t)
			link := c.link
			if c.name == "vmess-raw" {
				raw, _ := base64.StdEncoding.DecodeString(link[len("vmess://"):])
				link = "vmess://" + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(string(raw), sport)))
			} else {
				link = strings.Replace(fmt.Sprintf(link, sport), "PIN", pin, 1)
			}
			rec, err := ParseLink(link, "s1", nil)
			if err != nil {
				t.Fatalf("ParseLink: %v", err)
			}
			dir := t.TempDir()
			ccfg, err := XrayConfig(rec, GenSettings{Listen: "127.0.0.1", SocksPort: json.Number(fmt.Sprint(cport)),
				LogLevel: os.Getenv("BRORAY_XRAY_LOGLEVEL"), AccessLog: filepath.Join(dir, "a.log"), ErrorLog: filepath.Join(dir, "e.log")}, "s1")
			if err != nil {
				t.Fatalf("XrayConfig: %v", err)
			}
			var in map[string]any
			inb := strings.NewReplacer(`"CERT"`, strconv.Quote(certFile), `"KEY"`, strconv.Quote(keyFile)).Replace(c.inbound)
			if err := json.Unmarshal([]byte(inb), &in); err != nil {
				t.Fatal(err)
			}
			in["listen"], in["port"] = "127.0.0.1", sport
			scfg, _ := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{in},
				"outbounds": []any{map[string]any{"protocol": "freedom", "sendThrough": "127.0.0.3",
					"settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "ip": []any{"127.0.0.0/8"}}}}}}})
			wait := sport
			if strings.Contains(c.inbound, `"kcp"`) || strings.Contains(c.inbound, `"hysteria"`) {
				wait = 0 // mKCP / Hysteria listen on UDP
			}
			startXray(t, bin, filepath.Join(dir, "server.json"), scfg, wait, nil)
			startXray(t, bin, filepath.Join(dir, "client.json"), ccfg, cport, env)
			got, err := udpprobe.ViaSOCKS5(fmt.Sprintf("127.0.0.1:%d", cport), stun, 4*time.Second)
			if err != nil {
				el, _ := os.ReadFile(filepath.Join(dir, "e.log"))
				t.Fatalf("UDP via %s: %v\nclient error.log:\n%s", c.name, err, el)
			}
			if !got.IP.Equal(net.ParseIP("127.0.0.3")) {
				t.Fatalf("UDP via %s left from %v, not from the server", c.name, got)
			}
			t.Logf("%s: STUN mapped %v (server exit)", c.name, got)
			if stun443 != "" {
				q, err := udpprobe.ViaSOCKS5(fmt.Sprintf("127.0.0.1:%d", cport), stun443, 2*time.Second)
				t.Logf("%s: UDP/443 (QUIC port) → %v, err=%v", c.name, q, err)
			}
		})
	}
}

func freeTCP(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startXray(t *testing.T, bin, path string, cfg []byte, port int, env []string) {
	t.Helper()
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-c", path)
	cmd.Env = env
	out, _ := os.Create(path + ".out")
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		out.Close()
		if t.Failed() {
			b, _ := os.ReadFile(path + ".out")
			t.Logf("%s:\n%s", filepath.Base(path), b)
		}
	})
	if port == 0 {
		time.Sleep(500 * time.Millisecond)
		return
	}
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			c.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("xray %s did not listen on %d", filepath.Base(path), port)
}

// selfSigned writes a localhost certificate; the BROray-side Xray accepts it
// by the share-link pin (pcs → pinnedPeerCertSha256), allowInsecure stays false.
func selfSigned(t *testing.T) (string, string, string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	dir := t.TempDir()
	c, k := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	_ = os.WriteFile(c, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(k, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0o600)
	sum := sha256.Sum256(der)
	return c, k, hex.EncodeToString(sum[:])
}

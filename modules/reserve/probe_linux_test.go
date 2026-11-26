package reserve

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func configHash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

type httpsFixture struct {
	roots            *x509.CertPool
	address          string
	status           atomic.Int32
	cloudflareStatus atomic.Int32
	calls            atomic.Int32
	mu               sync.Mutex
	during           func()
}

func localControlTLS(t *testing.T) *httpsFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "BROray local test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"cp.cloudflare.com", "www.gstatic.com"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	f := &httpsFixture{roots: x509.NewCertPool()}
	f.roots.AddCert(parsed)
	f.status.Store(204)
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.mu.Lock()
		during := f.during
		f.mu.Unlock()
		if during != nil {
			during()
		}
		if r.URL.Path != "/generate_204" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Location", "https://cp.cloudflare.com/redirected")
		if status := f.cloudflareStatus.Load(); r.Host == "cp.cloudflare.com" && status != 0 {
			w.WriteHeader(int(status))
			return
		}
		w.WriteHeader(int(f.status.Load()))
	}))
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	s.StartTLS()
	t.Cleanup(s.Close)
	f.address = s.Listener.Addr().String()
	return f
}

// Resolve only the fixed control hosts to the local TLS fixture. Nothing in
// the offline guest can contact the public control endpoints.
func localTLSUpstream(t *testing.T, dest string) netip.AddrPort {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				var hello [2]byte
				if _, err := io.ReadFull(c, hello[:]); err != nil || hello[0] != 5 || hello[1] == 0 {
					return
				}
				if _, err := io.ReadFull(c, make([]byte, int(hello[1]))); err != nil {
					return
				}
				if _, err := c.Write([]byte{5, 0}); err != nil {
					return
				}
				var request [5]byte
				if _, err := io.ReadFull(c, request[:]); err != nil || request[0] != 5 || request[1] != 1 || request[3] != 3 {
					return
				}
				target := make([]byte, int(request[4])+2)
				if _, err := io.ReadFull(c, target); err != nil {
					return
				}
				host := string(target[:len(target)-2])
				if (host != "cp.cloudflare.com" && host != "www.gstatic.com") || binary.BigEndian.Uint16(target[len(target)-2:]) != 443 {
					return
				}
				remote, err := net.DialTimeout("tcp", dest, time.Second)
				if err != nil {
					return
				}
				defer remote.Close()
				_ = remote.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
					return
				}
				done := make(chan struct{})
				go func() { _, _ = io.Copy(remote, c); _ = remote.Close(); close(done) }()
				_, _ = io.Copy(c, remote)
				_ = c.Close()
				<-done
			}()
		}
	}()
	return l.Addr().(*net.TCPAddr).AddrPort()
}

func localProbeTarget(t *testing.T, upstream netip.AddrPort, network string) ProbeTarget {
	t.Helper()
	bin := os.Getenv("BRORAY_XRAY_BIN")
	binary, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if configHash(binary) != "c2d1e66a130556d32089adcf38202fcb6f9027e41fb456cc2cda9a5fabdc093a" {
		t.Fatal("unexpected Xray fixture")
	}
	address := "127.0.0.1:0"
	if network == "tcp6" {
		address = "[::1]:0"
	}
	normal := healthFreeEndpoint(t, network, address)
	diagnostic := healthFreeEndpoint(t, network, address)
	if normal.Port() == diagnostic.Port() {
		t.Fatal("fixture collision; no silent retry")
	}
	cfg := map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": []any{map[string]any{"tag": "socks", "listen": normal.Addr().String(), "port": normal.Port(), "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}}, "outbounds": []any{map[string]any{"tag": "proxy", "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": upstream.Addr().String(), "port": upstream.Port()}}}}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data, err := AttachHealthInbound(raw, diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-c", path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// No asynchronous Wait: the exact child retains its PID until cleanup.
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p, err := CaptureProcess(cmd.Process.Pid, bin)
		if err == nil {
			if _, err = VerifyListener(p, diagnostic); err == nil {
				return ProbeTarget{Process: p, ConfigPath: path, ConfigSHA256: configHash(data), Endpoint: diagnostic}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("owned Xray not ready")
	return ProbeTarget{}
}

func TestReserveHTTPSActualTrafficAndValidation(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			f := localControlTLS(t)
			up := localTLSUpstream(t, f.address)
			target := localProbeTarget(t, up, network)
			result := probeWithRoots(context.Background(), target, f.roots)
			if result.Verdict != "healthy" || result.Identity == "" || result.ErrorCode != "" || f.calls.Load() != 1 || result.FinishedAt.Before(result.StartedAt) {
				t.Fatal("validated HTTPS204 not healthy", result, f.calls.Load())
			}
			identity := result.Identity
			f.cloudflareStatus.Store(503)
			result = probeWithRoots(context.Background(), target, f.roots)
			if result.Verdict != "healthy" || result.Identity != identity || len(result.Controls) != 2 ||
				result.Controls[0].Target != "cloudflare" || result.Controls[0].HTTPStatus != 503 ||
				result.Controls[1].Target != "google" || result.Controls[1].HTTPStatus != 204 {
				t.Fatal("second control should confirm traffic and retain the first status", result)
			}
			f.cloudflareStatus.Store(0)
			for _, status := range []int{200, 302, 500} {
				f.status.Store(int32(status))
				before := f.calls.Load()
				result = probeWithRoots(context.Background(), target, f.roots)
				if result.Verdict != "unhealthy" || result.Identity != identity || f.calls.Load() != before+2 {
					t.Fatal("status/redirect incorrectly healthy", status, result, f.calls.Load())
				}
			}
			f.status.Store(204)
			before := f.calls.Load()
			result = probeWithRoots(context.Background(), target, x509.NewCertPool())
			if result.Verdict != "unhealthy" || result.Identity != identity || f.calls.Load() != before {
				t.Fatal("untrusted TLS certificate accepted", result)
			}
			result = Probe(context.Background(), target)
			if result.Verdict != "unhealthy" || result.Identity != identity || f.calls.Load() != before {
				t.Fatal("public probe accepted private fixture CA", result)
			}
		})
	}
}

func TestReserveHTTPSRejectsAlteredRouteAndSymlink(t *testing.T) {
	for _, kind := range []string{"route", "inbound", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := localControlTLS(t)
			target := localProbeTarget(t, localTLSUpstream(t, f.address), "tcp4")
			data, err := os.ReadFile(target.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				real := target.ConfigPath + ".real"
				if err := os.Rename(target.ConfigPath, real); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, target.ConfigPath); err != nil {
					t.Fatal(err)
				}
			} else {
				cfg := configObject(t, data)
				if kind == "route" {
					cfg["routing"].(map[string]any)["rules"].([]any)[0].(map[string]any)["port"] = "80,443"
				} else {
					cfg["inbounds"].([]any)[1].(map[string]any)["listen"] = "0.0.0.0"
				}
				data, err = json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target.ConfigPath, data, 0600); err != nil {
					t.Fatal(err)
				}
				// Matching bytes alone are insufficient: the exact owned route
				// and restricted listener still have to be reconstructed.
				target.ConfigSHA256 = configHash(data)
			}
			result := probeWithRoots(context.Background(), target, f.roots)
			if result.Verdict != "unknown" || result.Identity != "" || f.calls.Load() != 0 {
				t.Fatal("unsafe diagnostic proof accepted", kind, result)
			}
		})
	}
}

func TestReserveHTTPSRejectsChangedEvidenceAndCancellation(t *testing.T) {
	f := localControlTLS(t)
	target := localProbeTarget(t, localTLSUpstream(t, f.address), "tcp4")
	for name, mutate := range map[string]func(*ProbeTarget){
		"hash":  func(p *ProbeTarget) { p.ConfigSHA256 = strings.Repeat("a", 64) },
		"owner": func(p *ProbeTarget) { p.Process.StartTicks++ },
		"cmdline": func(p *ProbeTarget) {
			data, err := os.ReadFile(p.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			p.ConfigPath = filepath.Join(t.TempDir(), "other.json")
			if err := os.WriteFile(p.ConfigPath, data, 0600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := target
			mutate(&bad)
			result := probeWithRoots(context.Background(), bad, f.roots)
			if result.Verdict != "unknown" || result.Identity != "" || f.calls.Load() != 0 {
				t.Fatal("unbound proof accepted", result)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := probeWithRoots(ctx, target, f.roots)
	if result.Verdict != "unknown" || f.calls.Load() != 0 {
		t.Fatal("cancelled probe contacted network", result)
	}
	writeResult := make(chan error, 1)
	f.mu.Lock()
	f.during = func() { writeResult <- os.WriteFile(target.ConfigPath, []byte("{}"), 0600) }
	f.mu.Unlock()
	result = probeWithRoots(context.Background(), target, f.roots)
	select {
	case writeErr := <-writeResult:
		if writeErr != nil {
			t.Fatal(writeErr)
		}
	default:
		t.Fatal("configuration mutation fixture was not reached")
	}
	if result.Verdict != "unknown" || result.Identity != "" || f.calls.Load() != 1 {
		t.Fatal("changed configuration accepted after HTTP204", result)
	}
}

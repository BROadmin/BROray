package servers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

// The synthetic CA is confined to this callback. Production reserve.Probe keeps
// system trust and its fixed HTTPS endpoints; the fixture proves actual native
// proxy traffic and proposal isolation, not public-Internet or ISP efficacy.
func dpiProposalControl(t *testing.T) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "BROray DPI local control"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"cp.cloudflare.com", "www.gstatic.com"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate_204" || r.Host != "www.gstatic.com" {
			w.WriteHeader(404)
			return
		}
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		w.Header().Set("X-DPI-Egress", host)
		w.WriteHeader(204)
	}))
	server.Config.ReadHeaderTimeout = 3 * time.Second
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, roots
}

func TestDPIProposalNativeHTTPS(t *testing.T) {
	for _, family := range []string{"tcp4", "tcp6"} {
		for _, mode := range []string{"on", "off"} {
			t.Run(family+"/"+mode, func(t *testing.T) {
				f := newReserveRuntimeFixture(t)
				control, roots := dpiProposalControl(t)
				cert, key, pin := selfSigned(t)
				port := freeTCP(t)
				cfg, err := json.Marshal(map[string]any{
					"log":       map[string]any{"loglevel": "warning"},
					"inbounds":  []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "trojan", "settings": map[string]any{"clients": []any{map[string]any{"password": "synthetic-dpi"}}}, "streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}}}},
					"outbounds": []any{map[string]any{"protocol": "freedom", "sendThrough": "127.0.0.3", "settings": map[string]any{"redirect": control.Listener.Addr().String(), "finalRules": []any{map[string]any{"action": "allow", "ip": []any{"127.0.0.0/8"}}}}}},
				})
				if err != nil {
					t.Fatal(err)
				}
				startXray(t, os.Getenv("BRORAY_XRAY_BIN"), filepath.Join(t.TempDir(), "server.json"), cfg, port, nil)
				host := "127.0.0.1"
				if family == "tcp6" {
					host = "::1"
				}
				endpoint, records := dpiTLSRecordRelay(t, family, net.JoinHostPort(host, "0"), net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
				id := f.e.addServer(t, "trojan://synthetic-dpi@"+endpoint+"?type=tcp&security=tls&sni=localhost&pcs="+pin+"#DPI")
				rec, err := ReadRecord(f.e.d, id)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "off" {
					rec, err = prepareDPIRecord(rec, true)
					if err != nil {
						t.Fatal(err)
					}
					if err = WriteRecord(f.e.d, rec); err != nil {
						t.Fatal(err)
					}
					rec, err = ReadRecord(f.e.d, id)
					if err != nil {
						t.Fatal(err)
					}
				}
				next, err := prepareDPIRecord(rec, mode == "on")
				if err != nil {
					t.Fatal(err)
				}
				path := RecordPath(f.e.d, id)
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				snapshot := f.process.RuntimeSnapshot()
				calls := 0
				got, err := f.e.s.probeDPIProposalWith(f.ctx, path, compat.SHA256Hex(before), next, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
					calls++
					start := time.Now()
					proof, err := reserve.VerifyProbeTarget(target)
					if err != nil {
						t.Fatal(err)
					}
					conn, err := dialTestSOCKS(target.Endpoint.String(), "www.gstatic.com:443")
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					if err = conn.SetDeadline(time.Now().Add(8 * time.Second)); err != nil {
						t.Fatal(err)
					}
					secure := tls.Client(conn, &tls.Config{RootCAs: roots, ServerName: "www.gstatic.com", MinVersion: tls.VersionTLS12})
					if err = secure.HandshakeContext(ctx); err != nil {
						t.Fatal("real proxied TLS", err)
					}
					req, err := http.NewRequestWithContext(ctx, "GET", "https://www.gstatic.com/generate_204", nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Close = true
					if err = req.Write(secure); err != nil {
						t.Fatal(err)
					}
					response, err := http.ReadResponse(bufio.NewReader(secure), req)
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					if response.StatusCode != 204 || response.Header.Get("X-DPI-Egress") != "127.0.0.3" {
						t.Fatal("proxy HTTPS/egress absent", response.Status, response.Header)
					}
					after, err := reserve.VerifyProbeTarget(target)
					if err != nil || after != proof {
						t.Fatal("proof changed", err)
					}
					return reserve.ProbeOutcome{Verdict: "healthy", Identity: proof, StartedAt: start, FinishedAt: time.Now()}
				})
				if err != nil || !got.Verified || got.Verdict != "healthy" || calls != 1 {
					t.Fatal(got, err, calls)
				}
				count := records.Load()
				if (mode == "on" && count < 2) || (mode == "off" && count != 1) {
					t.Fatal("fragmentation behavior", mode, count)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) || snapshot != f.process.RuntimeSnapshot() {
					t.Fatal("main/record mutation", err)
				}
				t.Logf("actual HTTPS204 through native %s proposal %s; TLS handshake records %d", family, mode, count)
			})
		}
	}
}

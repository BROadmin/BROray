package dns

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

// probeResult is the outcome of one TLS/SNI check.
type probeResult struct {
	OK        bool
	Status    string // ok | failed | unavailable
	Message   string
	LatencyMs any // whole seconds × 1000 (3.x date +%s arithmetic), null when unavailable
	EndEpoch  int64
}

const (
	msgProbeOK          = "TLS-соединение и имя сертификата проверены."
	msgProbeFailed      = "TLS-проверка завершилась ошибкой."
	msgProbeUnavailable = "OpenSSL недоступен."
	probeTimeout        = 12 * time.Second
)

// Entware keeps the CA bundle under /opt; Go's default search path does not
// include it.
var extraCAFiles = []string{"/opt/etc/ssl/certs/ca-certificates.crt", "/opt/etc/ssl/cert.pem", "/opt/etc/ssl/certs/ca-bundle.crt"}

var (
	rootsOnce sync.Once
	roots     *x509.CertPool
)

func caRoots() *x509.CertPool {
	rootsOnce.Do(func() {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		added := false
		for _, f := range extraCAFiles {
			if b, err := os.ReadFile(f); err == nil && pool.AppendCertsFromPEM(b) {
				added = true
			}
		}
		if err == nil || added {
			roots = pool
		}
	})
	return roots
}

// tlsProbe replaces `openssl s_client -connect a:p -servername sni
// -verify_hostname sni -verify_return_error -brief` under a 12 s timeout:
// a completed handshake whose chain verifies and whose certificate matches
// the SNI host name.
func tlsProbe(ctx context.Context, c catEntry) probeResult {
	pool := caRoots()
	end := time.Now().Unix()
	if pool == nil {
		return probeResult{OK: false, Status: "unavailable", Message: msgProbeUnavailable, LatencyMs: nil, EndEpoch: end}
	}
	start := time.Now().Unix()
	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	d := &tls.Dialer{NetDialer: &net.Dialer{}, Config: &tls.Config{ServerName: c.SNI, RootCAs: pool, MinVersion: tls.VersionTLS12}}
	conn, err := d.DialContext(cctx, "tcp", net.JoinHostPort(c.Address, strconv.Itoa(853)))
	end = time.Now().Unix()
	latency := (end - start) * 1000
	if err != nil {
		return probeResult{OK: false, Status: "failed", Message: msgProbeFailed, LatencyMs: latency, EndEpoch: end}
	}
	_ = conn.Close()
	return probeResult{OK: true, Status: "ok", Message: msgProbeOK, LatencyMs: latency, EndEpoch: end}
}

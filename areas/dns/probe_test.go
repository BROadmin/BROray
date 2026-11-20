package dns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

// A real TLS endpoint on the exact production port. Only the external DNS
// provider is replaced; the production TCP/TLS/DNS client runs unchanged.
func probeServer(t *testing.T, address, mode string) <-chan bool {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"resolver.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	// caRoots is process-cached in production. These nonparallel tests replace
	// only its trust store, then restore it after the listener has stopped.
	prior := caRoots()
	roots = x509.NewCertPool()
	if mode != "untrusted" {
		roots.AddCert(cert)
	}
	t.Cleanup(func() { roots = prior })
	network := "tcp4"
	if address == "::1" {
		network = "tcp6"
	}
	listener, err := tls.Listen(network, net.JoinHostPort(address, "853"), &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	} // IPv6 is required here, never silently skipped.
	observed := make(chan bool, 1)
	done := make(chan struct{})
	var mu sync.Mutex
	var active net.Conn
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		mu.Lock()
		active = conn
		mu.Unlock()
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if mode == "resolver-timeout" {
			_ = conn.SetDeadline(time.Now().Add(11 * time.Second))
		}
		if conn.(*tls.Conn).Handshake() != nil {
			return
		}
		var head [2]byte
		if _, err := io.ReadFull(conn, head[:]); err != nil {
			observed <- false
			return
		}
		n := int(binary.BigEndian.Uint16(head[:]))
		if n < 17 || n > 4096 {
			observed <- false
			return
		}
		q := make([]byte, n)
		if _, err := io.ReadFull(conn, q); err != nil {
			observed <- false
			return
		}
		valid := q[2]&0x80 == 0 && q[4] == 0 && q[5] == 1 && string(q[12:17]) == "\x00\x00\x02\x00\x01"
		observed <- valid
		if !valid {
			return
		}
		if mode == "silent" || mode == "resolver-timeout" {
			var b [1]byte
			_, _ = conn.Read(b[:])
			return
		}
		// Independent DNS wire fixture: root IN NS -> a.root-servers.net.
		reply := append([]byte(nil), q[:17]...)
		reply[2], reply[3] = 0x81, 0x80
		reply[6], reply[7], reply[8], reply[9], reply[10], reply[11] = 0, 1, 0, 0, 0, 0
		rdata := []byte("\x01a\x0croot-servers\x03net\x00")
		reply = append(reply, 0, 0, 2, 0, 1, 0, 0, 0, 60, 0, byte(len(rdata)))
		reply = append(reply, rdata...)
		switch mode {
		case "empty":
			reply = reply[:17]
			reply[7] = 0
		case "wrong-id":
			reply[0] ^= 0xff
		case "wrong-question":
			reply[14] = 1
		case "query-instead-of-answer":
			reply[2] &^= 0x80
		case "servfail":
			reply[3] = 0x82
		case "nxdomain":
			reply[3] = 0x83
		case "malformed":
			reply = reply[:20]
		case "bad-rdata":
			reply[len(reply)-1] = 0xc0
		}
		binary.BigEndian.PutUint16(head[:], uint16(len(reply)))
		_, _ = conn.Write(append(head[:], reply...))
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		if active != nil {
			_ = active.Close()
		}
		mu.Unlock()
		<-done
	})
	return observed
}

func TestProbeRequiresDNSAnswer(t *testing.T) {
	for _, mode := range []string{"empty", "wrong-id", "wrong-question", "query-instead-of-answer", "servfail", "nxdomain", "malformed", "bad-rdata", "silent"} {
		t.Run(mode, func(t *testing.T) {
			probeServer(t, "127.0.0.1", mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got := tlsProbe(ctx, catEntry{Address: "127.0.0.1", SNI: "resolver.test"})
			if got.OK || got.Status != "failed" {
				t.Fatalf("TLS alone must not pass DNS probe: %+v", got)
			}
			if got.TCP != "ok" || got.TLS != "ok" || got.DNS != "failed" {
				t.Fatalf("DNS failure must preserve successful transport/TLS: %+v", got)
			}
			if mode == "silent" && got.ErrorCode != "DOT_PROBE_TIMEOUT" {
				t.Fatalf("DNS timeout must be explicit: %+v", got)
			}
		})
	}
}

func TestProbeRealIPv4AndIPv6DNS(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1"} {
		t.Run(address, func(t *testing.T) {
			seen := probeServer(t, address, "ok")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			got := tlsProbe(ctx, catEntry{Address: address, SNI: "resolver.test"})
			if !got.OK {
				t.Fatalf("real DNS resolution: %+v", got)
			}
			family := "IPv4"
			if address == "::1" {
				family = "IPv6"
			}
			if got.Family != family || got.TCP != "ok" || got.TLS != "ok" || got.DNS != "ok" || got.ErrorCode != "" {
				t.Fatalf("transport family and independent stages: %+v", got)
			}
			select {
			case valid := <-seen:
				if !valid {
					t.Fatal("no real DNS query received over selected address family")
				}
			case <-ctx.Done():
				t.Fatal("probe returned success without DNS query")
			}
		})
	}
}

func TestProbeRejectsCertificateErrors(t *testing.T) {
	for _, mode := range []string{"untrusted", "wrong-sni"} {
		t.Run(mode, func(t *testing.T) {
			probeServer(t, "127.0.0.1", mode)
			sni := "resolver.test"
			if mode == "wrong-sni" {
				sni = "wrong.test"
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if got := tlsProbe(ctx, catEntry{Address: "127.0.0.1", SNI: sni}); got.OK || got.TCP != "ok" || got.TLS != "failed" || got.DNS != "not_checked" || got.ErrorCode != "DOT_TLS_FAILED" {
				t.Fatalf("unverified TLS accepted: %+v", got)
			}
		})
	}
}

func TestProbeCancellationDuringDNS(t *testing.T) {
	seen := probeServer(t, "::1", "silent")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan probeResult, 1)
	go func() { done <- tlsProbe(ctx, catEntry{Address: "::1", SNI: "resolver.test"}) }()
	select {
	case valid := <-seen:
		if !valid {
			t.Fatal("missing DNS query")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DNS exchange did not start")
	}
	cancel()
	select {
	case got := <-done:
		if got.OK || got.ErrorCode != "DOT_PROBE_CANCELLED" || got.TLS != "ok" || got.DNS != "failed" {
			t.Fatalf("cancellation evidence: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt DNS read")
	}
}

func TestProbeResolverReadTimeout(t *testing.T) {
	probeServer(t, "127.0.0.1", "resolver-timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got := tlsProbe(ctx, catEntry{Address: "127.0.0.1", SNI: "resolver.test"})
	if got.OK || got.TLS != "ok" || got.DNS != "failed" || got.ErrorCode != "DOT_PROBE_TIMEOUT" {
		t.Fatalf("resolver read timeout lost behind retry refusal: %+v", got)
	}
}

func TestProbeRejectsNonLiteralAndAmbiguousAddresses(t *testing.T) {
	for _, address := range []string{"localhost", "[::1]", "::ffff:127.0.0.1", "fe80::1%eth0", "127.000.0.1", "::1;reboot"} {
		got := tlsProbe(context.Background(), catEntry{Address: address, SNI: "resolver.test"})
		if got.OK || got.ErrorCode != "DOT_ADDRESS_INVALID" || got.TCP != "not_checked" {
			t.Fatalf("address %q: %+v", address, got)
		}
	}
}

func TestProbeMissingCAAndCancelledConnect(t *testing.T) {
	prior := caRoots()
	roots = nil
	t.Cleanup(func() { roots = prior })
	got := tlsProbe(context.Background(), catEntry{Address: "::1", SNI: "resolver.test"})
	if got.OK || got.Status != "unavailable" || got.ErrorCode != "DOT_CA_UNAVAILABLE" || got.TCP != "not_checked" || got.LatencyMs != nil {
		t.Fatalf("CA unavailable: %+v", got)
	}
	roots = x509.NewCertPool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got = tlsProbe(ctx, catEntry{Address: "::1", SNI: "resolver.test"})
	if got.OK || got.ErrorCode != "DOT_PROBE_CANCELLED" || got.TCP != "failed" || got.TLS != "not_checked" {
		t.Fatalf("cancelled connect: %+v", got)
	}
}

func TestProbeEvidencePersistsManualAndAuto(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		name := "manual"
		if automatic {
			name = "automatic"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.s.probe = func(context.Context, catEntry) probeResult {
				return probeResult{Status: "failed", Family: "IPv4", TCP: "ok", TLS: "ok", DNS: "failed", ErrorCode: "DOT_DNS_FAILED", EndEpoch: h.now.Unix(), LatencyMs: int64(21)}
			}
			if _, err := h.s.ensureFiles(); err != nil {
				t.Fatal(err)
			}
			if automatic {
				if err := h.s.autoSave(true); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() { defer close(done); h.d.Queue.Run(ctx) }()
				t.Cleanup(func() { cancel(); <-done })
				h.s.enqueueDue()
				deadline := time.Now().Add(5 * time.Second)
				finished := false
				for time.Now().Before(deadline) {
					state, err := readMap(h.s.statePath())
					if err != nil {
						t.Fatal(err)
					}
					if ac, ok := state["autoCheck"].(map[string]any); ok && ac["status"] == "failed" {
						finished = true
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !finished {
					t.Fatal("automatic probe did not finish")
				}
			} else {
				if _, err := h.s.runTest(context.Background(), map[string]any{"serverIds": []any{"google-primary"}}); err != nil {
					t.Fatal(err)
				}
			}
			state, err := readMap(h.s.statePath())
			if err != nil {
				t.Fatal(err)
			}
			rows := testRows(state)
			if len(rows) == 0 {
				t.Fatal("missing probe results")
			}
			for _, row := range rows {
				for key, want := range map[string]any{"ok": false, "addressFamily": "IPv4", "tcpStatus": "ok", "tlsStatus": "ok", "dnsStatus": "failed", "errorCode": "DOT_DNS_FAILED"} {
					if row.raw[key] != want {
						t.Errorf("%s=%v, want %v", key, row.raw[key], want)
					}
				}
			}
			if testsFreshAndOK(state, []string{"google-primary"}, h.now.Unix()) {
				t.Fatal("DNS failure authorized apply")
			}
		})
	}
}

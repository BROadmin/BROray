package dns

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// probeResult describes the selected IP transport, verified TLS and DNS exchange
// independently. A DNS record type is never evidence of the transport family.
type probeResult struct {
	OK        bool
	Status    string // ok | failed | unavailable
	Message   string
	LatencyMs any // complete probe duration in milliseconds; null when unavailable
	EndEpoch  int64
	Family    string
	TCP       string // ok | failed | not_checked
	TLS       string
	DNS       string
	ErrorCode string
}

const (
	msgProbeOK          = "Соединение, TLS-сертификат и DNS-ответ проверены."
	msgProbeFailed      = "TLS-проверка завершилась ошибкой."
	msgProbeUnavailable = "Хранилище доверенных TLS-сертификатов недоступно."
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

// tlsProbe uses one literal IP, one TCP/TLS connection and a real root NS
// lookup. LookupNS does not consult /etc/hosts. The custom resolver cannot
// fall back to a system DNS server or reuse a successful result from a cache.
func tlsProbe(ctx context.Context, c catEntry) (result probeResult) {
	start := time.Now()
	result = probeResult{Status: "failed", TCP: "not_checked", TLS: "not_checked", DNS: "not_checked"}
	defer func() {
		result.EndEpoch = time.Now().Unix()
		if result.Status != "unavailable" {
			result.LatencyMs = time.Since(start).Milliseconds()
		}
	}()
	address, err := netip.ParseAddr(c.Address)
	if err != nil || address.Zone() != "" || address.Is4In6() {
		result.ErrorCode, result.Message = "DOT_ADDRESS_INVALID", "Некорректный IP-адрес DNS-over-TLS сервера."
		return
	}
	result.Family = "IPv4"
	network := "tcp4"
	if address.Is6() {
		result.Family, network = "IPv6", "tcp6"
	}
	pool := caRoots()
	if pool == nil {
		result.Status, result.ErrorCode, result.Message = "unavailable", "DOT_CA_UNAVAILABLE", msgProbeUnavailable
		return
	}
	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(cctx, network, net.JoinHostPort(address.String(), "853"))
	if err != nil {
		result.TCP = "failed"
		result.ErrorCode, result.Message = "DOT_CONNECT_FAILED", "Не удалось подключиться к выбранному DNS-over-TLS серверу."
		if address.Is6() && (errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.EAFNOSUPPORT)) {
			result.ErrorCode, result.Message = "DOT_IPV6_PATH_UNAVAILABLE", "Нет доступного IPv6-пути к выбранному серверу."
		}
		result.contextFailure(cctx, err)
		return
	}
	defer raw.Close()
	// Closing on cancellation also interrupts TLS/DNS reads before their deadline.
	stop := context.AfterFunc(cctx, func() { _ = raw.Close() })
	defer stop()
	deadline, _ := cctx.Deadline()
	if err = raw.SetDeadline(deadline); err != nil {
		result.TCP, result.ErrorCode, result.Message = "failed", "DOT_CONNECT_FAILED", "Не удалось ограничить время проверки соединения."
		return
	}
	result.TCP = "ok"
	conn := tls.Client(raw, &tls.Config{ServerName: c.SNI, RootCAs: pool, MinVersion: tls.VersionTLS12})
	if err = conn.HandshakeContext(cctx); err != nil {
		result.TLS, result.ErrorCode, result.Message = "failed", "DOT_TLS_FAILED", msgProbeFailed
		result.contextFailure(cctx, err)
		return
	}
	result.TLS = "ok"
	dnsConn := &probeDNSConn{Conn: conn}
	var mu sync.Mutex
	used := false
	resolver := &net.Resolver{PreferGo: true, StrictErrors: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		if used {
			return nil, errors.New("selected DoT connection already used")
		}
		used = true
		return dnsConn, nil
	}}
	answers, err := resolver.LookupNS(cctx, ".")
	if err != nil || len(answers) == 0 {
		result.DNS, result.ErrorCode, result.Message = "failed", "DOT_DNS_FAILED", "TLS проверен, но корректный DNS-ответ не получен."
		if dnsConn.timedOut.Load() {
			err = context.DeadlineExceeded
		}
		result.contextFailure(cctx, err)
		return
	}
	result.DNS, result.OK, result.Status, result.Message = "ok", true, "ok", msgProbeOK
	return
}

// The resolver can replace its first read error with an error from a retry.
// Keep the timeout evidence while continuing to refuse all fallback dials.
type probeDNSConn struct {
	net.Conn
	timedOut atomic.Bool
}

func (c *probeDNSConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		c.timedOut.Store(true)
	}
	return n, err
}

func (p *probeResult) contextFailure(ctx context.Context, err error) {
	if errors.Is(ctx.Err(), context.Canceled) {
		p.ErrorCode, p.Message = "DOT_PROBE_CANCELLED", "Проверка DNS-over-TLS отменена."
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		p.ErrorCode, p.Message = "DOT_PROBE_TIMEOUT", "Истекло время проверки DNS-over-TLS."
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		p.ErrorCode, p.Message = "DOT_PROBE_TIMEOUT", "Истекло время проверки DNS-over-TLS."
	}
}

func (p probeResult) addEvidence(row map[string]any) {
	row["addressFamily"], row["tcpStatus"], row["tlsStatus"], row["dnsStatus"] = p.Family, p.TCP, p.TLS, p.DNS
	row["errorCode"] = p.ErrorCode
}

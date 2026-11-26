package routes

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/strictjson"
	modxray "github.com/BROadmin/broray/modules/xray"
	"golang.org/x/sys/unix"
)

const premiumID = "own-premium-domains"
const premiumName = "Premium домены"

type premiumProof struct {
	domain, user, identity string
	expires                time.Time
	isIP                   bool
}

type premiumObservation struct {
	State string `json:"state"`
	Code  string `json:"code"`
	HTTP  int    `json:"http,omitempty"`
}

type premiumCheck struct {
	Domain     string             `json:"domain"`
	Direct     premiumObservation `json:"direct"`
	VPN        premiumObservation `json:"vpn"`
	Token      string             `json:"token,omitempty"`
	AddBlocked string             `json:"addBlocked,omitempty"`
}

func premiumDomain(input string) (string, bool) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input), "."))
	if !keenetic.ValidNativeDomainName(host) {
		return "", false
	}
	for _, suffix := range []string{".localhost", ".local", ".lan", ".internal", ".home.arpa", ".onion"} {
		if strings.HasSuffix(host, suffix) {
			return "", false
		}
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
	}
	return host, true
}

var premiumSpecial = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func premiumResolve(ctx context.Context, host string, lookup func(context.Context, string, string) ([]netip.Addr, error), local []netip.Prefix) ([]netip.Addr, string) {
	addresses, err := lookup(ctx, "ip", host+".")
	if err != nil || len(addresses) == 0 {
		return nil, "dns"
	}
	var chosen []netip.Addr
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsGlobalUnicast() || address.IsPrivate() || address.Zone() != "" {
			return nil, "non-public"
		}
		for _, ranges := range [][]netip.Prefix{premiumSpecial, local} {
			for _, prefix := range ranges {
				if prefix.Contains(address) {
					return nil, "non-public"
				}
			}
		}
		if address.Is4() && len(chosen) < 4 && !slices.Contains(chosen, address) {
			chosen = append(chosen, address)
		}
	}
	if len(chosen) == 0 {
		return nil, "ipv6-only"
	}
	return chosen, ""
}

func premiumAddresses(ctx context.Context, host string) ([]netip.Addr, string) {
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return nil, "network"
	}
	var local []netip.Prefix
	for _, addr := range interfaces {
		if prefix, err := netip.ParsePrefix(addr.String()); err == nil {
			local = append(local, prefix.Masked())
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return premiumResolve(ctx, host, net.DefaultResolver.LookupNetIP, local)
}

func premiumTransport(host string, ips []netip.Addr, dial func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	return &http.Transport{
		DisableKeepAlives: true, MaxResponseHeaderBytes: 24 << 10,
		TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second,
		TLSClientConfig: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			if addr != net.JoinHostPort(host, "443") {
				return nil, errors.New("unexpected HTTPS destination")
			}
			var err error
			for _, ip := range ips {
				var conn net.Conn
				conn, err = dial(ctx, "tcp4", netip.AddrPortFrom(ip, 443).String())
				if err == nil {
					return conn, nil
				}
			}
			return nil, err
		},
	}
}

func premiumHTTPS(ctx context.Context, host, destination string, transport *http.Transport) premiumObservation {
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 6 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+destination+"/", nil)
	if err != nil {
		return premiumObservation{State: "unknown", Code: "network"}
	}
	req.Host = host
	resp, err := client.Do(req)
	if err != nil {
		var network net.Error
		var certificate *tls.CertificateVerificationError
		var authority x509.UnknownAuthorityError
		switch {
		case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES):
			return premiumObservation{State: "unknown", Code: "network"}
		case errors.As(err, &certificate), errors.As(err, &authority):
			return premiumObservation{State: "unavailable", Code: "certificate"}
		case errors.As(err, &network) && network.Timeout():
			return premiumObservation{State: "unavailable", Code: "timeout"}
		default:
			return premiumObservation{State: "unavailable", Code: "connection"}
		}
	}
	resp.Body.Close()
	state := "available"
	if resp.StatusCode == 403 || resp.StatusCode == 451 || resp.StatusCode >= 500 || resp.StatusCode < 200 {
		state = "unavailable"
	}
	return premiumObservation{State: state, Code: "http", HTTP: resp.StatusCode}
}

func hPremiumDomain(e *engine, c *cgi.Req) (any, error) {
	if c.R.Method == http.MethodGet {
		_, err := os.Lstat(e.premiumChangePath())
		return map[string]bool{"recoveryRequired": !errors.Is(err, os.ErrNotExist)}, nil
	}
	if err := requestGuard(c); err != nil {
		return nil, err
	}
	raw, err := strictjson.Decode(c.Body())
	fields, ok := raw.(map[string]any)
	if err != nil || !ok {
		return nil, cgi.E(400, "PREMIUM_DOMAIN_INVALID", "Введите публичное имя домена.")
	}
	switch fields["action"] {
	case "check":
		input, _ := fields["domain"].(string)
		domain, ok := premiumDomain(input)
		if !ok || len(fields) != 2 {
			return nil, cgi.E(400, "PREMIUM_DOMAIN_INVALID", "Введите имя публичного домена без адреса страницы.")
		}
		return e.checkPremiumDomain(c.R.Context(), c.User, domain)
	case "add":
		token, _ := fields["token"].(string)
		if len(fields) == 2 {
			return e.addPremiumDomain(c.User, token)
		}
	case "recover":
		if len(fields) == 1 {
			return e.recoverPremium()
		}
	}
	return nil, cgi.E(400, "PREMIUM_DOMAIN_INVALID", "Действие не поддерживается.")
}

func (e *engine) checkPremiumDomain(ctx context.Context, user, domain string) (premiumCheck, error) {
	result := premiumCheck{Domain: domain,
		Direct: premiumObservation{State: "unknown", Code: "network"},
		VPN:    premiumObservation{State: "unknown", Code: "vpn-unconfirmed"}}
	release, ok := e.d.Lock.TryAcquire("routes", "routes:premium-domain-check", premiumID)
	if !ok {
		return result, cgi.E(409, "POLICY_BUSY", "Дождитесь завершения текущей операции.")
	}
	defer release()
	e.clearPremiumProofs(user)
	ctx, cancel := context.WithTimeout(compat.WithFence(ctx), 18*time.Second)
	defer cancel()
	ips, code := premiumAddresses(ctx, domain)
	if code != "" {
		result.Direct.Code, result.VPN.Code = code, code
		return result, nil
	}
	resolve := e.directInterface
	if resolve == nil {
		resolve = modxray.DirectInterface
	}
	if name, err := resolve(); err == nil {
		dialer := net.Dialer{Timeout: 2 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
			var bind error
			if err := raw.Control(func(fd uintptr) { bind = unix.BindToDevice(int(fd), name) }); err != nil {
				return err
			}
			return bind
		}}
		result.Direct = premiumHTTPS(ctx, domain, domain, premiumTransport(domain, ips, dialer.DialContext))
	}
	svc := e.d.Svc
	if svc.ServersPremiumVPNIdentity == nil || svc.ServersPremiumVPNProbe == nil {
		return result, nil
	}
	identity := svc.ServersPremiumVPNIdentity()
	if identity == "" {
		return result, nil
	}
	last := result.VPN
	verified, probeErr := svc.ServersPremiumVPNProbe(ctx, identity, ips[:1], func(call context.Context, endpoint string) bool {
		proxyAddress, err := netip.ParseAddrPort(endpoint)
		if err != nil || !proxyAddress.Addr().IsLoopback() {
			return false
		}
		dialer := net.Dialer{Timeout: 2 * time.Second}
		transport := premiumTransport(domain, nil, nil)
		transport.Proxy = http.ProxyURL(&url.URL{Scheme: "socks5", Host: endpoint})
		transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
			if address != endpoint {
				return nil, errors.New("unexpected local VPN probe endpoint")
			}
			return dialer.DialContext(ctx, "tcp4", address)
		}
		last = premiumHTTPS(call, domain, ips[0].String(), transport)
		return last.State == "available"
	})
	if probeErr != nil || !verified {
		return result, nil
	}
	result.VPN = last
	if result.Direct.State != "unavailable" || result.VPN.State != "available" {
		return result, nil
	}
	return e.issuePremiumProof(result, user, identity, false)
}

func (e *engine) clearPremiumProofs(user string) {
	now := time.Now()
	for key, proof := range e.premiumProofs {
		if proof.user == user || !proof.expires.After(now) {
			delete(e.premiumProofs, key)
		}
	}
}

func (e *engine) issuePremiumProof(result premiumCheck, user, identity string, isIP bool) (premiumCheck, error) {
	if len(e.premiumProofs) >= 32 {
		return result, nil
	}
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return result, err
	}
	if e.premiumProofs == nil {
		e.premiumProofs = make(map[string]premiumProof)
	}
	result.Token = hex.EncodeToString(secret[:])
	e.premiumProofs[result.Token] = premiumProof{domain: result.Domain, user: user, identity: identity, expires: time.Now().Add(2 * time.Minute), isIP: isIP}
	return result, nil
}

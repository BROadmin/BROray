package routes

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
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

const premiumIPID = "own-premium-ip"
const premiumIPName = "Premium IP"

var premiumIPv6Special = []netip.Prefix{
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}
var premiumIPv6Global = netip.MustParsePrefix("2000::/3")

func premiumIP(input string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(input))
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || (addr.Is6() && !premiumIPv6Global.Contains(addr)) {
		return netip.Addr{}, false
	}
	for _, ranges := range [][]netip.Prefix{premiumSpecial, premiumIPv6Special} {
		for _, prefix := range ranges {
			if prefix.Contains(addr) {
				return netip.Addr{}, false
			}
		}
	}
	return addr, true
}

func premiumIPNetwork(addr netip.Addr) string {
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return "network"
	}
	for _, value := range interfaces {
		prefix, err := netip.ParsePrefix(value.String())
		if err == nil && prefix.Masked().Contains(addr) {
			return "non-public"
		}
	}
	return ""
}

func hPremiumIP(e *engine, c *cgi.Req) (any, error) {
	list := premiumIPList
	if c.R.Method == http.MethodGet {
		_, err := os.Lstat(e.p(list.pending))
		return map[string]bool{"recoveryRequired": !errors.Is(err, os.ErrNotExist)}, nil
	}
	if err := requestGuard(c); err != nil {
		return nil, err
	}
	value, err := strictjson.Decode(c.Body())
	fields, ok := value.(map[string]any)
	if err == nil && ok {
		switch fields["action"] {
		case "check":
			input, _ := fields["address"].(string)
			if addr, valid := premiumIP(input); valid && len(fields) == 2 {
				return e.checkPremiumIP(c.R.Context(), c.User, addr)
			}
		case "add":
			token, _ := fields["token"].(string)
			if len(fields) == 2 {
				return e.addPremiumTarget(c.User, token, list)
			}
		case "recover":
			if len(fields) == 1 {
				return e.recoverPremiumTarget(list)
			}
		}
	}
	return nil, cgi.E(400, "PREMIUM_IP_INVALID", "Укажите публичный IPv4 или IPv6 без порта и маски.")
}

func (e *engine) checkPremiumIP(ctx context.Context, user string, addr netip.Addr) (premiumCheck, error) {
	result := premiumCheck{Domain: addr.String(),
		Direct: premiumObservation{State: "unknown", Code: "network"},
		VPN:    premiumObservation{State: "unknown", Code: "vpn-unconfirmed"}}
	release, ok := e.d.Lock.TryAcquire("routes", "routes:premium-ip-check", premiumIPID)
	if !ok {
		return result, cgi.E(409, "POLICY_BUSY", "Дождитесь завершения текущей операции.")
	}
	defer release()
	e.clearPremiumProofs(user)
	if code := premiumIPNetwork(addr); code != "" {
		result.Direct.Code, result.VPN.Code = code, code
		return result, nil
	}
	ctx, cancel := context.WithTimeout(compat.WithFence(ctx), 18*time.Second)
	defer cancel()
	resolve, network := e.directInterface, "tcp4"
	if resolve == nil {
		resolve = modxray.DirectInterface
	}
	if addr.Is6() {
		resolve, network = modxray.DirectIPv6Interface, "tcp6"
		result.Direct.Code = "ipv6-egress"
	}
	if name, err := resolve(); err == nil {
		dialer := net.Dialer{Timeout: 4 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
			var bind error
			if err := raw.Control(func(fd uintptr) { bind = unix.BindToDevice(int(fd), name) }); err != nil {
				return err
			}
			return bind
		}}
		conn, err := dialer.DialContext(ctx, network, netip.AddrPortFrom(addr, 443).String())
		if err == nil {
			conn.Close()
		}
		result.Direct = premiumTCPResult(err)
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
	probed := false
	verified, err := svc.ServersPremiumVPNProbe(ctx, identity, []netip.Addr{addr}, func(call context.Context, endpoint string) bool {
		last, probed = premiumTCPSOCKS(call, endpoint, addr), true
		return last.State == "available"
	})
	if err != nil || !probed {
		return result, nil
	}
	if !verified && last.State == "available" {
		return result, nil
	}
	result.VPN = last
	if !verified || result.Direct.State != "unavailable" {
		return result, nil
	}
	rc := e.nativeDomainRCI()
	if rc == nil {
		result.AddBlocked = "route-unconfirmed"
		return result, nil
	}
	snapshot, err := rc.ReadDomainRoutes(ctx)
	if err != nil {
		result.AddBlocked = "route-unconfirmed"
		return result, nil
	}
	if premiumIPConflict(snapshot, e.managedInterface(), addr) {
		result.AddBlocked = "route-conflict"
		return result, nil
	}
	return e.issuePremiumProof(result, user, identity, true)
}

func premiumIPConflict(snapshot keenetic.DomainRouteSnapshot, iface string, addr netip.Addr) bool {
	for _, binding := range snapshot.Bindings {
		if binding.Disabled || binding.Interface == iface {
			continue
		}
		for _, entry := range snapshot.Lists[binding.Group].Include {
			if ip, err := netip.ParseAddr(entry.Address); err == nil && ip.Unmap() == addr {
				return true
			}
			if prefix, err := netip.ParsePrefix(entry.Address); err == nil && prefix.Contains(addr) {
				return true
			}
		}
	}
	return false
}

func premiumTCPResult(err error) premiumObservation {
	var timeout net.Error
	switch {
	case err == nil:
		return premiumObservation{State: "available", Code: "tcp"}
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES), errors.Is(err, unix.ENETDOWN),
		errors.Is(err, unix.ENETUNREACH), errors.Is(err, unix.EADDRNOTAVAIL), errors.Is(err, context.Canceled):
		return premiumObservation{State: "unknown", Code: "network"}
	case errors.Is(err, unix.ECONNREFUSED):
		return premiumObservation{State: "unavailable", Code: "tcp-refused"}
	case errors.As(err, &timeout) && timeout.Timeout():
		return premiumObservation{State: "unavailable", Code: "tcp-timeout"}
	default:
		return premiumObservation{State: "unavailable", Code: "tcp-connection"}
	}
}

func premiumTCPSOCKS(ctx context.Context, endpoint string, addr netip.Addr) premiumObservation {
	unknown := premiumObservation{State: "unknown", Code: "vpn-unconfirmed"}
	proxy, err := netip.ParseAddrPort(endpoint)
	if err != nil || !proxy.Addr().Is4() || !proxy.Addr().IsLoopback() {
		return unknown
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp4", endpoint)
	if err != nil {
		return unknown
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return unknown
	}
	var reply [4]byte
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		return unknown
	}
	if _, err := io.ReadFull(conn, reply[:2]); err != nil || reply[0] != 5 || reply[1] != 0 {
		return unknown
	}
	kind := byte(1)
	if addr.Is6() {
		kind = 4
	}
	req := append([]byte{5, 1, 0, kind}, addr.AsSlice()...)
	req = append(req, 1, 187) // TCP/443.
	if _, err := conn.Write(req); err != nil {
		return unknown
	}
	if _, err := io.ReadFull(conn, reply[:]); err != nil || reply[0] != 5 || reply[2] != 0 {
		return unknown
	}
	switch reply[1] {
	case 0:
		var boundBytes int
		switch reply[3] {
		case 1:
			boundBytes = 4
		case 4:
			boundBytes = 16
		case 3:
			if _, err := io.ReadFull(conn, reply[:1]); err != nil {
				return unknown
			}
			boundBytes = int(reply[0])
		default:
			return unknown
		}
		if _, err := io.CopyN(io.Discard, conn, int64(boundBytes+2)); err != nil {
			return unknown
		}
		// Xray may acknowledge CONNECT before the remote connection opens.
		// Require the destination's TLS response. A certificate name is not
		// part of the check for a literal address.
		client := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true})
		if err := client.HandshakeContext(ctx); err != nil {
			return premiumObservation{State: "unknown", Code: "tcp-unconfirmed"}
		}
		return premiumObservation{State: "available", Code: "tcp-verified"}
	case 5:
		return premiumTCPResult(unix.ECONNREFUSED)
	default:
		return premiumObservation{State: "unavailable", Code: "tcp-connection"}
	}
}

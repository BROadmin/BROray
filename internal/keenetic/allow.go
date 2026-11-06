package keenetic

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Area names a write domain with its own allowlist of exact command shapes,
// like BROray 3.x (routes-router-config.sh, routes-dot.sh, web-publish.sh,
// interface-core.sh). A command outside the shape is refused before dispatch.
type Area string

const (
	AreaInterface Area = "interface"
	AreaRoutes    Area = "routes"
	AreaDoT       Area = "dot"
	AreaWeb       Area = "web"
	AreaSystem    Area = "system"
)

var (
	reProxyName = regexp.MustCompile(`^Proxy([0-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-5])$`)
	reAppName   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	reSNI       = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?)*$`)
	reSPKI      = regexp.MustCompile(`^[A-Za-z0-9+/=]{20,128}$`)
	reDesc      = regexp.MustCompile(`^[^"\x00-\x1f]{1,128}$`)
)

func ValidProxyName(s string) bool { return reProxyName.MatchString(s) }

func ValidIPv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

func validMask(s string) bool {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return false
	}
	b := a.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	// contiguous ones followed by zeros
	inv := ^v
	return inv&(inv+1) == 0
}

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == s
}

// Allow validates one command against the area's shapes.
func Allow(area Area, cmd string) error {
	f := strings.Fields(cmd)
	bad := func() error { return fmt.Errorf("KEENETIC_COMMAND_REFUSED: %q is outside the %s allowlist", cmd, area) }
	switch area {
	case AreaSystem:
		if cmd == "system configuration save" {
			return nil
		}
		return bad()
	case AreaRoutes:
		// ip route <net> <mask> ProxyN <metric>   |   no ip route <net> <mask> ProxyN
		switch {
		case len(f) == 6 && f[0] == "ip" && f[1] == "route" && ValidIPv4(f[2]) && validMask(f[3]) && ValidProxyName(f[4]) && validMetric(f[5]):
			return nil
		case len(f) == 6 && f[0] == "no" && f[1] == "ip" && f[2] == "route" && ValidIPv4(f[3]) && validMask(f[4]) && ValidProxyName(f[5]):
			return nil
		}
		return bad()
	case AreaInterface:
		if len(f) >= 2 && f[0] == "no" && f[1] == "interface" && len(f) == 3 && ValidProxyName(f[2]) {
			return nil
		}
		if len(f) < 2 || f[0] != "interface" || !ValidProxyName(f[1]) {
			return bad()
		}
		// SOCKS5 UDP (KeeneticOS 4.1+): byte-exact, single spaces, no suffix.
		if strings.Contains(cmd, "socks5-udp") || strings.Contains(cmd, "udpgw") {
			if cmd == "interface "+f[1]+" proxy socks5-udp" || cmd == "interface "+f[1]+" no proxy socks5-udp" {
				return nil
			}
			return bad()
		}
		rest := strings.TrimSpace(strings.TrimPrefix(cmd, "interface "+f[1]))
		switch {
		case rest == "", rest == "up", rest == "security-level public", rest == "proxy protocol socks5", rest == "proxy connect",
			rest == "no description", rest == "description null": // legacy schema-1 migration rollback (3.x interface-core.sh)
			return nil
		case strings.HasPrefix(rest, "proxy upstream "):
			p := strings.Fields(rest)
			if len(p) == 4 && ValidIPv4(p[2]) && validPort(p[3]) {
				return nil
			}
		case strings.HasPrefix(rest, "description "):
			d := strings.TrimPrefix(rest, "description ")
			if d == "BROray" {
				return nil
			}
			if strings.HasPrefix(d, "\"") && strings.HasSuffix(d, "\"") && reDesc.MatchString(d[1:len(d)-1]) {
				return nil
			}
		}
		return bad()
	case AreaDoT:
		// dns-proxy tls upstream <ip> <port> sni <sni> [spki <b64>]   |   no dns-proxy tls upstream <ip> <port>
		if len(f) == 6 && f[0] == "no" && f[1] == "dns-proxy" && f[2] == "tls" && f[3] == "upstream" && ValidIPv4(f[4]) && validPort(f[5]) {
			return nil
		}
		if (len(f) == 7 || len(f) == 9) && f[0] == "dns-proxy" && f[1] == "tls" && f[2] == "upstream" && ValidIPv4(f[3]) && validPort(f[4]) && f[5] == "sni" && reSNI.MatchString(f[6]) {
			if len(f) == 7 || (f[7] == "spki" && reSPKI.MatchString(f[8])) {
				return nil
			}
		}
		return bad()
	case AreaWeb:
		// ip http proxy <name> [upstream http <ip> <port> | domain ndns | ssl redirect | security-level public] | no ip http proxy <name>
		if len(f) == 5 && f[0] == "no" && f[1] == "ip" && f[2] == "http" && f[3] == "proxy" && reAppName.MatchString(f[4]) {
			return nil
		}
		if len(f) < 4 || f[0] != "ip" || f[1] != "http" || f[2] != "proxy" || !reAppName.MatchString(f[3]) {
			return bad()
		}
		rest := strings.Join(f[4:], " ")
		switch rest {
		case "", "domain ndns", "ssl redirect", "security-level public":
			return nil
		}
		if len(f) == 7 && f[4] == "upstream" && f[5] == "http" && ValidIPv4(f[6]) {
			return bad() // port required
		}
		if len(f) == 8 && f[4] == "upstream" && f[5] == "http" && ValidIPv4(f[6]) && validPort(f[7]) {
			return nil
		}
		return bad()
	}
	return bad()
}

func validMetric(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == s
}

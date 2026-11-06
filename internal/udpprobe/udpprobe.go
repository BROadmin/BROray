// Package udpprobe measures which public address a UDP datagram leaves the
// router from, with a STUN Binding Request (RFC 5389). Three paths make the
// BROray UDP path provable without application traffic:
//
//	direct  — a STUN server that is NOT routed to ProxyN: the WAN address;
//	socks   — the same request through Xray's SOCKS5 UDP ASSOCIATE: the
//	          VPN server's exit address (Xray + server UDP capability);
//	routed  — a STUN server whose /32 IS routed to ProxyN by a BROray route
//	          set: Keenetic → ProxyN (SOCKS5 UDP) → Xray → server.
//
// routed == socks != direct proves UDP took the BROray path; routed ==
// direct proves a bypass; a routed timeout means ProxyN does not carry UDP.
package udpprobe

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	magicCookie   = 0x2112A442
	bindingReq    = 0x0001
	bindingResp   = 0x0101
	attrMapped    = 0x0001
	attrXorMapped = 0x0020
)

// Request builds a STUN Binding Request and returns it with its transaction id.
func Request() ([]byte, [12]byte, error) {
	var tx [12]byte
	if _, err := rand.Read(tx[:]); err != nil {
		return nil, tx, err
	}
	b := make([]byte, 20)
	binary.BigEndian.PutUint16(b[0:], bindingReq)
	binary.BigEndian.PutUint16(b[2:], 0)
	binary.BigEndian.PutUint32(b[4:], magicCookie)
	copy(b[8:], tx[:])
	return b, tx, nil
}

// Parse extracts the mapped address from a Binding Success Response.
func Parse(b []byte, tx [12]byte) (*net.UDPAddr, error) {
	if len(b) < 20 || binary.BigEndian.Uint16(b[0:]) != bindingResp || binary.BigEndian.Uint32(b[4:]) != magicCookie {
		return nil, errors.New("not a STUN binding success response")
	}
	if [12]byte(b[8:20]) != tx {
		return nil, errors.New("STUN transaction id mismatch")
	}
	n := int(binary.BigEndian.Uint16(b[2:]))
	if 20+n > len(b) {
		return nil, errors.New("truncated STUN message")
	}
	var mapped *net.UDPAddr
	for p := 20; p+4 <= 20+n; {
		t, l := binary.BigEndian.Uint16(b[p:]), int(binary.BigEndian.Uint16(b[p+2:]))
		v := b[p+4:]
		if p+4+l > len(b) {
			return nil, errors.New("truncated STUN attribute")
		}
		v = v[:l]
		if (t == attrXorMapped || t == attrMapped) && l >= 8 && v[1] == 0x01 {
			port := binary.BigEndian.Uint16(v[2:])
			ip := net.IP(append([]byte{}, v[4:8]...))
			if t == attrXorMapped {
				port ^= uint16(magicCookie >> 16)
				c := make([]byte, 4)
				binary.BigEndian.PutUint32(c, magicCookie)
				for i := range ip {
					ip[i] ^= c[i]
				}
				return &net.UDPAddr{IP: ip, Port: int(port)}, nil
			}
			mapped = &net.UDPAddr{IP: ip, Port: int(port)}
		}
		p += 4 + (l+3)&^3
	}
	if mapped != nil {
		return mapped, nil
	}
	return nil, errors.New("no mapped address in STUN response")
}

// Direct sends a Binding Request straight to server (host:port).
func Direct(server string, timeout time.Duration) (*net.UDPAddr, error) {
	c, err := net.DialTimeout("udp", server, timeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return exchange(c, nil, timeout)
}

// DirectFrom is Direct from a fixed local address (e.g. "127.0.0.3:0"):
// host tests emulate distinct exit addresses with it.
func DirectFrom(local, server string, timeout time.Duration) (*net.UDPAddr, error) {
	la, err := net.ResolveUDPAddr("udp", local)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: timeout, LocalAddr: la}
	c, err := d.Dial("udp", server)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return exchange(c, nil, timeout)
}

func exchange(c net.Conn, wrap func([]byte) []byte, timeout time.Duration) (*net.UDPAddr, error) {
	req, tx, err := Request()
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	_ = c.SetDeadline(deadline)
	buf := make([]byte, 2048)
	for attempt := 0; attempt < 3 && time.Now().Before(deadline); attempt++ {
		out := req
		if wrap != nil {
			out = wrap(req)
		}
		if _, err := c.Write(out); err != nil {
			return nil, err
		}
		_ = c.SetReadDeadline(time.Now().Add(timeout / 3))
		n, err := c.Read(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return nil, err
		}
		b := buf[:n]
		if wrap != nil {
			if b, err = unwrapSOCKS(b); err != nil {
				return nil, err
			}
		}
		return Parse(b, tx)
	}
	return nil, errors.New("UDP_TIMEOUT: no STUN response")
}

// ViaSOCKS5 sends the request through a SOCKS5 server with UDP ASSOCIATE
// (RFC 1928, no authentication), e.g. Xray's inbound on 127.0.0.1:2080.
func ViaSOCKS5(socks, server string, timeout time.Duration) (*net.UDPAddr, error) {
	host, portS, err := net.SplitHostPort(server)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portS)
	if err != nil {
		return nil, err
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: %v", host, err)
	}
	var target net.IP
	for _, ip := range ips {
		if ip.To4() != nil {
			target = ip.To4()
			break
		}
	}
	if target == nil {
		return nil, errors.New("no IPv4 address for " + host)
	}
	ctl, err := net.DialTimeout("tcp", socks, timeout)
	if err != nil {
		return nil, err
	}
	defer ctl.Close()
	_ = ctl.SetDeadline(time.Now().Add(timeout))
	if _, err := ctl.Write([]byte{5, 1, 0}); err != nil {
		return nil, err
	}
	r := make([]byte, 2)
	if _, err := io.ReadFull(ctl, r); err != nil || r[0] != 5 || r[1] != 0 {
		return nil, errors.New("SOCKS5 method negotiation failed")
	}
	if _, err := ctl.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(ctl, head); err != nil || head[0] != 5 {
		return nil, errors.New("SOCKS5 UDP ASSOCIATE: bad reply")
	}
	if head[1] != 0 {
		return nil, fmt.Errorf("SOCKS5_UDP_REFUSED: UDP ASSOCIATE rejected (reply %d)", head[1])
	}
	var bnd net.IP
	switch head[3] {
	case 1:
		a := make([]byte, 4)
		if _, err := io.ReadFull(ctl, a); err != nil {
			return nil, err
		}
		bnd = net.IP(a)
	case 4:
		a := make([]byte, 16)
		if _, err := io.ReadFull(ctl, a); err != nil {
			return nil, err
		}
		bnd = net.IP(a)
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(ctl, l); err != nil {
			return nil, err
		}
		name := make([]byte, l[0])
		if _, err := io.ReadFull(ctl, name); err != nil {
			return nil, err
		}
		if ip := net.ParseIP(string(name)); ip != nil {
			bnd = ip
		}
	default:
		return nil, errors.New("SOCKS5 UDP ASSOCIATE: bad address type")
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(ctl, pb); err != nil {
		return nil, err
	}
	relayPort := int(binary.BigEndian.Uint16(pb))
	socksHost, _, _ := net.SplitHostPort(socks)
	if bnd == nil || bnd.IsUnspecified() {
		bnd = net.ParseIP(socksHost)
	}
	u, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: bnd, Port: relayPort})
	if err != nil {
		return nil, err
	}
	defer u.Close()
	hdr := []byte{0, 0, 0, 1}
	hdr = append(hdr, target...)
	hdr = binary.BigEndian.AppendUint16(hdr, uint16(port))
	return exchange(u, func(p []byte) []byte { return append(append([]byte{}, hdr...), p...) }, timeout)
}

func unwrapSOCKS(b []byte) ([]byte, error) {
	if len(b) < 4 || b[2] != 0 {
		return nil, errors.New("bad SOCKS5 UDP datagram")
	}
	switch b[3] {
	case 1:
		if len(b) < 10 {
			return nil, errors.New("short SOCKS5 UDP datagram")
		}
		return b[10:], nil
	case 4:
		if len(b) < 22 {
			return nil, errors.New("short SOCKS5 UDP datagram")
		}
		return b[22:], nil
	case 3:
		if len(b) < 5 || len(b) < 7+int(b[4]) {
			return nil, errors.New("short SOCKS5 UDP datagram")
		}
		return b[7+int(b[4]):], nil
	}
	return nil, errors.New("bad SOCKS5 UDP address type")
}

// Responder is a minimal STUN server for tests: it answers every Binding
// Request with the sender's address (XOR-MAPPED-ADDRESS).
func Responder(pc net.PacketConn) {
	buf := make([]byte, 2048)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		b := buf[:n]
		if n < 20 || binary.BigEndian.Uint16(b[0:]) != bindingReq {
			continue
		}
		ua, ok := addr.(*net.UDPAddr)
		if !ok || ua.IP.To4() == nil {
			continue
		}
		resp := make([]byte, 20, 32)
		binary.BigEndian.PutUint16(resp[0:], bindingResp)
		binary.BigEndian.PutUint16(resp[2:], 12)
		binary.BigEndian.PutUint32(resp[4:], magicCookie)
		copy(resp[8:20], b[8:20])
		attr := make([]byte, 12)
		binary.BigEndian.PutUint16(attr[0:], attrXorMapped)
		binary.BigEndian.PutUint16(attr[2:], 8)
		attr[5] = 0x01
		binary.BigEndian.PutUint16(attr[6:], uint16(ua.Port)^uint16(magicCookie>>16))
		ip := ua.IP.To4()
		c := make([]byte, 4)
		binary.BigEndian.PutUint32(c, magicCookie)
		for i := 0; i < 4; i++ {
			attr[8+i] = ip[i] ^ c[i]
		}
		resp = append(resp, attr...)
		_, _ = pc.WriteTo(resp, addr)
	}
}

// Result is one probe line of `broray diag udp`.
type Result struct {
	Path   string `json:"path"`
	Target string `json:"target"`
	Via    string `json:"via,omitempty"`
	Mapped string `json:"mapped,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Report is the JSON printed by `broray diag udp` / tools/udpprobe.
type Report struct {
	Results []Result `json:"results"`
	Verdict string   `json:"verdict"`
	Detail  string   `json:"detail"`
}

func ipOf(a *net.UDPAddr) string {
	if a == nil {
		return ""
	}
	return a.IP.String()
}

// Run probes: routed — a STUN server whose /32 is in a BROray route set (sent
// directly: the routing table decides); direct — one that is not; socks —
// routed again through a SOCKS5 server (Xray inbound). Empty arguments skip
// that path. Nothing on the router is changed.
func Run(routed, direct, socks string, timeout time.Duration) Report {
	var rep Report
	add := func(path, target, via string, a *net.UDPAddr, err error) {
		r := Result{Path: path, Target: target, Via: via}
		if a != nil {
			r.Mapped = a.String()
		}
		if err != nil {
			r.Error = err.Error()
		}
		rep.Results = append(rep.Results, r)
	}
	var ra, da, sa *net.UDPAddr
	var rerr, derr, serr error
	if direct != "" {
		da, derr = Direct(direct, timeout)
		add("direct", direct, "", da, derr)
	}
	if routed != "" {
		ra, rerr = Direct(routed, timeout)
		add("routed", routed, "", ra, rerr)
	}
	if socks != "" && routed != "" {
		sa, serr = ViaSOCKS5(socks, routed, timeout)
		add("socks", routed, socks, sa, serr)
	}
	switch {
	case socks != "" && serr != nil:
		rep.Verdict, rep.Detail = "SOCKS_UDP_FAILED", "Xray SOCKS5 не передал UDP (UDP ASSOCIATE или сервер без UDP)"
	case routed == "":
		rep.Verdict, rep.Detail = "INCOMPLETE", "не задан STUN-адрес из набора маршрутов BROray"
	case rerr != nil:
		rep.Verdict, rep.Detail = "ROUTED_UDP_TIMEOUT", "UDP к адресу из набора маршрутов не вернулся: ProxyN не передаёт UDP"
	case da != nil && ipOf(ra) == ipOf(da):
		rep.Verdict, rep.Detail = "BYPASS_WAN", "UDP к адресу из набора маршрутов ушёл с WAN-адреса, а не через BROray"
	case sa != nil && ipOf(ra) != ipOf(sa):
		rep.Verdict, rep.Detail = "UNEXPECTED_EXIT", "адрес выхода не совпадает ни с WAN, ни с выходом Xray"
	case da == nil && sa == nil:
		rep.Verdict, rep.Detail = "INCOMPLETE", "нет пути для сравнения (direct или socks)"
	default:
		rep.Verdict, rep.Detail = "UDP_VIA_BROray", "UDP к адресу из набора маршрутов вышел с адреса сервера BROray"
	}
	return rep
}

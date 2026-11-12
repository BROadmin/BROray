// Package wireguardimport normalizes the known Clash WireGuard client schema.
// Source options remain private; only a proven projection becomes Xray settings.
package wireguardimport

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxSourceBytes = 32768
const ExtensionComment = "# BROray WireGuard extensions: "

type Peer struct {
	Endpoint, PublicKey, PreSharedKey string
	AllowedIPs                        []string
	Reserved                          [3]byte
}

type Profile struct {
	Source                            map[string]any
	Name, PrivateKey                  string
	Addresses, DNS                    []string
	Peers                             []Peer
	MTU, Keepalive                    int
	HasKeepalive, HasReserved         bool
	RemoteDNS, UDP, TFO, MPTCP        bool
	IPVersion, Interface, DialerProxy string
	RoutingMark, Workers, Refresh     int
	StackMode, CongestionController   string
	AWG                               map[string]any
}

func bad(field string) error {
	return errors.New("WireGuard: неправильное поле " + field)
}

func plain(s string) bool {
	if !utf8.ValidString(s) || len(s) > 16384 {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

func stringValue(m map[string]any, key string) (string, error) {
	v, exists := m[key]
	if !exists {
		return "", nil
	}
	s, ok := v.(string)
	if !ok || !plain(s) {
		return "", bad(key)
	}
	return s, nil
}

func integer(m map[string]any, key string, min, max int) (int, error) {
	v, exists := m[key]
	if !exists {
		return 0, nil
	}
	var n int64
	switch x := v.(type) {
	case json.Number:
		value, err := x.Int64()
		if err != nil {
			return 0, bad(key)
		}
		n = value
	case float64:
		if x < float64(min) || x > float64(max) || x != float64(int64(x)) {
			return 0, bad(key)
		}
		n = int64(x)
	default:
		return 0, bad(key)
	}
	if n < int64(min) || n > int64(max) {
		return 0, bad(key)
	}
	return int(n), nil
}

func boolean(m map[string]any, key string) (bool, error) {
	v, exists := m[key]
	if !exists {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, bad(key)
	}
	return b, nil
}

func list(m map[string]any, key string, limit int) ([]string, error) {
	out := []string{}
	v, exists := m[key]
	if !exists {
		return out, nil
	}
	values, ok := v.([]any)
	if !ok || len(values) > limit {
		return nil, bad(key)
	}
	for _, v := range values {
		s, ok := v.(string)
		if !ok || s == "" || !plain(s) || strings.Contains(s, ",") {
			return nil, bad(key)
		}
		out = append(out, s)
	}
	return out, nil
}

func key(s string) (string, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		b, err := encoding.Strict().DecodeString(s)
		if err == nil && len(b) == 32 && !bytes.Equal(b, make([]byte, 32)) {
			return base64.StdEncoding.EncodeToString(b), nil
		}
	}
	return "", bad("key")
}

func reserved(v any) ([3]byte, bool, error) {
	var out [3]byte
	if s, ok := v.(string); ok {
		b, err := base64.StdEncoding.Strict().DecodeString(s)
		if err != nil || len(b) != 0 && len(b) != 3 {
			return out, false, bad("reserved")
		}
		copy(out[:], b)
		return out, len(b) != 0, nil
	}
	values, ok := v.([]any)
	if !ok || len(values) != 0 && len(values) != 3 {
		return out, false, bad("reserved")
	}
	for i, v := range values {
		n, err := integer(map[string]any{"reserved": v}, "reserved", 0, 255)
		if err != nil {
			return out, false, err
		}
		out[i] = byte(n)
	}
	return out, len(values) != 0, nil
}

func bounded(v any, depth int, budget *int) bool {
	*budget--
	if depth > 32 || *budget < 0 {
		return false
	}
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if len(k) > 128 || !plain(k) || !bounded(child, depth+1, budget) {
				return false
			}
		}
	case []any:
		for _, child := range x {
			if !bounded(child, depth+1, budget) {
				return false
			}
		}
	case string:
		return plain(x)
	case json.Number, float64, int, bool, nil:
	default:
		return false
	}
	return true
}

func hostValid(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// Parse validates source bounds before normalization, so the same parser can
// safely verify a provider-authored extension comment on later record reads.
func Parse(source map[string]any) (*Profile, error) {
	budget := 4096
	if !bounded(source, 0, &budget) {
		return nil, bad("bounds")
	}
	b, err := json.Marshal(source)
	if err != nil || len(b) > MaxSourceBytes {
		return nil, bad("size")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	var m map[string]any
	if decoder.Decode(&m) != nil {
		return nil, bad("source")
	}
	allowed := map[string]bool{}
	for _, k := range strings.Fields("name type server port udp ip-version tfo mptcp interface-name routing-mark dialer-proxy ip ipv6 private-key public-key pre-shared-key mtu dns reserved allowed-ips persistent-keepalive remote-dns-resolve amnezia-wg-option peers workers ip-stack refresh-server-ip-interval") {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return nil, bad("source")
		}
	}
	if m["type"] != "wireguard" {
		return nil, bad("type")
	}
	p := &Profile{Source: m, MTU: 1408, DNS: []string{}, IPVersion: "dual", StackMode: "auto", CongestionController: "cubic"}
	if p.Name, err = stringValue(m, "name"); err != nil || p.Name == "" {
		return nil, bad("name")
	}
	secret, err := stringValue(m, "private-key")
	if err != nil {
		return nil, err
	}
	if p.PrivateKey, err = key(secret); err != nil {
		return nil, err
	}
	defaultRoutes := []string{}
	for _, field := range []string{"ip", "ipv6"} {
		s, err := stringValue(m, field)
		if err != nil {
			return nil, err
		}
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			if field == "ip" {
				s += "/32"
			} else {
				s += "/128"
			}
		}
		prefix, err := netip.ParsePrefix(s)
		if err != nil || prefix.Addr().Is4() != (field == "ip") || prefix.Addr().Zone() != "" {
			return nil, bad(field)
		}
		p.Addresses = append(p.Addresses, prefix.String())
		if field == "ip" {
			defaultRoutes = append(defaultRoutes, "0.0.0.0/0")
		} else {
			defaultRoutes = append(defaultRoutes, "::/0")
		}
	}
	if len(p.Addresses) == 0 {
		return nil, bad("address")
	}
	mtu, err := integer(m, "mtu", 0, 9000)
	if err != nil || mtu != 0 && mtu < 576 {
		return nil, bad("mtu")
	}
	if mtu != 0 {
		p.MTU = mtu
	}
	for _, a := range p.Addresses {
		prefix, _ := netip.ParsePrefix(a)
		if prefix.Addr().Is6() && p.MTU < 1280 {
			return nil, bad("mtu")
		}
	}
	for _, option := range []struct {
		key    string
		target *bool
	}{
		{"udp", &p.UDP}, {"remote-dns-resolve", &p.RemoteDNS}, {"tfo", &p.TFO}, {"mptcp", &p.MPTCP},
	} {
		if *option.target, err = boolean(m, option.key); err != nil {
			return nil, err
		}
	}
	if p.DNS, err = list(m, "dns", 16); err != nil {
		return nil, err
	}
	for _, option := range []struct {
		key    string
		target *int
		max    int
	}{
		{"persistent-keepalive", &p.Keepalive, 65535}, {"workers", &p.Workers, 2147483647},
		{"refresh-server-ip-interval", &p.Refresh, 2147483647}, {"routing-mark", &p.RoutingMark, 2147483647},
	} {
		if *option.target, err = integer(m, option.key, 0, option.max); err != nil {
			return nil, err
		}
	}
	_, p.HasKeepalive = m["persistent-keepalive"]
	for _, option := range []struct {
		key    string
		target *string
	}{
		{"interface-name", &p.Interface}, {"dialer-proxy", &p.DialerProxy},
	} {
		if *option.target, err = stringValue(m, option.key); err != nil {
			return nil, err
		}
	}
	if _, exists := m["ip-version"]; exists {
		if p.IPVersion, err = stringValue(m, "ip-version"); err != nil {
			return nil, err
		}
		switch p.IPVersion {
		case "dual", "ipv4", "ipv6", "ipv4-prefer", "ipv6-prefer":
		default:
			return nil, bad("ip-version")
		}
	}
	if value, exists := m["ip-stack"]; exists {
		stack, ok := value.(map[string]any)
		if !ok {
			return nil, bad("ip-stack")
		}
		for k := range stack {
			if k != "mode" && k != "congestion-controller" {
				return nil, bad("ip-stack")
			}
		}
		if s, err := stringValue(stack, "mode"); err != nil {
			return nil, err
		} else if s != "" {
			p.StackMode = strings.ToLower(s)
		}
		if s, err := stringValue(stack, "congestion-controller"); err != nil {
			return nil, err
		} else if s != "" {
			p.CongestionController = strings.ToLower(s)
		}
		switch p.StackMode {
		case "auto", "gvisor", "mips":
		default:
			return nil, bad("ip-stack.mode")
		}
		switch p.CongestionController {
		case "cubic", "reno", "bbr", "bbr3":
		default:
			return nil, bad("ip-stack.congestion-controller")
		}
	}
	if value, exists := m["amnezia-wg-option"]; exists {
		var ok bool
		p.AWG, ok = value.(map[string]any)
		if !ok || p.AWG == nil {
			return nil, bad("amnezia-wg-option")
		}
	}
	var global [3]byte
	if v, exists := m["reserved"]; exists {
		global, p.HasReserved, err = reserved(v)
		if err != nil {
			return nil, err
		}
	}
	peerMaps := []any{m}
	full := false
	if v, exists := m["peers"]; exists {
		values, ok := v.([]any)
		if !ok || len(values) > 32 {
			return nil, bad("peers")
		}
		if len(values) != 0 {
			peerMaps, full = values, true
		}
	}
	for _, value := range peerMaps {
		pm, ok := value.(map[string]any)
		if !ok {
			return nil, bad("peers")
		}
		if full {
			for k := range pm {
				switch k {
				case "server", "port", "public-key", "pre-shared-key", "reserved", "allowed-ips":
				default:
					return nil, bad("peers")
				}
			}
		}
		host, err := stringValue(pm, "server")
		if err != nil || !hostValid(host) {
			return nil, bad("server")
		}
		port, err := integer(pm, "port", 1, 65535)
		if err != nil || port == 0 {
			return nil, bad("port")
		}
		pub, err := stringValue(pm, "public-key")
		if err != nil {
			return nil, err
		}
		publicKey, err := key(pub)
		if err != nil {
			return nil, err
		}
		peer := Peer{Endpoint: net.JoinHostPort(host, strconv.Itoa(port)), PublicKey: publicKey, Reserved: global}
		if ps, err := stringValue(pm, "pre-shared-key"); err != nil {
			return nil, err
		} else if ps != "" {
			if peer.PreSharedKey, err = key(ps); err != nil {
				return nil, err
			}
		}
		if peer.AllowedIPs, err = list(pm, "allowed-ips", 128); err != nil {
			return nil, err
		}
		if len(peer.AllowedIPs) == 0 {
			if full {
				return nil, bad("allowed-ips")
			}
			peer.AllowedIPs = append([]string{}, defaultRoutes...)
		}
		for i, a := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(a)
			if err != nil {
				return nil, bad("allowed-ips")
			}
			peer.AllowedIPs[i] = prefix.String()
		}
		if v, exists := pm["reserved"]; exists {
			r, supplied, err := reserved(v)
			if err != nil {
				return nil, err
			}
			if supplied {
				peer.Reserved = r
				p.HasReserved = true
			}
		}
		p.Peers = append(p.Peers, peer)
	}
	return p, nil
}

func (p *Profile) PlainDNS() bool {
	for _, s := range p.DNS {
		if a, err := netip.ParseAddr(s); err != nil || a.Zone() != "" {
			return false
		}
	}
	return true
}

func (p *Profile) SameReserved() bool {
	for _, peer := range p.Peers[1:] {
		if peer.Reserved != p.Peers[0].Reserved {
			return false
		}
	}
	return true
}

// Settings contains normalized semantic extensions only. Names, raw numeric
// spellings and redundant shortcut fields never participate in identity.
func (p *Profile) Settings() map[string]any {
	reserved := make([]any, 0, len(p.Peers))
	for _, peer := range p.Peers {
		reserved = append(reserved, []any{int(peer.Reserved[0]), int(peer.Reserved[1]), int(peer.Reserved[2])})
	}
	dns := make([]any, 0, len(p.DNS))
	for _, server := range p.DNS {
		dns = append(dns, server)
	}
	return map[string]any{
		"peerReserved": reserved, "dns": map[string]any{"enabled": p.RemoteDNS, "servers": dns},
		"udp": p.UDP, "workers": p.Workers, "refreshServerIPInterval": p.Refresh,
		"ipStack": map[string]any{"mode": p.StackMode, "congestionController": p.CongestionController},
		"socket": map[string]any{"ipVersion": p.IPVersion, "interface": p.Interface, "routingMark": p.RoutingMark,
			"dialerProxy": p.DialerProxy, "tfo": p.TFO, "mptcp": p.MPTCP},
	}
}

func (p *Profile) Extension() map[string]any {
	return map[string]any{"source": p.Source, "settings": p.Settings()}
}

// ParseExtension checks both sides of the private carrier before comparing
// them. In particular, a forged settings object has its own depth/node/byte
// bounds rather than relying on the source object's limits.
func ParseExtension(extension map[string]any) (*Profile, error) {
	budget := 8192
	if !bounded(extension, 0, &budget) || len(extension) != 2 {
		return nil, bad("extensions")
	}
	encoded, err := json.Marshal(extension)
	if err != nil || len(encoded) > 2*MaxSourceBytes {
		return nil, bad("extensions")
	}
	source, ok := extension["source"].(map[string]any)
	if !ok {
		return nil, bad("extensions")
	}
	settings, ok := extension["settings"].(map[string]any)
	if !ok {
		return nil, bad("extensions")
	}
	profile, err := Parse(source)
	if err != nil || profile.AWG != nil {
		return nil, bad("extensions")
	}
	actual, err := json.Marshal(settings)
	if err != nil {
		return nil, bad("extensions")
	}
	expected, err := json.Marshal(profile.Settings())
	if err != nil || !bytes.Equal(actual, expected) {
		return nil, bad("extensions")
	}
	return profile, nil
}

func (p *Profile) Unsupported() []string {
	var fields []string
	if !p.SameReserved() {
		fields = append(fields, "peers.reserved")
	}
	if p.RemoteDNS && !p.PlainDNS() {
		fields = append(fields, "dns")
	}
	if p.Workers != 0 {
		fields = append(fields, "workers")
	}
	if p.Refresh != 0 {
		fields = append(fields, "refresh-server-ip-interval")
	}
	if p.StackMode == "mips" || p.CongestionController != "cubic" {
		fields = append(fields, "ip-stack")
	}
	if p.IPVersion != "dual" {
		fields = append(fields, "ip-version")
	}
	if p.Interface != "" {
		fields = append(fields, "interface-name")
	}
	if p.RoutingMark != 0 {
		fields = append(fields, "routing-mark")
	}
	if p.DialerProxy != "" {
		fields = append(fields, "dialer-proxy")
	}
	sort.Strings(fields)
	return fields
}

// INI is the supported projection. Unsupported extensions must be checked
// before execution; they remain beside this projection in the private record.
func (p *Profile) INI() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nMTU = %d\n", p.PrivateKey, strings.Join(p.Addresses, ", "), p.MTU)
	if p.RemoteDNS && p.PlainDNS() && len(p.DNS) != 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(p.DNS, ", "))
	}
	if p.AWG == nil && p.HasReserved && p.SameReserved() {
		r := p.Peers[0].Reserved
		fmt.Fprintf(&b, "Reserved = %d, %d, %d\n", r[0], r[1], r[2])
	}
	for _, peer := range p.Peers {
		fmt.Fprintf(&b, "[Peer]\nPublicKey = %s\nEndpoint = %s\nAllowedIPs = %s\n", peer.PublicKey, peer.Endpoint, strings.Join(peer.AllowedIPs, ", "))
		if peer.PreSharedKey != "" {
			fmt.Fprintf(&b, "PresharedKey = %s\n", peer.PreSharedKey)
		}
		if p.HasKeepalive {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", p.Keepalive)
		}
	}
	return b.String()
}

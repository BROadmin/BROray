package keenetic

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"sort"
	"strings"
)

// IPv6 detection (read-only). BROray routes only IPv4 networks to ProxyN:
// when the ISP gives the router global IPv6 and the home segment gets it
// too, a service's AAAA traffic leaves past BROray. BROray never disables
// IPv6 and never changes Keenetic for this; it only warns.
//
// The exact KeeneticOS output of "show ipv6 addresses", "show ipv6 prefixes"
// and the IPv6 part of "show interface" was not verified on a router with
// IPv6, so the parsers are generic: every address/prefix value anywhere in
// the answer is classified, and its interface is taken from the same record
// ("interface", "id", "interface-name") or the enclosing interface. Anything
// unreadable is an error, which callers report as "unknown" — never as a
// warning.

const (
	IPv6Global  = "global"
	IPv6None    = "none"
	IPv6Unknown = "unknown"
)

// IPv6WarningText is shown on the Home and Routes pages for state "global".
const IPv6WarningText = "На роутере включён IPv6: сервисы с IPv6-адресами могут работать мимо BROray (наборы маршрутов только IPv4)."

// IPv6Facts is what BROray knows about the router's IPv6. The zero value is
// "unknown".
type IPv6Facts struct {
	State     string   // global | none | unknown
	WAN       []string // non-LAN interfaces with a global address or prefix (sorted)
	LANPrefix bool     // a global address or prefix on a home segment (Bridge*)
}

// IPv6ReadQueries are the read-only commands detection runs (CLI form; RCI
// paths replace spaces with "/").
var IPv6ReadQueries = []string{"show ipv6 addresses", "show ipv6 prefixes", "show interface"}

func (f IPv6Facts) state() string {
	switch f.State {
	case IPv6Global, IPv6None:
		return f.State
	}
	return IPv6Unknown
}

// Merge combines the answers of several queries: any global evidence wins,
// one successful query without it gives "none", no successful query keeps
// "unknown".
func (f IPv6Facts) Merge(o IPv6Facts) IPv6Facts {
	a, b := f.state(), o.state()
	out := IPv6Facts{State: IPv6Unknown, LANPrefix: f.LANPrefix || o.LANPrefix, WAN: uniqueNames(append(append([]string{}, f.WAN...), o.WAN...))}
	switch {
	case a == IPv6Global || b == IPv6Global:
		out.State = IPv6Global
	case a == IPv6None || b == IPv6None:
		out.State = IPv6None
	}
	return out
}

// Object is the additive API form: {state, wan, lanPrefix, warning, checkedAt}.
func (f IPv6Facts) Object(checkedAt string) map[string]any {
	st := f.state()
	wan := f.WAN
	if wan == nil || st != IPv6Global {
		wan = []string{}
	}
	o := map[string]any{"state": st, "wan": wan, "lanPrefix": st == IPv6Global && f.LANPrefix, "warning": nil, "checkedAt": nil}
	if st == IPv6Global {
		o["warning"] = IPv6WarningText
	}
	if checkedAt != "" && st != IPv6Unknown {
		o["checkedAt"] = checkedAt
	}
	return o
}

func uniqueNames(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range in {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// isGlobalIPv6 reports a global unicast IPv6 address or prefix (2000::/3):
// not link-local fe80::/10, not ULA fc00::/7, not multicast, not
// IPv4-mapped. A "/len" suffix or "%zone" is ignored.
func isGlobalIPv6(v string) bool {
	v = strings.TrimSpace(v)
	if f := strings.Fields(v); len(f) > 0 {
		v = f[0]
	}
	if i := strings.IndexAny(v, "/%"); i >= 0 {
		v = v[:i]
	}
	if !strings.Contains(v, ":") {
		return false
	}
	ip := net.ParseIP(v)
	if ip == nil || ip.To4() != nil {
		return false
	}
	return ip[0]&0xe0 == 0x20
}

// addressKey: keys that hold the router's own addresses or prefixes. Peer,
// gateway, DNS and endpoint addresses say nothing about the router itself.
func addressKey(k string) bool {
	k = strings.ToLower(k)
	if !strings.Contains(k, "address") && !strings.Contains(k, "prefix") {
		return false
	}
	for _, x := range []string{"remote", "peer", "gateway", "dns", "server", "mac", "endpoint", "destination", "source", "via", "length"} {
		if strings.Contains(k, x) {
			return false
		}
	}
	return true
}

// lanSegment: KeeneticOS home/guest segments are Bridge interfaces.
func lanSegment(name string) bool { return strings.HasPrefix(name, "Bridge") || name == "Home" }

// Tunnel and proxy interfaces can carry IPv6 but are not direct WAN uplinks.
func tunnelInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"wireguard", "openvpn", "ipsec", "l2tp", "pptp", "sstp", "proxy", "openconnect"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

type ipv6Collector struct {
	found     bool
	wan       []string
	lanPrefix bool
}

func (c *ipv6Collector) add(iface, value string) {
	if !isGlobalIPv6(value) {
		return
	}
	c.found = true
	if lanSegment(iface) {
		c.lanPrefix = true
		return
	}
	if iface != "" && !tunnelInterface(iface) {
		c.wan = append(c.wan, iface)
	}
}

func (c *ipv6Collector) facts() IPv6Facts {
	f := IPv6Facts{State: IPv6None, WAN: uniqueNames(c.wan), LANPrefix: c.lanPrefix}
	if c.found {
		f.State = IPv6Global
	}
	return f
}

var errIPv6Malformed = errors.New("KEENETIC_IPV6_UNREADABLE")

// ParseIPv6JSON reads an RCI answer of any of IPv6ReadQueries.
func ParseIPv6JSON(raw []byte) (IPv6Facts, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return IPv6Facts{}, errIPv6Malformed
	}
	if dec.More() {
		return IPv6Facts{}, errIPv6Malformed
	}
	top, ok := v.(map[string]any)
	if !ok {
		return IPv6Facts{}, errIPv6Malformed
	}
	if rciError(top) {
		return IPv6Facts{}, errIPv6Malformed
	}
	c := &ipv6Collector{}
	walkIPv6(c, top, "", "")
	return c.facts(), nil
}

// rciError: RCI answers a failed show with {"status":[{"status":"error",…}]}.
func rciError(top map[string]any) bool {
	st, ok := top["status"].([]any)
	if !ok {
		return false
	}
	for _, x := range st {
		if m, ok := x.(map[string]any); ok && m["status"] == "error" {
			return true
		}
	}
	return false
}

func objectIface(m map[string]any, inherited string) string {
	for _, k := range []string{"interface", "id", "interface-name"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return inherited
}

func walkIPv6(c *ipv6Collector, v any, key, iface string) {
	switch x := v.(type) {
	case map[string]any:
		own := objectIface(x, iface)
		for k, child := range x {
			walkIPv6(c, child, k, own)
		}
	case []any:
		for _, child := range x {
			walkIPv6(c, child, key, iface)
		}
	case string:
		if addressKey(key) {
			c.add(iface, x)
		}
	}
}

var (
	reTextKV    = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.\-]*):(?:[ \t]+(.*))?$`)
	reTextIface = regexp.MustCompile(`^Interface, name = "([^"]+)"`)
	reTextError = regexp.MustCompile(`(?i)\berror\b|unknown command|no such command`)
)

// ParseIPv6Text reads the ndmc text (or the RCI JSON rendered in the ndmc
// layout) of any of IPv6ReadQueries. ndmc right-aligns keys, so nesting is
// not taken from indentation: a header line ("key:" without a value) or a
// repeated key starts a new record, and an "id" (or the ndmc "Interface,
// name = …" line) names the interface of the records that follow it.
func ParseIPv6Text(lines []string) (IPv6Facts, error) {
	c := &ipv6Collector{}
	ctx := ""
	type record struct {
		iface string
		keys  map[string]bool
		vals  []string
	}
	cur := record{keys: map[string]bool{}}
	flush := func() {
		name := cur.iface
		if name == "" {
			name = ctx
		}
		for _, v := range cur.vals {
			c.add(name, v)
		}
		cur = record{keys: map[string]bool{}}
	}
	recognized, nonBlank := false, false
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		nonBlank = true
		if m := reTextIface.FindStringSubmatch(line); m != nil {
			flush()
			ctx, recognized = m[1], true
			continue
		}
		m := reTextKV.FindStringSubmatch(line)
		if m == nil {
			if reTextError.MatchString(line) {
				return IPv6Facts{}, errIPv6Malformed
			}
			continue
		}
		recognized = true
		k, v := m[1], strings.TrimSpace(m[2])
		if k == "status" && v == "error" { // an RCI error answer rendered as text
			return IPv6Facts{}, errIPv6Malformed
		}
		if v == "" || cur.keys[k] {
			flush()
			if v == "" {
				continue
			}
		}
		cur.keys[k] = true
		switch k {
		case "id":
			ctx = v
			cur.iface = v
		case "interface", "interface-name":
			if cur.iface == "" || k == "interface" {
				cur.iface = v
			}
		}
		if addressKey(k) {
			cur.vals = append(cur.vals, v)
		}
	}
	flush()
	if nonBlank && !recognized {
		return IPv6Facts{}, errIPv6Malformed
	}
	return c.facts(), nil
}

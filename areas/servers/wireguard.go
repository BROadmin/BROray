package servers

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

func isWireGuardInput(text string) bool {
	for _, line := range strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		return line == "[Interface]"
	}
	return false
}

func wgKey(s string) (string, bool) {
	var b []byte
	if len(s) == 64 {
		b, _ = hex.DecodeString(s)
	}
	if len(b) != 32 {
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			decoded, e := enc.Strict().DecodeString(s)
			if e == nil && len(decoded) == 32 {
				b = decoded
				break
			}
		}
	}
	if len(b) != 32 || bytes.Equal(b, make([]byte, 32)) {
		return "", false
	}
	return base64.StdEncoding.EncodeToString(b), true
}

func wgList(values []string) []any {
	var out []any
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}

func parseWireGuard(text, id string, source map[string]any) (Record, *ParseError) {
	if len(text) > 65536 {
		return nil, die("WireGuard: профиль слишком большой")
	}
	var iface, current map[string][]string
	var peers []map[string][]string
	name := ""
	for _, line := range strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# BROray name: ") {
			name = strings.TrimPrefix(line, "# BROray name: ")
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if len(line) > 16384 || !isText(line) {
			return nil, die("WireGuard: неправильная строка INI")
		}
		switch line {
		case "[Interface]":
			if iface != nil {
				return nil, die("WireGuard: повторный Interface")
			}
			iface = map[string][]string{}
			current = iface
			continue
		case "[Peer]":
			if iface == nil || len(peers) >= 32 {
				return nil, die("WireGuard: неправильный раздел Peer")
			}
			current = map[string][]string{}
			peers = append(peers, current)
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		allowed := []string{"PrivateKey", "Address", "DNS", "MTU", "Reserved"}
		if len(peers) > 0 {
			allowed = []string{"PublicKey", "PresharedKey", "Endpoint", "AllowedIPs", "PersistentKeepalive"}
		}
		if !ok || current == nil || !in(k, allowed...) || v == "" {
			return nil, die("WireGuard: неподдерживаемое или пустое поле INI")
		}
		if len(current[k]) > 0 && !in(k, "Address", "DNS", "AllowedIPs") {
			return nil, die("WireGuard: повторное поле INI")
		}
		current[k] = append(current[k], v)
	}
	first := func(m map[string][]string, k string) string {
		if len(m[k]) == 0 {
			return ""
		}
		return m[k][0]
	}
	secret, ok := wgKey(first(iface, "PrivateKey"))
	if !ok || len(peers) == 0 {
		return nil, die("WireGuard: нет корректного ключа или пира")
	}
	wg := map[string]any{"secretKey": secret, "address": wgList(iface["Address"]), "noKernelTun": true, "mtu": 1420}
	if value := first(iface, "MTU"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil {
			return nil, die("WireGuard: неправильный MTU")
		}
		wg["mtu"] = n
	}
	if len(iface["DNS"]) > 0 {
		wg["remoteDNS"] = wgList(iface["DNS"])
	}
	if len(iface["Reserved"]) > 0 {
		var reserved []any
		for _, v := range wgList(iface["Reserved"]) {
			n, e := strconv.Atoi(v.(string))
			if e != nil {
				return nil, die("WireGuard: неправильный reserved")
			}
			reserved = append(reserved, n)
		}
		wg["reserved"] = reserved
	}
	var mapped []any
	for _, p := range peers {
		pub, ok := wgKey(first(p, "PublicKey"))
		if !ok {
			return nil, die("WireGuard: неправильный public key")
		}
		peer := map[string]any{"publicKey": pub, "endpoint": first(p, "Endpoint"), "allowedIPs": wgList(p["AllowedIPs"])}
		if ps := first(p, "PresharedKey"); ps != "" {
			key, ok := wgKey(ps)
			if !ok {
				return nil, die("WireGuard: неправильный preshared key")
			}
			peer["preSharedKey"] = key
		}
		if ka := first(p, "PersistentKeepalive"); ka != "" {
			n, e := strconv.Atoi(ka)
			if e != nil {
				return nil, die("WireGuard: неправильный keepalive")
			}
			peer["keepAlive"] = n
		}
		mapped = append(mapped, peer)
	}
	wg["peers"] = mapped
	host, port, e := net.SplitHostPort(first(peers[0], "Endpoint"))
	if e != nil {
		return nil, die("WireGuard: неправильный endpoint")
	}
	n, pe := portCheck(port, "WireGuard ")
	if pe != nil {
		return nil, pe
	}
	if name == "" {
		name = "WireGuard @ " + host
	}
	r := normalize(Record{"schemaVersion": 2, "id": id, "name": name, "source": cloneSource(source), "uri": text, "protocol": "wireguard", "network": "wireguard", "security": "wireguard", "address": host, "port": n, "wireguard": wg})
	if e := validateWireGuard(r); e != nil {
		return nil, e
	}
	return r, nil
}

func validateWireGuard(r Record) *ParseError {
	bad := func() *ParseError { return die("неправильные параметры WireGuard") }
	if !basicRecordValid(r) || r["network"] != "wireguard" || r["security"] != "wireguard" {
		return bad()
	}
	wg, ok := r["wireguard"].(map[string]any)
	if !ok || !keysSubset(wg, "secretKey", "address", "peers", "mtu", "reserved", "remoteDNS", "noKernelTun") || wg["noKernelTun"] != true {
		return bad()
	}
	if _, ok := wgKey(asString(wg["secretKey"])); !ok {
		return bad()
	}
	mtu, ok := isInt(wg["mtu"])
	if !ok || mtu < 576 || mtu > 9000 {
		return bad()
	}
	addresses, ok := wg["address"].([]any)
	if !ok || len(addresses) == 0 || len(addresses) > 32 {
		return bad()
	}
	for _, a := range addresses {
		p, e := netip.ParsePrefix(asString(a))
		if e != nil || p.Addr().Is6() && mtu < 1280 {
			return bad()
		}
	}
	if dns, exists := wg["remoteDNS"]; exists {
		arr, ok := dns.([]any)
		if !ok || len(arr) > 16 {
			return bad()
		}
		for _, x := range arr {
			if ip, e := netip.ParseAddr(asString(x)); e != nil || ip.Zone() != "" {
				return bad()
			}
		}
	}
	if reserved, exists := wg["reserved"]; exists {
		arr, ok := reserved.([]any)
		if !ok || len(arr) != 3 {
			return bad()
		}
		for _, x := range arr {
			n, ok := isInt(x)
			if !ok || n < 0 || n > 255 {
				return bad()
			}
		}
	}
	peers, ok := wg["peers"].([]any)
	if !ok || len(peers) == 0 || len(peers) > 32 {
		return bad()
	}
	for i, v := range peers {
		p, ok := v.(map[string]any)
		if !ok || !keysSubset(p, "publicKey", "preSharedKey", "endpoint", "allowedIPs", "keepAlive") {
			return bad()
		}
		if _, ok := wgKey(asString(p["publicKey"])); !ok {
			return bad()
		}
		if ps, exists := p["preSharedKey"]; exists {
			if _, ok := wgKey(asString(ps)); !ok {
				return bad()
			}
		}
		host, port, e := net.SplitHostPort(asString(p["endpoint"]))
		if e != nil || !serverAddressValid(host) {
			return bad()
		}
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return bad()
		}
		if i == 0 && (r["address"] != host || !jsonEq(r["port"], n)) {
			return bad()
		}
		arr, ok := p["allowedIPs"].([]any)
		if !ok || len(arr) == 0 || len(arr) > 128 {
			return bad()
		}
		for _, a := range arr {
			if _, e := netip.ParsePrefix(asString(a)); e != nil {
				return bad()
			}
		}
		if ka, exists := p["keepAlive"]; exists {
			n, ok := isInt(ka)
			if !ok || n < 0 || n > 65535 {
				return bad()
			}
		}
	}
	return nil
}

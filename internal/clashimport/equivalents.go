package clashimport

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/clashschema"
	"github.com/BROadmin/broray/internal/xrayimport"
)

// These conversions are pinned to the source adapters and target constructors
// named by clash_equivalent_357_test.go. Unsupported context never authorizes
// execution of the validation projection.
func mappedWSPlugin(p map[string]any) bool {
	if p["plugin"] != "v2ray-plugin" {
		return false
	}
	o := objectField(p, "plugin-opts")
	if o["mux"] != false || o["v2ray-http-upgrade"] == true || o["v2ray-http-upgrade-fast-open"] == true {
		return false
	}
	for key := range o {
		if !inList(key, "mode", "mux", "host", "path", "headers", "tls", "skip-cert-verify", "fingerprint", "name-cert-verify", "certificate", "private-key", "ech-opts", "v2ray-http-upgrade", "v2ray-http-upgrade-fast-open") {
			return false
		}
	}
	// These source transport aliases are not part of the actual SS plugin;
	// allowing both could silently replace an independently requested layer.
	for _, key := range []string{"network", "ws-opts", "tls", "servername", "sni", "ech-opts", "reality-opts"} {
		if has(p, key) {
			return false
		}
	}
	return true
}

func wsPluginProxy(p map[string]any) map[string]any {
	o := objectField(p, "plugin-opts")
	q := without(p, "plugin", "plugin-opts")
	q["network"] = "ws"
	host := stringField(o, "host", "bing.com")
	headers := without(objectField(o, "headers"))
	for k, v := range headers {
		if strings.EqualFold(k, "Host") {
			host = v.(string)
		}
	}
	// Mihomo uses the plugin host for both the WebSocket Host and TLS SNI.
	headers["Host"] = host
	q["ws-opts"] = map[string]any{"path": stringField(o, "path", ""), "headers": headers}
	for _, key := range []string{"tls", "skip-cert-verify", "fingerprint", "name-cert-verify", "certificate", "private-key", "ech-opts"} {
		if has(o, key) {
			q[key] = o[key]
		}
	}
	if o["tls"] == true {
		q["servername"], q["alpn"] = host, []any{"http/1.1"}
	}
	return q
}

func compatibleWSEarlyData(o map[string]any) bool {
	if o["v2ray-http-upgrade-fast-open"] == true {
		return false
	}
	if stringFieldNumber(o, "max-early-data", 0) <= 0 {
		return true
	}
	if o["v2ray-http-upgrade"] == true {
		return false
	}
	return strings.EqualFold(stringField(o, "early-data-header-name", ""), "Sec-WebSocket-Protocol")
}

func mappedWSOptions(o map[string]any) map[string]any {
	need(compatibleWSEarlyData(o))
	need(keysOnly(o, "path", "headers", "v2ray-http-upgrade", "max-early-data", "early-data-header-name", "v2ray-http-upgrade-fast-open"))
	out := without(o, "max-early-data", "early-data-header-name", "v2ray-http-upgrade-fast-open")
	if n := stringFieldNumber(o, "max-early-data", 0); n > 0 {
		u, err := url.Parse(stringField(o, "path", ""))
		need(err == nil)
		q := u.Query()
		// An existing ed query overrides explicit options in Mihomo too.
		if q.Get("ed") == "" {
			q.Set("ed", strconv.FormatInt(int64(n), 10))
		}
		u.RawQuery = q.Encode()
		out["path"] = u.String()
	}
	return out
}

func downloadProxy(p map[string]any) map[string]any {
	x := objectField(p, "xhttp-opts")
	d := obj(x["download-settings"])
	q := without(p)
	options := without(x, "download-settings")
	for _, key := range []string{"host", "path", "headers", "reuse-settings"} {
		if d[key] != nil {
			options[key] = d[key]
		}
	}
	q["xhttp-opts"] = options
	for _, key := range []string{"server", "port", "tls", "alpn", "ech-opts", "shadow-tls-opts", "restls-opts", "jls-opts", "reality-opts", "skip-cert-verify", "name-cert-verify", "fingerprint", "certificate", "private-key", "servername", "client-fingerprint"} {
		if d[key] != nil {
			q[key] = d[key]
		}
	}
	if d["servername"] != nil {
		delete(q, "sni")
	}
	if stringField(options, "host", "") == "" {
		host := stringField(q, "servername", stringField(q, "sni", ""))
		if host == "" {
			host = stringField(q, "server", "")
		}
		if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() {
			host = "[" + host + "]"
		}
		options["host"] = host
	}
	if q["tls"] == false {
		for _, key := range []string{"reality-opts", "shadow-tls-opts", "restls-opts", "jls-opts"} {
			need(len(objectField(q, key)) == 0)
		}
		// Source download TLS=false disables inherited TLS-only values. Keep
		// the independently inherited HTTP Host above, including its fallback.
		for _, key := range []string{"servername", "sni", "alpn", "ech-opts", "client-fingerprint", "fingerprint", "name-cert-verify", "certificate", "private-key", "skip-cert-verify"} {
			delete(q, key)
		}
	}
	return q
}

func xhttpDownloadStream(p map[string]any) map[string]any {
	q := downloadProxy(p)
	st := map[string]any{"address": q["server"], "port": q["port"], "network": "xhttp", "security": "none",
		"xhttpSettings": xhttpSettings(objectField(q, "xhttp-opts"))}
	configureTLS(q, q["type"].(string), st)
	return st
}

func hysteriaMask(p map[string]any) map[string]any {
	fm := map[string]any{}
	q := mappedFields(p, map[string]string{"up": "brutalUp", "down": "brutalDown", "bbr-profile": "bbrProfile",
		"initial-stream-receive-window": "initStreamReceiveWindow", "max-stream-receive-window": "maxStreamReceiveWindow",
		"initial-connection-receive-window": "initConnectionReceiveWindow", "max-connection-receive-window": "maxConnectionReceiveWindow"})
	for _, key := range []string{"brutalUp", "brutalDown"} {
		if raw, exists := q[key]; exists {
			q[key] = hysteriaBandwidth(raw.(string))
		}
	}
	if len(q) > 0 {
		fm["quicParams"] = q
	}
	var masks []any
	if obfs := stringField(p, "obfs", ""); obfs != "" {
		settings := map[string]any{"password": p["obfs-password"]}
		if obfs == "gecko" {
			lo, hi := stringFieldNumber(p, "obfs-min-packet-size", 0), stringFieldNumber(p, "obfs-max-packet-size", 0)
			if lo == 0 {
				lo = 512
			}
			if hi == 0 {
				hi = 1200
			}
			settings["packetSize"] = fmt.Sprintf("%d-%d", int64(lo), int64(hi))
		}
		masks = append(masks, map[string]any{"type": "salamander", "settings": settings})
	}
	if ports := stringField(p, "ports", ""); ports != "" {
		text := stringField(p, "hop-interval", "")
		lo, hi := int64(0), int64(0)
		if text != "" {
			parts := strings.Split(text, "-")
			need(len(parts) <= 2)
			var err error
			lo, err = strconv.ParseInt(parts[0], 10, 32)
			need(err == nil && lo >= 0)
			hi = lo
			if len(parts) == 2 {
				hi, err = strconv.ParseInt(parts[1], 10, 32)
				need(err == nil && hi >= 0)
			}
		}
		if lo == 0 {
			lo = 30
		} else if lo < 5 {
			lo = 5
		}
		if hi < lo {
			hi = lo
		}
		masks = append(masks, map[string]any{"type": "udphop", "settings": map[string]any{"mode": "intervalremote", "remotePorts": ports, "interval": fmt.Sprintf("%d-%d", lo, hi)}})
	}
	if len(masks) > 0 {
		fm["udp"] = masks
	}
	return fm
}

func convertMasque(node *clashschema.Node, checks xrayimport.Checks) (Input, error) {
	p := node.Fields
	capability := &CapabilityError{Kind: "conversion-unimplemented"}
	allowed := []string{"name", "type", "server", "port", "private-key", "public-key", "ip", "ipv6", "sni", "uri", "network", "udp", "tfo", "mptcp", "interface-name", "routing-mark", "skip-cert-verify"}
	for key := range p {
		if !inList(key, allowed...) {
			capability.Fields = append(capability.Fields, key)
		}
	}
	if network := stringField(p, "network", "h3"); network != "h3" && network != "" {
		capability.Fields = append(capability.Fields, "network")
	}
	if p["udp"] == false {
		capability.Kind = "local-policy"
		capability.Fields = append(capability.Fields, "udp")
	}
	if p["skip-cert-verify"] == true {
		capability.Kind = "core-removed"
		capability.Fields = append(capability.Fields, "skip-cert-verify")
	}
	if len(capability.Fields) > 0 {
		sort.Strings(capability.Fields)
		return Input{Unavailable: node, Capability: capability}, nil
	}
	addresses := []any{}
	for _, key := range []string{"ip", "ipv6"} {
		if text := stringField(p, key, ""); text != "" {
			if !strings.Contains(text, "/") {
				if strings.Contains(text, ":") {
					text += "/128"
				} else {
					text += "/32"
				}
			}
			addresses = append(addresses, text)
		}
	}
	sni := stringField(p, "sni", "")
	if sni == "" {
		sni = "consumer-masque.cloudflareclient.com"
	}
	uri := stringField(p, "uri", "")
	if uri == "" {
		uri = "https://cloudflareaccess.com"
	}
	u, err := url.Parse(uri)
	need(err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "")
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	st := map[string]any{"network": "masque", "security": "tls",
		"tlsSettings":    map[string]any{"serverName": sni, "alpn": []any{"h3"}},
		"masqueSettings": map[string]any{"warp": map[string]any{"privateKey": p["private-key"], "publicKey": p["public-key"], "address": addresses}}}
	obj(st["masqueSettings"])["host"], obj(st["masqueSettings"])["path"] = u.Host, path
	if sock := mappedFields(p, map[string]string{"tfo": "tcpFastOpen", "mptcp": "tcpMptcp", "interface-name": "interface", "routing-mark": "mark"}); len(sock) > 0 {
		st["sockopt"] = sock
	}
	return fromXray(node, map[string]any{"protocol": "masque", "tag": p["name"], "settings": map[string]any{"address": p["server"], "port": p["port"]}, "streamSettings": st}, checks)
}

var sourceHY2Bandwidth = regexp.MustCompile(`^(\d+)\s*([KMGT]?)([Bb])ps$`)

func hysteriaBandwidth(value string) string {
	// Mihomo common/utils/mbps.go treats bare integers as Mbps. Its unit is
	// case-sensitive: uppercase B is bytes, whereas Xray's Bandwidth parser
	// lowercases all units and interprets them as bits.
	if _, err := strconv.ParseUint(value, 10, 64); err == nil {
		return value + " Mbps"
	}
	m := sourceHY2Bandwidth.FindStringSubmatch(value)
	if m == nil {
		return "0"
	} // exact source fallback for unrecognized strings
	if m[3] == "b" {
		return value
	}
	n, err := strconv.ParseUint(m[1], 10, 64)
	need(err == nil && n <= ^uint64(0)/8)
	return fmt.Sprintf("%d %sbps", n*8, m[2])
}

func validateHysteriaSource(p map[string]any) {
	for _, key := range []string{"up", "down", "hop-interval"} {
		stringField(p, key, "")
	}
	enum(p, "obfs", "", "salamander", "gecko")
	if stringField(p, "obfs", "") != "" {
		requireText(p, "obfs-password")
	}
	enum(p, "bbr-profile", "", "standard", "conservative", "aggressive")
	if ports := stringField(p, "ports", ""); ports != "" {
		sourcePortRange(ports)
	}
	for _, key := range []string{"obfs-min-packet-size", "obfs-max-packet-size"} {
		integerBounds(p, key, 0, 2048)
	}
	if p["obfs"] == "gecko" {
		lo, hi := stringFieldNumber(p, "obfs-min-packet-size", 0), stringFieldNumber(p, "obfs-max-packet-size", 0)
		if lo == 0 {
			lo = 512
		}
		if hi == 0 {
			hi = 1200
		}
		need(lo <= hi)
	}
	// Parsing the bounded range occurs even before an unrelated capability
	// can make the node unavailable; malformed known values cannot hide.
	hysteriaMask(p)
}

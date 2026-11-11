package subscriptions

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const clashWarning = "Из Clash/Mihomo импортируются отдельные серверы. DNS, rules, proxy-groups, providers и локальная политика UDP не применяются; удалённые providers не загружаются."

var reClashRoot = regexp.MustCompile(`(?m)(^|[,{])[ \t]*(proxies|'proxies'|"proxies")[ \t]*:`)

// Decode the syntax tree before conversion: never expose decoder errors, scalar
// values or user-controlled mapping keys in diagnostics.
func clashDocument(body []byte) (map[string]any, bool, error) {
	candidate := reClashRoot.Match(body)
	if len(body) > 2*1024*1024 {
		return nil, candidate, fmt.Errorf("size")
	}
	// Bound potential AST allocation before the decoder sees untrusted input.
	// This deliberately also counts punctuation inside quoted/literal scalars.
	structural := 0
	for _, c := range body {
		if bytes.IndexByte([]byte(":,[]{}\n&*"), c) >= 0 {
			structural++
			if structural > 20000 {
				return nil, candidate, fmt.Errorf("complexity")
			}
		}
	}
	d := yaml.NewDecoder(bytes.NewReader(body))
	var n yaml.Node
	if d.Decode(&n) != nil {
		return nil, candidate, fmt.Errorf("syntax")
	}
	var extra yaml.Node
	if e := d.Decode(&extra); e != io.EOF {
		return nil, candidate, fmt.Errorf("documents")
	}
	// Recognition precedes bounded conversion: limits for Clash must not be
	// imposed on unrelated legacy documents.
	if len(n.Content) != 1 || n.Content[0].Kind != yaml.MappingNode {
		return nil, false, nil
	}
	root := n.Content[0]
	found := false
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Kind == yaml.ScalarNode && root.Content[i].Value == "proxies" {
			found = true
			break
		}
	}
	if !found {
		return nil, false, nil
	}
	budget := 20000
	active := map[*yaml.Node]bool{}
	var cv func(*yaml.Node, int) (any, error)
	cv = func(n *yaml.Node, depth int) (any, error) {
		budget--
		if budget < 0 || depth > 32 || active[n] {
			return nil, fmt.Errorf("bounds")
		}
		active[n] = true
		defer delete(active, n)
		if n.Kind == yaml.AliasNode {
			return cv(n.Alias, depth+1)
		}
		switch n.Tag {
		case "!!map", "!!seq", "!!str", "!!int", "!!float", "!!bool", "!!null", "":
		default:
			return nil, fmt.Errorf("tag")
		}
		switch n.Kind {
		case yaml.DocumentNode:
			if len(n.Content) != 1 {
				return nil, fmt.Errorf("document")
			}
			return cv(n.Content[0], depth+1)
		case yaml.MappingNode:
			m := map[string]any{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Tag != "!!str" || k.Value == "<<" {
					return nil, fmt.Errorf("key")
				}
				if _, ok := m[k.Value]; ok {
					return nil, fmt.Errorf("duplicate")
				}
				v, e := cv(n.Content[i+1], depth+1)
				if e != nil {
					return nil, e
				}
				m[k.Value] = v
			}
			return m, nil
		case yaml.SequenceNode:
			a := []any{}
			for _, c := range n.Content {
				v, e := cv(c, depth+1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			return a, nil
		case yaml.ScalarNode:
			var v any
			if n.Decode(&v) != nil {
				return nil, fmt.Errorf("scalar")
			} // JSON roundtrip normalizes numeric types for existing validators.
			b, e := json.Marshal(v)
			if e != nil {
				return nil, e
			}
			decoder := json.NewDecoder(bytes.NewReader(b))
			decoder.UseNumber()
			if decoder.Decode(&v) != nil {
				return nil, fmt.Errorf("scalar")
			}
			return v, nil
		}
		return nil, fmt.Errorf("node")
	}
	v, e := cv(&n, 0)
	if e != nil {
		return nil, true, e
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false, nil
	}
	_, ok = m["proxies"]
	return m, ok, nil
}

func clashExtract(body []byte) (*extracted, bool, *subErr) {
	m, recognized, e := clashDocument(body)
	if !recognized {
		return nil, false, nil
	}
	if e != nil {
		return nil, true, fail("PARSE_ERROR", "Некорректная или слишком сложная структура Clash YAML.")
	}
	ps, ok := m["proxies"].([]any)
	if !ok {
		return nil, true, fail("PARSE_ERROR", "Clash proxies должен быть списком.")
	}
	if len(ps) > maxNodes {
		return nil, true, fail("CONTENT_TOO_LARGE", "Подписка содержит слишком много узлов.")
	}
	ex := &extracted{Clash: true, NodeWarnings: map[int][]string{}}
	for i, p := range ps {
		line, w, field := clashNode(p)
		if field != "" {
			line = unsupportedMarker + "#Clash." + field
		}
		ex.Lines = append(ex.Lines, line)
		ex.NodeWarnings[i] = w
	}
	if len(ps) == 0 {
		return nil, true, fail("NO_VALID_NODES", "Подписка не содержит узлов.")
	}
	return ex, true, nil
}

func clashNode(v any) (line string, w []string, field string) {
	field = "parameters"
	defer func() {
		if r := recover(); r != nil {
			line = ""
			if f, ok := r.(jqFail); ok && f.field != "" {
				field += "." + f.field
			}
			if field == "" {
				field = "parameters"
			}
		}
	}()
	p := obj(v)
	typ := stringField(p, "type", "")
	field = "type"
	need(inList(typ, "vless", "vmess", "trojan", "ss", "http", "socks5", "hysteria2", "wireguard"))
	allowed := []string{"name", "type", "server", "port", "udp", "ip-version", "tfo", "mptcp", "interface-name", "routing-mark"}
	switch typ {
	case "vless":
		allowed = append(allowed, "uuid", "encryption", "flow")
	case "vmess":
		allowed = append(allowed, "uuid", "cipher", "alterId")
	case "trojan", "hysteria2":
		allowed = append(allowed, "password")
	case "ss":
		allowed = append(allowed, "cipher", "password")
	case "http", "socks5":
		allowed = append(allowed, "username", "password")
	case "wireguard":
		allowed = append(allowed, "ip", "ipv6", "private-key", "public-key", "pre-shared-key", "mtu", "dns", "reserved", "persistent-keepalive", "remote-dns-resolve", "amnezia-wg-option")
	}
	if typ != "wireguard" {
		allowed = append(allowed, "tls", "servername", "sni", "client-fingerprint", "alpn", "skip-cert-verify", "reality-opts", "network", "ws-opts", "grpc-opts", "xhttp-opts", "http-upgrade-opts")
	}
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !inList(k, allowed...) {
			field = safeJSONField(k)
			return
		}
		if inList(k, "ip-version", "tfo", "mptcp", "interface-name", "routing-mark") {
			field = k
			return
		}
	}
	field = "parameters"
	need(textValue(p["name"]) && textValue(p["server"]) && isInt(p["port"], 1, 65535))
	if u, ok := p["udp"]; ok {
		_, b := u.(bool)
		need(b)
		if u == false {
			w = append(w, "Clash udp=false — локальная политика не переносится.")
		}
	}
	if typ == "wireguard" {
		line = clashWG(p)
		field = ""
		return
	}
	s := map[string]any{"address": p["server"], "port": p["port"]}
	proto := typ
	switch typ {
	case "vless":
		s["id"] = p["uuid"]
		for _, k := range []string{"encryption", "flow"} {
			if x, ok := p[k]; ok {
				s[k] = x
			}
		}
	case "vmess":
		s["id"] = p["uuid"]
		s["security"] = stringField(p, "cipher", "auto")
		if x, ok := p["alterId"]; ok {
			s["alterId"] = x
		}
	case "ss":
		proto = "shadowsocks"
		s["method"] = p["cipher"]
		s["password"] = p["password"]
	case "trojan":
		s["password"] = p["password"]
	case "http", "socks5":
		if typ == "socks5" {
			proto = "socks"
		}
		s["user"] = stringField(p, "username", "")
		s["pass"] = stringField(p, "password", "")
	case "hysteria2":
		proto = "hysteria"
		s["version"] = float64(2)
	}
	st := map[string]any{"network": stringField(p, "network", "tcp"), "security": "none"}
	netw := st["network"].(string)
	field = "network"
	need(inList(netw, "tcp", "ws", "grpc", "xhttp", "httpupgrade"))
	field = "transport"
	for opt, n := range map[string]string{"ws-opts": "ws", "grpc-opts": "grpc", "xhttp-opts": "xhttp", "http-upgrade-opts": "httpupgrade"} {
		if _, ok := p[opt]; ok {
			need(netw == n)
		}
	}
	switch netw {
	case "ws":
		field = "ws-opts"
		o := objectField(p, "ws-opts")
		need(keysOnly(o, "path", "headers", "v2ray-http-upgrade"))
		if upgrade, ok := o["v2ray-http-upgrade"]; ok {
			_, good := upgrade.(bool)
			need(good)
			if upgrade == true {
				st["network"] = "httpupgrade"
				st["httpupgradeSettings"] = without(o, "v2ray-http-upgrade")
			} else {
				st["wsSettings"] = without(o, "v2ray-http-upgrade")
			}
		} else {
			st["wsSettings"] = o
		}
	case "grpc":
		field = "grpc-opts"
		o := objectField(p, "grpc-opts")
		need(keysOnly(o, "grpc-service-name"))
		st["grpcSettings"] = map[string]any{"serviceName": stringField(o, "grpc-service-name", "")}
	case "httpupgrade":
		field = "http-upgrade-opts"
		o := objectField(p, "http-upgrade-opts")
		need(keysOnly(o, "path", "host"))
		st["httpupgradeSettings"] = o
	case "xhttp":
		field = "xhttp-opts"
		o := objectField(p, "xhttp-opts")
		need(keysOnly(o, "host", "path", "mode", "reuse-settings", "x-padding-bytes", "x-padding-obfs-mode", "no-grpc-header"))
		x := map[string]any{}
		for _, k := range []string{"host", "path", "mode"} {
			if a, ok := o[k]; ok {
				x[k] = a
			}
		}
		extra := map[string]any{}
		for a, b := range map[string]string{"x-padding-bytes": "xPaddingBytes", "x-padding-obfs-mode": "xPaddingObfsMode", "no-grpc-header": "noGRPCHeader"} {
			if z, ok := o[a]; ok {
				extra[b] = z
			}
		}
		if r, ok := o["reuse-settings"]; ok {
			field = "xhttp-opts.reuse-settings"
			rm := obj(r)
			need(keysOnly(rm, "h-max-request-times", "h-max-reusable-secs", "max-concurrency", "max-connections", "c-max-reuse-times", "h-keep-alive-period"))
			xm := map[string]any{}
			for a, b := range map[string]string{"h-max-request-times": "hMaxRequestTimes", "h-max-reusable-secs": "hMaxReusableSecs", "max-concurrency": "maxConcurrency", "max-connections": "maxConnections", "c-max-reuse-times": "cMaxReuseTimes", "h-keep-alive-period": "hKeepAlivePeriod"} {
				if z, ok := rm[a]; ok {
					xm[b] = z
				}
			}
			extra["xmux"] = xm
		}
		x["extra"] = extra
		st["xhttpSettings"] = x
	}
	field = "tls"
	tls := false
	if a, ok := p["tls"]; ok {
		var good bool
		tls, good = a.(bool)
		need(good)
	}
	if typ == "trojan" || typ == "hysteria2" {
		tls = true
	}
	if a, ok := p["skip-cert-verify"]; ok {
		need(a == false)
	}
	sn := stringField(p, "servername", stringField(p, "sni", ""))
	if has(p, "servername") && has(p, "sni") {
		need(p["servername"] == p["sni"])
	}
	t := map[string]any{"serverName": sn}
	if x, ok := p["client-fingerprint"]; ok {
		t["fingerprint"] = x
	}
	if x, ok := p["alpn"]; ok {
		t["alpn"] = x
	}
	if r, ok := p["reality-opts"]; ok {
		field = "reality-opts"
		need(tls)
		need(!has(p, "alpn"))
		rm := obj(r)
		need(keysOnly(rm, "public-key", "short-id", "support-x25519mlkem768"))
		t = map[string]any{"serverName": sn, "publicKey": rm["public-key"], "shortId": stringField(rm, "short-id", ""), "fingerprint": stringField(p, "client-fingerprint", "chrome")}
		if z, ok := rm["support-x25519mlkem768"]; ok {
			_, good := z.(bool)
			need(good)
		}
		st["security"] = "reality"
		st["realitySettings"] = t
	} else if tls {
		st["security"] = "tls"
		st["tlsSettings"] = t
	} else {
		need(!has(p, "servername") && !has(p, "sni") && !has(p, "alpn") && !has(p, "client-fingerprint"))
	}
	if typ == "hysteria2" {
		need(netw == "tcp")
		st["network"] = "hysteria"
		st["hysteriaSettings"] = map[string]any{"version": float64(2), "auth": p["password"]}
	}
	out := map[string]any{"protocol": proto, "tag": p["name"], "settings": s, "streamSettings": st}
	lines := outboundURIs(map[string]any{}, out, 1, false)
	need(len(lines) == 1)
	line = lines[0]
	if rm, ok := p["reality-opts"].(map[string]any); ok {
		if z, ok := rm["support-x25519mlkem768"]; ok {
			parts := strings.SplitN(line, "#", 2)
			line = parts[0] + "&support-x25519mlkem768=" + fmt.Sprint(z)
			if len(parts) == 2 {
				line += "#" + parts[1]
			}
		}
	}
	field = ""
	return
}

func clashWG(p map[string]any) string {
	need(!has(p, "network") && !has(p, "tls") && !has(p, "reality-opts"))
	str := func(k string) string { return stringField(p, k, "") }
	safe := func(s string) string { need(!strings.ContainsAny(s, "\r\n\x00")); return s }
	prefix := func(s string) string {
		if !strings.Contains(s, "/") {
			a, e := netip.ParseAddr(s)
			need(e == nil)
			if a.Is6() {
				s += "/128"
			} else {
				s += "/32"
			}
		}
		_, e := netip.ParsePrefix(s)
		need(e == nil)
		return s
	}
	ini := "[Interface]\nPrivateKey = " + safe(str("private-key")) + "\nAddress = " + prefix(str("ip"))
	if x := str("ipv6"); x != "" {
		ini += ", " + prefix(x)
	}
	ini += "\n"
	if x, ok := p["mtu"]; ok {
		need(isInt(x, 576, 9000))
		ini += "MTU = " + numText(x) + "\n"
	}
	if x, ok := p["dns"]; ok {
		a, ok := x.([]any)
		need(ok)
		ss := []string{}
		for _, v := range a {
			need(textValue(v))
			ss = append(ss, safe(v.(string)))
		}
		if len(ss) > 0 {
			ini += "DNS = " + strings.Join(ss, ", ") + "\n"
		}
	}
	awg, am := p["amnezia-wg-option"]
	if am {
		a := obj(awg)
		names := map[string]string{"jc": "Jc", "jmin": "Jmin", "jmax": "Jmax", "s1": "S1", "s2": "S2", "s3": "S3", "s4": "S4", "h1": "H1", "h2": "H2", "h3": "H3", "h4": "H4", "i1": "I1", "i2": "I2", "i3": "I3", "i4": "I4", "i5": "I5"}
		keys := make([]string, 0, len(a))
		for k := range a {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := a[k]
			if n, ok := names[k]; ok {
				switch v.(type) {
				case string, float64, json.Number:
					ini += n + " = " + safe(fmt.Sprint(v)) + "\n"
				default:
					panic("type")
				}
			}
		}
	}
	ini += "[Peer]\nPublicKey = " + safe(str("public-key")) + "\nEndpoint = " + net.JoinHostPort(str("server"), numText(p["port"])) + "\nAllowedIPs = 0.0.0.0/0, ::/0\n"
	if x := str("pre-shared-key"); x != "" {
		ini += "PresharedKey = " + safe(x) + "\n"
	}
	if x, ok := p["persistent-keepalive"]; ok {
		need(isInt(x, 0, 65535))
		ini += "PersistentKeepalive = " + numText(x) + "\n"
	}
	if am {
		need(!has(p, "reserved"))
		b, _ := json.Marshal(map[string]any{"config": ini})
		root := map[string]any{"description": p["name"], "containers": []any{map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": string(b), "sourceFormat": "clash-yaml", "clashOptions": p}}}}
		b, _ = json.Marshal(root)
		return "vpn://" + base64.StdEncoding.EncodeToString(b)
	}
	need(!has(p, "remote-dns-resolve") || p["remote-dns-resolve"] == false)
	need(!has(p, "reserved"))
	return "# BROray name: " + safe(str("name")) + "\n" + ini
}

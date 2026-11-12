package clashimport

import (
	"encoding/base64"
	"sort"
	"strings"

	"github.com/BROadmin/broray/internal/xrayimport"
)

func directFields(typ string) []string {
	keys := []string{"name", "type", "server", "port", "udp", "ip-version", "tfo", "mptcp", "interface-name", "routing-mark"}
	switch typ {
	case "vless":
		keys = append(keys, "uuid", "encryption", "flow")
	case "vmess":
		keys = append(keys, "uuid", "cipher", "alterId")
	case "trojan":
		keys = append(keys, "password")
	case "hysteria2":
		keys = append(keys, "password", "up", "down", "bbr-profile", "ports", "hop-interval", "obfs", "obfs-password", "obfs-min-packet-size", "obfs-max-packet-size", "initial-stream-receive-window", "max-stream-receive-window", "initial-connection-receive-window", "max-connection-receive-window")
	case "ss":
		keys = append(keys, "cipher", "password")
	case "http":
		keys = append(keys, "username", "password", "headers")
	case "socks5":
		keys = append(keys, "username", "password")
	}
	return append(keys, "tls", "servername", "sni", "client-fingerprint", "fingerprint", "name-cert-verify", "certificate", "private-key", "alpn", "skip-cert-verify", "ech-opts", "reality-opts", "network", "ws-opts", "grpc-opts", "xhttp-opts", "http-upgrade-opts", "http-opts", "mkcp-opts")
}

// capabilityProjection is a validation projection, never an executable result.
// Every removed field has a named disposition; errors in the remaining direct
// mapping are propagated unchanged. The caller retains the complete source.
func capabilityProjection(typ string, source map[string]any, checks xrayimport.Checks) (map[string]any, *CapabilityError) {
	p := without(source)
	capability := &CapabilityError{Kind: "conversion-unimplemented"}
	add := func(path, kind string) {
		capability.Fields = append(capability.Fields, path)
		priority := map[string]int{"conversion-unimplemented": 0, "core-unsupported": 1, "core-removed": 2}
		if priority[kind] > priority[capability.Kind] {
			capability.Kind = kind
		}
	}
	validateCapabilityFields(typ, source, checks)
	if typ == "ss" {
		method := stringField(source, "cipher", "")
		if !inList(method, "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305") {
			kind := "core-unsupported"
			add("cipher", kind)
			// Source credentials were checked above. These values exist only
			// to validate independent fields; the projection is never emitted.
			p["cipher"], p["password"] = "aes-128-gcm", "validation-only"
		}
	}
	for key := range source {
		if inList(key, directFields(typ)...) {
			continue
		}
		kind := "conversion-unimplemented"
		switch key {
		case "shadow-tls-opts", "restls-opts", "jls-opts", "mekya-opts", "tlsmirror-opts", "smux", "ss-opts":
			kind = "core-unsupported"
		case "plugin", "plugin-opts":
			if inList(stringField(source, "plugin", ""), "obfs", "shadow-tls", "restls", "jls", "kcptun") {
				kind = "core-unsupported"
			}
		case "packet-addr":
			if source[key] == true {
				kind = "core-unsupported"
			}
		case "packet-encoding":
			if inList(stringField(source, key, ""), "packet", "packetaddr") {
				kind = "core-unsupported"
			}
		}
		add(key, kind)
		delete(p, key)
	}
	if pref := stringField(p, "ip-version", ""); inList(pref, "ipv4-prefer", "ipv6-prefer") {
		add("ip-version", "conversion-unimplemented")
		delete(p, "ip-version")
	}
	if p["skip-cert-verify"] == true {
		add("skip-cert-verify", "core-removed")
		delete(p, "skip-cert-verify")
	}
	if ech := objectField(p, "ech-opts"); ech["enable"] == true && stringField(ech, "config", "") == "" {
		add("ech-opts", "conversion-unimplemented")
		delete(p, "ech-opts")
	}
	if objectField(p, "ech-opts")["enable"] == true && has(p, "reality-opts") {
		add("ech-opts", "conversion-unimplemented")
		delete(p, "ech-opts")
	}
	cert, _ := p["certificate"].(string)
	key, _ := p["private-key"].(string)
	if cert != "" && key != "" && !strings.Contains(cert, "BEGIN") && !strings.Contains(key, "BEGIN") {
		add("certificate", "conversion-unimplemented")
		add("private-key", "conversion-unimplemented")
		delete(p, "certificate")
		delete(p, "private-key")
	}
	if network := stringField(p, "network", "tcp"); inList(network, "h2", "mekya", "tlsmirror") {
		need(inList(typ, "vless", "vmess"))
		add("network", "core-unsupported")
		p["network"] = "tcp"
	}
	for block, mapped := range map[string][]string{
		"grpc-opts": {"grpc-service-name", "grpc-user-agent"},
		"ws-opts":   {"path", "headers", "v2ray-http-upgrade"},
		"mkcp-opts": {"mtu", "tti", "uplink-capacity", "downlink-capacity", "seed", "header"},
	} {
		if !has(p, block) {
			continue
		}
		o := objectField(p, block)
		if block == "ws-opts" && compatibleWSEarlyData(o) {
			mapped = append(mapped, "max-early-data", "early-data-header-name", "v2ray-http-upgrade-fast-open")
		}
		clean := without(o)
		for k := range o {
			if inList(k, mapped...) {
				continue
			}
			kind := "conversion-unimplemented"
			if block == "mkcp-opts" {
				kind = "core-unsupported"
			}
			add(block+"."+k, kind)
			delete(clean, k)
		}
		p[block] = clean
	}
	if x := objectField(p, "xhttp-opts"); x["download-settings"] != nil {
		converted, err := convertNode(downloadProxy(p), checks, true)
		need(err == nil)
		if converted.Unavailable != nil {
			add("xhttp-opts.download-settings", converted.Capability.Kind)
			p["xhttp-opts"] = without(x, "download-settings")
		}
	}
	if len(capability.Fields) == 0 {
		return p, nil
	}
	sort.Strings(capability.Fields)
	return p, capability
}

func validateCapabilityFields(typ string, p map[string]any, checks xrayimport.Checks) {
	if typ == "ss" {
		validateShadowsocksSource(p)
	}
	if typ == "hysteria2" {
		validateHysteriaSource(p)
	}
	enum(p, "ip-version", "dual", "ipv4", "ipv6", "ipv4-prefer", "ipv6-prefer")
	validateSourceSecurity(p, checks)
	if has(p, "smux") {
		o := objectField(p, "smux")
		enum(o, "protocol", "", "smux", "yamux", "h2mux")
		for _, k := range []string{"max-connections", "min-streams", "max-streams"} {
			integerBounds(o, k, 0, 2147483647)
		}
		if has(o, "brutal-opts") {
			b := objectField(o, "brutal-opts")
			for _, k := range []string{"up", "down"} {
				sourceBandwidth(stringField(b, k, ""), b["enabled"] == true)
			}
		}
	}
	if has(p, "grpc-opts") {
		o := objectField(p, "grpc-opts")
		for _, k := range []string{"ping-interval", "max-connections", "min-streams", "max-streams"} {
			integerBounds(o, k, 0, 2147483647)
		}
		if stringFieldNumber(o, "max-streams", 0) > 0 {
			need(stringFieldNumber(o, "min-streams", 0) <= stringFieldNumber(o, "max-streams", 0))
		}
	}
	if has(p, "ws-opts") {
		integerBounds(objectField(p, "ws-opts"), "max-early-data", 0, 2147483647)
	}
	if has(p, "mkcp-opts") {
		o := objectField(p, "mkcp-opts")
		for _, k := range []string{"read-buffer", "write-buffer"} {
			integerBounds(o, k, 0, 4294967295)
		}
	}
	if has(p, "h2-opts") {
		need(stringField(p, "network", "") == "h2")
	}
	if has(p, "ech-opts") {
		o := objectField(p, "ech-opts")
		if s := stringField(o, "config", ""); o["enable"] == true && s != "" {
			_, err := base64.StdEncoding.DecodeString(s)
			need(err == nil)
		}
	}
	if has(p, "ss-opts") {
		o := objectField(p, "ss-opts")
		if o["enabled"] == true {
			requireText(o, "password")
			enum(o, "method", "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305")
			need(has(o, "method"))
		}
	}
	if typ == "ss" && has(p, "plugin") {
		plugin := stringField(p, "plugin", "")
		o := objectField(p, "plugin-opts")
		switch plugin {
		case "obfs":
			enum(o, "mode", "http", "tls")
			need(has(o, "mode"))
		case "v2ray-plugin":
			need(stringField(o, "mode", "") == "websocket")
		case "gost-plugin":
			enum(o, "mode", "", "websocket", "ws", "http", "http2", "quic")
		case "shadow-tls":
			integerBounds(o, "version", 0, 3)
		case "kcptun":
			for _, k := range []string{"mtu", "sndwnd", "rcvwnd", "datashard", "parityshard", "conn", "keepalive"} {
				integerBounds(o, k, 0, 2147483647)
			}
		}
		validateSourceSecurity(o, checks)
	}
	for _, block := range []string{"shadow-tls-opts", "restls-opts", "jls-opts"} {
		if has(p, block) {
			o := objectField(p, block)
			validateSourceSecurity(o, checks)
			if block == "shadow-tls-opts" {
				integerBounds(o, "version", 0, 3)
			}
		}
	}
	if x := objectField(p, "xhttp-opts"); x["download-settings"] != nil {
		need(stringField(p, "network", "tcp") == "xhttp" && stringField(x, "mode", "auto") != "stream-one")
		d := obj(x["download-settings"])
		integerBounds(d, "port", 1, 65535)
		if has(d, "server") && d["server"] != nil {
			requireText(d, "server")
		}
		validateSourceSecurity(d, checks)
		if d["reuse-settings"] != nil {
			// Reuse the actual mapped range validator rather than accepting a
			// structurally valid but contradictory XMUX pair in a disabled node.
			xx := xhttpSettings(map[string]any{"reuse-settings": d["reuse-settings"]})
			out := map[string]any{"protocol": "vless", "settings": map[string]any{"address": "validation.invalid", "port": 443, "id": "00000000-0000-4000-8000-000000000357", "encryption": "none"}, "streamSettings": map[string]any{"network": "xhttp", "security": "none", "xhttpSettings": xx}}
			_, err := xrayimport.ConvertOutbound(map[string]any{}, out, 1, false, checks)
			need(err == nil)
		}
	}
}

// Package clashimport converts individual Mihomo server definitions without
// applying foreign routing, DNS, proxy groups or process configuration.
package clashimport

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/BROadmin/broray/internal/clashschema"
	"github.com/BROadmin/broray/internal/xrayimport"
	"github.com/BROadmin/broray/internal/xrayschema"
)

// Input is private import state; the caller must apply ClientFields before
// using a Link. An Unavailable input must never produce a runnable outbound.
type Input struct {
	Link           string
	NativeOutbound map[string]any
	Name           string
	ClientFields   map[string]any
	Warnings       []string
	Unavailable    *clashschema.Node
	Capability     *CapabilityError
}

type CapabilityError struct {
	Kind   string   `json:"kind"`
	Fields []string `json:"fields"`
}

func (e *CapabilityError) Error() string { return "Clash client capability is unavailable" }

type FieldError struct{ Field string }

func (e *FieldError) Error() string {
	return "invalid or unmapped Clash client field: " + safeField(e.Field)
}

// ConvertNode checks the pinned source shape, then validates every mapped
// target field through the shared Xray importer. Structural validation by
// itself never establishes runtime capability.
func ConvertNode(source map[string]any, checks xrayimport.Checks) (input Input, err error) {
	return convertNode(source, checks, false)
}

func convertNode(source map[string]any, checks xrayimport.Checks, knownProjection bool) (input Input, err error) {
	field := "parameters"
	defer func() {
		if failure := recover(); failure != nil {
			if f, ok := failure.(fieldFailure); ok && f.field != "" {
				field += "." + f.field
			}
			input, err = Input{}, &FieldError{Field: safeField(field)}
		}
	}()
	node, err := clashschema.Parse(source)
	if err != nil {
		return Input{}, &FieldError{Field: "parameters"}
	}
	p := node.Fields
	typ := node.Type
	if !knownProjection && len(node.UnknownPaths) > 0 {
		known := node.KnownFields
		// Existing BROray aliases have an explicit validator below. Preserve
		// them in the validation projection rather than treating them as opaque.
		for _, alias := range directFields(typ) {
			if has(p, alias) && !has(known, alias) {
				known[alias] = p[alias]
			}
		}
		if !reflect.DeepEqual(known, p) {
			// Remove only declared-unknown fields for this validation pass.
			// Known unmapped fields remain and cannot be hidden by an extension.
			if _, err := convertNode(known, checks, true); err != nil {
				return Input{}, err
			}
			return Input{Unavailable: node, Capability: &CapabilityError{Kind: "unknown-extension", Fields: node.UnknownPaths}}, nil
		}
	}
	field = "type"
	if kind, remote := remoteBackends[typ]; remote {
		field = typ
		validateRemote(typ, p, checks)
		if typ == "masque" {
			return convertMasque(node, checks)
		}
		return Input{Unavailable: node, Capability: &CapabilityError{Kind: kind, Fields: []string{"type"}}}, nil
	}
	need(inList(typ, "vless", "vmess", "trojan", "ss", "http", "socks5", "hysteria2", "wireguard"))
	if p["udp"] == false {
		// The source explicitly disables UDP. Removing the flag is only a
		// validation pass; returning that projection would broaden its policy.
		if _, err := convertNode(without(p, "udp"), checks, true); err != nil {
			return Input{}, err
		}
		return Input{Unavailable: node, Capability: &CapabilityError{Kind: "local-policy", Fields: []string{"udp"}}}, nil
	}
	if typ == "wireguard" {
		field = "wireguard"
		if has(p, "smux") {
			validateCapabilityFields(typ, p, checks)
			wireGuard(without(p, "smux"))
			return Input{Unavailable: node, Capability: &CapabilityError{Kind: "core-unsupported", Fields: []string{"smux"}}}, nil
		}
		// The WG parser retains the exact source as well as its typed semantic
		// extension model, including deliberate source-only capabilities.
		input.Link = wireGuard(source)
		return input, nil
	}
	field = "parameters"
	if typ == "ss" && mappedWSPlugin(p) {
		validateCapabilityFields(typ, p, checks)
		prepared := wsPluginProxy(p)
		converted, err := convertNode(prepared, checks, true)
		if err != nil {
			return Input{}, err
		}
		if converted.Unavailable != nil {
			return Input{Unavailable: node, Capability: converted.Capability}, nil
		}
		return converted, nil
	}
	projection, capability := capabilityProjection(typ, p, checks)
	if capability != nil {
		// This is an explicit field disposition, not a catch-all fallback on
		// conversion failure. Credentials and all remaining known settings
		// still pass through the normal importer before source preservation.
		if _, err := convertNode(projection, checks, true); err != nil {
			return Input{}, err
		}
		return Input{Unavailable: node, Capability: capability}, nil
	}
	need(keysOnly(p, directFields(typ)...))
	need(textValue(p["name"]) && textValue(p["server"]) && p["server"] != "" && isInt(p["port"], 1, 65535))
	if has(p, "udp") {
		_, ok := p["udp"].(bool)
		need(ok)
	}
	s := map[string]any{"address": p["server"], "port": p["port"]}
	proto := typ
	switch typ {
	case "vless":
		s["id"] = p["uuid"]
		for _, k := range []string{"encryption", "flow"} {
			if has(p, k) {
				s[k] = p[k]
			}
		}
	case "vmess":
		s["id"], s["security"] = p["uuid"], stringField(p, "cipher", "auto")
		if has(p, "alterId") {
			s["alterId"] = p["alterId"]
		}
	case "ss":
		proto, s["method"], s["password"] = "shadowsocks", p["cipher"], p["password"]
		if s["method"] == "xchacha20-ietf-poly1305" {
			s["method"] = "xchacha20-poly1305"
		}
	case "trojan":
		s["password"] = p["password"]
	case "http", "socks5":
		if typ == "socks5" {
			proto = "socks"
		}
		s["user"], s["pass"] = stringField(p, "username", ""), stringField(p, "password", "")
		if has(p, "headers") {
			s["headers"] = p["headers"]
		}
	case "hysteria2":
		proto, s["version"] = "hysteria", 2
	}
	st := map[string]any{"network": stringField(p, "network", "tcp"), "security": "none"}
	netw := st["network"].(string)
	field = "network"
	need(inList(netw, "tcp", "ws", "grpc", "xhttp", "httpupgrade", "http", "mkcp", "kcp"))
	field = "transport"
	for opt, n := range map[string]string{"ws-opts": "ws", "grpc-opts": "grpc", "xhttp-opts": "xhttp", "http-upgrade-opts": "httpupgrade", "http-opts": "http", "mkcp-opts": "mkcp"} {
		if has(p, opt) {
			need(netw == n || n == "mkcp" && netw == "kcp")
		}
	}
	switch netw {
	case "ws":
		field = "ws-opts"
		o := mappedWSOptions(objectField(p, "ws-opts"))
		if has(o, "v2ray-http-upgrade") {
			_, ok := o["v2ray-http-upgrade"].(bool)
			need(ok)
		}
		if o["v2ray-http-upgrade"] == true {
			st["network"], st["httpupgradeSettings"] = "httpupgrade", without(o, "v2ray-http-upgrade")
		} else {
			st["wsSettings"] = without(o, "v2ray-http-upgrade")
		}
	case "grpc":
		field = "grpc-opts"
		o := objectField(p, "grpc-opts")
		need(keysOnly(o, "grpc-service-name", "grpc-user-agent"))
		st["grpcSettings"] = mappedFields(o, map[string]string{"grpc-service-name": "serviceName", "grpc-user-agent": "user_agent"})
	case "httpupgrade":
		field = "http-upgrade-opts"
		o := objectField(p, "http-upgrade-opts")
		need(keysOnly(o, "path", "host"))
		st["httpupgradeSettings"] = o
	case "http":
		field = "http-opts"
		need(inList(typ, "vless", "vmess"))
		o := objectField(p, "http-opts")
		need(keysOnly(o, "method", "path", "headers"))
		st["network"] = "raw"
		st["rawSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": o}}
	case "mkcp", "kcp":
		field = "mkcp-opts"
		need(typ == "vmess")
		o := objectField(p, "mkcp-opts")
		need(keysOnly(o, "mtu", "tti", "uplink-capacity", "downlink-capacity", "seed", "header"))
		k := mappedFields(o, map[string]string{"mtu": "mtu", "tti": "tti", "uplink-capacity": "uplinkCapacity", "downlink-capacity": "downlinkCapacity", "seed": "seed"})
		if has(o, "header") {
			k["header"] = map[string]any{"type": o["header"]}
		}
		st["network"], st["kcpSettings"] = "kcp", k
	case "xhttp":
		field = "xhttp-opts"
		st["xhttpSettings"] = xhttpSettings(without(objectField(p, "xhttp-opts"), "download-settings"))
	}
	field = "socket"
	socket := mappedFields(p, map[string]string{"tfo": "tcpFastOpen", "mptcp": "tcpMptcp", "interface-name": "interface", "routing-mark": "mark"})
	if has(p, "ip-version") {
		// Family preference also affects connection fallback in Mihomo. Keep
		// it outside this direct DNS-family mapping until equivalence is proven.
		strategy, ok := map[string]string{"dual": "ForceIP", "ipv4": "ForceIPv4", "ipv6": "ForceIPv6"}[stringField(p, "ip-version", "")]
		need(ok)
		socket["domainStrategy"] = strategy
	}
	if len(socket) > 0 {
		st["sockopt"] = socket
	}
	field = "tls"
	mtls := configureTLS(p, typ, st)
	if netw == "xhttp" && objectField(p, "xhttp-opts")["download-settings"] != nil {
		download := xhttpDownloadStream(p)
		if socket := st["sockopt"]; socket != nil {
			download["sockopt"] = socket
		}
		obj(obj(st["xhttpSettings"])["extra"])["downloadSettings"] = download
	}
	if typ == "hysteria2" {
		need(netw == "tcp")
		st["network"] = "hysteria"
		st["hysteriaSettings"] = map[string]any{"version": 2, "auth": p["password"]}
		if mask := hysteriaMask(p); len(mask) > 0 {
			st["finalmask"] = mask
		}
	}
	out := map[string]any{"protocol": proto, "tag": p["name"], "settings": s, "streamSettings": st}
	input, err = fromXray(node, out, checks)
	if err != nil || input.Unavailable != nil {
		return input, err
	}
	if mtls {
		return Input{Unavailable: node, Capability: &CapabilityError{Kind: "core-unsupported", Fields: []string{"certificate", "private-key"}}}, nil
	}
	if rm, ok := p["reality-opts"].(map[string]any); ok {
		if z, ok := rm["support-x25519mlkem768"]; ok {
			parts := strings.SplitN(input.Link, "#", 2)
			input.Link = parts[0] + "&support-x25519mlkem768=" + fmt.Sprint(z)
			if len(parts) == 2 {
				input.Link += "#" + parts[1]
			}
		}
	}
	return input, nil
}

func fromXray(node *clashschema.Node, out map[string]any, checks xrayimport.Checks) (Input, error) {
	field := "parameters"
	inputs, conversionError := xrayimport.ConvertOutbound(map[string]any{}, out, 1, false, checks)
	if conversionError != nil {
		if e, ok := conversionError.(*xrayimport.FieldError); ok && e.Field != "" {
			field = e.Field
		}
		return Input{}, &FieldError{Field: field}
	}
	need(len(inputs) == 1)
	if inputs[0].UnavailableSource != nil {
		// The target importer may supply a validated projection together with
		// its source-only controls. Never consume that projection's Link.
		assessment, err := xrayschema.Classify(inputs[0].UnavailableSource)
		if err != nil || len(assessment.Reasons) == 0 {
			return Input{}, &FieldError{Field: "parameters"}
		}
		capability := &CapabilityError{Kind: assessment.Reasons[0].Kind}
		for _, reason := range assessment.Reasons {
			path := reason.Path
			switch path {
			case "Xray.streamSettings.sockopt.interface":
				path = "interface-name"
			case "Xray.streamSettings.sockopt.mark":
				path = "routing-mark"
			}
			capability.Fields = append(capability.Fields, safeField(path))
		}
		return Input{Unavailable: node, Capability: capability}, nil
	}
	return Input{Link: inputs[0].Link, NativeOutbound: inputs[0].NativeOutbound, Name: inputs[0].Name,
		ClientFields: inputs[0].ClientFields, Warnings: inputs[0].Warnings}, nil
}

func configureTLS(p map[string]any, typ string, st map[string]any) bool {
	tls := typ == "trojan" || typ == "hysteria2"
	if has(p, "tls") {
		value, ok := p["tls"].(bool)
		need(ok)
		tls = tls || value
	}
	if has(p, "skip-cert-verify") {
		need(p["skip-cert-verify"] == false)
	}
	sn := stringField(p, "servername", stringField(p, "sni", ""))
	if has(p, "servername") && has(p, "sni") {
		need(p["servername"] == p["sni"])
	}
	if typ == "hysteria2" && sn == "" {
		// Mihomo's Hysteria2 constructor defaults empty SNI to Server. This
		// must be explicit before attaching TLS fields over the parsed link.
		sn = stringField(p, "server", "")
	}
	t := mappedFields(p, map[string]string{"client-fingerprint": "fingerprint", "fingerprint": "pinnedPeerCertSha256", "name-cert-verify": "verifyPeerCertByName", "alpn": "alpn"})
	t["serverName"] = sn
	if ech := objectField(p, "ech-opts"); ech["enable"] == true {
		need(tls && stringField(ech, "config", "") != "")
		t["echConfigList"] = ech["config"]
	}
	mtls := has(p, "certificate") || has(p, "private-key")
	if mtls {
		need(tls && has(p, "certificate") && has(p, "private-key"))
		lines := func(v any) []any {
			s, ok := v.(string)
			need(ok && len(s) <= 128*1024)
			out := []any{}
			for _, line := range strings.Split(strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")), "\n") {
				out = append(out, line)
			}
			return out
		}
		t["certificates"] = []any{map[string]any{"certificate": lines(p["certificate"]), "key": lines(p["private-key"]), "usage": "encipherment"}}
	}
	if has(p, "reality-opts") {
		need(tls && !has(p, "alpn") && !mtls && !has(p, "fingerprint") && !has(p, "name-cert-verify"))
		rm := obj(p["reality-opts"])
		need(keysOnly(rm, "public-key", "short-id", "support-x25519mlkem768"))
		if has(rm, "support-x25519mlkem768") {
			_, ok := rm["support-x25519mlkem768"].(bool)
			need(ok)
		}
		st["security"] = "reality"
		st["realitySettings"] = map[string]any{"serverName": sn, "publicKey": rm["public-key"], "shortId": stringField(rm, "short-id", ""), "fingerprint": stringField(p, "client-fingerprint", "chrome")}
	} else if tls {
		st["security"], st["tlsSettings"] = "tls", t
	} else {
		for _, k := range []string{"servername", "sni", "alpn", "client-fingerprint", "fingerprint", "name-cert-verify", "certificate", "private-key"} {
			need(!has(p, k))
		}
	}
	return mtls
}

func xhttpSettings(o map[string]any) map[string]any {
	mapping := map[string]string{
		"headers": "headers", "no-grpc-header": "noGRPCHeader", "x-padding-bytes": "xPaddingBytes", "x-padding-obfs-mode": "xPaddingObfsMode",
		"x-padding-key": "xPaddingKey", "x-padding-header": "xPaddingHeader", "x-padding-placement": "xPaddingPlacement", "x-padding-method": "xPaddingMethod",
		"uplink-http-method": "uplinkHTTPMethod", "session-placement": "sessionIDPlacement", "session-key": "sessionIDKey", "session-table": "sessionIDTable", "session-length": "sessionIDLength",
		"seq-placement": "seqPlacement", "seq-key": "seqKey", "uplink-data-placement": "uplinkDataPlacement", "uplink-data-key": "uplinkDataKey",
		"uplink-chunk-size": "uplinkChunkSize", "sc-max-each-post-bytes": "scMaxEachPostBytes", "sc-min-posts-interval-ms": "scMinPostsIntervalMs",
	}
	keys := []string{"host", "path", "mode", "reuse-settings"}
	for k := range mapping {
		keys = append(keys, k)
	}
	need(keysOnly(o, keys...))
	x := mappedFields(o, map[string]string{"host": "host", "path": "path", "mode": "mode"})
	extra := mappedFields(o, mapping)
	if has(o, "reuse-settings") {
		rm := obj(o["reuse-settings"])
		mapping := map[string]string{"h-max-request-times": "hMaxRequestTimes", "h-max-reusable-secs": "hMaxReusableSecs", "max-concurrency": "maxConcurrency", "max-connections": "maxConnections", "c-max-reuse-times": "cMaxReuseTimes", "h-keep-alive-period": "hKeepAlivePeriod"}
		keys := []string{}
		for k := range mapping {
			keys = append(keys, k)
		}
		need(keysOnly(rm, keys...))
		extra["xmux"] = mappedFields(rm, mapping)
	}
	x["extra"] = extra
	return x
}

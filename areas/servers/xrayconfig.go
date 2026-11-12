package servers

import (
	"encoding/json"
	"strings"
)

// GenSettings are the inputs of broray_generate_server_config that come from
// config/system/settings.json and the BROray log directory.
type GenSettings struct {
	Listen    any    // settings .listenAddress (nil → JSON null, Xray listens on all addresses)
	SocksPort any    // settings .socksPort // 2080
	LogLevel  any    // settings .logLevel // "warning"
	AccessLog string // <logs>/access.log
	ErrorLog  string // <logs>/error.log
}

// compactObject is the generator's compact_object.
func compactObject(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		switch x := v.(type) {
		case nil:
			continue
		case string:
			if x == "" {
				continue
			}
		case []any:
			if len(x) == 0 {
				continue
			}
		case map[string]any:
			if len(x) == 0 {
				continue
			}
		}
		out[k] = v
	}
	return out
}

func lenPositive(v any) bool {
	l, ok := jqLength(v)
	return ok && l > 0
}

func tlsSettings(s Record) map[string]any {
	out := compactObject(map[string]any{
		"serverName":           alt(get(s, "tls", "serverName"), get(s, "reality", "serverName"), ""),
		"fingerprint":          alt(get(s, "tls", "fingerprint"), get(s, "reality", "fingerprint"), "chrome"),
		"alpn":                 alt(get(s, "tls", "alpn"), []any{}),
		"echConfigList":        alt(get(s, "tls", "echConfigList"), ""),
		"verifyPeerCertByName": alt(get(s, "tls", "verifyPeerCertByName"), ""),
		"pinnedPeerCertSha256": alt(get(s, "tls", "pinnedPeerCertSha256"), ""),
	})
	for _, field := range clientTLSExtraNames {
		if has(s["tls"], field) {
			out[field] = get(s, "tls", field)
		}
	}
	return out
}

func realitySettings(s Record) map[string]any {
	out := compactObject(map[string]any{
		"serverName":    alt(get(s, "reality", "serverName"), ""),
		"fingerprint":   alt(get(s, "reality", "fingerprint"), "chrome"),
		"publicKey":     alt(get(s, "reality", "publicKey"), ""),
		"shortId":       alt(get(s, "reality", "shortId"), ""),
		"spiderX":       alt(get(s, "reality", "spiderX"), ""),
		"mldsa65Verify": alt(get(s, "reality", "mldsa65Verify"), ""),
	})
	if has(s["reality"], "show") {
		out["show"] = get(s, "reality", "show")
	}
	return out
}

func xhttpSettings(s Record) map[string]any {
	src := "transport"
	if s["protocol"] == "vless" {
		src = "xhttp"
	}
	out := map[string]any{"path": alt(get(s, src, "path"), "/"), "mode": alt(get(s, src, "mode"), "auto")}
	if lenPositive(alt(get(s, "transport", "host"), "")) {
		out["host"] = get(s, "transport", "host")
	}
	if extra := alt(get(s, src, "extra"), map[string]any{}); lenPositive(extra) {
		out["extra"] = extra
	}
	return out
}

func rawSettings(s Record) map[string]any {
	if h := get(s, "transport", "header"); h != nil {
		return map[string]any{"header": h}
	}
	ht := alt(get(s, "transport", "headerType"), "none")
	header := map[string]any{"type": ht}
	if ht == "http" {
		req := map[string]any{"path": []any{alt(get(s, "transport", "path"), "/")}}
		if host := alt(get(s, "transport", "host"), ""); host != "" {
			hs, _ := host.(string)
			parts := []any{}
			for _, p := range strings.Split(hs, ",") {
				parts = append(parts, p)
			}
			req["headers"] = map[string]any{"Host": parts}
		}
		header["request"] = req
	}
	return map[string]any{"header": header}
}

func kcpFinalMask(s Record) any {
	if !truthy(get(s, "transport", "kcpLegacy")) {
		return alt(get(s, "transport", "finalMask"), map[string]any{})
	}
	settings := map[string]any{}
	if seed := alt(get(s, "transport", "kcpSeed"), ""); seed != "" {
		settings["value"] = get(s, "transport", "kcpSeed")
	}
	udp := []any{map[string]any{"type": "mkcp-legacy", "settings": settings}}
	if ht := alt(get(s, "transport", "headerType"), "none"); ht != "none" {
		h := get(s, "transport", "headerType")
		if h == "wechat-video" {
			h = "wechat"
		}
		st := map[string]any{"header": h}
		if get(s, "transport", "headerType") == "dns" && alt(get(s, "transport", "host"), "") != "" {
			st["value"] = get(s, "transport", "host")
		}
		udp = append(udp, map[string]any{"type": "mkcp-legacy", "settings": st})
	}
	return map[string]any{"udp": udp}
}

func hysteriaFinalMask(s Record) any {
	fm := alt(get(s, "hysteria", "finalMask"), map[string]any{})
	var obfs, hop []any
	if alt(get(s, "hysteria", "obfs"), "") != "" {
		st := map[string]any{"password": get(s, "hysteria", "obfsPassword")}
		if get(s, "hysteria", "obfs") == "gecko" {
			st["packetSize"] = "512-1200"
		}
		obfs = append(obfs, map[string]any{"type": "salamander", "settings": st})
	}
	if alt(get(s, "hysteria", "ports"), "") != "" {
		hop = append(hop, map[string]any{"type": "udphop", "settings": map[string]any{"mode": "intervalremote", "interval": 30, "remotePorts": get(s, "hysteria", "ports")}})
	}
	if len(obfs)+len(hop) == 0 {
		return fm
	}
	out := map[string]any{}
	if m, ok := fm.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	udp := append([]any{}, obfs...)
	if arr, ok := alt(get(fm, "udp"), []any{}).([]any); ok {
		udp = append(udp, arr...)
	}
	udp = append(udp, hop...)
	out["udp"] = udp
	return out
}

func streamSettings(s Record) (map[string]any, error) {
	if s["network"] == "xdrive" || s["network"] == "masque" {
		st := nativeStream(s, asString(s["network"]))
		mergeClientStreamEndpoint(st, s)
		return st, nil
	}
	st := map[string]any{"network": s["network"], "security": s["security"]}
	switch s["network"] {
	case "xhttp":
		st["xhttpSettings"] = xhttpSettings(s)
	case "raw":
		st["rawSettings"] = rawSettings(s)
	case "ws":
		ws := map[string]any{"path": alt(get(s, "transport", "path"), "/")}
		if lenPositive(alt(get(s, "transport", "host"), "")) {
			ws["host"] = get(s, "transport", "host")
		}
		st["wsSettings"] = ws
	case "grpc":
		st["grpcSettings"] = compactObject(map[string]any{
			"serviceName": alt(get(s, "transport", "serviceName"), ""),
			"authority":   alt(get(s, "transport", "host"), ""),
			"multiMode":   alt(get(s, "transport", "mode"), "") == "multi",
		})
	case "httpupgrade":
		st["httpupgradeSettings"] = compactObject(map[string]any{
			"path": alt(get(s, "transport", "path"), "/"),
			"host": alt(get(s, "transport", "host"), ""),
		})
	case "kcp":
		st["kcpSettings"] = alt(get(s, "transport", "kcp"), map[string]any{})
	case "hysteria":
		st["hysteriaSettings"] = map[string]any{"version": 2, "auth": s["auth"]}
		if has(s["hysteria"], "udpIdleTimeout") {
			st["hysteriaSettings"].(map[string]any)["udpIdleTimeout"] = get(s, "hysteria", "udpIdleTimeout")
		}
	default:
		return nil, die("неподдерживаемый транспорт: " + jqRaw(s["network"]))
	}
	switch s["security"] {
	case "tls":
		st["tlsSettings"] = tlsSettings(s)
	case "reality":
		st["realitySettings"] = realitySettings(s)
	case "none":
	default:
		return nil, die("неподдерживаемая защита: " + jqRaw(s["security"]))
	}
	if s["network"] == "hysteria" && lenPositive(hysteriaFinalMask(s)) {
		st["finalmask"] = hysteriaFinalMask(s)
	} else if s["network"] == "kcp" && lenPositive(kcpFinalMask(s)) {
		st["finalmask"] = kcpFinalMask(s)
	} else if fm := alt(get(s, "transport", "finalMask"), map[string]any{}); lenPositive(fm) {
		st["finalmask"] = get(s, "transport", "finalMask")
	}
	mergeStreamOptions(st, s)
	mergeClientStreamEndpoint(st, s)
	return st, nil
}

// Outbound renders the Xray outbound (tag "proxy") of a record.
func Outbound(s Record) (map[string]any, error) {
	if err := requireConnectionSupport(s); err != nil {
		return nil, err
	}
	if err := validateClientFields(s); err != nil {
		return nil, err
	}
	var settings map[string]any
	withStream := true
	protocol := s["protocol"]
	switch protocol {
	case "masque":
		settings = map[string]any{"address": s["address"], "port": s["port"]}
		if has(s, "remoteDNS") {
			settings["remoteDNS"] = s["remoteDNS"]
		}
	case "wireguard":
		settings, _ = s["wireguard"].(map[string]any)
		withStream = false
	case "socks", "http":
		if asString(s["username"]) == "" && asString(s["password"]) != "" && s["proxyAccount"] != true {
			return nil, nativeError("settings.servers.users")
		}
		server := map[string]any{"address": s["address"], "port": s["port"]}
		if asString(s["username"]) != "" || s["proxyAccount"] == true {
			user := map[string]any{"user": s["username"], "pass": s["password"]}
			if has(s, "accountEmail") {
				user["email"] = s["accountEmail"]
			}
			if has(s, "accountLevel") {
				user["level"] = s["accountLevel"]
			}
			server["users"] = []any{user}
		}
		settings = map[string]any{"servers": []any{server}}
		if asString(s["username"]) == "" && s["proxyAccount"] != true && (has(s, "accountEmail") || has(s, "accountLevel")) {
			// Flat metadata without an account does not enable proxy auth.
			settings = map[string]any{"address": s["address"], "port": s["port"]}
			if has(s, "accountEmail") {
				settings["email"] = s["accountEmail"]
			}
			if has(s, "accountLevel") {
				settings["level"] = s["accountLevel"]
			}
		}
		if s["protocol"] == "http" && has(s, "httpHeaders") {
			settings["headers"] = s["httpHeaders"]
		}
	case "vless":
		user := map[string]any{"id": s["uuid"], "encryption": alt(s["encryption"], "none")}
		if has(s, "accountLevel") {
			user["level"] = s["accountLevel"]
		}
		if has(s, "accountEmail") {
			user["email"] = s["accountEmail"]
		}
		if has(s, "vlessTestpre") {
			user["testpre"] = s["vlessTestpre"]
		}
		if lenPositive(alt(s["flow"], "")) {
			user["flow"] = s["flow"]
		}
		settings = map[string]any{"vnext": []any{map[string]any{"address": s["address"], "port": s["port"], "users": []any{user}}}}
	case "vmess":
		user := map[string]any{"id": s["uuid"], "alterId": alt(s["alterId"], json.Number("0")), "security": alt(s["encryption"], "auto")}
		if has(s, "accountLevel") {
			user["level"] = s["accountLevel"]
		}
		if has(s, "accountEmail") {
			user["email"] = s["accountEmail"]
		}
		if has(s, "vmessExperiments") {
			user["experiments"] = s["vmessExperiments"]
		}
		settings = map[string]any{"vnext": []any{map[string]any{"address": s["address"], "port": s["port"], "users": []any{user}}}}
	case "trojan":
		server := map[string]any{"address": s["address"], "port": s["port"], "password": s["password"]}
		if has(s, "accountLevel") {
			server["level"] = s["accountLevel"]
		}
		if has(s, "accountEmail") {
			server["email"] = s["accountEmail"]
		}
		settings = map[string]any{"servers": []any{server}}
	case "shadowsocks":
		server := map[string]any{"address": s["address"], "port": s["port"], "method": s["method"], "password": s["password"]}
		if has(s, "accountLevel") {
			server["level"] = s["accountLevel"]
		}
		if has(s, "accountEmail") {
			server["email"] = s["accountEmail"]
		}
		settings = map[string]any{"servers": []any{server}}
		withStream = s["network"] != "raw" || s["security"] != "none" || lenPositive(s["transport"]) || lenPositive(s["streamOptions"]) || has(s, "streamEndpoint")
	case "hysteria2":
		protocol = "hysteria"
		settings = map[string]any{"version": 2, "address": s["address"], "port": s["port"]}
		if lenPositive(alt(get(s, "hysteria", "obfsPassword"), "")) {
			settings["obfsPassword"] = get(s, "hysteria", "obfsPassword")
		}
	default:
		return nil, die("генератор не поддерживает протокол: " + jqRaw(s["protocol"]))
	}
	out := map[string]any{"tag": "proxy", "protocol": protocol, "settings": settings}
	if has(s, "mux") {
		out["mux"] = s["mux"]
	}
	if opts, ok := s["outboundOptions"].(map[string]any); ok {
		for k, v := range opts {
			out[k] = v
		}
	}
	if withStream {
		st, err := streamSettings(s)
		if err != nil {
			return nil, err
		}
		out["streamSettings"] = st
	}
	return out, nil
}

// XrayConfig is broray_generate_server_config: it validates the record and
// renders the full Xray configuration (one SOCKS inbound + outbound "proxy").
func XrayConfig(rec Record, gs GenSettings, label string) ([]byte, error) {
	if err := requireConnectionSupport(rec); err != nil {
		return nil, err
	}
	s := normalize(rec)
	if err := validate(s, label); err != nil {
		return nil, err
	}
	ob, err := Outbound(s)
	if err != nil {
		pe, _ := err.(*ParseError)
		return nil, then(pe, "не удалось создать конфигурацию сервера")
	}
	cfg := map[string]any{
		"log": map[string]any{"access": gs.AccessLog, "error": gs.ErrorLog, "loglevel": alt(gs.LogLevel, "warning")},
		"inbounds": []any{map[string]any{"tag": "socks", "listen": gs.Listen, "port": alt(gs.SocksPort, json.Number("2080")),
			"protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}}},
		"outbounds": []any{ob},
	}
	if rec["network"] == "xdrive" || rec["protocol"] == "masque" {
		// Core backend errors may contain raw API response bodies/credentials.
		// BROray publishes bounded sanitized operation results separately.
		cfg["log"] = privateCoreLogSettings()
	}
	return Marshal(cfg)
}

func privateCoreLogSettings() map[string]any {
	return map[string]any{"loglevel": "none", "access": "none", "error": "none"}
}

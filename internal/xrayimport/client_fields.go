package xrayimport

import "strings"

// extractClientFields removes only explicitly validated endpoint fields from
// the legacy share-link bridge. No arbitrary JSON member can bypass validation.
func (c *converter) extractClientFields(input map[string]any) (map[string]any, map[string]any, []map[string]any) {
	out := without(input)
	fields := map[string]any{}
	var variants []map[string]any
	stream := objectField(out, "streamSettings")
	stream = without(stream)
	if has(stream, "method") {
		method := stringField(stream, "method", "")
		if has(stream, "network") {
			stringField(stream, "network", "")
		}
		stream["network"] = method
		delete(stream, "method")
	}
	net := stringField(stream, "network", "raw")
	switch strings.ToLower(net) {
	case "raw", "tcp":
		net = "raw"
	case "xhttp", "splithttp":
		net = "xhttp"
	case "mkcp", "kcp":
		net = "kcp"
	case "websocket", "ws":
		net = "ws"
	case "httpupgrade", "grpc", "hysteria", "masque", "xdrive":
		net = strings.ToLower(net)
	default:
		panic(jqFail{field: "streamSettings.network"})
	}
	stream["network"] = net
	if out["protocol"] == "hysteria" && net == "hysteria" {
		h := objectField(stream, "hysteriaSettings")
		if v, ok := h["masquerade"]; ok {
			validateHysteriaMasquerade(v)
			fields["hysteriaMasquerade"] = v
		}
	}
	if has(stream, "security") {
		security := strings.ToLower(stringField(stream, "security", "none"))
		if security == "" {
			security = "none"
		}
		stream["security"] = security
	}
	for _, aliases := range [][2]string{{"rawSettings", "tcpSettings"}, {"xhttpSettings", "splithttpSettings"}} {
		if has(stream, aliases[0]) && has(stream, aliases[1]) {
			// Both values must have their declared object shape, even though
			// the current spelling has upstream precedence.
			objectField(stream, aliases[0])
			objectField(stream, aliases[1])
			delete(stream, aliases[1])
		}
	}
	endpoint := map[string]any{}
	for _, key := range []string{"address", "port"} {
		if has(stream, key) {
			endpoint[key] = stream[key]
			delete(stream, key)
		}
	}
	if len(endpoint) > 0 {
		fields["streamEndpoint"] = endpoint
	}
	native := out["protocol"] == "masque" || net == "masque" || net == "xdrive"
	if has(out, "mux") {
		fields["mux"] = out["mux"]
		if !native {
			delete(out, "mux")
		}
	}
	options := map[string]any{}
	for _, key := range []string{"targetStrategy", "sendThrough"} {
		if has(out, key) {
			options[key] = out[key]
			delete(out, key)
		}
	}
	if len(options) > 0 {
		fields["outboundOptions"] = options
	}
	if has(stream, "tlsSettings") {
		full := objectField(stream, "tlsSettings")
		full = without(full)
		if value, ok := full["alpn"].(string); ok {
			var list []any
			for _, item := range strings.Split(value, ",") {
				list = append(list, item)
			}
			full["alpn"] = list
		}
		fields["tls"] = full
		if native {
			stream["tlsSettings"] = full
		} else {
			basic := map[string]any{}
			for _, key := range []string{"serverName", "fingerprint", "alpn", "allowInsecure", "echConfigList", "pinnedPeerCertSha256", "verifyPeerCertByName"} {
				if has(full, key) {
					basic[key] = full[key]
				}
			}
			stream["tlsSettings"] = basic
		}
	}
	if has(stream, "realitySettings") {
		reality := objectField(stream, "realitySettings")
		if has(reality, "show") {
			fields["reality"] = map[string]any{"show": reality["show"]}
		}
	}
	if has(out, "streamSettings") || len(stream) > 1 {
		out["streamSettings"] = stream
	}
	settings := objectField(out, "settings")
	settings = without(settings)
	if inList(asStringValue(out["protocol"]), "vless", "vmess", "trojan", "shadowsocks", "http", "socks") {
		if has(settings, "email") || has(settings, "level") {
			need(!has(settings, "servers") && !has(settings, "vnext"))
			extractAccountMetadata(settings, fields)
		}
	}
	if out["protocol"] == "http" && has(settings, "headers") {
		fields["httpHeaders"] = settings["headers"]
		delete(settings, "headers")
	}
	if out["protocol"] == "vmess" || out["protocol"] == "vless" {
		inputField, recordField := "experiments", "vmessExperiments"
		if out["protocol"] == "vless" {
			inputField, recordField = "testpre", "vlessTestpre"
		}
		need(!(has(settings, inputField) && has(settings, "vnext")))
		if has(settings, inputField) {
			fields[recordField] = settings[inputField]
			delete(settings, inputField)
		}
		if has(settings, "vnext") {
			var endpoints []any
			for _, ev := range listOf(settings["vnext"]) {
				ep := without(obj(ev))
				var users []any
				for _, uv := range listOf(ep["users"]) {
					user := without(obj(uv))
					extra := map[string]any{}
					extractAccountMetadata(user, extra)
					if has(user, inputField) {
						extra[recordField] = user[inputField]
						delete(user, inputField)
					}
					variants = append(variants, extra)
					users = append(users, user)
				}
				ep["users"] = users
				endpoints = append(endpoints, ep)
			}
			settings["vnext"] = endpoints
		}
	}
	if inList(asStringValue(out["protocol"]), "http", "socks", "trojan", "shadowsocks") && has(settings, "servers") {
		var servers []any
		for _, value := range listOf(settings["servers"]) {
			server := without(obj(value))
			extra := map[string]any{}
			if out["protocol"] == "http" || out["protocol"] == "socks" {
				if has(server, "users") {
					values, ok := server["users"].([]any)
					need(ok && len(values) <= 1)
					users := make([]any, 0, len(values))
					for _, value := range values {
						user := without(obj(value))
						extractAccountMetadata(user, extra)
						if stringField(user, "user", "") == "" {
							// A legacy users entry creates a core account even
							// with empty credentials; a flat empty username does not.
							extra["proxyAccount"] = true
							if has(user, "pass") {
								// Preserve the private credential outside the URI
								// projection, whose grammar requires a username.
								extra["password"] = user["pass"]
								user["pass"] = ""
							}
						}
						users = append(users, user)
					}
					server["users"] = users
				}
			} else {
				extractAccountMetadata(server, extra)
			}
			variants = append(variants, extra)
			servers = append(servers, server)
		}
		settings["servers"] = servers
	}
	out["settings"] = settings
	protocol := stringField(out, "protocol", "")
	if protocol == "hysteria" {
		protocol = "hysteria2"
	}
	security := stringField(stream, "security", "none")
	if len(fields) > 0 {
		need(c.checks.ValidateClientFields != nil)
		need(c.checks.ValidateClientFields(protocol, net, security, fields) == nil)
	}
	for _, extra := range variants {
		need(c.checks.ValidateClientFields != nil)
		need(c.checks.ValidateClientFields(protocol, net, security, extra) == nil)
	}
	return out, fields, variants
}

func asStringValue(v any) string { s, _ := v.(string); return s }

func extractAccountMetadata(account, fields map[string]any) {
	for _, pair := range [][2]string{{"email", "accountEmail"}, {"level", "accountLevel"}} {
		if has(account, pair[0]) {
			fields[pair[1]] = account[pair[0]]
			delete(account, pair[0])
		}
	}
}

// Nested download TLS remains in its structured XHTTP extra rather than the
// ordinary share-link TLS fields. The same owner validates both locations.
func (c *converter) downloadProtection(stream map[string]any) {
	security := strings.ToLower(stringField(stream, "security", "none"))
	if security == "" {
		security = "none"
	}
	if has(stream, "security") {
		stream["security"] = security
	}
	if security != "tls" {
		protection(stream)
		return
	}
	need(!has(stream, "realitySettings"))
	settings := objectField(stream, "tlsSettings")
	need(c.checks.ValidateClientFields != nil)
	need(c.checks.ValidateClientFields("vless", "xhttp", "tls", map[string]any{"tls": settings}) == nil)
}

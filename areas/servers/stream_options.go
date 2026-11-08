package servers

import (
	"encoding/json"
	"strings"
)

// StreamOptions carries only explicitly supported client-side stream fields.
// It cannot change listeners, routing, ownership, or process configuration.
func streamOptionsError(r Record) *ParseError {
	if !has(r, "streamOptions") {
		return nil
	}
	m, ok := r["streamOptions"].(map[string]any)
	if !ok {
		return die("streamOptions должен быть объектом")
	}
	b, e := json.Marshal(m)
	if e != nil || len(b) > 8192 {
		return die("streamOptions превышает допустимый размер")
	}
	bad := func(k string) *ParseError {
		return die("неподдерживаемое или неправильное поле streamOptions: " + safeField(k))
	}
	for block, v := range m {
		opts, ok := v.(map[string]any)
		if !ok {
			return bad(block)
		}
		expected := map[string]string{"wsSettings": "ws", "httpupgradeSettings": "httpupgrade", "grpcSettings": "grpc"}
		if block != "sockopt" && (expected[block] == "" || expected[block] != r["network"]) {
			return bad(block)
		}
		for k, val := range opts {
			valid := false
			switch block {
			case "wsSettings", "httpupgradeSettings":
				switch k {
				case "headers":
					headers, ok := val.(map[string]any)
					valid = ok && len(headers) <= 32
					seen := map[string]bool{}
					for name, value := range headers {
						lower := strings.ToLower(name)
						if !headerToken(name) || lower == "host" || seen[lower] || !textValue(value) {
							valid = false
							break
						}
						seen[lower] = true
					}
				case "heartbeatPeriod":
					valid = block == "wsSettings" && nonNegInt32(val)
				}
			case "grpcSettings":
				switch k {
				case "idle_timeout", "health_check_timeout", "initial_windows_size":
					valid = nonNegInt32(val)
				case "permit_without_stream":
					_, valid = val.(bool)
				case "user_agent":
					valid = textValue(val)
				}
			case "sockopt":
				switch k {
				case "tcpFastOpen":
					_, valid = val.(bool)
					if !valid {
						valid = nonNegInt32(val)
					}
				case "tcpKeepAliveIdle", "tcpKeepAliveInterval":
					valid = nonNegInt32(val)
				case "domainStrategy":
					valid = in(asString(val), "AsIs", "UseIP", "UseIPv4", "UseIPv6", "UseIPv4v6", "UseIPv6v4", "ForceIP", "ForceIPv4", "ForceIPv6", "ForceIPv4v6", "ForceIPv6v4")
				}
			}
			if !valid {
				return bad(block + "." + k)
			}
		}
	}
	return nil
}

func safeField(s string) string {
	if len(s) > 96 {
		return "(имя поля скрыто)"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return "(имя поля скрыто)"
		}
	}
	return s
}

func headerToken(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

func parseLink(link, id string, source map[string]any) (Record, *ParseError) {
	r, err := parseBaseLink(link, id, source)
	if err != nil {
		return nil, err
	}
	if in(asString(r["protocol"]), "wireguard", "amneziawg") {
		return r, nil
	}
	body, _, _ := splitFirst(link, '#')
	_, raw, _ := splitFirst(body, '?')
	q := query(raw)
	if err := auditQuery(q, r); err != nil {
		return nil, err
	}
	if q.has("streamOptions") {
		s, e := q.value("streamOptions")
		if e != nil {
			return nil, e
		}
		if len(s) > 8192 {
			return nil, die("streamOptions превышает допустимый размер")
		}
		v, ok := decodeJSON(s)
		if !ok || !isObject(v) {
			return nil, die("неправильный JSON streamOptions")
		}
		r["streamOptions"] = v
		if e := streamOptionsError(r); e != nil {
			return nil, e
		}
	}
	if e := validate(r, tempLabel()); e != nil {
		return nil, e
	}
	return r, nil
}

func auditQuery(q query, r Record) *ParseError {
	allowed := []string{"streamOptions"}
	proto := asString(r["protocol"])
	switch proto {
	case "vless", "vmess", "trojan":
		allowed = append(allowed, "type", "security", "encryption", "flow", "sni", "fp", "pbk", "sid", "spx", "host", "authority", "path", "serviceName", "service_name", "mode", "headerType", "extra", "alpn", "allowInsecure", "insecure", "header", "ech", "pcs", "vcn", "pqv", "fm", "seed", "mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow")
	case "http", "socks":
		allowed = append(allowed, "type", "security", "sni", "fp", "alpn", "pcs", "vcn", "ech")
	case "hysteria2":
		allowed = append(allowed, "sni", "peer", "security", "fp", "insecure", "alpn", "pinSHA256", "obfs", "obfs-password", "fm", "udpIdleTimeout", "ech", "pcs", "vcn", "pqv")
	case "shadowsocks":
		allowed = append(allowed, "plugin")
	}
	if q.has("extra") && r["network"] != "xhttp" {
		raw, _ := q.value("extra")
		if raw != "" && raw != "{}" {
			return die("поле extra поддерживается только для XHTTP")
		}
	}
	if q.has("peer") && q.has("sni") {
		return die("повторяющийся параметр URI: sni/peer")
	}
	if q.has("x_padding_bytes") {
		if r["network"] != "xhttp" {
			return die("поле x_padding_bytes поддерживается только для XHTTP")
		}
		padding, _ := q.value("x_padding_bytes")
		if _, ok := rangePair(padding); !ok {
			return die("неправильное поле x_padding_bytes")
		}
		for _, block := range []string{"transport", "xhttp"} {
			m, ok := r[block].(map[string]any)
			if !ok {
				continue
			}
			extra, ok := m["extra"].(map[string]any)
			if !ok {
				extra = map[string]any{}
				m["extra"] = extra
			}
			if old, exists := extra["xPaddingBytes"]; exists && !jsonValueEqual(old, padding) {
				return die("конфликт x_padding_bytes и extra.xPaddingBytes")
			}
			extra["xPaddingBytes"] = padding
		}
	}
	allowed = append(allowed, "x_padding_bytes")
	metadata := []string{"support-x25519mlkem768"}
	allowed = append(allowed, metadata...)
	if e := queryKeys(q, allowed...); e != nil {
		return e
	}
	var warnings []any
	for _, key := range metadata {
		if q.has(key) {
			warnings = append(warnings, "Поле URI "+key+" не применяется; параметры соединения берутся из поддерживаемых полей.")
		}
	}
	if len(warnings) > 0 {
		r["importWarnings"] = warnings
	}
	return nil
}

func mergeStreamOptions(st map[string]any, r Record) {
	m, _ := r["streamOptions"].(map[string]any)
	for block, v := range m {
		target, _ := st[block].(map[string]any)
		if target == nil {
			target = map[string]any{}
			st[block] = target
		}
		opts, _ := v.(map[string]any)
		for k, val := range opts {
			target[k] = val
		}
	}
}

// ValidateStreamOptions is shared by URI, stored records and Xray JSON import.
func ValidateStreamOptions(network string, options map[string]any) error {
	if e := streamOptionsError(Record{"network": network, "streamOptions": options}); e != nil {
		return e
	}
	return nil
}

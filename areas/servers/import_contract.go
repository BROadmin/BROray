package servers

import (
	"encoding/json"
	"strings"
)

// Context checks concern fresh input only. Stored records may contain unused
// historical defaults; reading them must not silently rewrite a connection.
func validateQueryContext(q query, rec Record) *ParseError {
	proto, network, security := asString(rec["protocol"]), asString(rec["network"]), asString(rec["security"])
	if !in(proto, "vless", "vmess", "trojan", "http", "socks", "shadowsocks") {
		return nil
	}
	for _, key := range []string{"flow", "encryption", "authority", "serviceName", "service_name", "mode", "sni", "fp", "alpn", "pbk", "sid", "spx", "pqv", "ech", "pcs", "vcn", "host", "path"} {
		v, err := q.value(key)
		if err != nil {
			return err
		}
		if v == "" {
			continue
		}
		if !contextFieldAllowed(key, v, proto, network, security, asString(get(rec, "transport", "headerType"))) {
			return die("поле " + key + " неприменимо к выбранным протоколу, транспорту или защите")
		}
	}
	return nil
}

func contextFieldAllowed(key, v, proto, network, security, headerType string) bool {
	if v == "" {
		return true
	}
	switch key {
	case "flow", "encryption":
		return proto != "trojan"
	case "authority", "serviceName", "service_name":
		return network == "grpc"
	case "mode":
		// An explicit auto on a transport without modes is a neutral
		// compatibility default. Other values must never disappear.
		return in(network, "grpc", "xhttp") || v == "auto"
	case "sni", "fp":
		return in(security, "tls", "reality")
	case "alpn":
		return security == "tls"
	case "pbk", "sid", "spx", "pqv":
		return security == "reality"
	case "ech", "pcs", "vcn":
		return security == "tls"
	case "host":
		return in(network, "ws", "httpupgrade", "grpc", "xhttp") || network == "raw" && headerType == "http" || network == "kcp" && headerType == "dns"
	case "path":
		return in(network, "ws", "httpupgrade", "xhttp") || network == "raw" && headerType == "http" || v == "/"
	}
	return true
}

// resolveAlias compares meanings after field-specific normalization. The
// returned value is the original spelling for the existing parser; no secret
// value is included in a conflict error.
func resolveAlias(m map[string]any, canonical string, aliases ...string) (any, *ParseError) {
	var first, normalized any
	firstKey := ""
	for _, key := range append([]string{canonical}, aliases...) {
		value, present := m[key]
		if !present {
			continue
		}
		norm, ok := normalizeAlias(canonical, value)
		if !ok {
			if canonical == "aid" {
				return nil, die("VMess alterId должен быть числом")
			}
			return nil, die("неправильное поле VMess JSON: " + key)
		}
		if firstKey == "" {
			first, normalized, firstKey = value, norm, key
		} else if !jsonValueEqual(normalized, norm) {
			return nil, die("конфликт полей VMess JSON: " + firstKey + "/" + key)
		}
	}
	return first, nil
}

func normalizeAlias(field string, value any) (any, bool) {
	if field == "aid" {
		if s, ok := value.(string); ok {
			if !allDigits(s) {
				return nil, false
			}
			value = json.Number(s)
		}
		n, ok := isInt(value)
		return n, ok && n >= 0 && n <= 65535
	}
	s, ok := value.(string)
	if !ok {
		return nil, false
	}
	switch field {
	case "net":
		switch s {
		case "", "tcp", "raw":
			s = "raw"
		case "websocket", "ws":
			s = "ws"
		case "httpUpgrade", "httpupgrade":
			s = "httpupgrade"
		case "splithttp", "xhttp":
			s = "xhttp"
		}
	case "tls":
		if s == "" {
			s = "none"
		}
	case "fp":
		s = strings.ToLower(s)
	}
	return s, true
}

func validateLegacyAliases(m map[string]any) *ParseError {
	for _, keys := range [][]string{
		{"ps", "remarks", "name"}, {"add", "address"}, {"id", "uuid"}, {"aid", "alterId"},
		{"scy", "securityCipher", "cipher"}, {"net", "network"}, {"tls", "security"},
		{"sni", "serverName"}, {"fp", "fingerprint"}, {"serviceName", "service_name"},
		{"pbk", "publicKey"}, {"sid", "shortId"}, {"spx", "spiderX"}, {"type", "headerType"},
	} {
		if _, err := resolveAlias(m, keys[0], keys[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func validateLegacyContext(m map[string]any, p *parsed) *ParseError {
	if p.network == "grpc" {
		if _, err := resolveAlias(m, "serviceName", "service_name", "path"); err != nil {
			return err
		}
	}
	for _, field := range []string{"host", "path", "serviceName", "service_name", "mode", "sni", "serverName", "fp", "fingerprint", "pbk", "publicKey", "sid", "shortId", "spx", "spiderX", "pqv", "ech", "pcs", "vcn", "alpn"} {
		v, present := m[field]
		if !present {
			continue
		}
		var value string
		if field == "alpn" {
			switch a := v.(type) {
			case string:
				value = a
			case []any:
				for _, item := range a {
					s, ok := item.(string)
					if !ok {
						return die("неправильное поле VMess JSON: alpn")
					}
					value += s
				}
			default:
				return die("неправильное поле VMess JSON: alpn")
			}
		} else {
			var ok bool
			value, ok = v.(string)
			if !ok {
				return die("неправильное поле VMess JSON: " + field)
			}
		}
		key := field
		switch field {
		case "serverName":
			key = "sni"
		case "fingerprint":
			key = "fp"
		case "publicKey":
			key = "pbk"
		case "shortId":
			key = "sid"
		case "spiderX":
			key = "spx"
		case "path":
			if in(p.network, "grpc", "kcp") {
				continue
			} // defined legacy aliases
		}
		if !contextFieldAllowed(key, value, "vmess", p.network, p.security, p.headerType) {
			return die("неприменимое поле VMess JSON: " + field)
		}
	}
	if p.network != "kcp" {
		for _, field := range []string{"seed", "mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow", "congestion", "readBufferSize", "writeBufferSize"} {
			if has(m, field) {
				return die("параметр VMess " + field + " поддерживается только для mKCP")
			}
		}
	}
	return nil
}

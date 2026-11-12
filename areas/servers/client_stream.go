package servers

import "net"

// Proxy and SS links use the same explicit transport query vocabulary as the
// ordinary stream parser. Only parsed transport fields are copied; this helper
// never supplies an account or changes protocol/source identity.
var clientStreamQueryKeys = []string{"type", "security", "sni", "fp", "pbk", "sid", "spx", "host", "authority", "path", "serviceName", "service_name", "mode", "headerType", "extra", "alpn", "header", "ech", "pcs", "vcn", "pqv", "fm", "seed", "mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow", "streamOptions"}

func parseClientTransport(address, port, rawQuery, defaultSecurity string) (*parsed, *ParseError) {
	q := query(rawQuery)
	if !q.has("security") && defaultSecurity != "none" {
		if rawQuery != "" {
			rawQuery += "&"
		}
		rawQuery += "security=" + defaultSecurity
	}
	return parseVless("vless://stream@" + net.JoinHostPort(address, port) + "?" + rawQuery)
}

func saveClientTransport(r Record, p *parsed) (Record, *ParseError) {
	extra, ok := decodeJSON(p.extra)
	if !ok || !isObject(extra) {
		return nil, die("неправильный JSON параметров транспорта")
	}
	r["network"], r["security"] = p.network, p.security
	r["transport"] = transportBlock(p, extra)
	if p.security == "tls" {
		tls := tlsBlock(p)
		if in(asString(r["protocol"]), "http", "socks") {
			delete(tls, "allowInsecure")
			tls = compactObject(tls)
		}
		r["tls"] = tls
	}
	if p.security == "reality" {
		r["reality"] = realityBlock(p)
	}
	return finish(r, p)
}

func ordinaryClientStreamValid(r Record) bool {
	if !in(asString(r["network"]), "raw", "ws", "httpupgrade", "grpc", "xhttp", "kcp") ||
		!in(asString(r["security"]), "none", "tls", "reality") {
		return false
	}
	if r["security"] == "reality" {
		if !in(asString(r["network"]), "raw", "grpc", "xhttp") ||
			!nonEmptyString(get(r, "reality", "serverName")) || !nonEmptyString(get(r, "reality", "publicKey")) {
			return false
		}
	}
	return streamFieldsValid(r)
}

package servers

import (
	"net/netip"
	"net/url"
	"strings"
)

func serverAddressValid(v any) bool {
	host, ok := v.(string)
	if !ok || host == "" || len(host) > 253 {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func basicRecordValid(r Record) bool {
	p, ok := isInt(r["port"])
	return jsonEq(r["schemaVersion"], 2) && ValidID(asString(r["id"])) && textValue(r["name"]) && isObject(r["source"]) &&
		nonEmptyString(get(r, "source", "type")) && serverAddressValid(r["address"]) && ok && p >= 1 && p <= 65535
}

func queryKeys(q query, allowed ...string) *ParseError {
	for _, item := range q.items() {
		key, err := uriDecode(itemKey(item))
		if err != nil {
			return err
		}
		if !in(key, allowed...) {
			// The value may contain credentials. Even a field name is bounded
			// and checked before including it in a public error.
			if len(key) > 64 || !isText(key) {
				return die("неподдерживаемое поле URI")
			}
			return die("неподдерживаемое поле URI: " + safeField(key))
		}
		if _, err := q.value(key); err != nil {
			return err
		}
	}
	return nil
}

func parseProxy(link, id string, source map[string]any) (Record, *ParseError) {
	u, err := url.Parse(link)
	if err != nil || u.Opaque != "" || !in(u.Scheme, "socks", "socks5", "http", "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return nil, die("неправильная ссылка прокси")
	}
	q := query(u.RawQuery)
	if err := queryKeys(q, "type", "security", "sni", "fp", "alpn", "pcs", "vcn", "ech", "streamOptions"); err != nil {
		return nil, err
	}
	proto, security, port := "http", "none", "80"
	switch u.Scheme {
	case "socks", "socks5":
		proto, port = "socks", "1080"
	case "https":
		security, port = "tls", "443"
	}
	if u.Port() != "" {
		port = u.Port()
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, die("не указан порт прокси")
	}
	n, pe := portCheck(port, "прокси ")
	if pe != nil {
		return nil, pe
	}
	network, _ := q.value("type")
	if !in(network, "", "raw", "tcp") {
		return nil, die("для прокси поддерживается транспорт RAW/TCP")
	}
	if sec, _ := q.value("security"); sec != "" {
		if !in(sec, "none", "tls") || u.Scheme == "https" && sec != "tls" {
			return nil, die("защита не соответствует схеме прокси")
		}
		security = sec
	}
	user, password := "", ""
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
		if user == "" {
			return nil, die("не указан пользователь прокси")
		}
	}
	name := u.Fragment
	if name == "" {
		name = u.Hostname() + ":" + port
	}
	r := Record{"schemaVersion": 2, "id": id, "name": name, "source": cloneSource(source), "uri": link, "protocol": proto,
		"address": u.Hostname(), "port": n, "network": "raw", "security": security, "username": user, "password": password}
	tls := map[string]any{}
	for _, pair := range [][2]string{{"sni", "serverName"}, {"fp", "fingerprint"}, {"pcs", "pinnedPeerCertSha256"}, {"vcn", "verifyPeerCertByName"}, {"ech", "echConfigList"}} {
		if v, _ := q.value(pair[0]); v != "" {
			tls[pair[1]] = v
		}
	}
	if v, _ := q.value("alpn"); v != "" {
		tls["alpn"] = splitNonEmpty(v)
	}
	if security == "none" && len(tls) > 0 {
		return nil, die("параметры TLS требуют защищённого соединения прокси")
	}
	if security == "tls" {
		if !has(tls, "serverName") {
			tls["serverName"] = u.Hostname()
		}
		if !has(tls, "fingerprint") {
			tls["fingerprint"] = "chrome"
		}
		r["tls"] = tls
	}
	r = normalize(r)
	if e := validateProxy(r); e != nil {
		return nil, e
	}
	return r, nil
}

func validateProxy(r Record) *ParseError {
	if !basicRecordValid(r) || !in(asString(r["protocol"]), "http", "socks") || r["network"] != "raw" ||
		!in(asString(r["security"]), "none", "tls") || !textValue(r["username"]) || !textValue(r["password"]) ||
		(r["username"] == "" && r["password"] != "") || !streamFieldsValid(r) {
		return die("неправильные параметры прокси")
	}
	if r["protocol"] == "socks" && (len(asString(r["username"])) > 255 || len(asString(r["password"])) > 255) {
		return die("логин или пароль SOCKS превышает 255 байт")
	}
	return nil
}

package servers

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// parsed holds the BRORAY_* shell variables a 3.x parser produced.
type parsed struct {
	protocol                               string
	uuid, address, port, network, security string
	encryption, flow, sni, fp, pbk, sid    string
	spx, host, path, serviceName, mode     string
	headerType, extra, name, alterID       string
	alpn                                   []any
	password, method, auth                 string
	hy2Ports, obfs, obfsPassword           string
	upMbps, downMbps, pin, finalMask       string
	// stream extensions (util.sh:167-187, 279-305)
	rawHeader, ech, pcs, vcn, pqv, streamMask string
	kcp                                       map[string]any
	kcpSeed                                   string
	kcpLegacy                                 bool
}

// jqNumber is jq `tonumber` of a decimal string.
func jqNumber(s string) any {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseKcpOptions is broray_parse_kcp_options.
func parseKcpOptions(q query, p *parsed) *ParseError {
	p.kcp, p.kcpSeed, p.kcpLegacy = map[string]any{}, "", false
	for _, key := range []string{"congestion", "readBufferSize", "writeBufferSize"} {
		if q.has(key) {
			return die("параметр mKCP " + key + " не поддерживается текущим Xray")
		}
	}
	if p.network != "kcp" {
		for _, key := range []string{"seed", "mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow"} {
			if q.has(key) {
				return die("параметр " + key + " поддерживается только для mKCP")
			}
		}
		return nil
	}
	seed, err := q.value("seed")
	if err != nil {
		return err
	}
	p.kcpSeed = seed
	if q.has("seed") || q.has("headerType") {
		p.kcpLegacy = true
	}
	for _, key := range []string{"mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow"} {
		v, err := q.value(key)
		if err != nil {
			return err
		}
		if v == "" {
			continue
		}
		if !allDigits(v) {
			return die("mKCP: параметр " + key + " должен быть целым числом")
		}
		p.kcp[key] = jqNumber(v)
	}
	return nil
}

// parseStreamExtensions is broray_parse_stream_extensions.
func parseStreamExtensions(q query, p *parsed) *ParseError {
	if err := parseKcpOptions(q, p); err != nil {
		return err
	}
	var err *ParseError
	if p.rawHeader, err = q.value("header"); err != nil {
		return err
	}
	if q.has("header") {
		if p.network != "raw" || p.rawHeader == "" {
			return silent()
		}
		for _, k := range []string{"host", "path", "headerType"} {
			if q.has(k) {
				return die("header нельзя сочетать с host/path/headerType")
			}
		}
	}
	if p.ech, err = q.value("ech"); err != nil {
		return err
	}
	if p.pcs, err = q.value("pcs"); err != nil {
		return err
	}
	if p.vcn, err = q.value("vcn"); err != nil {
		return err
	}
	if p.pqv, err = q.value("pqv"); err != nil {
		return err
	}
	p.streamMask = "{}"
	if p.network != "hysteria" {
		if p.streamMask, err = q.value("fm"); err != nil {
			return err
		}
		if p.streamMask == "" {
			p.streamMask = "{}"
		}
	}
	return nil
}

func splitNonEmpty(s string) []any {
	out := []any{}
	for _, part := range strings.Split(s, ",") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ---------- VLESS (lib/parser-vless.sh) ----------

func parseVless(uri string) (*parsed, *ParseError) {
	if uri == "" {
		return nil, die("не указана ссылка VLESS")
	}
	if !strings.HasPrefix(uri, "vless://") {
		return nil, die("поддерживаются только ссылки vless://")
	}
	body := strings.TrimPrefix(uri, "vless://")
	body, fragment, _ := splitFirst(body, '#')
	authority, rawQuery, _ := splitFirst(body, '?')
	q := query(rawQuery)
	if i := strings.IndexByte(authority, '/'); i >= 0 {
		if "/"+authority[i+1:] != "/" {
			return nil, die("путь в ссылке VLESS не поддерживается")
		}
		authority = authority[:i]
	}
	encodedID := authority
	if i := strings.LastIndexByte(authority, '@'); i >= 0 {
		encodedID = authority[:i]
	}
	hp := authority
	if i := strings.IndexByte(authority, '@'); i >= 0 {
		hp = authority[i+1:]
	}
	if encodedID == authority {
		return nil, die("не найден UUID")
	}
	p := &parsed{protocol: "vless"}
	var err *ParseError
	if p.uuid, err = uriDecode(encodedID); err != nil {
		return nil, then(err, "неправильное кодирование VLESS ID")
	}
	p.address, p.port = hostPort(hp)
	fields := []struct {
		dst  *string
		key  string
		alts []string
	}{
		{&p.network, "type", nil}, {&p.security, "security", nil}, {&p.encryption, "encryption", nil},
		{&p.flow, "flow", nil}, {&p.sni, "sni", nil}, {&p.fp, "fp", nil}, {&p.pbk, "pbk", nil},
		{&p.sid, "sid", nil}, {&p.spx, "spx", nil}, {&p.host, "host", nil},
	}
	for _, f := range fields {
		if *f.dst, err = q.value(f.key, f.alts...); err != nil {
			return nil, err
		}
	}
	authorityValue, err := q.value("authority")
	if err != nil {
		return nil, err
	}
	if p.path, err = q.value("path"); err != nil {
		return nil, err
	}
	if p.serviceName, err = q.value("serviceName", "service_name"); err != nil {
		return nil, err
	}
	if p.mode, err = q.value("mode"); err != nil {
		return nil, err
	}
	if p.headerType, err = q.value("headerType"); err != nil {
		return nil, err
	}
	if p.extra, err = q.value("extra"); err != nil {
		return nil, err
	}
	if p.name, err = uriDecode(fragment); err != nil {
		return nil, err
	}
	alpnValue, err := q.value("alpn")
	if err != nil {
		return nil, err
	}
	p.alpn = splitNonEmpty(alpnValue)
	insecure, err := q.value("allowInsecure", "insecure")
	if err != nil {
		return nil, err
	}
	switch insecure {
	case "", "0", "false", "FALSE":
	default:
		return nil, die("неподдерживаемая настройка TLS allowInsecure")
	}
	switch p.network {
	case "", "tcp", "raw":
		p.network = "raw"
	case "ws", "websocket":
		p.network = "ws"
	case "grpc", "kcp":
	case "httpupgrade", "httpUpgrade":
		p.network = "httpupgrade"
	case "xhttp", "splithttp":
		p.network = "xhttp"
	default:
		return nil, die("неподдерживаемый транспорт VLESS: " + p.network)
	}
	switch p.security {
	case "", "none":
		p.security = "none"
	case "tls", "reality":
	default:
		return nil, die("неподдерживаемая защита VLESS: " + p.security)
	}
	if p.network == "grpc" && authorityValue != "" {
		if p.host != "" && p.host != authorityValue {
			return nil, die("VLESS gRPC: host и authority различаются")
		}
		p.host = authorityValue
	}
	if p.security == "reality" && !in(p.network, "raw", "grpc", "xhttp") {
		return nil, die("VLESS REALITY не поддерживает этот транспорт")
	}
	if !in(p.flow, "", "xtls-rprx-vision", "xtls-rprx-vision-udp443") {
		return nil, die("неподдерживаемый режим VLESS flow: " + p.flow)
	}
	if !VlessEncryptionValid(orStr(p.encryption, "none")) {
		return nil, die("неподдерживаемый или некорректный VLESS encryption")
	}
	if p.flow != "" && ((p.network != "raw" && orStr(p.encryption, "none") == "none") || (p.security != "reality" && p.security != "tls")) {
		return nil, die("VLESS Vision требует TLS/REALITY и TCP/RAW либо поддерживаемого VLESS encryption")
	}
	p.fp = orStr(p.fp, "chrome")
	p.path = orStr(p.path, "/")
	if p.network == "grpc" && !in(p.mode, "", "gun", "multi") {
		return nil, die("неподдерживаемый gRPC mode")
	}
	p.mode = orStr(p.mode, "auto")
	p.headerType = orStr(p.headerType, "none")
	p.encryption = orStr(p.encryption, "none")
	p.extra = orStr(p.extra, "{}")
	p.name = orStr(p.name, p.address+":"+p.port)
	if p.security == "tls" && p.sni == "" {
		p.sni = p.address
	}
	if err := parseStreamExtensions(q, p); err != nil {
		return nil, err
	}
	return p, nil
}

func in(s string, set ...string) bool {
	for _, x := range set {
		if s == x {
			return true
		}
	}
	return false
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// VlessEncryptionValid is broray_vless_encryption_valid (util.sh:145-164):
// "none" or mlkem768x25519plus.<native|xorpub|random>.<0rtt|1rtt>[.padding…].<keys>.
func VlessEncryptionValid(v string) bool {
	if v == "none" {
		return true
	}
	p := strings.Split(v, ".")
	if len(p) < 4 || p[0] != "mlkem768x25519plus" || !in(p[1], "native", "xorpub", "random") || !in(p[2], "0rtt", "1rtt") {
		return false
	}
	key := -1
	for i := 3; i < len(p); i++ {
		if runeLen(p[i]) >= 20 {
			key = i
			break
		}
	}
	if key < 0 {
		return false
	}
	for _, k := range p[key:] {
		if n := runeLen(k); n != 43 && n != 1579 {
			return false
		}
		for _, r := range k {
			if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-' || r == '_') {
				return false
			}
		}
	}
	padding := p[3:key]
	if len(padding) == 0 {
		return true
	}
	var nums [][3]float64
	for _, pad := range padding {
		if runeLen(pad) >= 20 {
			return false
		}
		parts := strings.Split(pad, "-")
		if len(parts) != 3 {
			return false
		}
		var t [3]float64
		for i, part := range parts {
			if !allDigits(part) {
				return false
			}
			t[i], _ = strconv.ParseFloat(part, 64)
		}
		nums = append(nums, t)
	}
	for _, t := range nums {
		for _, x := range t {
			if x < 0 || x > 2147483647 {
				return false
			}
		}
		if t[1] > t[2] {
			return false
		}
	}
	if nums[0][0] < 100 || nums[0][1] < 35 || nums[0][2] < 35 {
		return false
	}
	sum := 0.0
	for i, t := range nums {
		if i%2 == 0 {
			sum += t[2]
		}
	}
	return sum <= 65553
}

func runeLen(s string) int { return len([]rune(s)) }

// ---------- VMess (lib/parser-vmess.sh) ----------

func parseVmess(uri string) (*parsed, *ParseError) {
	if uri == "" {
		return nil, die("не указана ссылка VMess")
	}
	if !strings.HasPrefix(uri, "vmess://") {
		return nil, die("поддерживаются только ссылки vmess://")
	}
	modern, _, _ := splitFirst(uri, '#')
	modern, _, _ = splitFirst(modern, '?')
	if strings.Contains(modern, "@") {
		return parseVmessModern(uri)
	}
	encoded := strings.TrimPrefix(uri, "vmess://")
	encoded, _, _ = splitFirst(encoded, '#')
	normalized := strings.NewReplacer("_", "/", "-", "+").Replace(encoded)
	switch len(normalized) % 4 {
	case 0:
	case 2:
		normalized += "=="
	case 3:
		normalized += "="
	default:
		return nil, die("VMess содержит неправильную Base64-строку")
	}
	decoded, ok := base64Decode(normalized)
	if !ok {
		return nil, die("не удалось декодировать VMess Base64")
	}
	obj, ok := decodeObject(decoded)
	if !ok {
		return nil, die("VMess Base64 не содержит корректный JSON")
	}
	// insecure / extra / TLS / FinalMask shape (parser-vmess.sh:86-94)
	shapeOK := true
	for _, k := range []string{"allowInsecure", "allow_insecure", "insecure"} {
		if v, has := obj[k]; has {
			if !(v == false || jsonEq(v, 0) || v == "0" || v == "false" || v == "") {
				shapeOK = false
			}
		}
	}
	if v, has := obj["extra"]; has {
		if _, isObj := v.(map[string]any); !isObj {
			shapeOK = false
		}
	}
	for _, k := range []string{"ech", "pcs", "vcn", "pqv"} {
		if v, has := obj[k]; has {
			if _, isStr := v.(string); !isStr {
				shapeOK = false
			}
		}
	}
	if v, has := obj["fm"]; has {
		if _, isObj := v.(map[string]any); !isObj {
			shapeOK = false
		}
	}
	if !shapeOK {
		return nil, die("VMess: неподдерживаемый insecure или неправильные extra/TLS/FinalMask")
	}
	p := &parsed{protocol: "vmess"}
	p.ech, p.pcs, p.vcn, p.pqv = jqRaw(alt(obj["ech"], "")), jqRaw(alt(obj["pcs"], "")), jqRaw(alt(obj["vcn"], "")), jqRaw(alt(obj["pqv"], ""))
	p.streamMask = compactJSON(alt(obj["fm"], map[string]any{}))
	p.name = jqRawEmpty(alt(obj["ps"], obj["remarks"], obj["name"]))
	p.address = jqRawEmpty(alt(obj["add"], obj["address"]))
	p.port = jqRawEmpty(alt(obj["port"]))
	p.uuid = jqRawEmpty(alt(obj["id"], obj["uuid"]))
	p.alterID = jqRaw(alt(obj["aid"], obj["alterId"], json.Number("0")))
	p.encryption = jqRaw(alt(obj["scy"], obj["securityCipher"], obj["cipher"], "auto"))
	netRaw := alt(obj["net"], obj["network"])
	p.network = jqRaw(alt(netRaw, "tcp"))
	p.security = jqRaw(alt(obj["tls"], obj["security"], "none"))
	p.sni = jqRawEmpty(alt(obj["sni"], obj["serverName"]))
	p.fp = jqRaw(alt(obj["fp"], obj["fingerprint"], "chrome"))
	switch a := obj["alpn"].(type) {
	case []any:
		p.alpn = a
	case string:
		p.alpn = []any{}
		if a != "" {
			for _, part := range strings.Split(a, ",") {
				part = strings.TrimFunc(part, alpnSpace)
				if part != "" {
					p.alpn = append(p.alpn, part)
				}
			}
		}
	default:
		p.alpn = []any{}
	}
	p.host = jqRawEmpty(alt(obj["host"]))
	p.path = jqRaw(alt(obj["path"], "/"))
	p.serviceName = jqRawEmpty(alt(obj["serviceName"], obj["service_name"], obj["path"]))
	if m := obj["mode"]; truthy(m) {
		p.mode = jqRaw(m)
	} else {
		typ := obj["type"]
		switch {
		case jsonEq(netRaw, "grpc"):
			p.mode = "gun"
			if typ == "multi" {
				p.mode = "multi"
			}
		case jsonEq(netRaw, "xhttp") || jsonEq(netRaw, "splithttp"):
			if typ == nil || typ == "" || typ == "none" {
				p.mode = "auto"
			} else {
				p.mode = jqRaw(typ)
			}
		default:
			p.mode = "auto"
		}
	}
	p.headerType = jqRaw(alt(obj["type"], obj["headerType"], "none"))
	p.pbk = jqRawEmpty(alt(obj["pbk"], obj["publicKey"]))
	p.sid = jqRawEmpty(alt(obj["sid"], obj["shortId"]))
	p.spx = jqRawEmpty(alt(obj["spx"], obj["spiderX"]))
	if e, ok := obj["extra"].(map[string]any); ok {
		p.extra = compactJSON(e)
	} else {
		p.extra = "{}"
	}
	if p.network == "grpc" {
		switch p.headerType {
		case "multi", "gun":
			if p.mode != p.headerType {
				return nil, die("VMess gRPC: mode и type различаются")
			}
			p.headerType = "none"
		case "none":
		default:
			return nil, die("неподдерживаемый VMess gRPC type")
		}
		if !in(p.mode, "gun", "multi") {
			return nil, die("неподдерживаемый VMess gRPC mode")
		}
	}
	switch p.network {
	case "xhttp", "splithttp":
		if !in(p.mode, "auto", "packet-up", "stream-up", "stream-one") {
			return nil, die("неподдерживаемый VMess XHTTP mode")
		}
		if !in(p.headerType, "none", "") && p.headerType != p.mode {
			return nil, die("VMess XHTTP: mode и type различаются")
		}
		p.headerType = "none"
	default:
		if p.extra != "{}" {
			return nil, die("VMess extra поддерживается только для XHTTP")
		}
	}
	p.kcp, p.kcpSeed, p.kcpLegacy = map[string]any{}, "", false
	if p.network == "kcp" {
		_, a := obj["congestion"]
		_, b := obj["readBufferSize"]
		_, c := obj["writeBufferSize"]
		if a || b || c {
			return nil, die("VMess mKCP: устаревшие параметры congestion/readBufferSize/writeBufferSize не поддерживаются текущим Xray")
		}
		seed, path := obj["seed"], obj["path"]
		_, seedStr := seed.(string)
		_, pathStr := path.(string)
		if !((seed == nil || seedStr) && (path == nil || pathStr) && (seed == nil || path == nil || seed == path)) {
			return nil, die("VMess mKCP: seed и path должны совпадать")
		}
		p.kcpLegacy = true
		p.kcpSeed = jqRaw(alt(seed, path, ""))
		for _, k := range []string{"mtu", "tti", "uplinkCapacity", "downlinkCapacity", "cwndMultiplier", "maxSendingWindow"} {
			v, has := obj[k]
			if !has {
				continue
			}
			if s, isStr := v.(string); isStr {
				n, ok := jqToNumber(s)
				if !ok {
					return nil, die("неправильные параметры VMess mKCP")
				}
				v = n
			}
			p.kcp[k] = v
		}
	}
	switch p.network {
	case "tcp":
		p.network = "raw"
	case "ws", "websocket":
		p.network = "ws"
	case "grpc":
	case "httpupgrade", "httpUpgrade":
		p.network = "httpupgrade"
	case "xhttp", "splithttp":
		p.network = "xhttp"
	case "raw", "kcp":
	default:
		return nil, die("неподдерживаемый транспорт VMess: " + p.network)
	}
	switch p.security {
	case "", "none":
		p.security = "none"
	case "tls", "reality":
	default:
		return nil, die("неподдерживаемая защита VMess: " + p.security)
	}
	if !allDigits(p.alterID) {
		return nil, die("VMess alterId должен быть числом")
	}
	if p.address == "" {
		return nil, die("VMess не содержит адрес сервера")
	}
	if p.port == "" {
		return nil, die("VMess не содержит порт")
	}
	if p.uuid == "" {
		return nil, die("VMess не содержит UUID")
	}
	p.name = orStr(p.name, p.address+":"+p.port)
	return p, nil
}

func parseVmessModern(uri string) (*parsed, *ParseError) {
	body := strings.TrimPrefix(uri, "vmess://")
	body, fragment, _ := splitFirst(body, '#')
	endpoint, rawQuery, _ := splitFirst(body, '?')
	q := query(rawQuery)
	cipher, err := q.value("encryption")
	if err != nil {
		return nil, err
	}
	if cipher == "" {
		if q.has("encryption") {
			return nil, die("VMess encryption не может быть пустым")
		}
		cipher = "auto"
	}
	if !in(cipher, "auto", "aes-128-gcm", "chacha20-poly1305", "none") {
		return nil, die("неподдерживаемый VMess encryption")
	}
	var clean []string
	for _, item := range q.items() {
		key, err := uriDecode(itemKey(item))
		if err != nil {
			return nil, err
		}
		switch key {
		case "encryption":
			continue
		case "flow", "aid", "alterId":
			return nil, die("современный VMess AEAD не поддерживает параметр " + key)
		}
		clean = append(clean, item)
	}
	p, err := parseVless("vless://" + endpoint + "?" + strings.Join(clean, "&") + "#" + fragment)
	if err != nil {
		return nil, err
	}
	p.protocol, p.alterID, p.encryption = "vmess", "0", cipher
	if p.network == "grpc" {
		mode, err := q.value("mode")
		if err != nil {
			return nil, err
		}
		p.mode = orStr(mode, "gun")
		if !in(p.mode, "gun", "multi") {
			return nil, die("неподдерживаемый VMess gRPC mode")
		}
	}
	return p, nil
}

// alpnSpace is the Unicode White_Space set of parser-vmess.sh:168-172.
func alpnSpace(r rune) bool {
	switch {
	case r == 9 || r == 10 || r == 11 || r == 12 || r == 13 || r == 32 || r == 133 || r == 160 || r == 5760:
		return true
	case r >= 8192 && r <= 8202, r == 8232, r == 8233, r == 8239, r == 8287, r == 12288:
		return true
	}
	return false
}

// ---------- Trojan (lib/parser-trojan.sh) ----------

func parseTrojan(uri string) (*parsed, *ParseError) {
	if uri == "" {
		return nil, die("не указана ссылка Trojan")
	}
	if !strings.HasPrefix(uri, "trojan://") {
		return nil, die("поддерживаются только ссылки trojan://")
	}
	const bad = "неправильное кодирование URI Trojan"
	body := strings.TrimPrefix(uri, "trojan://")
	body, fragment, _ := splitFirst(body, '#')
	endpoint, rawQuery, _ := splitFirst(body, '?')
	q := query(rawQuery)
	if i := strings.IndexByte(endpoint, '/'); i >= 0 {
		if "/"+endpoint[i+1:] != "/" {
			return nil, die("путь в ссылке Trojan не поддерживается")
		}
		endpoint = endpoint[:i]
	}
	password := endpoint
	if i := strings.LastIndexByte(endpoint, '@'); i >= 0 {
		password = endpoint[:i]
	}
	hp := endpoint
	if i := strings.IndexByte(endpoint, '@'); i >= 0 {
		hp = endpoint[i+1:]
	}
	if password == endpoint {
		return nil, die("Trojan не содержит пароль")
	}
	p := &parsed{protocol: "trojan"}
	var err *ParseError
	if p.password, err = uriDecode(password); err != nil {
		return nil, then(err, bad)
	}
	p.address, p.port = hostPort(hp)
	if strings.HasPrefix(p.address, "[") && strings.HasSuffix(p.address, "]") && len(p.address) >= 2 {
		p.address = p.address[1 : len(p.address)-1]
	}
	if p.address == "" {
		return nil, die("Trojan не содержит адрес сервера")
	}
	if p.port == "" {
		return nil, die("Trojan не содержит порт")
	}
	for _, f := range []struct {
		dst *string
		key string
	}{{&p.network, "type"}, {&p.security, "security"}, {&p.sni, "sni"}, {&p.fp, "fp"}} {
		if *f.dst, err = q.value(f.key); err != nil {
			return nil, then(err, bad)
		}
	}
	if p.host, err = q.value("host"); err != nil {
		return nil, err
	}
	authorityValue, err := q.value("authority")
	if err != nil {
		return nil, err
	}
	if p.serviceName, err = q.value("serviceName", "service_name"); err != nil {
		return nil, err
	}
	if p.headerType, err = q.value("headerType"); err != nil {
		return nil, err
	}
	p.headerType = orStr(p.headerType, "none")
	if p.mode, err = q.value("mode"); err != nil {
		return nil, then(err, bad)
	}
	if p.path, err = q.value("path"); err != nil {
		return nil, then(err, bad)
	}
	alpnText, err := q.value("alpn")
	if err != nil {
		return nil, then(err, bad)
	}
	p.alpn = []any{}
	if alpnText != "" {
		for _, part := range strings.Split(alpnText, ",") {
			part = strings.TrimFunc(part, posixSpace)
			if part != "" {
				p.alpn = append(p.alpn, part)
			}
		}
	}
	insecure, err := q.value("allowInsecure", "insecure")
	if err != nil {
		return nil, then(err, bad)
	}
	switch insecure {
	case "", "0", "false", "FALSE":
	default:
		return nil, die("неподдерживаемая настройка TLS allowInsecure")
	}
	if p.name, err = uriDecode(fragment); err != nil {
		return nil, then(err, bad)
	}
	switch p.network {
	case "", "tcp", "raw":
		p.network = "raw"
	case "kcp", "grpc":
	case "ws", "websocket":
		p.network = "ws"
	case "httpupgrade", "httpUpgrade":
		p.network = "httpupgrade"
	case "xhttp", "splithttp":
		p.network = "xhttp"
	default:
		return nil, die("неподдерживаемый транспорт Trojan: " + p.network)
	}
	if p.network == "grpc" && authorityValue != "" {
		if p.host != "" && p.host != authorityValue {
			return nil, die("Trojan gRPC: host и authority различаются")
		}
		p.host = authorityValue
	} else if authorityValue != "" {
		return nil, die("Trojan authority поддерживается только для gRPC")
	}
	switch p.headerType + ":" + p.network {
	case "http:raw", "srtp:kcp", "utp:kcp", "wechat-video:kcp", "wechat:kcp", "dtls:kcp", "wireguard:kcp", "dns:kcp":
	default:
		if p.headerType != "none" {
			return nil, die("неподдерживаемый Trojan headerType для этого транспорта")
		}
	}
	if p.pbk, err = q.value("pbk"); err != nil {
		return nil, err
	}
	if p.sid, err = q.value("sid"); err != nil {
		return nil, err
	}
	if p.spx, err = q.value("spx"); err != nil {
		return nil, err
	}
	p.security = orStr(p.security, "tls")
	switch p.security {
	case "tls":
	case "reality":
		if !in(p.network, "raw", "grpc", "xhttp") {
			return nil, die("Trojan REALITY не поддерживает этот транспорт")
		}
		if p.sni == "" || p.pbk == "" {
			return nil, die("Trojan REALITY требует SNI и public key")
		}
	default:
		return nil, die("неподдерживаемая защита Trojan")
	}
	p.sni = orStr(p.sni, p.address)
	p.fp = orStr(p.fp, "chrome")
	switch p.network {
	case "grpc":
		p.mode = orStr(p.mode, "gun")
		if !in(p.mode, "gun", "multi") {
			return nil, die("неподдерживаемый режим Trojan gRPC")
		}
	case "xhttp":
		p.mode = orStr(p.mode, "auto")
		if !in(p.mode, "auto", "packet-up", "stream-up", "stream-one") {
			return nil, die("неподдерживаемый режим Trojan XHTTP")
		}
	}
	if p.extra, err = q.value("extra"); err != nil {
		return nil, then(err, "неправильный параметр Trojan extra")
	}
	for _, item := range q.items() {
		key, err := uriDecode(itemKey(item))
		if err != nil {
			return nil, then(err, "неправильный параметр Trojan extra")
		}
		if key == "extra" && p.network != "xhttp" {
			return nil, die("Trojan extra поддерживается только для XHTTP")
		}
	}
	p.extra = orStr(p.extra, "{}")
	if v, ok := decodeJSON(p.extra); !ok || !isObject(v) {
		return nil, die("Trojan extra должен быть JSON object")
	}
	p.path = orStr(p.path, "/")
	p.name = orStr(p.name, p.address+":"+p.port)
	if p.password == "" {
		return nil, die("Trojan содержит пустой пароль")
	}
	if err := parseStreamExtensions(q, p); err != nil {
		return nil, err
	}
	return p, nil
}

// posixSpace is sed [[:space:]] in the C locale.
func posixSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r'
}

// ---------- Hysteria2 (lib/parser-hysteria2.sh) ----------

func parseHysteria2(uri string) (*parsed, *ParseError) {
	var body string
	switch {
	case strings.HasPrefix(uri, "hysteria2://"):
		body = strings.TrimPrefix(uri, "hysteria2://")
	case strings.HasPrefix(uri, "hy2://"):
		body = strings.TrimPrefix(uri, "hy2://")
	default:
		return nil, die("ссылка не является Hysteria2")
	}
	const badEnc = "неправильное кодирование URI Hysteria2"
	const badParam = "неправильный параметр URI Hysteria2"
	body, encodedName, hasName := splitFirst(body, '#')
	authority, rawQuery, _ := splitFirst(body, '?')
	q := query(rawQuery)
	if i := strings.IndexByte(authority, '/'); i >= 0 {
		path := "/" + authority[i+1:]
		authority = authority[:i]
		if path != "/" {
			return nil, die("путь в ссылке Hysteria2 не поддерживается")
		}
	}
	encodedAuth, hp := "", authority
	if strings.Contains(authority, "@") {
		encodedAuth = authority[:strings.LastIndexByte(authority, '@')]
		hp = authority[strings.LastIndexByte(authority, '@')+1:]
	}
	p := &parsed{protocol: "hysteria2"}
	var err *ParseError
	if p.auth, err = uriDecode(encodedAuth); err != nil {
		return nil, then(err, badEnc)
	}
	switch {
	case strings.HasPrefix(hp, "[") && strings.Contains(hp, "]:"):
		// \[*\]:*
		p.address = hp[:strings.IndexByte(hp, ']')]
		p.address = strings.TrimPrefix(p.address, "[")
		p.port = hp[strings.LastIndex(hp, "]:")+2:]
	case strings.HasPrefix(hp, "[") && strings.HasSuffix(hp, "]"):
		p.address = strings.TrimSuffix(strings.TrimPrefix(hp, "["), "]")
		p.port = "443"
	case strings.Contains(hp, ":"):
		p.address, p.port = hostPort(hp)
	default:
		p.address, p.port = hp, "443"
	}
	if p.address == "" {
		return nil, die("в ссылке Hysteria2 отсутствует адрес")
	}
	ports, ok := hy2Ports(p.port)
	if !ok {
		return nil, die("неправильный порт или диапазон портов Hysteria2")
	}
	if strings.Count(p.port, ",") != strings.Count(ports, ",") {
		return nil, die("неправильный список портов Hysteria2")
	}
	first, _, _ := splitFirst(ports, ',')
	first, _, _ = splitFirst(first, '-')
	p.port = first
	if strings.ContainsAny(ports, ",-") {
		p.hy2Ports = ports
	}
	if hasName && encodedName != "" {
		if p.name, err = uriDecode(encodedName); err != nil {
			return nil, then(err, badEnc)
		}
	} else {
		p.name = p.address
	}
	get := func(key, msg string) (string, *ParseError) {
		v, err := q.value(key)
		if err != nil {
			return "", then(err, msg)
		}
		return v, nil
	}
	if p.security, err = get("security", badParam); err != nil {
		return nil, err
	}
	p.security = orStr(p.security, "tls")
	if p.sni, err = get("sni", badParam); err != nil {
		return nil, err
	}
	if p.sni == "" {
		if p.sni, err = get("peer", badParam); err != nil {
			return nil, err
		}
	}
	p.sni = orStr(p.sni, p.address)
	if p.fp, err = get("fp", badParam); err != nil {
		return nil, err
	}
	p.fp = orStr(p.fp, "chrome")
	alpnValue, err := get("alpn", badParam)
	if err != nil {
		return nil, err
	}
	if alpnValue != "" {
		p.alpn = splitNonEmpty(alpnValue)
	} else {
		p.alpn = []any{"h3"}
	}
	if p.obfs, err = get("obfs", badParam); err != nil {
		return nil, err
	}
	if p.obfsPassword, err = get("obfs-password", badParam); err != nil {
		return nil, err
	}
	if p.upMbps, err = get("upmbps", badParam); err != nil {
		return nil, err
	}
	if p.downMbps, err = get("downmbps", badParam); err != nil {
		return nil, err
	}
	switch p.obfs {
	case "":
		if p.obfsPassword != "" {
			return nil, die("Hysteria2 obfs-password требует obfs=salamander/gecko")
		}
	case "salamander", "gecko":
		if p.obfsPassword == "" {
			return nil, die("Hysteria2 salamander требует obfs-password")
		}
	default:
		return nil, die("неподдерживаемый Hysteria2 obfs: " + p.obfs)
	}
	if p.upMbps != "" || p.downMbps != "" {
		return nil, die("Hysteria2 upmbps/downmbps не поддерживаются")
	}
	if p.pin, err = get("pinSHA256", "неправильный pinSHA256"); err != nil {
		return nil, err
	}
	if p.pin != "" {
		p.pin = strings.ReplaceAll(p.pin, ":", "")
		p.pin = strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'F' {
				return r + 32
			}
			return r
		}, p.pin)
		if runeLen(p.pin) != 64 {
			return nil, die("pinSHA256 должен содержать 64 hex-символа")
		}
		for _, r := range p.pin {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				return nil, die("неправильный pinSHA256")
			}
		}
	}
	insecure, err := get("insecure", badParam)
	if err != nil {
		return nil, err
	}
	if in(insecure, "1", "true", "TRUE", "yes", "YES") && p.pin == "" {
		return nil, die("Hysteria2 insecure требует pinSHA256")
	}
	fm, err := get("fm", badParam)
	if err != nil {
		return nil, err
	}
	if fm != "" {
		if v, ok := decodeJSON(fm); !ok || !isObject(v) {
			return nil, die("Hysteria2 fm должен быть JSON object")
		}
		p.finalMask = fm
	} else {
		p.finalMask = "{}"
	}
	p.network = "hysteria"
	if err := parseStreamExtensions(q, p); err != nil {
		return nil, err
	}
	return p, nil
}

// hy2Ports is the jq port-list normaliser of parser-hysteria2.sh:108-115.
func hy2Ports(s string) (string, bool) {
	items := strings.Split(s, ",")
	if len(items) == 0 || len(items) > 128 {
		return "", false
	}
	var out []string
	for _, item := range items {
		parts := strings.Split(item, "-")
		if len(parts) != 1 && len(parts) != 2 {
			continue
		}
		okAll := true
		var nums []int
		for _, part := range parts {
			if len(part) == 0 || len(part) > 5 || !allDigits(part) {
				okAll = false
				break
			}
			n, _ := strconv.Atoi(part)
			nums = append(nums, n)
		}
		if !okAll {
			continue
		}
		inRange := true
		for _, n := range nums {
			if n < 1 || n > 65535 {
				inRange = false
			}
		}
		if !inRange || (len(nums) == 2 && nums[0] > nums[1]) {
			continue
		}
		strs := make([]string, len(nums))
		for i, n := range nums {
			strs[i] = strconv.Itoa(n)
		}
		out = append(out, strings.Join(strs, "-"))
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, ","), true
}

// ---------- JSON helpers emulating jq ----------

// alt is jq `a // b // …`: the first value that is neither null nor false,
// else the last alternative (which may itself be null or false).
func alt(vals ...any) any {
	for i, v := range vals {
		if truthy(v) || i == len(vals)-1 {
			return v
		}
	}
	return nil
}

// truthy is jq truthiness (everything but null and false).
func truthy(v any) bool { return v != nil && v != false }

// jqRaw is `jq -r` output of one value.
func jqRaw(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case json.Number:
		return normNumber(string(x))
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return normNumber(strconv.FormatFloat(x, 'g', -1, 64))
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}
}

// jqRawEmpty is `jq -r '… // empty'`.
func jqRawEmpty(v any) string {
	if !truthy(v) {
		return ""
	}
	return jqRaw(v)
}

func normNumber(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e17 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', 17, 64)
}

// jqToNumber is jq `tonumber` of a string.
func jqToNumber(s string) (any, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil, false
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, false
	}
	if f == math.Trunc(f) && math.Abs(f) < 9e15 {
		return int64(f), true
	}
	return f, true
}

func compactJSON(v any) string {
	b, err := Marshal(v)
	if err != nil {
		return "null"
	}
	return strings.TrimSuffix(string(b), "\n")
}

func decodeJSON(s string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	var extra any
	if dec.Decode(&extra) == nil {
		return nil, false
	}
	return v, true
}

func decodeObject(b []byte) (map[string]any, bool) {
	v, ok := decodeJSON(string(b))
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

func isObject(v any) bool { _, ok := v.(map[string]any); return ok }

// jsonEq compares a decoded JSON value with a Go literal (numbers by value).
func jsonEq(v any, want any) bool {
	if f, ok := num(v); ok {
		if g, ok := num(want); ok {
			return f == g
		}
		return false
	}
	switch w := want.(type) {
	case string:
		s, ok := v.(string)
		return ok && s == w
	case bool:
		b, ok := v.(bool)
		return ok && b == w
	case nil:
		return v == nil
	}
	return false
}

// num returns a JSON number as float64.
func num(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

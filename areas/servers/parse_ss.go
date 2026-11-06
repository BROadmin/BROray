package servers

import (
	"encoding/base64"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BROadmin/broray/internal/compat"
)

// Marshal is compact JSON without HTML escaping (jq -c style), with "\n".
func Marshal(v any) ([]byte, error) { return compat.Marshal(v) }

func base64Decode(s string) ([]byte, bool) {
	b, err := base64.StdEncoding.DecodeString(s)
	return b, err == nil
}

// ssBase64 is broray_shadowsocks_base64_decode (strict canonical base64,
// url-safe alphabet accepted, padding optional but exact when present).
func ssBase64(value string) ([]byte, bool) {
	if value == "" {
		return nil, false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '+' || c == '/' || c == '=' || c == '-') {
			return nil, false
		}
	}
	normalized := strings.NewReplacer("_", "/", "-", "+").Replace(value)
	bare := normalized
	if i := strings.IndexByte(normalized, '='); i >= 0 {
		bare = normalized[:i]
	}
	padding := normalized[len(bare):]
	if !in(padding, "", "=", "==") {
		return nil, false
	}
	var canonical string
	switch len(bare) % 4 {
	case 0:
		canonical = bare
	case 2:
		canonical = bare + "=="
	case 3:
		canonical = bare + "="
	default:
		return nil, false
	}
	if padding != "" && normalized != canonical {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(canonical)
	if err != nil || len(b) == 0 {
		return nil, false
	}
	if base64.StdEncoding.EncodeToString(b) != canonical {
		return nil, false
	}
	return b, true
}

// ssText is broray_ss_text: valid UTF-8, ≤8192 code points, no control chars.
func ssText(b []byte) (string, bool) {
	if !utf8.Valid(b) {
		return "", false
	}
	s := string(b)
	if runeLen(s) > 8192 {
		return "", false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return "", false
		}
	}
	return s, true
}

// ssPercentText is broray_ss_percent_text.
func ssPercentText(s string) (string, bool) {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '%' {
			b = append(b, c)
			continue
		}
		if i+2 >= len(s) {
			return "", false
		}
		h, l := s[i+1], s[i+2]
		if !isHex(h) || !isHex(l) {
			return "", false
		}
		v := unhex(h)<<4 | unhex(l)
		if v < 32 || v == 127 {
			return "", false
		}
		b = append(b, v)
		i += 2
	}
	return ssText(b)
}

func ssIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, a := range parts {
		if !allDigits(a) || len(a) > 3 {
			return false
		}
		n, _ := strconv.Atoi(a)
		if n > 255 || (len(a) > 1 && a[0] == '0') {
			return false
		}
	}
	return true
}

// ssValidHost is broray_ss_valid_host (awk).
func ssValidHost(s string) bool {
	if len(s) < 1 || len(s) > 255 {
		return false
	}
	if strings.Contains(s, ":") {
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !(isHex(c) || c == ':' || c == '.') {
				return false
			}
		}
		if strings.Contains(s, ":::") {
			return false
		}
		p := strings.Index(s, "::")
		if p >= 0 && strings.Contains(s[p+2:], "::") {
			return false
		}
		a := strings.Split(s, ":")
		count := 0
		for i, g := range a {
			if g == "" {
				if p < 0 {
					return false
				}
				continue
			}
			if strings.Contains(g, ".") {
				if i != len(a)-1 || !ssIPv4(g) {
					return false
				}
				count += 2
			} else {
				if len(g) > 4 {
					return false
				}
				for j := 0; j < len(g); j++ {
					if !isHex(g[j]) {
						return false
					}
				}
				count++
			}
		}
		if p >= 0 {
			return count < 8
		}
		return count == 8
	}
	onlyDigitsDots := true
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
			onlyDigitsDots = false
		}
	}
	if onlyDigitsDots && strings.Contains(s, ".") {
		return ssIPv4(s)
	}
	s = strings.TrimSuffix(s, ".")
	for _, l := range strings.Split(s, ".") {
		if len(l) < 1 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for j := 0; j < len(l); j++ {
			c := l[j]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
				return false
			}
		}
	}
	return true
}

// parseShadowsocks is broray_parse_shadowsocks + broray_shadowsocks_uri_json.
func parseShadowsocks(uri string) (*parsed, *ParseError) {
	p, err := ssURI(uri)
	if err != nil {
		return nil, then(err, "Shadowsocks: импорт ссылки отклонён")
	}
	return p, nil
}

func ssURI(uri string) (*parsed, *ParseError) {
	if len(uri) > 16384 {
		return nil, die("Shadowsocks: ссылка слишком длинная")
	}
	if !strings.HasPrefix(uri, "ss://") {
		return nil, die("поддерживаются только ссылки ss://")
	}
	body := strings.TrimPrefix(uri, "ss://")
	// jq -R reads the body as UTF-8 text; invalid bytes become U+FFFD (allowed).
	for _, r := range strings.ToValidUTF8(body, "�") {
		if r < 32 || r == 127 {
			return nil, die("Shadowsocks: ссылка содержит управляющие символы")
		}
	}
	body, rawName, _ := splitFirst(body, '#')
	authority, rawQuery, _ := splitFirst(body, '?')
	name, ok := ssPercentText(rawName)
	if !ok {
		return nil, die("Shadowsocks: неправильное имя сервера")
	}
	for _, pair := range query(rawQuery).items() {
		key, ok := ssPercentText(itemKey(pair))
		if !ok {
			return nil, die("Shadowsocks: неправильный параметр ссылки")
		}
		if asciiOnlyLower(key) != "plugin" {
			continue
		}
		i := strings.IndexByte(pair, '=')
		if i < 0 {
			return nil, die("Shadowsocks SIP003 plugins пока не поддерживаются")
		}
		value, ok := ssPercentText(pair[i+1:])
		if !ok {
			return nil, die("Shadowsocks: неправильный параметр plugin")
		}
		if value != "" {
			return nil, die("Shadowsocks SIP003 plugins пока не поддерживаются")
		}
	}
	var credentials, hp string
	if strings.Contains(authority, "@") {
		credentials = authority[:strings.LastIndexByte(authority, '@')]
		hp = authority[strings.LastIndexByte(authority, '@')+1:]
		if strings.Contains(credentials, "@") {
			return nil, die("Shadowsocks: неоднозначный разделитель адреса")
		}
		if strings.Contains(credentials, ":") {
			if credentials, ok = ssPercentText(credentials); !ok {
				return nil, die("Shadowsocks: неправильное кодирование данных подключения")
			}
		} else {
			b, ok := ssBase64(credentials)
			if !ok {
				return nil, die("Shadowsocks: неправильная Base64-строка")
			}
			if credentials, ok = ssText(b); !ok {
				return nil, die("Shadowsocks: данные подключения не являются допустимым UTF-8")
			}
		}
	} else {
		b, ok := ssBase64(authority)
		if !ok {
			return nil, die("Shadowsocks: неправильная legacy Base64-строка")
		}
		decoded, ok := ssText(b)
		if !ok {
			return nil, die("Shadowsocks: legacy данные не являются допустимым UTF-8")
		}
		i := strings.LastIndexByte(decoded, '@')
		if i < 0 {
			return nil, die("Shadowsocks: не найден адрес сервера")
		}
		credentials, hp = decoded[:i], decoded[i+1:]
	}
	i := strings.IndexByte(credentials, ':')
	if i < 0 {
		return nil, die("Shadowsocks: не найдено method:password")
	}
	method, password := credentials[:i], credentials[i+1:]
	if password == "" {
		return nil, die("Shadowsocks: пустой пароль")
	}
	hp = strings.TrimSuffix(hp, "/")
	var address, port string
	switch {
	case strings.HasPrefix(hp, "[") && strings.Contains(hp, "]:"):
		address = strings.TrimPrefix(hp[:strings.IndexByte(hp, ']')], "[")
		port = hp[strings.Index(hp, "]:")+2:]
		if !strings.Contains(address, ":") {
			return nil, die("Shadowsocks: скобки допустимы только для IPv6")
		}
	case strings.Contains(hp, ":"):
		address, port = hostPort(hp)
		if strings.Contains(address, ":") {
			return nil, die("Shadowsocks: IPv6 должен быть в квадратных скобках")
		}
	default:
		return nil, die("Shadowsocks: не найден порт")
	}
	if !ssValidHost(address) {
		return nil, die("Shadowsocks: неправильный адрес сервера")
	}
	if !allDigits(port) {
		return nil, die("Shadowsocks: порт должен быть числом")
	}
	port = strings.TrimLeft(port, "0")
	if n, err := strconv.Atoi(port); port == "" || len(port) > 5 || err != nil || n < 1 || n > 65535 {
		return nil, die("Shadowsocks: порт вне диапазона 1–65535")
	}
	keyBytes := 0
	switch method {
	case "aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "chacha20-ietf-poly1305", "xchacha20-poly1305":
	case "2022-blake3-aes-128-gcm":
		keyBytes = 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		keyBytes = 32
	default:
		return nil, die("неподдерживаемый метод Shadowsocks")
	}
	if keyBytes > 0 {
		if method == "2022-blake3-chacha20-poly1305" && strings.Contains(password, ":") {
			return nil, die("Shadowsocks: цепочка ключей поддерживается только для AES-2022")
		}
		var keys []string
		for _, k := range strings.Split(password, ":") {
			b, ok := ssBase64(k)
			if !ok {
				return nil, die("Shadowsocks 2022: неправильное Base64-кодирование ключа")
			}
			if len(b) != keyBytes {
				return nil, die("Shadowsocks 2022: неправильная длина ключа")
			}
			keys = append(keys, base64.StdEncoding.EncodeToString(b))
		}
		password = strings.Join(keys, ":")
	}
	name = orStr(name, address+":"+port)
	return &parsed{protocol: "shadowsocks", method: method, password: password, address: address, port: port,
		name: name, network: "raw", security: "none"}, nil
}

// asciiOnlyLower is `tr 'A-Z' 'a-z'` (ASCII only).
func asciiOnlyLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

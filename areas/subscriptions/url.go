package subscriptions

import (
	"net/netip"
	"strconv"
	"strings"
)

// maskURL is broray_subscription_mask_url (displayUrl).
func maskURL(u string) string {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "***"
	}
	scheme, rest, _ := strings.Cut(u, "://")
	rest, _, _ = strings.Cut(rest, "#")
	authority, path := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority, path = rest[:i], rest[i:]
	}
	if i := strings.IndexByte(authority, '@'); i >= 0 {
		authority = "***@" + authority[i+1:]
	}
	hasQuery := false
	if i := strings.IndexByte(path, '?'); i >= 0 {
		hasQuery, path = true, path[:i]
	}
	last := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		last = path[i+1:]
	}
	secret := false
	for _, p := range []string{"/sub/", "/subscription/", "/subscriptions/", "/subscribe/", "/link/", "/s/"} {
		if strings.HasPrefix(path, p) {
			secret = true
		}
	}
	if !secret && runeLen(last) >= 12 {
		secret = true
	}
	if secret && last != "" {
		path = path[:strings.LastIndexByte(path, '/')] + "/***"
	}
	out := scheme + "://" + authority + path
	if hasQuery {
		out += "?***"
	}
	return out
}

// parsedURL is the result of broray_subscription_parse_url.
type parsedURL struct {
	Scheme, Host, Port, Authority string
}

// parseURL is broray_subscription_parse_url (syntax only, no DNS).
func parseURL(u string) (parsedURL, *subErr) {
	var p parsedURL
	switch {
	case strings.HasPrefix(u, "http://"):
		p.Scheme = "http"
	case strings.HasPrefix(u, "https://"):
		p.Scheme = "https"
	default:
		return p, fail("INVALID_URL", "URL подписки должен использовать HTTP или HTTPS.")
	}
	if strings.ContainsAny(u, " \t\n\v\f\r") {
		return p, fail("INVALID_URL", "URL подписки содержит пробельные символы.")
	}
	if strings.ContainsAny(u, "\"'`\\") {
		return p, fail("INVALID_URL", "URL подписки содержит недопустимые символы.")
	}
	_, rest, _ := strings.Cut(u, "://")
	authority, _, _ := strings.Cut(rest, "/")
	authority, _, _ = strings.Cut(authority, "?")
	authority, _, _ = strings.Cut(authority, "#")
	if authority == "" {
		return p, fail("INVALID_URL", "В URL подписки отсутствует адрес сервера.")
	}
	if strings.Contains(authority, "@") {
		return p, fail("INVALID_URL", "Данные пользователя в URL подписки запрещены.")
	}
	p.Authority = authority
	switch {
	case strings.HasPrefix(authority, "[") && strings.Contains(authority[1:], "]"):
		// sed 's/^\[\([^]]*\)\].*/\1/p' and 's/^\[[^]]*\]:\([0-9][0-9]*\)$/\1/p'
		end := strings.IndexByte(authority, ']')
		p.Host = authority[1:end]
		tail := authority[end+1:]
		if strings.HasPrefix(tail, ":") && allDigits(tail[1:]) {
			p.Port = tail[1:]
		}
	case strings.Contains(authority, ":"):
		p.Host = authority[:strings.IndexByte(authority, ':')]
		p.Port = authority[strings.LastIndexByte(authority, ':')+1:]
	default:
		p.Host = authority
	}
	if p.Host == "" {
		return p, fail("INVALID_URL", "В URL подписки отсутствует имя хоста.")
	}
	p.Host = asciiLower(p.Host)
	h := p.Host
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") ||
		strings.HasSuffix(h, ".home") || h == "metadata" || h == "metadata.google.internal" {
		return p, fail("DOWNLOAD_SECURITY", "Локальные и служебные адреса запрещены.")
	}
	if p.Port == "" {
		p.Port = map[string]string{"https": "443", "http": "80"}[p.Scheme]
	}
	if !allDigits(p.Port) {
		return p, fail("INVALID_URL", "Порт в URL подписки имеет неправильный формат.")
	}
	if n, err := strconv.Atoi(p.Port); err != nil || n < 1 || n > 65535 {
		return p, fail("INVALID_URL", "Порт в URL подписки находится вне допустимого диапазона.")
	}
	return p, nil
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// ipPublic is broray_subscription_ip_is_public on a parsed address:
// IPv4 outside the special ranges, IPv6 only in 2000::/3 minus 2001:db8::/32.
func ipPublic(a netip.Addr) bool {
	if !a.IsValid() || a.Zone() != "" {
		return false
	}
	if a.Is4() {
		b := a.As4()
		switch {
		case b[0] == 0, b[0] == 10, b[0] == 127, b[0] >= 224:
			return false
		case b[0] == 169 && b[1] == 254, b[0] == 192 && b[1] == 168:
			return false
		case b[0] == 100 && b[1] >= 64 && b[1] <= 127:
			return false
		case b[0] == 172 && b[1] >= 16 && b[1] <= 31:
			return false
		case b[0] == 198 && (b[1] == 18 || b[1] == 19):
			return false
		case b[0] == 192 && b[1] == 0 && (b[2] == 0 || b[2] == 2):
			return false
		case b[0] == 198 && b[1] == 51 && b[2] == 100, b[0] == 203 && b[1] == 0 && b[2] == 113:
			return false
		}
		return true
	}
	if a.Is4In6() {
		return false
	}
	b := a.As16()
	if b[0]&0xE0 != 0x20 {
		return false
	}
	return !(b[0] == 0x20 && b[1] == 0x01 && b[2] == 0x0d && b[3] == 0xb8)
}

// literalIP recognises the literal forms of broray_subscription_resolve_public_ip:
// a host with ':' is an IPv6 literal, a host of digits and dots an IPv4
// literal (strict dotted quad). ok=false → resolve by name.
func literalIP(host string) (netip.Addr, bool) {
	if strings.Contains(host, ":") {
		if strings.ContainsAny(host, ".%") {
			return netip.Addr{}, true
		}
		a, err := netip.ParseAddr(host)
		if err != nil {
			return netip.Addr{}, true
		}
		return a, true
	}
	for i := 0; i < len(host); i++ {
		if !(host[i] >= '0' && host[i] <= '9' || host[i] == '.') {
			return netip.Addr{}, false
		}
	}
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return netip.Addr{}, true
	}
	for _, p := range parts {
		if p == "" || len(p) > 1 && p[0] == '0' {
			return netip.Addr{}, true
		}
		if n, err := strconv.Atoi(p); err != nil || n > 255 {
			return netip.Addr{}, true
		}
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, true
	}
	return a, true
}

// resolveRedirect is broray_subscription_resolve_redirect.
func resolveRedirect(base, location string) (string, *subErr) {
	switch {
	case strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://"):
		return location, nil
	case strings.HasPrefix(location, "?"):
		b, _, _ := strings.Cut(base, "?")
		return b + location, nil
	case strings.HasPrefix(location, "//"):
		scheme, _, _ := strings.Cut(base, "://")
		return scheme + ":" + location, nil
	case strings.HasPrefix(location, "/"):
		p, err := parseURL(base)
		if err != nil {
			return "", err
		}
		return p.Scheme + "://" + p.Authority + location, nil
	}
	prefix, _, _ := strings.Cut(base, "?")
	if i := strings.LastIndexByte(prefix, '/'); i >= 0 {
		prefix = prefix[:i]
	}
	return prefix + "/" + location, nil
}

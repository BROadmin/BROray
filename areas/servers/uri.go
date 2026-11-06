package servers

import (
	"strings"
)

// ParseError is a share-link rejection. Lines are the stderr lines 3.x
// printed (each "Ошибка: …"); a silent 3.x failure has no lines.
type ParseError struct{ Lines []string }

func (e *ParseError) Error() string {
	if len(e.Lines) == 0 {
		return "Конфигурация не прошла проверку."
	}
	return strings.Join(e.Lines, "\n")
}

// Text is the 3.x stderr text (empty for a silent failure).
func (e *ParseError) Text() string { return strings.Join(e.Lines, "\n") }

// die is broray_die: one "Ошибка: <text>" line.
func die(text string) *ParseError { return &ParseError{Lines: []string{"Ошибка: " + text}} }

// silent is a 3.x `return 1` without any message.
func silent() *ParseError { return &ParseError{} }

// then appends a broray_die line after an earlier failure's lines
// (`x="$(f)" || broray_die "…"`).
func then(err *ParseError, text string) *ParseError {
	lines := append(append([]string(nil), err.Lines...), "Ошибка: "+text)
	return &ParseError{Lines: lines}
}

// uriDecode is broray_uri_component_decode (util.sh:45-83): only %XX is
// decoded ('+' stays), %00 and a trailing LF (raw or %0A) are rejected.
func uriDecode(s string) (string, *ParseError) {
	if strings.HasSuffix(s, "\n") {
		return "", die("завершающий перевод строки в URI не поддерживается")
	}
	rest := s
	for {
		i := strings.IndexByte(rest, '%')
		if i < 0 {
			break
		}
		rest = rest[i+1:]
		switch {
		case strings.HasPrefix(rest, "00") || rest == "0a" || rest == "0A":
			return "", die("NUL или завершающий перевод строки в URI не поддерживается")
		case len(rest) >= 2 && isHex(rest[0]) && isHex(rest[1]):
			rest = rest[2:]
		default:
			return "", die("некорректное percent-кодирование URI")
		}
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// query is a raw `&`-separated query string as 3.x walked it.
type query string

func (q query) items() []string {
	var out []string
	rest := string(q)
	for rest != "" {
		i := strings.IndexByte(rest, '&')
		if i < 0 {
			out = append(out, rest)
			break
		}
		out = append(out, rest[:i])
		rest = rest[i+1:]
	}
	return out
}

func itemKey(item string) string {
	if i := strings.IndexByte(item, '='); i >= 0 {
		return item[:i]
	}
	return item
}

// value is broray_uri_query_value: every key is decoded (a bad key fails the
// call), a repeated key or alias is an error, the value is decoded.
func (q query) value(wanted string, aliases ...string) (string, *ParseError) {
	found := false
	result := ""
	for _, item := range q.items() {
		key, err := uriDecode(itemKey(item))
		if err != nil {
			return "", err
		}
		for _, alias := range append([]string{wanted}, aliases...) {
			if key != alias {
				continue
			}
			if found {
				return "", die("повторяющийся параметр URI: " + wanted)
			}
			found = true
			raw := ""
			if i := strings.IndexByte(item, '='); i >= 0 {
				raw = item[i+1:]
			}
			v, err := uriDecode(raw)
			if err != nil {
				return "", err
			}
			result = v
			break
		}
	}
	return result, nil
}

// has is broray_uri_query_has (a key decode failure counts as absent).
func (q query) has(wanted string) bool {
	for _, item := range q.items() {
		key, err := uriDecode(itemKey(item))
		if err != nil {
			return false
		}
		if key == wanted {
			return true
		}
	}
	return false
}

// splitFragment / splitQuery implement the `${x#*#}` / `${x%%#*}` pairs.
func splitFirst(s string, sep byte) (before, after string, ok bool) {
	if i := strings.IndexByte(s, sep); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

// hostPort is `${hp%:*}` / `${hp##*:}` (last colon; no colon → both = hp).
func hostPort(hp string) (string, string) {
	if i := strings.LastIndexByte(hp, ':'); i >= 0 {
		return hp[:i], hp[i+1:]
	}
	return hp, hp
}

package subscriptions

import (
	"bytes"
	"encoding/base64"
	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/strictjson"
	"regexp"
	"strings"
)

// Payload detection: broray_subscription_extract_nodes (svc:857-936).

var (
	reURILine  = regexp.MustCompile(`^[ \t\n\v\f\r]*(vless|vmess|trojan|ss|hysteria2|hy2|tuic|socks|socks5|http|https|vpn)://`)
	reJSONLine = regexp.MustCompile(`^[ \t\n\v\f\r]*(\{|\[)`)
	reHTML     = regexp.MustCompile(`(?i)<!doctype[ \t\n\v\f\r]+html|<html[ \t\n\v\f\r>]`)
)

const spaceChars = " \t\n\v\f\r"

// normalizeText strips a leading UTF-8 BOM and every CR, then splits lines
// the way line-oriented tools see the file (a final line without LF counts).
func normalizeText(b []byte) []string {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	b = bytes.ReplaceAll(b, []byte{'\r'}, nil)
	if len(b) == 0 {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func anyMatch(re *regexp.Regexp, lines []string) bool {
	for _, l := range lines {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

func trimLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.Trim(l, spaceChars)
	}
	return out
}

// decodeBase64Body is broray_subscription_decode_base64.
func decodeBase64Body(lines []string) ([]byte, bool) {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t', ' ':
			return -1
		case '_':
			return '/'
		case '-':
			return '+'
		}
		return r
	}, strings.Join(lines, "\n"))
	switch len(compact) % 4 {
	case 2:
		compact += "=="
	case 3:
		compact += "="
	}
	out, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return nil, false
	}
	return out, true
}

// extracted is the parse input: node lines and whether they came from Xray JSON.
type extracted struct {
	Clash        bool
	NodeWarnings map[int][]string
	Lines        []string
	JSONImport   bool
	Native       map[int]nativeJSONInput
}

// extractNodes recognises the payload: URI list, Base64 URI list or Xray
// JSON (converted to links). Provider metadata lines of the body are
// collected into md and removed.
func extractNodes(body []byte, md *metadata) (*extracted, *subErr) {
	// A WireGuard INI is one node, even though its payload has many lines.
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if strings.HasPrefix(trimmed, "[Interface]") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		if r, err := servers.ParseLink(trimmed, "wg-detect", nil); err == nil && r["protocol"] == "wireguard" {
			return &extracted{Lines: []string{trimmed}}, nil
		}
	}
	lines := normalizeText(body)
	if ex, recognized, err := structuredSubscription(lines); recognized {
		return ex, err
	}
	md.apply(metaRecordsFromLines(lines))
	lines = stripMetadata(lines)
	if anyMatch(reHTML, lines) {
		return nil, fail("UNSUPPORTED_CONTENT", "Сервер вернул HTML вместо подписки.")
	}
	ex := &extracted{}
	var out []string
	if anyMatch(reURILine, lines) {
		out = trimLines(lines)
	} else {
		input := lines
		if !anyMatch(reJSONLine, lines) {
			if decoded, ok := decodeBase64Body(lines); ok {
				input = normalizeText(decoded)
				if ex, recognized, err := structuredSubscription(input); recognized {
					return ex, err
				}
				md.apply(metaRecordsFromLines(input))
				input = stripMetadata(input)
			}
		}
		if anyMatch(reURILine, input) {
			out = trimLines(input)
		} else {
			text := strings.Join(input, "\n")
			ex.Native = map[int]nativeJSONInput{}
			conv, ok := xrayJSONToInputs([]byte(text), maxNodes, ex.Native)
			if !ok {
				if cx, recognized, err := clashExtract([]byte(text)); recognized {
					return cx, err
				}
				return nil, fail("PARSE_ERROR", "Содержимое подписки не удалось распознать.")
			}
			out, ex.JSONImport = conv, true
		}
	}
	for _, l := range out {
		t := strings.TrimLeft(l, spaceChars)
		if t == "" || !ex.JSONImport && strings.HasPrefix(t, "#") {
			continue
		}
		ex.Lines = append(ex.Lines, l)
	}
	if len(ex.Lines) == 0 {
		return nil, fail("NO_VALID_NODES", "Подписка не содержит узлов.")
	}
	if len(ex.Lines) > maxNodes {
		return nil, fail("CONTENT_TOO_LARGE", "Подписка содержит слишком много узлов.")
	}
	return ex, nil
}

// Structured formats must see the exact normalized document before legacy
// metadata/comment stripping or URI-looking lines inside YAML scalar values.
func structuredSubscription(lines []string) (*extracted, bool, *subErr) {
	text := []byte(strings.Join(lines, "\n"))
	trimmed := bytes.TrimSpace(text)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		native := map[int]nativeJSONInput{}
		if conv, ok := xrayJSONToInputs(text, maxNodes, native); ok {
			if len(conv) == 0 {
				return nil, true, fail("NO_VALID_NODES", "Подписка не содержит узлов.")
			}
			if len(conv) > maxNodes {
				return nil, true, fail("CONTENT_TOO_LARGE", "Подписка содержит слишком много узлов.")
			}
			return &extracted{Lines: conv, JSONImport: true, Native: native}, true, nil
		}
		// A JSON export may wrap explicit share links rather than profiles.
		// Keep the document's provenance while using the same strict parser.
		if document, err := strictjson.Decode(text); err == nil {
			if items, ok := document.([]any); ok && len(items) > 0 {
				if len(items) > maxNodes {
					return nil, true, errTooLarge
				}
				links := make([]string, 0, len(items))
				for _, item := range items {
					link, ok := item.(string)
					if !ok || !reURILine.MatchString(link) || strings.ContainsAny(link, "\r\n") {
						return nil, false, nil
					}
					links = append(links, link)
				}
				if len(links) > maxNodes {
					return nil, true, errTooLarge
				}
				return &extracted{Lines: links, JSONImport: true}, true, nil
			}
		}
	}
	return clashExtract(text)
}

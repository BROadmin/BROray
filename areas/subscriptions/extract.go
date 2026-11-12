package subscriptions

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/BROadmin/broray/internal/amnezia"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/internal/xrayimport"
)

var (
	reURILine = regexp.MustCompile(`^[ \t\n\v\f\r]*(vless|vmess|trojan|ss|hysteria2|hy2|tuic|socks|socks5|http|https|vpn)://`)
	reHTML    = regexp.MustCompile(`(?i)<!doctype[ \t\n\v\f\r]+html|<html[ \t\n\v\f\r>]`)
)

const spaceChars = " \t\n\v\f\r"

// Normalize line endings only. A literal CR inside JSON string data must not
// disappear before the strict document decoder sees it.
func normalizeText(b []byte) []string {
	b = xrayimport.StripBOM(b)
	b = bytes.ReplaceAll(b, []byte{'\r', '\n'}, []byte{'\n'})
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
	for _, line := range lines {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func trimLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.Trim(line, spaceChars)
	}
	return out
}

func decodeBase64Body(lines []string) ([]byte, bool) {
	return xrayimport.DecodeBase64([]byte(strings.Join(lines, "\n")))
}

type extracted struct {
	Clash            bool
	NodeWarnings     map[int][]string
	Lines            []string
	JSONImport       bool
	Native           map[int]nativeJSONInput
	ClientFields     map[int]map[string]any
	UnavailableClash map[int]map[string]any
	UnavailableXray  map[int]map[string]any
	Amnezia          map[int]amnezia.Imported
}

func extractNodes(body []byte, md *metadata) (*extracted, *subErr) {
	return extractPayload(body, md, true)
}

func extractPayload(body []byte, md *metadata, allowBase64 bool) (*extracted, *subErr) {
	if bytes.HasPrefix(body, []byte("\ufeff\ufeff")) {
		return nil, fail("PARSE_ERROR", "Содержимое подписки не удалось распознать.")
	}
	// Recognize INI framing before validation. A damaged private document must
	// remain one node, rather than falling through to URI-list extraction.
	for _, line := range strings.Split(string(xrayimport.StripBOM(body)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if line == "[Interface]" {
			return &extracted{Lines: []string{string(body)}}, nil
		}
		break
	}
	// Preserve exact JSON bytes until strict decoding; in particular do not
	// strip comments, metadata, or embedded carriage returns first.
	original := strings.Split(string(body), "\n")
	if ex, recognized, err := structuredSubscription(original); recognized {
		return ex, err
	}
	lines := normalizeText(body)
	md.apply(metaRecordsFromLines(lines))
	lines = stripMetadata(lines)
	if anyMatch(reHTML, lines) {
		return nil, fail("UNSUPPORTED_CONTENT", "Сервер вернул HTML вместо подписки.")
	}
	// Metadata can precede a structured document, but cannot change its grammar.
	if ex, recognized, err := structuredSubscription(lines); recognized {
		return ex, err
	}
	if !anyMatch(reURILine, lines) {
		if allowBase64 {
			if decoded, ok := decodeBase64Body(lines); ok {
				return extractPayload(decoded, md, false)
			}
		}
		return nil, fail("PARSE_ERROR", "Содержимое подписки не удалось распознать.")
	}
	ex := &extracted{Amnezia: map[int]amnezia.Imported{}}
	for _, line := range trimLines(lines) {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "vpn://") {
			profiles, err := amnezia.ParseLinks(line, maxNodes-len(ex.Lines))
			if err == amnezia.ErrTooManyContainers {
				return nil, errTooLarge
			}
			if err == nil && len(profiles) > 1 {
				for _, profile := range profiles {
					ex.Amnezia[len(ex.Lines)] = profile
					ex.Lines = append(ex.Lines, "")
				}
				continue
			}
		}
		ex.Lines = append(ex.Lines, line)
	}
	if len(ex.Lines) == 0 {
		return nil, fail("NO_VALID_NODES", "Подписка не содержит узлов.")
	}
	if len(ex.Lines) > maxNodes {
		return nil, errTooLarge
	}
	return ex, nil
}

// Once a JSON document is recognized, malformed syntax/shape is terminal.
// Never scan a failed JSON document for URI-looking lines and import a subset.
func structuredSubscription(lines []string) (*extracted, bool, *subErr) {
	text := []byte(strings.Join(lines, "\n"))
	if xrayimport.LooksJSON(text) {
		document, err := strictjson.Decode(xrayimport.StripBOM(text))
		if err != nil {
			return nil, true, fail("PARSE_ERROR", "Содержимое подписки не удалось распознать.")
		}
		// Explicit Clash JSON retains its existing schema adapter. Arbitrary
		// objects do not gain a permissive YAML fallback after a JSON failure.
		if m, ok := document.(map[string]any); ok {
			if has(m, "containers") {
				profiles, _, err := amnezia.ParseDocument(text, maxNodes)
				if err == amnezia.ErrTooManyContainers {
					return nil, true, errTooLarge
				}
				if err != nil {
					return nil, true, fail("PARSE_ERROR", "Неправильная клиентская конфигурация AmneziaWG.")
				}
				ex := &extracted{Amnezia: map[int]amnezia.Imported{}, Lines: make([]string, len(profiles))}
				for i, profile := range profiles {
					ex.Amnezia[i] = profile
				}
				return ex, true, nil
			}
			if has(m, "proxies") || has(m, "type") {
				if ex, recognized, err := clashExtract(text); recognized {
					return ex, true, err
				}
			}
		}
		ex, ok := xrayJSONExtract(text, maxNodes)
		if !ok {
			return nil, true, fail("PARSE_ERROR", "Содержимое подписки не удалось распознать.")
		}
		if len(ex.Lines) == 0 {
			return nil, true, fail("NO_VALID_NODES", "Подписка не содержит узлов.")
		}
		if len(ex.Lines) > maxNodes {
			return nil, true, errTooLarge
		}
		return ex, true, nil
	}
	return clashExtract(text)
}

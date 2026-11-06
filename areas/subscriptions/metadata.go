package subscriptions

import (
	"encoding/base64"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Provider information (lib/subscription-metadata.jq, subscription-http.sh:51-72).

var knownMeta = map[string]bool{
	"profile-title": true, "subscription-userinfo": true, "profile-update-interval": true, "support-url": true,
	"profile-web-page-url": true, "announce": true, "announce-url": true, "routing": true, "routing-enable": true,
	"update-always": true, "custom-tunnel-config": true,
}

type metaRecord struct{ name, value string }

// metaRecordsFromLines is the awk filter of broray_subscription_metadata_collect.
func metaRecordsFromLines(lines []string) []metaRecord {
	var out []metaRecord
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		line = strings.TrimLeft(line, " \t")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimLeft(line, " \t")
		colon := strings.IndexByte(line, ':')
		if colon < 1 {
			continue
		}
		name := strings.ToLower(line[:colon])
		if !knownMeta[name] {
			continue
		}
		value := line[colon+1:]
		if runeLen(value) > 4097 {
			value = string([]rune(value)[:4097])
		}
		out = append(out, metaRecord{name, value})
	}
	return out
}

// stripMetadata is broray_subscription_strip_metadata.
func stripMetadata(lines []string) []string {
	var out []string
	for _, line := range lines {
		t := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(t, "#") {
			continue
		}
		name, _, _ := strings.Cut(t, ":")
		if knownMeta[strings.ToLower(name)] {
			continue
		}
		out = append(out, line)
	}
	return out
}

var errMeta = errors.New("invalid-metadata")

func metaTrim(s string) string { return strings.Trim(s, " \t\r") }

// clean is `type=="string" and length<=n and no control chars, DEL or U+FFFD`.
func metaClean(s string, n int) bool {
	if runeLen(s) > n || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 || r == 0xFFFD {
			return false
		}
	}
	return true
}

func b64Char(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/'
}

// decodeText is decode_text($n).
func decodeText(v string, n int) (string, error) {
	if !metaClean(v, 4096) {
		return "", errMeta
	}
	if strings.HasPrefix(v, "base64:") {
		enc := strings.NewReplacer("-", "+", "_", "/").Replace(v[7:])
		head, _, _ := strings.Cut(enc, "=")
		pad := len(enc) - len(head)
		if len(head) == 0 || len(head)%4 == 1 || pad > 2 {
			return "", errMeta
		}
		for i := 0; i < len(head); i++ {
			if !b64Char(head[i]) {
				return "", errMeta
			}
		}
		if pad > 0 && (len(enc)%4 != 0 || enc != head+strings.Repeat("=", pad)) {
			return "", errMeta
		}
		padded := head + strings.Repeat("=", (4-len(head)%4)%4)
		raw, err := base64.StdEncoding.DecodeString(padded)
		if err != nil {
			return "", errMeta
		}
		if strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=") != head {
			return "", errMeta
		}
		v = string(raw)
	}
	if !metaClean(v, n) || v == "" {
		return "", errMeta
	}
	return v, nil
}

func metaInteger(v string, max int64) (int64, error) {
	v = metaTrim(v)
	if v == "" || len(v) > 16 || !allDigits(v) {
		return 0, errMeta
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n > max {
		return 0, errMeta
	}
	return n, nil
}

func metaLink(v string) (string, error) {
	v, err := decodeText(v, 2048)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
		return "", errMeta
	}
	_, rest, _ := strings.Cut(v, "://")
	authority, _, _ := strings.Cut(rest, "/")
	authority, _, _ = strings.Cut(authority, "?")
	authority, _, _ = strings.Cut(authority, "#")
	if authority == "" || strings.Contains(authority, "@") {
		return "", errMeta
	}
	for _, r := range v {
		if r <= 32 || r == 127 || r == '"' || r == '\'' || r == '<' || r == '>' || r == '\\' {
			return "", errMeta
		}
	}
	return v, nil
}

func metaUsage(v string) (map[string]any, error) {
	v, err := decodeText(v, 512)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	count := 0
	for _, part := range strings.Split(v, ";") {
		part = metaTrim(part)
		if part == "" {
			continue
		}
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			return nil, errMeta
		}
		key, val := metaTrim(kv[0]), metaTrim(kv[1])
		switch key {
		case "upload", "download", "total", "expire":
		default:
			return nil, errMeta
		}
		if _, dup := out[key]; dup {
			return nil, errMeta
		}
		n, err := metaInteger(val, 9007199254740991)
		if err != nil {
			return nil, err
		}
		out[key] = n
		count++
	}
	if count == 0 || count > 4 {
		return nil, errMeta
	}
	return out, nil
}

func metaParse(key, v string) (map[string]any, error) {
	switch key {
	case "profile-title":
		t, err := decodeText(v, 128)
		return map[string]any{"title": t}, err
	case "announce":
		t, err := decodeText(v, 1024)
		return map[string]any{"announcement": t}, err
	case "support-url":
		t, err := metaLink(v)
		return map[string]any{"supportUrl": t}, err
	case "profile-web-page-url":
		t, err := metaLink(v)
		return map[string]any{"webPageUrl": t}, err
	case "announce-url":
		t, err := metaLink(v)
		return map[string]any{"announcementUrl": t}, err
	case "subscription-userinfo":
		u, err := metaUsage(v)
		return map[string]any{"usage": u}, err
	case "profile-update-interval":
		t, err := decodeText(v, 16)
		if err != nil {
			return nil, err
		}
		h, err := metaInteger(t, 168)
		if err != nil || h < 1 {
			return nil, errMeta
		}
		return map[string]any{"suggestedUpdateMinutes": h * 60}, nil
	}
	return map[string]any{"ignoredDirectives": []string{key}}, nil
}

// metadata is the provider-metadata.json object being collected during one fetch/parse.
type metadata struct{ m map[string]any }

func newMetadata() *metadata { return &metadata{m: map[string]any{"schemaVersion": 1}} }

// apply merges one record group set (subscription-metadata.jq).
func (md *metadata) apply(records []metaRecord) {
	groups := map[string][]string{}
	var names []string
	for _, r := range records {
		if !knownMeta[r.name] {
			continue
		}
		if _, ok := groups[r.name]; !ok {
			names = append(names, r.name)
		}
		groups[r.name] = append(groups[r.name], metaTrim(r.value))
	}
	sort.Strings(names)
	for _, key := range names {
		values := uniqueSorted(groups[key])
		var fields map[string]any
		var err error
		if len(values) != 1 {
			err = errMeta
		} else {
			fields, err = metaParse(key, values[0])
		}
		if err != nil {
			fields = map[string]any{"invalidFields": []string{key}}
		}
		invalid := append(strList(md.m["invalidFields"]), strList(fields["invalidFields"])...)
		ignored := append(strList(md.m["ignoredDirectives"]), strList(fields["ignoredDirectives"])...)
		for k, v := range fields {
			md.m[k] = v
		}
		md.m["invalidFields"] = uniqueSorted(invalid)
		md.m["ignoredDirectives"] = uniqueSorted(ignored)
	}
	md.m["schemaVersion"] = 1
}

func (md *metadata) object() map[string]any { return clone(md.m) }

package subscriptions

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ---------- decoded JSON helpers (jq semantics) ----------

// decodeAll decodes every JSON document of b (jq -s). ok=false on a syntax error.
func decodeAll(b []byte) ([]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out []any
	for {
		var v any
		err := dec.Decode(&v)
		if err == io.EOF {
			return out, true
		}
		if err != nil {
			return nil, false
		}
		out = append(out, v)
	}
}

// decodeObject decodes exactly one JSON object.
func decodeObject(b []byte) (map[string]any, bool) {
	docs, ok := decodeAll(b)
	if !ok || len(docs) != 1 {
		return nil, false
	}
	m, ok := docs[0].(map[string]any)
	return m, ok && m != nil
}

// num returns a JSON number as float64.
func num(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(x), 64)
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

// isInt is `type=="number" and .==floor and .>=lo and .<=hi`.
func isInt(v any, lo, hi float64) bool {
	f, ok := num(v)
	return ok && f == math.Floor(f) && f >= lo && f <= hi
}

func intOf(v any) (int64, bool) {
	f, ok := num(v)
	if !ok || f != math.Floor(f) || math.Abs(f) > 1<<53 {
		return 0, false
	}
	return int64(f), true
}

// truthy is jq truthiness (only null and false are false).
func truthy(v any) bool { return v != nil && v != false }

// alt is jq `a // b // …`.
func alt(vs ...any) any {
	for _, v := range vs[:len(vs)-1] {
		if truthy(v) {
			return v
		}
	}
	return vs[len(vs)-1]
}

// jqRaw is `jq -r` output of one value (without the trailing newline).
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
	case json.Number, float64, int, int64:
		f, _ := num(x)
		if f == math.Trunc(f) && math.Abs(f) < 1e17 {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'g', 17, 64)
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}
}

// shellValue is "$(jq -r …)": command substitution drops trailing newlines.
func shellValue(v any) string { return strings.TrimRight(jqRaw(v), "\n") }

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// printableASCII is `length<=n and all(explode[]; .>=32 and .<=126)`.
func printableASCII(s string, n int) bool {
	if runeLen(s) > n {
		return false
	}
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
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

func clone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func uniqueSorted(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func strList(v any) []string {
	var out []string
	switch x := v.(type) {
	case []string:
		return append(out, x...)
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// cutBytes is `cut -c 1-n` (bytes) kept on a UTF-8 boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func itoa(n int) string { return strconv.Itoa(n) }

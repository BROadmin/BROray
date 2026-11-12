package subscriptions

import (
	"math"
	"strconv"
	"strings"
)

type jqFail struct{ field string }

func isDecimal(s string) bool { return allDigits(s) }

func need(ok bool) {
	if !ok {
		panic(jqFail{})
	}
}

func obj(v any) map[string]any {
	m, ok := v.(map[string]any)
	need(ok && m != nil)
	return m
}

func has(m map[string]any, k string) bool { _, ok := m[k]; return ok }

// keysOnly is `type=="object" and ((keys - $keys)|length)==0`.

func keysOnly(v any, keys ...string) bool {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return false
	}
	for k := range m {
		found := false
		for _, a := range keys {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			panic(jqFail{field: safeJSONField(k)})
		}
	}
	return true
}

// textValue is `type=="string" and length<=4096 and all(explode[]; .>=32 and .!=127)`.

func textValue(v any) bool {
	s, ok := v.(string)
	if !ok || runeLen(s) > 4096 {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func stringField(m map[string]any, key, def string) string {
	if v, ok := m[key]; ok {
		need(textValue(v))
		return v.(string)
	}
	return def
}

func objectField(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		return obj(v)
	}
	return map[string]any{}
}

// uriEscape is jq @uri (unreserved A-Za-z0-9-_.~ kept, %XX uppercase).

func uriEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)|0x100, 16)[1:]))
		}
	}
	return b.String()
}

func numText(v any) string {
	f, _ := num(v)
	if f == math.Trunc(f) && math.Abs(f) < 1e17 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', 17, 64)
}

func inList(v string, list ...string) bool {
	for _, x := range list {
		if v == x {
			return true
		}
	}
	return false
}

func jsonEqual(a, b any) bool {
	if fa, ok := num(a); ok {
		fb, ok := num(b)
		return ok && fa == fb
	}
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !jsonEqual(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// rangePair is range_pair.

func rangePair(v any) [2]float64 {
	if _, isNum := num(v); isNum {
		need(isInt(v, 0, 2147483647))
		f, _ := num(v)
		return [2]float64{f, f}
	}
	s, ok := v.(string)
	need(ok)
	if s == "" {
		return [2]float64{}
	}
	parts := strings.Split(s, "-")
	need(len(parts) == 1 || len(parts) == 2)
	var vals []float64
	for _, p := range parts {
		need(isDecimal(p) && len(p) <= 10)
		f, _ := strconv.ParseFloat(p, 64)
		need(f <= 2147483647)
		vals = append(vals, f)
	}
	if len(vals) == 1 {
		return [2]float64{vals[0], vals[0]}
	}
	need(vals[0] <= vals[1])
	return [2]float64{vals[0], vals[1]}
}

func without(m map[string]any, keys ...string) map[string]any {
	out := clone(m)
	for _, k := range keys {
		delete(out, k)
	}
	return out
}

func safeJSONField(s string) string {
	if len(s) == 0 || len(s) > 64 {
		return "unknown"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return "unknown"
		}
	}
	return s
}

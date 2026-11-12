package xrayimport

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"
)

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

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// printableASCII is `length<=n and all(explode[]; .>=32 and .<=126)`.

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


func clone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}


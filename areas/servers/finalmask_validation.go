package servers

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// validateFreshFinalMasks is an import-only, side-effect-free schema check.
// Stored-record Validate intentionally retains the older compatibility contract.
// Values are never rewritten: range aliases, zero, false and source spelling
// remain available to Xray exactly as imported.
func validateFreshFinalMasks(r Record) *ParseError {
	for _, block := range []string{"transport", "hysteria"} {
		if has(r[block], "finalMask") {
			if e := freshMask(get(r, block, "finalMask"), block+".finalMask", 0); e != nil {
				return e
			}
		}
	}
	for _, block := range []string{"transport", "xhttp"} {
		if extra, ok := get(r, block, "extra").(map[string]any); ok {
			if e := freshDownloadMasks(extra, block+".extra", 0); e != nil {
				return e
			}
		}
	}
	if r["network"] == "xdrive" && r["security"] == "tls" &&
		jsonValueEqual(get(r, "tls", "alpn"), []any{"h3"}) {
		congestion := strings.ToLower(asString(get(r, "transport", "finalMask", "quicParams", "congestion")))
		if in(congestion, "brutal", "force-brutal") {
			return nativeError("transport.finalMask.quicParams.congestion")
		}
	}
	return nil
}

func freshDownloadMasks(extra map[string]any, path string, depth int) *ParseError {
	if depth > 64 {
		return nativeError(path)
	}
	if !has(extra, "downloadSettings") {
		return nil
	}
	d, ok := extra["downloadSettings"].(map[string]any)
	if !ok || d == nil {
		return nativeError(path + ".downloadSettings")
	}
	path += ".downloadSettings"
	if has(d, "finalmask") {
		if e := freshMask(d["finalmask"], path+".finalmask", depth+1); e != nil {
			return e
		}
	}
	for _, key := range []string{"xhttpSettings", "splithttpSettings"} {
		if x, ok := d[key].(map[string]any); ok {
			if e := freshDownloadMasks(x, path+"."+key, depth+1); e != nil {
				return e
			}
			if nested, ok := x["extra"].(map[string]any); ok {
				if e := freshDownloadMasks(nested, path+"."+key+".extra", depth+1); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

type freshField func(any, string, int) *ParseError

func freshObject(v any, path string, depth int, fields map[string]freshField) (map[string]any, *ParseError) {
	m, ok := v.(map[string]any)
	if !ok || m == nil || depth > 64 {
		return nil, nativeError(path)
	}
	for _, key := range sortedKeys(m) {
		check, known := fields[key]
		if !known || m[key] == nil {
			return nil, nativeError(path + "." + key)
		}
		if e := check(m[key], path+"."+key, depth+1); e != nil {
			return nil, e
		}
	}
	return m, nil
}

func freshFields(names string, check freshField) map[string]freshField {
	out := map[string]freshField{}
	for _, name := range strings.Fields(names) {
		out[name] = check
	}
	return out
}

func freshAdd(fields map[string]freshField, names string, check freshField) {
	for _, name := range strings.Fields(names) {
		fields[name] = check
	}
}

func freshString(v any, path string, _ int) *ParseError {
	if _, ok := v.(string); !ok {
		return nativeError(path)
	}
	return nil
}

func freshBool(v any, path string, _ int) *ParseError {
	if _, ok := v.(bool); !ok {
		return nativeError(path)
	}
	return nil
}

func freshArray(check freshField) freshField {
	return func(v any, path string, depth int) *ParseError {
		a, ok := v.([]any)
		if !ok || a == nil || depth > 64 {
			return nativeError(path)
		}
		for i, item := range a {
			p := path + "." + strconv.Itoa(i)
			if item == nil {
				return nativeError(p)
			}
			if e := check(item, p, depth+1); e != nil {
				return e
			}
		}
		return nil
	}
}

func freshSigned(v any, bits int) (int64, bool) {
	b, e := json.Marshal(v)
	if e != nil {
		return 0, false
	}
	n, e := strconv.ParseInt(string(b), 10, bits)
	return n, e == nil
}

func freshUnsigned(v any, bits int) (uint64, bool) {
	b, e := json.Marshal(v)
	if e != nil {
		return 0, false
	}
	n, e := strconv.ParseUint(string(b), 10, bits)
	return n, e == nil
}

func freshInt(bits int, signed bool) freshField {
	return func(v any, path string, _ int) *ParseError {
		var ok bool
		if signed {
			_, ok = freshSigned(v, bits)
		} else {
			_, ok = freshUnsigned(v, bits)
		}
		if !ok {
			return nativeError(path)
		}
		return nil
	}
}

var freshRangePattern = regexp.MustCompile(`^([+-]?[0-9]+)(?:-([+-]?[0-9]+))?$`)

// ParseRangeString itself preserves order; only Int32Range sorts its bounds.
func freshRangePair(text string, bits int) (int64, int64, bool) {
	if text == "" {
		return 0, 0, true
	}
	m := freshRangePattern.FindStringSubmatch(text)
	if m == nil {
		return 0, 0, false
	}
	a, e := strconv.ParseInt(m[1], 10, bits)
	if m[2] == "" {
		return a, a, e == nil
	}
	b, f := strconv.ParseInt(m[2], 10, bits)
	return a, b, e == nil && f == nil
}

func freshRange(v any) (int64, int64, bool) {
	if text, ok := v.(string); ok {
		a, b, valid := freshRangePair(text, 32)
		if !valid {
			return 0, 0, false
		}
		if a > b {
			a, b = b, a
		}
		return a, b, true
	}
	n, ok := freshSigned(v, 32)
	return n, n, ok
}

func freshRangeField(v any, path string, _ int) *ParseError {
	if _, _, ok := freshRange(v); !ok {
		return nativeError(path)
	}
	return nil
}

func freshDefaultRange(m map[string]any, key string) (int64, int64) {
	if !has(m, key) {
		return 0, 0
	}
	a, b, _ := freshRange(m[key])
	return a, b
}

func freshBandwidth(v any) (uint64, bool) {
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, true
	}
	i := len(s)
	for at, c := range s {
		if (c < '0' || c > '9') && c != '.' {
			i = at
			break
		}
	}
	n, e := strconv.ParseFloat(s[:i], 64)
	units := map[string]float64{"": 1, "b": 1, "bps": 1, "k": 1024, "kb": 1024, "kbps": 1024, "m": 1048576, "mb": 1048576, "mbps": 1048576, "g": 1073741824, "gb": 1073741824, "gbps": 1073741824, "t": 1099511627776, "tb": 1099511627776, "tbps": 1099511627776}
	mul, known := units[strings.TrimSpace(s[i:])]
	if e != nil || !known || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n*mul >= math.Exp2(64) {
		return 0, false
	}
	return uint64(n*mul) / 8, true
}

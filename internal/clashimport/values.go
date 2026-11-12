package clashimport

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

type fieldFailure struct{ field string }

func need(ok bool) {
	if !ok {
		panic(fieldFailure{})
	}
}
func obj(v any) map[string]any {
	m, ok := v.(map[string]any)
	need(ok && m != nil)
	return m
}
func has(m map[string]any, k string) bool { _, ok := m[k]; return ok }
func inList(v string, list ...string) bool {
	for _, item := range list {
		if v == item {
			return true
		}
	}
	return false
}
func keysOnly(m map[string]any, keys ...string) bool {
	for k := range m {
		if !inList(k, keys...) {
			panic(fieldFailure{field: safeField(k)})
		}
	}
	return true
}
func textValue(v any) bool {
	s, ok := v.(string)
	if !ok || utf8.RuneCountInString(s) > 4096 {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
func stringField(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		need(textValue(v))
		return v.(string)
	}
	return fallback
}
func objectField(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		return obj(v)
	}
	return map[string]any{}
}
func isInt(v any, lo, hi float64) bool {
	var n float64
	switch x := v.(type) {
	case json.Number:
		var err error
		n, err = strconv.ParseFloat(string(x), 64)
		if err != nil {
			return false
		}
	case float64:
		n = x
	case int:
		n = float64(x)
	default:
		return false
	}
	return n == math.Floor(n) && n >= lo && n <= hi
}
func without(m map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if !inList(k, keys...) {
			out[k] = v
		}
	}
	return out
}
func safeField(s string) string {
	if len(s) == 0 || len(s) > 96 {
		return "parameters"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return "parameters"
		}
	}
	return s
}

func mappedFields(source map[string]any, names map[string]string) map[string]any {
	out := map[string]any{}
	for sourceKey, targetKey := range names {
		if value, exists := source[sourceKey]; exists {
			out[targetKey] = value
		}
	}
	return out
}

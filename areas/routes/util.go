package routes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

// obj is a decoded JSON object (numbers as json.Number). State files keep
// unknown fields, exactly like the 3.x jq rewrites did.
type obj = map[string]any

// nowFn is the clock (tests may override it).
var nowFn = time.Now

func stamp() string { return compat.Stamp(nowFn()) }

func readObj(path string) (obj, bool) {
	b, err := platform.ReadRegularNoFollow(path)
	if err != nil {
		return nil, false
	}
	return decodeObjBytes(b)
}

func decodeObjBytes(b []byte) (obj, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m obj
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

func writeObj(path string, v any) error {
	if err := platform.MkdirAllDurable(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := compat.Marshal(v)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomicNoFollow(path, b, 0o600)
}

// writeLight writes a small file atomically without fsync (progress counters).
func writeLight(path string, data []byte) error {
	return platform.WriteFileAtomicLightNoFollow(path, data, 0o600)
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// listBytes is the 3.x list-file form: one line per entry, trailing newline.
func listBytes(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

func sortedUnique(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	j := 0
	for i, s := range out {
		if i > 0 && s == out[j-1] {
			continue
		}
		out[j] = s
		j++
	}
	return out[:j]
}

func str(m obj, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

func sub(m obj, k string) obj {
	if m == nil {
		return nil
	}
	o, _ := m[k].(obj)
	return o
}

func isNull(m obj, k string) bool {
	if m == nil {
		return true
	}
	return m[k] == nil
}

func numOf(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
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

func intOf(v any) (int, bool) {
	f, ok := numOf(v)
	if !ok || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}

func intOr(v any, def int) int {
	if n, ok := intOf(v); ok {
		return n
	}
	return def
}

func boolOf(v any) bool { b, _ := v.(bool); return b }

func strList(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func anyList(v any) []any {
	arr, _ := v.([]any)
	return arr
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// clone deep-copies a JSON value through encoding (keeps json.Number).
func clone(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	_ = dec.Decode(&out)
	return out
}

func cloneObj(m obj) obj {
	if m == nil {
		return nil
	}
	o, _ := clone(m).(obj)
	return o
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func regularFile(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode().IsRegular()
}

func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func itoa(n int) string { return strconv.Itoa(n) }

// copyFile copies src to dst atomically.
func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return platform.WriteFileAtomic(dst, b, 0o600)
}

func validBundleID(id string) bool {
	if id == "" || len(id) >= 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validUserID(id string) bool {
	if !strings.HasPrefix(id, "user-") || len(id) > 63 || len(id) == len("user-") {
		return false
	}
	return validBundleID(id)
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func decodeAny(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

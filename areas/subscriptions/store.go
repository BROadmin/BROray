package subscriptions

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

const (
	maxNodes       = 500
	minInterval    = 5
	maxInterval    = 10080
	maxBodyBytes   = 2097152
	maxHeaderBytes = 65536
)

// now is the clock (tests replace it).
var now = time.Now

// ---------- errors (BRORAY_ERROR:<CODE>:<message>) ----------

// subErr is a service error of subscription-service.sh.
type subErr struct{ Code, Msg string }

func (e *subErr) Error() string { return e.Code + ": " + e.Msg }

func fail(code, msg string) *subErr { return &subErr{code, msg} }

var (
	errNotFound    = fail("SUBSCRIPTION_NOT_FOUND", "Подписка не найдена.")
	errIDMissing   = fail("INVALID_SUBSCRIPTION_ID", "Не указан идентификатор подписки.")
	errIDChars     = fail("INVALID_SUBSCRIPTION_ID", "Идентификатор подписки содержит недопустимые символы.")
	errPersistence = fail("PERSISTENCE_ERROR", "Данные подписки не прошли проверку.")
)

// ---------- paths (3.x /opt/broray/... → Deps.Path / RunPath) ----------

func (s *svc) subDir() string               { return s.d.Path("config", "subscriptions") }
func (s *svc) recPath(id string) string     { return filepath.Join(s.subDir(), id+".json") }
func (s *svc) disabledRoot() string         { return s.d.Path("config", "disabled-subscription-servers") }
func (s *svc) disabledDir(id string) string { return filepath.Join(s.disabledRoot(), id) }
func (s *svc) runDir() string               { return s.d.RunPath("subscriptions") }
func (s *svc) logFile() string              { return filepath.Join(s.d.Root.Logs(), "subscriptions.log") }

// validID is broray_subscription_validate_id.
func validID(id string) *subErr {
	if id == "" {
		return errIDMissing
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return errIDChars
		}
	}
	return nil
}

// regular reports a regular file that is not a symlink.
func regular(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.Mode().IsRegular()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// readRecord reads config/subscriptions/<id>.json (object) and its bytes.
func (s *svc) readRecord(id string) (map[string]any, []byte, error) {
	p := s.recPath(id)
	if !regular(p) {
		return nil, nil, os.ErrNotExist
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, err
	}
	m, ok := decodeObject(b)
	if !ok {
		return nil, b, errors.New("subscription record is not a JSON object")
	}
	return m, b, nil
}

// recordHash is `sha256sum <record>` (queue context).
func (s *svc) recordHash(id string) (string, bool) {
	p := s.recPath(id)
	if !regular(p) {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return compat.SHA256Hex(b), true
}

// recordIDs lists config/subscriptions/*.json stems in glob order.
func (s *svc) recordIDs() []string {
	entries, err := os.ReadDir(s.subDir())
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") || !strings.HasSuffix(n, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(n, ".json"))
	}
	sort.Strings(ids)
	return ids
}

// validRecord is broray_subscription_validate_file.
func validRecord(m map[string]any) bool {
	if m == nil || !isInt(m["schemaVersion"], 1, 1) {
		return false
	}
	for _, key := range []string{"fetchJSON", "fetchClash"} {
		if v, exists := m[key]; exists {
			if _, ok := v.(bool); !ok {
				return false
			}
		}
	}
	if value, exists := m["alternateExports"]; exists {
		formats, ok := value.([]any)
		if !ok || len(formats) > 2 {
			return false
		}
		seen := map[string]bool{}
		for _, value := range formats {
			format, ok := value.(string)
			if !ok || (format != "json" && format != "clash") || seen[format] {
				return false
			}
			seen[format] = true
		}
	}
	if v, ok := m["sendDeviceInfo"]; ok {
		if _, isBool := v.(bool); !isBool {
			return false
		}
	}
	if v, ok := m["httpUserAgent"]; ok {
		ua, isStr := v.(string)
		if !isStr || !printableASCII(ua, 128) {
			return false
		}
	}
	if id, ok := m["id"].(string); !ok || id == "" {
		return false
	}
	if name, ok := m["name"].(string); !ok || name == "" || runeLen(name) > 128 {
		return false
	}
	if u, ok := m["url"].(string); !ok || u == "" || runeLen(u) > 4096 {
		return false
	}
	if v, ok := m["clientHwid"]; ok {
		h, isStr := v.(string)
		if !isStr || !hwidValid(h) {
			return false
		}
	}
	if _, ok := m["enabled"].(bool); !ok {
		return false
	}
	if _, ok := m["autoUpdateEnabled"].(bool); !ok {
		return false
	}
	if !isInt(m["updateIntervalMinutes"], minInterval, maxInterval) {
		return false
	}
	switch m["lastUpdateStatus"] {
	case "never", "running", "success", "partial", "error":
	default:
		return false
	}
	if f, ok := num(m["serversReceived"]); !ok || f < 0 {
		return false
	}
	if _, ok := m["createdAt"].(string); !ok {
		return false
	}
	if _, ok := m["updatedAt"].(string); !ok {
		return false
	}
	return true
}

// writeRecord is broray_subscription_write_json (jq -S, validate, 0600, atomic).
func (s *svc) writeRecord(m map[string]any) error {
	id, _ := m["id"].(string)
	if validID(id) != nil {
		return fail("PERSISTENCE_ERROR", "Не удалось сохранить подписку.")
	}
	b, err := compat.Marshal(m)
	if err != nil {
		return fail("PERSISTENCE_ERROR", "Не удалось подготовить данные подписки.")
	}
	check, ok := decodeObject(b)
	if !ok || !validRecord(check) {
		return errPersistence
	}
	if err := os.MkdirAll(s.subDir(), 0o700); err != nil {
		return fail("PERSISTENCE_ERROR", "Не удалось сохранить подписку.")
	}
	if err := platform.WriteFileAtomic(s.recPath(id), b, 0o600); err != nil {
		return fail("PERSISTENCE_ERROR", "Не удалось сохранить подписку.")
	}
	return nil
}

// ---------- ids ----------

func (s *svc) newID() string {
	for {
		id := "sub-" + compat.RandHex(8)
		if !exists(s.recPath(id)) {
			return id
		}
	}
}

func newHwid() string { return "broray-" + compat.RandHex(16) }

// hwidValid is broray_subscription_client_hwid_valid.
func hwidValid(h string) bool {
	if len(h) < 10 || len(h) > 64 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '=' || c == '-') {
			return false
		}
	}
	return true
}

// ---------- schedule ----------

// schedule is broray_subscription_schedule_values: next run (epoch, ISO) or nil.
func schedule(enabled, auto bool, interval int64, base time.Time) (any, any) {
	if !enabled || !auto {
		return nil, nil
	}
	next := base.Unix() + interval*60
	return next, compat.Stamp(time.Unix(next, 0))
}

func setSchedule(m map[string]any, base time.Time) {
	interval, _ := intOf(m["updateIntervalMinutes"])
	epoch, at := schedule(m["enabled"] == true, m["autoUpdateEnabled"] == true, interval, base)
	m["nextUpdateEpoch"], m["nextUpdateAt"] = epoch, at
}

// ---------- log (logs/subscriptions.log) ----------

func (s *svc) logf(line string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	p := s.logFile()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if st, err := os.Stat(p); err == nil && st.Size() > 262144 {
		if b, err := os.ReadFile(p); err == nil {
			lines := strings.SplitAfter(string(b), "\n")
			if n := len(lines); n > 0 && lines[n-1] == "" {
				lines = lines[:n-1]
			}
			if len(lines) > 600 {
				lines = lines[len(lines)-600:]
			}
			_ = platform.WriteFileAtomic(p, []byte(strings.Join(lines, "")), 0o600)
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(compat.Now() + " " + line + "\n")
}

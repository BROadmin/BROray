package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
)

// Settings backup part "servers" (compat.BackupPart): the manual server
// records of servers/ (subscription servers travel with their subscription
// in the "subscriptions" part), the active server id and the auto-switch /
// quality-schedule settings (config/system/server-auto-switch.json).
// Quality results, connection state and Xray runtime files are not saved.
//
// Import merges by id: a record of the backup is added or replaces the
// record with the same id, records missing from the backup stay. It stages
// after the subscriptions part, so the active-server check sees the final
// server set. The active server is re-activated through ReapplyActive after
// the swap.

const backupMaxServers = 4096

type serversBackup struct {
	Records        []json.RawMessage `json:"records"`
	ActiveServerID *string           `json:"activeServerId"`
	AutoSwitch     json.RawMessage   `json:"autoSwitch"`
}

func registerBackup(s *svc) {
	compat.RegisterBackupPart(compat.BackupPart{
		Name: "servers", Order: 20,
		Export: func(ctx context.Context) (any, error) { return s.backupExport() },
		Stage:  s.backupStage,
		Before: s.backupBefore,
		After:  s.backupAfter,
	})
}

func (s *svc) backupExport() (any, error) {
	recs, err := ListRecords(s.d)
	if err != nil {
		return nil, err
	}
	records := make([]any, 0, len(recs))
	for _, r := range recs {
		if !IsSubscriptionRecord(r) {
			records = append(records, r)
		}
	}
	var active any
	if id := ActiveID(s.d); id != "" && RecordExists(s.d, id) {
		active = id
	}
	var auto any
	if m, ok := readObjectFile(autoSwitchFile(s.d)); ok {
		auto = m
	}
	return map[string]any{"records": records, "activeServerId": active, "autoSwitch": auto}, nil
}

// BackupIDValid is the id rule of an imported server: the 3.x character set
// and no leading dot (".", ".." and hidden files are never record stems).
func BackupIDValid(id string) bool {
	return ValidID(id) && id[0] != '.' && len(id) <= 128
}

// IsSubscriptionRecord reports a server that belongs to a subscription.
func IsSubscriptionRecord(rec Record) bool {
	src, _ := rec["source"].(map[string]any)
	return src != nil && src["type"] == "subscription"
}

func backupInvalid(format string, args ...any) error {
	return fmt.Errorf("BACKUP_INVALID: "+format, args...)
}

func recordName(rec Record) string {
	if n, ok := rec["name"].(string); ok && strings.TrimSpace(n) != "" {
		return n
	}
	return asString(rec["id"])
}

func sameRecord(a, b Record) bool {
	x, err1 := compat.Canonical(normalize(a))
	y, err2 := compat.Canonical(normalize(b))
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// DecodeBackupRecord decodes and validates one server record of a backup
// (shared with the subscriptions part for disabled subscription servers).
func DecodeBackupRecord(raw json.RawMessage, label string) (Record, error) {
	rec, ok := decodeObject(raw)
	if !ok {
		return nil, backupInvalid("запись сервера %s не является объектом JSON", label)
	}
	id, _ := rec["id"].(string)
	if !BackupIDValid(id) {
		return nil, backupInvalid("некорректный идентификатор сервера %q", id)
	}
	if err := Validate(rec, "резервная копия: "+label+"/"+id); err != nil {
		msg := strings.TrimSpace(strings.ReplaceAll(err.Error(), "\n", " "))
		return nil, backupInvalid("сервер «%s» не прошёл проверку: %s", recordName(rec), msg)
	}
	return normalize(rec), nil
}

func (s *svc) backupStage(ctx context.Context, raw json.RawMessage, st *compat.BackupStage) (compat.BackupPlan, error) {
	var plan compat.BackupPlan
	var in serversBackup
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return plan, backupInvalid("раздел серверов повреждён")
		}
	}
	if len(in.Records) > backupMaxServers {
		return plan, backupInvalid("слишком много серверов (%d)", len(in.Records))
	}
	seen := map[string]bool{}
	var changed []Record
	for _, r := range in.Records {
		rec, err := DecodeBackupRecord(r, "servers")
		if err != nil {
			return plan, err
		}
		id := rec["id"].(string)
		if IsSubscriptionRecord(rec) {
			return plan, backupInvalid("сервер %s принадлежит подписке и восстанавливается только вместе с ней", id)
		}
		if seen[id] {
			return plan, backupInvalid("сервер %s указан дважды", id)
		}
		seen[id] = true
		cur, err := ReadRecord(s.d, id)
		if err == nil && IsSubscriptionRecord(cur) {
			return plan, fmt.Errorf("BACKUP_CONFLICT: идентификатор %s занят сервером подписки", id)
		}
		// the subscriptions part stages first: a server it restores is not
		// live yet, and a manual record must not overwrite it
		if staged, serr := ReadRecord(st.St, id); serr == nil && IsSubscriptionRecord(staged) {
			return plan, fmt.Errorf("BACKUP_CONFLICT: идентификатор %s занят сервером восстанавливаемой подписки", id)
		}
		switch {
		case errors.Is(err, ErrNotFound):
			plan.Add("server", id, recordName(rec), "add", "")
			changed = append(changed, rec)
		case err != nil:
			return plan, fmt.Errorf("BACKUP_STAGE_FAILED: не удалось прочитать сервер %s", id)
		case sameRecord(cur, rec):
			plan.Add("server", id, recordName(rec), "unchanged", "")
		default:
			plan.Add("server", id, recordName(rec), "replace", "")
			changed = append(changed, rec)
		}
	}
	ids, _ := RecordIDs(s.d)
	for _, id := range ids {
		if !seen[id] {
			if rec, err := ReadRecord(s.d, id); err == nil && !IsSubscriptionRecord(rec) {
				plan.Add("server", id, recordName(rec), "keep", "")
			}
		}
	}
	if len(changed) > 0 {
		if err := st.Take(compat.DataRel("servers")); err != nil {
			return plan, err
		}
		for _, rec := range changed {
			if err := WriteRecord(st.St, rec); err != nil {
				return plan, fmt.Errorf("BACKUP_STAGE_FAILED: сервер %s не записан: %v", asString(rec["id"]), err)
			}
		}
	} else if err := st.Copy(compat.DataRel("servers")); err != nil {
		return plan, err
	}
	exists := func(id string) bool { return RecordExists(st.St, id) }
	finalActive := ActiveID(s.d)
	if in.ActiveServerID != nil && *in.ActiveServerID != "" {
		id := *in.ActiveServerID
		if !BackupIDValid(id) || !exists(id) {
			return plan, backupInvalid("активный сервер %q отсутствует в копии", id)
		}
		finalActive = id
		if ActiveID(s.d) != id {
			rec, _ := ReadRecord(st.St, id)
			plan.Add("setting", "activeServerId", "Активный сервер", "replace", recordName(rec))
			if err := st.Take(compat.DataRel("config", "active-server")); err != nil {
				return plan, err
			}
			if err := setActiveID(st.St, id); err != nil {
				return plan, err
			}
		}
	}
	if finalActive != "" && !exists(finalActive) {
		// a restored subscription no longer has the server that is active now
		rec, _ := ReadRecord(s.d, finalActive)
		return plan, fmt.Errorf("ACTIVE_SERVER_CONFLICT: восстановление удалит активный сервер «%s». Сначала подключите другой сервер или отключите текущий на странице «Серверы».", recordName(rec))
	}
	if len(in.AutoSwitch) > 0 && string(in.AutoSwitch) != "null" {
		cfg, err := s.backupAutoSwitch(in.AutoSwitch, exists)
		if err != nil {
			return plan, err
		}
		cur, _ := readObjectFile(autoSwitchFile(s.d))
		if !sameSettings(cur, cfg) {
			plan.Add("setting", "autoSwitch", "Автовыбор сервера и плановая проверка качества", "replace", "")
			if err := st.Take(compat.DataRel("config", "system", "server-auto-switch.json")); err != nil {
				return plan, err
			}
			if err := os.MkdirAll(st.St.Path("config", "system"), 0o700); err != nil {
				return plan, err
			}
			if err := compat.WriteJSON(autoSwitchFile(st.St), cfg); err != nil {
				return plan, err
			}
		}
	}
	return plan, nil
}

// sameSettings compares two settings objects ignoring updatedAt.
func sameSettings(a, b map[string]any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	strip := func(m map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			if k != "updatedAt" {
				out[k] = v
			}
		}
		return out
	}
	x, err1 := compat.Canonical(strip(a))
	y, err2 := compat.Canonical(strip(b))
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// backupAutoSwitch validates server-auto-switch.json from a backup with the
// ranges of auto-switch-save.cgi and quality-refresh-save.cgi.
func (s *svc) backupAutoSwitch(raw json.RawMessage, exists func(string) bool) (map[string]any, error) {
	m, ok := decodeObject(raw)
	bad := func(what string) error {
		return backupInvalid("настройки автовыбора сервера некорректны: %s", what)
	}
	if !ok {
		return nil, bad("не объект")
	}
	enabled, ok := m["enabled"].(bool)
	if !ok {
		return nil, bad("enabled")
	}
	intIn := func(k string, lo, hi float64) (int64, bool) {
		f, ok := num(m[k])
		return int64(f), ok && f == float64(int64(f)) && f >= lo && f <= hi
	}
	th, ok := intIn("failureThreshold", 1, 10)
	if !ok {
		return nil, bad("failureThreshold")
	}
	cd, ok := intIn("cooldownMinutes", 1, 120)
	if !ok {
		return nil, bad("cooldownMinutes")
	}
	minimum, _ := m["minimumRating"].(string)
	if !in(minimum, "excellent", "good", "acceptable", "poor") {
		return nil, bad("minimumRating")
	}
	rule, _ := m["selectionRule"].(string)
	if !in(rule, "best-quality", "lowest-ping", "preferred", "manual") {
		return nil, bad("selectionRule")
	}
	var pref any
	switch p := m["preferredServerId"].(type) {
	case nil:
	case string:
		if p != "" {
			if !BackupIDValid(p) || !exists(p) {
				return nil, bad("предпочтительный сервер отсутствует")
			}
			pref = p
		}
	default:
		return nil, bad("preferredServerId")
	}
	if rule == "preferred" && pref == nil {
		return nil, bad("не выбран предпочтительный сервер")
	}
	qr := false
	if v, present := m["qualityRefreshEnabled"]; present {
		b, ok := v.(bool)
		if !ok {
			return nil, bad("qualityRefreshEnabled")
		}
		qr = b
	}
	iv := int64(60)
	if _, present := m["qualityRefreshIntervalMinutes"]; present {
		if !in(jqRaw(m["qualityRefreshIntervalMinutes"]), "30", "60", "180", "360") {
			return nil, bad("qualityRefreshIntervalMinutes")
		}
		iv = intervalOf(m)
	}
	return map[string]any{"schemaVersion": 3, "enabled": enabled, "failureThreshold": th, "cooldownMinutes": cd,
		"minimumRating": minimum, "selectionRule": rule, "preferredServerId": pref, "qualityRefreshEnabled": qr,
		"qualityRefreshIntervalMinutes": iv, "updatedAt": compat.Stamp(s.now())}, nil
}

type backupLive struct {
	active, recHash string
	pointer         []byte
	hadPointer      bool
}

func (s *svc) backupBefore() any {
	var b backupLive
	b.active = ActiveID(s.d)
	if p, err := os.ReadFile(ActiveFile(s.d)); err == nil {
		b.pointer, b.hadPointer = p, true
	}
	if b.active != "" {
		b.recHash, _ = fileSHA256(RecordPath(s.d, b.active))
	}
	return b
}

// backupAfter re-activates the active server when the import changed the
// pointer or the active record (Xray config + Keenetic interface through the
// normal ReapplyActive path). If that fails, the previous pointer is put
// back so the pointer keeps naming the server Xray actually runs.
func (s *svc) backupAfter(ctx context.Context, before any) compat.BackupPlan {
	var out compat.BackupPlan
	b, _ := before.(backupLive)
	id := ActiveID(s.d)
	if id == "" {
		return out
	}
	h, _ := fileSHA256(RecordPath(s.d, id))
	if id == b.active && h == b.recHash {
		return out
	}
	rec, _ := ReadRecord(s.d, id)
	name := recordName(rec)
	if err := s.ReapplyActive(ctx, "backup-import"); err != nil {
		msg := cgiMessage(err)
		if id != b.active {
			if b.hadPointer {
				_ = os.WriteFile(ActiveFile(s.d), b.pointer, 0o600)
			} else {
				_ = os.Remove(ActiveFile(s.d))
			}
		}
		s.logf("SERVER_BACKUP_REAPPLY=FAIL SERVER=%s ERROR=%v", id, err)
		out.FollowUps = append(out.FollowUps, "Сервер «"+name+"» восстановлен, но не подключён ("+msg+"). Подключите его на странице «Серверы».")
		return out
	}
	out.Notes = append(out.Notes, "Активный сервер «"+name+"» подключён.")
	return out
}

func cgiMessage(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 && strings.ToUpper(msg[:i]) == msg[:i] {
		msg = msg[i+2:]
	}
	return strings.TrimSuffix(strings.TrimSpace(msg), ".")
}

package servers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// Content revisions also detect writes from existing automation/older clients.
// The caller compares and writes while holding the existing global fence.
func autoSwitchRevision(config any) string {
	b, _ := json.Marshal(config)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *svc) handleAutoSwitchEnabled(c *cgi.Req) (any, error) {
	return s.handleServerSettingEnabled(c, "enabled")
}

func (s *svc) handleServerSettingEnabled(c *cgi.Req, field string) (any, error) {
	release, ok := s.d.Lock.TryAcquire("servers", "servers:auto-switch-enabled", "")
	if !ok {
		return nil, errBusy
	}
	defer release()
	if e := bodyCheck(c); e != nil {
		return nil, e
	}
	var body struct {
		Enabled          *bool  `json:"enabled"`
		ExpectedRevision string `json:"expectedRevision"`
	}
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Enabled == nil || body.ExpectedRevision == "" {
		return nil, cgi.E(400, "INVALID_JSON", "Ожидались enabled и expectedRevision.")
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, cgi.E(400, "INVALID_JSON", "Ожидался один объект JSON.")
	}
	cur, ok := readObjectFile(autoSwitchFile(s.d))
	if !ok {
		return nil, cgi.E(500, "CONFIG_INVALID", "Текущие настройки недоступны; состояние не изменено.")
	}
	if autoSwitchRevision(cur) != body.ExpectedRevision {
		return nil, cgi.E(409, "SETTINGS_CHANGED", "Настройки изменились. Обновите состояние.")
	}
	raw, _ := json.Marshal(cur)
	// A legacy catalog can retain a valid but absent preferred ID. A narrow
	// toggle must preserve that unrelated field; only enabling preferred mode
	// below needs a live, supported record. Backup imports separately validate
	// the reference when preferred mode is selected.
	if _, err := s.backupAutoSwitch(raw, func(string) bool { return true }); err != nil {
		return nil, cgi.E(500, "CONFIG_INVALID", "Текущие настройки повреждены; состояние не изменено.")
	}
	if field == "enabled" && *body.Enabled && cur["selectionRule"] == "preferred" {
		rec, err := ReadRecord(s.d, jqRawEmpty(cur["preferredServerId"]))
		if err != nil {
			return nil, cgi.E(400, "SERVER_NOT_FOUND", "Предпочтительный сервер не найден.")
		}
		if err := s.requireRuntimeSupport(s.d.Ctx, rec); err != nil {
			return nil, err
		}
	}
	cur[field] = *body.Enabled
	cur["updatedAt"] = compat.Stamp(s.now())
	if err := compat.WriteJSON(autoSwitchFile(s.d), cur); err != nil {
		return nil, cgi.E(500, "CONFIG_WRITE_FAILED", "Не удалось сохранить состояние.")
	}
	return map[string]any{"value": *body.Enabled, "revision": autoSwitchRevision(cur)}, nil
}

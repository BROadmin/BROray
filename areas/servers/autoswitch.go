package servers

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// bodyCheck is broray_web_request_body_to_file rc mapping of the two save CGIs.
func bodyCheck(c *cgi.Req) *cgi.APIError {
	switch {
	case c.R.ContentLength < 0, c.R.ContentLength == 0:
		return cgi.E(400, "INVALID_CONTENT_LENGTH", "Тело запроса пусто или имеет некорректный размер.")
	case int64(len(c.Body())) != c.R.ContentLength:
		return cgi.E(400, "REQUEST_BODY_INCOMPLETE", "Тело запроса передано не полностью.")
	}
	return nil
}

func decodeBody(c *cgi.Req) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(c.Body())))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// jqText is `jq -r '.field'` (missing → "null").
func jqText(m map[string]any, k string) string { return jqRaw(m[k]) }

func (s *svc) handleAutoSwitchSave(c *cgi.Req) (any, error) {
	release, ok := s.d.Lock.TryAcquire("servers", "servers:auto-switch-save", "")
	if !ok {
		return nil, errBusy
	}
	defer release()
	if e := bodyCheck(c); e != nil {
		return nil, e
	}
	body, ok := decodeBody(c)
	if !ok {
		return nil, cgi.E(400, "INVALID_JSON", "Тело запроса должно быть объектом JSON.")
	}
	enabled := jqText(body, "enabled")
	threshold := jqText(body, "failureThreshold")
	cooldown := jqText(body, "cooldownMinutes")
	minimum := jqText(body, "minimumRating")
	rule := jqText(body, "selectionRule")
	preferred := jqRawEmpty(body["preferredServerId"])
	qrEnabled, qrInterval := false, int64(60)
	if cur, ok := readObjectFile(autoSwitchFile(s.d)); ok {
		if b, ok := cur["qualityRefreshEnabled"].(bool); ok {
			qrEnabled = b
		}
		qrInterval = intervalOf(cur)
	}
	if !in(enabled, "true", "false") {
		return nil, cgi.E(400, "INVALID_ENABLED", "Поле enabled должно быть логическим.")
	}
	if !allDigits(threshold) {
		return nil, cgi.E(400, "INVALID_THRESHOLD", "Порог ошибок должен быть числом.")
	}
	th, err := strconv.ParseInt(threshold, 10, 64)
	if err != nil || th < 1 || th > 10 {
		return nil, cgi.E(400, "INVALID_THRESHOLD", "Порог ошибок должен быть от 1 до 10.")
	}
	if !allDigits(cooldown) {
		return nil, cgi.E(400, "INVALID_COOLDOWN", "Защитный интервал должен быть числом.")
	}
	cd, err := strconv.ParseInt(cooldown, 10, 64)
	if err != nil || cd < 1 || cd > 120 {
		return nil, cgi.E(400, "INVALID_COOLDOWN", "Защитный интервал должен быть от 1 до 120 минут.")
	}
	if !in(minimum, "excellent", "good", "acceptable", "poor") {
		return nil, cgi.E(400, "INVALID_MINIMUM_RATING", "Некорректное минимальное качество.")
	}
	if !in(rule, "best-quality", "lowest-ping", "preferred") {
		return nil, cgi.E(400, "INVALID_SELECTION_RULE", "Некорректное правило выбора.")
	}
	if rule == "preferred" && preferred == "" {
		return nil, cgi.E(400, "PREFERRED_SERVER_REQUIRED", "Выберите предпочтительный сервер.")
	}
	var pref any
	if preferred != "" {
		if !ValidID(preferred) {
			return nil, cgi.E(400, "INVALID_SERVER_ID", "Некорректный идентификатор сервера.")
		}
		if !RecordExists(s.d, preferred) {
			return nil, cgi.E(400, "SERVER_NOT_FOUND", "Предпочтительный сервер не найден.")
		}
		if rec, err := ReadRecord(s.d, preferred); err == nil {
			if err := s.requireRuntimeSupport(s.d.Ctx, rec); err != nil {
				return nil, err
			}
		}
		pref = preferred
	}
	cfg := map[string]any{"schemaVersion": 3, "enabled": enabled == "true", "failureThreshold": th, "cooldownMinutes": cd,
		"minimumRating": minimum, "selectionRule": rule, "preferredServerId": pref, "qualityRefreshEnabled": qrEnabled,
		"qualityRefreshIntervalMinutes": qrInterval, "updatedAt": compat.Stamp(s.now())}
	if err := os.MkdirAll(filepath.Dir(autoSwitchFile(s.d)), 0o700); err != nil {
		return nil, cgi.E(500, "CONFIG_WRITE_FAILED", "Не удалось сохранить настройки.")
	}
	if err := compat.WriteJSON(autoSwitchFile(s.d), cfg); err != nil {
		return nil, cgi.E(500, "CONFIG_WRITE_FAILED", "Не удалось сохранить настройки.")
	}
	s.logf("SERVER_AUTO_SWITCH_SETTINGS=SAVED ENABLED=%s RULE=%s", enabled, rule)
	return normalize(cfg), nil
}

func (s *svc) handleQualityRefreshSave(c *cgi.Req) (any, error) {
	release, ok := s.d.Lock.TryAcquire("servers", "servers:quality-refresh-save", "")
	if !ok {
		return nil, errBusy
	}
	defer release()
	if e := bodyCheck(c); e != nil {
		return nil, e
	}
	body, ok := decodeBody(c)
	_, enOK := body["enabled"].(bool)
	_, ivOK := num(body["intervalMinutes"])
	if !ok || len(body) != 2 || !enOK || !ivOK {
		return nil, cgi.E(400, "INVALID_JSON", "Ожидались поля enabled и intervalMinutes.")
	}
	interval := jqText(body, "intervalMinutes")
	if !in(interval, "30", "60", "180", "360") {
		return nil, cgi.E(400, "INVALID_INTERVAL", "Допустимые интервалы: 30, 60, 180 или 360 минут.")
	}
	invalid := func(msg string) error { return cgi.E(500, "CONFIG_INVALID", msg) }
	cur, ok := readObjectFile(autoSwitchFile(s.d))
	_, b1 := cur["enabled"].(bool)
	_, n1 := num(cur["failureThreshold"])
	_, n2 := num(cur["cooldownMinutes"])
	_, s1 := cur["minimumRating"].(string)
	_, s2 := cur["selectionRule"].(string)
	_, s3 := cur["preferredServerId"].(string)
	if !ok || !b1 || !n1 || !n2 || !s1 || !s2 || !(cur["preferredServerId"] == nil || s3) {
		return nil, invalid("Текущие настройки серверов повреждены; автоматическая проверка не изменена.")
	}
	threshold, cooldown := jqText(cur, "failureThreshold"), jqText(cur, "cooldownMinutes")
	if !allDigits(threshold) {
		return nil, invalid("Текущий порог автовыбора некорректен.")
	}
	if !allDigits(cooldown) {
		return nil, invalid("Текущий защитный интервал некорректен.")
	}
	th, _ := strconv.ParseInt(threshold, 10, 64)
	cd, _ := strconv.ParseInt(cooldown, 10, 64)
	if th < 1 || th > 10 {
		return nil, invalid("Текущий порог автовыбора вне допустимого диапазона.")
	}
	if cd < 1 || cd > 120 {
		return nil, invalid("Текущий защитный интервал вне допустимого диапазона.")
	}
	if !in(cur["minimumRating"].(string), "excellent", "good", "acceptable", "poor") {
		return nil, invalid("Текущее минимальное качество некорректно.")
	}
	if !in(cur["selectionRule"].(string), "best-quality", "lowest-ping", "preferred") {
		return nil, invalid("Текущее правило автовыбора некорректно.")
	}
	preferred := jqRawEmpty(cur["preferredServerId"])
	if preferred != "" && !ValidID(preferred) {
		return nil, invalid("Идентификатор предпочтительного сервера некорректен.")
	}
	var pref any
	if preferred != "" {
		pref = preferred
	}
	iv, _ := strconv.ParseInt(interval, 10, 64)
	updatedAt := compat.Stamp(s.now())
	cfg := map[string]any{"schemaVersion": 3, "enabled": cur["enabled"], "failureThreshold": th, "cooldownMinutes": cd,
		"minimumRating": cur["minimumRating"], "selectionRule": cur["selectionRule"], "preferredServerId": pref,
		"qualityRefreshEnabled": body["enabled"], "qualityRefreshIntervalMinutes": iv, "updatedAt": updatedAt}
	if err := compat.WriteJSON(autoSwitchFile(s.d), cfg); err != nil {
		return nil, cgi.E(500, "CONFIG_WRITE_FAILED", "Не удалось сохранить настройки автопроверки.")
	}
	s.logf("SERVER_QUALITY_REFRESH_SETTINGS=SAVED ENABLED=%v INTERVAL=%d", body["enabled"], iv)
	return map[string]any{"enabled": body["enabled"], "intervalMinutes": iv, "updatedAt": updatedAt}, nil
}

// publicState is broray_auto_switch_public_state (lib/auto-switch-status.sh):
// the cached state with the queue state of its quality-refresh request.
func (s *svc) publicState() any {
	st, ok := readObjectFile(stateFile(s.d))
	if !ok {
		return nil
	}
	qr, ok := st["qualityRefresh"].(map[string]any)
	if !ok {
		return st
	}
	request, _ := qr["requestId"].(string)
	if request == "" {
		return st
	}
	queueState := "unknown"
	if r, ok := s.d.Queue.Request(request); ok && in(r.State, "queued", "running", "completed", "cancelled", "failed") {
		queueState = r.State
	}
	qr["queueState"] = queueState
	if qr["status"] == "running" && queueState != "queued" && queueState != "running" {
		msg := "Продолжение проверки не подтверждено. Сохранены результаты завершённых измерений."
		switch queueState {
		case "cancelled":
			msg = "Проверка отменена. Сохранены результаты завершённых измерений."
		case "failed":
			msg = "Проверка прервана. Сохранены результаты завершённых измерений."
		case "completed":
			msg = "Задание завершено, но итог проверки не подтверждён."
		}
		qr["status"], qr["runStartedAt"], qr["lastError"] = "error", nil, msg
	}
	return st
}

// serviceStatus is broray_service_status_json for the "auto-switch" service
// (the daemon loop in 3.5.0).
func (s *svc) serviceStatus() map[string]any {
	if s.loopRunning.Load() {
		return map[string]any{"service": "auto-switch", "state": "running", "running": true, "ready": true, "complete": true,
			"pid": os.Getpid(), "errorCode": nil}
	}
	return map[string]any{"service": "auto-switch", "state": "stopped", "running": false, "ready": false, "complete": true,
		"pid": nil, "errorCode": nil}
}

func (s *svc) handleAutoSwitchStatus(c *cgi.Req) (any, error) {
	var cfg any = map[string]any{"enabled": false}
	if m, ok := readObjectFile(autoSwitchFile(s.d)); ok {
		cfg = m
	}
	var monitor any
	if m, ok := readObjectFile(s.d.RunPath("connection-status.json")); ok {
		monitor = m
	}
	var pid any
	if s.monitorRunning.Load() {
		pid = os.Getpid()
	}
	var logTail any
	if b, err := os.ReadFile(s.logPath()); err == nil {
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) > 20 {
			lines = lines[len(lines)-20:]
		}
		if t := strings.Join(lines, "\n"); t != "" {
			logTail = t
		}
	}
	return map[string]any{"config": cfg, "state": s.publicState(), "monitor": monitor, "service": s.serviceStatus(),
		"connectionMonitor": map[string]any{"running": s.monitorRunning.Load(), "pid": pid},
		"automation":        map[string]any{"paused": s.d.Queue.Paused(), "complete": true},
		"logTail":           logTail, "checkedAt": compat.Stamp(s.now())}, nil
}

func (s *svc) handleQualityRefreshStatus(c *cgi.Req) (any, error) {
	config := map[string]any{"enabled": false, "intervalMinutes": 60, "updatedAt": nil}
	if m, ok := readObjectFile(autoSwitchFile(s.d)); ok {
		en, _ := m["qualityRefreshEnabled"].(bool)
		iv := any(int64(60))
		if v := m["qualityRefreshIntervalMinutes"]; jsonEq(v, 30) || jsonEq(v, 60) || jsonEq(v, 180) || jsonEq(v, 360) {
			iv = v
		}
		config = map[string]any{"enabled": en, "intervalMinutes": iv, "updatedAt": alt(m["updatedAt"], nil)}
	}
	var state any
	if st, ok := s.publicState().(map[string]any); ok {
		state = alt(st["qualityRefresh"], nil)
	}
	return map[string]any{"config": config, "state": state, "service": s.serviceStatus(),
		"automation": map[string]any{"paused": s.d.Queue.Paused(), "complete": true}, "checkedAt": compat.Stamp(s.now())}, nil
}

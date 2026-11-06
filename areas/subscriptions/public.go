package subscriptions

import (
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// ---------- effective status overlay (svc:1151-1176) ----------

type effective struct {
	status, errMsg, code, finished string
}

func validOpID(id string) bool {
	if !strings.HasPrefix(id, "op-") || len(id) > 96 {
		return false
	}
	return validID(id) == nil
}

// operationOf returns the public row of a subscription background operation.
func (s *svc) operationOf(id string) (map[string]any, bool) {
	if !validOpID(id) {
		return nil, false
	}
	op, ok := s.d.Queue.Operation(id)
	if !ok {
		return nil, false
	}
	pub := op.Public()
	return pub, pub["type"] == "subscription_update"
}

// effectiveStatus is broray_subscription_effective_status. An operation that
// is no longer known to the coordinator (daemon restart, registry trimmed)
// counts as interrupted: no worker can still own the "running" record.
func (s *svc) effectiveStatus(rec map[string]any) effective {
	e := effective{status: jqRaw(alt(rec["lastUpdateStatus"], "never"))}
	if e.status != "running" {
		return e
	}
	opID, _ := rec["backgroundOperationId"].(string)
	if !validOpID(opID) {
		return e
	}
	op, found := s.d.Queue.Operation(opID)
	if found {
		pub := op.Public()
		if pub["type"] != "subscription_update" || pub["running"] != false {
			return e
		}
		switch pub["state"] {
		case "aborted", "failed", "recovered", "completed":
		default:
			return e
		}
		if f, ok := pub["finishedAt"].(string); ok {
			e.finished = f
		}
		if pub["state"] == "aborted" && pub["errorCode"] == "CANCELLED" {
			e.status, e.code, e.errMsg = "error", "CANCELLED", "Обновление остановлено пользователем. Сохранены последние доступные данные подписки."
			return e
		}
	}
	e.status, e.code, e.errMsg = "error", "OPERATION_INTERRUPTED", "Обновление прервано. Сохранены последние доступные данные подписки."
	return e
}

// public is broray_subscription_public_file.
func (s *svc) public(rec map[string]any, includeURL bool, live []srvFile) map[string]any {
	out := clone(rec)
	id := jqRaw(rec["id"])
	count := 0
	if validID(id) == nil {
		count = s.serverCount(live, id)
	}
	eff := s.effectiveStatus(rec)
	operation := map[string]any{}
	progress := map[string]any{}
	opID, _ := rec["backgroundOperationId"].(string)
	if pub, ok := s.operationOf(opID); ok {
		phase, _ := pub["phase"].(string)
		switch phase {
		case "fetching", "parsing", "committing", "waiting":
		default:
			phase = "working"
		}
		running := pub["running"] == true
		cancelReq := pub["cancelRequested"] == true
		operation = map[string]any{"id": opID, "running": running, "phase": phase, "cancelRequested": cancelReq,
			"canCancel": running && pub["cancelability"] == "cooperative" && !cancelReq}
	}
	if eff.status == "running" && validOpID(opID) {
		if p, ok := s.getProgress(opID); ok && p[0] >= 0 && p[0] <= p[1] && p[1] <= maxNodes {
			progress = map[string]any{"processed": p[0], "total": p[1]}
		}
	}
	out["lastUpdateStatus"] = eff.status
	if eff.errMsg != "" {
		out["lastError"] = eff.errMsg
		out["lastUpdateResult"] = map[string]any{"errorCode": eff.code, "durationMs": nil, "warnings": []any{}}
		if eff.finished != "" {
			out["lastUpdatedAt"] = eff.finished
		}
	}
	out["updateOperation"] = operation
	out["parseProgress"] = progress
	out["sendDeviceInfo"] = rec["sendDeviceInfo"] == true
	out["displayUrl"] = maskURL(jqRaw(rec["url"]))
	out["serversCount"] = count
	if !includeURL {
		delete(out, "url")
	}
	delete(out, "clientHwid")
	delete(out, "lastUpdatedEpoch")
	delete(out, "nextUpdateEpoch")
	return out
}

// validRecords returns the records passing validate_file, in glob order.
func (s *svc) validRecords() []map[string]any {
	var out []map[string]any
	for _, id := range s.recordIDs() {
		rec, _, err := s.readRecord(id)
		if err != nil || !validRecord(rec) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// list is broray_subscription_list (newest first).
func (s *svc) list() []any {
	live := s.liveServers()
	items := []map[string]any{}
	for _, rec := range s.validRecords() {
		items = append(items, s.public(rec, false, live))
	}
	sort.SliceStable(items, func(i, j int) bool { return jqRaw(items[i]["createdAt"]) < jqRaw(items[j]["createdAt"]) })
	out := make([]any, len(items))
	for i, it := range items {
		out[len(items)-1-i] = it
	}
	return out
}

// details is broray_subscription_get (with the URL; no validate_file).
func (s *svc) details(id string) (map[string]any, error) {
	if e := validID(id); e != nil {
		return nil, e
	}
	if !exists(s.recPath(id)) {
		return nil, errNotFound
	}
	rec, _, err := s.readRecord(id)
	if err != nil {
		return nil, errOpFailed
	}
	return s.public(rec, true, s.liveServers()), nil
}

var errOpFailed = fail("SUBSCRIPTION_OPERATION_FAILED", "Операция с подпиской завершилась ошибкой.")

// ---------- summary (svc:1957-2123) ----------

func reason(code, message string) map[string]any {
	return map[string]any{"code": code, "message": message, "details": nil}
}

func (s *svc) summary() map[string]any {
	total, enabled, partial, errorsN, running, stale, never := 0, 0, 0, 0, 0, 0, 0
	auto := false
	var latestEpoch int64
	latestStatus, latestAt := "never", ""
	var latestWarnings any = []any{}
	nowEpoch := now().Unix()
	for _, rec := range s.validRecords() {
		total++
		isEnabled := rec["enabled"] == true
		if isEnabled {
			enabled++
			if rec["autoUpdateEnabled"] == true {
				auto = true
			}
		}
		eff := s.effectiveStatus(rec)
		switch eff.status {
		case "partial":
			partial++
		case "error":
			errorsN++
		case "running":
			running++
		}
		epoch, ok := intOf(alt(rec["lastUpdatedEpoch"], 0))
		if !ok || epoch < 0 {
			epoch = 0
		}
		at := ""
		if v := rec["lastUpdatedAt"]; truthy(v) {
			at = jqRaw(v)
		}
		var warnings any = []any{}
		if r, ok := rec["lastUpdateResult"].(map[string]any); ok && truthy(r["warnings"]) {
			warnings = r["warnings"]
		}
		if eff.finished != "" {
			if t, err := time.Parse("2006-01-02T15:04:05Z", eff.finished); err == nil && t.Unix() >= 0 {
				epoch, at, warnings = t.Unix(), eff.finished, []any{}
			}
		}
		if isEnabled {
			interval, ok := intOf(alt(rec["updateIntervalMinutes"], 360))
			if !ok || interval < 0 {
				interval = 360
			}
			if interval < 15 {
				interval = 15
			}
			if epoch <= 0 {
				never++
			} else if nowEpoch-epoch > interval*120 {
				stale++
			}
		}
		if epoch > latestEpoch {
			latestEpoch, latestStatus, latestAt, latestWarnings = epoch, eff.status, at, warnings
		}
	}
	reasons := []any{}
	if errorsN > 0 {
		reasons = append(reasons, reason("SUBSCRIPTION_UPDATE_ERROR", "Последнее обновление одной или нескольких подписок завершилось ошибкой."))
	}
	if partial > 0 {
		reasons = append(reasons, reason("SUBSCRIPTION_UPDATE_PARTIAL", "Одна или несколько подписок обновлены частично."))
	}
	if running > 0 {
		reasons = append(reasons, reason("SUBSCRIPTION_UPDATE_RUNNING", "Обновление подписки выполняется."))
	}
	if stale > 0 {
		reasons = append(reasons, reason("SUBSCRIPTION_UPDATE_STALE", "Автоматическое обновление одной или нескольких подписок просрочено."))
	}
	if never > 0 {
		reasons = append(reasons, reason("SUBSCRIPTION_NEVER_UPDATED", "Одна или несколько активных подписок ещё не обновлялись."))
	}
	severity, operational, action := "ok", true, false
	switch {
	case errorsN > 0:
		severity, operational, action = "error", false, true
	case partial > 0 || stale > 0 || never > 0:
		severity, action = "warning", true
	case running > 0:
		severity = "busy"
	}
	freshness := "fresh"
	if stale > 0 {
		freshness = "stale"
	}
	if never > 0 {
		freshness = "unknown"
	}
	var checkedAt, lastAt any
	if latestAt != "" {
		checkedAt, lastAt = latestAt, latestAt
	}
	health := map[string]any{"schemaVersion": 1, "module": "subscriptions", "availability": "available", "severity": severity,
		"operational": operational, "consistent": partial == 0 && errorsN == 0 && stale == 0 && never == 0, "actionRequired": action,
		"freshness": map[string]any{"state": freshness, "checkedAt": checkedAt}, "reasons": reasons,
		"facts": map[string]any{"total": total, "enabled": enabled, "partial": partial, "errors": errorsN, "running": running,
			"stale": stale, "neverUpdated": never, "lastUpdateStatus": latestStatus},
		"lastOperation": nil}
	return map[string]any{"total": total, "enabled": enabled, "serversReceived": s.serverCountAll(), "lastUpdateStatus": latestStatus,
		"lastUpdatedAt": lastAt, "lastWarnings": latestWarnings, "partialCount": partial, "errorCount": errorsN, "runningCount": running,
		"staleCount": stale, "neverUpdatedCount": never, "autoUpdateEnabled": auto, "health": health}
}

// recoverStale persists the interrupted state of records left "running" by
// a previous daemon process (broray_subscription_recover_stale).
func (s *svc) recoverStale() {
	for _, rec := range s.validRecords() {
		if rec["lastUpdateStatus"] != "running" {
			continue
		}
		id := jqRaw(rec["id"])
		if validID(id) != nil || exists(s.d.RunPath("subscriptions", id+".lock")) {
			continue
		}
		if opID, _ := rec["backgroundOperationId"].(string); !validOpID(opID) {
			continue
		}
		if _, live := s.d.Queue.Operation(jqRaw(rec["backgroundOperationId"])); live {
			continue
		}
		t := now()
		rec["lastUpdateStatus"] = "error"
		rec["lastUpdatedAt"] = compat.Stamp(t)
		rec["lastUpdatedEpoch"] = t.Unix()
		rec["lastError"] = "Обновление прервано. Сохранены последние доступные данные подписки."
		rec["lastUpdateResult"] = map[string]any{"errorCode": "OPERATION_INTERRUPTED", "durationMs": nil, "warnings": []any{}}
		setSchedule(rec, t)
		rec["updatedAt"] = compat.Stamp(t)
		if err := s.writeRecord(rec); err != nil {
			s.d.Log.Printf("subscriptions: recover %s: %v", id, err)
		}
	}
}

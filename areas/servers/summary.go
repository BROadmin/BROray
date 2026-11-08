package servers

import (
	"context"
	"os"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// opFailed is the servers wrapper's generic failure
// (400 SERVER_OPERATION_FAILED, details = the helper's stderr).
func opFailed(details string) *cgi.APIError {
	return cgi.Ed(400, "SERVER_OPERATION_FAILED", "Операция с сервером завершилась ошибкой.", details)
}

func selectionRuleLabel(rule any) string {
	switch rule {
	case "best-quality":
		return "Лучшее качество"
	case "lowest-ping":
		return "Минимальная задержка"
	case "preferred":
		return "Предпочитаемый сервер"
	}
	return "Ручной выбор"
}

func reason(code, message string) map[string]any {
	return map[string]any{"code": code, "message": message, "details": nil}
}

// cachedActiveHealth is broray_active_proxy_cached (active-proxy-health.sh:53-72).
func (s *svc) cachedActiveHealth(id string) map[string]any {
	context := s.runtimeContext(id)
	now := s.now().Unix()
	var best map[string]any
	bestEpoch := -1.0
	files := []string{QualityPath(s.d, id), stateFile(s.d)}
	if id == "" {
		files = []string{s.d.RunPath("server-quality", ".json"), stateFile(s.d)}
	}
	for _, f := range files {
		obj, ok := readObjectFile(f)
		if !ok {
			continue
		}
		h, ok := obj["activeHealth"].(map[string]any)
		if !ok {
			continue
		}
		ep, ok := num(h["checkedEpoch"])
		if !ok || h["method"] != "current-socks-https" || h["serverId"] != id || context == "" || h["context"] != context ||
			ep > float64(now) || float64(now)-ep > 120 || !(h["status"] == "healthy" || h["status"] == "unhealthy") {
			continue
		}
		if ep > bestEpoch {
			best, bestEpoch = h, ep
		}
	}
	if best == nil {
		return map[string]any{"method": "current-socks-https", "status": "unknown", "errorCode": "ACTIVE_HEALTH_NOT_CURRENT"}
	}
	return best
}

// summary is broray_server_summary (server-service.sh:216-539).
func (s *svc) summary(ctx context.Context) (map[string]any, error) {
	_ = os.MkdirAll(s.d.RunPath("server-quality"), 0o700)
	active := ActiveID(s.d)
	ids, err := RecordIDs(s.d)
	if err != nil {
		return nil, opFailed("Ошибка: не удалось сформировать список серверов")
	}
	stale, expired := s.qualityPolicy()
	rows := []any{}
	var activeRow any
	total, available, unavailable := 0, 0, 0
	counts := map[string]int{}
	keys := map[string][]map[string]any{} // CanonicalKey → rows (DEV5_SERVER_MANUAL_SUBSCRIPTION_DUPLICATE)
	for _, id := range ids {
		path := RecordPath(s.d, id)
		if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
			continue
		}
		if err := validateID(id); err != nil {
			return nil, opFailed(err.Text())
		}
		rec, err := readRecordFile(path)
		if err != nil || rec["id"] != id {
			return nil, opFailed("Ошибка: не удалось сформировать данные сервера " + id)
		}
		q := s.qualityView(id, stale, expired)
		support := connectionSupport(rec)
		if support["supported"] == false {
			q = map[string]any{"status": "unsupported", "freshness": "not-applicable", "method": nil}
		}
		row := map[string]any{
			"id": rec["id"], "name": alt(rec["name"], rec["id"]), "address": rec["address"], "port": rec["port"],
			"protocol": rec["protocol"], "transport": alt(rec["network"], get(rec, "transport", "type"), "unknown"),
			"security": alt(rec["security"], "none"), "sourceType": alt(get(rec, "source", "type"), "manual"),
			"subscriptionId": alt(get(rec, "source", "subscriptionId"), nil), "nodeIndex": alt(get(rec, "source", "nodeIndex"), nil),
			"active": rec["id"] == active, "quality": q, "connectionSupport": support,
		}
		rows = append(rows, row)
		if k, err := CanonicalKey(rec); err == nil {
			keys[k] = append(keys[k], row)
		}
		total++
		switch q["status"] {
		case "available":
			available++
		case "unavailable":
			unavailable++
		}
		counts[asString(q["freshness"])]++
		if active != "" && activeRow == nil && id == active {
			activeRow = row
		}
	}
	activePresent := activeRow != nil
	// 3.5.0: the same connection imported by hand and from a subscription
	// stays two records (different lifecycles, as in 3.x); each card names
	// its twins so the owner sees they are one server.
	for _, group := range keys {
		for _, row := range group {
			same := []any{}
			for _, other := range group {
				if other["id"] != row["id"] {
					same = append(same, map[string]any{"id": other["id"], "name": other["name"],
						"sourceType": other["sourceType"], "subscriptionId": other["subscriptionId"]})
				}
			}
			row["sameAs"] = same
		}
	}

	autoSwitch := map[string]any{"enabled": false, "selectionRule": "manual", "selectionRuleLabel": "Ручной выбор"}
	if cfg, ok := readObjectFile(autoSwitchFile(s.d)); ok {
		cfg["selectionRuleLabel"] = selectionRuleLabel(cfg["selectionRule"])
		autoSwitch = cfg
	}

	xrayRunning := s.xrayRunning()
	st := s.settings(ctx)
	socksActive := xrayRunning && s.socksListening(st.socksAddr, st.socksPort)
	xrayOperational := xrayRunning && socksActive

	connAvailable, connUp, connFreshness := false, false, "unknown"
	switch s.cachedActiveHealth(active)["status"] {
	case "healthy":
		connAvailable, connUp, connFreshness = true, true, "fresh"
	case "unhealthy":
		connAvailable, connFreshness = true, "fresh"
	}

	kExists, kHealthy, kConsistent, kFreshness := false, false, false, "unknown"
	if ks, ok := readObjectFile(s.d.RunPath("keenetic-status.json")); ok {
		kExists = jqRaw(alt(ks["exists"], false)) == "true"
		kHealthy = jqRaw(alt(ks["healthy"], false)) == "true"
		kConsistent = jqRaw(alt(ks["matchesExpected"], false)) == "true"
		if st, err := os.Stat(s.d.RunPath("keenetic-status.json")); err == nil {
			kFreshness = Freshness(s.age(st.ModTime()), 60, 300)
		}
	}
	connCurrent := connFreshness == "fresh"
	kCurrent := kFreshness == "fresh"

	var state string
	operational, consistent := false, false
	switch {
	case activePresent && xrayOperational && socksActive && connAvailable && connUp && connCurrent:
		state = "connected"
		if kHealthy && kCurrent {
			operational, consistent = true, kConsistent
		}
	case !activePresent:
		state = "disabled"
	case xrayRunning && socksActive:
		state = "degraded"
	default:
		state = "error"
	}

	reasons := []any{}
	add := func(cond bool, code, msg string) {
		if !cond {
			reasons = append(reasons, reason(code, msg))
		}
	}
	add(activePresent, "ACTIVE_SERVER_MISSING", "Активный сервер не выбран.")
	add(xrayOperational, "XRAY_NOT_OPERATIONAL", "Xray или его локальный SOCKS-интерфейс не готовы.")
	add(connAvailable, "ACTIVE_PROXY_HEALTH_UNAVAILABLE", "Текущий VPN ещё не проверен HTTPS-запросом. Нажмите «Проверить» у активного сервера.")
	add(connUp, "CONNECTION_DOWN", "Соединение через активный сервер не подтверждено.")
	add(connCurrent, "CONNECTION_STATUS_STALE", "Состояние соединения устарело.")
	add(kExists, "PROXY0_MISSING", "Управляемый интерфейс ProxyN не создан.")
	add(kHealthy, "PROXY0_NOT_READY", "Управляемый ProxyN не работает.")
	add(kConsistent, "PROXY0_MISMATCH", "Управляемый ProxyN не соответствует активному серверу.")
	add(kCurrent, "PROXY0_STATUS_STALE", "Проверка управляемого ProxyN устарела.")
	add(counts["unknown"] == 0, "QUALITY_INCOMPLETE", "Не для всех серверов выполнена проверка качества.")
	add(counts["stale"] == 0, "QUALITY_STALE", "Часть измерений качества устарела.")
	add(counts["expired"] == 0, "QUALITY_EXPIRED", "Часть измерений качества просрочена.")

	severity := "ok"
	switch {
	case activePresent && !xrayOperational:
		severity = "error"
	case !operational || !consistent || counts["unknown"] > 0 || counts["stale"] > 0 || counts["expired"] > 0:
		severity = "warning"
	}
	updatedAt := compat.Stamp(s.now())
	facts := map[string]any{"connectionState": state, "activeServerPresent": activePresent, "xrayOperational": xrayOperational,
		"socksActive": socksActive,
		"connection":  map[string]any{"available": connAvailable, "up": connUp, "freshness": connFreshness},
		"proxy0":      map[string]any{"healthy": kHealthy, "consistent": kConsistent, "freshness": kFreshness},
		"quality":     map[string]any{"fresh": counts["fresh"], "stale": counts["stale"], "expired": counts["expired"], "unknown": counts["unknown"]}}
	health := map[string]any{"schemaVersion": 1, "module": "servers", "availability": "available", "severity": severity,
		"operational": operational, "consistent": consistent, "actionRequired": severity != "ok",
		"freshness": map[string]any{"state": worstFreshness(connFreshness, kFreshness), "checkedAt": updatedAt},
		"reasons":   reasons, "facts": facts, "lastOperation": nil}
	return map[string]any{
		"total": total, "available": available, "unavailable": unavailable, "connectionState": state,
		"xrayRunning": xrayRunning, "socksActive": socksActive,
		"activeConnection": map[string]any{"method": "current-socks-https", "available": connAvailable, "up": connUp, "freshness": connFreshness},
		"keenetic":         map[string]any{"healthy": kHealthy, "consistent": kConsistent, "freshness": kFreshness},
		"activeServer":     activeRow, "servers": rows, "autoSwitch": autoSwitch,
		"qualityPolicy": map[string]any{"method": "https-proxy+icmp", "throughputMeasured": false, "listLoadProbes": false,
			"automaticProbe": "on-active-failure"},
		"health": health, "updatedAt": updatedAt,
	}, nil
}

// details is broray_server_details.
func (s *svc) details(id string) (map[string]any, error) {
	if err := validateID(id); err != nil {
		return nil, opFailed(err.Text())
	}
	if !RecordExists(s.d, id) {
		return nil, opFailed("Ошибка: сервер " + id + " не найден")
	}
	rec, err := ReadRecord(s.d, id)
	if err != nil {
		return nil, opFailed("Ошибка: неправильный JSON: " + RecordPath(s.d, id))
	}
	stale, expired := s.qualityPolicy()
	out := MaskRecord(rec)
	out["connectionSupport"] = connectionSupport(rec)
	out["active"] = out["id"] == ActiveID(s.d)
	out["quality"] = s.detailsQuality(id, stale, expired)
	return out, nil
}

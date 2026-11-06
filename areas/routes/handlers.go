package routes

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

const jobDeadline = 3600 * time.Second

var (
	curMu sync.RWMutex
	cur   *engine
)

func current() *engine {
	curMu.RLock()
	defer curMu.RUnlock()
	return cur
}

func setCurrent(e *engine) {
	curMu.Lock()
	cur = e
	curMu.Unlock()
}

var registerOnce sync.Once

// wrap dispatches to the current engine (handlers are registered once per process).
func wrap(fn func(e *engine, c *cgi.Req) (any, error)) cgi.Handler {
	return func(c *cgi.Req) (any, error) {
		e := current()
		if e == nil {
			return nil, cgi.E(500, "ROUTES_PAGE_SUMMARY_UNAVAILABLE", "Модуль сводки маршрутов недоступен.")
		}
		return fn(e, c)
	}
}

func registerHandlers() {
	registerOnce.Do(func() {
		post := cgi.POST()
		cgi.Register("/api/routes/catalog-summary.cgi", wrap(hCatalogSummary))
		cgi.Register("/api/routes/overview.cgi", wrap(hOverview))
		cgi.Register("/api/routes/operation-status.cgi", wrap(hOperationStatus))
		cgi.Register("/api/routes/check.cgi", wrap(hCheck), post)
		cgi.Register("/api/routes/download.cgi", wrap(hDownload), post)
		cgi.Register("/api/routes/verify.cgi", wrap(hVerify), post)
		cgi.Register("/api/routes/plan.cgi", wrap(hPlan), post)
		cgi.Register("/api/routes/preflight.cgi", wrap(hPreflight), post)
		cgi.Register("/api/routes/export.cgi", wrap(func(e *engine, c *cgi.Req) (any, error) { return hMutation(e, c, "export") }), post)
		cgi.Register("/api/routes/delete.cgi", wrap(func(e *engine, c *cgi.Req) (any, error) { return hMutation(e, c, "delete") }), post)
		cgi.Register("/api/routes/resume.cgi", wrap(func(e *engine, c *cgi.Req) (any, error) { return hMutation(e, c, "resume") }), post)
		cgi.Register("/api/routes/progress.cgi", wrap(hProgress))
		cgi.Register("/api/routes/status.cgi", wrap(hStatus))
		cgi.Register("/api/routes/summary.cgi", wrap(hSummary))
		cgi.Register("/api/routes/stop.cgi", wrap(hStop), post, cgi.Status(202))
		registerCustomHandlers()
		registerScheduleHandlers()
		compat.RegisterSnapshot("routes", func(ctx context.Context) (map[string]any, error) {
			return current().homeSnapshot(ctx)
		})
		compat.RegisterUninstallStep("routes", func(ctx context.Context) error {
			return current().uninstallRoutes(ctx)
		})
	})
}

// ---------- query parsing ----------

// prefixBundle is the "prefix style" query (check, download, verify, plan,
// progress, status): the query must start with bundleId=, the rest is the id.
func prefixBundle(raw string) (string, *cgi.APIError) {
	if !strings.HasPrefix(raw, "bundleId=") {
		return "", cgi.E(400, "ROUTES_BUNDLE_REQUIRED", "Не указан идентификатор набора маршрутов.")
	}
	id := strings.TrimPrefix(raw, "bundleId=")
	if !validBundleID(id) {
		return "", cgi.E(400, "ROUTES_BUNDLE_INVALID", "Некорректный идентификатор набора маршрутов.")
	}
	return id, nil
}

// splitQuery is the "split style" query: split on &, the last key=value wins.
func splitQuery(raw string) map[string]string {
	out := map[string]string{}
	for _, item := range strings.Split(raw, "&") {
		for _, k := range []string{"bundleId", "preflightToken", "action", "operationId"} {
			if strings.HasPrefix(item, k+"=") {
				out[k] = strings.TrimPrefix(item, k+"=")
			}
		}
	}
	return out
}

// bundleInList is `.bundles[] | select(. == $id)` on routes/bundles.json.
func (e *engine) bundleInList(id string) bool {
	b, ok := readObj(e.bundlesFile())
	if !ok {
		return false
	}
	for _, x := range strList(b["bundles"]) {
		if x == id {
			return true
		}
	}
	return false
}

var errNotFound = cgi.E(404, "ROUTES_BUNDLE_NOT_FOUND", "Набор маршрутов не найден.")

func (e *engine) jobCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(e.ctx, jobDeadline)
}

func detailsOf(err error) string {
	if err == nil {
		return ""
	}
	return lastLines(err.Error(), 30)
}

func isBusyText(s string) bool { return strings.Contains(s, msgRoutesBusyCLI) }

// ---------- reads ----------

func hCatalogSummary(e *engine, c *cgi.Req) (any, error) {
	return e.pageSummary(c.R.Context(), "catalog"), nil
}

func hOverview(e *engine, c *cgi.Req) (any, error) {
	return e.overview(c.R.Context()), nil
}

func hOperationStatus(e *engine, c *cgi.Req) (any, error) {
	return e.operationStatus(), nil
}

func hProgress(e *engine, c *cgi.Req) (any, error) {
	id, aerr := prefixBundle(c.RawQuery)
	if aerr != nil {
		return nil, aerr
	}
	if !e.bundleInList(id) {
		return nil, errNotFound
	}
	if !fileExists(e.progressFile(id)) {
		p := idleProgress(id)
		p["canStop"] = false
		return p, nil
	}
	p, ok := e.readProgress(id)
	if !ok {
		return nil, cgi.E(500, "ROUTES_PROGRESS_INVALID", "Состояние прогресса операции повреждено.")
	}
	return p, nil
}

func hStatus(e *engine, c *cgi.Req) (any, error) {
	id, aerr := prefixBundle(c.RawQuery)
	if aerr != nil {
		return nil, aerr
	}
	if !e.bundleInList(id) {
		return nil, errNotFound
	}
	if !fileExists(e.stateFile(id)) {
		return nil, cgi.E(500, "ROUTES_STATE_UNAVAILABLE", "Локальное состояние маршрутов недоступно.")
	}
	s, ok := readObj(e.stateFile(id))
	if !ok || intOr(s["schemaVersion"], 0) != 1 || str(s, "bundleId") != id {
		return nil, cgi.E(500, "ROUTES_STATE_INVALID", "Локальное состояние маршрутов повреждено.")
	}
	presence, ok := e.presence(c.R.Context(), id, 5*time.Second)
	if !ok {
		presence = obj{"available": false, "registered": false, "source": nil, "checkedAt": nil, "cacheAgeSeconds": nil,
			"expectedRouteCount": nil, "presentRouteCount": nil, "missingRouteCount": nil, "duplicateRouteCount": nil,
			"complete": nil, "actualInstalled": nil, "drift": nil, "status": "unavailable", "missingRoutes": []any{}}
	}
	out := statusProjection(s)
	delete(out, "resumeOperation")
	delete(out, "operation")
	out["routerPresence"] = presence
	out["operationProgress"] = e.progressOrIdle(id)
	out["globalOperation"] = e.globalOperation()
	return out, nil
}

// statusProjection is the §2.8 projection of a state file.
func statusProjection(s obj) obj {
	out := obj{}
	for _, k := range []string{"schemaVersion", "bundleId", "status", "availableVersion", "downloadedVersion", "installedVersion", "routeCount",
		"lastCheckedAt", "lastVerifiedAt", "lastDownloadedAt", "lastExportedAt", "lastDeletedAt", "lastError", "checkResult",
		"verifyResult", "downloadResult", "exportBuild", "preflight", "exportResult", "deleteResult", "resumeOperation"} {
		out[k] = orNull(s, k)
	}
	out["updatedAt"] = orNull(s, "updatedAt")
	return out
}

func hSummary(e *engine, c *cgi.Req) (any, error) {
	q := c.RawQuery
	var data obj
	var serr *summaryError
	switch {
	case q == "":
		if !reProxy.MatchString(e.managedInterface()) {
			return nil, cgi.E(500, "ROUTES_INTERFACE_POLICY_INVALID", "Разрешённый интерфейс маршрутов настроен неверно.")
		}
		data, serr = e.summaryAll()
	case strings.HasPrefix(q, "bundleId="):
		id := strings.TrimPrefix(q, "bundleId=")
		if !validBundleID(id) {
			return nil, cgi.E(400, "ROUTES_BUNDLE_INVALID", "Некорректный идентификатор набора маршрутов.")
		}
		data, serr = e.bundleSummary(id)
		if serr == nil && (str(data, "managedInterface") != e.managedInterface() || intOr(data["managedMetric"], 0) != managedMetric) {
			return nil, cgi.E(500, "ROUTES_SUMMARY_INVALID", "Модуль маршрутов вернул некорректный summary.")
		}
	default:
		return nil, cgi.E(400, "ROUTES_QUERY_INVALID", "Некорректные параметры запроса.")
	}
	if serr != nil {
		return nil, cgi.E(500, serr.code, serr.message)
	}
	return data, nil
}

func hStop(e *engine, c *cgi.Req) (any, error) {
	q := splitQuery(c.RawQuery)
	id, operationID := q["bundleId"], q["operationId"]
	if id == "" {
		return nil, cgi.E(400, "ROUTES_BUNDLE_REQUIRED", "Не указан идентификатор набора маршрутов.")
	}
	if !validBundleID(id) {
		return nil, cgi.E(400, "ROUTES_BUNDLE_INVALID", "Некорректный идентификатор набора маршрутов.")
	}
	if !e.bundleInList(id) {
		return nil, errNotFound
	}
	if operationID == "" || len(operationID) > 128 {
		return nil, cgi.E(400, "ROUTES_OPERATION_ID_REQUIRED", "Не указан действующий идентификатор операции.")
	}
	if !e.requestRouteStop(id, operationID) {
		return nil, cgi.E(409, "ROUTES_STOP_NOT_RUNNING", "Эту операцию уже нельзя остановить.")
	}
	return obj{"accepted": true, "bundleId": id, "operationId": operationID}, nil
}

// ---------- check / download ----------

func (e *engine) sourceJobPrologue(c *cgi.Req, action string) (string, *job, *cgi.APIError) {
	if !fileExists(e.bundlesFile()) || !fileExists(e.configFile()) {
		return "", nil, cgi.E(500, "ROUTES_CONFIGURATION_UNAVAILABLE", "Конфигурация маршрутов недоступна.")
	}
	id, aerr := prefixBundle(c.RawQuery)
	if aerr != nil {
		return "", nil, aerr
	}
	if !e.bundleInList(id) {
		return "", nil, errNotFound
	}
	j, aerr := e.admit(action, id, msgBusy)
	if aerr != nil {
		return "", nil, aerr
	}
	fail := func(ae *cgi.APIError) (string, *job, *cgi.APIError) {
		j.end(errors.New(ae.Code + ": " + ae.Message))
		return "", nil, ae
	}
	iface := e.managedInterface()
	if !reProxy.MatchString(iface) {
		return fail(cgi.E(500, "ROUTES_INTERFACE_POLICY_INVALID", "Разрешённый интерфейс маршрутов настроен неверно."))
	}
	mf, ok1 := readObj(e.manifestFile(id))
	_, ok2 := readObj(e.stateFile(id))
	if !ok1 || !ok2 {
		return fail(cgi.E(500, "ROUTES_BUNDLE_FILES_UNAVAILABLE", "Файлы выбранного набора маршрутов недоступны."))
	}
	if str(mf, "id") != id || str(mf, "targetInterface") != iface || str(mf, "exportComment") != "BROray" {
		return fail(cgi.E(500, "ROUTES_BUNDLE_POLICY_INVALID", "Набор маршрутов не соответствует политике BROray."))
	}
	return id, j, nil
}

func (e *engine) stateWithOperation(id, typ, output string, readFail string) (any, error) {
	s, ok := readObj(e.stateFile(id))
	if !ok {
		return nil, cgi.E(500, "ROUTES_STATE_READ_FAILED", readFail)
	}
	s["operation"] = obj{"type": typ, "completedAt": stamp(), "output": lastLines(output, 30)}
	return s, nil
}

func (e *engine) stateValid(id string) bool {
	s, ok := readObj(e.stateFile(id))
	_, isStr := s["status"].(string)
	return ok && intOr(s["schemaVersion"], 0) == 1 && str(s, "bundleId") == id && isStr
}

func hCheck(e *engine, c *cgi.Req) (any, error) {
	id, j, aerr := e.sourceJobPrologue(c, "check")
	if aerr != nil {
		return nil, aerr
	}
	ctx, cancel := e.jobCtx()
	defer cancel()
	out, err := e.runSource(ctx, id, false)
	j.end(err)
	if !e.stateValid(id) {
		return nil, cgi.Ed(500, "ROUTES_STATE_INVALID", "Состояние набора маршрутов повреждено.", detailsOf(err))
	}
	if err != nil {
		return nil, cgi.Ed(502, "ROUTES_CHECK_FAILED", "Не удалось проверить обновление маршрутов.", detailsOf(err))
	}
	return e.stateWithOperation(id, "check", out, "Не удалось прочитать результат проверки маршрутов.")
}

func hDownload(e *engine, c *cgi.Req) (any, error) {
	id, j, aerr := e.sourceJobPrologue(c, "download")
	if aerr != nil {
		return nil, aerr
	}
	ctx, cancel := e.jobCtx()
	defer cancel()
	out, err := e.runSource(ctx, id, true)
	j.end(err)
	if !e.stateValid(id) {
		return nil, cgi.Ed(500, "ROUTES_STATE_INVALID", "Состояние набора маршрутов повреждено.", detailsOf(err))
	}
	if err != nil {
		if isBusyText(err.Error()) {
			return nil, cgi.Ed(409, "ROUTES_OPERATION_BUSY", msgBusy, detailsOf(err))
		}
		return nil, cgi.Ed(502, "ROUTES_DOWNLOAD_FAILED", "Не удалось загрузить файлы маршрутов.", detailsOf(err))
	}
	return e.stateWithOperation(id, "download", out, "Не удалось прочитать результат загрузки маршрутов.")
}

// runSource dispatches check/download; custom bundles have no remote source.
func (e *engine) runSource(ctx context.Context, id string, download bool) (string, error) {
	if !isBuiltinID(id) {
		return "", cliErr("Набор маршрутов не найден.")
	}
	if download {
		return e.runDownload(ctx, id)
	}
	return e.runCheck(ctx, id)
}

// ---------- verify ----------

func hVerify(e *engine, c *cgi.Req) (any, error) {
	id, aerr := prefixBundle(c.RawQuery)
	if aerr != nil {
		return nil, aerr
	}
	if b, ok := readObj(e.bundlesFile()); !ok || intOr(b["schemaVersion"], 0) != 1 || !e.bundleInList(id) {
		return nil, errNotFound
	}
	j, aerr := e.admit("verify", id, msgBusy)
	if aerr != nil {
		return nil, aerr
	}
	data, err := e.verify(id)
	j.end(errOf(err))
	return data, err
}

func errOf(err error) error {
	var ae *cgi.APIError
	if errors.As(err, &ae) {
		return errors.New(ae.Code + ": " + ae.Message)
	}
	return err
}

func (e *engine) verify(id string) (any, error) {
	if !fileExists(e.stateFile(id)) {
		return nil, cgi.E(500, "ROUTES_STATE_UNAVAILABLE", "Локальное состояние маршрутов недоступно.")
	}
	s, ok := readObj(e.stateFile(id))
	if _, isObj := s["downloadedVersion"].(obj); !ok || intOr(s["schemaVersion"], 0) != 1 || str(s, "bundleId") != id || (s["downloadedVersion"] != nil && !isObj) {
		return nil, cgi.E(500, "ROUTES_STATE_INVALID", "Локальное состояние маршрутов повреждено.")
	}
	if s["downloadedVersion"] == nil {
		return nil, cgi.E(409, "ROUTES_SET_NOT_DOWNLOADED", "Сначала скачайте набор маршрутов.")
	}
	now := stamp()
	ctx, cancel := e.jobCtx()
	defer cancel()
	writeState := func(mut func(obj)) error {
		st, ok := readObj(e.stateFile(id))
		if !ok {
			return cgi.E(500, "ROUTES_VERIFY_STATE_WRITE_FAILED", "Не удалось сохранить результат проверки набора.")
		}
		mut(st)
		if err := writeObj(e.stateFile(id), st); err != nil {
			return cgi.E(500, "ROUTES_VERIFY_STATE_WRITE_FAILED", "Не удалось сохранить результат проверки набора.")
		}
		return nil
	}
	if _, _, _, err := e.runBuildExport(id); err != nil {
		details := lastLines(err.Error(), 30)
		if isBusyText(details) {
			return nil, cgi.Ed(409, "ROUTES_OPERATION_BUSY", msgBusy, details)
		}
		msg := "Локальный набор маршрутов не прошёл проверку. Скачайте файлы заново."
		if werr := writeState(func(st obj) {
			dv := sub(st, "downloadedVersion")
			rc := st["routeCount"]
			if rc == nil {
				rc = 0
			}
			st["lastVerifiedAt"] = now
			st["verifyResult"] = obj{"result": "invalid_local", "success": false, "message": msg, "checkedAt": now,
				"contentSha256": orNull(dv, "contentSha256"),
				"local":         obj{"valid": false, "routeCount": rc, "sourceFileCount": intOr(orNull(dv, "sourceFileCount"), 0), "duplicateRouteCount": nil, "invalidRouteCount": nil},
				"keenetic": obj{"checked": false, "available": nil, "status": "not_checked", "expectedRouteCount": nil, "presentRouteCount": nil,
					"missingRouteCount": nil, "conflictCount": nil, "externalRouteCount": nil, "complete": nil, "updatePending": nil}}
			st["lastError"] = obj{"code": "ROUTES_SET_INVALID", "message": msg, "details": details}
			st["updatedAt"] = now
		}); werr != nil {
			return nil, werr
		}
		return nil, cgi.Ed(422, "ROUTES_SET_INVALID", msg, details)
	}
	plan, err := e.runPlan(ctx, id)
	if err != nil {
		details := lastLines(err.Error(), 30)
		if isBusyText(details) {
			return nil, cgi.Ed(409, "ROUTES_OPERATION_BUSY", msgBusy, details)
		}
		reason, msg := "unknown", "Локальный набор исправен, но проверить его состояние в Keenetic не удалось."
		switch {
		case strings.Contains(details, "ROUTES_CONFIG_NDMC_LOCK_FAILED"):
			reason, msg = "keenetic_read_busy", "Keenetic временно занят чтением конфигурации. Повторите проверку через несколько секунд."
		case strings.Contains(details, "ROUTES_CONFIG_NDMC_TIMEOUT") || strings.Contains(details, "ROUTES_CONFIG_RUNNING_CONFIG_READ_FAILED") ||
			strings.Contains(details, "ROUTES_PLAN_RUNNING_CONFIG_READ_FAILED"):
			reason, msg = "keenetic_read_failed", "Keenetic не ответил на безопасное чтение конфигурации. Повторите проверку."
		case strings.Contains(details, "ROUTES_PLAN_RUNNING_CONFIG_UNSUPPORTED"):
			reason, msg = "unsupported_route_format", "В конфигурации Keenetic есть статический маршрут неподдерживаемого формата. BROray не продолжит без безопасного разбора."
		case strings.Contains(details, "ROUTES_PLAN_REGISTRY_INVALID"):
			reason, msg = "registry_invalid", "Внутренний реестр маршрутов BROray не прошёл проверку. Маршруты Keenetic не изменялись."
		case strings.Contains(details, "ROUTES_PLAN_LOCAL_VERSION_MISMATCH"):
			reason, msg = "local_version_changed", "Локальные данные набора изменились во время проверки. Повторите подготовку набора."
		case strings.Contains(details, "откат") || strings.Contains(details, "rollback"):
			reason, msg = "rollback_required", "Проверка заблокирована: предыдущий откат маршрутов требует восстановления."
		}
		if werr := writeState(func(st obj) {
			dv := sub(st, "downloadedVersion")
			rc := st["routeCount"]
			if rc == nil {
				rc = 0
			}
			st["lastVerifiedAt"] = now
			st["verifyResult"] = obj{"result": "router_unavailable", "success": false, "failureReason": reason, "message": msg, "checkedAt": now,
				"contentSha256": orNull(dv, "contentSha256"),
				"local":         obj{"valid": true, "routeCount": rc, "sourceFileCount": intOr(orNull(dv, "sourceFileCount"), 0), "duplicateRouteCount": 0, "invalidRouteCount": 0},
				"keenetic": obj{"checked": false, "available": false, "status": "unavailable", "reason": reason, "expectedRouteCount": nil,
					"presentRouteCount": nil, "missingRouteCount": nil, "conflictCount": nil, "externalRouteCount": nil, "complete": nil, "updatePending": nil}}
			st["lastError"] = obj{"code": "ROUTES_VERIFY_ROUTER_FAILED", "message": msg, "details": details}
			st["updatedAt"] = now
		}); werr != nil {
			return nil, werr
		}
		return nil, cgi.Ed(502, "ROUTES_VERIFY_ROUTER_FAILED", msg, details)
	}
	sm := plan.Summary
	total, present, missing := intOr(sm["total"], 0), intOr(sm["managedExisting"], 0), intOr(sm["toCreate"], 0)
	conflicts, external := intOr(sm["conflicts"], 0), intOr(sm["externalExisting"], 0)
	var message string
	if werr := writeState(func(st obj) {
		installed := st["installedVersion"] != nil
		updatePending := installed && str(sub(st, "downloadedVersion"), "contentSha256") != str(sub(st, "installedVersion"), "contentSha256")
		status := "complete"
		switch {
		case !plan.CanApply:
			status = "conflict"
		case !installed:
			status = "not_installed"
		case updatePending:
			status = "update_pending"
		case missing > 0 || intOr(sm["sharedToRestore"], 0) > 0:
			status = "restore_required"
		}
		complete := installed && !updatePending && plan.CanApply && missing == 0 && conflicts == 0 && external == 0
		switch status {
		case "conflict":
			message = "Локальный набор исправен, но в Keenetic обнаружены конфликты: " + itoa(conflicts) + "."
		case "not_installed":
			message = "Набор исправен: " + itoa(total) + " маршрутов, ошибок и дубликатов нет. В Keenetic набор не установлен."
		case "update_pending":
			message = "Набор исправен. Загруженная версия содержит " + itoa(total) + " маршрутов и ожидает обновления в Keenetic."
		case "restore_required":
			message = "Набор исправен. В Keenetic найдено " + itoa(present) + " из " + itoa(total) + " маршрутов; отсутствует " + itoa(missing) + "."
		default:
			message = "Набор исправен. В Keenetic найдено " + itoa(total) + " из " + itoa(total) + " маршрутов."
		}
		dv := sub(st, "downloadedVersion")
		st["lastVerifiedAt"] = now
		st["verifyResult"] = obj{"result": status, "success": status != "conflict", "message": message, "checkedAt": now,
			"contentSha256": orNull(dv, "contentSha256"),
			"local":         obj{"valid": true, "routeCount": total, "sourceFileCount": intOr(orNull(dv, "sourceFileCount"), 0), "duplicateRouteCount": 0, "invalidRouteCount": 0},
			"keenetic": obj{"checked": true, "available": true, "status": status, "expectedRouteCount": total, "presentRouteCount": present,
				"missingRouteCount": missing, "conflictCount": conflicts, "externalRouteCount": external, "complete": complete,
				"updatePending": updatePending, "mode": plan.Mode, "canApply": plan.CanApply, "targetInterface": plan.Iface, "managedMetric": managedMetric}}
		st["lastError"] = nil
		st["operation"] = obj{"type": "verify", "completedAt": now, "output": message}
		st["updatedAt"] = now
	}); werr != nil {
		return nil, werr
	}
	s, ok = readObj(e.stateFile(id))
	if !ok {
		return nil, cgi.E(500, "ROUTES_STATE_READ_FAILED", "Не удалось прочитать результат проверки набора.")
	}
	return s, nil
}

// ---------- plan ----------

func hPlan(e *engine, c *cgi.Req) (any, error) {
	if !fileExists(e.bundlesFile()) {
		return nil, cgi.E(500, "ROUTES_CONFIGURATION_UNAVAILABLE", "Конфигурация маршрутов недоступна.")
	}
	id, aerr := prefixBundle(c.RawQuery)
	if aerr != nil {
		return nil, aerr
	}
	if !e.bundleInList(id) {
		return nil, errNotFound
	}
	j, aerr := e.admit("plan", id, msgBusy)
	if aerr != nil {
		return nil, aerr
	}
	ctx, cancel := e.jobCtx()
	defer cancel()
	plan, err := e.runPlan(ctx, id)
	j.end(err)
	if err != nil {
		return nil, cgi.Ed(409, "ROUTES_PLAN_FAILED", "Не удалось подготовить безопасный план установки в Keenetic.", lastLines(err.Error(), 40))
	}
	return plan.object(), nil
}

// ---------- preflight ----------

func hPreflight(e *engine, c *cgi.Req) (any, error) {
	q := splitQuery(c.RawQuery)
	id, action := q["bundleId"], q["action"]
	if !validBundleID(id) {
		return nil, cgi.E(400, "ROUTES_BUNDLE_INVALID", "Некорректный идентификатор набора маршрутов.")
	}
	if action != "export" && action != "delete" && action != "resume" {
		return nil, cgi.E(400, "ROUTES_PREFLIGHT_ACTION_INVALID", "Некорректный тип предварительной проверки.")
	}
	if b, ok := readObj(e.bundlesFile()); !ok || intOr(b["schemaVersion"], 0) != 1 || !e.bundleInList(id) {
		return nil, errNotFound
	}
	j, aerr := e.admit("preflight:"+action, id, msgBusy)
	if aerr != nil {
		return nil, aerr
	}
	data, err := e.preflight(id, action)
	if j.tookPending || (action == "resume" && e.progressResumable(id)) {
		j.pend() // the paused operation keeps its fence until it is resumed
	}
	if fenceErr := j.end(errOf(err)); fenceErr != nil {
		return nil, cgi.Ed(500, "ROUTES_FENCE_PERSIST_FAILED", "Не удалось сохранить блокировку маршрутов; состояние операции требует проверки.", fenceErr.Error())
	}
	return data, err
}

func (e *engine) progressResumable(id string) bool {
	m, ok := e.readProgress(id)
	return ok && boolOf(m["resumable"]) && !boolOf(m["running"])
}

func (e *engine) preflight(id, action string) (any, error) {
	resolved, saved := "", ""
	switch action {
	case "export":
		resolved = "export"
	case "delete":
		resolved, saved = "delete", "delete"
	case "resume":
		p, ok := readObj(e.progressFile(id))
		if !ok || p["resumable"] != true || p["running"] != false {
			return nil, cgi.E(409, "ROUTES_RESUME_NOT_READY", "Для этого набора нет операции, которую можно продолжить.")
		}
		switch op := str(p, "operation"); op {
		case "install", "update", "restore":
			resolved, saved = "export", op
		case "delete":
			resolved, saved = "delete", "delete"
		default:
			return nil, cgi.E(409, "ROUTES_RESUME_NOT_READY", "Для этого набора нет операции, которую можно продолжить.")
		}
	}
	ctx, cancel := e.jobCtx()
	defer cancel()
	var plan obj
	if resolved == "export" {
		p, err := e.runPlan(ctx, id)
		if err != nil {
			if isBusyText(err.Error()) {
				return nil, cgi.Ed(409, "ROUTES_OPERATION_BUSY", msgRoutesBusyCLI, lastLines(err.Error(), 40))
			}
			return nil, cgi.Ed(409, "ROUTES_PREFLIGHT_PLAN_FAILED", "Предварительная проверка установки в Keenetic не завершена.", lastLines(err.Error(), 40))
		}
		plan = p.object()
	} else {
		p, ok := e.deletePlan(ctx, id)
		if !ok {
			return nil, cgi.E(409, "ROUTES_PREFLIGHT_DELETE_FAILED", "Предварительная проверка удаления из Keenetic не завершена.")
		}
		plan = p
	}
	res, ok := e.finalizePreflight(ctx, id, action, resolved, saved, plan)
	if !ok {
		return nil, cgi.E(500, "ROUTES_PREFLIGHT_BUILD_FAILED", "Не удалось сформировать результат предварительной проверки.")
	}
	return res, nil
}

// ---------- export / delete / resume ----------

func hMutation(e *engine, c *cgi.Req, action string) (any, error) {
	if !fileExists(e.bundlesFile()) || !fileExists(e.configFile()) {
		return nil, cgi.E(500, "ROUTES_CONFIGURATION_UNAVAILABLE", "Конфигурация маршрутов недоступна.")
	}
	q := splitQuery(c.RawQuery)
	id, token := q["bundleId"], q["preflightToken"]
	if id == "" {
		return nil, cgi.E(400, "ROUTES_BUNDLE_REQUIRED", "Не указан идентификатор набора маршрутов.")
	}
	if !validBundleID(id) {
		return nil, cgi.E(400, "ROUTES_BUNDLE_INVALID", "Некорректный идентификатор набора маршрутов.")
	}
	if !e.bundleInList(id) {
		return nil, errNotFound
	}
	j, aerr := e.admit(action, id, msgBusy)
	if aerr != nil {
		return nil, aerr
	}
	data, err := e.mutation(id, action, token, j.opID())
	if e.rollbackBlocked() {
		j.block() // uncertain transaction: never release the global fence
	} else if e.progressResumable(id) {
		j.pend() // stopped resumably: the fence stays pending until resume
	}
	if fenceErr := j.end(errOf(err)); fenceErr != nil {
		return nil, cgi.Ed(500, "ROUTES_FENCE_PERSIST_FAILED", "Не удалось сохранить блокировку маршрутов; состояние операции требует проверки.", fenceErr.Error())
	}
	return data, err
}

func (e *engine) mutation(id, action, token, opID string) (any, error) {
	switch e.validatePreflight(id, action, token) {
	case 0:
	case 2:
		return nil, cgi.E(409, "ROUTES_PREFLIGHT_REQUIRED", "Перед запуском выполните предварительную проверку и подтвердите актуальный план.")
	case 3:
		return nil, cgi.E(409, "ROUTES_PREFLIGHT_EXPIRED", "Предварительная проверка устарела. Выполните её повторно.")
	case 4:
		return nil, cgi.E(409, "ROUTES_PREFLIGHT_CHANGED", "Состояние набора изменилось после проверки. Выполните предварительную проверку повторно.")
	default:
		return nil, cgi.E(500, "ROUTES_PREFLIGHT_VALIDATE_FAILED", "Не удалось подтвердить предварительную проверку.")
	}
	iface := e.managedInterface()
	if !reProxy.MatchString(iface) || !e.managedMetricIs1200() {
		return nil, cgi.E(500, "ROUTES_POLICY_INVALID", "Политика управляемых маршрутов настроена неверно.")
	}
	mf, ok1 := readObj(e.manifestFile(id))
	state, ok2 := readObj(e.stateFile(id))
	breg, ok3 := readObj(e.registryFile(id))
	if !ok1 || !ok2 || !ok3 {
		return nil, cgi.E(500, "ROUTES_BUNDLE_FILES_UNAVAILABLE", "Файлы выбранного набора маршрутов недоступны.")
	}
	if str(mf, "id") != id || str(mf, "targetInterface") != iface || str(mf, "exportComment") != "BROray" {
		return nil, cgi.E(500, "ROUTES_BUNDLE_POLICY_INVALID", "Набор маршрутов не соответствует политике BROray.")
	}
	resultAction := action
	switch action {
	case "export":
		if state["downloadedVersion"] == nil {
			return nil, cgi.E(409, "ROUTES_EXPORT_NOT_READY", "Маршруты ещё не загружены и не готовы к установке в Keenetic.")
		}
	case "delete":
		if breg["installedVersion"] == nil || len(anyList(breg["managedRouteKeys"])) == 0 {
			return nil, cgi.E(409, "ROUTES_DELETE_NOT_READY", "Управляемые маршруты этого набора не установлены.")
		}
	case "resume":
		p, ok := readObj(e.progressFile(id))
		if !ok || p["resumable"] != true || p["running"] != false {
			return nil, cgi.E(409, "ROUTES_RESUME_NOT_READY", "Для этого набора нет операции, которую можно продолжить.")
		}
		switch str(p, "operation") {
		case "install", "update", "restore":
			resultAction = "export"
		case "delete":
			resultAction = "delete"
		default:
			return nil, cgi.E(409, "ROUTES_RESUME_TYPE_INVALID", "Тип сохранённой операции не поддерживается.")
		}
	}
	ctx, cancel := e.jobCtx()
	defer cancel()
	var out string
	var err error
	if resultAction == "export" {
		out, err = e.runExport(ctx, id, opID)
	} else {
		out, err = e.runDelete(ctx, id, opID)
	}
	paused := errors.Is(err, errPaused)
	if err != nil && !paused {
		details := lastLines(err.Error(), 60)
		if isBusyText(details) {
			return nil, cgi.Ed(409, "ROUTES_OPERATION_BUSY", msgBusy, details)
		}
		switch action {
		case "export":
			return nil, cgi.Ed(502, "ROUTES_EXPORT_FAILED", "Не удалось установить маршруты в Keenetic.", details)
		case "delete":
			return nil, cgi.Ed(502, "ROUTES_DELETE_FAILED", "Не удалось удалить маршруты из Keenetic.", details)
		default:
			return nil, cgi.Ed(502, "ROUTES_RESUME_FAILED", "Не удалось продолжить операцию с маршрутами.", details)
		}
	}
	details := lastLines(out, 60)
	if !e.stateValid(id) {
		return nil, cgi.Ed(500, "ROUTES_STATE_INVALID", "Состояние набора маршрутов повреждено после операции.", details)
	}
	state, _ = readObj(e.stateFile(id))
	if !paused {
		switch resultAction {
		case "export":
			if str(state, "status") != "installed" || state["installedVersion"] == nil || str(sub(state, "exportResult"), "result") != "installed" {
				return nil, cgi.Ed(500, "ROUTES_EXPORT_RESULT_INVALID", "Результат установки не подтверждён локальным состоянием.", details)
			}
		case "delete":
			// 3.x required status "downloaded" and failed after a successful
			// delete of a never-downloaded set ("not_checked"); accepted here.
			st := str(state, "status")
			if (st != "downloaded" && st != "not_checked") || state["installedVersion"] != nil || str(sub(state, "deleteResult"), "result") != "removed" {
				return nil, cgi.Ed(500, "ROUTES_DELETE_RESULT_INVALID", "Результат удаления не подтверждён локальным состоянием.", details)
			}
		}
	}
	data := statusProjection(state)
	var progress any
	if p, ok := readObj(e.progressFile(id)); ok {
		progress = p
	}
	data["operationProgress"] = progress
	data["operation"] = obj{"type": action, "completedAt": stamp(), "output": details}
	return data, nil
}

// managedMetricIs1200 is `jq -r .managedMetric` == "1200".
func (e *engine) managedMetricIs1200() bool {
	c, _ := e.routesConfig()
	if c == nil {
		return false
	}
	if s, ok := c["managedMetric"].(string); ok {
		return s == "1200"
	}
	f, ok := numOf(c["managedMetric"])
	return ok && f == 1200
}

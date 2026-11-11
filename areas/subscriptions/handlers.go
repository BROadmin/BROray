package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// ---------- error → HTTP (api/subscriptions/common.sh:53-127) ----------

func statusOf(code string) int {
	switch code {
	case "SUBSCRIPTION_NOT_FOUND":
		return 404
	case "UPDATE_ALREADY_RUNNING", "ACTIVE_SERVER_CONFLICT", "SERVER_SYNC_BUSY", "SERVER_ID_CONFLICT", "OPERATION_CANCELLED":
		return 409
	case "DOWNLOAD_TIMEOUT", "PARSE_TIMEOUT":
		return 504
	case "OPERATION_UNRESOLVED":
		return 503
	case "HTTP_ERROR", "DOWNLOAD_SECURITY":
		return 502
	case "CONTENT_TOO_LARGE":
		return 413
	case "INTERNAL_ERROR", "PERSISTENCE_ERROR", "SERVER_SYNC_ERROR", "SERVER_SOURCE_REMOVE_FAILED", "SERVER_SOURCE_STATE_FAILED":
		return 500
	}
	return 400
}

func toAPI(err error) *cgi.APIError {
	var ae *cgi.APIError
	if errors.As(err, &ae) {
		return ae
	}
	if errors.Is(err, errCancelled) || errors.Is(err, context.Canceled) {
		return cgi.E(409, "OPERATION_CANCELLED", "Обновление подписки остановлено.")
	}
	var se *subErr
	if errors.As(err, &se) {
		return cgi.E(statusOf(se.Code), se.Code, se.Msg)
	}
	return cgi.E(400, "SUBSCRIPTION_OPERATION_FAILED", "Операция с подпиской завершилась ошибкой.")
}

// queryID is broray_subscriptions_api_query id: the first `id` pair, raw.
func queryID(raw string) string {
	for _, pair := range strings.Split(raw, "&") {
		name, value, found := strings.Cut(pair, "=")
		if name == "id" {
			if !found {
				return pair
			}
			return value
		}
	}
	return ""
}

var tooLarge = cgi.E(413, "REQUEST_TOO_LARGE", "Тело запроса слишком большое.")

// readBody is broray_subscriptions_api_read_body_to_file (limit 65536).
func readBody(c *cgi.Req) (map[string]any, *cgi.APIError) {
	switch {
	case c.R.ContentLength < 0:
		return nil, cgi.E(411, "CONTENT_LENGTH_REQUIRED", "Для запроса требуется корректный Content-Length.")
	case c.R.ContentLength == 0:
		return nil, cgi.E(400, "REQUEST_BODY_REQUIRED", "Тело запроса отсутствует.")
	case c.R.ContentLength > 65536:
		return nil, tooLarge
	case int64(len(c.Body())) != c.R.ContentLength:
		return nil, cgi.E(400, "REQUEST_BODY_INCOMPLETE", "Тело запроса получено не полностью.")
	}
	m, ok := c.Map()
	if !ok {
		return nil, cgi.E(400, "REQUEST_JSON_INVALID", "Тело запроса должно быть корректным JSON-объектом.")
	}
	return m, nil
}

var errBusy = cgi.E(409, "OPERATION_BUSY", "Другая конфликтующая операция BROray уже выполняется.")

// job is broray_subscriptions_api_lock <action> (broray_job_begin system
// "subscriptions:<action>" subscriptions USER cooperative). fn returns
// handedOff=true when it passed release/op to a background worker.
func (s *svc) job(action string, fn func(op *compat.Operation, release compat.Release) (any, bool, error)) (any, bool, error) {
	release, ok := s.d.Lock.TryAcquire("subscriptions", "subscriptions:"+action, "")
	if !ok {
		return nil, false, errBusy
	}
	op := s.d.Queue.StartOperation("", "subscription_update", "subscriptions:"+action, "USER", "cooperative", "")
	data, handedOff, err := fn(op, release)
	if handedOff {
		return data, true, nil
	}
	op.Finish(err)
	release()
	return data, false, err
}

// ---------- body field helpers (jq -r semantics) ----------

func boolText(v any) (bool, bool) {
	switch shellValue(v) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

func intervalText(v any) (int64, *subErr) {
	t := shellValue(v)
	if !allDigits(t) {
		return 0, fail("INVALID_INTERVAL", "Интервал обновления должен быть целым числом минут.")
	}
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil || n < minInterval || n > maxInterval {
		return 0, fail("INVALID_INTERVAL", fmt.Sprintf("Интервал обновления должен быть от %d до %d минут.", minInterval, maxInterval))
	}
	return n, nil
}

// validateBody is broray_subscription_validate_body (UA, sendDeviceInfo).
func validateBody(b map[string]any) *subErr {
	if v, ok := b["httpUserAgent"]; ok {
		ua, isStr := v.(string)
		if !isStr || !printableASCII(ua, 128) {
			return fail("INVALID_USER_AGENT", "User-Agent должен содержать не более 128 печатных ASCII-символов.")
		}
	}
	if v, ok := b["sendDeviceInfo"]; ok {
		if _, isBool := v.(bool); !isBool {
			return fail("INVALID_DEVICE_INFO_SETTING", "Разрешение отправки сведений об устройстве должно быть логическим.")
		}
	}
	return nil
}

var errName = fail("INVALID_NAME", "Название подписки должно содержать от 1 до 128 символов.")

// ---------- create (svc:1340-1480) ----------

func (s *svc) create(op *compat.Operation, release compat.Release, b map[string]any) (any, bool, error) {
	if e := validateBody(b); e != nil {
		return nil, false, e
	}
	textOrEmpty := func(k string) string {
		if !truthy(b[k]) {
			return ""
		}
		return shellValue(b[k])
	}
	name, url := textOrEmpty("name"), textOrEmpty("url")
	field := func(k string, def bool) any {
		if v, ok := b[k]; ok {
			return v
		}
		return def
	}
	enabled, ok1 := boolText(field("enabled", true))
	auto, ok2 := boolText(field("autoUpdateEnabled", true))
	immediate, ok3 := boolText(field("updateImmediately", false))
	ua := ""
	if truthy(b["httpUserAgent"]) {
		ua = jqRaw(b["httpUserAgent"])
	}
	if name == "" || runeLen(name) > 128 {
		return nil, false, errName
	}
	if url == "" || runeLen(url) > 4096 {
		return nil, false, fail("INVALID_URL", "URL подписки не указан или слишком длинный.")
	}
	if _, e := parseURL(url); e != nil {
		return nil, false, e
	}
	if !ok1 || !ok2 || !ok3 {
		return nil, false, fail("INVALID_REQUEST", "Поля enabled, autoUpdateEnabled и updateImmediately должны быть логическими.")
	}
	interval, e := intervalText(alt(b["updateIntervalMinutes"], int64(360)))
	if e != nil {
		return nil, false, e
	}
	id := s.newID()
	t := now()
	rec := map[string]any{"schemaVersion": 1, "id": id, "name": name, "url": url, "clientHwid": newHwid(), "httpUserAgent": ua,
		"sendDeviceInfo": b["sendDeviceInfo"] == true, "enabled": enabled, "autoUpdateEnabled": auto, "updateIntervalMinutes": interval,
		"lastUpdateStatus": "never", "lastUpdatedAt": nil, "lastUpdatedEpoch": nil, "lastError": nil, "serversReceived": 0,
		"lastUpdateResult": nil, "createdAt": compat.Stamp(t), "updatedAt": compat.Stamp(t)}
	setSchedule(rec, t)
	if err := s.writeRecord(rec); err != nil {
		return nil, false, err
	}
	s.logf("subscription=" + id + " action=create url=" + maskURL(url))
	if immediate {
		data, err := s.launch(op, release, id, "initial")
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}
	data, err := s.details(id)
	return data, false, err
}

// ---------- update settings (svc:1482-1614) ----------

func (s *svc) updateSettings(id string, b map[string]any) (any, error) {
	if e := validID(id); e != nil {
		return nil, e
	}
	if e := validateBody(b); e != nil {
		return nil, e
	}
	if !exists(s.recPath(id)) {
		return nil, errNotFound
	}
	old, _, err := s.readRecord(id)
	if err != nil {
		return nil, errOpFailed
	}
	pick := func(k string) any {
		if v, ok := b[k]; ok {
			return v
		}
		return old[k]
	}
	name, url := shellValue(pick("name")), shellValue(pick("url"))
	sendDevice := old["sendDeviceInfo"] == true
	if v, ok := b["sendDeviceInfo"]; ok {
		sendDevice = v == true
	}
	ua := ""
	if v, ok := b["httpUserAgent"]; ok {
		ua = jqRaw(v)
	} else if truthy(old["httpUserAgent"]) {
		ua = jqRaw(old["httpUserAgent"])
	}
	if name == "" || runeLen(name) > 128 {
		return nil, errName
	}
	if _, e := parseURL(url); e != nil {
		return nil, e
	}
	enabled, ok1 := boolText(pick("enabled"))
	auto, ok2 := boolText(pick("autoUpdateEnabled"))
	if !ok1 || !ok2 {
		return nil, fail("INVALID_REQUEST", "Поля enabled и autoUpdateEnabled должны быть логическими.")
	}
	interval, e := intervalText(pick("updateIntervalMinutes"))
	if e != nil {
		return nil, e
	}
	if shellValue(pick("enabled")) != shellValue(old["enabled"]) {
		if err := s.setEnabled(id, enabled); err != nil {
			return nil, err
		}
	}
	t := now()
	rec := clone(old)
	if u, isStr := old["url"].(string); !isStr || u != url {
		delete(rec, "providerMetadata")
		delete(rec, "alternateExports")
	}
	rec["name"], rec["httpUserAgent"], rec["sendDeviceInfo"], rec["url"] = name, ua, sendDevice, url
	rec["enabled"], rec["autoUpdateEnabled"], rec["updateIntervalMinutes"] = enabled, auto, interval
	setSchedule(rec, t)
	rec["updatedAt"] = compat.Stamp(t)
	if err := s.writeRecord(rec); err != nil {
		return nil, err
	}
	s.logf(fmt.Sprintf("subscription=%s action=update-settings url=%s enabled=%t auto=%t interval=%d", id, maskURL(url), enabled, auto, interval))
	return s.details(id)
}

// ---------- delete (svc:1891-1938) ----------

func (s *svc) remove(id string) (any, error) {
	if e := validID(id); e != nil {
		return nil, e
	}
	if !exists(s.recPath(id)) {
		return nil, errNotFound
	}
	if exists(s.d.RunPath("subscriptions", id+".lock")) {
		return nil, fail("UPDATE_ALREADY_RUNNING", "Состояние прежней операции подписки требует восстановления.")
	}
	removed, err := s.removeServers(id)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(s.recPath(id)); err != nil && !os.IsNotExist(err) {
		return nil, fail("PERSISTENCE_ERROR", "Серверы удалены, но запись подписки удалить не удалось.")
	}
	s.logf("subscription=" + id + " action=delete")
	return map[string]any{"subscriptionId": id, "removed": removed, "activeServerImpact": "none"}, nil
}

// ---------- servers list (svc:1940-1955) ----------

func (s *svc) serversOf(id string) (any, error) {
	if e := validID(id); e != nil {
		return nil, e
	}
	if !exists(s.recPath(id)) {
		return nil, errNotFound
	}
	return s.serverList(id), nil
}

// ---------- legacy refresh (refresh.cgi without X-BROray-Queue) ----------

func (s *svc) refreshLegacy(op *compat.Operation, release compat.Release, id string) (any, bool, error) {
	if e := validID(id); e != nil {
		return nil, false, e
	}
	if !regular(s.recPath(id)) {
		return nil, false, errNotFound
	}
	data, err := s.launch(op, release, id, "manual")
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// ---------- registration ----------

func writeResult(c *cgi.Req, data any, accepted bool, err error) {
	if err != nil {
		cgi.WriteErr(c.W, toAPI(err))
		return
	}
	status := 200
	if accepted {
		status = 202
	}
	cgi.WriteOKStatus(c.W, status, data)
}

func register() {
	cgi.Register("/api/subscriptions/list.cgi", func(c *cgi.Req) (any, error) { return cur().list(), nil })
	cgi.Register("/api/subscriptions/summary.cgi", func(c *cgi.Req) (any, error) { return cur().summary(), nil })
	cgi.Register("/api/subscriptions/details.cgi", func(c *cgi.Req) (any, error) {
		d, err := cur().details(queryID(c.RawQuery))
		if err != nil {
			return nil, toAPI(err)
		}
		return d, nil
	})
	cgi.Register("/api/subscriptions/servers.cgi", func(c *cgi.Req) (any, error) {
		d, err := cur().serversOf(queryID(c.RawQuery))
		if err != nil {
			return nil, toAPI(err)
		}
		return d, nil
	})
	bodyOpts := []cgi.Opt{cgi.POST(), cgi.Limit(1 << 20), cgi.TooLarge(tooLarge)}
	cgi.RegisterRaw("/api/subscriptions/create.cgi", func(c *cgi.Req) {
		s := cur()
		data, accepted, err := s.job("create", func(op *compat.Operation, release compat.Release) (any, bool, error) {
			b, e := readBody(c)
			if e != nil {
				return nil, false, e
			}
			return s.create(op, release, b)
		})
		writeResult(c, data, accepted, err)
	}, bodyOpts...)
	cgi.RegisterRaw("/api/subscriptions/update.cgi", func(c *cgi.Req) {
		s := cur()
		data, _, err := s.job("update", func(op *compat.Operation, _ compat.Release) (any, bool, error) {
			id := queryID(c.RawQuery)
			b, e := readBody(c)
			if e != nil {
				return nil, false, e
			}
			d, err := s.updateSettings(id, b)
			return d, false, err
		})
		writeResult(c, data, false, err)
	}, bodyOpts...)
	cgi.RegisterRaw("/api/subscriptions/delete.cgi", func(c *cgi.Req) {
		s := cur()
		data, _, err := s.job("delete", func(*compat.Operation, compat.Release) (any, bool, error) {
			d, err := s.remove(queryID(c.RawQuery))
			return d, false, err
		})
		writeResult(c, data, false, err)
	}, bodyOpts...)
	cgi.RegisterRaw("/api/subscriptions/refresh.cgi", func(c *cgi.Req) {
		s := cur()
		if c.Header("X-BROray-Queue") == "1" {
			s.d.ServeQueued(c, s.resolveRefresh)
			return
		}
		data, accepted, err := s.job("refresh", func(op *compat.Operation, release compat.Release) (any, bool, error) {
			return s.refreshLegacy(op, release, queryID(c.RawQuery))
		})
		writeResult(c, data, accepted, err)
	}, bodyOpts...)
}

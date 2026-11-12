package subscriptions

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
)

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	long := strings.Repeat("я", 129)
	cases := []struct {
		name, body string
		hdr        map[string]string
		status     int
		code, msg  string
	}{
		{"no length", `{}`, map[string]string{"X-Test-Content-Length": "-1"}, 411, "CONTENT_LENGTH_REQUIRED", "Для запроса требуется корректный Content-Length."},
		{"empty", ``, nil, 400, "REQUEST_BODY_REQUIRED", "Тело запроса отсутствует."},
		{"too large", `{"x":"` + strings.Repeat("a", 70000) + `"}`, nil, 413, "REQUEST_TOO_LARGE", "Тело запроса слишком большое."},
		{"incomplete", `{}`, map[string]string{"X-Test-Content-Length": "xx"}, 400, "REQUEST_BODY_INCOMPLETE", "Тело запроса получено не полностью."},
		{"not object", `[1]`, nil, 400, "REQUEST_JSON_INVALID", "Тело запроса должно быть корректным JSON-объектом."},
		{"bad json", `{`, nil, 400, "REQUEST_JSON_INVALID", "Тело запроса должно быть корректным JSON-объектом."},
		{"ua", `{"name":"n","httpUserAgent":"é"}`, nil, 400, "INVALID_USER_AGENT", "User-Agent должен содержать не более 128 печатных ASCII-символов."},
		{"ua type", `{"httpUserAgent":5}`, nil, 400, "INVALID_USER_AGENT", "User-Agent должен содержать не более 128 печатных ASCII-символов."},
		{"device", `{"sendDeviceInfo":"yes"}`, nil, 400, "INVALID_DEVICE_INFO_SETTING", "Разрешение отправки сведений об устройстве должно быть логическим."},
		{"no name", `{"url":"https://a.example/"}`, nil, 400, "INVALID_NAME", "Название подписки должно содержать от 1 до 128 символов."},
		{"long name", `{"name":"` + long + `"}`, nil, 400, "INVALID_NAME", "Название подписки должно содержать от 1 до 128 символов."},
		{"no url", `{"name":"n"}`, nil, 400, "INVALID_URL", "URL подписки не указан или слишком длинный."},
		{"long url", `{"name":"n","url":"https://a.example/` + strings.Repeat("a", 4080) + `"}`, nil, 400, "INVALID_URL", "URL подписки не указан или слишком длинный."},
		{"scheme", `{"name":"n","url":"ftp://a"}`, nil, 400, "INVALID_URL", "URL подписки должен использовать HTTP или HTTPS."},
		{"local", `{"name":"n","url":"https://nas.local/sub"}`, nil, 502, "DOWNLOAD_SECURITY", "Локальные и служебные адреса запрещены."},
		{"enabled", `{"name":"n","url":"https://a.example/","enabled":"maybe"}`, nil, 400, "INVALID_REQUEST", "Поля enabled, autoUpdateEnabled и updateImmediately должны быть логическими."},
		{"enabled null", `{"name":"n","url":"https://a.example/","enabled":null}`, nil, 400, "INVALID_REQUEST", "Поля enabled, autoUpdateEnabled и updateImmediately должны быть логическими."},
		{"immediate", `{"name":"n","url":"https://a.example/","updateImmediately":1}`, nil, 400, "INVALID_REQUEST", "Поля enabled, autoUpdateEnabled и updateImmediately должны быть логическими."},
		{"interval text", `{"name":"n","url":"https://a.example/","updateIntervalMinutes":"abc"}`, nil, 400, "INVALID_INTERVAL", "Интервал обновления должен быть целым числом минут."},
		{"interval frac", `{"name":"n","url":"https://a.example/","updateIntervalMinutes":5.5}`, nil, 400, "INVALID_INTERVAL", "Интервал обновления должен быть целым числом минут."},
		{"interval low", `{"name":"n","url":"https://a.example/","updateIntervalMinutes":4}`, nil, 400, "INVALID_INTERVAL", "Интервал обновления должен быть от 5 до 10080 минут."},
		{"interval high", `{"name":"n","url":"https://a.example/","updateIntervalMinutes":10081}`, nil, 400, "INVALID_INTERVAL", "Интервал обновления должен быть от 5 до 10080 минут."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := call(t, "POST", "/api/subscriptions/create.cgi", c.body, c.hdr)
			if r.Status != c.status || r.errCode() != c.code || r.errMsg() != c.msg {
				t.Fatalf("%d %s", r.Status, r.Raw)
			}
			if r.Body["success"] != false || r.Body["data"] != nil {
				t.Fatalf("envelope: %s", r.Raw)
			}
		})
	}
	if r := call(t, "GET", "/api/subscriptions/create.cgi", "", nil); r.Status != 405 || r.errMsg() != "Для этой операции требуется метод POST." {
		t.Fatalf("method: %s", r.Raw)
	}
	// busy fence wins over invalid input (lock before body)
	release, _ := e.d.Lock.TryAcquire("routes", "export", "b")
	r := call(t, "POST", "/api/subscriptions/create.cgi", `[1]`, nil)
	release()
	if r.Status != 409 || r.errCode() != "OPERATION_BUSY" || r.errMsg() != "Другая конфликтующая операция BROray уже выполняется." {
		t.Fatalf("busy: %s", r.Raw)
	}
	if len(e.s.recordIDs()) != 0 {
		t.Fatal("records written by failed creates")
	}
}

func TestCreateUpdateDeleteFlow(t *testing.T) {
	e := newEnv(t)
	r := call(t, "POST", "/api/subscriptions/create.cgi",
		`{"name":"Мой провайдер","url":"https://example.com/sub/AbCdEf123?x=1","httpUserAgent":"","sendDeviceInfo":true,"enabled":"true","autoUpdateEnabled":false,"updateIntervalMinutes":"60","updateImmediately":false}`, nil)
	if r.Status != 200 {
		t.Fatalf("%s", r.Raw)
	}
	d := r.data()
	id := d["id"].(string)
	if !strings.HasPrefix(id, "sub-") || len(id) != 20 || d["url"] != "https://example.com/sub/AbCdEf123?x=1" ||
		d["displayUrl"] != "https://example.com/sub/***?***" || d["lastUpdateStatus"] != "never" || d["serversCount"] != float64(0) ||
		d["sendDeviceInfo"] != true || d["enabled"] != true || d["autoUpdateEnabled"] != false || d["updateIntervalMinutes"] != float64(60) ||
		d["nextUpdateAt"] != nil {
		t.Fatalf("details: %v", d)
	}
	for _, k := range []string{"clientHwid", "lastUpdatedEpoch", "nextUpdateEpoch"} {
		if _, ok := d[k]; ok {
			t.Fatalf("%s leaked", k)
		}
	}
	rec := e.record(t, id)
	if h, _ := rec["clientHwid"].(string); !strings.HasPrefix(h, "broray-") || len(h) != 39 {
		t.Fatalf("hwid %v", rec["clientHwid"])
	}
	// list hides url; summary
	l := call(t, "GET", "/api/subscriptions/list.cgi", "", nil)
	items := l.Body["data"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["url"] != nil {
		t.Fatalf("list: %s", l.Raw)
	}
	// update: toggles and settings
	upd := func(q, body string) resp { return call(t, "POST", "/api/subscriptions/update.cgi"+q, body, nil) }
	if r := upd("?id="+id, `{"autoUpdateEnabled":true}`); r.Status != 200 || r.data()["nextUpdateAt"] == nil {
		t.Fatalf("auto on: %s", r.Raw)
	}
	errs := []struct {
		q, body       string
		status        int
		code, message string
	}{
		{"", `{}`, 400, "INVALID_SUBSCRIPTION_ID", "Не указан идентификатор подписки."},
		{"?id=a/b", `{}`, 400, "INVALID_SUBSCRIPTION_ID", "Идентификатор подписки содержит недопустимые символы."},
		{"?id=sub-0000000000000000", `{}`, 404, "SUBSCRIPTION_NOT_FOUND", "Подписка не найдена."},
		{"?id=" + id, `{"httpUserAgent":"\u0001"}`, 400, "INVALID_USER_AGENT", "User-Agent должен содержать не более 128 печатных ASCII-символов."},
		{"?id=" + id, `{"sendDeviceInfo":1}`, 400, "INVALID_DEVICE_INFO_SETTING", "Разрешение отправки сведений об устройстве должно быть логическим."},
		{"?id=" + id, `{"name":""}`, 400, "INVALID_NAME", "Название подписки должно содержать от 1 до 128 символов."},
		{"?id=" + id, `{"url":"https://u@h/"}`, 400, "INVALID_URL", "Данные пользователя в URL подписки запрещены."},
		{"?id=" + id, `{"enabled":"x"}`, 400, "INVALID_REQUEST", "Поля enabled и autoUpdateEnabled должны быть логическими."},
		{"?id=" + id, `{"updateIntervalMinutes":null}`, 400, "INVALID_INTERVAL", "Интервал обновления должен быть целым числом минут."},
		{"?id=" + id, `{"url":"https://a.example/` + strings.Repeat("a", 4100) + `"}`, 500, "PERSISTENCE_ERROR", "Данные подписки не прошли проверку."},
	}
	for _, c := range errs {
		if r := upd(c.q, c.body); r.Status != c.status || r.errCode() != c.code || r.errMsg() != c.message {
			t.Errorf("update %s %s: %d %s", c.q, c.body, r.Status, r.Raw)
		}
	}
	// url change drops providerMetadata
	rec = e.record(t, id)
	rec["providerMetadata"] = map[string]any{"schemaVersion": 1, "title": "x"}
	if err := e.s.writeRecord(rec); err != nil {
		t.Fatal(err)
	}
	if r := upd("?id="+id+"&id=other", `{"name":"Renamed"}`); r.Status != 200 || r.data()["providerMetadata"] == nil || r.data()["name"] != "Renamed" {
		t.Fatalf("rename: %s", r.Raw)
	}
	if r := upd("?id="+id, `{"url":"https://other.example/s"}`); r.Status != 200 || r.data()["providerMetadata"] != nil {
		t.Fatalf("url change: %s", r.Raw)
	}
	// details / servers errors
	for _, p := range []string{"details", "servers"} {
		if r := call(t, "GET", "/api/subscriptions/"+p+".cgi", "", nil); r.Status != 400 || r.errCode() != "INVALID_SUBSCRIPTION_ID" {
			t.Errorf("%s: %s", p, r.Raw)
		}
		if r := call(t, "GET", "/api/subscriptions/"+p+".cgi?id=sub-zz", "", nil); r.Status != 404 || r.errMsg() != "Подписка не найдена." {
			t.Errorf("%s: %s", p, r.Raw)
		}
	}
	// delete
	del := func(q string) resp { return call(t, "POST", "/api/subscriptions/delete.cgi"+q, `{}`, nil) }
	if r := del("?id=sub-zz"); r.Status != 404 {
		t.Fatalf("delete 404: %s", r.Raw)
	}
	writeFile(t, e.d.RunPath("subscriptions", id+".lock"), "")
	if r := del("?id=" + id); r.Status != 409 || r.errCode() != "UPDATE_ALREADY_RUNNING" || r.errMsg() != "Состояние прежней операции подписки требует восстановления." {
		t.Fatalf("delete lock: %s", r.Raw)
	}
	os.Remove(e.d.RunPath("subscriptions", id+".lock"))
	writeFile(t, e.d.RunPath("server-subscription.lock"), "")
	if r := del("?id=" + id); r.Status != 409 || r.errCode() != "SERVER_SYNC_BUSY" {
		t.Fatalf("delete sync lock: %s", r.Raw)
	}
	os.Remove(e.d.RunPath("server-subscription.lock"))
	writeFile(t, e.d.Root.Tmp()+"/server-subscription-sync.1.x/rollback-required", "x")
	if r := del("?id=" + id); r.Status != 400 || r.errCode() != "SERVER_SYNC_RECOVERY_REQUIRED" || r.errMsg() != "Прежнее восстановление серверов не завершено. Резервная копия сохранена." {
		t.Fatalf("delete recovery: %s", r.Raw)
	}
	os.RemoveAll(e.d.Root.Tmp() + "/server-subscription-sync.1.x")
	if r := del("?id=" + id); r.Status != 200 || r.data()["subscriptionId"] != id || r.data()["removed"] != float64(0) || r.data()["activeServerImpact"] != "none" {
		t.Fatalf("delete: %s", r.Raw)
	}
	if exists2(e.s.recPath(id)) {
		t.Fatal("record not removed")
	}
	// operations were registered for every mutating call
	found := 0
	for _, op := range e.d.Queue.Operations() {
		if op["type"] == "subscription_update" {
			found++
		}
	}
	if found < 5 {
		t.Fatalf("operations: %d", found)
	}
}

func TestEnableDisableMovesFiles(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, linkA+"\n"+linkC+"\n")
	id := e.create(t, p.URL+"/sub", "")
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	live := liveIDs(t, e)
	if len(live) != 2 {
		t.Fatalf("live %v", live)
	}
	r := call(t, "POST", "/api/subscriptions/update.cgi?id="+id, `{"enabled":false}`, nil)
	if r.Status != 200 || r.data()["enabled"] != false || r.data()["serversCount"] != float64(2) {
		t.Fatalf("disable: %s", r.Raw)
	}
	if len(liveIDs(t, e)) != 0 || len(disabledIDs(e, id)) != 2 {
		t.Fatalf("not moved: %v %v", liveIDs(t, e), disabledIDs(e, id))
	}
	sv := call(t, "GET", "/api/subscriptions/servers.cgi?id="+id, "", nil)
	items := sv.data()["items"].([]any)
	if sv.data()["total"] != float64(2) || items[0].(map[string]any)["enabled"] != false {
		t.Fatalf("servers: %s", sv.Raw)
	}
	if _, ok := items[0].(map[string]any)["uri"]; ok {
		t.Fatal("servers.cgi leaks uri")
	}
	// refresh of a disabled subscription lands in the disabled dir
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	if len(liveIDs(t, e)) != 0 || len(disabledIDs(e, id)) != 2 {
		t.Fatal("disabled refresh wrote live servers")
	}
	// enable conflict: same file name already live
	clash := e.s.disabledServers(id)[0]
	writeFile(t, servers.RecordPath(e.d, clash.id), string(clash.raw))
	r = call(t, "POST", "/api/subscriptions/update.cgi?id="+id, `{"enabled":true}`, nil)
	if r.Status != 409 || r.errCode() != "SERVER_ID_CONFLICT" || r.errMsg() != "Не удалось включить подписку: идентификатор сервера уже занят." {
		t.Fatalf("conflict: %s", r.Raw)
	}
	os.Remove(servers.RecordPath(e.d, clash.id))
	r = call(t, "POST", "/api/subscriptions/update.cgi?id="+id, `{"enabled":true}`, nil)
	if r.Status != 200 || len(liveIDs(t, e)) != 2 || exists2(e.s.disabledDir(id)) {
		t.Fatalf("enable: %s", r.Raw)
	}
	// active server blocks disable and delete
	if err := e.s.writeActive(liveIDs(t, e)[0]); err != nil {
		t.Fatal(err)
	}
	r = call(t, "POST", "/api/subscriptions/update.cgi?id="+id, `{"enabled":false}`, nil)
	if r.Status != 409 || r.errMsg() != "Нельзя выключить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер." {
		t.Fatalf("active disable: %s", r.Raw)
	}
	r = call(t, "POST", "/api/subscriptions/delete.cgi?id="+id, `{}`, nil)
	if r.Status != 409 || r.errMsg() != "Нельзя удалить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер." {
		t.Fatalf("active delete: %s", r.Raw)
	}
	os.Remove(servers.ActiveFile(e.d))
	writeFile(t, servers.QualityPath(e.d, liveIDs(t, e)[0]), "{}")
	r = call(t, "POST", "/api/subscriptions/delete.cgi?id="+id, `{}`, nil)
	if r.Status != 200 || r.data()["removed"] != float64(2) || len(liveIDs(t, e)) != 0 {
		t.Fatalf("delete: %s", r.Raw)
	}
	if q, _ := os.ReadDir(e.d.RunPath("server-quality")); len(q) != 0 {
		t.Fatal("quality cache kept")
	}
}

func TestLegacyRefreshAndCancel(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, linkA+"\n")
	p.headers["Profile-Title"] = "Premium"
	p.headers["Subscription-Userinfo"] = "upload=1; download=2; total=3; expire=0"
	id := e.create(t, p.URL+"/s", "")
	for _, c := range []struct{ q, code string }{{"", "INVALID_SUBSCRIPTION_ID"}, {"?id=sub-nope", "SUBSCRIPTION_NOT_FOUND"}} {
		if r := call(t, "POST", "/api/subscriptions/refresh.cgi"+c.q, `{}`, nil); r.errCode() != c.code {
			t.Fatalf("%s: %s", c.q, r.Raw)
		}
	}
	r := call(t, "POST", "/api/subscriptions/refresh.cgi?id="+id, ``, nil)
	if r.Status != 202 || r.data()["accepted"] != true || r.data()["id"] != id {
		t.Fatalf("refresh: %s", r.Raw)
	}
	opID := r.data()["backgroundOperationId"].(string)
	waitFor(t, "update", func() bool { return e.record(t, id)["lastUpdateStatus"] == "success" })
	waitFor(t, "fence", func() bool { return !e.d.Lock.Busy() })
	rec := e.record(t, id)
	res := rec["lastUpdateResult"].(map[string]any)
	if rec["backgroundOperationId"] != opID || !numIs(res["added"], 1) || res["trigger"] != "manual" || res["deletionPolicy"] != "replace" {
		t.Fatalf("result %v", rec)
	}
	pm := rec["providerMetadata"].(map[string]any)
	if pm["title"] != "Premium" || pm["usage"].(map[string]any)["total"] == nil || pm["updatedAt"] == nil {
		t.Fatalf("metadata %v", pm)
	}
	seen := p.header(0)
	if !strings.HasPrefix(seen.Get("X-Hwid"), "broray-") || seen.Get("User-Agent") != defaultUserAgent() || seen.Get("X-Device-Os") != "" {
		t.Fatalf("headers %v", seen)
	}
	d := call(t, "GET", "/api/subscriptions/details.cgi?id="+id, "", nil).data()
	op := d["updateOperation"].(map[string]any)
	if op["id"] != opID || op["running"] != false || op["canCancel"] != false || d["serversCount"] != float64(1) {
		t.Fatalf("details op %v", d)
	}

	// cancel while downloading
	p.setBlock(make(chan struct{}))
	r = call(t, "POST", "/api/subscriptions/refresh.cgi?id="+id, ``, nil)
	opID = r.data()["backgroundOperationId"].(string)
	waitFor(t, "fetching", func() bool {
		l := call(t, "GET", "/api/subscriptions/list.cgi", "", nil).Body["data"].([]any)[0].(map[string]any)
		uo := l["updateOperation"].(map[string]any)
		return l["lastUpdateStatus"] == "running" && uo["phase"] == "fetching" && uo["canCancel"] == true
	})
	if r := call(t, "POST", "/api/subscriptions/create.cgi", `{}`, nil); r.errCode() != "OPERATION_BUSY" {
		t.Fatalf("fence not held by worker: %s", r.Raw)
	}
	if _, err := e.d.Queue.CancelOperation(opID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "fence", func() bool { return !e.d.Lock.Busy() })
	close(p.block)
	l := call(t, "GET", "/api/subscriptions/list.cgi", "", nil).Body["data"].([]any)[0].(map[string]any)
	lr := l["lastUpdateResult"].(map[string]any)
	if l["lastUpdateStatus"] != "error" || lr["errorCode"] != "CANCELLED" ||
		l["lastError"] != "Обновление остановлено пользователем. Сохранены последние доступные данные подписки." || l["serversCount"] != float64(1) {
		t.Fatalf("cancel overlay %v", l)
	}
	if e.record(t, id)["lastUpdateStatus"] != "running" {
		t.Fatal("cancelled record should stay running on disk (3.x)")
	}
	s := call(t, "GET", "/api/subscriptions/summary.cgi", "", nil).data()
	if s["errorCount"] != float64(1) || s["lastUpdateStatus"] != "error" || s["health"].(map[string]any)["severity"] != "error" {
		t.Fatalf("summary %v", s)
	}
	// a daemon restart (empty operation registry) persists the interrupted state
	d2 := *e.d
	d2.Queue = compat.NewCoordinator(&d2)
	newSvc(&d2).recoverStale()
	rec = e.record(t, id)
	if rec["lastUpdateStatus"] != "error" || rec["lastUpdateResult"].(map[string]any)["errorCode"] != "OPERATION_INTERRUPTED" ||
		rec["lastError"] != "Обновление прервано. Сохранены последние доступные данные подписки." {
		t.Fatalf("recover %v", rec)
	}
}

func numIs(v any, want float64) bool { f, ok := num(v); return ok && f == want }

func TestCreateImmediate(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, "#profile-title: base64:0J/RgNC+0LLQsNC50LTQtdGA\n"+linkA+"\njunk\n")
	r := call(t, "POST", "/api/subscriptions/create.cgi", `{"name":"P","url":"`+p.URL+`/x","sendDeviceInfo":true,"httpUserAgent":"MyUA/1","updateImmediately":true}`, nil)
	if r.Status != 202 || r.data()["accepted"] != true {
		t.Fatalf("%s", r.Raw)
	}
	id := r.data()["id"].(string)
	waitFor(t, "update", func() bool { return e.record(t, id)["lastUpdateStatus"] == "partial" })
	rec := e.record(t, id)
	res := rec["lastUpdateResult"].(map[string]any)
	if res["trigger"] != "initial" || !numIs(res["rejected"], 1) || res["deletionPolicy"] != "retain-unmatched" ||
		rec["providerMetadata"].(map[string]any)["title"] != "Провайдер" {
		t.Fatalf("%v", rec)
	}
	if w := res["warnings"].([]any); len(w) != 1 || w[0] != "Узел 2: Ошибка: неподдерживаемое или неправильное поле JSON: document" {
		t.Fatalf("warnings %v", w)
	}
	h := p.header(0)
	if h.Get("User-Agent") != "MyUA/1" || h.Get("X-Device-Os") != "KeeneticOS" || h.Get("X-Device-Model") != "KN-1010" ||
		h.Get("X-Ver-Os") != "4.2.3" || h.Get("X-App-Version") != appVersion() {
		t.Fatalf("device headers %v", h)
	}
	if rec["nextUpdateEpoch"] == nil {
		t.Fatal("schedule")
	}
	_ = fmt.Sprint()
}

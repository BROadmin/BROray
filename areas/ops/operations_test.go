package ops

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestOperationsCommonChecks(t *testing.T) {
	testArea(t)
	h := opsHdr()
	cases := []struct {
		name, method, path, body string
		hdr                      map[string]string
		code                     int
		err, msg                 string
	}{
		{"query", "GET", "/api/operations/status.cgi?x=1", "", nil, 400, "INVALID_REQUEST", "Параметры запроса не поддерживаются."},
		{"get body", "GET", "/api/operations/events.cgi", "{}", nil, 400, "INVALID_REQUEST", "Тело GET-запроса не поддерживается."},
		{"method", "PUT", "/api/operations/status.cgi", "", nil, 405, "METHOD_NOT_ALLOWED", "Для этой операции требуется метод GET."},
		{"csrf", "POST", "/api/operations/automation.cgi", `{"paused":false}`, map[string]string{"Content-Type": "application/json", "Origin": "http://example.com"}, 403, "CSRF_REJECTED", "Обновите страницу и повторите действие."},
		{"origin", "POST", "/api/operations/automation.cgi", `{"paused":false}`, map[string]string{"X-BROray-Request": "operations", "Content-Type": "application/json", "Origin": "http://evil.example"}, 403, "ORIGIN_REJECTED", "Источник запроса не подтверждён."},
		{"origin mismatch", "POST", "/api/operations/automation.cgi", `{"paused":false}`, map[string]string{"X-BROray-Request": "operations", "Content-Type": "application/json", "Origin": "http://example.com", "X-BROray-Origin": "http://other.com"}, 403, "ORIGIN_REJECTED", ""},
		{"content type", "POST", "/api/operations/automation.cgi", `{"paused":false}`, map[string]string{"X-BROray-Request": "operations", "Content-Type": "text/plain", "Origin": "http://example.com"}, 415, "INVALID_CONTENT_TYPE", "Требуется JSON."},
		{"too large", "POST", "/api/operations/automation.cgi", `{"paused":false,"x":"` + strings.Repeat("a", 4100) + `"}`, h, 413, "REQUEST_TOO_LARGE", "Запрос превышает 4 КБ."},
		{"not object", "POST", "/api/operations/automation.cgi", `[1]`, h, 400, "INVALID_BODY", "Требуется один JSON-объект."},
		{"automation body", "POST", "/api/operations/automation.cgi", `{"paused":"no"}`, h, 400, "INVALID_REQUEST", "Некорректное состояние автоматики."},
		{"automation extra", "POST", "/api/operations/automation.cgi", `{"paused":true,"x":1}`, h, 400, "INVALID_REQUEST", "Некорректное состояние автоматики."},
		{"stop body", "POST", "/api/operations/stop-background.cgi", `{"pauseAutomation":false}`, h, 400, "INVALID_REQUEST", "Требуется пауза автоматики."},
		{"recover body", "POST", "/api/operations/recover.cgi", `{"x":1}`, h, 400, "INVALID_REQUEST", "Параметры восстановления не поддерживаются."},
		{"cancel body", "POST", "/api/operations/cancel.cgi", `{"operationId":"bad"}`, h, 400, "INVALID_REQUEST", "Некорректный идентификатор операции."},
		{"cancel both", "POST", "/api/operations/cancel.cgi", `{"operationId":"op-q-0123456789abcdef0123456789abcdef","requestId":"q-0123456789abcdef0123456789abcdef"}`, h, 400, "INVALID_REQUEST", ""},
		{"lookup body", "POST", "/api/operations/status.cgi", `{"nonce":"ABC"}`, h, 400, "INVALID_REQUEST", "Некорректный идентификатор запроса."},
		{"lookup unknown", "POST", "/api/operations/status.cgi", `{"nonce":"0123456789abcdef0123456789abcdef"}`, h, 404, "REQUEST_UNCONFIRMED", ""},
		{"cancel unknown request", "POST", "/api/operations/cancel.cgi", `{"requestId":"q-0123456789abcdef0123456789abcdef"}`, h, 404, "REQUEST_UNCONFIRMED", ""},
		{"cancel unknown op", "POST", "/api/operations/cancel.cgi", `{"operationId":"op-20261102101955-4711-a1b2c3d4e5f6"}`, h, 503, "STATE_UNAVAILABLE", ""},
	}
	for _, c := range cases {
		r := call(t, c.method, c.path, c.body, c.hdr)
		if r.Code != c.code || errCode(r) != c.err || (c.msg != "" && errMessage(r) != c.msg) {
			t.Errorf("%s: %d %s", c.name, r.Code, r.Raw)
		}
	}
	// KeenDNS proxy origin without Origin header: X-BROray-Origin + live publication proof
	compat.KeenDNSOrigin = func(context.Context) (string, bool) { return "https://broray.my.keenetic.pro", true }
	defer func() { compat.KeenDNSOrigin = nil }()
	r := call(t, "POST", "/api/operations/automation.cgi", `{"paused":false}`,
		map[string]string{"X-BROray-Request": "operations", "Content-Type": "application/json", "X-BROray-Origin": "https://broray.my.keenetic.pro"})
	if r.Code != 200 || r.Body["ok"] != true {
		t.Fatalf("keendns origin: %d %s", r.Code, r.Raw)
	}
}

func TestOperationsFlow(t *testing.T) {
	a := testArea(t)
	q := a.d.Queue
	gate := make(chan struct{})
	q.Define(compat.ActionSpec{Name: "servers:quality", Type: "server_operation", Priority: map[string]int{"USER": 2},
		Stages: []compat.Stage{{Name: "probe", Phase: "checking", Run: func(t *compat.Task) error {
			select {
			case <-gate:
			case <-t.Ctx.Done():
				return t.Ctx.Err()
			}
			return nil
		}}}})
	nonce := compat.RandHex(16)
	rq, _, err := q.Submit("servers:quality", "a", "USER", "ctx", nonce)
	if err != nil {
		t.Fatal(err)
	}
	// queue-lookup
	r := call(t, "POST", "/api/operations/status.cgi", `{"nonce":"`+nonce+`"}`, opsHdr())
	if r.Code != 200 || r.Body["requestId"] != "q-"+nonce || r.Body["state"] != "queued" || r.Body["coalesced"] != false || r.Body["priority"] != float64(2) {
		t.Fatalf("lookup %d %s", r.Code, r.Raw)
	}
	// status shows the queued request
	r = call(t, "GET", "/api/operations/status.cgi", "", nil)
	if r.Code != 200 || r.Body["complete"] != true || r.Body["globalFence"] != "absent" || r.Body["automationPaused"] != false {
		t.Fatalf("status %s", r.Raw)
	}
	row := r.Body["queue"].([]any)[0].(map[string]any)
	if row["requestId"] != rq.RequestID || row["type"] != "server_operation" || row["stage"] != "probe" || row["reason"] != "awaiting_resource" {
		t.Fatalf("row %v", row)
	}
	// run it and cancel the running operation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)
	waitFor(t, "running", func() bool { r, _ := q.Request(rq.RequestID); return r.State == "running" })
	opID := "op-" + rq.RequestID
	r = call(t, "POST", "/api/operations/cancel.cgi", `{"operationId":"`+opID+`"}`, opsHdr())
	if r.Code != 202 || r.Body["cancelRequested"] != true {
		t.Fatalf("cancel %d %s", r.Code, r.Raw)
	}
	waitFor(t, "cancelled", func() bool { r, _ := q.Request(rq.RequestID); return r.State == "cancelled" })
	r = call(t, "POST", "/api/operations/cancel.cgi", `{"operationId":"`+opID+`"}`, opsHdr())
	if r.Code != 202 || r.Body["alreadyFinished"] != true {
		t.Fatalf("already finished %s", r.Raw)
	}
	ops := call(t, "GET", "/api/operations/status.cgi", "", nil).Body["operations"].([]any)
	op := ops[0].(map[string]any)
	if op["state"] != "aborted" || op["ownerStatus"] != "FINISHED" || op["ownerReason"] != "operation_finished" ||
		op["errorCode"] != "CANCELLED" || op["requestId"] != rq.RequestID || op["operationId"] != opID {
		t.Fatalf("op %v", op)
	}
	// queued cancel by requestId
	q.SetPaused(true) // automatic sources stay queued while paused
	rq2, _ := q.Enqueue("servers:quality", "b", "SERVER_CHECK_AUTO", "ctx")
	r = call(t, "POST", "/api/operations/cancel.cgi", `{"requestId":"`+rq2.RequestID+`"}`, opsHdr())
	if r.Code != 202 || r.Body["state"] != "cancelled" || r.Body["requestId"] != rq2.RequestID {
		t.Fatalf("cancel request %d %s", r.Code, r.Raw)
	}
	// automation resume
	r = call(t, "POST", "/api/operations/automation.cgi", `{"paused":false}`, opsHdr())
	if r.Code != 200 || r.Body["ok"] != true || q.Paused() {
		t.Fatal(r.Raw)
	}
	// stop-background with a protected direct operation
	prot := q.StartOperation("", "route_operation", "export", "USER", "cooperative", "")
	r = call(t, "POST", "/api/operations/stop-background.cgi", `{"pauseAutomation":true}`, opsHdr())
	if r.Code != 202 || r.Body["automationPaused"] != true || !q.Paused() {
		t.Fatalf("stop %d %s", r.Code, r.Raw)
	}
	rows := r.Body["operations"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["protected"] != true || rows[0].(map[string]any)["cancelRequested"] != false {
		t.Fatalf("stop rows %v", rows)
	}
	// recover: a live fence blocks (ACTIVE), not retryable for a protected op
	rel, _ := a.d.Lock.TryAcquire("routes", "export", "x")
	r = call(t, "POST", "/api/operations/recover.cgi", `{}`, opsHdr())
	if r.Code != 409 || r.Body["ok"] != false || r.Body["result"] != "ACTIVE" || r.Body["errorCode"] != "RECOVERY_BLOCKED" || r.Body["retryable"] != false {
		t.Fatalf("recover active %d %s", r.Code, r.Raw)
	}
	if s := call(t, "GET", "/api/operations/status.cgi", "", nil).Body; s["globalFence"] != "managed_active" {
		t.Fatal(s["globalFence"])
	}
	a.d.Lock.MarkPending(true)
	rel()
	if r := call(t, "POST", "/api/operations/recover.cgi", `{}`, opsHdr()); r.Body["result"] != "domain_pending" {
		t.Fatal(r.Raw)
	}
	if s := call(t, "GET", "/api/operations/status.cgi", "", nil).Body; s["globalFence"] != "managed_stale" {
		t.Fatal(s["globalFence"])
	}
	a.d.Lock.ClearPending()
	prot.Finish(nil)
	r = call(t, "POST", "/api/operations/recover.cgi", `{}`, opsHdr())
	if r.Code != 202 || r.Body["ok"] != true || r.Body["result"] != "absent" || r.Body["errorCode"] != nil || r.Body["automationPaused"] != true {
		t.Fatalf("recover ok %d %s", r.Code, r.Raw)
	}
	// a running BROray page operation blocks recovery
	a.page.begin(&pageOp{OperationID: "uninstall-20261102101500-1", Operation: "uninstall", State: "running", Running: true})
	if r := call(t, "POST", "/api/operations/recover.cgi", `{}`, opsHdr()); r.Code != 409 || r.Body["result"] != "updater_pending" {
		t.Fatal(r.Raw)
	}
	// events
	r = call(t, "GET", "/api/operations/events.cgi", "", nil)
	evs := r.Body["events"].([]any)
	if r.Code != 200 || r.Body["complete"] != true || len(evs) < 4 {
		t.Fatalf("events %s", r.Raw)
	}
	ev := evs[0].(map[string]any)
	if ev["event"] != "started" || ev["message"] != "Операция запущена" || ev["operationType"] != "server_operation" || ev["sequence"] != float64(1) {
		t.Fatalf("event %v", ev)
	}
	// report
	r = call(t, "GET", "/api/operations/report.cgi", "", nil)
	if r.Code != 200 || r.Body["reportKind"] != "broray-diagnostics" || r.Body["complete"] != false ||
		r.Header.Get("Content-Disposition") != `attachment; filename="BROray-diagnostics.json"` {
		t.Fatalf("report %d %s", r.Code, r.Raw)
	}
	if f := r.Body["fences"].(map[string]any); f["updaterRequest"] != "present" || f["global"] != "absent" {
		t.Fatalf("fences %v", f)
	}
	if !strings.Contains(r.Body["summary"].(string), "фоновых служб проверены") {
		t.Fatal(r.Body["summary"])
	}
	if r.Body["runtimeDetails"].(map[string]any)["updater"].(map[string]any)["errorCode"] != "UPDATER_STATUS_UNAVAILABLE" {
		t.Fatal("updater facts")
	}
	if r := call(t, "GET", "/api/operations/report.cgi?x", "", nil); r.Code != 400 {
		t.Fatal(r.Code)
	}
}

func TestReportAutomationFacts(t *testing.T) {
	a := testArea(t)
	writeFile(t, a.d.Path("config", "system", "server-auto-switch.json"), []byte(`{"enabled":true,"qualityRefreshEnabled":false}`), 0)
	writeFile(t, a.d.Path("config", "subscriptions", "a.json"), []byte(`{"enabled":true,"autoUpdateEnabled":true}`), 0)
	writeFile(t, a.d.Path("config", "subscriptions", "b.json"), []byte(`{"enabled":true,"autoUpdateEnabled":false}`), 0)
	m := a.reportAutomation()
	if m["autoSwitch"] != true || m["serverCheck"] != false || m["subscriptionUpdate"] != true || m["complete"] != true ||
		m["automaticSubscriptions"] != 1 || m["subscriptionRecordsRead"] != 2 {
		t.Fatalf("%v", m)
	}
	writeFile(t, a.d.Path("config", "subscriptions", "c.json"), []byte(`{"enabled":"yes"}`), 0)
	if m := a.reportAutomation(); m["complete"] != false || m["subscriptionUpdate"] != nil {
		t.Fatalf("%v", m)
	}
}

func TestJournalGapAndProjection(t *testing.T) {
	dir := t.TempDir()
	lines := `{"sequence":1,"event":"started","operationId":"op-20261102101955-4711-a1b2c3d4e5f6","operationType":"servers:quality","source":"USER","result":"pending","timestamp":"2026-11-02T10:19:55Z","pid":4711,"errorCode":"SOMETHING"}
{"sequence":2,"event":"weird","operationId":"bad","source":"ME","result":"meh","timestamp":"x"}
{"sequence":4,"event":"completed","result":"success"}
`
	writeFile(t, filepath.Join(dir, "events.jsonl"), []byte(lines), 0)
	j := compat.NewJournal(dir)
	ev := j.Events()
	if ev["complete"] != false || ev["errors"].([]string)[0] != "JOURNAL_GAP" {
		t.Fatalf("gap not detected: %v", ev)
	}
	evs := ev["events"].([]map[string]any)
	if evs[0]["operationType"] != "server_operation" || evs[0]["errorCode"] != nil || evs[0]["pid"] != int64(4711) {
		t.Fatalf("projection %v", evs[0])
	}
	if evs[1]["event"] != "unknown" || evs[1]["operationId"] != nil || evs[1]["source"] != "UNKNOWN" || evs[1]["result"] != "unknown" ||
		evs[1]["timestamp"] != nil || evs[1]["message"] != "Событие не распознано" {
		t.Fatalf("projection %v", evs[1])
	}
}

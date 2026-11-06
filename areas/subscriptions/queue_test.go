package subscriptions

import (
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func queueHeaders() map[string]string {
	return map[string]string{"X-BROray-Queue": "1", "X-BROray-Request": "operations", "Origin": "http://example.com",
		"Content-Type": "application/json"}
}

func TestQueueRefresh(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, linkA+"\n"+linkC+"\n")
	id := e.create(t, p.URL+"/q", "")
	nonce := strings.Repeat("ab", 16)
	bad := []struct {
		body, query string
		hdr         map[string]string
		status      int
		code        string
	}{
		{`{"id":"` + id + `","nonce":"` + nonce + `"}`, "?id=x", queueHeaders(), 400, "INVALID_REQUEST"},
		{`{"id":"` + id + `"}`, "", queueHeaders(), 400, "INVALID_REQUEST"},
		{`{"id":"-x","nonce":"` + nonce + `"}`, "", queueHeaders(), 400, "INVALID_REQUEST"},
		{`{"id":"sub-0000000000000000","nonce":"` + nonce + `"}`, "", queueHeaders(), 404, "SUBSCRIPTION_NOT_FOUND"},
		{`{"id":"` + id + `","nonce":"` + nonce + `"}`, "", map[string]string{"X-BROray-Queue": "1"}, 403, "CSRF_REJECTED"},
	}
	for _, b := range bad {
		r := call(t, "POST", "/api/subscriptions/refresh.cgi"+b.query, b.body, b.hdr)
		if r.Status != b.status || r.errCode() != b.code {
			t.Errorf("%s: %d %s", b.body, r.Status, r.Raw)
		}
	}
	before, _ := e.s.recordHash(id)
	r := call(t, "POST", "/api/subscriptions/refresh.cgi", `{"id":"`+id+`","nonce":"`+nonce+`"}`, queueHeaders())
	stop := e.startWorker()
	if r.Status != 202 || r.Body["ok"] != true || r.Body["requestId"] != "q-"+nonce || r.Body["priority"] != float64(2) || r.Body["accepted"] != true {
		t.Fatalf("submit: %d %s", r.Status, r.Raw)
	}
	waitFor(t, "request", func() bool {
		q, _ := e.d.Queue.Request("q-" + nonce)
		return q.State == "completed" || q.State == "failed"
	})
	q, _ := e.d.Queue.Request("q-" + nonce)
	rec := e.record(t, id)
	if q.State != "completed" || rec["lastUpdateStatus"] != "success" || rec["backgroundOperationId"] != "op-q-"+nonce {
		t.Fatalf("state %s %s %v", q.State, q.Message, rec)
	}
	if len(liveIDs(t, e)) != 2 {
		t.Fatal("servers not synced")
	}
	_ = before

	// record changed between enqueue and apply → failed, record untouched
	p.setBlock(make(chan struct{}))
	stop()
	nonce2 := strings.Repeat("cd", 16)
	r = call(t, "POST", "/api/subscriptions/refresh.cgi", `{"id":"`+id+`","nonce":"`+nonce2+`"}`, queueHeaders())
	stop = e.startWorker()
	if r.Status != 202 {
		t.Fatalf("%s", r.Raw)
	}
	waitFor(t, "fetching", func() bool {
		q, _ := e.d.Queue.Request("q-" + nonce2)
		return q.State == "running" && q.Stage == "fetch"
	})
	// during fetch the record is not modified and the fence is free
	if e.record(t, id)["lastUpdateStatus"] != "success" || e.d.Lock.Busy() {
		t.Fatal("fetch stage touched record or fence")
	}
	if r := call(t, "POST", "/api/subscriptions/update.cgi?id="+id, `{"name":"Changed"}`, nil); r.Status != 200 {
		t.Fatalf("%s", r.Raw)
	}
	changed := e.record(t, id)
	close(p.block)
	waitFor(t, "request2", func() bool { q, _ := e.d.Queue.Request("q-" + nonce2); return q.State == "failed" })
	if after := e.record(t, id); after["updatedAt"] != changed["updatedAt"] || after["lastUpdateStatus"] != "success" {
		t.Fatal("record touched by stale request")
	}
	q, _ = e.d.Queue.Request("q-" + nonce2)
	if q.ErrorCode != "SUBSCRIPTION_CHANGED" {
		t.Fatalf("code %s", q.ErrorCode)
	}

	// fetch failure is persisted by the apply stage as a normal failed update
	p.set("")
	p.setStatus(404)
	stop()
	nonce3 := strings.Repeat("ef", 16)
	call(t, "POST", "/api/subscriptions/refresh.cgi", `{"id":"`+id+`","nonce":"`+nonce3+`"}`, queueHeaders())
	stop = e.startWorker()
	waitFor(t, "request3", func() bool { q, _ := e.d.Queue.Request("q-" + nonce3); return q.State == "failed" })
	rec = e.record(t, id)
	if rec["lastUpdateStatus"] != "error" || rec["lastError"] != "Сервер подписки вернул HTTP 404." ||
		rec["lastUpdateResult"].(map[string]any)["errorCode"] != "HTTP_ERROR" || len(liveIDs(t, e)) != 2 {
		t.Fatalf("failed update %v", rec)
	}
	q, _ = e.d.Queue.Request("q-" + nonce3)
	if q.Message != "Сервер подписки вернул HTTP 404." {
		t.Fatalf("message %q", q.Message)
	}
}

func TestQueueCancelWhileFetching(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, linkA+"\n")
	p.setBlock(make(chan struct{}))
	defer close(p.block)
	id := e.create(t, p.URL+"/q", "")
	nonce := strings.Repeat("12", 16)
	call(t, "POST", "/api/subscriptions/refresh.cgi", `{"id":"`+id+`","nonce":"`+nonce+`"}`, queueHeaders())
	stop := e.startWorker()
	defer stop()
	waitFor(t, "fetching", func() bool { q, _ := e.d.Queue.Request("q-" + nonce); return q.State == "running" })
	if _, err := e.d.Queue.CancelOperation("op-q-" + nonce); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "cancelled", func() bool { q, _ := e.d.Queue.Request("q-" + nonce); return q.State == "cancelled" })
	if e.record(t, id)["lastUpdateStatus"] != "never" {
		t.Fatal("record changed by a cancelled preparation")
	}
}

func TestScheduler(t *testing.T) {
	e := newEnv(t)
	due := e.create(t, "https://a.example/s", `,"updateIntervalMinutes":5`)
	manual := e.create(t, "https://b.example/s", `,"autoUpdateEnabled":false`)
	off := e.create(t, "https://c.example/s", `,"enabled":false`)
	_, _ = manual, off
	if n := e.s.enqueueDue(); n != 0 {
		t.Fatalf("nothing due yet: %d", n)
	}
	old := now
	now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	defer func() { now = old }()
	if n := e.s.enqueueDue(); n != 1 {
		t.Fatalf("due: %d", n)
	}
	r, ok := e.d.Queue.FindLive(actionRefresh, due)
	h, _ := e.s.recordHash(due)
	if !ok || r.Source != "SUBSCRIPTION_AUTO" || r.Priority != 4 || r.Context != h {
		t.Fatalf("request %+v", r)
	}
	// coalesced: a second tick reuses the live request
	e.s.enqueueDue()
	rows := 0
	for _, q := range e.d.Queue.QueueRows() {
		if q["state"] == "queued" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("rows %d", rows)
	}
	if err := e.d.Queue.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.Queue.CancelRequest(r.RequestID); err != nil {
		t.Fatal(err)
	}
	if n := e.s.enqueueDue(); n != 0 {
		t.Fatalf("paused: %d", n)
	}
	var _ = compat.Now
}

func TestQueueHwidPersistedAndInvalidRecords(t *testing.T) {
	e := newEnv(t)
	p := newProvider(t, linkA+"\n")
	id := e.create(t, p.URL+"/h", "")
	rec := e.record(t, id)
	delete(rec, "clientHwid")
	if err := e.s.writeRecord(rec); err != nil {
		t.Fatal(err)
	}
	nonce := strings.Repeat("9a", 16)
	if r := call(t, "POST", "/api/subscriptions/refresh.cgi", `{"id":"`+id+`","nonce":"`+nonce+`"}`, queueHeaders()); r.Status != 202 {
		t.Fatalf("%s", r.Raw)
	}
	stop := e.startWorker()
	defer stop()
	waitFor(t, "done", func() bool { q, _ := e.d.Queue.Request("q-" + nonce); return q.State == "completed" })
	if got := e.record(t, id)["clientHwid"]; got == nil || got != p.header(0).Get("X-Hwid") {
		t.Fatalf("hwid %v vs %s", got, p.header(0).Get("X-Hwid"))
	}
	// invalid files: skipped by list/summary, details answers the generic error
	writeFile(t, e.s.recPath("sub-broken"), "{not json")
	writeFile(t, e.s.recPath("sub-invalid"), `{"schemaVersion":2,"id":"sub-invalid"}`)
	if l := call(t, "GET", "/api/subscriptions/list.cgi", "", nil).Body["data"].([]any); len(l) != 1 {
		t.Fatalf("list %v", l)
	}
	if s := e.s.summary(); s["total"] != 1 {
		t.Fatalf("summary %v", s["total"])
	}
	if r := call(t, "GET", "/api/subscriptions/details.cgi?id=sub-broken", "", nil); r.Status != 400 || r.errCode() != "SUBSCRIPTION_OPERATION_FAILED" {
		t.Fatalf("%s", r.Raw)
	}
	if r := call(t, "GET", "/api/subscriptions/details.cgi?id=sub-invalid", "", nil); r.Status != 200 || r.data()["displayUrl"] != "***" {
		t.Fatalf("%s", r.Raw)
	}
}

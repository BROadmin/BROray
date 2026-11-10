package servers

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func activationRequest(t *testing.T, id, nonce string) resp {
	t.Helper()
	return call(t, "POST", "http://example.com/api/servers/activate.cgi", `{"id":"`+id+`","nonce":"`+nonce+`"}`, map[string]string{
		"X-BROray-Queue": "1", "X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json",
	})
}

// A slow local interface sync must never hold the external HTTP request open.
func TestActivationQueueAcknowledgesBeforeInterfaceCompletes(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://private-test-secret@a.example:443#A")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	e.d.Svc.KeeneticEnsureInterface = func(ctx context.Context) error {
		e.ensured.Add(1)
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	go e.d.Queue.Run(e.d.Ctx)
	nonce := strings.Repeat("ab", 16)
	done := make(chan resp, 1)
	go func() { done <- activationRequest(t, id, nonce) }()
	var r resp
	select {
	case r = <-done:
	case <-time.After(time.Second):
		unblock()
		r = <-done
		t.Fatalf("activation kept HTTP open until interface finished: status=%d", r.Code)
	}
	if r.Code != 202 || r.Body["requestId"] != "q-"+nonce {
		t.Fatalf("receipt: %d %s", r.Code, r.Raw)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("activation did not start")
	}
	replay := activationRequest(t, id, nonce)
	if replay.Code != 202 || replay.Body["requestId"] != r.Body["requestId"] {
		t.Fatalf("replay: %s", replay.Raw)
	}
	unblock()
	waitReq(t, e, "q-"+nonce, "completed")
	replay = activationRequest(t, id, nonce)
	if replay.Code != 202 || replay.Body["requestId"] != r.Body["requestId"] || e.ensured.Load() != 1 || ActiveID(e.d) != id {
		t.Fatal("replay duplicated activation or lost active target")
	}
	q, _ := e.d.Queue.Request("q-" + nonce)
	if strings.Contains(q.Message, "private-test-secret") {
		t.Fatal("private value in receipt")
	}
}

func TestActivationQueueRejectsChangedRecordBeforeApply(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	nonce := strings.Repeat("cd", 16)
	r := activationRequest(t, id, nonce)
	if r.Code != 202 {
		t.Fatalf("receipt: %d %s", r.Code, r.Raw)
	}
	rec, err := ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	rec["password"] = "changed"
	if err := WriteRecord(e.d, rec); err != nil {
		t.Fatal(err)
	}
	go e.d.Queue.Run(e.d.Ctx)
	waitReq(t, e, "q-"+nonce, "failed")
	q, _ := e.d.Queue.Request("q-" + nonce)
	if q.ErrorCode != "SERVER_CATALOG_CHANGED" || e.running.Load() || ActiveID(e.d) != "" {
		t.Fatalf("changed input applied: %s", q.ErrorCode)
	}
}

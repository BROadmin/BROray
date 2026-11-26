package subscriptions

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

func sourceQueueOwner(t *testing.T, e *env) {
	t.Helper()
	if e.daemonGuard != nil {
		return
	}
	guard, err := platform.TryLock(e.d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Release(); err != nil {
			t.Error(err)
		}
	})
	if err = e.d.Lock.ReconcileDaemonReserve(e.d.Root, guard, false, nil); err != nil {
		t.Fatal(err)
	}
	e.daemonGuard = guard
}

func submitSourceRemoval(t *testing.T, id, nonce string) resp {
	t.Helper()
	return call(t, "POST", "/api/subscriptions/delete.cgi", `{"id":"`+id+`","nonce":"`+nonce+`"}`, queueHeaders())
}

// The queue/fence/catalog are real. Preparation is deliberately a callback
// boundary; the actual-core owner tests provide separate runtime evidence.
func TestQueuedSourceRemovalCatalogCommit(t *testing.T) {
	for _, kind := range []string{"replace", "withdraw", "inactive", "disabled", "owner-failure", "pending", "source-drift", "candidate-drift", "active-drift"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			sourceQueueOwner(t, e)
			sub := e.create(t, "https://example.com/source", "")
			if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@b.example:443#B", "trojan://pw@foreign.example:443#Foreign"); err != nil {
				t.Fatal(err)
			}
			var owned []string
			foreign := ""
			images := map[string][]byte{}
			ownedPaths := map[string]bool{}
			for _, f := range e.s.liveServers() {
				isForeign := f.rec["address"] == "foreign.example" && kind != "withdraw"
				sourceOf(f.rec)["subscriptionId"] = sub
				if isForeign {
					sourceOf(f.rec)["subscriptionId"] = "sub-00000000000000ff"
					foreign = f.id
				} else {
					owned = append(owned, f.id)
				}
				if err := servers.WriteRecord(e.d, f.rec); err != nil {
					t.Fatal(err)
				}
				path := f.path
				if kind == "disabled" && !isForeign {
					path = filepath.Join(e.s.disabledDir(sub), filepath.Base(path))
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(f.path, path); err != nil {
						t.Fatal(err)
					}
				}
				quality := servers.QualityPath(e.d, f.id)
				writeFile(t, quality, `{"sample":true}`)
				for _, p := range []string{path, quality} {
					b, err := os.ReadFile(p)
					if err != nil {
						t.Fatal(err)
					}
					images[p] = b
					ownedPaths[p] = !isForeign
				}
			}
			active := owned[0]
			if kind == "inactive" || kind == "disabled" {
				active = foreign
			}
			if err := e.s.writeActive(active); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			e.d.Svc.ServersPrepareSourceRemoval = func(ctx context.Context, id string, removed, candidates []string) (string, error) {
				calls.Add(1)
				holder, ok := e.d.Lock.Current()
				if !compat.FenceHeld(ctx) || !ok || !holder.DaemonOwnedReserve || holder.Action != compat.SourceRemovalAction || id != sub || len(removed) != len(owned) {
					return "", errors.New("wrong source authority")
				}
				if kind == "owner-failure" {
					return "", errors.New("preparation failed")
				}
				if kind == "pending" {
					return "", compat.ErrReserveTransactionPending
				}
				for _, p := range candidates {
					if p != foreign {
						return "", errors.New("candidate from removed source")
					}
				}
				if (foreign == "" && len(candidates) != 0) || (foreign != "" && len(candidates) != 1) {
					return "", errors.New("incomplete candidate snapshot")
				}
				if kind == "source-drift" {
					b, err := os.ReadFile(e.s.recPath(sub))
					if err != nil {
						return "", err
					}
					if err = os.WriteFile(e.s.recPath(sub), append(b, '\n'), 0600); err != nil {
						return "", err
					}
				}
				if kind == "candidate-drift" {
					p := servers.RecordPath(e.d, foreign)
					images[p] = append(images[p], '\n')
					if err := os.WriteFile(p, images[p], 0600); err != nil {
						return "", err
					}
				}
				if kind == "active-drift" {
					return foreign, nil
				}
				if foreign == "" {
					return "", os.Remove(servers.ActiveFile(e.d))
				}
				return foreign, e.s.writeActive(foreign)
			}
			nonce := strings.Repeat("ab", 16)
			response := submitSourceRemoval(t, sub, nonce)
			if response.Status != 202 || response.Body["accepted"] != true {
				t.Fatalf("queued source deletion absent: %d %s", response.Status, response.Raw)
			}
			q, ok := e.d.Queue.Request("q-" + nonce)
			if !ok || q.Target != compat.ReserveOperationTarget || q.Action != compat.SourceRemovalAction {
				t.Fatal("wrong protected queue target", q)
			}
			for p, want := range images {
				got, err := os.ReadFile(p)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("submission mutated catalog", p, err)
				}
			}
			stop := e.startWorker()
			defer stop()
			waitFor(t, "source removal", func() bool {
				q, _ := e.d.Queue.Request("q-" + nonce)
				return q.State == "completed" || q.State == "failed"
			})
			q, _ = e.d.Queue.Request("q-" + nonce)
			success := kind == "replace" || kind == "withdraw" || kind == "inactive" || kind == "disabled"
			if (q.State == "completed") != success || calls.Load() != 1 {
				t.Fatal("source terminal result", kind, q.State, q.ErrorCode, q.Message, calls.Load())
			}
			if exists2(e.s.recPath(sub)) == success {
				t.Fatal("source record commit order")
			}
			for p, want := range images {
				got, err := os.ReadFile(p)
				if success && ownedPaths[p] {
					if !os.IsNotExist(err) {
						t.Fatal("owned server/quality left", p, err)
					}
				} else if err != nil || !bytes.Equal(got, want) {
					t.Fatal("catalog lost on failure/foreign record changed", p, err)
				}
			}
			if kind == "pending" {
				if q.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" || !e.d.Lock.Busy() {
					t.Fatal("pending source fence released", q.ErrorCode)
				}
			} else if e.d.Lock.Busy() {
				t.Fatal("completed/refused source operation retained ordinary fence")
			}
		})
	}
}

func TestQueuedSourceRemovalRequestAndRevision(t *testing.T) {
	e := newEnv(t)
	sourceQueueOwner(t, e)
	sub := e.create(t, "https://example.com/schema", "")
	nonce := strings.Repeat("cd", 16)
	valid := `{"id":"` + sub + `","nonce":"` + nonce + `"}`
	cases := []struct {
		body, query string
		headers     map[string]string
		status      int
	}{
		{valid, "?id=x", queueHeaders(), 400}, {`{"id":"` + sub + `"}`, "", queueHeaders(), 400},
		{`{"id":"` + sub + `","id":"` + sub + `","nonce":"` + nonce + `"}`, "", queueHeaders(), 400},
		{`{"id":"` + sub + `","nonce":"bad"}`, "", queueHeaders(), 400},
		{`{"id":"..","nonce":"` + nonce + `"}`, "", queueHeaders(), 400},
		{`{"id":"` + sub + `","nonce":"` + nonce + `","extra":true}`, "", queueHeaders(), 400},
		{valid, "", map[string]string{"X-BROray-Queue": "1"}, 403},
	}
	for _, tc := range cases {
		r := call(t, "POST", "/api/subscriptions/delete.cgi"+tc.query, tc.body, tc.headers)
		if r.Status != tc.status {
			t.Fatalf("request validation: %d %s", r.Status, r.Raw)
		}
	}
	var calls atomic.Int32
	e.d.Svc.ServersPrepareSourceRemoval = func(context.Context, string, []string, []string) (string, error) { calls.Add(1); return "", nil }
	response := submitSourceRemoval(t, sub, nonce)
	if response.Status != 202 {
		t.Fatalf("queue unavailable: %d %s", response.Status, response.Raw)
	}
	b, err := os.ReadFile(e.s.recPath(sub))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(e.s.recPath(sub), append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	stop := e.startWorker()
	defer stop()
	waitFor(t, "changed source", func() bool { q, _ := e.d.Queue.Request("q-" + nonce); return q.State == "failed" })
	q, _ := e.d.Queue.Request("q-" + nonce)
	if q.ErrorCode != "SUBSCRIPTION_CHANGED" || calls.Load() != 0 || !exists2(e.s.recPath(sub)) {
		t.Fatal("stale source revision executed", q, calls.Load())
	}
}

func TestQueuedSourceRemovalDistinctSources(t *testing.T) {
	e := newEnv(t)
	sourceQueueOwner(t, e)
	a := e.create(t, "https://example.com/a", "")
	b := e.create(t, "https://example.com/b", "")
	e.d.Svc.ServersPrepareSourceRemoval = func(context.Context, string, []string, []string) (string, error) { return "", nil }
	n1, n2 := strings.Repeat("12", 16), strings.Repeat("34", 16)
	for _, v := range []struct{ id, nonce string }{{a, n1}, {b, n2}} {
		r := submitSourceRemoval(t, v.id, v.nonce)
		if r.Status != 202 || r.Body["requestId"] != "q-"+v.nonce {
			t.Fatal("different source coalesced", r.Status, r.Raw)
		}
	}
	q1, _ := e.d.Queue.Request("q-" + n1)
	q2, _ := e.d.Queue.Request("q-" + n2)
	if q1.Context == q2.Context {
		t.Fatal("source identity missing from context")
	}
	stop := e.startWorker()
	defer stop()
	waitFor(t, "both sources", func() bool {
		q1, _ := e.d.Queue.Request("q-" + n1)
		q2, _ := e.d.Queue.Request("q-" + n2)
		return q1.State == "completed" && q2.State == "completed"
	})
	if exists2(e.s.recPath(a)) || exists2(e.s.recPath(b)) {
		t.Fatal("source retained after terminal success")
	}
}

func TestSourceRemovalFinalRescan(t *testing.T) {
	e := newEnv(t)
	sub := e.create(t, "https://example.com/final", "")
	if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A"); err != nil {
		t.Fatal(err)
	}
	for _, f := range e.s.liveServers() {
		sourceOf(f.rec)["subscriptionId"] = sub
		if err := servers.WriteRecord(e.d, f.rec); err != nil {
			t.Fatal(err)
		}
	}
	source, err := readDeletionImage(e.s.recPath(sub), false)
	if err != nil {
		t.Fatal(err)
	}
	active, err := readDeletionImage(servers.ActiveFile(e.d), true)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := e.s.pendingProfileImage(sub)
	if err != nil {
		t.Fatal(err)
	}
	var blocked *subErr
	err = e.s.finishSourceRemoval(sub, source, active, pending)
	if !errors.As(err, &blocked) || blocked.Code != "SERVER_SOURCE_REMOVE_FAILED" || !exists2(e.s.recPath(sub)) {
		t.Fatal("source removed while an owned record remained", err)
	}
}

func TestQueuedSourceRemovalContextValidation(t *testing.T) {
	for _, id := range []string{"..", "-bad", ".hidden"} {
		t.Run(id, func(t *testing.T) {
			e := newEnv(t)
			sourceQueueOwner(t, e)
			nonce := strings.Repeat("de", 16)
			request, _, err := e.d.Queue.Submit(compat.SourceRemovalAction, compat.ReserveOperationTarget, "USER", id+":"+strings.Repeat("a", 64), nonce)
			if err != nil {
				t.Fatal(err)
			}
			stop := e.startWorker()
			defer stop()
			waitFor(t, "invalid source context", func() bool { q, _ := e.d.Queue.Request(request.RequestID); return q.State == "failed" })
			q, _ := e.d.Queue.Request(request.RequestID)
			if q.ErrorCode != "INVALID_REQUEST" {
				t.Fatal("stage accepted invalid source ID for lookup", q.ErrorCode)
			}
		})
	}
}

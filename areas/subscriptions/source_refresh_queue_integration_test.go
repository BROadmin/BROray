package subscriptions

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
)

func TestSourceRefreshPendingSurvivesUpdateAndQueue(t *testing.T) {
	for _, queued := range []bool{false, true} {
		name := "direct-update"
		if queued {
			name = "actual-queue"
		}
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			provider := newProvider(t, "trojan://pw@a.example:443#A\ntrojan://pw@b.example:443#B\n")
			sub := e.create(t, provider.URL+"/source", "")
			if err := e.refreshSync(t, sub); err != nil {
				t.Fatal(err)
			}
			old := ""
			for _, f := range e.s.liveServers() {
				if f.rec["address"] == "a.example" {
					old = f.id
				}
			}
			if old == "" {
				t.Fatal("fixture old source absent")
			}
			if err := e.s.writeActive(old); err != nil {
				t.Fatal(err)
			}
			provider.set("trojan://pw@b.example:443#B\n")
			var calls atomic.Int32
			e.d.Svc.ServersPrepareSourceRefresh = func(ctx context.Context, source string, removed, candidates []string) (string, error) {
				calls.Add(1)
				if !compat.FenceHeld(ctx) || source != sub || len(removed) != 1 || removed[0] != old || len(candidates) != 1 {
					return "", errors.New("wrong refresh owner scope")
				}
				if queued {
					holder, ok := e.d.Lock.Current()
					if !ok || !holder.DaemonOwnedReserve || holder.Action != compat.SourceRefreshAction || holder.BundleID != compat.ReserveOperationTarget {
						return "", errors.New("wrong durable refresh owner")
					}
				}
				if err := e.s.writeActive(candidates[0]); err != nil {
					return "", err
				}
				return "", errors.Join(compat.ErrReserveTransactionPending, errors.New("synthetic uncertainty after selection"))
			}
			if !queued {
				err := e.refreshSync(t, sub)
				assertSourceRefreshPending(t, e, err)
			} else {
				nonce := strings.Repeat("ac", 16)
				reply := call(t, "POST", "/api/subscriptions/refresh.cgi", `{"id":"`+sub+`","nonce":"`+nonce+`"}`, queueHeaders())
				if reply.Status != 202 {
					t.Fatal(reply.Status, reply.Raw)
				}
				stop := e.startWorker()
				waitFor(t, "pending refresh terminal", func() bool {
					r, _ := e.d.Queue.Request("q-" + nonce)
					return r.State == "failed" || r.State == "completed"
				})
				stop()
				request, _ := e.d.Queue.Request("q-" + nonce)
				if request.State != "failed" {
					t.Fatal("uncertain refresh reported completed", request.State)
				}
				holder, ok := e.d.Lock.Current()
				if !ok || !holder.DaemonOwnedReserve || holder.Action != compat.SourceRefreshAction || holder.BundleID != compat.ReserveOperationTarget {
					t.Fatal("queue released pending source owner")
				}
				assertSourceRefreshPending(t, e, compat.ErrReserveTransactionPending)
			}
			if calls.Load() != 1 || servers.ActiveID(e.d) == old || len(liveIDs(t, e)) != 2 {
				t.Fatal("pending update rolled back selection or removed recovery records")
			}
			if _, err := servers.ReadRecord(e.d, old); err != nil {
				t.Fatal("old profile removed before successful owner confirmation", err)
			}
			if _, err := servers.ReadRecord(e.d, servers.ActiveID(e.d)); err != nil {
				t.Fatal("current selected profile absent", err)
			}
		})
	}
}

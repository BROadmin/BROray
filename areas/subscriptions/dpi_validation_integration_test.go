package subscriptions

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type dpiTestLease struct {
	check  func() error
	closed bool
}

func (l *dpiTestLease) Check(context.Context) error {
	if l.closed {
		return errors.New("closed")
	}
	if l.check != nil {
		return l.check()
	}
	return nil
}
func (l *dpiTestLease) Close() error { l.closed = true; return nil }
func TestDPISourceChangedValidatedBeforeCommit(t *testing.T) {
	for _, kind := range []string{"healthy", "unhealthy", "unknown", "nil-lease", "pending", "record-drift", "lease-drift", "prepare-drift", "input-drift"} {
		t.Run(kind, func(t *testing.T) {
			e, id := dpiSourceFixture(t)
			path := servers.RecordPath(e.d, id)
			old, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			stale := stageLinks(t, true, "trojan://stale@stale.example:443#Stale")[0]
			if err = servers.WriteRecord(e.d, stale); err != nil {
				t.Fatal(err)
			}
			staleID := stale["id"].(string)
			lease := &dpiTestLease{}
			calls, prepared := 0, 0
			var exact []byte
			e.d.Svc.ServersValidateSourceDPIProposals = func(ctx context.Context, source string, proposals []compat.DPIProposal) (compat.DPIValidation, error) {
				calls++
				if !compat.FenceHeld(ctx) || source != subX || len(proposals) != 1 || proposals[0].Path != path || proposals[0].PreviousSHA256 != compat.SHA256Hex(old) {
					t.Fatal("proposal authority", source, len(proposals))
				}
				exact = bytes.Clone(proposals[0].Record)
				markers, er := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
				if er != nil || len(markers) != 0 {
					t.Fatal("marker before validation", markers, er)
				}
				got, er := os.ReadFile(path)
				if er != nil || !bytes.Equal(old, got) || !servers.RecordExists(e.d, staleID) {
					t.Fatal("catalog changed before validation", er)
				}
				switch kind {
				case "unhealthy":
					return nil, compat.ErrDPIProposalUnhealthy
				case "unknown":
					return nil, errors.New("unconfirmed")
				case "pending":
					return nil, compat.ErrReserveTransactionPending
				case "nil-lease":
					return nil, nil
				case "record-drift":
					if er = os.WriteFile(path, append(old, ' '), 0600); er != nil {
						t.Fatal(er)
					}
				case "lease-drift":
					lease.check = func() error { return errors.New("dependency changed") }
				case "input-drift":
					proposals[0].Record[0] = ' '
					lease.check = func() error { return errors.New("candidate changed") }
				}
				return lease, nil
			}
			e.d.Svc.ServersPrepareSourceRefreshCatalog = func(context.Context, string, []string) error {
				prepared++
				if kind == "prepare-drift" {
					lease.check = func() error { return errors.New("core changed during preparation") }
				}
				return nil
			}
			result, err := e.sync(t, "replace", dpiSourceLink("changed"))
			if calls != 1 {
				t.Fatal("validator not called", calls, err)
			}
			if kind == "healthy" {
				if err != nil || result == nil || prepared != 1 {
					t.Fatal("validated refresh failed", result, err, prepared)
				}
				after, er := os.ReadFile(path)
				if er != nil || !bytes.Equal(after, exact) || bytes.Equal(after, old) || servers.RecordExists(e.d, staleID) {
					t.Fatal("validated bytes/vanished pruning", er)
				}
			} else {
				if err == nil || result != nil {
					t.Fatal("unconfirmed refresh succeeded", kind)
				}
				if kind != "prepare-drift" && prepared != 0 {
					t.Fatal("owner prepared before validation")
				}
				if kind == "pending" && !errors.Is(err, compat.ErrReserveTransactionPending) {
					t.Fatal("pending lost", err)
				}
				if kind == "unhealthy" && !errors.Is(err, compat.ErrDPIProposalUnhealthy) {
					t.Fatal("unhealthy lost", err)
				}
				if !servers.RecordExists(e.d, staleID) {
					t.Fatal("pruned before validation")
				}
				if kind != "record-drift" {
					after, er := os.ReadFile(path)
					if er != nil || !bytes.Equal(old, after) {
						t.Fatal("old image changed", er)
					}
				}
			}
			returned := kind != "unhealthy" && kind != "unknown" && kind != "pending" && kind != "nil-lease"
			if returned && !lease.closed {
				t.Fatal("lease leaked")
			}
			markers, er := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
			if er != nil {
				t.Fatal(er)
			}
			if kind == "prepare-drift" {
				if len(markers) != 1 || !errors.Is(err, compat.ErrReserveTransactionPending) {
					t.Fatal("uncertain dependencies lost backup", err, markers)
				}
			} else if len(markers) != 0 {
				t.Fatal("early refusal left marker", kind, markers)
			}
		})
	}
}

func TestDPISourceValidationCleanupRetainsActualQueueOwner(t *testing.T) {
	e := newEnv(t)
	provider := newProvider(t, dpiSourceLink("old")+"\n")
	sub := e.create(t, provider.URL+"/source", "")
	if err := e.refreshSync(t, sub); err != nil {
		t.Fatal(err)
	}
	id := liveIDs(t, e)[0]
	rec, err := servers.ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	dpiSourceManaged(rec)
	if err = servers.WriteRecord(e.d, rec); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(servers.RecordPath(e.d, id))
	if err != nil {
		t.Fatal(err)
	}
	provider.set(dpiSourceLink("changed") + "\n")
	var calls atomic.Int32
	e.d.Svc.ServersValidateSourceDPIProposals = func(ctx context.Context, source string, proposals []compat.DPIProposal) (compat.DPIValidation, error) {
		calls.Add(1)
		holder, ok := e.d.Lock.Current()
		if !ok || !compat.FenceHeld(ctx) || source != sub || len(proposals) != 1 || !holder.DaemonOwnedReserve || holder.Action != compat.SourceRefreshAction {
			t.Fatal("wrong validation queue owner")
		}
		if err := os.Mkdir(filepath.Join(e.d.Root.Tmp(), "reserve-probe-retained"), 0700); err != nil {
			t.Fatal(err)
		}
		return nil, errors.Join(errors.New("synthetic cleanup uncertainty"), compat.ErrReserveTransactionPending)
	}
	nonce := strings.Repeat("dd", 16)
	reply := call(t, "POST", "/api/subscriptions/refresh.cgi", `{"id":"`+sub+`","nonce":"`+nonce+`"}`, queueHeaders())
	if reply.Status != 202 {
		t.Fatal(reply.Status, reply.Raw)
	}
	stop := e.startWorker()
	waitFor(t, "DPI pending terminal", func() bool {
		r, _ := e.d.Queue.Request("q-" + nonce)
		return r.State == "failed" || r.State == "completed"
	})
	stop()
	request, _ := e.d.Queue.Request("q-" + nonce)
	holder, ok := e.d.Lock.Current()
	if calls.Load() != 1 || request.State != "failed" || !ok || !holder.DaemonOwnedReserve || holder.Action != compat.SourceRefreshAction {
		t.Fatal("pending DPI released queue", calls.Load(), request.State, holder)
	}
	after, err := os.ReadFile(servers.RecordPath(e.d, id))
	if err != nil || !bytes.Equal(after, old) {
		t.Fatal("unvalidated candidate saved", err)
	}
	markers, err := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
	if err != nil || len(markers) != 0 {
		t.Fatal("source marker before native validation", markers, err)
	}
}

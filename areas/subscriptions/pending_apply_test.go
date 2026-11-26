package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
)

// The real coordinator and catalog write/rollback pipeline are exercised
// here. The native core/VPN owner is tested separately with real payloads.
func pendingProfileQueueFixture(t *testing.T) (*env, string, []byte, []byte, string, []byte, []byte) {
	t.Helper()
	e, id := activeReplacementFixture(t)
	e.d.App = app.New(e.d.Root, platform.DefaultConfig(), e.d.Log)
	cfg := []byte("{\"fixture\":true}\n")
	if err := os.WriteFile(e.d.Root.XrayConfig(), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	source := e.record(t, e.create(t, "https://pending.example/source", ""))
	source["id"] = subX
	if err := e.s.writeRecord(source); err != nil {
		t.Fatal(err)
	}
	sourceImage, err := readDeletionImage(e.s.recPath(subX), false)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := pendingSourceAuthority(sourceImage, subX)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(servers.RecordPath(e.d, id))
	if err != nil {
		t.Fatal(err)
	}
	next, err := servers.ParseLink("trojan://rotated@a.example:443#A", id, map[string]any{"type": "subscription", "subscriptionId": subX})
	if err != nil {
		t.Fatal(err)
	}
	proposed, err := servers.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	doc := pendingActiveProfile{SchemaVersion: 1, SourceID: subX, ServerID: id, SourceSHA256: auth,
		ActiveSHA256: compat.SHA256Hex(before), CandidateSHA256: compat.SHA256Hex(proposed), Candidate: proposed}
	raw, err := compat.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.s.subDir(), ".pending-active-"+compat.SHA256Hex([]byte(subX))+".json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return e, id, cfg, before, path, raw, proposed
}

func TestPendingProfileManualApplyQueue(t *testing.T) {
	e, id, cfg, before, path, raw, proposed := pendingProfileQueueFixture(t)
	revision := compat.SHA256Hex(raw)
	invoked := 0
	e.d.Svc.ServersApplyPendingSourceProfile = func(ctx context.Context, proposal compat.SourceProfileApply, op *compat.Operation) error {
		invoked++
		current, err := os.ReadFile(servers.RecordPath(e.d, id))
		if err != nil || !bytes.Equal(current, proposed) || !compat.FenceHeld(ctx) || op == nil ||
			proposal.ServerID != id || proposal.SourceID != subX || proposal.ConfigSHA256 != compat.SHA256Hex(cfg) ||
			proposal.CandidateSHA256 != compat.SHA256Hex(proposed) || proposal.PreviousName != "A" ||
			!bytes.Equal(proposal.PreviousRecord, before) {
			t.Error("incorrect protected catalog/runtime handoff", err)
		}
		if invoked == 1 {
			return compat.ErrSourceProfileRolledBack
		}
		if err := os.MkdirAll(e.d.Path("config", "system"), 0700); err != nil {
			return err
		}
		receipt := []byte(`{"owner":"fixture"}`)
		if err := os.WriteFile(e.d.Path("config", "interface.json"), receipt, 0600); err != nil {
			return err
		}
		active, err := os.ReadFile(servers.ActiveFile(e.d))
		if err != nil {
			return err
		}
		j, err := reserve.OpenTransactionJournal(e.d.Root)
		if err != nil {
			return err
		}
		defer j.Close()
		tx, err := j.Begin(ctx, reserve.TransactionFiles{Config: cfg, Active: active}, reserve.TransactionIntent{
			Action: reserve.SourceProfileTransactionAction, Authority: proposal.RecoveryAuthority, InterfaceName: "Proxy1",
			PreviousDescription: "BROray - A", PreviousDescriptionLine: `    description "BROray - A"`,
			NextDescription: "BROray - A", Upstream: "127.0.0.1:2080", ReceiptSHA256: compat.SHA256Hex(receipt),
		})
		if err != nil {
			return err
		}
		return j.ConfirmSourceProfile(ctx, tx, func(context.Context, reserve.TransactionRecord) error { return nil })
	}
	e.d.Svc.ServersPrepareSourceRefreshCatalog = func(context.Context, string, []string) error {
		t.Error("explicit active apply must not disable reserve through catalog preparation")
		return nil
	}
	submit := func(nonce, rev string) resp {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"id": subX, "revision": rev, "nonce": nonce})
		return call(t, "POST", "/api/subscriptions/apply-profile.cgi", string(body), queueHeaders())
	}
	await := func(nonce string) compat.Request {
		t.Helper()
		waitFor(t, "manual profile request", func() bool {
			r, _ := e.d.Queue.Request("q-" + nonce)
			return r != nil && (r.State == "completed" || r.State == "failed")
		})
		r, _ := e.d.Queue.Request("q-" + nonce)
		return *r
	}
	if r := submit(strings.Repeat("1", 32), strings.Repeat("1", 64)); r.Status != 409 {
		t.Fatalf("stale form revision status %d", r.Status)
	}
	first := strings.Repeat("2", 32)
	if r := submit(first, revision); r.Status != 202 {
		t.Fatalf("manual apply admission %d", r.Status)
	}
	for _, other := range e.s.liveServers() {
		if other.id != id {
			if err := e.s.writeActive(other.id); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := e.s.writeActive(id); err != nil {
		t.Fatal(err)
	}
	stop := e.startWorker()
	defer stop()
	if r := await(first); r.State != "failed" || r.ErrorCode != "SUBSCRIPTION_PROFILE_CHANGED" || invoked != 0 {
		t.Fatalf("queued A-B-A stale authority: %s %s", r.State, r.ErrorCode)
	}
	second := strings.Repeat("3", 32)
	if r := submit(second, revision); r.Status != 202 {
		t.Fatalf("fresh admission after stale request %d", r.Status)
	}
	if r := await(second); r.State != "failed" || r.ErrorCode != "SUBSCRIPTION_PROFILE_VPN_ROLLED_BACK" {
		t.Fatalf("confirmed rollback: %s %s", r.State, r.ErrorCode)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("failed apply did not restore pending receipt", err)
	}
	got, err = os.ReadFile(servers.RecordPath(e.d, id))
	if err != nil || !bytes.Equal(got, before) {
		t.Fatal("failed apply did not restore working record", err)
	}
	third := strings.Repeat("4", 32)
	if r := submit(third, revision); r.Status != 202 {
		t.Fatalf("fresh admission after rollback %d", r.Status)
	}
	if r := await(third); r.State != "completed" || invoked != 2 {
		t.Fatalf("completed request: %s %s", r.State, r.ErrorCode)
	}
	got, err = os.ReadFile(servers.RecordPath(e.d, id))
	if err != nil || !bytes.Equal(got, proposed) {
		t.Fatal("confirmed apply did not publish exact candidate", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("confirmed apply did not consume receipt", err)
	}
	if r := submit(third, revision); r.Status != 202 || r.Body["requestId"] != "q-"+third || r.Body["state"] != "completed" || invoked != 2 {
		t.Fatal("lost response retry did not reuse its terminal receipt")
	}
	if r := submit(strings.Repeat("5", 32), revision); r.Status != 409 {
		t.Fatal("new request reused a consumed revision")
	}
}

func TestPendingProfileScheduledApplyQueue(t *testing.T) {
	for _, mode := range []string{"changed settings", "expired", "once per UTC day"} {
		t.Run(mode, func(t *testing.T) {
			e, _, _, _, path, raw, _ := pendingProfileQueueFixture(t)
			var clock atomic.Int64
			day := time.Date(2026, time.November, 24, 7, 21, 20, 0, time.UTC)
			clock.Store(day.Unix())
			savedNow := now
			now = func() time.Time { return time.Unix(clock.Load(), 0) }
			defer func() { now = savedNow }()
			if _, err := e.s.updateSettings(subX, map[string]any{"applyProfileAtUTC": "7:21"}); err == nil {
				t.Fatal("noncanonical apply time was accepted")
			}
			scheduler, ok := any(e.s).(interface{ enqueueDueProfileApply() *compat.Request })
			if !ok {
				t.Fatal("missing protected schedule admission")
			}
			if r := scheduler.enqueueDueProfileApply(); r != nil {
				t.Fatal("legacy subscription scheduled an automatic apply")
			}
			set := func(at string) {
				t.Helper()
				dst, err := e.s.updateSettings(subX, map[string]any{"applyProfileAtUTC": at})
				if err != nil || dst.(map[string]any)["applyProfileAtUTC"] != at {
					t.Fatalf("schedule was not saved or projected: %v", err)
				}
			}
			set("07:21")
			if err := e.d.Queue.SetPaused(true); err != nil {
				t.Fatal(err)
			}
			if r := scheduler.enqueueDueProfileApply(); r != nil {
				t.Fatal("paused queue scheduled apply")
			}
			if err := e.d.Queue.SetPaused(false); err != nil {
				t.Fatal(err)
			}
			invoked := 0
			e.d.Svc.ServersApplyPendingSourceProfile = func(_ context.Context, _ compat.SourceProfileApply, op *compat.Operation) error {
				invoked++
				if op == nil || op.Source != "SCHEDULER" {
					t.Error("automatic owner source was not retained")
				}
				return compat.ErrSourceProfileRolledBack
			}
			r := scheduler.enqueueDueProfileApply()
			if r == nil || r.Source != "SCHEDULER" || scheduler.enqueueDueProfileApply() != nil {
				t.Fatal("schedule did not admit exactly one protected pending request")
			}
			switch mode {
			case "changed settings":
				set("07:25")
				set("07:21")
			case "expired":
				clock.Store(day.Add(time.Minute).Unix())
			}
			stop := e.startWorker()
			defer stop()
			waitFor(t, "scheduled profile attempt", func() bool {
				q, _ := e.d.Queue.Request(r.RequestID)
				return q != nil && q.State == "failed"
			})
			terminal, _ := e.d.Queue.Request(r.RequestID)
			if mode != "once per UTC day" {
				if terminal.ErrorCode != "SUBSCRIPTION_PROFILE_WINDOW_EXPIRED" || invoked != 0 {
					t.Fatalf("stale settings or minute reached native apply: %s", terminal.ErrorCode)
				}
			} else {
				if invoked != 1 || terminal.ErrorCode != "SUBSCRIPTION_PROFILE_VPN_ROLLED_BACK" {
					t.Fatalf("confirmed rollback result: %s", terminal.ErrorCode)
				}
				reopened := newSvc(e.d)
				reader := any(reopened).(interface{ enqueueDueProfileApply() *compat.Request })
				set("")
				set("07:21")
				if reader.enqueueDueProfileApply() != nil {
					t.Fatal("schedule settings recreation replayed the same UTC day")
				}
				clock.Store(day.Add(-24 * time.Hour).Unix())
				if reader.enqueueDueProfileApply() != nil {
					t.Fatal("backward clock replayed an automatic attempt")
				}
				clock.Store(day.Add(24 * time.Hour).Unix())
				next := reader.enqueueDueProfileApply()
				if next == nil {
					t.Fatal("next configured UTC day did not admit an attempt")
				}
				waitFor(t, "next UTC day", func() bool {
					q, _ := e.d.Queue.Request(next.RequestID)
					return q != nil && q.State == "failed"
				})
				if invoked != 2 {
					t.Fatal("next day was not independently applied")
				}
			}
			restored, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(restored, raw) {
				t.Fatal("skipped or rolled-back schedule lost pending revision", err)
			}
		})
	}
}

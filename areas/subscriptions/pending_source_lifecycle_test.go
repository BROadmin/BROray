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
)

func TestPendingProfileSourceLifecycle(t *testing.T) {
	for _, scenario := range []string{"delete", "owner refused", "unexpected link"} {
		t.Run(scenario, func(t *testing.T) {
			e, active, _, working, path, pending, _ := pendingProfileQueueFixture(t)
			other := filepath.Join(e.s.subDir(), ".pending-active-"+compat.SHA256Hex([]byte("unrelated"))+".json")
			writeFile(t, other, "unrelated pending")
			if scenario == "unexpected link" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, path); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			e.d.Svc.ServersPrepareSourceRemoval = func(ctx context.Context, id string, removed, candidates []string) (string, error) {
				calls.Add(1)
				if !compat.FenceHeld(ctx) || id != subX || len(removed) < 1 || len(candidates) != 0 {
					return "", errors.New("wrong source or deletion authority")
				}
				if scenario == "owner refused" {
					return "", errors.New("runtime transition unconfirmed")
				}
				return "", os.Remove(servers.ActiveFile(e.d))
			}
			nonce := strings.Repeat("96", 16)
			if r := submitSourceRemoval(t, subX, nonce); r.Status != 202 {
				t.Fatalf("removal admission: %d %s", r.Status, r.Raw)
			}
			stop := e.startWorker()
			defer stop()
			waitFor(t, "source removal pending profile", func() bool {
				r, _ := e.d.Queue.Request("q-" + nonce)
				return r != nil && (r.State == "completed" || r.State == "failed")
			})
			r, _ := e.d.Queue.Request("q-" + nonce)
			switch scenario {
			case "delete":
				if r.State != "completed" || calls.Load() != 1 || exists(e.s.recPath(subX)) {
					t.Fatalf("protected deletion failed: %#v", r)
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("deleted source left its private pending payload: %v", err)
				}
			case "owner refused":
				got, err := os.ReadFile(path)
				if r.State != "failed" || calls.Load() != 1 || err != nil || !bytes.Equal(got, pending) {
					t.Fatalf("refused transition discarded pending bytes: %#v %v", r, err)
				}
			case "unexpected link":
				got, err := os.ReadFile(servers.RecordPath(e.d, active))
				if r.State != "failed" || calls.Load() != 0 || err != nil || !bytes.Equal(got, working) ||
					!exists(path) || !exists(e.s.recPath(subX)) {
					t.Fatalf("ambiguous pending path changed runtime or source: %#v %v", r, err)
				}
			}
			if got, err := os.ReadFile(other); err != nil || string(got) != "unrelated pending" {
				t.Fatalf("foreign pending bytes changed: %v", err)
			}
		})
	}

	t.Run("backup stage", func(t *testing.T) {
		e, _, _, _, path, _, _ := pendingProfileQueueFixture(t)
		if _, err := e.s.updateSettings(subX, map[string]any{profileApplyTimeField: "05:20"}); err != nil {
			t.Fatal(err)
		}
		cur, _, err := e.s.readRecord(subX)
		if err != nil {
			t.Fatal(err)
		}
		cur[pendingSourceRevisionField] = strings.Repeat("a", 32)
		if err := e.s.writeRecord(cur); err != nil {
			t.Fatal(err)
		}
		// Only compare exact bytes in the staged/living trees here. A full
		// restore issues its own input authority and cannot reuse old pending.
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ledger, err := e.s.profileApplyAttempt(subX, "2026-11-24")
		if err != nil {
			t.Fatal(err)
		}
		day := []byte(`{"dayUTC":"2026-11-24","schemaVersion":1}`)
		if err := os.WriteFile(ledger.path, day, 0600); err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(e.s.subDir(), ".pending-active-"+compat.SHA256Hex([]byte("unrelated"))+".json")
		writeFile(t, other, "unrelated pending")
		data := e.s.backupExport()
		exported, err := compat.Marshal(data)
		if err != nil || bytes.Contains(exported, []byte(pendingSourceRevisionField)) ||
			bytes.Contains(exported, []byte(profileApplyScheduleRevisionField)) || !bytes.Contains(exported, []byte("05:20")) {
			t.Fatalf("backup must contain schedule without private authority: %v", err)
		}
		section := data.(map[string]any)
		sub := clone(cur) // an older backup may still include these internal fields
		section["records"] = []any{sub}
		section["catalogs"] = map[string]any{subX: section["catalogs"].(map[string]any)[subX]}
		encoded, err := compat.Marshal(section)
		if err != nil {
			t.Fatal(err)
		}
		st, err := compat.NewBackupStage(e.d, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		plan, err := e.s.backupStage(compat.WithFence(context.Background()), encoded, st)
		if err != nil {
			t.Fatal(err)
		}
		changed := false
		for _, change := range plan.Changes {
			if change.Kind == "subscription" && change.Action == "replace" {
				changed = true
			}
		}
		if !changed {
			t.Fatal("preview omitted the pending profile impact")
		}
		staged := newSvc(st.St)
		if _, err := os.Lstat(filepath.Join(staged.subDir(), filepath.Base(path))); !os.IsNotExist(err) {
			t.Fatalf("staged import can replay an old pending profile: %v", err)
		}
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, before) {
			t.Fatalf("preview mutated the live pending payload: %v", err)
		}
		if got, err := os.ReadFile(ledger.path); err != nil || !bytes.Equal(got, day) {
			t.Fatalf("preview changed today's attempt: %v", err)
		}
		if _, err := os.Lstat(st.St.Path("config", filepath.Base(ledger.path))); !os.IsNotExist(err) {
			t.Fatalf("settings import captured a live day receipt: %v", err)
		}
		if got, err := os.ReadFile(filepath.Join(staged.subDir(), filepath.Base(other))); err != nil || string(got) != "unrelated pending" {
			t.Fatalf("import discarded an unrelated pending payload: %v", err)
		}
		got, _, err := staged.readRecord(subX)
		if err != nil || got[profileApplyTimeField] != "05:20" || got[pendingSourceRevisionField] != nil ||
			got[profileApplyScheduleRevisionField] == nil || got[profileApplyScheduleRevisionField] == cur[profileApplyScheduleRevisionField] {
			t.Fatalf("restored schedule retained previous authorization: %v", err)
		}
	})
}

package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type removedSourceSelector interface {
	selectRemovedSource(context.Context, string, string, []string) (string, error)
}

func TestRemovedSourceCandidateSelection(t *testing.T) {
	for _, kind := range []string{"rejected-then-valid", "large-catalog", "schema-rejected", "all-rejected", "empty", "operational", "rejection-drift", "manual-stop", "duplicate", "same-source"} {
		t.Run(kind, func(t *testing.T) {
			f, description := reserveDisableFixture(t)
			selector, ok := any(f.e.s).(removedSourceSelector)
			if !ok {
				t.Fatal("SOURCE_SELECTION_ABSENT")
			}
			if err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision); err != nil {
				t.Fatal(err)
			}
			sourceID := "selection-source"
			oldID := f.plan.node.ID
			last := f.backup
			first := f.e.addServer(t, "trojan://pw@127.0.0.1:3#B")
			setSource := func(id string) {
				t.Helper()
				rec, err := os.ReadFile(RecordPath(f.e.d, id))
				if err != nil {
					t.Fatal(err)
				}
				var m Record
				if err = json.Unmarshal(rec, &m); err != nil {
					t.Fatal(err)
				}
				m["source"] = map[string]any{"type": "subscription", "subscriptionId": sourceID}
				b, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(RecordPath(f.e.d, id), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			setSource(oldID)
			if kind == "schema-rejected" {
				p := RecordPath(f.e.d, first)
				raw, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				var rec Record
				if err = json.Unmarshal(raw, &rec); err != nil {
					t.Fatal(err)
				}
				rec["address"] = ""
				raw, err = json.Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(p, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}

			if kind == "same-source" {
				setSource(first)
			}
			if kind == "manual-stop" {
				if err := f.process.Stop(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			inputs := map[string][]byte{}
			for _, p := range []string{RecordPath(f.e.d, oldID), RecordPath(f.e.d, first), RecordPath(f.e.d, last), reservePreferencesFile(f.e.d)} {
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				inputs[p] = b
			}
			before := f.process.RuntimeSnapshot()
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = f.e.d.Svc.KeeneticVerifyInterfaceTarget
			f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) {
				if ActiveID(f.e.d) == last {
					return "BROray - B", nil
				}
				return "BROray - A", nil
			}
			f.e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) { return "BROray - " + name, nil }
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error { *description = "BROray - B"; return nil }
			calls := 0
			f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, cfg []byte) error {
				calls++
				if kind == "operational" {
					return errors.New("validator unavailable")
				}
				if (calls == 1 && kind != "schema-rejected" && kind != "large-catalog") || kind == "all-rejected" {
					if kind == "rejection-drift" {
						p := RecordPath(f.e.d, first)
						inputs[p] = append(inputs[p], '\n')
						if err := os.WriteFile(p, inputs[p], 0600); err != nil {
							return err
						}
					}
					return compat.ErrXrayCandidateRejected
				}
				p := filepath.Join(t.TempDir(), "candidate.json")
				if err := os.WriteFile(p, cfg, 0600); err != nil {
					return err
				}
				return xray.Test(ctx, f.e.d.Root.XrayBinary(), p)
			}
			candidates := []string{first, last}
			if kind == "large-catalog" {
				candidates = []string{last}
				for i := 0; i < 1024; i++ {
					candidates = append(candidates, fmt.Sprintf("manual-20000101000000-%08x", i))
				}
			}
			if kind == "empty" {
				candidates = nil
			}
			if kind == "duplicate" {
				candidates = []string{first, first}
			}
			chosen, err := selector.selectRemovedSource(f.ctx, sourceID, oldID, candidates)
			replace := kind == "large-catalog" || kind == "rejected-then-valid" || kind == "manual-stop" || kind == "schema-rejected"
			withdraw := kind == "all-rejected" || kind == "empty"
			if (err == nil) != (replace || withdraw) {
				t.Fatal("selection result", kind, chosen, err)
			}
			if replace {
				wantCalls := 2
				if kind == "schema-rejected" || kind == "large-catalog" {
					wantCalls = 1
				}
				if chosen != last || ActiveID(f.e.d) != last || calls != wantCalls {
					t.Fatal("did not skip only rejected candidate", chosen, calls)
				}
				after := f.process.RuntimeSnapshot()
				if kind == "manual-stop" {
					if after.Running || after.Wanted || after.Generation != before.Generation {
						t.Fatal("manual stop lost")
					}
				} else if !after.Running || after.Generation != before.Generation+1 {
					t.Fatal("replacement not running")
				}
			} else if withdraw {
				if chosen != "" || ActiveID(f.e.d) != "" || f.process.RuntimeSnapshot().Running {
					t.Fatal("exhausted source not withdrawn")
				}
				if _, err := os.Stat(f.e.d.Root.XrayConfig()); !os.IsNotExist(err) {
					t.Fatal("withdrawal retained config", err)
				}
				if kind == "all-rejected" && calls != 2 {
					t.Fatal("not all candidates examined", calls)
				}
			} else {
				if chosen != "" || ActiveID(f.e.d) != oldID || f.process.RuntimeSnapshot() != before {
					t.Fatal("uncertain selection changed active runtime")
				}
				want := 1
				if kind == "duplicate" || kind == "same-source" {
					want = 0
				}
				if calls != want {
					t.Fatal("attempted fallback after uncertainty", calls)
				}
			}
			if pending, e := reserve.TransactionPending(f.ctx, f.e.d.Root); e != nil || pending {
				t.Fatal("unexpected pending transaction", pending, e)
			}
			for p, want := range inputs {
				got, e := os.ReadFile(p)
				if e != nil || !bytes.Equal(got, want) {
					t.Fatal("selection mutated catalog/preferences", p, e)
				}
			}
		})
	}
}

package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type sourceRemovalPreparer interface {
	prepareRemovedSource(context.Context, string, []string, []string) (string, error)
}

func TestSourceRemovalReserveIntegration(t *testing.T) {
	for _, kind := range []string{"enabled-active", "enabled-unexpected", "selection-failure", "enabled-stopped", "enabled-backup", "unrelated", "disabled-active", "withdraw", "absent-preferences", "foreign-member", "foreign-interface", "unfenced"} {
		t.Run(kind, func(t *testing.T) {
			f, description := reserveDisableFixture(t)
			owner, ok := any(f.e.s).(sourceRemovalPreparer)
			if !ok {
				t.Fatal("SOURCE_RESERVE_INTEGRATION_ABSENT")
			}
			oldID, nextID := f.plan.node.ID, f.backup
			sourceID := "removed-source"
			third := f.e.addServer(t, "trojan://pw@127.0.0.1:3#C")
			setSource := func(id string) {
				t.Helper()
				p := RecordPath(f.e.d, id)
				raw, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				var rec Record
				if err = json.Unmarshal(raw, &rec); err != nil {
					t.Fatal(err)
				}
				rec["source"] = map[string]any{"type": "subscription", "subscriptionId": sourceID}
				raw, err = json.Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(p, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			removed, candidates := []string{oldID}, []string{nextID, third}
			if kind == "enabled-backup" {
				removed = []string{nextID}
				candidates = []string{oldID, third}
			}
			if kind == "unrelated" {
				removed = []string{third}
				candidates = []string{oldID, nextID}
			}
			if kind == "withdraw" {
				removed = []string{oldID, nextID, third}
				candidates = nil
			}
			if kind == "disabled-active" || kind == "absent-preferences" {
				if err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision); err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range removed {
				if kind != "foreign-member" {
					setSource(id)
				}
			}
			if kind == "absent-preferences" {
				if err := os.Remove(reservePreferencesFile(f.e.d)); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "enabled-stopped" {
				if err := f.process.Stop(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "enabled-unexpected" {
				reserveTerminateOwnedFixture(t, f)
			}
			verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				if kind == "foreign-interface" {
					return compat.OwnedInterfaceTarget{}, errors.New("foreign interface")
				}
				return verify(ctx, want)
			}
			if kind == "foreign-interface" {
				f.e.d.Svc.KeeneticVerifyInterfaceTarget = f.e.d.Svc.KeeneticVerifyInterfaceConfiguration
			}
			f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) {
				if ActiveID(f.e.d) == nextID {
					return "BROray - B", nil
				}
				return "BROray - A", nil
			}
			f.e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) { return "BROray - " + name, nil }
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
				*description = "BROray - A"
				if ActiveID(f.e.d) == nextID {
					*description = "BROray - B"
				}
				return nil
			}
			f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, cfg []byte) error {
				if kind == "selection-failure" {
					return errors.New("validator unavailable")
				}
				p := filepath.Join(t.TempDir(), "candidate.json")
				if err := os.WriteFile(p, cfg, 0600); err != nil {
					return err
				}
				return xray.Test(ctx, f.e.d.Root.XrayBinary(), p)
			}
			records := map[string][]byte{}
			for _, id := range []string{oldID, nextID, third} {
				p := RecordPath(f.e.d, id)
				raw, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				records[p] = raw
			}
			before := f.process.RuntimeSnapshot()
			prefsBefore, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			ctx := f.ctx
			if kind == "unfenced" {
				ctx = context.Background()
			}
			chosen, err := owner.prepareRemovedSource(ctx, sourceID, removed, candidates)
			failed := kind == "selection-failure" || kind == "foreign-member" || kind == "foreign-interface" || kind == "unfenced"
			if (err != nil) != failed {
				t.Fatal("preparation result", kind, chosen, err)
			}
			prefs, e := readReservePreferences(f.e.d)
			if e != nil {
				t.Fatal(e)
			}
			after := f.process.RuntimeSnapshot()
			if failed {
				if kind == "selection-failure" {
					if prefs.Enabled || prefs.PrimaryServerID != prefsBefore.PrimaryServerID || len(prefs.BackupServerIDs) != len(prefsBefore.BackupServerIDs) || !reserveRosterContains(prefs, oldID) || after != before || ActiveID(f.e.d) != oldID {
						t.Fatal("post-disable failure pruned/restarted/lost catalog authority")
					}
				} else if prefs.Revision != prefsBefore.Revision || after != before || ActiveID(f.e.d) != oldID {
					t.Fatal("refusal mutated reserve/runtime")
				}
			} else {
				if kind == "unrelated" {
					if prefs.Revision != prefsBefore.Revision || !prefs.Enabled || after != before || chosen != oldID {
						t.Fatal("unrelated source changed reserve")
					}
				} else {
					if prefs.Enabled {
						t.Fatal("affected reserve left enabled")
					}
					for _, id := range removed {
						if reserveRosterContains(prefs, id) {
							t.Fatal("removed node retained in roster", id)
						}
					}
					if kind == "absent-preferences" && prefs.Exists {
						t.Fatal("absent preferences created")
					}
					if kind == "enabled-backup" {
						if chosen != oldID || after != before || prefs.PrimaryServerID != "" || len(prefs.BackupServerIDs) != 1 || prefs.BackupServerIDs[0] != oldID {
							t.Fatal("backup removal lost active/remaining roster")
						}
					} else if kind == "withdraw" {
						if chosen != "" || ActiveID(f.e.d) != "" || after.Running || after.Wanted {
							t.Fatal("empty source not withdrawn")
						}
					} else {
						if chosen != nextID || ActiveID(f.e.d) != nextID {
							t.Fatal("remaining server not selected")
						}
						if kind == "enabled-stopped" {
							if after.Running || after.Wanted || after.Generation != before.Generation {
								t.Fatal("manual stop lost")
							}
						} else if !after.Running || after.Generation != before.Generation+1 {
							t.Fatal("replacement restarted incorrectly")
						}
					}
				}
			}
			for p, want := range records {
				got, e := os.ReadFile(p)
				if e != nil || !bytes.Equal(got, want) {
					t.Fatal("preparation deleted/changed catalog", p, e)
				}
			}
			if pending, e := reserve.TransactionPending(f.ctx, f.e.d.Root); e != nil || pending {
				t.Fatal("unexpected transaction", pending, e)
			}
		})
	}
}

package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
	"os"
	"path/filepath"
	"testing"
)

type refreshCatalogOwner interface {
	PrepareSourceRefreshCatalog(context.Context, string, []string) error
}
type refreshReapplyOwner interface {
	ReapplySourceRefresh(context.Context, string, string, string) (string, error)
}

func refreshFixtureRecord(t *testing.T, d *compat.Deps, id string, change func(Record)) []byte {
	t.Helper()
	p := RecordPath(d, id)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	change(rec)
	b, err = json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return b
}
func TestSourceRefreshPreparesOldReserveBinding(t *testing.T) {
	for _, kind := range []string{"active", "backup", "unrelated", "disabled", "absent", "foreign", "unfenced", "stopped"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := reserveDisableFixture(t)
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = f.e.d.Svc.KeeneticVerifyInterfaceTarget
			owner, ok := any(f.e.d.Svc).(refreshCatalogOwner)
			if !ok {
				t.Fatal("SOURCE_REFRESH_CATALOG_PREPARER_ABSENT")
			}
			active := f.plan.node.ID
			changed := active
			if kind == "backup" {
				changed = f.backup
			}
			if kind == "unrelated" {
				changed = f.e.addServer(t, "trojan://pw@127.0.0.1:3#C")
			}
			if kind == "disabled" || kind == "absent" {
				if err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision); err != nil {
					t.Fatal(err)
				}
			}
			raw := refreshFixtureRecord(t, f.e.d, changed, func(rec Record) {
				if kind != "foreign" {
					rec["source"] = map[string]any{"type": "subscription", "subscriptionId": "refresh-source"}
				}
			})
			if kind == "absent" {
				if err := os.Remove(reservePreferencesFile(f.e.d)); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "stopped" {
				if err := f.process.Stop(f.ctx); err != nil {
					t.Fatal(err)
				}
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
			err = owner.PrepareSourceRefreshCatalog(ctx, "refresh-source", []string{changed})
			failed := kind == "foreign" || kind == "unfenced"
			if (err != nil) != failed {
				t.Fatal("preparation", kind, err)
			}
			after := f.process.RuntimeSnapshot()
			prefs, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			if after != before || ActiveID(f.e.d) != active {
				t.Fatal("pre-staging changed runtime selection")
			}
			if failed || kind == "unrelated" || kind == "disabled" || kind == "absent" {
				if prefs.Revision != prefsBefore.Revision {
					t.Fatal("unaffected preferences changed")
				}
			} else if prefs.Enabled || prefs.PrimaryServerID != prefsBefore.PrimaryServerID || len(prefs.BackupServerIDs) != len(prefsBefore.BackupServerIDs) {
				t.Fatal("reserve not disabled without roster loss")
			}
			got, err := os.ReadFile(RecordPath(f.e.d, changed))
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("pre-staging changed catalog", err)
			}
			if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
				t.Fatal("pending pre-stage transaction", err)
			}
		})
	}
}
func TestSourceRefreshSameIDPreservesIntent(t *testing.T) {
	for _, kind := range []string{"live", "stopped", "rename", "rejected", "foreign", "unfenced", "wrong-description"} {
		t.Run(kind, func(t *testing.T) {
			f, description := reserveDisableFixture(t)
			owner, ok := any(f.e.d.Svc).(refreshReapplyOwner)
			if !ok {
				t.Fatal("SOURCE_REFRESH_REAPPLY_ABSENT")
			}
			if err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision); err != nil {
				t.Fatal(err)
			}
			id := f.plan.node.ID
			if kind == "stopped" {
				if err := f.process.Stop(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			previous, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			before := f.process.RuntimeSnapshot()
			raw := refreshFixtureRecord(t, f.e.d, id, func(rec Record) {
				rec["address"] = "127.0.0.2"
				if kind == "rename" {
					rec["name"] = "Renamed"
				}
				if kind != "foreign" {
					rec["source"] = map[string]any{"type": "subscription", "subscriptionId": "refresh-source"}
				}
			})
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = f.e.d.Svc.KeeneticVerifyInterfaceTarget
			f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) {
				rec, err := ReadRecord(f.e.d, id)
				if err != nil {
					return "", err
				}
				name, _ := rec["name"].(string)
				return "BROray - " + name, nil
			}
			f.e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) { return "BROray - " + name, nil }
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
				d, err := f.e.d.Svc.KeeneticOrdinaryDescription()
				if err == nil {
					*description = d
				}
				return err
			}
			f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, cfg []byte) error {
				if kind == "rejected" {
					return compat.ErrXrayCandidateRejected
				}
				p := filepath.Join(t.TempDir(), "candidate.json")
				if err := os.WriteFile(p, cfg, 0600); err != nil {
					return err
				}
				return xray.Test(ctx, f.e.d.Root.XrayBinary(), p)
			}
			ctx := f.ctx
			if kind == "unfenced" {
				ctx = context.Background()
			}
			oldName := "A"
			if kind == "wrong-description" {
				oldName = "Wrong"
			}
			chosen, err := owner.ReapplySourceRefresh(ctx, "refresh-source", id, oldName)
			failed := kind == "rejected" || kind == "foreign" || kind == "unfenced" || kind == "wrong-description"
			if (err != nil) != failed {
				t.Fatal("same-ID result", kind, chosen, err)
			}
			after := f.process.RuntimeSnapshot()
			cfg, readErr := os.ReadFile(f.e.d.Root.XrayConfig())
			if readErr != nil {
				t.Fatal(readErr)
			}
			if ActiveID(f.e.d) != id {
				t.Fatal("same-ID changed active marker")
			}
			if failed {
				if after != before || !bytes.Equal(cfg, previous) {
					t.Fatal("refusal changed runtime/config")
				}
			} else {
				if chosen != id || bytes.Equal(cfg, previous) || after.Intent != before.Intent+1 {
					t.Fatal("new same-ID config not confirmed")
				}
				if kind == "stopped" {
					if after.Running || after.Wanted || after.Generation != before.Generation {
						t.Fatal("manual stop lost")
					}
				} else if !after.Running || !after.Wanted || after.Generation != before.Generation+1 {
					t.Fatal("replacement generation wrong")
				}
				_, reasonState, reasonErr := sourceStateObject(stateFile(f.e.d))
				wantReason := "Параметры активного сервера обновлены."
				if kind == "stopped" {
					wantReason += " Сохранена ручная остановка подключения."
				}
				if reasonErr != nil || reasonState["lastReason"] != wantReason {
					t.Fatalf("refresh status reason=%v want=%s err=%v", reasonState["lastReason"], wantReason, reasonErr)
				}
				if kind == "rename" && *description != "BROray - Renamed" {
					t.Fatal("renamed description not synchronized", *description)
				}
			}
			got, err := os.ReadFile(RecordPath(f.e.d, id))
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("owner changed staged record", err)
			}
			if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
				t.Fatal("pending same-ID transaction", err)
			}
		})
	}
}

func TestSourceRefreshLifecycleProvidersUnavailable(t *testing.T) {
	var services compat.Services
	if err := services.PrepareSourceRefreshCatalog(context.Background(), "source", nil); !errors.Is(err, compat.ErrServiceUnavailable) {
		t.Fatal(err)
	}
	if id, err := services.ReapplySourceRefresh(context.Background(), "source", "id", "name"); id != "" || !errors.Is(err, compat.ErrServiceUnavailable) {
		t.Fatal(id, err)
	}
}

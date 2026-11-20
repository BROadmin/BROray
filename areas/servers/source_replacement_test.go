package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type sourceReplacer interface {
	replaceRemovedSource(context.Context, string, string, string) error
}

func TestRemovedSourceReplacement(t *testing.T) {
	for _, kind := range []string{"running", "hysteria", "manual-stop", "unexpected-exit", "outside-roster", "absent-preferences", "same-source-candidate", "wrong-record-id", "enabled-reserve", "unfenced", "validation", "input-drift", "foreign-interface", "metadata-failure", "new-generation", "core-replaced"} {
		t.Run(kind, func(t *testing.T) {
			f, description := reserveDisableFixture(t)
			executor, ok := any(f.e.s).(sourceReplacer)
			if !ok {
				t.Fatal("SOURCE_REPLACE_EXECUTOR_ABSENT")
			}
			if kind != "enabled-reserve" {
				if err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision); err != nil {
					t.Fatal(err)
				}
			}
			sourceID := "test-subscription"
			oldID, nextID := f.plan.node.ID, f.backup
			if kind == "outside-roster" {
				nextID = f.e.addServer(t, "trojan://pw@127.0.0.1:3#B")
			}
			if kind == "hysteria" {
				nextID = f.e.addServer(t, "hy2://synthetic@127.0.0.1:443?sni=localhost#B")
			}
			if kind == "absent-preferences" {
				if err := os.Remove(reservePreferencesFile(f.e.d)); err != nil {
					t.Fatal(err)
				}
			}
			modify := func(id string, fn func(Record)) []byte {
				t.Helper()
				p := RecordPath(f.e.d, id)
				raw, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				var rec Record
				if err := json.Unmarshal(raw, &rec); err != nil {
					t.Fatal(err)
				}
				fn(rec)
				raw, err = json.Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, raw, 0600); err != nil {
					t.Fatal(err)
				}
				return raw
			}
			oldRecord := modify(oldID, func(r Record) { r["source"] = map[string]any{"type": "subscription", "subscriptionId": sourceID} })
			if kind == "same-source-candidate" {
				modify(nextID, func(r Record) { r["source"] = map[string]any{"type": "subscription", "subscriptionId": sourceID} })
			}
			if kind == "wrong-record-id" {
				modify(nextID, func(r Record) { r["id"] = oldID })
			}
			if kind == "manual-stop" {
				if err := f.process.Stop(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "unexpected-exit" {
				reserveTerminateOwnedFixture(t, f)
			}
			before := f.process.RuntimeSnapshot()
			oldConfig, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			oldActive, err := os.ReadFile(ActiveFile(f.e.d))
			if err != nil {
				t.Fatal(err)
			}
			oldPrefs, err := os.ReadFile(reservePreferencesFile(f.e.d))
			if err != nil && !(kind == "absent-preferences" && errors.Is(err, os.ErrNotExist)) {
				t.Fatal(err)
			}
			candidate, err := os.ReadFile(RecordPath(f.e.d, nextID))
			if err != nil {
				t.Fatal(err)
			}
			verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				if kind == "foreign-interface" {
					return compat.OwnedInterfaceTarget{}, errors.New("foreign interface")
				}
				return verify(ctx, want)
			}
			f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) {
				if ActiveID(f.e.d) == nextID {
					return "BROray - B", nil
				}
				return "BROray - A", nil
			}
			f.e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) {
				if name != "B" {
					return "", errors.New("wrong candidate name")
				}
				return "BROray - B", nil
			}
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(ctx context.Context) error {
				if kind == "metadata-failure" {
					return errors.New("description save failed")
				}
				*description = "BROray - B"
				if kind == "new-generation" {
					return f.process.Restart(ctx)
				}
				if kind == "core-replaced" {
					raw, err := os.ReadFile(f.e.d.Root.XrayBinary())
					if err != nil {
						return err
					}
					p := filepath.Join(f.e.d.Root.Config(), "replacement-core")
					if err := os.WriteFile(p, raw, 0700); err != nil {
						return err
					}
					return os.Rename(p, f.e.d.Root.XrayBinary())
				}
				return nil
			}
			validated := false
			f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, cfg []byte) error {
				validated = true
				if kind == "validation" {
					return errors.New("candidate rejected")
				}
				p := filepath.Join(t.TempDir(), "validate.json")
				if err := os.WriteFile(p, cfg, 0600); err != nil {
					return err
				}
				if err := xray.Test(ctx, f.e.d.Root.XrayBinary(), p); err != nil {
					return err
				}
				if kind == "input-drift" {
					candidate = modify(nextID, func(r Record) { r["password"] = "changed" })
				}
				return nil
			}
			ctx := f.ctx
			if kind == "unfenced" {
				ctx = context.Background()
			}
			err = executor.replaceRemovedSource(ctx, sourceID, oldID, nextID)
			success := kind == "hysteria" || kind == "running" || kind == "manual-stop" || kind == "unexpected-exit" || kind == "outside-roster" || kind == "absent-preferences"
			pending := kind == "metadata-failure" || kind == "new-generation" || kind == "core-replaced"
			if (err == nil) != success || errors.Is(err, errReserveTransactionPending) != pending {
				t.Fatal("replacement result", kind, err)
			}
			actualPending, e := reserve.TransactionPending(f.ctx, f.e.d.Root)
			if e != nil || actualPending != pending {
				t.Fatal("journal retention", actualPending, e)
			}
			after := f.process.RuntimeSnapshot()
			if success || pending {
				if !validated {
					t.Fatal("replacement without native validation")
				}
				raw, e := os.ReadFile(ActiveFile(f.e.d))
				if e != nil || strings.TrimSpace(string(raw)) != nextID {
					t.Fatal("wrong selected remaining node", e)
				}
				if kind == "manual-stop" {
					if after.Running || after.Wanted || after.Generation != before.Generation || after.Intent != before.Intent+1 {
						t.Fatal("manual stop not preserved", before, after)
					}
				} else {
					wantGen := before.Generation + 1
					if kind == "new-generation" {
						wantGen++
					}
					if !after.Running || !after.Wanted || after.Generation != wantGen {
						t.Fatal("replacement runtime lost or foreign child killed", before, after)
					}
				}
				if success && *description != "BROray - B" {
					t.Fatal("ordinary description not updated")
				}
			} else {
				if after != before {
					t.Fatal("refused replacement changed runtime", before, after)
				}
				for p, want := range map[string][]byte{f.e.d.Root.XrayConfig(): oldConfig, ActiveFile(f.e.d): oldActive} {
					got, e := os.ReadFile(p)
					if e != nil || !bytes.Equal(got, want) {
						t.Fatal("refused replacement changed selection", p, e)
					}
				}
			}
			for p, want := range map[string][]byte{RecordPath(f.e.d, oldID): oldRecord, RecordPath(f.e.d, nextID): candidate, reservePreferencesFile(f.e.d): oldPrefs} {
				got, e := os.ReadFile(p)
				if want == nil {
					if !errors.Is(e, os.ErrNotExist) {
						t.Fatal("absent preferences appeared", p, e)
					}
				} else if e != nil || !bytes.Equal(got, want) {
					t.Fatal("catalog/preferences changed", p, e)
				}
			}
		})
	}
}

package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type sourceWithdrawer interface {
	withdrawRemovedSource(context.Context, string, string) error
}

func requireSourceWithdrawal(t *testing.T, s *svc) sourceWithdrawer {
	t.Helper()
	v, ok := any(s).(sourceWithdrawer)
	if !ok {
		t.Fatal("SOURCE_WITHDRAW_EXECUTOR_ABSENT")
	}
	return v
}
func TestRemovedSourceWithdrawal(t *testing.T) {
	for _, kind := range []string{"running", "manual-stop", "unexpected-exit", "absent-preferences", "different-active", "enabled-reserve", "foreign-source", "foreign-interface", "unfenced", "unknown-runtime", "commit-proof-failure", "new-generation"} {
		t.Run(kind, func(t *testing.T) {
			f, desc := reserveDisableFixture(t)
			executor := requireSourceWithdrawal(t, f.e.s)
			if kind != "enabled-reserve" {
				if err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision); err != nil {
					t.Fatal(err)
				}
			}
			sourceID := "test-subscription"
			path := RecordPath(f.e.d, f.plan.node.ID)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var record Record
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			record["source"] = map[string]any{"type": "subscription", "subscriptionId": sourceID}
			recordBytes, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, recordBytes, 0600); err != nil {
				t.Fatal(err)
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
			config, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			active, err := os.ReadFile(ActiveFile(f.e.d))
			if err != nil {
				t.Fatal(err)
			}
			if kind == "absent-preferences" {
				if err := os.Remove(reservePreferencesFile(f.e.d)); err != nil {
					t.Fatal(err)
				}
			}
			prefs, err := os.ReadFile(reservePreferencesFile(f.e.d))
			if err != nil && !(kind == "absent-preferences" && errors.Is(err, os.ErrNotExist)) {
				t.Fatal(err)
			}
			receipt, err := os.ReadFile(f.e.d.Path("config", "interface.json"))
			if err != nil {
				t.Fatal(err)
			}
			originalVerify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
			calls := 0
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				calls++
				if kind == "foreign-interface" {
					return compat.OwnedInterfaceTarget{}, errors.New("foreign owner")
				}
				if calls == 3 {
					if kind == "commit-proof-failure" {
						return compat.OwnedInterfaceTarget{}, errors.New("ownership changed")
					}
					if kind == "new-generation" {
						if err := os.WriteFile(f.e.d.Root.XrayConfig(), config, 0600); err != nil {
							return compat.OwnedInterfaceTarget{}, err
						}
						if err := f.process.Start(ctx); err != nil {
							return compat.OwnedInterfaceTarget{}, err
						}
					}
				}
				return originalVerify(ctx, want)
			}
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
				t.Error("withdrawal wrote router metadata")
				return errors.New("unexpected metadata write")
			}
			ctx := f.ctx
			if kind == "unfenced" {
				ctx = context.Background()
			}
			if kind == "foreign-source" {
				sourceID = "other-subscription"
			}
			if kind == "unknown-runtime" {
				f.e.d.App.Xray = xray.NewProcess(f.e.d.Root)
			}
			activeID := f.plan.node.ID
			if kind == "different-active" {
				activeID = f.backup
			}
			err = executor.withdrawRemovedSource(ctx, sourceID, activeID)
			success := kind == "running" || kind == "manual-stop" || kind == "unexpected-exit" || kind == "absent-preferences"
			uncertain := kind == "commit-proof-failure" || kind == "new-generation"
			if (err == nil) != success {
				t.Fatalf("outcome %s: %v", kind, err)
			}
			if errors.Is(err, errReserveTransactionPending) != uncertain {
				t.Fatal("wrong recovery classification", err)
			}
			pending, e := reserve.TransactionPending(f.ctx, f.e.d.Root)
			if e != nil || pending != uncertain {
				t.Fatal("wrong durable recovery state", pending, e)
			}
			after := f.process.RuntimeSnapshot()
			if success || kind == "commit-proof-failure" {
				if after.Running || after.Wanted || !after.ExitConfirmed || after.Generation != before.Generation || after.Intent != before.Intent+1 {
					t.Fatal("disconnect runtime", before, after)
				}
				for _, p := range []string{f.e.d.Root.XrayConfig(), ActiveFile(f.e.d)} {
					if _, e := os.Lstat(p); !errors.Is(e, os.ErrNotExist) {
						t.Fatal("selection not withdrawn", p, e)
					}
				}
			} else if kind == "new-generation" {
				if !after.Running || after.Generation != before.Generation+1 {
					t.Fatal("new child killed or not observed", after)
				}
			} else {
				if after != before {
					t.Fatal("refused withdrawal changed runtime", before, after)
				}
				for p, want := range map[string][]byte{f.e.d.Root.XrayConfig(): config, ActiveFile(f.e.d): active} {
					got, e := os.ReadFile(p)
					if e != nil || !bytes.Equal(got, want) {
						t.Fatal("refused withdrawal changed selection", p, e)
					}
				}
			}
			for p, want := range map[string][]byte{path: recordBytes, reservePreferencesFile(f.e.d): prefs, f.e.d.Path("config", "interface.json"): receipt} {
				got, e := os.ReadFile(p)
				if want == nil {
					if !errors.Is(e, os.ErrNotExist) {
						t.Fatal("absent caller-owned file appeared", p, e)
					}
				} else if e != nil || !bytes.Equal(got, want) {
					t.Fatal("caller-owned data changed", p, e)
				}
			}
			wantDescription := "BROray - A"
			if kind == "enabled-reserve" {
				wantDescription = "BROray - mix-auto"
			}
			if *desc != wantDescription {
				t.Fatal("interface renamed", *desc)
			}
		})
	}
}

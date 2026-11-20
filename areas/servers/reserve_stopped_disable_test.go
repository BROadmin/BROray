package servers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

func TestReserveModeDisableStoppedCore(t *testing.T) {
	for _, kind := range []string{"manual-stop", "unexpected-exit", "unconfirmed-exit", "foreign-interface", "rename-failure", "new-generation"} {
		t.Run(kind, func(t *testing.T) {
			f, desc := reserveDisableFixture(t)
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = f.e.d.Svc.KeeneticVerifyInterfaceTarget
			if kind == "unexpected-exit" {
				reserveTerminateOwnedFixture(t, f)
			} else if err := f.process.Stop(f.ctx); err != nil {
				t.Fatal(err)
			}
			stopped := f.process.RuntimeSnapshot()
			if stopped.Running || !stopped.ExitConfirmed {
				t.Fatal("fixture exit not confirmed")
			}
			beforeConfig, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			beforeActive, err := os.ReadFile(ActiveFile(f.e.d))
			if err != nil {
				t.Fatal(err)
			}
			f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(context.Context, string) (compat.OwnedInterfaceTarget, error) {
				return compat.OwnedInterfaceTarget{}, errors.New("dead child has no live target")
			}
			if kind == "foreign-interface" {
				f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(context.Context, string) (compat.OwnedInterfaceTarget, error) {
					return compat.OwnedInterfaceTarget{}, errors.New("foreign interface")
				}
			}
			if kind == "rename-failure" {
				f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error { return errors.New("save failed") }
			}
			if kind == "new-generation" {
				f.e.d.Svc.KeeneticSyncOwnedDescription = func(ctx context.Context) error {
					if err := f.process.Start(ctx); err != nil {
						return err
					}
					*desc = "BROray - A"
					return nil
				}
			}
			if kind == "unconfirmed-exit" {
				f.e.d.App.Xray = xray.NewProcess(f.e.d.Root)
			}
			reserveQueueAuthority(t, f)
			if err := f.e.d.Queue.SetPaused(true); err != nil {
				t.Fatal(err)
			}
			prefs, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			nonce := strings.Repeat("ef", 16)
			r := reserveModeRequest(t, false, prefs.Revision, nonce)
			if r.Code != 202 {
				t.Fatal(r.Raw)
			}
			runReserveQueue(t, f.e)
			want := "completed"
			if kind == "unconfirmed-exit" || kind == "foreign-interface" || kind == "rename-failure" || kind == "new-generation" {
				want = "failed"
			}
			waitReq(t, f.e, "q-"+nonce, want)
			after, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			if after.PrimaryServerID != prefs.PrimaryServerID || len(after.BackupServerIDs) != 1 || after.BackupServerIDs[0] != prefs.BackupServerIDs[0] {
				t.Fatal("roster changed")
			}
			config, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil || !bytes.Equal(config, beforeConfig) {
				t.Fatal("config changed", err)
			}
			active, err := os.ReadFile(ActiveFile(f.e.d))
			if err != nil || !bytes.Equal(active, beforeActive) {
				t.Fatal("active changed", err)
			}
			pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "rename-failure" || kind == "new-generation" {
				if !pending || !f.e.d.Lock.Busy() {
					t.Fatal("uncertain disable released recovery fence")
				}
			} else if pending || f.e.d.Lock.Busy() {
				t.Fatal("clean/refused disable retained fence")
			}
			if kind == "rename-failure" {
				result, _ := f.e.d.Queue.Request("q-" + nonce)
				if !after.Enabled || after.Revision != prefs.Revision || *desc != "BROray - mix-auto" || result.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" {
					t.Fatal("partial restoration must retain old mode and recovery failure", after, result.ErrorCode)
				}
			} else if kind == "foreign-interface" || kind == "unconfirmed-exit" {
				if !after.Enabled || *desc != "BROray - mix-auto" {
					t.Fatal("foreign refusal mutated mode")
				}
			} else if after.Enabled {
				t.Fatal("mode stayed enabled")
			}
			if kind == "new-generation" {
				if !f.process.RuntimeSnapshot().Running {
					t.Fatal("replacement child stopped")
				}
			} else if f.process.RuntimeSnapshot() != stopped {
				t.Fatal("disable changed stopped lifecycle")
			}
			if want == "completed" && *desc != "BROray - A" {
				t.Fatal("ordinary description not applied")
			}
		})
	}
}

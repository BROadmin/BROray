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
)

func TestReserveDisableQueuedRollbackPreservesRuntime(t *testing.T) {
	for _, kind := range []string{"live-before", "live-after", "manual-before", "manual-after", "unexpected-after"} {
		t.Run(kind, func(t *testing.T) {
			f, desc := reserveDisableFixture(t)
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = f.e.d.Svc.KeeneticVerifyInterfaceTarget
			if strings.HasPrefix(kind, "manual") {
				if err := f.process.Stop(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(kind, "unexpected") {
				reserveTerminateOwnedFixture(t, f)
			}
			snapshot := f.process.RuntimeSnapshot()
			prefs, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			config, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			active, err := os.ReadFile(ActiveFile(f.e.d))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(ctx context.Context) error {
				calls++
				if calls == 1 {
					if strings.HasSuffix(kind, "after") {
						*desc = "BROray - A"
						if err := os.WriteFile(f.e.d.Path("config", "interface.json"), []byte(`{"fixture":"canonical-disabled-receipt"}`), 0600); err != nil {
							return err
						}
					}
					return errors.New("disable metadata write unconfirmed")
				}
				restored, err := readReservePreferences(f.e.d)
				if err != nil || !restored.Enabled {
					return errors.New("old preferences not installed before metadata rollback")
				}
				*desc = "BROray - mix-auto"
				return os.WriteFile(f.e.d.Path("config", "interface.json"), []byte(`{"fixture":"canonical-restored-receipt"}`), 0600)
			}
			reserveQueueAuthority(t, f)
			if err := f.e.d.Queue.SetPaused(true); err != nil {
				t.Fatal(err)
			}
			reply := reserveModeRequest(t, false, prefs.Revision, strings.Repeat("8a", 16))
			if reply.Code != 202 {
				t.Fatal(reply.Code, reply.Raw)
			}
			row, ok := f.e.d.Queue.FindLive(compat.ReserveModeAction, compat.ReserveOperationTarget)
			if !ok {
				t.Fatal("mode request absent")
			}
			runReserveQueue(t, f.e)
			waitReq(t, f.e, row.RequestID, "failed")
			result, _ := f.e.d.Queue.Request(row.RequestID)
			if result.ErrorCode != "RESERVE_MODE_ROLLED_BACK" {
				t.Fatal("confirmed rollback missing", result.ErrorCode, result.Message)
			}
			after, err := readReservePreferences(f.e.d)
			if err != nil || after.Revision != prefs.Revision || !after.Enabled {
				t.Fatal("old mode not restored", after, err)
			}
			if f.process.RuntimeSnapshot() != snapshot || *desc != "BROray - mix-auto" || calls != 2 {
				t.Fatal("rollback changed runtime or missed metadata", calls, *desc)
			}
			got, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil || !bytes.Equal(got, config) {
				t.Fatal("config changed", err)
			}
			got, err = os.ReadFile(ActiveFile(f.e.d))
			if err != nil || !bytes.Equal(got, active) {
				t.Fatal("active changed", err)
			}
			if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending || f.e.d.Lock.Busy() {
				t.Fatal("proven rollback retained fence", pending, err)
			}
		})
	}
}

func TestReserveDisableRollbackRetainsUncertainAuthority(t *testing.T) {
	for _, kind := range []string{"manual-stop", "foreign-receipt", "foreign-profile", "committed-journal", "restore-proof"} {
		t.Run(kind, func(t *testing.T) {
			f, desc := reserveDisableFixture(t)
			before := f.process.RuntimeSnapshot()
			verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
			calls := 0
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				if kind == "foreign-receipt" && calls > 0 {
					return compat.OwnedInterfaceTarget{}, errors.New("foreign receipt")
				}
				return verify(ctx, want)
			}
			f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				if kind == "restore-proof" && calls > 1 {
					return compat.OwnedInterfaceTarget{}, errors.New("restoration unproven")
				}
				return verify(ctx, want)
			}
			var path string
			var protected []byte
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(ctx context.Context) error {
				calls++
				if calls > 1 {
					*desc = "BROray - mix-auto"
					return nil
				}
				*desc = "BROray - A"
				switch kind {
				case "manual-stop":
					if err := f.process.Stop(ctx); err != nil {
						return err
					}
				case "foreign-receipt":
					path = f.e.d.Path("config", "interface.json")
					protected = []byte(`{"foreign":true}`)
				case "foreign-profile":
					path = RecordPath(f.e.d, f.plan.node.ID)
					raw, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					protected = append(raw, byte(' '))
				case "committed-journal":
					path = filepath.Join(f.e.d.Root.Config(), reserve.TransactionJournalFile)
					raw, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					var record reserve.TransactionRecord
					if err := json.Unmarshal(raw, &record); err != nil {
						return err
					}
					record.Phase = "committed"
					protected, err = json.Marshal(record)
					if err != nil {
						return err
					}
				}
				if path != "" {
					if err := os.WriteFile(path, protected, 0600); err != nil {
						return err
					}
				}
				return errors.New("disable failed")
			}
			err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision)
			if !errors.Is(err, errReserveTransactionPending) {
				t.Fatal("unproven rollback discarded owner", err)
			}
			now := f.process.RuntimeSnapshot()
			if now.Generation != before.Generation {
				t.Fatal("rollback restarted process")
			}
			if kind == "manual-stop" {
				if now.Running || now.Wanted {
					t.Fatal("manual stop resurrected")
				}
			} else if now != before {
				t.Fatal("runtime changed")
			}
			if path != "" {
				raw, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, protected) {
					t.Fatal("foreign or committed evidence changed", err)
				}
			}
			if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || !pending {
				t.Fatal("pending journal lost", err)
			}
		})
	}
}

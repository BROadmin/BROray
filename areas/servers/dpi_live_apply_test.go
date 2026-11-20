package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type dpiLiveApplier interface {
	applyDPIWith(context.Context, string, string, bool, func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) error
}

func TestDPILiveApplyLifecycle(t *testing.T) {
	for _, mode := range []string{"running-reserve", "running-ordinary", "inactive", "stopped", "cold", "rollback", "unknown", "proposal-rejected", "stale", "owned-exit", "manual-stop", "foreign-restart"} {
		t.Run(mode, func(t *testing.T) {
			if _, ok := any((*svc)(nil)).(dpiLiveApplier); !ok {
				t.Fatal("DPI_LIVE_APPLY_ABSENT")
			}
			f := newReserveRuntimeFixture(t)
			id := f.plan.node.ID
			guard, err := platform.TryLock(f.e.d.Root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { guard.Release() })
			f.e.d.DaemonGuard = guard
			if mode == "running-ordinary" {
				writeReserveAutomatic(t, f.e, id, f.backup, false)
			}
			if mode == "inactive" {
				id = f.backup
			}
			if err = compat.WriteJSON(f.e.d.Path("config", "interface.json"), map[string]any{"fixture": "owned"}); err != nil {
				t.Fatal(err)
			}
			f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) { return "BROray - A", nil }
			endpoint, err := netip.ParseAddrPort("127.0.0.1:" + fmt.Sprint(f.e.s.genSettings(f.ctx).SocksPort))
			if err != nil {
				t.Fatal(err)
			}
			target := compat.OwnedInterfaceTarget{Name: "Proxy0", Upstream: endpoint, DescriptionLine: `    description "BROray - A"`}
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(context.Context, string) (compat.OwnedInterfaceTarget, error) { return target, nil }
			f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(context.Context, string) (compat.OwnedInterfaceTarget, error) { return target, nil }
			if mode == "stopped" || mode == "cold" {
				if err = f.process.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(RecordPath(f.e.d, id))
			if err != nil {
				t.Fatal(err)
			}
			config, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cold" {
				f.process = xray.NewProcess(f.e.d.Root)
				f.e.d.App.Xray = f.process
				t.Cleanup(func() { _ = f.process.Stop(context.Background()) })
				hold, _ := json.Marshal(map[string]any{"schemaVersion": 1, "operation": strings.Repeat("a", 64), "serverId": id, "recordSHA256": compat.SHA256Hex(before), "configSHA256": compat.SHA256Hex(config)})
				if err = os.WriteFile(filepath.Join(f.e.d.Root.Config(), reserve.ProfileBootHoldFile), hold, 0600); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := f.process.RuntimeSnapshot()
			revision := compat.SHA256Hex(before)
			if mode == "stale" {
				revision = strings.Repeat("0", 64)
			}
			foreignSnapshot := snapshot
			calls := 0
			probe := func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
				calls++
				out := reserveVerifiedOutcome(ctx, target)
				if calls == 2 && mode == "owned-exit" {
					terminateReserveEnableReplacement(t, f)
				}
				if calls == 2 && (mode == "manual-stop" || mode == "foreign-restart") {
					if e := f.process.Stop(ctx); e != nil {
						t.Fatal(e)
					}
					if mode == "foreign-restart" {
						if e := f.process.Start(ctx); e != nil {
							t.Fatal(e)
						}
					}
					foreignSnapshot = f.process.RuntimeSnapshot()
				}
				if mode == "proposal-rejected" || (mode == "rollback" && calls == 2) {
					out.Verdict = "unhealthy"
				}
				if mode == "unknown" && calls == 2 {
					out.Verdict = "unknown"
				}
				return out
			}
			err = any(f.e.s).(dpiLiveApplier).applyDPIWith(f.ctx, id, revision, true, probe)
			pending, pe := reserve.TransactionPending(f.ctx, f.e.d.Root)
			if pe != nil {
				t.Fatal(pe)
			}
			rec, re := ReadRecord(f.e.d, id)
			if re != nil {
				t.Fatal(re)
			}
			enabled, re := ManagedDPI(rec)
			if re != nil {
				t.Fatal(re)
			}
			switch mode {
			case "unknown", "manual-stop", "foreign-restart":
				if !errors.Is(err, compat.ErrReserveTransactionPending) || !pending {
					t.Fatal("uncertainty lost owner", err, pending)
				}
			case "rollback", "proposal-rejected", "stale", "owned-exit":
				if err == nil || pending || enabled {
					t.Fatal("failed apply not restored", err, pending, enabled)
				}
				after, e := os.ReadFile(RecordPath(f.e.d, id))
				current, ce := os.ReadFile(f.e.d.Root.XrayConfig())
				if e != nil || ce != nil || !bytes.Equal(after, before) || !bytes.Equal(current, config) {
					t.Fatal("old images not retained", e, ce)
				}
			default:
				if err != nil || pending || !enabled {
					t.Fatal("apply failed", err, pending, enabled)
				}
			}
			if mode == "inactive" || mode == "cold" || mode == "proposal-rejected" || mode == "stale" {
				if f.process.RuntimeSnapshot() != snapshot {
					t.Fatal("unexpected main intent mutation")
				}
			}
			if mode == "cold" || mode == "stopped" {
				if e := f.process.StartAtBoot(f.ctx); !errors.Is(e, reserve.ErrProfileBootHeld) {
					t.Fatal("stopped intent lost", e)
				}
			}
			if (mode == "manual-stop" || mode == "foreign-restart") && f.process.RuntimeSnapshot() != foreignSnapshot {
				t.Fatal("foreign intent changed after uncertainty")
			}
			if mode == "owned-exit" && (!errors.Is(err, errDPIApplyRolledBack) || !f.process.RuntimeSnapshot().Running || !f.process.RuntimeSnapshot().Wanted) {
				t.Fatal("owned exit rollback not confirmed", err)
			}
			if mode == "stale" && calls != 0 {
				t.Fatal("stale request probed", calls)
			}
		})
	}
}

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
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

func TestHotReserveStoppedExit(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		cold, rollback, noHold bool
	}{
		{name: "reaped"},
		{name: "cold", cold: true},
		{name: "reaped-rollback", rollback: true},
		{name: "cold-rollback", cold: true, rollback: true},
		{name: "no-hold", cold: true, noHold: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReserveRuntimeFixtureWithLinks(t,
				"vless://947dee4b-019c-4489-8b63-82853bb5f08c@127.0.0.1:1?security=none&type=tcp#A",
				"vless://947dee4b-019c-4489-8b63-82853bb5f08c@127.0.0.1:2?security=none&type=tcp#B")
			s, d := f.e.s, f.e.d
			active := f.plan.node.ID
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			writeReserveAutomatic(t, f.e, f.backup, active, true)
			chosen, err := s.prepareReserveStockRuntime(f.ctx, active, f.plan.endpoint, false)
			must(err)
			primary, err := s.prepareReserveStockRuntime(f.ctx, f.backup, f.plan.endpoint, false)
			must(err)
			hot, err := s.buildReserveHotPlan(chosen, []string{f.backup, active}, map[string]reserveRuntimePlan{
				active: chosen, f.backup: primary,
			})
			must(err)
			must(f.process.Stop(f.ctx))
			must(platform.WriteFileAtomicNoFollow(d.Root.XrayConfig(), hot.config, 0600))
			p := f.process
			holdPath := filepath.Join(d.Root.Config(), reserve.ProfileBootHoldFile)
			var hold []byte
			if tc.cold {
				p = xray.NewProcess(d.Root)
				d.App.Xray = p
				daemon, err := platform.TryLock(d.Root.DaemonLock())
				must(err)
				t.Cleanup(func() { must(daemon.Release()) })
				d.DaemonGuard = daemon
				if !tc.noHold {
					hold, err = json.Marshal(map[string]any{"schemaVersion": 1, "operation": strings.Repeat("a", 64),
						"serverId": active, "recordSHA256": hot.recordSHA, "configSHA256": hot.configSHA})
					must(err)
					must(platform.WriteFileAtomicNoFollow(holdPath, hold, 0600))
				}
			}
			before := p.RuntimeSnapshot()
			description := "BROray - mix-auto"
			target := commitFixtureTarget(t, f)
			fail := tc.rollback
			d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				if !compat.FenceHeld(ctx) || want != description {
					return compat.OwnedInterfaceTarget{}, errors.New("wrong interface")
				}
				if want == "BROray - A" && fail {
					fail = false
					return compat.OwnedInterfaceTarget{}, errors.New("injected failure at stopped confirmation")
				}
				target.DescriptionLine = `    description "` + description + `"`
				return target, nil
			}
			d.Svc.KeeneticOrdinaryDescription = func() (string, error) { return "BROray - A", nil }
			d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
				prefs, err := readReservePreferences(d)
				if err == nil {
					description = "BROray - A"
					if prefs.Enabled {
						description = "BROray - mix-auto"
					}
				}
				return err
			}
			d.Svc.XrayValidateConfig = func(ctx context.Context, data []byte) error {
				path := filepath.Join(t.TempDir(), "check.json")
				if err := os.WriteFile(path, data, 0600); err != nil {
					return err
				}
				return xray.Test(ctx, d.Root.XrayBinary(), path)
			}
			receipt := []byte(`{"fixture":"owned-receipt"}`)
			must(os.WriteFile(d.Path("config", "interface.json"), receipt, 0600))
			err = s.disableReserveStopped(f.ctx, hot, hot.revision)
			switch {
			case tc.rollback && !errors.Is(err, errReserveModeRolledBack):
				t.Fatal("expected verified rollback", err)
			case tc.noHold && err == nil:
				t.Fatal("unleased cold operation accepted")
			case !tc.rollback && !tc.noHold && err != nil:
				t.Fatal("stopped exit", err)
			}
			after := p.RuntimeSnapshot()
			if after.Wanted || after.Running || after.PID != 0 || after.Generation != before.Generation || after.LastPID != before.LastPID ||
				(tc.cold && after != before) {
				t.Fatal("stopped operation changed process state", before, after)
			}
			keep := tc.rollback || tc.noHold
			prefs, err := readReservePreferences(d)
			must(err)
			if prefs.Enabled != keep || ActiveID(d) != active || prefs.PrimaryServerID != f.backup ||
				len(prefs.BackupServerIDs) != 1 || prefs.BackupServerIDs[0] != active {
				t.Fatal("inconsistent selection and preferences")
			}
			raw, err := os.ReadFile(d.Root.XrayConfig())
			must(err)
			_, marked := reserve.HotLayoutSocket(raw)
			if marked != keep || (keep && !bytes.Equal(raw, hot.config)) ||
				(!keep && !reservePrimaryMatches(raw, commitFixtureTarget(t, f).Upstream)) {
				t.Fatal("wrong stopped configuration image")
			}
			if pending, err := reserve.TransactionPending(f.ctx, d.Root); err != nil || pending {
				t.Fatal("journal not resolved", pending, err)
			}
			if len(hold) > 0 {
				raw, err := os.ReadFile(holdPath)
				must(err)
				if !bytes.Equal(raw, hold) {
					t.Fatal("cold boot hold changed")
				}
			}
		})
	}
}

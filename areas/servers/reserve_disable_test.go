package servers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

func reserveDisableFixture(t *testing.T) (reserveRuntimeFixture, *string) {
	t.Helper()
	f := newReserveRuntimeFixture(t)
	// Disable while the backup is serving; the primary must not be selected.
	writeReserveAutomatic(t, f.e, f.backup, f.plan.node.ID, true)
	var err error
	f.plan, err = f.e.s.prepareReserveRuntime(f.ctx, f.plan.node.ID, f.plan.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	f.binding, err = f.e.s.bindReserveRuntime(f.ctx, f.plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	description := "BROray - mix-auto"
	target := commitFixtureTarget(t, f)
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
		if !compat.FenceHeld(ctx) || want != description {
			return compat.OwnedInterfaceTarget{}, errors.New("description mismatch")
		}
		target.DescriptionLine = `    description "` + description + `"`
		return target, nil
	}
	f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) { return "BROray - A", nil }
	f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error { description = "BROray - A"; return nil }
	if err := os.WriteFile(f.e.d.Path("config", "interface.json"), []byte(`{"fixture":"owned-receipt"}`), 0600); err != nil {
		t.Fatal(err)
	}
	return f, &description
}

func TestReserveDisablePreservesBackupRuntime(t *testing.T) {
	f, description := reserveDisableFixture(t)
	before := f.process.RuntimeSnapshot()
	config, err := os.ReadFile(f.e.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	active, err := os.ReadFile(ActiveFile(f.e.d))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision); err != nil {
		t.Fatal("disable", err)
	}
	prefs, err := readReservePreferences(f.e.d)
	if err != nil || prefs.Enabled || prefs.PrimaryServerID != f.backup || len(prefs.BackupServerIDs) != 1 || prefs.BackupServerIDs[0] != f.plan.node.ID {
		t.Fatal("roster/selection changed", prefs, err)
	}
	if *description != "BROray - A" || f.process.RuntimeSnapshot() != before {
		t.Fatal("disable restarted or changed runtime", *description)
	}
	after, err := os.ReadFile(f.e.d.Root.XrayConfig())
	if err != nil || !bytes.Equal(config, after) {
		t.Fatal("configuration changed", err)
	}
	after, err = os.ReadFile(ActiveFile(f.e.d))
	if err != nil || !bytes.Equal(active, after) {
		t.Fatal("active selection changed", err)
	}
	if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
		t.Fatal("uncommitted disable", pending, err)
	}
}

func TestReserveDisableRejectsStaleAuthorityBeforeMutation(t *testing.T) {
	for _, kind := range []string{"revision", "generation", "unfenced"} {
		t.Run(kind, func(t *testing.T) {
			f, description := reserveDisableFixture(t)
			revision, binding, ctx := f.plan.revision, f.binding, f.ctx
			if kind == "revision" {
				revision = "stale"
			}
			if kind == "generation" {
				binding.snapshot.Generation++
			}
			if kind == "unfenced" {
				ctx = context.Background()
			}
			before, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.e.s.disableReserve(ctx, binding, revision); err == nil {
				t.Fatal("stale authority accepted")
			}
			after, err := readReservePreferences(f.e.d)
			if err != nil || after.Revision != before.Revision || *description != "BROray - mix-auto" {
				t.Fatal("refusal mutated state", err)
			}
			if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
				t.Fatal("refusal created journal", pending, err)
			}
		})
	}
}

func TestReserveDisableRetainsRecoveryJournalOnRenameFailure(t *testing.T) {
	f, _ := reserveDisableFixture(t)
	before := f.process.RuntimeSnapshot()
	f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error { return errors.New("router save failed") }
	err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision)
	if !errors.Is(err, errReserveTransactionPending) {
		t.Fatal("missing recovery requirement", err)
	}
	if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || !pending {
		t.Fatal("recovery journal lost", pending, err)
	}
	if f.process.RuntimeSnapshot() != before || ActiveID(f.e.d) != f.plan.node.ID {
		t.Fatal("failed disable changed runtime")
	}
}

package servers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

func reserveSwitchFixture(t *testing.T) (reserveRuntimeFixture, *reserveMonitor, reserveMonitorResult) {
	t.Helper()
	f := newReserveRuntimeFixture(t)
	m := &reserveMonitor{}
	if _, err := f.e.s.monitorReserveWith(f.ctx, m, reserveVerifiedOutcome); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.state.LastSwitchAt = now.Add(-time.Minute)
	m.state.EvidenceSince = m.state.LastSwitchAt
	m.state.ActiveFailures = 2
	m.state.Samples[m.state.ActiveID] = reserve.Sample{ServerID: m.state.ActiveID, Fingerprint: m.state.Settings.Primary.Fingerprint, Identity: m.state.ActiveIdentity, Verdict: "unhealthy", Verified: true, StartedAt: now.Add(-15 * time.Second), FinishedAt: now.Add(-14 * time.Second)}
	b := m.state.Settings.Backups[0]
	// Prior readiness history is synthetic. Runtime generation, configuration,
	// actual core validation and both owned SOCKS listeners are exercised below.
	m.state.Samples[b.ID] = reserve.Sample{ServerID: b.ID, Fingerprint: b.Fingerprint, Identity: "fixture-ready", Verdict: "healthy", Verified: true, StartedAt: now.Add(-2 * time.Second), FinishedAt: now.Add(-time.Second)}
	result, err := f.e.s.monitorReserve(f.ctx, m)
	if err != nil || result.Intent == nil {
		t.Fatal("fixture intent", err)
	}
	target := commitFixtureTarget(t, f)
	target.DescriptionLine = `    description "BROray - mix-auto"`
	verify := func(ctx context.Context, desc string) (compat.OwnedInterfaceTarget, error) {
		if !compat.FenceHeld(ctx) || desc != "BROray - mix-auto" {
			return compat.OwnedInterfaceTarget{}, errors.New("invalid fixture request")
		}
		return target, nil
	}
	f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = verify
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = verify
	if err := os.WriteFile(f.e.d.Path("config", "interface.json"), []byte(`{"fixture":"owned-router-receipt"}`), 0600); err != nil {
		t.Fatal(err)
	}
	f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, config []byte) error {
		path := filepath.Join(t.TempDir(), "validate.json")
		if err := os.WriteFile(path, config, 0600); err != nil {
			return err
		}
		return xray.Test(ctx, f.e.d.Root.XrayBinary(), path)
	}
	return f, m, result
}

func TestReserveSwitchTransactionCommitsOwnedReplacement(t *testing.T) {
	for _, exited := range []bool{false, true} {
		f, m, result := reserveSwitchFixture(t)
		if exited {
			reserveTerminateOwnedFixture(t, f)
		}
		receiptPath := f.e.d.Path("config", "interface.json")
		beforeReceipt, err := os.ReadFile(receiptPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.e.s.switchReserve(f.ctx, m, result); err != nil {
			t.Fatal("switch", err)
		}
		if ActiveID(f.e.d) != f.backup || m.state.Pending != nil || m.state.ActiveID != f.backup || m.state.Reason != "switched" || m.binding == nil || m.binding.snapshot.Generation <= result.Guard.snapshot.Generation {
			t.Fatal("switch not acknowledged", m.state)
		}
		if _, err := f.e.s.verifyReserveCommit(f.ctx, *m.binding, "BROray - mix-auto"); err != nil {
			t.Fatal("live proof", err)
		}
		if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
			t.Fatal("journal not committed", pending, err)
		}
		afterReceipt, err := os.ReadFile(receiptPath)
		if err != nil || !bytes.Equal(beforeReceipt, afterReceipt) {
			t.Fatal("switch rewrote interface", err)
		}
		if err := f.e.s.switchReserve(f.ctx, m, result); err == nil {
			t.Fatal("replayed intent accepted")
		}
	}
}

func TestReserveSwitchEmergencyPersistsAndReturns(t *testing.T) {
	f, m, original := reserveSwitchFixture(t)
	s := f.e.s
	primary := original.Guard.plan.node.ID
	temporary := f.e.addServer(t, "trojan://pw@127.0.0.1:3#Temporary")
	plan, err := s.prepareReserveRuntimeFor(f.ctx, temporary, original.Guard.plan.endpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	m.state.Pending = nil
	backup := m.state.Samples[f.backup]
	backup.Verdict = "unhealthy"
	m.state.Samples[f.backup] = backup
	now := time.Now()
	sample := reserve.Sample{ServerID: temporary, Fingerprint: plan.node.Fingerprint, Identity: "fixture-auto-choice", Verdict: "healthy", StartedAt: now.Add(-time.Millisecond), FinishedAt: now, Verified: true}
	state, intents := reserve.Advance(m.state, reserve.Event{Kind: "auto-choice", Sample: sample}, now, reserve.DefaultPolicy())
	if len(intents) != 1 {
		t.Fatal("emergency intent missing", state)
	}
	m.state = state
	cfg, ok := s.readAutoConfig()
	if !ok {
		t.Fatal("fixture auto-selection config missing")
	}
	result := reserveMonitorResult{Intent: &intents[0], Guard: original.Guard, AutoConfigSHA: cfg.hash}
	if err := s.switchReserve(f.ctx, m, result); err != nil {
		t.Fatal("emergency switch", err)
	}
	prefs, err := readReservePreferences(f.e.d)
	if err != nil || prefs.Emergency == nil || prefs.Emergency.ServerID != temporary ||
		ActiveID(f.e.d) != temporary || prefs.PrimaryServerID != primary || len(prefs.BackupServerIDs) != 1 {
		t.Fatal("temporary selection not committed with original roster", prefs, err)
	}
	// Rehydrate from disk and the owned runtime with an empty monitor.
	resumed := &reserveMonitor{}
	if _, err := s.monitorReserveWith(f.ctx, resumed, reserveVerifiedOutcome); err != nil || resumed.state.Emergency.ID != temporary {
		t.Fatal("temporary selection did not rehydrate", err)
	}
	returned, err := s.monitorReserveWith(f.ctx, resumed, reserveVerifiedOutcome)
	if err != nil || returned.Intent == nil || returned.Intent.Reason != "roster-recovered" || returned.Intent.To != primary {
		t.Fatal("roster recovery not selected", returned.Intent, err)
	}
	if err := s.switchReserve(f.ctx, resumed, returned); err != nil {
		t.Fatal("return from emergency", err)
	}
	prefs, err = readReservePreferences(f.e.d)
	if err != nil || prefs.Emergency != nil || ActiveID(f.e.d) != primary || !prefs.Enabled {
		t.Fatal("return did not clear temporary selection", prefs, err)
	}
}

func TestReserveSwitchTransactionRejectsStaleAuthority(t *testing.T) {
	for _, kind := range []string{"validation", "intent", "expired", "record", "generation", "unfenced"} {
		t.Run(kind, func(t *testing.T) {
			f, m, result := reserveSwitchFixture(t)
			ctx := f.ctx
			switch kind {
			case "validation":
				f.e.d.Svc.XrayValidateConfig = func(context.Context, []byte) error { return errors.New("invalid config") }
			case "intent":
				result.Intent.Token++
			case "expired":
				sample := m.state.Samples[f.backup]
				sample.FinishedAt = time.Now().Add(-3 * time.Minute)
				m.state.Samples[f.backup] = sample
			case "record":
				r, err := ReadRecord(f.e.d, f.backup)
				if err != nil {
					t.Fatal(err)
				}
				r["password"] = "changed"
				if err := WriteRecord(f.e.d, r); err != nil {
					t.Fatal(err)
				}
			case "generation":
				if err := f.process.Restart(ctx); err != nil {
					t.Fatal(err)
				}
			case "unfenced":
				ctx = context.Background()
			}
			before := f.process.RuntimeSnapshot()
			config, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			if err := f.e.s.switchReserve(ctx, m, result); err == nil {
				t.Fatal("stale authority accepted")
			}
			after, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil || !bytes.Equal(config, after) || f.process.RuntimeSnapshot() != before || ActiveID(f.e.d) != f.plan.node.ID {
				t.Fatal("preflight mutated runtime", err)
			}
			if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
				t.Fatal("preflight left transaction", pending, err)
			}
		})
	}
}

func TestReserveSwitchTransactionRetainsUnconfirmedCommit(t *testing.T) {
	f, m, result := reserveSwitchFixture(t)
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(context.Context, string) (compat.OwnedInterfaceTarget, error) {
		return compat.OwnedInterfaceTarget{}, errors.New("router proof unavailable")
	}
	if err := f.e.s.switchReserve(f.ctx, m, result); !errors.Is(err, errReserveTransactionPending) {
		t.Fatal("unconfirmed transaction not retained", err)
	}
	if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || !pending {
		t.Fatal("lost recovery journal", err)
	}
	j, err := reserve.OpenTransactionJournal(f.e.d.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	r, err := j.Read(f.ctx)
	if err != nil || r.Phase != "prepared" || string(r.Previous.Active.Data) != f.plan.node.ID+"\n" {
		t.Fatal("bad rollback image", r.Phase, err)
	}
	if _, err := f.e.s.monitorReserve(f.ctx, &reserveMonitor{}); !errors.Is(err, errReserveTransactionPending) {
		t.Fatal("fresh monitor bypassed pending transaction", err)
	}
	if m.state.Pending == nil || m.state.ActiveID != f.plan.node.ID {
		t.Fatal("unconfirmed switch acknowledged")
	}
}

func TestReserveSwitchTransactionWaitsForRouterConvergence(t *testing.T) {
	f, m, result := reserveSwitchFixture(t)
	verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
	calls := 0
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, description string) (compat.OwnedInterfaceTarget, error) {
		calls++
		if calls <= 2 {
			return compat.OwnedInterfaceTarget{}, errors.New("router reconnecting")
		}
		return verify(ctx, description)
	}
	if err := f.e.s.switchReserve(f.ctx, m, result); err != nil {
		t.Fatal("transient reconnect prevented commit", err)
	}
	if calls < 4 || m.state.Reason != "switched" {
		t.Fatal("missing converged live proof", calls, m.state.Reason)
	}
}

func TestReserveSwitchCommitRejectsReplacedCore(t *testing.T) {
	f, _, _ := reserveSwitchFixture(t)
	previous := f.binding
	core, err := os.ReadFile(f.e.d.Root.XrayBinary())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.process.Stop(f.ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !f.process.RuntimeSnapshot().ExitConfirmed && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !f.process.RuntimeSnapshot().ExitConfirmed {
		t.Fatal("owned stop not published")
	}
	if err := os.Rename(f.e.d.Root.XrayBinary(), f.e.d.Root.XrayBinary()+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.e.d.Root.XrayBinary(), core, 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.process.Start(f.ctx); err != nil {
		t.Fatal(err)
	}
	var next reserveActiveBinding
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		next, err = f.e.s.bindReserveRuntime(f.ctx, f.plan, nil)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("replacement core fixture", err)
	}
	target, err := f.e.d.Svc.VerifyOwnedInterfaceTarget(f.ctx, "BROray - mix-auto")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.s.verifyReserveCommit(f.ctx, next, "BROray - mix-auto"); err != nil {
		t.Fatal("new core is independently valid", err)
	}
	if err := f.e.s.verifyReserveSwitchCommit(f.ctx, previous, next, target); err == nil {
		t.Fatal("different core accepted under old validation authority")
	}
}

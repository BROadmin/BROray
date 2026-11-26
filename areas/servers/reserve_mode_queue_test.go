package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

func reserveModeQueueFixture(t *testing.T) (reserveRuntimeFixture, *string) {
	t.Helper()
	return reserveModeQueueFromRuntime(t, newReserveRuntimeFixture(t))
}

func reserveModeQueueFromRuntime(t *testing.T, f reserveRuntimeFixture) (reserveRuntimeFixture, *string) {
	t.Helper()
	writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
	rec, err := ReadRecord(f.e.d, f.plan.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := XrayConfig(rec, f.e.s.genSettings(f.ctx), RecordPath(f.e.d, f.plan.node.ID))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := f.process.ReplaceIfCurrent(f.ctx, f.process.RuntimeSnapshot(), func() error { return platform.WriteFileAtomic(f.e.d.Root.XrayConfig(), ordinary, 0600) })
	if err != nil {
		t.Fatal(err)
	}
	target := commitFixtureTarget(t, f)
	deadline := time.Now().Add(5 * time.Second)
	for {
		identity, e := reserve.CaptureProcess(snap.PID, f.e.d.Root.XrayBinary())
		if e == nil {
			if _, e = reserve.VerifyListener(identity, target.Upstream); e == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("ordinary core did not bind", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	description := "BROray - A"
	verify := func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
		if !compat.FenceHeld(ctx) || want != description {
			return compat.OwnedInterfaceTarget{}, errors.New("unowned description")
		}
		target.DescriptionLine = `    description "` + description + `"`
		return target, nil
	}
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = verify
	f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = verify
	f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) { return "BROray - A", nil }
	f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
		p, e := readReservePreferences(f.e.d)
		if e != nil {
			return e
		}
		description = "BROray - A"
		if p.Enabled {
			description = "BROray - mix-auto"
		}
		return nil
	}
	f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, data []byte) error {
		p := filepath.Join(t.TempDir(), "validate.json")
		if err := os.WriteFile(p, data, 0600); err != nil {
			return err
		}
		return xray.Test(ctx, f.e.d.Root.XrayBinary(), p)
	}
	if err := os.WriteFile(f.e.d.Path("config", "interface.json"), []byte(`{"fixture":"owned"}`), 0600); err != nil {
		t.Fatal(err)
	}
	f.release()
	guard, err := platform.TryLock(f.e.d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Release(); err != nil {
			t.Error(err)
		}
	})
	if err := f.e.d.Lock.ReconcileDaemonReserve(f.e.d.Root, guard, false, nil); err != nil {
		t.Fatal(err)
	}
	return f, &description
}

func reserveModeRequest(t *testing.T, enabled bool, revision, nonce string) resp {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"enabled": enabled, "expectedRevision": revision, "nonce": nonce})
	return call(t, "POST", "http://example.com/api/servers/reserve-mode.cgi", string(b), map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"})
}

func TestReserveModeQueueEnablesOrdinaryThenDisablesWithoutRestart(t *testing.T) {
	f, desc := reserveModeQueueFixture(t)
	prefs, err := readReservePreferences(f.e.d)
	if err != nil {
		t.Fatal(err)
	}
	before := f.process.RuntimeSnapshot()
	r := reserveModeRequest(t, true, prefs.Revision, strings.Repeat("ab", 16))
	if r.Code != 202 {
		t.Fatalf("enable not queued: %d %s", r.Code, r.Raw)
	}
	if f.process.RuntimeSnapshot() != before || *desc != "BROray - A" {
		t.Fatal("HTTP handler mutated runtime")
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	waitReq(t, f.e, "q-"+strings.Repeat("ab", 16), "completed")
	prefs, err = readReservePreferences(f.e.d)
	if err != nil || !prefs.Enabled || *desc != "BROray - mix-auto" || ActiveID(f.e.d) != f.plan.node.ID {
		t.Fatal("enable result", prefs, err, *desc)
	}
	config, err := os.ReadFile(f.e.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reserve.DiagnosticEndpoint(config); !ok {
		t.Fatal("missing monitor inbound")
	}
	after := f.process.RuntimeSnapshot()
	if !after.Running || after == before {
		t.Fatal("new owned generation not installed")
	}
	if err := f.e.d.Queue.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	r = reserveModeRequest(t, false, prefs.Revision, strings.Repeat("cd", 16))
	if r.Code != 202 {
		t.Fatal(r.Raw)
	}
	waitReq(t, f.e, "q-"+strings.Repeat("cd", 16), "completed")
	if f.process.RuntimeSnapshot() != after || *desc != "BROray - A" {
		t.Fatal("disable restarted core or kept reserve description")
	}
	current, _ := os.ReadFile(f.e.d.Root.XrayConfig())
	if !bytes.Equal(current, config) {
		t.Fatal("disable changed active config")
	}
	if f.e.d.Lock.Busy() {
		t.Fatal("confirmed transaction retained fence")
	}
}

func TestReserveModeQueueRejectsStaleRevisionAndRetainsRenameFailure(t *testing.T) {
	for _, kind := range []string{"stale", "rename"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := reserveModeQueueFixture(t)
			prefs, _ := readReservePreferences(f.e.d)
			before := f.process.RuntimeSnapshot()
			revision := prefs.Revision
			if kind == "stale" {
				revision = strings.Repeat("0", 64)
			} else {
				f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error { return errors.New("rename failed") }
			}
			nonce := strings.Repeat("ef", 16)
			r := reserveModeRequest(t, true, revision, nonce)
			if r.Code != 202 {
				t.Fatalf("not queued: %d %s", r.Code, r.Raw)
			}
			go f.e.d.Queue.Run(f.e.d.Ctx)
			waitReq(t, f.e, "q-"+nonce, "failed")
			pending, err := reserve.TransactionPending(context.Background(), f.e.d.Root)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "stale" {
				if pending || f.e.d.Lock.Busy() || before != f.process.RuntimeSnapshot() {
					t.Fatal("stale request mutated")
				}
			} else {
				q, _ := f.e.d.Queue.Request("q-" + nonce)
				if !pending || !f.e.d.Lock.Busy() || q.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" {
					t.Fatal("uncertain enable lost recovery", q)
				}
			}
		})
	}
}

func TestReserveModeQueueRepairsMissingOwnedDiagnostic(t *testing.T) {
	f, description := reserveModeQueueFixture(t)
	writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, true)
	*description = "BROray - mix-auto"
	prefs, err := readReservePreferences(f.e.d)
	if err != nil {
		t.Fatal(err)
	}
	before := f.process.RuntimeSnapshot()
	nonce := strings.Repeat("a7", 16)
	status, err := f.e.s.reserveStatus()
	if err != nil || status.(map[string]any)["runtimeRepairRequired"] != true {
		t.Fatal("status did not identify the missing diagnostic", err)
	}
	response := reserveModeRequest(t, true, prefs.Revision, nonce)
	if response.Code != 202 {
		t.Fatalf("repair not queued: %d %s", response.Code, response.Raw)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	waitReq(t, f.e, "q-"+nonce, "completed")
	config, err := os.ReadFile(f.e.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reserve.DiagnosticEndpoint(config); !ok {
		t.Fatal("enabled reserve still lacks the diagnostic listener after applying its mode")
	}
	after, err := readReservePreferences(f.e.d)
	if err != nil || after.Revision != prefs.Revision || ActiveID(f.e.d) != f.plan.node.ID {
		t.Fatal("repair changed the saved roster or original active server", err)
	}
	if current := f.process.RuntimeSnapshot(); !current.Running || current == before {
		t.Fatal("repair did not replace the original owned Xray generation")
	}
	if f.e.d.Lock.Busy() || *description != "BROray - mix-auto" {
		t.Fatal("repair retained its fence or changed the managed interface")
	}
	status, err = f.e.s.reserveStatus()
	if err != nil || status.(map[string]any)["runtimeRepairRequired"] != false {
		t.Fatal("status did not confirm the restored diagnostic", err)
	}
}

func TestReserveModeQueueRejectsUnsafeRequest(t *testing.T) {
	e := newEnv(t)
	_ = e
	for _, body := range []string{`{"enabled":true,"enabled":false,"expectedRevision":"` + strings.Repeat("a", 64) + `","nonce":"` + strings.Repeat("b", 32) + `"}`, `{"enabled":null,"expectedRevision":"` + strings.Repeat("a", 64) + `","nonce":"` + strings.Repeat("b", 32) + `"}`} {
		r := call(t, "POST", "http://example.com/api/servers/reserve-mode.cgi", body, map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"})
		if r.Code != 400 {
			t.Fatalf("unsafe schema: %d %s", r.Code, r.Raw)
		}
	}
	r := call(t, "POST", "http://example.com/api/servers/reserve-mode.cgi", `{}`, nil)
	if r.Code != 403 {
		t.Fatalf("unprotected request: %d", r.Code)
	}
}

func TestReserveRepairRollbackPreservesUnchangedOwnedDescription(t *testing.T) {
	f, description := reserveModeQueueFixture(t)
	writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, true)
	*description = "BROray - mix-auto"
	prefs, err := readReservePreferences(f.e.d)
	if err != nil {
		t.Fatal(err)
	}
	configPath := f.e.d.Root.XrayConfig()
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := f.e.d.Path("config", "interface.json")
	originalReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	before := f.process.RuntimeSnapshot()
	var rejectReplacement atomic.Bool
	rejectReplacement.Store(true)
	verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, name string) (compat.OwnedInterfaceTarget, error) {
		config, err := os.ReadFile(configPath)
		if err != nil {
			return compat.OwnedInterfaceTarget{}, err
		}
		if _, diagnostic := reserve.DiagnosticEndpoint(config); diagnostic && rejectReplacement.Load() {
			return compat.OwnedInterfaceTarget{}, errors.New("new runtime readback unavailable")
		}
		return verify(ctx, name)
	}
	var syncCalls atomic.Int32
	f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
		syncCalls.Add(1)
		return errors.New("concurrent interface read owns the metadata mutex")
	}
	nonce := strings.Repeat("d8", 16)
	if r := reserveModeRequest(t, true, prefs.Revision, nonce); r.Code != 202 {
		t.Fatalf("repair not queued: %d %s", r.Code, r.Raw)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	deadline := time.Now().Add(35 * time.Second)
	for {
		result, ok := f.e.d.Queue.Request("q-" + nonce)
		if ok && result.State == "failed" {
			if result.ErrorCode != "RESERVE_MODE_ROLLED_BACK" {
				t.Fatalf("previous connection restored, but repair did not finalize its rollback: %s, metadata sync calls %d", result.ErrorCode, syncCalls.Load())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("repair never ended with rollback: %+v", result)
		}
		time.Sleep(20 * time.Millisecond)
	}
	current := f.process.RuntimeSnapshot()
	config, configErr := os.ReadFile(configPath)
	receipt, receiptErr := os.ReadFile(receiptPath)
	after, prefsErr := readReservePreferences(f.e.d)
	pending, journalErr := reserve.TransactionPending(context.Background(), f.e.d.Root)
	if configErr != nil || receiptErr != nil || prefsErr != nil || journalErr != nil ||
		!current.Running || current.Generation <= before.Generation ||
		!bytes.Equal(config, originalConfig) || !bytes.Equal(receipt, originalReceipt) ||
		after.Revision != prefs.Revision || ActiveID(f.e.d) != f.plan.node.ID ||
		*description != "BROray - mix-auto" || syncCalls.Load() != 0 || pending || f.e.d.Lock.Busy() {
		t.Fatal("rollback did not preserve the working connection, its identity and a clear queue")
	}
	rejectReplacement.Store(false)
	secondNonce := strings.Repeat("e8", 16)
	if r := reserveModeRequest(t, true, prefs.Revision, secondNonce); r.Code != 202 {
		t.Fatalf("a later repair was blocked: %d %s", r.Code, r.Raw)
	}
	waitReq(t, f.e, "q-"+secondNonce, "completed")
	config, err = os.ReadFile(configPath)
	if _, diagnostic := reserve.DiagnosticEndpoint(config); err != nil || !diagnostic || f.e.d.Lock.Busy() {
		t.Fatal("later repair did not restore diagnostic runtime", err)
	}
}

func TestReserveModeMonitorEnqueuesWithoutOrdinaryAutoConfig(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@127.0.0.1:1#A")
	b := e.addServer(t, "trojan://pw@127.0.0.1:2#B")
	writeReserveAutomatic(t, e, a, b, true)
	_ = e.s.enqueueDue()
	r, ok := e.d.Queue.FindLive(compat.ReserveSwitchAction, compat.ReserveOperationTarget)
	if !ok {
		t.Fatal("enabled reserve has no monitor queue request")
	}
	_ = e.s.enqueueDue()
	again, _ := e.d.Queue.FindLive(compat.ReserveSwitchAction, compat.ReserveOperationTarget)
	if again.RequestID != r.RequestID {
		t.Fatal("monitor overlap")
	}
}

func TestReserveRepairAllowsMeasuredReadbackAfterTransientNotReady(t *testing.T) {
	f, description := reserveModeQueueFixture(t)
	writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, true)
	*description = "BROray - mix-auto"
	prefs, err := readReservePreferences(f.e.d)
	if err != nil {
		t.Fatal(err)
	}
	verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
	var observations atomic.Int32
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, name string) (compat.OwnedInterfaceTarget, error) {
		config, err := os.ReadFile(f.e.d.Root.XrayConfig())
		if err != nil {
			return compat.OwnedInterfaceTarget{}, err
		}
		if _, diagnostic := reserve.DiagnosticEndpoint(config); !diagnostic {
			return verify(ctx, name)
		}
		// A full live target proof includes two native running-config reads.
		timer := time.NewTimer(4400 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return compat.OwnedInterfaceTarget{}, ctx.Err()
		case <-timer.C:
		}
		if observations.Add(1) == 1 {
			return compat.OwnedInterfaceTarget{}, errors.New("proxy not ready after child replacement")
		}
		return verify(ctx, name)
	}
	nonce := strings.Repeat("f8", 16)
	if r := reserveModeRequest(t, true, prefs.Revision, nonce); r.Code != 202 {
		t.Fatalf("repair not queued: %d %s", r.Code, r.Raw)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	deadline := time.Now().Add(36 * time.Second)
	for {
		result, ok := f.e.d.Queue.Request("q-" + nonce)
		if ok && result.State == "completed" {
			break
		}
		if ok && result.State == "failed" {
			t.Fatalf("repair rolled back despite an eventually ready owned interface: %s after %d observations", result.ErrorCode, observations.Load())
		}
		if time.Now().After(deadline) {
			t.Fatalf("slow repair never completed: %+v", result)
		}
		time.Sleep(20 * time.Millisecond)
	}
	config, err := os.ReadFile(f.e.d.Root.XrayConfig())
	after, prefsErr := readReservePreferences(f.e.d)
	pending, journalErr := reserve.TransactionPending(context.Background(), f.e.d.Root)
	if _, diagnostic := reserve.DiagnosticEndpoint(config); err != nil || !diagnostic || prefsErr != nil ||
		journalErr != nil || pending || f.e.d.Lock.Busy() || observations.Load() != 3 ||
		after.Revision != prefs.Revision || ActiveID(f.e.d) != f.plan.node.ID || *description != "BROray - mix-auto" {
		t.Fatal("successful repair did not preserve the active primary, roster, interface and clear journal")
	}
}

func reserveQueueAuthority(t *testing.T, f reserveRuntimeFixture) {
	t.Helper()
	f.release()
	guard, err := platform.TryLock(f.e.d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Release(); err != nil {
			t.Error(err)
		}
	})
	if err := f.e.d.Lock.ReconcileDaemonReserve(f.e.d.Root, guard, false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestReserveModeQueuedMonitorUsesOwnedProbe(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	reserveQueueAuthority(t, f)
	before := f.process.RuntimeSnapshot()
	prefs, _ := readReservePreferences(f.e.d)
	r, err := f.e.d.Queue.Enqueue(compat.ReserveSwitchAction, compat.ReserveOperationTarget, "RESERVE_MONITOR", prefs.Revision)
	if err != nil {
		t.Fatal(err)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	waitReq(t, f.e, r.RequestID, "completed")
	m := f.e.s.reserveMon
	m.mu.Lock()
	sample := m.state.Samples[f.plan.node.ID]
	failures := m.state.ActiveFailures
	m.mu.Unlock()
	if !sample.Verified || sample.Verdict != "unhealthy" || failures != 1 {
		t.Fatalf("real refused proxy not counted: %+v failures=%d", sample, failures)
	}
	if f.process.RuntimeSnapshot() != before || f.e.d.Lock.Busy() {
		t.Fatal("monitor mutated main runtime or retained clean fence")
	}
}

func TestReserveModeQueuedMonitorSwitchesUnderSameFence(t *testing.T) {
	f, m, _ := reserveSwitchFixture(t)
	// The fixture already rechecked the backup after confirming the third
	// failure. The new active probe and the runtime switch are real.
	m.state.Pending = nil
	m.state.ActiveFailures = reserve.DefaultPolicy().Failures
	m.scheduler = reserve.ProbeScheduler{}
	prior := m.state.Samples[m.state.ActiveID]
	prior.StartedAt = time.Now().Add(-15 * time.Second)
	prior.FinishedAt = prior.StartedAt.Add(time.Second)
	m.state.Samples[m.state.ActiveID] = prior
	f.e.s.reserveMon = m
	reserveQueueAuthority(t, f)
	prefs, _ := readReservePreferences(f.e.d)
	r, err := f.e.d.Queue.Enqueue(compat.ReserveSwitchAction, compat.ReserveOperationTarget, "RESERVE_MONITOR", prefs.Revision)
	if err != nil {
		t.Fatal(err)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	waitReq(t, f.e, r.RequestID, "completed")
	if ActiveID(f.e.d) != f.backup || f.e.d.Lock.Busy() {
		t.Fatal("queued monitor did not commit backup")
	}
	m.mu.Lock()
	pending := m.state.Pending
	reason := m.state.Reason
	m.mu.Unlock()
	if pending != nil || reason != "switched" {
		t.Fatal("policy did not acknowledge durable switch", reason)
	}
}

func TestReserveModeEnableOutsideRosterSelectsPrimary(t *testing.T) {
	f, _ := reserveModeQueueFixture(t)
	c := f.e.addServer(t, "trojan://pw@127.0.0.1:3#C")
	writeReserveAutomatic(t, f.e, f.backup, c, false)
	prefs, _ := readReservePreferences(f.e.d)
	nonce := strings.Repeat("12", 16)
	r := reserveModeRequest(t, true, prefs.Revision, nonce)
	if r.Code != 202 {
		t.Fatal(r.Raw)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	waitReq(t, f.e, "q-"+nonce, "completed")
	if ActiveID(f.e.d) != f.backup {
		t.Fatal("configured primary not selected")
	}
}

func TestReserveModeEnableRejectsChangedInputsBeforeJournal(t *testing.T) {
	f, _ := reserveModeQueueFixture(t)
	prefs, _ := readReservePreferences(f.e.d)
	before := f.process.RuntimeSnapshot()
	validate := f.e.d.Svc.XrayValidateConfig
	f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, data []byte) error {
		if err := validate(ctx, data); err != nil {
			return err
		}
		rec, err := ReadRecord(f.e.d, f.backup)
		if err != nil {
			return err
		}
		rec["password"] = "changed"
		return WriteRecord(f.e.d, rec)
	}
	nonce := strings.Repeat("34", 16)
	r := reserveModeRequest(t, true, prefs.Revision, nonce)
	if r.Code != 202 {
		t.Fatal(r.Raw)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	waitReq(t, f.e, "q-"+nonce, "failed")
	pending, err := reserve.TransactionPending(context.Background(), f.e.d.Root)
	if err != nil || pending || f.e.d.Lock.Busy() || before != f.process.RuntimeSnapshot() {
		t.Fatal("changed inputs mutated runtime", err)
	}
}

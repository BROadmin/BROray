package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

const outsideReserveSelection = "RESERVE_SERVER_OUTSIDE_ROSTER"

func manualActivationRequest(t *testing.T, id, nonce string) resp {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"id": id, "nonce": nonce})
	return call(t, "POST", "http://example.com/api/servers/activate.cgi", string(b), map[string]string{
		"X-BROray-Queue": "1", "X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json",
	})
}

func confirmedReserveRequest(t *testing.T, id, mode, remove, revision, nonce string) resp {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"id": id, "mode": mode, "removeServerId": remove, "expectedRevision": revision, "nonce": nonce})
	return call(t, "POST", "http://example.com/api/servers/reserve-selection.cgi", string(b), map[string]string{
		"X-BROray-Queue": "1", "X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json",
	})
}

func TestReserveConfirmedSelectionMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, mode                                         string
		full, remove, member, unavailable, stale, syncFail bool
	}{
		{name: "promote-member", mode: "promote", member: true},
		{name: "promote-outside-free-slot", mode: "promote"},
		{name: "promote-outside-full-with-removal", mode: "promote", full: true, remove: true},
		{name: "reject-promotion-without-removal", mode: "promote", full: true},
		{name: "reject-stale-roster", mode: "promote", stale: true},
		{name: "leave-reserve-and-connect", mode: "ordinary"},
		{name: "unavailable-promotion", mode: "promote", unavailable: true},
		{name: "unavailable-exit", mode: "ordinary", unavailable: true},
		{name: "ordinary-metadata-failure-rolls-back", mode: "ordinary", syncFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, desc := reserveModeQueueFixture(t)
			outside := f.e.addServer(t, "trojan://test@127.0.0.1:4#Outside")
			backup := ""
			if tc.full {
				backup = f.e.addServer(t, "trojan://test@127.0.0.1:3#Second")
				if err := compat.WriteJSON(reservePreferencesFile(f.e.d), reservePreferences{1, false, f.plan.node.ID, []string{f.backup, backup}, nil}); err != nil {
					t.Fatal(err)
				}
			}
			f.e.s.reserveSelectionProbe = func(ctx context.Context, id string) (reserve.Sample, error) {
				return f.e.s.probeReserveCandidate(ctx, id, id == outside, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
					result := reserveVerifiedOutcome(ctx, target)
					if tc.unavailable && result.Verdict == "healthy" {
						result.Verdict = "unhealthy"
					}
					return result
				})
			}
			f.e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) { return "BROray - " + name, nil }
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(ctx context.Context) error {
				prefs, err := readReservePreferences(f.e.d)
				if err != nil {
					return err
				}
				if prefs.Enabled {
					*desc = "BROray - mix-auto"
					return nil
				}
				if tc.syncFail {
					return errors.New("test description write failed")
				}
				*desc = "BROray - Outside"
				return nil
			}
			prefs, _ := readReservePreferences(f.e.d)
			runReserveQueue(t, f.e)
			enableNonce := strings.Repeat("96", 16)
			if r := reserveModeRequest(t, true, prefs.Revision, enableNonce); r.Code != 202 {
				t.Fatal(r.Raw)
			}
			if got, code := waitManualReserveRequest(t, f.e, enableNonce); got != "completed" {
				t.Fatal(got, code)
			}
			prefs, _ = readReservePreferences(f.e.d)
			currentConfig, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			target, removed, revision := outside, "", prefs.Revision
			if tc.member {
				target = f.backup
			}
			if tc.remove {
				removed = backup
			}
			if tc.stale {
				revision = strings.Repeat("0", 64)
			}
			nonce := strings.Repeat("97", 16)
			response := confirmedReserveRequest(t, target, tc.mode, removed, revision, nonce)
			wantRefusal := tc.stale || (tc.full && !tc.remove)
			if wantRefusal {
				if response.Code != 409 {
					t.Fatalf("expected safe refusal, got %d", response.Code)
				}
			} else {
				if response.Code != 202 {
					t.Fatalf("expected protected queue admission, got %d", response.Code)
				}
				wantState, wantCode := "completed", ""
				if tc.unavailable {
					wantState, wantCode = "failed", "RESERVE_NO_REACHABLE_SERVER"
				}
				if tc.syncFail {
					wantState, wantCode = "failed", "RESERVE_MODE_ROLLED_BACK"
				}
				if state, code := waitManualReserveRequest(t, f.e, nonce); state != wantState || code != wantCode {
					t.Fatal(state, code)
				}
			}
			result, _ := readReservePreferences(f.e.d)
			config, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil || f.e.d.Lock.Busy() || !f.process.RuntimeSnapshot().Running {
				t.Fatal("runtime or fence unconfirmed", err)
			}
			if wantRefusal || tc.unavailable || tc.syncFail {
				if result.Revision != prefs.Revision || ActiveID(f.e.d) != f.plan.node.ID || *desc != "BROray - mix-auto" || !bytes.Equal(config, currentConfig) {
					t.Fatal("refusal or rollback disturbed the prior state")
				}
				return
			}
			_, diagnostic := reserve.DiagnosticEndpoint(config)
			if ActiveID(f.e.d) != target {
				t.Fatal("wrong active server")
			}
			if tc.mode == "ordinary" {
				if result.Enabled || diagnostic || *desc != "BROray - Outside" || result.PrimaryServerID != prefs.PrimaryServerID ||
					strings.Join(result.BackupServerIDs, ",") != strings.Join(prefs.BackupServerIDs, ",") {
					t.Fatal("exit did not preserve roster and create ordinary runtime")
				}
			} else {
				wantBackups := f.plan.node.ID + "," + f.backup
				if tc.member {
					wantBackups = f.plan.node.ID
				}
				if !result.Enabled || !diagnostic || *desc != "BROray - mix-auto" || result.PrimaryServerID != target || strings.Join(result.BackupServerIDs, ",") != wantBackups {
					t.Fatal("promotion did not install the verified roster and retain mode")
				}
			}
		})
	}
}

func waitManualReserveRequest(t *testing.T, e *fakeEnv, nonce string) (string, string) {
	t.Helper()
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		if row, ok := e.d.Queue.Request("q-" + nonce); ok && row.State != "queued" && row.State != "running" {
			return row.State, row.ErrorCode
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("reserve operation did not finish")
	return "", ""
}

func TestReserveManualOutsideRosterRejectedAtBothEntrypoints(t *testing.T) {
	e, a, b := reserveAutomaticEnv(t)
	outside := e.addServer(t, "trojan://test@127.0.0.1:1#Outside")
	writeReserveAutomatic(t, e, a, b, true)
	r := manualActivationRequest(t, outside, strings.Repeat("44", 16))
	if r.Code != 409 || errCode(r) != outsideReserveSelection || len(e.d.Queue.QueueRows()) != 0 {
		t.Errorf("outside-roster operation was not rejected before queue: %d %s", r.Code, r.Raw)
	}
	r = call(t, "POST", "/api/servers/activate.cgi", `{"id":"`+outside+`"}`, nil)
	if r.Code != 409 || errCode(r) != outsideReserveSelection || ActiveID(e.d) != a || len(e.applied) != 1 {
		t.Errorf("ordinary activation changed a reserved selection: %d %s", r.Code, r.Raw)
	}
}

func TestReserveManualGuardsDeactivateAndDelete(t *testing.T) {
	e, a, b := reserveAutomaticEnv(t)
	writeReserveAutomatic(t, e, a, b, true)
	for _, tc := range []struct{ path, body string }{
		{"/api/servers/deactivate.cgi", `{}`},
		{"/api/servers/delete.cgi", `{"id":"` + b + `"}`},
	} {
		r := call(t, "POST", tc.path, tc.body, nil)
		if r.Code != 409 || errCode(r) != "RESERVE_MODE_ACTIVE" {
			t.Errorf("operation on a reserved server escaped the guard: %s %d", tc.path, r.Code)
		}
	}
	if ActiveID(e.d) != a || !RecordExists(e.d, b) || !e.running.Load() {
		t.Fatal("protected server or its active runtime was changed")
	}
}

func TestReserveManualUnavailableMemberKeepsDiagnostic(t *testing.T) {
	f, desc := reserveModeQueueFixture(t)
	prefs, _ := readReservePreferences(f.e.d)
	runReserveQueue(t, f.e)
	enableNonce := strings.Repeat("54", 16)
	if r := reserveModeRequest(t, true, prefs.Revision, enableNonce); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	if got, code := waitManualReserveRequest(t, f.e, enableNonce); got != "completed" {
		t.Fatal(got, code)
	}
	before := f.process.RuntimeSnapshot()
	nonce := strings.Repeat("55", 16)
	if r := manualActivationRequest(t, f.backup, nonce); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	state, code := waitManualReserveRequest(t, f.e, nonce)
	config, err := os.ReadFile(f.e.d.Root.XrayConfig())
	_, diagnostic := reserve.DiagnosticEndpoint(config)
	if state != "failed" || code != "RESERVE_NO_REACHABLE_SERVER" || err != nil || !diagnostic ||
		ActiveID(f.e.d) != f.plan.node.ID || f.process.RuntimeSnapshot() != before || *desc != "BROray - mix-auto" {
		t.Fatalf("unavailable member disturbed reserve: %s %s diagnostic=%t err=%v", state, code, diagnostic, err)
	}
}

func TestReserveManualRepairWithNoReachableMemberKeepsConnection(t *testing.T) {
	f, desc := reserveModeQueueFixture(t)
	second := f.e.addServer(t, "trojan://test@127.0.0.1:3#Third")
	writeReserveAutomatic(t, f.e, f.backup, second, true)
	*desc = "BROray - mix-auto"
	prefs, _ := readReservePreferences(f.e.d)
	beforeConfig, err := os.ReadFile(f.e.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	before := f.process.RuntimeSnapshot()
	runReserveQueue(t, f.e)
	nonce := strings.Repeat("66", 16)
	if r := reserveModeRequest(t, true, prefs.Revision, nonce); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	state, code := waitManualReserveRequest(t, f.e, nonce)
	afterConfig, err := os.ReadFile(f.e.d.Root.XrayConfig())
	afterPrefs, _ := readReservePreferences(f.e.d)
	if state != "failed" || code != "RESERVE_NO_REACHABLE_SERVER" || err != nil || !bytes.Equal(beforeConfig, afterConfig) ||
		ActiveID(f.e.d) != f.plan.node.ID || f.process.RuntimeSnapshot() != before || afterPrefs.Revision != prefs.Revision || *desc != "BROray - mix-auto" {
		t.Fatalf("failed repair disturbed the live connection: %s %s %v", state, code, err)
	}
}

func TestReserveManualSelectionCommitsThroughProtectedQueue(t *testing.T) {
	f, desc := reserveModeQueueFixture(t)
	f.e.s.reserveSelectionProbe = func(ctx context.Context, id string) (reserve.Sample, error) {
		return f.e.s.probeReserveBackupWith(ctx, id, reserveVerifiedOutcome)
	}
	prefs, _ := readReservePreferences(f.e.d)
	runReserveQueue(t, f.e)
	enableNonce := strings.Repeat("76", 16)
	if r := reserveModeRequest(t, true, prefs.Revision, enableNonce); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	if state, code := waitManualReserveRequest(t, f.e, enableNonce); state != "completed" {
		t.Fatal(state, code)
	}
	prefs, _ = readReservePreferences(f.e.d)
	before := f.process.RuntimeSnapshot()
	nonce := strings.Repeat("77", 16)
	if r := manualActivationRequest(t, f.backup, nonce); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	if state, code := waitManualReserveRequest(t, f.e, nonce); state != "completed" {
		t.Fatal(state, code)
	}
	after, _ := readReservePreferences(f.e.d)
	config, err := os.ReadFile(f.e.d.Root.XrayConfig())
	_, diagnostic := reserve.DiagnosticEndpoint(config)
	if ActiveID(f.e.d) != f.backup || err != nil || !diagnostic || after.Revision != prefs.Revision ||
		!f.process.RuntimeSnapshot().Running || before == f.process.RuntimeSnapshot() || *desc != "BROray - mix-auto" || f.e.d.Lock.Busy() {
		t.Fatal("manual selection did not retain the reserve identity and roster", err)
	}
}

func TestReserveManualRepairSelectsFirstVerifiedMember(t *testing.T) {
	for _, reachablePrimary := range []bool{true, false} {
		name := "first-backup"
		if reachablePrimary {
			name = "primary"
		}
		t.Run(name, func(t *testing.T) {
			f, desc := reserveModeQueueFixture(t)
			backup := f.e.addServer(t, "trojan://test@127.0.0.1:3#Third")
			writeReserveAutomatic(t, f.e, f.backup, backup, true)
			*desc = "BROray - mix-auto"
			var probes []string
			f.e.s.reserveSelectionProbe = func(ctx context.Context, id string) (reserve.Sample, error) {
				probes = append(probes, id)
				return f.e.s.probeReserveBackupWith(ctx, id, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
					result := reserveVerifiedOutcome(ctx, target)
					if !reachablePrimary && id == f.backup && result.Verdict == "healthy" {
						result.Verdict = "unhealthy"
					}
					return result
				})
			}
			prefs, _ := readReservePreferences(f.e.d)
			runReserveQueue(t, f.e)
			nonce := strings.Repeat("88", 16)
			if r := reserveModeRequest(t, true, prefs.Revision, nonce); r.Code != 202 {
				t.Fatal(r.Raw)
			}
			if state, code := waitManualReserveRequest(t, f.e, nonce); state != "completed" {
				t.Fatal(state, code)
			}
			want, order := f.backup, f.backup
			if !reachablePrimary {
				want, order = backup, order+","+backup
			}
			after, _ := readReservePreferences(f.e.d)
			config, err := os.ReadFile(f.e.d.Root.XrayConfig())
			_, diagnostic := reserve.DiagnosticEndpoint(config)
			if ActiveID(f.e.d) != want || strings.Join(probes, ",") != order || err != nil || !diagnostic ||
				after.Revision != prefs.Revision || *desc != "BROray - mix-auto" || f.e.d.Lock.Busy() {
				t.Fatal("repair did not use priority, preserve roster, or clear the fence", probes, err)
			}
		})
	}
}

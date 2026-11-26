package servers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

// The actual core and loopback VLESS peers carry distinct payloads. The
// Keenetic interface fixture has no router I/O.
func TestPendingSourceNativeApplyChecksTrafficAndRollback(t *testing.T) {
	for _, mode := range []string{"ordinary", "reserve", "unhealthy", "unconfirmed", "stopped", "stale config",
		"emergency", "emergency rollback", "emergency stopped", "legacy emergency", "foreign emergency fingerprint"} {
		t.Run(mode, func(t *testing.T) {
			first, next := reserveHotPeer(t, "A"), reserveHotPeer(t, "B")
			f, description := reserveModeQueueFromRuntime(t, newReserveRuntimeFixtureWithLinks(t, first, next))
			id := f.plan.node.ID
			emergency := strings.Contains(mode, "emergency")
			var rosterBackup string
			if emergency {
				rosterBackup = f.e.addServer(t, "trojan://pw@127.0.0.1:17#ConfiguredBackup")
				fingerprint := f.plan.node.Fingerprint
				if mode == "foreign emergency fingerprint" {
					fingerprint = strings.Repeat("9", 64)
				}
				prefs := reservePreferences{1, true, f.backup, []string{rosterBackup}, &reserveEmergency{id, fingerprint}}
				if err := compat.WriteJSON(reservePreferencesFile(f.e.d), prefs); err != nil {
					t.Fatal(err)
				}
				*description = "BROray - mix-auto"
			}
			if mode != "legacy emergency" && (emergency || mode == "reserve" || mode == "unhealthy" || mode == "unconfirmed") {
				if !emergency {
					writeReserveAutomatic(t, f.e, id, f.backup, true)
				}
				if _, err := f.process.ReplaceIfCurrent(f.ctx, f.process.RuntimeSnapshot(), func() error {
					return os.WriteFile(f.e.d.Root.XrayConfig(), f.plan.config, 0600)
				}); err != nil {
					t.Fatal(err)
				}
				*description = "BROray - mix-auto"
				target := commitFixtureTarget(t, f)
				deadline := time.Now().Add(5 * time.Second)
				for {
					identity, err := reserve.CaptureProcess(f.process.RuntimeSnapshot().PID, f.e.d.Root.XrayBinary())
					if err == nil {
						if _, err := reserve.VerifyListener(identity, target.Upstream); err == nil {
							break
						}
					}
					if time.Now().After(deadline) {
						t.Fatal("reserve fixture main listener unavailable")
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			if mode == "stopped" || mode == "emergency stopped" {
				if err := f.process.Stop(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			f.e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) { return "BROray - " + name, nil }
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
				t.Fatal("same description and reserve label do not need a metadata write")
				return nil
			}
			before := f.process.RuntimeSnapshot()
			beforeConfig, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			beforePrefs, err := os.ReadFile(reservePreferencesFile(f.e.d))
			if err != nil {
				t.Fatal(err)
			}
			previous, err := ReadRecord(f.e.d, id)
			if err != nil {
				t.Fatal(err)
			}
			previous["source"] = map[string]any{"type": "subscription", "subscriptionId": "refresh-source"}
			if err := WriteRecord(f.e.d, previous); err != nil {
				t.Fatal(err)
			}
			previousBytes, err := os.ReadFile(RecordPath(f.e.d, id))
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := ParseLink(next, id, map[string]any{"type": "subscription", "subscriptionId": "refresh-source"})
			if err != nil {
				t.Fatal(err)
			}
			candidate["name"] = "A"
			if err := WriteRecord(f.e.d, candidate); err != nil {
				t.Fatal(err)
			}
			staged, err := os.ReadFile(RecordPath(f.e.d, id))
			if err != nil {
				t.Fatal(err)
			}
			configSHA := compat.SHA256Hex(beforeConfig)
			if mode == "stale config" {
				configSHA = compat.SHA256Hex([]byte("old observation"))
			}
			var results []string
			probe := func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
				begin := time.Now()
				owned, err := reserve.VerifyProbeTarget(target)
				if err != nil {
					t.Fatal(err)
				}
				marker, err := reserveHotPayload(ctx, target.Endpoint)
				if err != nil {
					t.Fatal("local proof:", err)
				}
				results = append(results, marker)
				verdict := "healthy"
				if marker == "B" && (mode == "unhealthy" || mode == "emergency rollback") {
					verdict = "unhealthy"
				}
				if marker == "B" && mode == "unconfirmed" {
					verdict = "unknown"
				}
				return reserve.ProbeOutcome{Verdict: verdict, Identity: owned, StartedAt: begin, FinishedAt: time.Now()}
			}
			err = f.e.s.applyPendingSourceProfile(f.ctx, compat.SourceProfileApply{
				SourceID: "refresh-source", ServerID: id, PreviousName: "A", ConfigSHA256: configSHA,
				CandidateSHA256: compat.SHA256Hex(staged), PreviousRecord: previousBytes,
			}, nil, probe)
			actual, readErr := os.ReadFile(f.e.d.Root.XrayConfig())
			if readErr != nil {
				t.Fatal(readErr)
			}
			after := f.process.RuntimeSnapshot()
			reverted := mode == "unhealthy" || mode == "unconfirmed" || mode == "emergency rollback"
			switch {
			case reverted:
				if err == nil || !bytes.Equal(actual, beforeConfig) || after.Generation != before.Generation+2 ||
					len(results) != 2 || results[0] != "B" || results[1] != "A" {
					t.Fatalf("exact VPN rollback not confirmed: %v, probes=%v", err, results)
				}
			case mode == "stale config" || mode == "foreign emergency fingerprint":
				if err == nil || after != before || !bytes.Equal(actual, beforeConfig) || len(results) != 0 {
					t.Fatal("outdated configuration witness was applied")
				}
			case mode == "stopped" || mode == "emergency stopped":
				if err != nil || after.Wanted || after.Running || before.Generation != after.Generation ||
					len(results) != 0 || bytes.Equal(actual, beforeConfig) {
					t.Fatalf("stopped proposal confirmation: %v, probes=%v", err, results)
				}
			default:
				if err != nil || after.Generation != before.Generation+1 || len(results) != 1 || results[0] != "B" ||
					bytes.Equal(actual, beforeConfig) {
					t.Fatalf("new live payload not confirmed: %v, probes=%v", err, results)
				}
			}
			afterPrefs, readErr := os.ReadFile(reservePreferencesFile(f.e.d))
			if readErr != nil || ActiveID(f.e.d) != id {
				t.Fatal("activation changed reserve preferences or active identity", readErr)
			}
			if emergency && !reverted && mode != "foreign emergency fingerprint" {
				prefs, e := decodeReservePreferences(afterPrefs)
				base, renderErr := XrayConfig(candidate, f.e.s.genSettings(f.ctx), RecordPath(f.e.d, id))
				if e != nil || renderErr != nil || prefs.Emergency == nil || prefs.Emergency.ServerID != id ||
					prefs.Emergency.Fingerprint != compat.SHA256Hex(base) || !prefs.Enabled || prefs.PrimaryServerID != f.backup ||
					len(prefs.BackupServerIDs) != 1 || prefs.BackupServerIDs[0] != rosterBackup || bytes.Equal(afterPrefs, beforePrefs) {
					t.Fatalf("emergency fingerprint did not change in place: %+v, %v %v", prefs, e, renderErr)
				}
			} else if !bytes.Equal(afterPrefs, beforePrefs) {
				t.Fatal("unexpected reserve preference write")
			}
			if pending, pErr := reserve.TransactionPending(f.ctx, f.e.d.Root); pErr != nil || pending {
				t.Fatal("settled or untouched transaction leaked a journal", pErr)
			}
			if errors.Is(err, compat.ErrReserveTransactionPending) {
				t.Fatal("confirmed rollback was not recognized")
			}
		})
	}
}

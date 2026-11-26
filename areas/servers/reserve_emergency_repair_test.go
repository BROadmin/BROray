package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

func TestReserveRepairWithPersistedEmergency(t *testing.T) {
	cases := []struct {
		name    string
		winner  int
		invalid bool
		drift   bool
	}{
		{name: "primary-returns"},
		{name: "only-first-reserve-returns", winner: 1},
		{name: "no-reachable-member", winner: -1},
		{name: "foreign-emergency-fingerprint", winner: -1, invalid: true},
		{name: "unexpected-routing-image", winner: -1, drift: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, description := reserveModeQueueFixture(t)
			active := f.plan.node.ID
			members := []string{f.backup, f.e.addServer(t, "trojan://test@127.0.0.1:3#Reserve")}
			fingerprint := f.plan.node.Fingerprint
			if tc.invalid {
				fingerprint = strings.Repeat("d", 64)
			}
			prefs := reservePreferences{
				SchemaVersion: 1, Enabled: true, PrimaryServerID: members[0], BackupServerIDs: members[1:],
				Emergency: &reserveEmergency{ServerID: active, Fingerprint: fingerprint},
			}
			if err := compat.WriteJSON(reservePreferencesFile(f.e.d), prefs); err != nil {
				t.Fatal(err)
			}
			*description = "BROray - mix-auto"
			previous := f.process.RuntimeSnapshot()
			path := f.e.d.Root.XrayConfig()
			originalConfig, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var formatted bytes.Buffer
			if err := json.Indent(&formatted, originalConfig, "", "  "); err != nil {
				t.Fatal(err)
			}
			config := formatted.Bytes()
			if tc.drift {
				config = []byte(strings.Replace(string(config), "\n}", ",\n  \"routing\": {\"rules\": []}\n}", 1))
			}
			if bytes.Equal(originalConfig, config) {
				t.Fatal("fixture should exercise a different config image")
			}
			if err := os.WriteFile(path, config, 0600); err != nil {
				t.Fatal(err)
			}
			snapshot, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			var probes []string
			f.e.s.reserveSelectionProbe = func(ctx context.Context, id string) (reserve.Sample, error) {
				probes = append(probes, id)
				return f.e.s.probeReserveBackupWith(ctx, id, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
					out := reserveVerifiedOutcome(ctx, target)
					if out.Verdict == "healthy" && (tc.winner < 0 || members[tc.winner] != id) {
						out.Verdict = "unhealthy"
					}
					return out
				})
			}
			runReserveQueue(t, f.e)
			nonce := strings.Repeat("e9", 16)
			if r := reserveModeRequest(t, true, snapshot.Revision, nonce); r.Code != http.StatusAccepted {
				t.Fatalf("repair admission returned %d", r.Code)
			}
			state, code := waitManualReserveRequest(t, f.e, nonce)
			after, err := readReservePreferences(f.e.d)
			if err != nil || !after.Enabled || after.PrimaryServerID != members[0] ||
				len(after.BackupServerIDs) != 1 || after.BackupServerIDs[0] != members[1] || *description != "BROray - mix-auto" {
				t.Fatal("repair changed the saved roster or interface", err)
			}
			if tc.winner < 0 {
				if state != "failed" || after.Emergency == nil || *after.Emergency != *prefs.Emergency ||
					ActiveID(f.e.d) != active || f.process.RuntimeSnapshot() != previous {
					t.Fatal("refused repair mutated the emergency connection", state, code)
				}
				wrongAuthority := tc.invalid || tc.drift
				if wrongAuthority && len(probes) != 0 || !wrongAuthority && code != "RESERVE_NO_REACHABLE_SERVER" {
					t.Fatal("unexpected refusal or probe policy", code, probes)
				}
				return
			}
			if state != "completed" || after.Emergency != nil || ActiveID(f.e.d) != members[tc.winner] ||
				len(probes) != tc.winner+1 || probes[len(probes)-1] != members[tc.winner] || f.e.d.Lock.Busy() {
				t.Fatal("emergency diagnostic repair failed", state, code, probes)
			}
			confirmedConfig, err := os.ReadFile(path)
			if _, ok := reserve.DiagnosticEndpoint(confirmedConfig); err != nil || !ok {
				t.Fatal("confirmed repair has no diagnostic listener", err)
			}
		})
	}
}

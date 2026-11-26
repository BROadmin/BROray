package servers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

func TestPendingHotSourceNativeApply(t *testing.T) {
	for _, mode := range []string{"verified", "rollback", "different prior slot", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			a, b, c, next := reserveHotPeer(t, "A"), reserveHotPeer(t, "B"), reserveHotPeer(t, "C"), reserveHotPeer(t, "D")
			f, description := reserveModeQueueFromRuntime(t, newReserveRuntimeFixtureWithLinks(t, a, b))
			d, s, id := f.e.d, f.e.s, f.plan.node.ID
			third := f.e.addServer(t, c)
			prefs := reservePreferences{1, true, f.backup, []string{id, third}, nil}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(compat.WriteJSON(reservePreferencesFile(d), prefs))
			*description = "BROray - mix-auto"
			old, err := ReadRecord(d, id)
			must(err)
			old["source"] = map[string]any{"type": "subscription", "subscriptionId": "hot-test-source"}
			must(WriteRecord(d, old))
			oldRecord, err := os.ReadFile(RecordPath(d, id))
			must(err)
			roster := []string{f.backup, id, third}
			plans := map[string]reserveRuntimePlan{}
			for _, node := range roster {
				plans[node], err = s.prepareReserveStockRuntime(f.ctx, node, f.plan.endpoint, false)
				must(err)
			}
			oldHot, err := s.buildReserveHotPlan(plans[id], roster, plans)
			must(err)
			snapshot, err := f.process.ReplaceIfCurrent(f.ctx, f.process.RuntimeSnapshot(), func() error {
				return os.WriteFile(d.Root.XrayConfig(), oldHot.config, 0600)
			})
			must(err)
			slot, marker := reserve.HotSlot(1), "A"
			if mode == "different prior slot" {
				slot, marker = reserve.HotSlot(0), "B"
			}
			current := func() bool { return f.process.RuntimeSnapshot() == snapshot }
			if !waitPendingSourceReady(f.ctx, current, func() bool {
				return f.process.SelectReserveOutboundIfCurrent(f.ctx, snapshot, slot) == nil
			}) {
				t.Fatal("fixture could not select old hot route")
			}
			if got, err := reserveHotPayload(f.ctx, f.plan.endpoint); err != nil || got != marker {
				t.Fatalf("wrong prior payload %s %v", got, err)
			}
			if mode == "stopped" {
				must(f.process.Stop(f.ctx))
			}
			before := f.process.RuntimeSnapshot()
			beforePrefs, err := os.ReadFile(reservePreferencesFile(d))
			must(err)
			candidate, err := ParseLink(next, id, map[string]any{"type": "subscription", "subscriptionId": "hot-test-source"})
			must(err)
			candidate["name"] = "A"
			must(WriteRecord(d, candidate))
			candidateBytes, err := os.ReadFile(RecordPath(d, id))
			must(err)
			var payloads []string
			probe := func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
				started := time.Now()
				identity, err := reserve.VerifyProbeTarget(target)
				must(err)
				payload, err := reserveHotPayload(ctx, target.Endpoint)
				must(err)
				payloads = append(payloads, payload)
				verdict := "healthy"
				if mode == "rollback" && payload == "D" {
					verdict = "unhealthy"
				}
				return reserve.ProbeOutcome{Verdict: verdict, Identity: identity, StartedAt: started, FinishedAt: time.Now()}
			}
			authority := strings.Repeat("c", 64)
			outcome := s.applyPendingSourceProfile(f.ctx, compat.SourceProfileApply{
				SourceID: "hot-test-source", ServerID: id, PreviousName: "A", PreviousRecord: oldRecord,
				CandidateSHA256: compat.SHA256Hex(candidateBytes), ConfigSHA256: oldHot.configSHA,
				RecoveryAuthority: authority,
			}, nil, probe)
			after := f.process.RuntimeSnapshot()
			config, err := os.ReadFile(d.Root.XrayConfig())
			must(err)
			if _, hot := reserve.HotLayoutSocket(config); !hot {
				t.Fatal("application dropped the existing three-outbound layout")
			}
			switch mode {
			case "verified":
				if outcome != nil || after.Generation != before.Generation+1 || !slices.Equal(payloads, []string{"D"}) ||
					bytes.Equal(config, oldHot.config) || f.process.VerifyReserveOutboundIfCurrent(f.ctx, after, reserve.HotSlot(1)) != nil {
					t.Fatalf("new profile did not activate in the original slot: %v probes=%v", outcome, payloads)
				}
			case "rollback":
				if !errors.Is(outcome, compat.ErrSourceProfileRolledBack) || after.Generation != before.Generation+2 ||
					!slices.Equal(payloads, []string{"D", "A"}) || !bytes.Equal(config, oldHot.config) ||
					f.process.VerifyReserveOutboundIfCurrent(f.ctx, after, reserve.HotSlot(1)) != nil {
					t.Fatalf("previous hot slot and traffic not restored: %v probes=%v", outcome, payloads)
				}
			case "different prior slot":
				if outcome == nil || before != after || !bytes.Equal(config, oldHot.config) || len(payloads) != 0 ||
					f.process.VerifyReserveOutboundIfCurrent(f.ctx, after, reserve.HotSlot(0)) != nil {
					t.Fatalf("wrong prior slot caused a mutation: %v probes=%v", outcome, payloads)
				}
			case "stopped":
				if outcome != nil || after.Running || after.Wanted || after.Generation != before.Generation || bytes.Equal(config, oldHot.config) || len(payloads) != 0 {
					t.Fatalf("stopped hot apply altered manual-stop intent: %v probes=%v", outcome, payloads)
				}
			}
			if outcome == nil {
				journal, err := reserve.OpenTransactionJournal(d.Root)
				must(err)
				defer journal.Close()
				must(journal.CheckSourceProfileCommit(f.ctx, authority))
				if err := journal.FinishSourceProfile(f.ctx, strings.Repeat("a", 64)); err == nil {
					t.Fatal("wrong catalog authority cleared the native commit")
				}
				must(journal.FinishSourceProfile(f.ctx, authority))
			}
			nowPrefs, err := os.ReadFile(reservePreferencesFile(d))
			must(err)
			if !bytes.Equal(nowPrefs, beforePrefs) || ActiveID(d) != id {
				t.Fatal("same-profile application changed the roster or selected ID")
			}
			pending, err := reserve.TransactionPending(f.ctx, d.Root)
			if err != nil || pending {
				t.Fatalf("confirmed/refused application left unexpected transaction: %v", err)
			}
		})
	}
}

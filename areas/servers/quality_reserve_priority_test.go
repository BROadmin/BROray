package servers

import (
	"context"
	"errors"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

func TestScheduledQualityYieldsToReserveBetweenServers(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		checked     int
		cancel      bool
		pending     bool
	}{
		{name: "resumes-same-scan", state: "completed", checked: 2},
		{name: "cancellation-preserves-the-protected-monitor", state: "cancelled", checked: 1, cancel: true},
		{name: "unconfirmed-reserve-transaction-stops-scan", state: "failed", checked: 1, pending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			a := e.addServer(t, "trojan://test@a.example:443#A")
			b := e.addServer(t, "trojan://test@b.example:443#B")
			writeReserveAutomatic(t, e, a, b, true)
			if err := compat.WriteJSON(autoSwitchFile(e.d), map[string]any{
				"qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 30,
			}); err != nil {
				t.Fatal(err)
			}
			guard, err := platform.TryLock(e.d.Root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			if err := e.d.Lock.ReconcileDaemonReserve(e.d.Root, guard, false, nil); err != nil {
				t.Fatal(err)
			}
			contextHash, err := e.s.qualityContext("all", "SERVER_CHECK_AUTO")
			if err != nil {
				t.Fatal(err)
			}
			rq, err := e.d.Queue.Enqueue(actionQuality, "all", "SERVER_CHECK_AUTO", contextHash)
			if err != nil {
				t.Fatal(err)
			}
			var probes []string
			monitorAt := -1
			var monitorErr error
			e.s.measure = func(_ context.Context, rec Record, _ []byte) probeResult {
				probes = append(probes, asString(rec["id"]))
				return probeResult{Success: true, Stage: "proxy-https"}
			}
			e.d.Queue.Define(compat.ActionSpec{Name: compat.ReserveSwitchAction, Type: "server_operation",
				Priority: map[string]int{"*": 1}, Stages: []compat.Stage{{
					Name: "monitor", Global: true, Protected: true, Run: func(task *compat.Task) error {
						monitorAt = len(probes)
						state, _ := e.s.readState()
						if !jsonEq(get(state, "qualityRefresh", "checkedCount"), 1) ||
							get(state, "qualityRefresh", "requestId") != rq.RequestID || !e.d.Lock.Busy() {
							monitorErr = errors.New("first result not published under the same request before protected monitor")
						}
						if tc.cancel {
							result, err := e.d.Queue.CancelOperation("op-" + rq.RequestID)
							if err != nil || result["cancelRequested"] != true || task.Ctx.Err() != nil {
								monitorErr = errors.New("quality cancellation was rejected or cancelled the protected monitor")
							}
						}
						if tc.pending {
							return compat.ErrReserveTransactionPending
						}
						return monitorErr
					},
				}}})
			ctx, cancel := context.WithCancel(e.d.Ctx)
			done := make(chan struct{})
			go func() { defer close(done); e.d.Queue.Run(ctx) }()
			defer func() { cancel(); <-done }()
			waitReq(t, e, rq.RequestID, tc.state)
			cancel()
			<-done
			if monitorAt != 1 || monitorErr != nil {
				t.Fatalf("monitor ran after %d probes, want 1, error: %v", monitorAt, monitorErr)
			}
			if len(probes) != tc.checked || (len(probes) == 2 && probes[0] == probes[1]) {
				t.Fatalf("expected %d distinct probes, got %v", tc.checked, probes)
			}
			state, _ := e.s.readState()
			if !jsonEq(get(state, "qualityRefresh", "checkedCount"), tc.checked) ||
				get(state, "qualityRefresh", "requestId") != rq.RequestID || e.d.Lock.Busy() != tc.pending {
				t.Fatalf("wrong final progress or retained fence: %v", state["qualityRefresh"])
			}
			if tc.pending {
				holder, present := e.d.Lock.Current()
				if !present || !holder.Pending || holder.Action != compat.ReserveSwitchAction {
					t.Fatal("unconfirmed reserve fence was not retained by its original owner")
				}
			}
			if tc.state == "completed" && get(state, "qualityRefresh", "status") != "success" {
				t.Fatal("resumed quality scan did not reach success")
			}
		})
	}
}

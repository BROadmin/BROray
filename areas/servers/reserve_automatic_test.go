package servers

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func reserveAutomaticEnv(t *testing.T) (*fakeEnv, string, string) {
	t.Helper()
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	b := e.addServer(t, "trojan://pw@b.example:443#B")
	e.probeOK[a], e.probeOK[b] = true, true
	if _, err := e.s.activate(a); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"schemaVersion": 3, "enabled": true,
		"failureThreshold": 1, "qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 30}); err != nil {
		t.Fatal(err)
	}
	return e, a, b
}

func writeReserveAutomatic(t *testing.T, e *fakeEnv, a, b string, enabled bool) {
	t.Helper()
	if err := compat.WriteJSON(reservePreferencesFile(e.d), reservePreferences{1, enabled, a, []string{b}, nil}); err != nil {
		t.Fatal(err)
	}
}

func runReserveQueue(t *testing.T, e *fakeEnv) {
	t.Helper()
	ctx, cancel := context.WithCancel(e.d.Ctx)
	done := make(chan struct{})
	go func() { defer close(done); e.d.Queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
}

func TestReserveAutomaticEnqueueKeepsQualityAndPreferences(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
			e, a, b := reserveAutomaticEnv(t)
			writeReserveAutomatic(t, e, a, b, enabled)
			before, err := os.ReadFile(autoSwitchFile(e.d))
			if err != nil {
				t.Fatal(err)
			}
			if err := e.s.enqueueDue(); err != nil {
				t.Fatal(err)
			}
			if _, ok := e.d.Queue.FindLive(actionHealth, a); ok == enabled {
				t.Fatalf("ordinary health queued=%v with reserve enabled=%v", ok, enabled)
			}
			if _, ok := e.d.Queue.FindLive(actionQuality, "all"); !ok {
				t.Fatal("quality refresh lost")
			}
			after, err := os.ReadFile(autoSwitchFile(e.d))
			if err != nil || string(after) != string(before) {
				t.Fatal("ordinary preference mutated")
			}
		})
	}
}

func TestReserveAutomaticQueuedStagesRefuseEnabledAndInvalidState(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		for _, stage := range []string{"health", "failover-verify", "failover-probe", "failover-activate", "replacement-verify", "replacement-probe", "replacement-activate"} {
			t.Run(stage+map[bool]string{false: "/enabled", true: "/invalid"}[invalid], func(t *testing.T) {
				e, a, b := reserveAutomaticEnv(t)
				context := e.s.runtimeContext(a)
				if context == "" {
					t.Fatal("fixture lacks active context")
				}
				steps := map[string]func(*compat.Task) error{"health": e.s.healthStep,
					"failover-verify": e.s.failoverVerify, "failover-probe": e.s.failoverProbe, "failover-activate": e.s.failoverActivate,
					"replacement-verify": e.s.replacementVerify, "replacement-probe": e.s.replacementProbe, "replacement-activate": e.s.replacementActivate}
				e.d.Queue.Define(compat.ActionSpec{Name: "test:reserve-guard", Type: "server_operation", Priority: map[string]int{"*": 0},
					Stages: []compat.Stage{{Name: stage, Global: true, Protected: true, Run: steps[stage]}}})
				r, err := e.d.Queue.Enqueue("test:reserve-guard", a, "AUTO_SWITCH", context)
				if err != nil {
					t.Fatal(err)
				}
				// The task already exists when mode ownership changes.
				writeReserveAutomatic(t, e, a, b, true)
				code := "RESERVE_MODE_ACTIVE"
				if invalid {
					if err := os.WriteFile(reservePreferencesFile(e.d), []byte(`{"enabled":false,"Enabled":true}`), 0600); err != nil {
						t.Fatal(err)
					}
					code = "RESERVE_SETTINGS_INVALID"
				}
				before, err := os.ReadFile(stateFile(e.d))
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				applies := len(e.applied)
				runReserveQueue(t, e)
				waitReq(t, e, r.RequestID, "failed")
				q, _ := e.d.Queue.Request(r.RequestID)
				if q.ErrorCode != code {
					t.Fatalf("guard code %q, want %q", q.ErrorCode, code)
				}
				after, err := os.ReadFile(stateFile(e.d))
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if string(before) != string(after) || ActiveID(e.d) != a || len(e.applied) != applies {
					t.Fatal("guard changed state/runtime")
				}
			})
		}
	}
}

func TestReserveAutomaticManualQualityRemainsAvailable(t *testing.T) {
	e, a, b := reserveAutomaticEnv(t)
	writeReserveAutomatic(t, e, a, b, true)
	action, context, apiErr := e.s.resolveCheck(a)
	if apiErr != nil || action != actionQuality || context == "" {
		t.Fatalf("manual check uses legacy health: %s %v", action, apiErr)
	}
	r, err := e.d.Queue.Enqueue(action, a, "USER", context)
	if err != nil {
		t.Fatal(err)
	}
	applies := len(e.applied)
	runReserveQueue(t, e)
	waitReq(t, e, r.RequestID, "completed")
	if len(e.applied) != applies || ActiveID(e.d) != a {
		t.Fatal("manual quality switched runtime")
	}
}

func TestReserveAutomaticDropsProbeCrossingModeChange(t *testing.T) {
	e, a, b := reserveAutomaticEnv(t)
	e.health.Store("unhealthy")
	probe := e.s.activeProbe
	errors := make(chan error, 1)
	var transition sync.Once
	e.s.activeProbe = func(ctx context.Context, path, id string) map[string]any {
		transition.Do(func() {
			errors <- compat.WriteJSON(reservePreferencesFile(e.d), reservePreferences{1, true, a, []string{b}, nil})
		})
		return probe(ctx, path, id)
	}
	before, err := os.ReadFile(stateFile(e.d))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	r, err := e.d.Queue.Enqueue(actionHealth, a, "AUTO_SWITCH", e.s.runtimeContext(a))
	if err != nil {
		t.Fatal(err)
	}
	runReserveQueue(t, e)
	waitReq(t, e, r.RequestID, "failed")
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	q, _ := e.d.Queue.Request(r.RequestID)
	if q.ErrorCode != "RESERVE_MODE_ACTIVE" {
		t.Fatalf("unexpected guard code: %s", q.ErrorCode)
	}
	after, err := os.ReadFile(stateFile(e.d))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("stale probe updated ordinary counters")
	}
	if _, ok := e.d.Queue.FindLive(actionFailover, a); ok {
		t.Fatal("stale probe queued failover")
	}
}

func TestReserveAutomaticNoWithdrawalReplacement(t *testing.T) {
	e, a, b := reserveAutomaticEnv(t)
	if _, err := e.d.Svc.WithdrawActiveServer(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeReserveAutomatic(t, e, a, b, true)
	if err := e.s.enqueueDue(); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.d.Queue.FindLive(actionReplacement, "all"); ok {
		t.Fatal("legacy replacement queued in reserve mode")
	}
	if _, err := e.s.replacementContext(); err == nil {
		t.Fatal("replacement context granted in reserve mode")
	}
}

func TestReserveAutomaticPreparedSwitchCannotTakeOwnership(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "failover", true: "withdrawal"}[replacement], func(t *testing.T) {
			e, a, b := reserveAutomaticEnv(t)
			e.health.Store("unhealthy")
			type preparation struct {
				request string
				ready   bool
				err     error
			}
			prepared := make(chan preparation, 1)
			action, verify, probe, activate := actionFailover, e.s.failoverVerify, e.s.failoverProbe, e.s.failoverActivate
			activeBefore := a
			if replacement {
				action, verify, probe, activate = actionReplacement, e.s.replacementVerify, e.s.replacementProbe, e.s.replacementActivate
				if _, err := e.d.Svc.WithdrawActiveServer(context.Background()); err != nil {
					t.Fatal(err)
				}
				activeBefore = ""
			}
			wrappedProbe := func(task *compat.Task) error {
				if err := probe(task); err != nil {
					prepared <- preparation{task.RequestID, false, err}
					return err
				}
				ready := false
				if replacement {
					f, ok := task.Get("replacement").(*replacementState)
					ready = ok && f.selected != nil
				} else {
					f, ok := task.Get("failover").(*failState)
					ready = ok && f.Status == "prepared" && len(f.NewBytes) > 0
				}
				err := compat.WriteJSON(reservePreferencesFile(e.d), reservePreferences{1, true, a, []string{b}, nil})
				prepared <- preparation{task.RequestID, ready, err}
				return err
			}
			e.d.Queue.Define(compat.ActionSpec{Name: action, Type: "server_operation", Priority: map[string]int{"*": 1}, Stages: []compat.Stage{
				{Name: "verify", Global: replacement, Run: verify}, {Name: "probe", Run: wrappedProbe},
				{Name: "activate", Global: true, Protected: true, Run: activate},
			}})
			before, err := os.ReadFile(autoSwitchFile(e.d))
			if err != nil {
				t.Fatal(err)
			}
			applies := len(e.applied)
			if err := e.s.enqueueDue(); err != nil {
				t.Fatal(err)
			}
			runReserveQueue(t, e)
			var result preparation
			select {
			case result = <-prepared:
			case <-time.After(5 * time.Second):
				t.Fatal("ordinary switch never reached preparation")
			}
			if result.err != nil || !result.ready {
				t.Fatalf("fixture did not prepare actual switch: %+v", result)
			}
			waitReq(t, e, result.request, "failed")
			q, _ := e.d.Queue.Request(result.request)
			if q.ErrorCode != "RESERVE_MODE_ACTIVE" {
				t.Fatalf("wrong final gate: %s", q.ErrorCode)
			}
			if len(e.applied) != applies || ActiveID(e.d) != activeBefore {
				t.Fatal("prepared ordinary switch took ownership")
			}
			after, err := os.ReadFile(autoSwitchFile(e.d))
			if err != nil || string(before) != string(after) {
				t.Fatal("ordinary settings changed")
			}
		})
	}
}

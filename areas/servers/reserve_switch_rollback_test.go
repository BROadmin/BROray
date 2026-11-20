package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

func TestReserveSwitchLiveRollbackRestoresVerifiedPrevious(t *testing.T) {
	for _, exited := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "previous-exited"}[exited], func(t *testing.T) {
			f, m, result := reserveSwitchFixture(t)
			before := f.process.RuntimeSnapshot()
			if exited {
				reserveTerminateOwnedFixture(t, f)
			}
			verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
			f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, description string) (compat.OwnedInterfaceTarget, error) {
				if ActiveID(f.e.d) == result.Intent.To {
					return compat.OwnedInterfaceTarget{}, errors.New("new target cannot be proven")
				}
				return verify(ctx, description)
			}
			err := f.e.s.switchReserve(f.ctx, m, result)
			if err == nil || errors.Is(err, errReserveTransactionPending) {
				t.Fatal("verified rollback missing", err)
			}
			after := f.process.RuntimeSnapshot()
			if !after.Running || !after.Wanted || after.Generation != before.Generation+2 || after.Intent != before.Intent+2 {
				t.Fatal("rollback did not restore an owned live generation", before, after)
			}
			cfg, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil || !bytes.Equal(cfg, f.plan.config) || ActiveID(f.e.d) != f.plan.node.ID {
				t.Fatal("previous config/selection not restored", err)
			}
			if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
				t.Fatal("confirmed rollback retained journal", err)
			}
			if m.state.Pending != nil || m.state.Reason != "switch-failed-confirmed" || m.state.ActiveID != f.plan.node.ID || m.state.ActiveIdentity == f.binding.identity {
				t.Fatal("monitor did not acknowledge failed switch with fresh identity", m.state.Reason)
			}
			if len(m.state.Samples) != 0 || m.state.ActiveFailures != 0 || m.lastFailure != nil || m.binding == nil || m.binding.snapshot != after {
				t.Fatal("stale evidence survived rollback")
			}
			if _, err := f.e.s.verifyReserveCommit(f.ctx, *m.binding, "BROray - mix-auto"); err != nil {
				t.Fatal("restored composite runtime proof failed", err)
			}
		})
	}
}

func TestReserveSwitchLiveRollbackPreservesChangedAuthority(t *testing.T) {
	for _, kind := range []string{"manual-stop", "receipt", "config", "committed-journal"} {
		t.Run(kind, func(t *testing.T) {
			f, m, result := reserveSwitchFixture(t)
			before := f.process.RuntimeSnapshot()
			verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
			injected := false
			var path string
			var protected []byte
			f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, description string) (compat.OwnedInterfaceTarget, error) {
				if !injected && ActiveID(f.e.d) == result.Intent.To {
					injected = true
					switch kind {
					case "manual-stop":
						if err := f.process.Stop(f.ctx); err != nil {
							return compat.OwnedInterfaceTarget{}, err
						}
					case "receipt":
						path = f.e.d.Path("config", "interface.json")
						protected = []byte(`{"foreign":true}`)
					case "config":
						path = f.e.d.Root.XrayConfig()
						protected = []byte(`{"foreign":true}`)
					case "committed-journal":
						path = filepath.Join(f.e.d.Root.Config(), reserve.TransactionJournalFile)
						raw, err := os.ReadFile(path)
						if err != nil {
							return compat.OwnedInterfaceTarget{}, err
						}
						var record reserve.TransactionRecord
						if err := json.Unmarshal(raw, &record); err != nil {
							return compat.OwnedInterfaceTarget{}, err
						}
						record.Phase = "committed"
						protected, err = json.Marshal(record)
						if err != nil {
							return compat.OwnedInterfaceTarget{}, err
						}
					}
					if path != "" {
						if err := os.WriteFile(path, protected, 0600); err != nil {
							return compat.OwnedInterfaceTarget{}, err
						}
					}
				}
				return verify(ctx, description)
			}
			err := f.e.s.switchReserve(f.ctx, m, result)
			if !injected || !errors.Is(err, errReserveTransactionPending) {
				t.Fatal("unsafe rollback accepted", injected, err)
			}
			after := f.process.RuntimeSnapshot()
			if after.Generation != before.Generation+1 {
				t.Fatal("foreign/manual authority restarted", before, after)
			}
			if kind == "manual-stop" && (after.Running || after.Wanted) {
				t.Fatal("manual stop resurrected")
			}
			if path != "" {
				raw, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, protected) {
					t.Fatal("foreign/committed evidence overwritten", err)
				}
			}
			if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || !pending {
				t.Fatal("recovery ownership discarded", err)
			}
			if m.state.Pending == nil {
				t.Fatal("unproven rollback acknowledged")
			}
		})
	}
}

func TestReserveSwitchLiveRollbackQueueRetainsConfirmedState(t *testing.T) {
	f, m, _ := reserveSwitchFixture(t)
	m.state.Pending = nil
	m.state.ActiveFailures = 2
	m.scheduler = reserve.ProbeScheduler{}
	prior := m.state.Samples[m.state.ActiveID]
	prior.StartedAt = time.Now().Add(-15 * time.Second)
	prior.FinishedAt = prior.StartedAt.Add(time.Second)
	m.state.Samples[m.state.ActiveID] = prior
	lastSwitch := m.state.LastSwitchAt
	f.e.s.reserveMon = m
	reserveQueueAuthority(t, f)
	verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, description string) (compat.OwnedInterfaceTarget, error) {
		if ActiveID(f.e.d) == f.backup {
			return compat.OwnedInterfaceTarget{}, errors.New("new target proof failed")
		}
		return verify(ctx, description)
	}
	prefs, err := readReservePreferences(f.e.d)
	if err != nil {
		t.Fatal(err)
	}
	req, err := f.e.d.Queue.Enqueue(compat.ReserveSwitchAction, compat.ReserveOperationTarget, "RESERVE_MONITOR", prefs.Revision)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.e.d.Ctx)
	done := make(chan struct{})
	go func() { defer close(done); f.e.d.Queue.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(40 * time.Second)
	for {
		row, ok := f.e.d.Queue.Request(req.RequestID)
		if ok && row.State != "queued" && row.State != "running" {
			if row.State != "failed" || row.ErrorCode != "RESERVE_SWITCH_ROLLED_BACK" {
				t.Fatal("queue did not report verified rollback", row.State, row.ErrorCode, row.Message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rollback request did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if f.e.d.Lock.Busy() || ActiveID(f.e.d) != f.plan.node.ID {
		t.Fatal("confirmed rollback retained fence or wrong selection")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Reason != "switch-failed-confirmed" || m.state.Pending != nil || m.binding == nil || m.binding.plan.node.ID != f.plan.node.ID || !m.state.LastSwitchAt.Equal(lastSwitch) {
		t.Fatal("queue reset verified rollback/cooldown state", m.state.Reason)
	}
	if m.retryRevision != prefs.Revision || !m.retryAfter.After(time.Now()) {
		t.Fatal("rollback retry backoff missing")
	}
}

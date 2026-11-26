package servers

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type reserveEnableRollback struct {
	record               reserve.TransactionRecord
	process              *xray.Process
	snapshot             xray.RuntimeSnapshot
	identity             reserve.ProcessIdentity
	plans                map[string]reserveRuntimePlan
	previousRecord       []byte
	authorized, replaced bool
}

// Restore the ordinary runtime captured before enabling. That runtime need not
// belong to the roster or expose a diagnostic endpoint. A successful guarded
// replacement is the sole authority for replacing the new child on failure.
func (s *svc) rollbackReserveEnable(ctx context.Context, j *reserve.TransactionJournal, state reserveEnableRollback) error {
	r := state.record
	if !state.authorized || state.process == nil || r.ID == "" || r.Phase != "prepared" ||
		(r.Intent.Action != "enable" && r.Intent.Action != "exit-switch") ||
		!r.Previous.Config.Exists || !r.Next.Config.Exists || !r.Previous.Active.Exists || !r.Next.Active.Exists ||
		!r.Previous.Preferences.Exists || !r.Next.Preferences.Exists || len(state.plans) < 2 {
		return errReserveRuntime
	}
	rollbackTimeout := 15 * time.Second
	if r.Intent.Action == "enable" && r.Intent.PreviousDescription == r.Intent.NextDescription {
		if _, diagnostic := reserve.DiagnosticEndpoint(r.Previous.Config.Data); !diagnostic {
			// Recheck router ownership before replacement and wait for the
			// original proxy to converge without exhausting the rollback.
			rollbackTimeout = 24 * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(ctx, rollbackTimeout)
	defer cancel()
	oldID := strings.TrimSpace(string(r.Previous.Active.Data))
	if !ValidID(oldID) {
		return errReserveRuntime
	}
	same := func(path string, want []byte, limit int64) bool {
		got, e := reserveRuntimeFile(path, limit)
		return e == nil && bytes.Equal(got, want)
	}
	image := func(path string, a, b []byte, limit int64) bool {
		got, e := reserveRuntimeFile(path, limit)
		return e == nil && (bytes.Equal(got, a) || bytes.Equal(got, b))
	}
	// No Process methods: also used while ReplaceIfCurrent holds Process.mu.
	inputs := func() bool {
		if ctx.Err() != nil || !s.reserveRuntimeFence(ctx) || s.d.App == nil || s.d.App.Xray != state.process || !s.reserveCoreMatches(state.identity) ||
			!same(RecordPath(s.d, oldID), state.previousRecord, 1<<20) ||
			!image(s.d.Root.XrayConfig(), r.Previous.Config.Data, r.Next.Config.Data, 2<<20) ||
			!image(ActiveFile(s.d), r.Previous.Active.Data, r.Next.Active.Data, 4096) {
			return false
		}
		prefs, e := reserveRuntimeFile(reservePreferencesFile(s.d), 4096)
		if e != nil || (!bytes.Equal(prefs, r.Previous.Preferences.Data) && !bytes.Equal(prefs, r.Next.Preferences.Data)) {
			return false
		}
		revision := reservePreferencesRevision(prefs, true)
		roster, e := decodeReservePreferences(prefs)
		if e != nil {
			return false
		}
		for _, p := range state.plans {
			p.revision = revision
			p.outsideRoster = !reserveRosterContains(reservePreferencesSnapshot{reservePreferences: roster}, p.node.ID)
			if !s.reservePlanInputs(ctx, p, false, false) {
				return false
			}
		}
		return true
	}
	recordCurrent := func() bool { got, e := j.Read(ctx); return e == nil && reflect.DeepEqual(got, r) }
	childCurrent := func() bool {
		if !inputs() {
			return false
		}
		now := state.process.RuntimeSnapshot()
		return now == state.snapshot || (state.replaced && now.ConfirmsUnexpectedExit(state.snapshot))
	}
	if !childCurrent() || !recordCurrent() {
		return errReserveRuntime
	}
	receiptPath := s.d.Path("config", "interface.json")
	receipt, err := reserveRuntimeFile(receiptPath, 64<<10)
	if err != nil {
		return err
	}
	// Either known description may be present after an interrupted metadata write.
	// Both must be backed by the existing provider's complete ownership proof.
	description := r.Intent.NextDescription
	target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
	if err != nil {
		description = r.Intent.PreviousDescription
		target, err = s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
	}
	if err != nil || target.Name != r.Intent.InterfaceName || target.Upstream.String() != r.Intent.Upstream || !childCurrent() || !recordCurrent() || !same(receiptPath, receipt, 64<<10) {
		return errReserveRuntime
	}
	again, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
	if err != nil || again != target || !childCurrent() || !same(receiptPath, receipt, 64<<10) {
		return errReserveRuntime
	}
	if state.replaced {
		if err := s.d.Svc.ValidateXrayConfig(ctx, r.Previous.Config.Data); err != nil {
			return err
		}
		if !childCurrent() || !recordCurrent() || !same(receiptPath, receipt, 64<<10) {
			return errReserveRuntime
		}
		state.snapshot, err = state.process.ReplaceIfCurrent(ctx, state.snapshot, func() error {
			if !inputs() || !recordCurrent() || !same(receiptPath, receipt, 64<<10) {
				return errReserveRuntime
			}
			return j.RestoreConfigAfterMetadataProof(ctx, r, receipt)
		})
		if err != nil {
			return err
		}
	} else if !same(s.d.Root.XrayConfig(), r.Previous.Config.Data, 2<<20) {
		return errReserveRuntime
	}
	descriptionUnchanged := r.Intent.PreviousDescription == r.Intent.NextDescription &&
		r.Receipt.Exists && bytes.Equal(receipt, r.Receipt.Data)
	return j.RollbackPrepared(ctx, r, func(ctx context.Context, actual reserve.TransactionRecord) error {
		if !reflect.DeepEqual(actual, r) || !inputs() || !same(receiptPath, receipt, 64<<10) {
			return errReserveRuntime
		}
		// An already proven original description needs no metadata write.
		// The live target and its receipt are still checked below.
		if !descriptionUnchanged {
			if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
				return err
			}
		}
		restoredReceipt, e := reserveRuntimeFile(receiptPath, 64<<10)
		if e != nil {
			return e
		}
		previous := func() bool {
			return inputs() && state.process.RuntimeSnapshot() == state.snapshot &&
				same(s.d.Root.XrayConfig(), r.Previous.Config.Data, 2<<20) && same(ActiveFile(s.d), r.Previous.Active.Data, 4096) &&
				same(reservePreferencesFile(s.d), r.Previous.Preferences.Data, 4096) && same(receiptPath, restoredReceipt, 64<<10)
		}
		deadline := time.Now().Add(8 * time.Second)
		for {
			if !previous() {
				return errReserveRuntime
			}
			identity, e := reserve.CaptureProcess(state.snapshot.PID, s.d.Root.XrayBinary())
			if e == nil && (state.replaced || identity == state.identity) {
				listener, e := reserve.VerifyListener(identity, target.Upstream)
				if e == nil {
					owned, e := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, r.Intent.PreviousDescription)
					if e == nil && owned.Name == target.Name && owned.Upstream == target.Upstream && previous() {
						last, e := reserve.VerifyListener(identity, target.Upstream)
						if e == nil && last == listener && previous() {
							if plan := state.plans[oldID]; plan.hotSlot != "" {
								binding, err := s.bindReserveRuntime(ctx, plan, nil)
								if err != nil || binding.snapshot != state.snapshot || !s.confirmReserveHotTraffic(ctx, binding) || !previous() {
									return errReserveRuntime
								}
							}
							return nil
						}
					}
				}
			}
			if !time.Now().Before(deadline) {
				return errReserveRuntime
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

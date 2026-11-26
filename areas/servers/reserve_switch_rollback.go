package servers

import (
	"context"
	"reflect"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

// A successful replacement supplies the only authority to stop the new child.
// Never infer that authority from a later observed PID, manual stop or restart.
// Failed installations and committed/foreign journals retain cold recovery.
func (s *svc) rollbackReserveSwitch(ctx context.Context, j *reserve.TransactionJournal, record reserve.TransactionRecord, previous reserveActiveBinding, replacement xray.RuntimeSnapshot, target compat.OwnedInterfaceTarget, hot bool, inputs func() bool) (reserveActiveBinding, error) {
	var restored reserveActiveBinding
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	validInputs := func() bool {
		return inputs != nil && inputs() && previous.process != nil && s.d.App != nil && s.d.App.Xray == previous.process &&
			s.reserveCoreMatches(previous.target.Process) && record.ID != "" && record.Phase == "prepared" && record.Intent.Action == "switch" &&
			record.Previous.Config.SHA256 == previous.plan.configSHA && record.Intent.InterfaceName == target.Name && record.Intent.Upstream == target.Upstream.String() &&
			record.Intent.PreviousDescription == "BROray - mix-auto" && record.Intent.NextDescription == "BROray - mix-auto"
	}
	currentRecord := func() bool { got, err := j.Read(ctx); return err == nil && reflect.DeepEqual(got, record) }
	currentChild := func() bool {
		if !validInputs() {
			return false
		}
		now := previous.process.RuntimeSnapshot()
		return now == replacement || (!hot && now.ConfirmsUnexpectedExit(replacement))
	}
	if !currentChild() || !currentRecord() {
		return restored, errReserveRuntime
	}
	owned, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, "BROray - mix-auto")
	if err != nil || owned != target || !currentChild() || !currentRecord() {
		return restored, errReserveRuntime
	}
	snapshot := replacement
	if hot {
		if previous.plan.hotSlot == "" || record.Next.Config.SHA256 != record.Previous.Config.SHA256 {
			return restored, errReserveRuntime
		}
	} else {
		if err := s.d.Svc.ValidateXrayConfig(ctx, previous.plan.config); err != nil {
			return restored, err
		}
		if !currentChild() || !currentRecord() {
			return restored, errReserveRuntime
		}
		snapshot, err = previous.process.ReplaceIfCurrent(ctx, replacement, func() error {
			// This callback runs under Process.mu: only bounded local input/owner checks.
			if !validInputs() || !currentRecord() {
				return errReserveRuntime
			}
			return j.RestoreConfig(ctx, record.ID)
		})
		if err != nil {
			return restored, err
		}
	}
	err = j.RollbackPrepared(ctx, record, func(ctx context.Context, r reserve.TransactionRecord) error {
		if !reflect.DeepEqual(r, record) || !validInputs() {
			return errReserveRuntime
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if !validInputs() || previous.process.RuntimeSnapshot() != snapshot {
				return errReserveRuntime
			}
			restored, err = s.bindReserveRuntime(ctx, previous.plan, nil)
			if err == nil && restored.snapshot == snapshot {
				break
			}
			if !time.Now().Before(deadline) {
				return errReserveRuntime
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
		if err := s.verifyReserveSwitchCommit(ctx, previous, restored, target); err != nil {
			return err
		}
		if !s.confirmReserveHotTraffic(ctx, restored) || !validInputs() || previous.process.RuntimeSnapshot() != snapshot {
			return errReserveRuntime
		}
		return nil
	})
	return restored, err
}

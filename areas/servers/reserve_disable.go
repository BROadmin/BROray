package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

// The caller must serialize the monitor and retain the daemon-owned reserve
// fence on errReserveTransactionPending. This executor never restarts Xray.
func (s *svc) disableReserve(ctx context.Context, binding reserveActiveBinding, revision string) (retErr error) {
	if !s.reserveRuntimeFence(ctx) || revision != binding.plan.revision {
		return errReserveRuntime
	}
	if binding.plan.hotSlot != "" {
		return s.applyReserveSelection(ctx, revision, reserveSelection{server: binding.plan.node.ID, mode: "disable-hot"})
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || !prefs.Enabled || !prefs.Configured || prefs.Revision != revision {
		return errReserveRuntime
	}
	before, err := s.verifyReserveCommit(ctx, binding, "BROray - mix-auto")
	if err != nil {
		return err
	}
	description, err := s.d.Svc.OrdinaryInterfaceDescription()
	if err != nil || description == "" {
		return errors.Join(errReserveRuntime, err)
	}
	prefBytes, err := readReservePreferencesBytes(reservePreferencesFile(s.d))
	if err != nil || reservePreferencesRevision(prefBytes, true) != revision {
		return errReserveRuntime
	}
	active, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
	if err != nil {
		return err
	}
	receipt, err := reserveRuntimeFile(s.d.Path("config", "interface.json"), 64<<10)
	if err != nil {
		return err
	}
	next := prefs.reservePreferences
	next.Enabled = false
	next.Emergency = nil
	nextBytes, err := json.Marshal(next)
	if err != nil {
		return err
	}
	unchanged := func() bool {
		proof, err := s.verifyReserveCommit(ctx, binding, "BROray - mix-auto")
		return err == nil && proof == before
	}
	if !unchanged() {
		return errReserveRuntime
	}
	j, err := reserve.OpenTransactionJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	id, err := j.Begin(ctx, reserve.TransactionFiles{Config: binding.plan.config, Preferences: nextBytes, Active: active}, reserve.TransactionIntent{
		Action: "disable", Authority: compat.SHA256Hex([]byte(binding.identity + revision)),
		InterfaceName: before.Interface.Name, Upstream: before.Interface.Upstream.String(),
		PreviousDescription: "BROray - mix-auto", PreviousDescriptionLine: before.Interface.DescriptionLine,
		NextDescription: description, ReceiptSHA256: compat.SHA256Hex(receipt),
	})
	var rollbackRecord reserve.TransactionRecord
	if id != "" {
		defer func() {
			if retErr == nil {
				return
			}
			if rollbackRecord.ID != "" {
				rollbackErr := s.rollbackReserveMetadata(ctx, j, rollbackRecord, binding.plan, func() bool {
					return s.d.App != nil && s.d.App.Xray == binding.process && binding.process.RuntimeSnapshot() == binding.snapshot && s.reserveCoreMatches(binding.target.Process)
				}, func(ctx context.Context) error {
					proof, err := s.verifyReserveCommit(ctx, binding, "BROray - mix-auto")
					if err != nil || proof.Interface.Name != before.Interface.Name || proof.Interface.Upstream != before.Interface.Upstream || proof.Listener != before.Listener {
						return errReserveRuntime
					}
					return nil
				}, nil)
				if rollbackErr == nil {
					retErr = errors.Join(errReserveModeRolledBack, retErr)
					return
				}
				retErr = errors.Join(retErr, errReserveRollbackUnconfirmed, rollbackErr)
			}
			retErr = errors.Join(errReserveTransactionPending, retErr)
		}()
	}
	if err != nil {
		return err
	}
	r, err := j.Read(ctx)
	if err != nil || r.Previous.Config.SHA256 != binding.plan.configSHA ||
		!bytes.Equal(r.Previous.Preferences.Data, prefBytes) || !bytes.Equal(r.Previous.Active.Data, active) || !unchanged() {
		return errReserveRuntime
	}
	rollbackRecord = r
	if err := j.InstallSelection(ctx, id); err != nil {
		return err
	}
	// The only changed runtime-plan input is the exact preferences image.
	// All process, socket, config, record and system-settings proofs stay bound.
	after := binding
	after.plan.revision = reservePreferencesRevision(nextBytes, true)
	if !s.reservePlanInputs(ctx, after.plan, false, true) || binding.process.RuntimeSnapshot() != binding.snapshot {
		return errReserveRuntime
	}
	if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
		return err
	}
	return j.Confirm(ctx, id, func(ctx context.Context, r reserve.TransactionRecord) error {
		if r.ID != id || r.Intent.Action != "disable" || !bytes.Equal(r.Previous.Config.Data, r.Next.Config.Data) ||
			!bytes.Equal(r.Previous.Active.Data, r.Next.Active.Data) {
			return errReserveRuntime
		}
		proof, err := s.verifyReserveCommit(ctx, after, description)
		if err != nil || proof.Interface.Name != before.Interface.Name || proof.Interface.Upstream != before.Interface.Upstream || proof.Listener != before.Listener {
			return errReserveRuntime
		}
		return nil
	})
}

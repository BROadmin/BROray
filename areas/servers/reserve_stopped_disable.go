package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

// A confirmed exited child needs no live listener to turn reserve mode off.
// The stopped path also removes an installed hot selector from the config.
// It preserves stopped intent and confirms only files and interface metadata.
func (s *svc) disableReserveStopped(ctx context.Context, plan reserveRuntimePlan, revision string) (retErr error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !s.reserveRuntimeFence(ctx) || s.d.App == nil || s.d.App.Xray == nil || plan.revision != revision {
		return errReserveRuntime
	}
	p := s.d.App.Xray
	snapshot := p.RuntimeSnapshot()
	hot := plan.hotSlot != ""
	if hot && snapshot.Wanted {
		return errReserveRuntime
	}
	var cold *reserve.StoppedProfileLease
	if snapshot.ColdStopped() {
		var err error
		var release func() error
		cold, release, err = s.acquireColdSourceLease(ctx, p, snapshot)
		if err != nil {
			return errors.Join(errReserveRuntime, err)
		}
		defer release()
	} else {
		if snapshot.Running || !snapshot.ExitConfirmed || snapshot.PID != 0 || snapshot.LastPID <= 0 ||
			snapshot.Generation == 0 || snapshot.Generation == ^uint64(0) || snapshot.Intent == 0 || snapshot.Intent == ^uint64(0) {
			return errReserveRuntime
		}
	}
	coldValid := func() bool { return cold == nil || cold.Check(ctx, s.d.Root) == nil }
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || !prefs.Enabled || !prefs.Configured || prefs.Revision != revision {
		return errReserveRuntime
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
	owner := func() bool { return coldValid() && s.d.App != nil && s.d.App.Xray == p }
	runtimeCurrent := func() bool { return owner() && p.RuntimeSnapshot() == snapshot }
	current := func(plan reserveRuntimePlan, enabled bool) bool {
		return runtimeCurrent() && s.reservePlanInputs(ctx, plan, enabled, true)
	}
	same := func(path string, expected []byte, limit int64) bool {
		actual, err := reserveRuntimeFile(path, limit)
		return err == nil && bytes.Equal(actual, expected)
	}
	beforeFiles := func() bool {
		return owner() && s.reservePlanInputs(ctx, plan, true, true) && same(reservePreferencesFile(s.d), prefBytes, 64<<10) &&
			same(ActiveFile(s.d), active, 4096) && same(s.d.Path("config", "interface.json"), receipt, 64<<10)
	}
	unchanged := func() bool { return runtimeCurrent() && beforeFiles() }
	installStopped := func(ctx context.Context, install func() error) error {
		if cold != nil {
			return p.InstallIfCold(ctx, snapshot, cold, install)
		}
		next, err := p.StopIfCurrent(ctx, snapshot, install)
		if err == nil {
			snapshot = next
		}
		return err
	}
	if !unchanged() {
		return errReserveRuntime
	}
	before, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, "BROray - mix-auto")
	if err != nil || before.Name == "" || !reservePrimaryMatches(plan.config, before.Upstream) || !unchanged() {
		return errors.Join(errReserveRuntime, err)
	}
	description, err := s.d.Svc.OrdinaryInterfaceDescription()
	if err != nil || description == "" {
		return errors.Join(errReserveRuntime, err)
	}
	action, after := "disable", plan
	if hot {
		action = "exit-switch"
		record, err := ReadRecord(s.d, plan.node.ID)
		if err != nil || description == "BROray - mix-auto" {
			return errors.Join(errReserveRuntime, err)
		}
		after, err = s.ordinaryReserveRuntime(ctx, plan, record)
		if err != nil || !reservePrimaryMatches(after.config, before.Upstream) || bytes.Equal(after.config, plan.config) {
			return errors.Join(errReserveRuntime, err)
		}
		if err := s.d.Svc.ValidateXrayConfig(ctx, after.config); err != nil {
			return err
		}
	}
	target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, "BROray - mix-auto")
	if err != nil || target != before || !unchanged() {
		return errors.Join(errReserveRuntime, err)
	}
	next := prefs.reservePreferences
	next.Enabled = false
	next.Emergency = nil
	nextBytes, err := json.Marshal(next)
	if err != nil {
		return err
	}
	after.revision = reservePreferencesRevision(nextBytes, true)
	j, err := reserve.OpenTransactionJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	id, err := j.Begin(ctx, reserve.TransactionFiles{Config: after.config, Preferences: nextBytes, Active: active}, reserve.TransactionIntent{
		Action: action, Authority: compat.SHA256Hex([]byte(revision + plan.configSHA + plan.recordSHA)),
		InterfaceName: before.Name, Upstream: before.Upstream.String(), PreviousDescription: "BROray - mix-auto",
		PreviousDescriptionLine: before.DescriptionLine, NextDescription: description, ReceiptSHA256: compat.SHA256Hex(receipt),
	})
	var rollbackRecord reserve.TransactionRecord
	var restoreConfig func(context.Context, []byte) error
	if id != "" {
		defer func() {
			if retErr == nil {
				return
			}
			if rollbackRecord.ID != "" {
				rollbackErr := s.rollbackReserveMetadata(ctx, j, rollbackRecord, plan, runtimeCurrent, func(ctx context.Context) error {
					target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, "BROray - mix-auto")
					if err != nil || target.Name != before.Name || target.Upstream != before.Upstream {
						return errReserveRuntime
					}
					return nil
				}, restoreConfig)
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
	if err != nil || !bytes.Equal(r.Previous.Config.Data, plan.config) || !bytes.Equal(r.Previous.Preferences.Data, prefBytes) ||
		!bytes.Equal(r.Previous.Active.Data, active) || !unchanged() {
		return errors.Join(errReserveRuntime, err)
	}
	if hot {
		if err := installStopped(ctx, func() error {
			if !beforeFiles() {
				return errReserveRuntime
			}
			return j.InstallConfig(ctx, id)
		}); err != nil {
			return err
		}
		restoreConfig = func(ctx context.Context, acceptedReceipt []byte) error {
			return installStopped(ctx, func() error {
				if !owner() || (!s.reservePlanInputs(ctx, plan, false, false) &&
					!s.reservePlanInputs(ctx, after, false, false)) {
					return errReserveRuntime
				}
				return j.RestoreConfigAfterMetadataProof(ctx, r, acceptedReceipt)
			})
		}
	}
	rollbackRecord = r
	if err := j.InstallSelection(ctx, id); err != nil {
		return err
	}
	if !current(after, false) {
		return errReserveRuntime
	}
	if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
		return err
	}
	return j.Confirm(ctx, id, func(ctx context.Context, r reserve.TransactionRecord) error {
		if r.ID != id || r.Intent.Action != action || r.Next.Config.SHA256 != after.configSHA ||
			!bytes.Equal(r.Previous.Active.Data, r.Next.Active.Data) || !current(after, false) {
			return errReserveRuntime
		}
		target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
		if err != nil || target.Name != before.Name || target.Upstream != before.Upstream || !current(after, false) {
			return errors.Join(errReserveRuntime, err)
		}
		return nil
	})
}

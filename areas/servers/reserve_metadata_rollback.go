package servers

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/BROadmin/broray/modules/reserve"
)

var errReserveModeRolledBack = errors.New("RESERVE_MODE_ROLLED_BACK")

// Roll back only a disable transaction whose config, selection and supervisor
// observation are unchanged. Runtime callbacks are read-only proofs; metadata
// restoration uses the existing description-only service, never interface repair.
func (s *svc) rollbackReserveMetadata(ctx context.Context, j *reserve.TransactionJournal, record reserve.TransactionRecord, plan reserveRuntimePlan, currentRuntime func() bool, verifyPrevious func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if record.ID == "" || record.Phase != "prepared" || record.Intent.Action != "disable" || currentRuntime == nil || verifyPrevious == nil ||
		!record.Previous.Config.Exists || !record.Next.Config.Exists || !record.Previous.Active.Exists || !record.Next.Active.Exists ||
		!record.Previous.Preferences.Exists || !record.Next.Preferences.Exists || record.Previous.Config.SHA256 != plan.configSHA ||
		!bytes.Equal(record.Previous.Config.Data, record.Next.Config.Data) || !bytes.Equal(record.Previous.Active.Data, record.Next.Active.Data) ||
		record.Intent.PreviousDescription != "BROray - mix-auto" {
		return errReserveRuntime
	}
	same := func(path string, want []byte, limit int64) bool {
		got, err := reserveRuntimeFile(path, limit)
		return err == nil && bytes.Equal(got, want)
	}
	fixed := func() bool {
		if !currentRuntime() || !same(s.d.Root.XrayConfig(), record.Previous.Config.Data, 2<<20) || !same(ActiveFile(s.d), record.Previous.Active.Data, 4096) {
			return false
		}
		prefs, err := reserveRuntimeFile(reservePreferencesFile(s.d), 4096)
		if err != nil || (!bytes.Equal(prefs, record.Previous.Preferences.Data) && !bytes.Equal(prefs, record.Next.Preferences.Data)) {
			return false
		}
		observed := plan
		observed.revision = reservePreferencesRevision(prefs, true)
		return s.reservePlanInputs(ctx, observed, false, true)
	}
	sameRecord := func() bool { got, err := j.Read(ctx); return err == nil && reflect.DeepEqual(got, record) }
	if !fixed() || !sameRecord() {
		return errReserveRuntime
	}
	receiptPath := s.d.Path("config", "interface.json")
	accepted, err := reserveRuntimeFile(receiptPath, 64<<10)
	if err != nil {
		return err
	}
	if !bytes.Equal(accepted, record.Receipt.Data) {
		// Description sync legitimately canonicalizes its owned receipt. Accept that
		// transition only after repeated provider proof and exact receipt readback.
		prefs, err := reserveRuntimeFile(reservePreferencesFile(s.d), 4096)
		if err != nil {
			return err
		}
		description := record.Intent.NextDescription
		if bytes.Equal(prefs, record.Previous.Preferences.Data) {
			description = record.Intent.PreviousDescription
		}
		target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
		if err != nil || target.Name != record.Intent.InterfaceName || target.Upstream.String() != record.Intent.Upstream || !fixed() || !sameRecord() || !same(receiptPath, accepted, 64<<10) {
			return errReserveRuntime
		}
		final, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
		if err != nil || final != target || !fixed() || !same(receiptPath, accepted, 64<<10) {
			return errReserveRuntime
		}
	}
	if !fixed() || !sameRecord() || !same(receiptPath, accepted, 64<<10) {
		return errReserveRuntime
	}
	return j.RollbackPrepared(ctx, record, func(ctx context.Context, r reserve.TransactionRecord) error {
		if !reflect.DeepEqual(r, record) || !fixed() || !same(receiptPath, accepted, 64<<10) {
			return errReserveRuntime
		}
		if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
			return err
		}
		restoredReceipt, err := reserveRuntimeFile(receiptPath, 64<<10)
		if err != nil || !fixed() {
			return errReserveRuntime
		}
		if err := verifyPrevious(ctx); err != nil {
			return err
		}
		if !fixed() || !same(receiptPath, restoredReceipt, 64<<10) {
			return errReserveRuntime
		}
		return nil
	})
}

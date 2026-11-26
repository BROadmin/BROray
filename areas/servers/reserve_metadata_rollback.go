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
var errReserveRollbackUnconfirmed = errors.New("RESERVE_ROLLBACK_UNCONFIRMED")
var errReserveRollbackVPNVerified = errors.New("RESERVE_ROLLBACK_VPN_VERIFIED")

// Roll back a disable with a fixed selection and an exact supervisor owner.
// A stopped hot exit supplies its guarded config installer. Metadata restoration
// uses only the description service and the proven ownership of the proxy.
func (s *svc) rollbackReserveMetadata(ctx context.Context, j *reserve.TransactionJournal, record reserve.TransactionRecord, plan reserveRuntimePlan, currentRuntime func() bool, verifyPrevious func(context.Context) error, restoreConfig func(context.Context, []byte) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	hot := restoreConfig != nil
	unchangedConfig := bytes.Equal(record.Previous.Config.Data, record.Next.Config.Data)
	if record.ID == "" || record.Phase != "prepared" || currentRuntime == nil || verifyPrevious == nil ||
		!record.Previous.Config.Exists || !record.Next.Config.Exists || !record.Previous.Active.Exists || !record.Next.Active.Exists ||
		!record.Previous.Preferences.Exists || !record.Next.Preferences.Exists || record.Previous.Config.SHA256 != plan.configSHA ||
		!bytes.Equal(record.Previous.Active.Data, record.Next.Active.Data) ||
		record.Intent.PreviousDescription != "BROray - mix-auto" {
		return errReserveRuntime
	}
	if (hot && (record.Intent.Action != "exit-switch" || plan.hotSlot == "" || unchangedConfig)) ||
		(!hot && (record.Intent.Action != "disable" || !unchangedConfig)) {
		return errReserveRuntime
	}
	same := func(path string, want []byte, limit int64) bool {
		got, err := reserveRuntimeFile(path, limit)
		return err == nil && bytes.Equal(got, want)
	}
	fixed := func() bool {
		if !currentRuntime() || !same(ActiveFile(s.d), record.Previous.Active.Data, 4096) {
			return false
		}
		observed := plan
		config, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
		if err != nil || !bytes.Equal(config, record.Previous.Config.Data) {
			if err != nil || !hot || !bytes.Equal(config, record.Next.Config.Data) {
				return false
			}
			observed.configSHA = record.Next.Config.SHA256
		}
		prefs, err := reserveRuntimeFile(reservePreferencesFile(s.d), 4096)
		if err != nil || (!bytes.Equal(prefs, record.Previous.Preferences.Data) && !bytes.Equal(prefs, record.Next.Preferences.Data)) {
			return false
		}
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
	if hot || !bytes.Equal(accepted, record.Receipt.Data) {
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
		if err != nil && hot {
			// A failed description sync can leave the previous owned label
			// even after the preference image has changed.
			description = record.Intent.PreviousDescription
			target, err = s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
		}
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
	if hot {
		if err := restoreConfig(ctx, accepted); err != nil {
			return err
		}
	}
	original := func() bool {
		return fixed() && (!hot || same(s.d.Root.XrayConfig(), record.Previous.Config.Data, 2<<20))
	}
	return j.RollbackPrepared(ctx, record, func(ctx context.Context, r reserve.TransactionRecord) error {
		if !reflect.DeepEqual(r, record) || !original() || !same(receiptPath, accepted, 64<<10) {
			return errReserveRuntime
		}
		if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
			return err
		}
		restoredReceipt, err := reserveRuntimeFile(receiptPath, 64<<10)
		if err != nil || !original() {
			return errReserveRuntime
		}
		if err := verifyPrevious(ctx); err != nil {
			return err
		}
		if !original() || !same(receiptPath, restoredReceipt, 64<<10) {
			return errReserveRuntime
		}
		return nil
	})
}

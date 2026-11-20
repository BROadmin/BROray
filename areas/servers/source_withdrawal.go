package servers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

// withdrawRemovedSource is the no-replacement branch of source removal. The
// caller has already disabled reserve and retains the protected source queue
// fence. Catalog records may be removed only after this transaction succeeds.
// An uncertain result keeps both the recovery journal and source catalog.
func (s *svc) withdrawRemovedSource(ctx context.Context, sourceID, activeID string) (retErr error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !s.reserveRuntimeFence(ctx) || sourceID == "" || len(sourceID) > 256 || !ValidID(activeID) || s.d.App == nil || s.d.App.Xray == nil {
		return errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || prefs.Enabled {
		return errReserveRuntime
	}
	prefBytes, err := readReservePreferencesBytes(reservePreferencesFile(s.d))
	if err != nil && (!errors.Is(err, os.ErrNotExist) || prefs.Exists) {
		return errReserveRuntime
	}
	if reservePreferencesRevision(prefBytes, prefs.Exists) != prefs.Revision {
		return errReserveRuntime
	}
	active, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
	if err != nil || strings.TrimSpace(string(active)) != activeID {
		return errReserveRuntime
	}
	record, err := reserveRuntimeFile(RecordPath(s.d, activeID), 1<<20)
	if err != nil {
		return errReserveRuntime
	}
	value, err := strictjson.Decode(record)
	m, ok := value.(map[string]any)
	if err != nil || !ok || m["id"] != activeID {
		return errReserveRuntime
	}
	source, ok := m["source"].(map[string]any)
	if !ok || source["type"] != "subscription" || source["subscriptionId"] != sourceID {
		return errReserveRuntime
	}
	config, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
	if err != nil {
		return errReserveRuntime
	}
	receipt, err := reserveRuntimeFile(s.d.Path("config", "interface.json"), 64<<10)
	if err != nil {
		return errReserveRuntime
	}
	settings, err := s.reserveRuntimeSettings()
	if err != nil {
		return errReserveRuntime
	}
	p := s.d.App.Xray
	snapshot := p.RuntimeSnapshot()
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
		if snapshot.Generation == 0 || snapshot.Generation >= ^uint64(0)-1 || snapshot.Intent == 0 || snapshot.Intent >= ^uint64(0)-1 {
			return errReserveRuntime
		}
		if snapshot.Running {
			if !snapshot.Wanted || snapshot.ExitConfirmed || snapshot.PID <= 0 || snapshot.LastPID != snapshot.PID {
				return errReserveRuntime
			}
		} else if !snapshot.ExitConfirmed || snapshot.PID != 0 || snapshot.LastPID <= 0 {
			return errReserveRuntime
		}
	}
	coldValid := func() bool { return cold == nil || cold.Check(ctx, s.d.Root) == nil }
	same := func(path string, want []byte, limit int64) bool {
		got, e := reserveRuntimeFile(path, limit)
		if want == nil {
			return errors.Is(e, os.ErrNotExist)
		}
		return e == nil && bytes.Equal(got, want)
	}
	// No Process calls here: this check also runs under the supervisor mutex.
	fixedInputs := func() bool {
		currentSettings, e := s.reserveRuntimeSettings()
		return coldValid() && e == nil && currentSettings == settings && s.reserveRuntimeFence(ctx) && s.d.App.Xray == p &&
			same(RecordPath(s.d, activeID), record, 1<<20) && same(reservePreferencesFile(s.d), prefBytes, 4096) &&
			same(s.d.Path("config", "interface.json"), receipt, 64<<10)
	}
	beforeFiles := func() bool {
		return fixedInputs() && same(s.d.Root.XrayConfig(), config, 2<<20) && same(ActiveFile(s.d), active, 4096)
	}
	current := func() bool { return beforeFiles() && p.RuntimeSnapshot() == snapshot }
	if !current() {
		return errReserveRuntime
	}
	description, err := s.d.Svc.OrdinaryInterfaceDescription()
	if err != nil || description == "" || description == "BROray - mix-auto" {
		return errReserveRuntime
	}
	target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
	if err != nil || target.Name == "" || !reservePrimaryMatches(config, target.Upstream) || !current() {
		return errReserveRuntime
	}
	if snapshot.Running {
		identity, e := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
		if e != nil {
			return errReserveRuntime
		}
		if _, e = reserve.VerifyListener(identity, target.Upstream); e != nil || !current() {
			return errReserveRuntime
		}
	}
	again, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
	if err != nil || again != target || !current() {
		return errReserveRuntime
	}
	j, err := reserve.OpenTransactionJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	id, err := j.Begin(ctx, reserve.TransactionFiles{Config: nil, Preferences: prefBytes, Active: nil}, reserve.TransactionIntent{
		Action: "source-withdraw", Authority: compat.SHA256Hex([]byte(sourceID + "\x00" + activeID + prefs.Revision + compat.SHA256Hex(record) + compat.SHA256Hex(config))),
		InterfaceName: target.Name, Upstream: target.Upstream.String(), PreviousDescription: description, PreviousDescriptionLine: target.DescriptionLine,
		NextDescription: description, ReceiptSHA256: compat.SHA256Hex(receipt),
	})
	if id != "" {
		defer func() {
			if retErr != nil {
				retErr = errors.Join(errReserveTransactionPending, retErr)
			}
		}()
	}
	if err != nil {
		return err
	}
	r, err := j.Read(ctx)
	if err != nil || !r.Previous.Config.Exists || !bytes.Equal(r.Previous.Config.Data, config) || !r.Previous.Active.Exists || !bytes.Equal(r.Previous.Active.Data, active) ||
		r.Previous.Preferences.Exists != prefs.Exists || !bytes.Equal(r.Previous.Preferences.Data, prefBytes) || !current() {
		return errReserveRuntime
	}
	install := func() error {
		if !beforeFiles() {
			return errReserveRuntime
		}
		return j.InstallConfig(ctx, id)
	}
	if cold != nil {
		err = p.InstallIfCold(ctx, snapshot, cold, install)
	} else {
		snapshot, err = p.StopIfCurrent(ctx, snapshot, install)
	}
	if err != nil {
		return err
	}
	if !fixedInputs() || p.RuntimeSnapshot() != snapshot || snapshot.Running || snapshot.Wanted || (!snapshot.ExitConfirmed && cold == nil) {
		return errReserveRuntime
	}
	if err := j.InstallSelection(ctx, id); err != nil {
		return err
	}
	return j.Confirm(ctx, id, func(ctx context.Context, r reserve.TransactionRecord) error {
		stopped := func() bool {
			return fixedInputs() && p.RuntimeSnapshot() == snapshot && !snapshot.Running && !snapshot.Wanted && (snapshot.ExitConfirmed || cold != nil) &&
				same(s.d.Root.XrayConfig(), nil, 2<<20) && same(ActiveFile(s.d), nil, 4096)
		}
		if r.Next.Config.Exists || r.Next.Active.Exists || !stopped() {
			return errReserveRuntime
		}
		after, e := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
		if e != nil || after != target || !stopped() {
			return errReserveRuntime
		}
		return nil
	})
}

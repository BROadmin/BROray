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

// replaceRemovedSource selects a validated profile outside the removed source.
// The caller retains the protected source queue fence and catalog snapshots;
// it must preserve the catalog on error and remove it only after success.
// This proves configuration/process/socket ownership, not Internet health.
func (s *svc) replaceRemovedSource(ctx context.Context, sourceID, activeID, nextID string) error {
	return s.replaceSourceCandidate(ctx, sourceID, activeID, nextID, false)
}

func (s *svc) replaceSourceCandidate(ctx context.Context, sourceID, activeID, nextID string, refreshed bool) error {
	return s.replaceSourceCandidateDescription(ctx, sourceID, activeID, nextID, refreshed, "")
}

func (s *svc) replaceSourceCandidateDescription(ctx context.Context, sourceID, activeID, nextID string, refreshed bool, previousDescription string) (retErr error) {
	phase := "replacement/preconditions"
	defer func() { s.logSourceFailure(phase, retErr) }()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if !s.reserveRuntimeFence(ctx) || sourceID == "" || len(sourceID) > 256 || !ValidID(activeID) || !ValidID(nextID) || (nextID == activeID && !refreshed) || s.d.App == nil || s.d.App.Xray == nil {
		return errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	if previousDescription != "" && (!refreshed || nextID != activeID) {
		return errReserveRuntime
	}
	phase = "replacement/preferences"
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
	phase = "replacement/catalog"
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
	nextRecord, err := reserveRuntimeFile(RecordPath(s.d, nextID), 1<<20)
	if err != nil {
		return errReserveRuntime
	}
	nextValue, err := strictjson.Decode(nextRecord)
	nextMap, ok := nextValue.(map[string]any)
	if err != nil || !ok || nextMap["id"] != nextID {
		return errReserveRuntime
	}
	sameSource := false
	if raw, exists := nextMap["source"]; exists && raw != nil {
		origin, ok := raw.(map[string]any)
		if !ok {
			return errReserveRuntime
		}
		sameSource = origin["type"] == "subscription" && origin["subscriptionId"] == sourceID
	}
	if sameSource != refreshed {
		return errReserveRuntime
	}
	if err := Validate(Record(nextMap), nextID); err != nil {
		return errors.Join(errSourceCandidateRejected, err)
	}
	phase = "replacement/runtime-files"
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
	core, err := reserveRuntimeOpen(s.d.Root.XrayBinary())
	if err != nil {
		return errReserveRuntime
	}
	defer core.Close()
	coreInfo, err := core.Stat()
	if err != nil || !coreInfo.Mode().IsRegular() || coreInfo.Mode().Perm()&0111 == 0 {
		return errReserveRuntime
	}
	coreMatches := func() bool {
		visible, e := reserveRuntimeOpen(s.d.Root.XrayBinary())
		if e != nil {
			return false
		}
		defer visible.Close()
		info, e := visible.Stat()
		return e == nil && os.SameFile(coreInfo, info) && info.Mode().Perm()&0111 != 0
	}
	p := s.d.App.Xray
	phase = "replacement/process-owner"
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
			coreMatches() && same(RecordPath(s.d, nextID), nextRecord, 1<<20)
	}
	beforeFiles := func() bool {
		return fixedInputs() && same(s.d.Root.XrayConfig(), config, 2<<20) && same(ActiveFile(s.d), active, 4096) && same(s.d.Path("config", "interface.json"), receipt, 64<<10)
	}
	current := func() bool { return beforeFiles() && p.RuntimeSnapshot() == snapshot }
	if !current() {
		return errReserveRuntime
	}
	phase = "replacement/interface-description"
	description := previousDescription
	var descriptionErr error
	if description == "" {
		description, descriptionErr = s.d.Svc.OrdinaryInterfaceDescription()
	}
	if descriptionErr != nil || description == "" || description == "BROray - mix-auto" {
		return errReserveRuntime
	}
	phase = "replacement/interface-before"
	target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
	if err != nil || target.Name == "" || !reservePrimaryMatches(config, target.Upstream) || !current() {
		return errReserveRuntime
	}
	phase = "replacement/listener-before"
	if snapshot.Running {
		identity, e := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
		if e != nil {
			return errReserveRuntime
		}
		if _, e = reserve.VerifyListener(identity, target.Upstream); e != nil || !current() {
			return errReserveRuntime
		}
	}
	phase = "replacement/render"
	nextConfig, err := XrayConfig(Record(nextMap), s.genSettings(ctx), RecordPath(s.d, nextID))
	if err != nil {
		return err
	}
	phase = "replacement/routing"
	nextConfig, err = s.d.Svc.ConfigureRouting(ctx, nextConfig)
	if err != nil {
		return err
	}
	phase = "replacement/health-inbound"
	if endpoint, present := reserve.DiagnosticEndpoint(config); present {
		nextConfig, err = reserve.AttachHealthInbound(nextConfig, endpoint)
		if err != nil {
			return err
		}
	}
	phase = "replacement/next-runtime-proof"
	if !reservePrimaryMatches(nextConfig, target.Upstream) || !current() {
		return errReserveRuntime
	}
	phase = "replacement/next-description"
	name, _ := nextMap["name"].(string)
	if name == "" {
		name = nextID
	}
	nextDescription, err := s.d.Svc.PreviewOrdinaryDescription(name)
	if err != nil || nextDescription == "" || nextDescription == "BROray - mix-auto" {
		return errReserveRuntime
	}
	phase = "replacement/core-validation"
	validationErr := s.d.Svc.ValidateXrayConfig(ctx, nextConfig)
	if !current() {
		return errReserveRuntime
	}
	if validationErr != nil {
		if errors.Is(validationErr, compat.ErrXrayCandidateRejected) {
			return errors.Join(errSourceCandidateRejected, validationErr)
		}
		return validationErr
	}
	phase = "replacement/interface-recheck"
	again, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
	if err != nil || again != target || !current() {
		return errReserveRuntime
	}
	phase = "replacement/journal"
	j, err := reserve.OpenTransactionJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	nextActive := []byte(nextID + "\n")
	id, err := j.Begin(ctx, reserve.TransactionFiles{Config: nextConfig, Preferences: prefBytes, Active: nextActive}, reserve.TransactionIntent{
		Action: "source-replace", Authority: compat.SHA256Hex([]byte(sourceID + "\x00" + activeID + nextID + prefs.Revision + compat.SHA256Hex(record) + compat.SHA256Hex(nextRecord) + compat.SHA256Hex(nextConfig))),
		InterfaceName: target.Name, Upstream: target.Upstream.String(), PreviousDescription: description, PreviousDescriptionLine: target.DescriptionLine,
		NextDescription: nextDescription, ReceiptSHA256: compat.SHA256Hex(receipt),
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
	phase = "replacement/install-runtime"
	wanted := snapshot.Wanted
	if cold != nil {
		err = p.InstallIfCold(ctx, snapshot, cold, install)
	} else if wanted {
		snapshot, err = p.ReplaceIfCurrent(ctx, snapshot, install)
	} else {
		snapshot, err = p.StopIfCurrent(ctx, snapshot, install)
	}
	if err != nil {
		return err
	}
	if !fixedInputs() || p.RuntimeSnapshot() != snapshot || snapshot.Wanted != wanted || snapshot.Running != wanted {
		return errReserveRuntime
	}
	phase = "replacement/install-selection"
	if err := j.InstallSelection(ctx, id); err != nil {
		return err
	}
	installed := func() bool {
		return fixedInputs() && p.RuntimeSnapshot() == snapshot &&
			same(s.d.Root.XrayConfig(), nextConfig, 2<<20) && same(ActiveFile(s.d), nextActive, 4096)
	}
	if !installed() {
		return errReserveRuntime
	}
	phase = "replacement/sync-description"
	if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
		return err
	}
	acceptedReceipt, err := reserveRuntimeFile(s.d.Path("config", "interface.json"), 64<<10)
	if err != nil || !installed() {
		return errReserveRuntime
	}
	phase = "replacement/confirm"
	return j.Confirm(ctx, id, func(ctx context.Context, r reserve.TransactionRecord) error {
		valid := func() bool { return installed() && same(s.d.Path("config", "interface.json"), acceptedReceipt, 64<<10) }
		deadline := time.Now().Add(10 * time.Second)
		for {
			if !valid() {
				return errReserveRuntime
			}
			if !wanted {
				after, e := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, nextDescription)
				if e != nil || after.Name != target.Name || after.Upstream != target.Upstream || !valid() || (!snapshot.ExitConfirmed && cold == nil) {
					return errReserveRuntime
				}
				return nil
			}
			identity, e := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
			if e == nil && valid() {
				after, e := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, nextDescription)
				if e == nil && after.Name == target.Name && after.Upstream == target.Upstream && valid() {
					listener, e := reserve.VerifyListener(identity, target.Upstream)
					if e == nil {
						final, e := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, nextDescription)
						last, le := reserve.VerifyListener(identity, target.Upstream)
						if e == nil && le == nil && final == after && last == listener && valid() {
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

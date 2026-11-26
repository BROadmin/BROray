package servers

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	return s.replaceSourceCandidateDescription(ctx, sourceID, activeID, nextID, refreshed, "", false, nil)
}

func (s *svc) replaceSourceCandidateDescription(ctx context.Context, sourceID, activeID, nextID string, refreshed bool, previousDescription string, renameOnly bool, proof *pendingSourceProof) (retErr error) {
	phase := "replacement/preconditions"
	defer func() { s.logSourceFailure(phase, retErr) }()
	budget := 45 * time.Second
	if proof != nil {
		budget = 80 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if !s.reserveRuntimeFence(ctx) || sourceID == "" || len(sourceID) > 256 || !ValidID(activeID) || !ValidID(nextID) || (nextID == activeID && !refreshed) || s.d.App == nil || s.d.App.Xray == nil {
		return errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	if previousDescription != "" && (!refreshed || nextID != activeID) || renameOnly && previousDescription == "" {
		return errReserveRuntime
	}
	phase = "replacement/preferences"
	prefs, err := readReservePreferences(s.d)
	if err != nil || (prefs.Enabled && proof == nil) {
		return errReserveRuntime
	}
	emergency := proof != nil && prefs.Enabled && !reserveRosterContains(prefs, activeID)
	if emergency && (prefs.Emergency == nil || prefs.Emergency.ServerID != activeID) {
		return compat.ErrXrayCandidateRejected
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
	var hotPlan *pendingSourceHotPlan
	if proof != nil {
		if compat.SHA256Hex(config) != proof.configSHA || compat.SHA256Hex(nextRecord) != proof.candidateSHA {
			return compat.ErrXrayCandidateRejected
		}
		if control, hot := reserve.HotLayoutSocket(config); hot {
			hotPlan, err = s.preparePendingSourceHot(ctx, sourceID, activeID, prefs, config, proof.previousRecord, control)
			if err != nil {
				return err
			}
		}
		if _, prepared := reserve.DiagnosticEndpoint(config); prefs.Enabled && !emergency && !prepared {
			return compat.ErrXrayCandidateRejected
		}
		if emergency {
			if err := s.proveEmergencySourcePredecessor(ctx, sourceID, activeID, prefs.Emergency.Fingerprint, proof.previousRecord, config); err != nil {
				return err
			}
		}
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
	fixedInputs := func(expectedPrefs []byte) bool {
		currentSettings, e := s.reserveRuntimeSettings()
		return coldValid() && e == nil && currentSettings == settings && s.reserveRuntimeFence(ctx) && s.d.App.Xray == p &&
			same(RecordPath(s.d, activeID), record, 1<<20) && same(reservePreferencesFile(s.d), expectedPrefs, 4096) &&
			coreMatches() && same(RecordPath(s.d, nextID), nextRecord, 1<<20) && (hotPlan == nil || hotPlan.current())
	}
	beforeFiles := func() bool {
		return fixedInputs(prefBytes) && same(s.d.Root.XrayConfig(), config, 2<<20) && same(ActiveFile(s.d), active, 4096) && same(s.d.Path("config", "interface.json"), receipt, 64<<10)
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
	if descriptionErr != nil || description == "" || (description == "BROray - mix-auto") != prefs.Enabled {
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
		if hotPlan != nil && (p.VerifyReserveOutboundIfCurrent(ctx, snapshot, hotPlan.slot) != nil || !current()) {
			return compat.ErrXrayCandidateRejected
		}
	}
	nextConfig := config
	nextPrefs := prefBytes
	if hotPlan != nil {
		nextConfig = hotPlan.nextConfig
	} else if !renameOnly {
		phase = "replacement/render"
		nextConfig, err = XrayConfig(Record(nextMap), s.genSettings(ctx), RecordPath(s.d, nextID))
		if err != nil {
			return err
		}
		if emergency && compat.SHA256Hex(nextConfig) != prefs.Emergency.Fingerprint {
			updated := prefs.reservePreferences
			updated.Emergency = &reserveEmergency{ServerID: activeID, Fingerprint: compat.SHA256Hex(nextConfig)}
			nextPrefs, err = compat.Marshal(updated)
			if err != nil {
				return err
			}
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
	}
	var checkLease io.Closer
	if proof != nil && snapshot.Wanted && hotPlan == nil {
		nextConfig, checkLease = s.preparePolicyVPN(ctx, nextConfig)
		defer func() {
			if checkLease != nil {
				_ = checkLease.Close()
			}
		}()
		if _, ok := reserve.DiagnosticEndpoint(nextConfig); !ok {
			return compat.ErrXrayCandidateRejected
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
	nextDescription := "BROray - mix-auto"
	if !prefs.Enabled {
		nextDescription, err = s.d.Svc.PreviewOrdinaryDescription(name)
		if err != nil || nextDescription == "" || nextDescription == "BROray - mix-auto" {
			return errReserveRuntime
		}
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
	// Display changes preserve the installed bytes and selected ID. A
	// retained live profile with equal rendered bytes also needs no restart.
	keepRuntime := renameOnly || (previousDescription != "" && snapshot.Running && bytes.Equal(nextConfig, config))
	action := "source-replace"
	if keepRuntime || proof != nil {
		nextActive = active
	}
	if proof != nil {
		action = reserve.SourceProfileTransactionAction
	}
	emergencyChange := emergency && !bytes.Equal(nextPrefs, prefBytes)
	if emergencyChange {
		action = reserve.SourceEmergencyProfileTransactionAction
	}
	authority := compat.SHA256Hex([]byte(sourceID + "\x00" + activeID + nextID + prefs.Revision + compat.SHA256Hex(record) + compat.SHA256Hex(nextRecord) + compat.SHA256Hex(nextConfig)))
	confirm := j.Confirm
	if proof != nil && proof.recoveryAuthority != "" {
		authority = proof.recoveryAuthority
		confirm = j.ConfirmSourceProfile
	}
	id, err := j.Begin(ctx, reserve.TransactionFiles{Config: nextConfig, Preferences: nextPrefs, Active: nextActive}, reserve.TransactionIntent{
		Action: action, Authority: authority,
		InterfaceName: target.Name, Upstream: target.Upstream.String(), PreviousDescription: description, PreviousDescriptionLine: target.DescriptionLine,
		NextDescription: nextDescription, ReceiptSHA256: compat.SHA256Hex(receipt),
	})
	rolledBack := false
	if id != "" {
		defer func() {
			if retErr != nil && !rolledBack {
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
	if checkLease != nil {
		err = checkLease.Close()
		checkLease = nil
		if err != nil {
			return err
		}
	}
	if proof != nil && proof.op != nil && wanted && !keepRuntime {
		proof.op.RecordSourceProfileApplyStart()
	}
	switch {
	case keepRuntime:
		if !current() {
			return errReserveRuntime
		}
	case cold != nil:
		err = p.InstallIfCold(ctx, snapshot, cold, install)
	case wanted:
		snapshot, err = p.ReplaceIfCurrent(ctx, snapshot, install)
	default:
		snapshot, err = p.StopIfCurrent(ctx, snapshot, install)
	}
	if err != nil {
		return err
	}
	if !fixedInputs(prefBytes) || p.RuntimeSnapshot() != snapshot || snapshot.Wanted != wanted || snapshot.Running != wanted {
		return errReserveRuntime
	}
	phase = "replacement/install-selection"
	if !emergencyChange {
		if err := j.InstallSelection(ctx, id); err != nil {
			return err
		}
	}
	expectedPrefs := prefBytes
	installed := func() bool {
		return fixedInputs(expectedPrefs) && p.RuntimeSnapshot() == snapshot &&
			same(s.d.Root.XrayConfig(), nextConfig, 2<<20) && same(ActiveFile(s.d), nextActive, 4096)
	}
	if !installed() {
		return errReserveRuntime
	}
	if proof != nil && wanted {
		phase = "replacement/vpn-check"
		preDescription := func() bool {
			return installed() && same(s.d.Path("config", "interface.json"), receipt, 64<<10)
		}
		check := compat.VPNUnconfirmed
		if hotPlan == nil || s.selectPendingSourceHot(ctx, p, snapshot, nextConfig, hotPlan, preDescription) {
			check = s.pendingSourceVPNCheck(ctx, nextConfig, preDescription, proof)
		}
		if proof.op != nil {
			proof.op.RecordSourceProfileVPNCheck(check)
		}
		if check != compat.VPNVerified {
			phase = "replacement/vpn-rollback"
			if proof.op != nil {
				proof.op.Phase("recovering")
				proof.op.RecordRollback(false, false)
			}
			if !preDescription() {
				return errReserveRuntime
			}
			if hotPlan != nil {
				// The new process is about to be replaced. Blocking is best
				// effort since an unconfirmed selector can already be absent.
				_ = p.SelectReserveOutboundIfCurrent(ctx, snapshot, reserve.HotBlockedTag)
			}
			if !keepRuntime {
				snapshot, err = p.ReplaceIfCurrent(ctx, snapshot, func() error {
					if !fixedInputs(prefBytes) || !same(s.d.Root.XrayConfig(), nextConfig, 2<<20) ||
						!same(ActiveFile(s.d), active, 4096) || !same(s.d.Path("config", "interface.json"), receipt, 64<<10) {
						return errReserveRuntime
					}
					return j.RestoreConfig(ctx, id)
				})
				if err != nil {
					return err
				}
			}
			oldCurrent := func() bool { return beforeFiles() && p.RuntimeSnapshot() == snapshot }
			oldListener := func() bool {
				processID, e := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
				if e != nil {
					return false
				}
				_, e = reserve.VerifyListener(processID, target.Upstream)
				return e == nil
			}
			if !waitPendingSourceReady(ctx, oldCurrent, oldListener) {
				return errReserveRuntime
			}
			if hotPlan != nil && !s.selectPendingSourceHot(ctx, p, snapshot, config, hotPlan, oldCurrent) {
				return errReserveRuntime
			}
			if err = j.RollbackPrepared(ctx, r, func(ctx context.Context, _ reserve.TransactionRecord) error {
				owned, e := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, description)
				if e != nil || owned != target || !oldCurrent() || !oldListener() {
					return errReserveRuntime
				}
				return nil
			}); err != nil {
				return err
			}
			rolledBack = true
			restoredVPN := s.pendingSourceVPNCheck(ctx, config, oldCurrent, proof)
			if proof.op != nil {
				proof.op.RecordRollback(true, restoredVPN == compat.VPNVerified)
			}
			return compat.ErrSourceProfileRolledBack
		}
	}
	if emergencyChange {
		phase = "replacement/install-emergency-fingerprint"
		if !installed() || !same(s.d.Path("config", "interface.json"), receipt, 64<<10) {
			return errReserveRuntime
		}
		if err := j.InstallSelection(ctx, id); err != nil {
			return err
		}
		expectedPrefs = nextPrefs
	}
	phase = "replacement/sync-description"
	if proof == nil || nextDescription != description {
		if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
			return err
		}
	}
	acceptedReceipt, err := reserveRuntimeFile(s.d.Path("config", "interface.json"), 64<<10)
	if err != nil || !installed() {
		return errReserveRuntime
	}
	phase = "replacement/confirm"
	return confirm(ctx, id, func(ctx context.Context, r reserve.TransactionRecord) error {
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
						if e == nil && le == nil && final == after && last == listener && valid() &&
							(hotPlan == nil || p.VerifyReserveOutboundIfCurrent(ctx, snapshot, hotPlan.slot) == nil && valid()) {
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

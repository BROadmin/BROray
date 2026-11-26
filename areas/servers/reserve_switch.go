package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

var errReserveTransactionPending = compat.ErrReserveTransactionPending
var errReserveSwitchRolledBack = errors.New("RESERVE_SWITCH_ROLLED_BACK")

// The queue integration must retain its global/daemon-owned reserve fence on
// errReserveTransactionPending. A verified live rollback may release the fence;
// otherwise cold recovery restores these images before any area starts.
func (s *svc) switchReserve(ctx context.Context, m *reserveMonitor, result reserveMonitorResult) (retErr error) {
	if m == nil || !s.reserveRuntimeFence(ctx) || !m.mu.TryLock() {
		return errReserveRuntime
	}
	defer m.mu.Unlock()
	if m.owner != s || m.blocked || result.Intent == nil || m.state.Pending == nil || *result.Intent != *m.state.Pending {
		return errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	intent, guard := *result.Intent, result.Guard
	if intent.Token == 0 || intent.From == intent.To || intent.From != m.state.ActiveID || intent.ActiveIdentity != m.state.ActiveIdentity ||
		guard.identity != intent.ActiveIdentity || guard.plan.node.ID != intent.From {
		return errReserveRuntime
	}
	previous := m.binding
	if intent.Reason == "active-failed" || intent.Reason == "auto-choice" {
		previous = m.lastFailure
	} else if intent.Reason != "primary-recovered" && intent.Reason != "roster-recovered" {
		return errReserveRuntime
	}
	if previous == nil || previous.process != guard.process || previous.snapshot != guard.snapshot || previous.identity != guard.identity ||
		previous.proofIdentity != guard.proofIdentity || !sameReservePlan(previous.plan, guard.plan) {
		return errReserveRuntime
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || !prefs.Enabled || !prefs.Configured {
		return errReserveRuntime
	}
	plans := map[string]reserveRuntimePlan{}
	authority := ""
	for _, id := range append([]string{prefs.PrimaryServerID}, prefs.BackupServerIDs...) {
		plan, err := s.prepareReserveRuntime(ctx, id, guard.plan.endpoint)
		if err != nil || plan.revision != prefs.Revision {
			return errReserveRuntime
		}
		plans[id] = plan
		authority += id + "\x00" + plan.revision + plan.recordSHA + plan.settingsSHA + plan.configSHA
	}
	if !reserveRosterContains(prefs, intent.From) {
		plan, err := s.prepareReserveRuntime(ctx, intent.From, guard.plan.endpoint)
		if err != nil {
			return err
		}
		plans[intent.From] = plan
	}
	autoPolicyCurrent := func() bool {
		if intent.Reason != "auto-choice" {
			return result.AutoConfigSHA == ""
		}
		h, err := fileSHA256(autoSwitchFile(s.d))
		return err == nil && h == result.AutoConfigSHA
	}
	if intent.Reason == "auto-choice" {
		if !autoPolicyCurrent() || reserveRosterContains(prefs, intent.To) ||
			!reserve.AllRosterUnavailable(m.state, time.Now(), reserve.DefaultPolicy()) {
			return errReserveRuntime
		}
		plan, err := s.prepareReserveRuntimeFor(ctx, intent.To, guard.plan.endpoint, true)
		if err != nil {
			return err
		}
		plans[intent.To] = plan
	}
	if intent.Reason == "roster-recovered" && (prefs.Emergency == nil || !reserveRosterContains(prefs, intent.To)) {
		return errReserveRuntime
	}
	next, ok := plans[intent.To]
	if !ok || authority != m.authority || !sameReservePlan(plans[intent.From], guard.plan) || next.node.Fingerprint != intent.Fingerprint {
		return errReserveRuntime
	}
	// These checks contain no Process calls and may run under its mutex in
	// the local installation callback. Runtime proof is checked separately.
	inputs := func(active, revision string) bool {
		for id, plan := range plans {
			plan.revision = revision
			if !s.reservePlanInputs(ctx, plan, true, id == active) {
				return false
			}
		}
		return true
	}
	ready := func() bool {
		now := time.Now()
		v := m.state.Samples[intent.To]
		policy := reserve.DefaultPolicy()
		return !intent.IssuedAt.After(now) && now.Sub(intent.IssuedAt) <= policy.SampleAge &&
			v.Verified && v.Verdict == "healthy" && v.Identity != "" && v.Fingerprint == next.node.Fingerprint &&
			!v.FinishedAt.After(now) && now.Sub(v.FinishedAt) <= policy.ReadinessTTL
	}
	current := func() bool {
		if !inputs(intent.From, prefs.Revision) || !autoPolicyCurrent() || !ready() || guard.process == nil || s.d.App == nil || s.d.App.Xray != guard.process || !s.reserveCoreMatches(guard.target.Process) {
			return false
		}
		snapshot := guard.process.RuntimeSnapshot()
		return snapshot == guard.snapshot || snapshot.ConfirmsUnexpectedExit(guard.snapshot)
	}
	if !current() {
		return errReserveRuntime
	}
	if err := s.d.Svc.ValidateXrayConfig(ctx, next.config); err != nil {
		return err
	}
	if !current() {
		return errReserveRuntime
	}
	receiptPath := s.d.Path("config", "interface.json")
	receipt, err := reserveRuntimeFile(receiptPath, 64<<10)
	if err != nil {
		return err
	}
	sameReceipt := func() bool {
		raw, err := reserveRuntimeFile(receiptPath, 64<<10)
		return err == nil && bytes.Equal(raw, receipt)
	}
	target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, "BROray - mix-auto")
	if err != nil || target.Name == "" || !reservePrimaryMatches(guard.plan.config, target.Upstream) || !reservePrimaryMatches(next.config, target.Upstream) || !sameReceipt() || !current() {
		return errReserveRuntime
	}
	prefBytes, err := readReservePreferencesBytes(reservePreferencesFile(s.d))
	if err != nil || reservePreferencesRevision(prefBytes, true) != prefs.Revision {
		return errReserveRuntime
	}
	nextBytes := prefBytes
	if intent.Reason == "auto-choice" || prefs.Emergency != nil {
		nextPrefs := prefs.reservePreferences
		nextPrefs.Emergency = nil
		if intent.Reason == "auto-choice" {
			nextPrefs.Emergency = &reserveEmergency{ServerID: next.node.ID, Fingerprint: next.node.Fingerprint}
		}
		nextBytes, err = json.Marshal(nextPrefs)
		if err != nil {
			return err
		}
	}
	nextRevision := reservePreferencesRevision(nextBytes, true)
	j, err := reserve.OpenTransactionJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	id, err := j.Begin(ctx, reserve.TransactionFiles{Config: next.config, Preferences: nextBytes, Active: []byte(intent.To + "\n")}, reserve.TransactionIntent{
		Action: "switch", Authority: compat.SHA256Hex([]byte(authority + guard.identity)), InterfaceName: target.Name, Upstream: target.Upstream.String(),
		PreviousDescription: "BROray - mix-auto", PreviousDescriptionLine: target.DescriptionLine, NextDescription: "BROray - mix-auto", ReceiptSHA256: compat.SHA256Hex(receipt),
	})
	var replaced xray.RuntimeSnapshot
	var rollbackRecord reserve.TransactionRecord
	hot := guard.plan.hotSlot != "" && next.hotSlot != "" && bytes.Equal(guard.plan.config, next.config) &&
		guard.process.RuntimeSnapshot() == guard.snapshot
	if id != "" {
		defer func() {
			if retErr == nil {
				return
			}
			if replaced.Running {
				restored, rollbackErr := s.rollbackReserveSwitch(ctx, j, rollbackRecord, guard, replaced, target, hot, func() bool {
					return (inputs("", prefs.Revision) || inputs("", nextRevision)) && sameReceipt()
				})
				if rollbackErr == nil {
					m.state, _ = reserve.Advance(m.state, reserve.Event{Kind: "switch-result", Token: intent.Token, Verified: true, Success: false, CurrentID: intent.From, CurrentIdentity: restored.identity}, time.Now(), reserve.DefaultPolicy())
					m.binding, m.lastFailure = &restored, nil
					retErr = errors.Join(errReserveSwitchRolledBack, retErr)
					if restored.plan.hotSlot != "" || s.probeRestoredReserveTraffic(ctx, restored) {
						retErr = errors.Join(errReserveRollbackVPNVerified, retErr)
					}
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
	if err != nil || r.Previous.Config.SHA256 != guard.plan.configSHA || !bytes.Equal(r.Previous.Preferences.Data, prefBytes) || !current() || !sameReceipt() {
		return errReserveRuntime
	}
	rollbackRecord = r
	snapshot := guard.snapshot
	if !hot {
		snapshot, err = guard.process.ReplaceIfCurrent(ctx, guard.snapshot, func() error {
			if !inputs(intent.From, prefs.Revision) || !autoPolicyCurrent() || !ready() || !sameReceipt() || !s.reserveCoreMatches(guard.target.Process) {
				return errReserveRuntime
			}
			return j.InstallConfig(ctx, id)
		})
		if err != nil {
			return err
		}
	} else if guard.process.RuntimeSnapshot() != snapshot {
		return errReserveRuntime
	}
	replaced = snapshot
	if err := j.InstallSelection(ctx, id); err != nil {
		return err
	}
	next.revision = nextRevision
	committedGuard := guard
	committedGuard.plan.revision = nextRevision
	// The child can need a short interval to bind its sockets. A different
	// generation is never accepted while waiting, including automatic restart.
	var binding reserveActiveBinding
	deadline := time.Now().Add(5 * time.Second)
	for {
		if guard.process.RuntimeSnapshot() != snapshot || !inputs(intent.To, nextRevision) || !sameReceipt() {
			return errReserveRuntime
		}
		binding, err = s.bindReserveRuntime(ctx, next, nil)
		if err == nil && binding.snapshot == snapshot {
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
	err = j.Confirm(ctx, id, func(ctx context.Context, r reserve.TransactionRecord) error {
		if !inputs(intent.To, nextRevision) || !autoPolicyCurrent() || !sameReceipt() || r.ID != id {
			return errReserveRuntime
		}
		if err := s.verifyReserveSwitchCommit(ctx, committedGuard, binding, target); err != nil ||
			!s.confirmReserveHotTraffic(ctx, binding) || !inputs(intent.To, nextRevision) || !sameReceipt() {
			return errReserveRuntime
		}
		return nil
	})
	if err != nil {
		return err
	}
	m.activeEvidence.Store(nil)
	m.state, _ = reserve.Advance(m.state, reserve.Event{Kind: "switch-result", Token: intent.Token, Verified: true, Success: true, CurrentID: intent.To, CurrentIdentity: binding.identity}, time.Now(), reserve.DefaultPolicy())
	m.binding, m.lastFailure = &binding, nil
	return nil
}

func (s *svc) verifyReserveSwitchCommit(ctx context.Context, previous, next reserveActiveBinding, target compat.OwnedInterfaceTarget) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	valid := func() bool {
		return previous.process != nil && next.process == previous.process &&
			next.process.RuntimeSnapshot() == next.snapshot &&
			s.reserveCoreMatches(previous.target.Process) && s.reserveCoreMatches(next.target.Process) &&
			s.reservePlanInputs(ctx, previous.plan, true, false) && s.reservePlanInputs(ctx, next.plan, true, true)
	}
	for {
		if !valid() {
			return errReserveRuntime
		}
		proof, err := s.verifyReserveCommit(ctx, next, "BROray - mix-auto")
		if err == nil {
			if proof.Interface != target || !valid() {
				return errReserveRuntime
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return errReserveRuntime
		case <-time.After(200 * time.Millisecond):
		}
	}
}

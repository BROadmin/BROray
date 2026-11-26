package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

var errReserveNoReachable = errors.New("RESERVE_NO_REACHABLE_SERVER")

// Enable starts from an owned runtime. Reapplying an enabled setting repairs a
// missing diagnostic listener; an unauthorized outside server needs a proven
// replacement from the saved roster.
func (s *svc) enableReserve(ctx context.Context, revision string) error {
	return s.applyReserveSelection(ctx, revision, reserveSelection{})
}

func (s *svc) applyReserveSelection(ctx context.Context, revision string, request reserveSelection) (retErr error) {
	if !s.reserveRuntimeFence(ctx) || s.d.App == nil || s.d.App.Xray == nil {
		return errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || !prefs.Configured || prefs.Revision != revision {
		return errReserveRuntime
	}
	prefBytes, err := readReservePreferencesBytes(reservePreferencesFile(s.d))
	if err != nil || reservePreferencesRevision(prefBytes, true) != revision {
		return errReserveRuntime
	}
	active, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
	if err != nil || !ValidID(strings.TrimSpace(string(active))) {
		return errReserveRuntime
	}
	previousRecord, err := reserveRuntimeFile(RecordPath(s.d, strings.TrimSpace(string(active))), 1<<20)
	if err != nil {
		return err
	}
	previousConfig, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
	if err != nil {
		return err
	}
	previousID := strings.TrimSpace(string(active))
	selected := previousID
	endpoint, hasEndpoint := reserve.DiagnosticEndpoint(previousConfig)
	manual := request.server != ""
	disableHot := request.mode == "disable-hot"
	newPrefs := prefs.reservePreferences
	if manual {
		if !prefs.Enabled || !hasEndpoint {
			return errReserveRuntime
		}
		if disableHot {
			if _, hot := reserve.HotLayoutSocket(previousConfig); !hot || request.server != previousID || request.remove != "" {
				return errReserveRuntime
			}
			newPrefs.Enabled = false
			newPrefs.Emergency = nil
		} else {
			proposed, denied := proposedReserveSelection(prefs, request)
			if denied != nil {
				return denied
			}
			newPrefs = proposed
		}
	} else {
		newPrefs.Enabled = true
	}
	if !manual && prefs.Enabled && hasEndpoint {
		return errReserveRuntime
	}
	repairDiagnostic := prefs.Enabled && !manual
	repairOutside := repairDiagnostic && !reserveRosterContains(prefs, selected)
	repairEmergency := repairDiagnostic && prefs.Emergency != nil
	if repairEmergency && (!repairOutside || prefs.Emergency.ServerID != selected) {
		return errReserveRuntime
	}
	if repairOutside {
		rec, err := ReadRecord(s.d, selected)
		if err != nil {
			return errReserveRuntime
		}
		base, err := XrayConfig(rec, s.genSettings(ctx), RecordPath(s.d, selected))
		if err != nil {
			return errReserveRuntime
		}
		if repairEmergency && prefs.Emergency.Fingerprint != compat.SHA256Hex(base) {
			return errReserveRuntime
		}
		expected, err := s.d.Svc.ConfigureRouting(ctx, base)
		if err != nil {
			return errReserveRuntime
		}
		if !bytes.Equal(previousConfig, expected) {
			if !repairEmergency {
				return errReserveRuntime
			}
			// An emergency config was rendered with the reserve diagnostic
			// encoder. After it disappears, only serialization can differ
			// from the ordinary routed image. Reject ambiguous JSON.
			got, gotErr := strictjson.Decode(previousConfig)
			want, wantErr := strictjson.Decode(expected)
			if gotErr != nil || wantErr != nil || !reflect.DeepEqual(got, want) {
				return errReserveRuntime
			}
		}
	}
	p := s.d.App.Xray
	snapshot := p.RuntimeSnapshot()
	if !snapshot.Running || !snapshot.Wanted || snapshot.ExitConfirmed {
		return errReserveRuntime
	}
	identity, err := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
	if err != nil {
		return errReserveRuntime
	}
	description := "BROray - mix-auto"
	if !prefs.Enabled {
		description, err = s.d.Svc.OrdinaryInterfaceDescription()
		if err != nil {
			return err
		}
	}
	before, err := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, description)
	if err != nil || before.Name == "" || !reservePrimaryMatches(previousConfig, before.Upstream) {
		return errReserveRuntime
	}
	listener, err := reserve.VerifyListener(identity, before.Upstream)
	if err != nil {
		return errReserveRuntime
	}
	receipt, err := reserveRuntimeFile(s.d.Path("config", "interface.json"), 64<<10)
	if err != nil {
		return err
	}
	var reserved net.Listener
	if !hasEndpoint {
		reserved, err = net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return err
		}
		defer reserved.Close()
		endpoint = reserved.Addr().(*net.TCPAddr).AddrPort()
	}
	plans := map[string]reserveRuntimePlan{}
	roster := append([]string{prefs.PrimaryServerID}, prefs.BackupServerIDs...)
	for _, id := range roster {
		plan, err := s.prepareReserveRuntime(ctx, id, endpoint)
		if err != nil || plan.revision != revision {
			return errReserveRuntime
		}
		plans[id] = plan
	}
	var previousBinding reserveActiveBinding
	if manual {
		previous, err := s.prepareReserveRuntime(ctx, selected, endpoint)
		if err != nil || !bytes.Equal(previousConfig, previous.config) {
			return errReserveRuntime
		}
		previousBinding, err = s.bindReserveRuntime(ctx, previous, nil)
		if err != nil || previousBinding.snapshot != snapshot {
			return errReserveRuntime
		}
		if request.mode == "" && request.server == selected {
			return nil
		}
		if !reserveRosterContains(prefs, request.server) {
			outside, err := s.prepareReserveRuntimeFor(ctx, request.server, endpoint, true)
			if err != nil || outside.revision != revision {
				return errReserveRuntime
			}
			plans[request.server] = outside
		}
	}
	planPrefs := prefs
	inputs := func() bool {
		for _, plan := range plans {
			plan.revision = planPrefs.Revision
			plan.outsideRoster = !reserveRosterContains(planPrefs, plan.node.ID)
			if !s.reservePlanInputs(ctx, plan, false, false) {
				return false
			}
		}
		return true
	}
	sameFile := func(path string, want []byte, limit int64) bool {
		got, e := reserveRuntimeFile(path, limit)
		return e == nil && bytes.Equal(got, want)
	}
	unchangedFiles := func() bool {
		return inputs() && sameFile(RecordPath(s.d, previousID), previousRecord, 1<<20) && sameFile(s.d.Root.XrayConfig(), previousConfig, 2<<20) && sameFile(ActiveFile(s.d), active, 4096) && sameFile(s.d.Path("config", "interface.json"), receipt, 64<<10) && s.reserveCoreMatches(identity)
	}
	current := func() bool { return s.d.App.Xray == p && p.RuntimeSnapshot() == snapshot && unchangedFiles() }
	if !current() {
		return errReserveRuntime
	}
	var choice reserve.Sample
	needsProbe := (manual && !disableHot) || repairOutside
	if needsProbe {
		candidates := roster
		if manual {
			candidates = []string{request.server}
		}
		for _, id := range candidates {
			if id == previousID {
				choice, err = s.probeReserveRuntime(ctx, previousBinding), nil
			} else {
				choice, err = s.reserveSelectionProbe(ctx, id)
			}
			if errors.Is(err, errReserveProbeCleanup) || errors.Is(err, reserve.ErrProbeWorkspaceUnconfirmed) {
				return errors.Join(errReserveTransactionPending, err)
			}
			if !current() {
				return errReserveRuntime
			}
			if err == nil && verifiedReserveSelection(choice, plans[id]) {
				selected = id
				break
			}
			choice = reserve.Sample{}
		}
		if choice.ServerID == "" {
			return errReserveNoReachable
		}
	} else if !reserveRosterContains(prefs, selected) {
		selected = prefs.PrimaryServerID
	}
	next := plans[selected]
	if !prefs.Enabled {
		next = s.initialReserveHotPlan(next, roster, plans)
	}
	if request.mode == "promote" && previousBinding.plan.hotSlot != "" && next.hotSlot != "" {
		// The saved slot order belongs to the previous roster. Commit a
		// standard runtime while the roster and primary change together.
		next, err = s.prepareReserveStockRuntime(ctx, selected, endpoint, false)
		if err != nil {
			return err
		}
	}
	nextDescription, action := "BROray - mix-auto", "enable"
	if !newPrefs.Enabled {
		record, err := ReadRecord(s.d, selected)
		if err != nil {
			return errReserveRuntime
		}
		next, err = s.ordinaryReserveRuntime(ctx, next, record)
		if err != nil {
			return err
		}
		if disableHot {
			nextDescription, err = s.d.Svc.OrdinaryInterfaceDescription()
		} else {
			nextDescription, err = s.d.Svc.PreviewOrdinaryDescription(jqRaw(alt(record["name"], selected)))
		}
		if err != nil || nextDescription == "" || nextDescription == "BROray - mix-auto" {
			return errReserveRuntime
		}
		action = "exit-switch"
	}
	ready := func() bool { return !needsProbe || verifiedReserveSelection(choice, next) }
	if !reservePrimaryMatches(next.config, before.Upstream) {
		return errReserveRuntime
	}
	if prefs.Enabled && !needsProbe && !disableHot {
		expected, err := reserve.AttachHealthInbound(previousConfig, endpoint)
		if err != nil || !bytes.Equal(expected, next.config) {
			return errReserveRuntime
		}
	}
	if err := s.d.Svc.ValidateXrayConfig(ctx, next.config); err != nil {
		return err
	}
	afterTarget, err := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, description)
	if err != nil || afterTarget != before || !current() || !ready() {
		return errReserveRuntime
	}
	afterListener, err := reserve.VerifyListener(identity, before.Upstream)
	if err != nil || afterListener != listener {
		return errReserveRuntime
	}
	nextBytes, nextActive := prefBytes, active
	if repairEmergency {
		newPrefs.Emergency = nil
	}
	if !prefs.Enabled || repairEmergency || (manual && (prefs.Emergency != nil || request.mode != "")) {
		nextBytes, err = json.Marshal(newPrefs)
		if err != nil {
			return err
		}
	}
	if selected != previousID || !prefs.Enabled {
		nextActive = []byte(selected + "\n")
	}
	j, err := reserve.OpenTransactionJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	id, err := j.Begin(ctx, reserve.TransactionFiles{Config: next.config, Preferences: nextBytes, Active: nextActive}, reserve.TransactionIntent{
		Action: action, Authority: compat.SHA256Hex([]byte(revision + compat.SHA256Hex(previousConfig))), InterfaceName: before.Name, Upstream: before.Upstream.String(), PreviousDescription: description, PreviousDescriptionLine: before.DescriptionLine, NextDescription: nextDescription, ReceiptSHA256: compat.SHA256Hex(receipt),
	})
	var rollback reserveEnableRollback
	stage := "prepare"
	if id != "" {
		defer func() {
			if retErr == nil {
				return
			}
			if rollback.authorized {
				rollbackErr := s.rollbackReserveEnable(ctx, j, rollback)
				if rollbackErr == nil {
					s.logf("RESERVE_SELECTION_ROLLBACK=CONFIRMED stage=%s", stage)
					retErr = errors.Join(errReserveModeRolledBack, retErr)
					if previousBinding.plan.hotSlot != "" {
						retErr = errors.Join(errReserveRollbackVPNVerified, retErr)
					}
					return
				}
				retErr = errors.Join(retErr, errReserveRollbackUnconfirmed, rollbackErr)
			}
			s.logf("RESERVE_SELECTION_RECOVERY=REQUIRED stage=%s", stage)
			retErr = errors.Join(errReserveTransactionPending, retErr)
		}()
	}
	if err != nil {
		return err
	}
	r, err := j.Read(ctx)
	if err != nil || !bytes.Equal(r.Previous.Config.Data, previousConfig) || !bytes.Equal(r.Previous.Active.Data, active) || !bytes.Equal(r.Previous.Preferences.Data, prefBytes) || !current() || !ready() {
		return errReserveRuntime
	}
	rollback = reserveEnableRollback{record: r, process: p, snapshot: snapshot, identity: identity, plans: plans, previousRecord: previousRecord, authorized: true}
	stage = "replace_xray"
	if reserved != nil {
		if err := reserved.Close(); err != nil {
			return err
		}
	}
	if !bytes.Equal(previousConfig, next.config) {
		rollback.authorized = false // Failed replacement supplies no rollback authority.
		snapshot, err = p.ReplaceIfCurrent(ctx, snapshot, func() error {
			if !unchangedFiles() || !ready() {
				return errReserveRuntime
			}
			return j.InstallConfig(ctx, id)
		})
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(previousConfig, next.config) {
		rollback.snapshot = snapshot
		rollback.replaced = true
		rollback.authorized = true
	}
	stage = "install_selection"
	if err := j.InstallSelection(ctx, id); err != nil {
		return err
	}
	planPrefs.reservePreferences = newPrefs
	planPrefs.Revision = reservePreferencesRevision(nextBytes, true)
	next.revision = planPrefs.Revision
	next.outsideRoster = !reserveRosterContains(planPrefs, selected)
	stage = "verify_selection"
	if !inputs() || !s.reserveCoreMatches(identity) || p.RuntimeSnapshot() != snapshot {
		return errReserveRuntime
	}
	if description != nextDescription {
		stage = "sync_interface"
		if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
			return err
		}
	}
	stage = "confirm_runtime"
	confirmTimeout := 10 * time.Second
	if repairDiagnostic {
		// Slower routers may need a complete second readback after the
		// managed proxy converges following Xray's restart.
		confirmTimeout = 18 * time.Second
	}
	return j.Confirm(ctx, id, func(ctx context.Context, r reserve.TransactionRecord) error {
		ctx, cancel := context.WithTimeout(ctx, confirmTimeout)
		defer cancel()
		if r.ID != id || r.Intent.Action != action {
			return errReserveRuntime
		}
		for {
			if !inputs() || s.d.App.Xray != p || p.RuntimeSnapshot() != snapshot || !s.reserveCoreMatches(identity) {
				return errReserveRuntime
			}
			binding, ok := s.verifySelectedRuntime(ctx, p, snapshot, next, before, nextDescription, newPrefs.Enabled)
			if ok && inputs() && s.reserveCoreMatches(identity) {
				if !s.confirmReserveHotTraffic(ctx, binding) || !inputs() || p.RuntimeSnapshot() != snapshot {
					return errReserveRuntime
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

func (s *svc) verifySelectedRuntime(ctx context.Context, process *xray.Process, snapshot xray.RuntimeSnapshot, plan reserveRuntimePlan, before compat.OwnedInterfaceTarget, description string, enabled bool) (reserveActiveBinding, bool) {
	if enabled {
		binding, err := s.bindReserveRuntime(ctx, plan, nil)
		if err != nil || binding.snapshot != snapshot {
			return reserveActiveBinding{}, false
		}
		proof, err := s.verifyReserveCommit(ctx, binding, description)
		return binding, err == nil && proof.Interface.Name == before.Name && proof.Interface.Upstream == before.Upstream
	}
	current := func() bool {
		return process.RuntimeSnapshot() == snapshot && s.reservePlanInputs(ctx, plan, false, true)
	}
	identity, err := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
	if err != nil || !current() {
		return reserveActiveBinding{}, false
	}
	listener, err := reserve.VerifyListener(identity, before.Upstream)
	if err != nil {
		return reserveActiveBinding{}, false
	}
	target, err := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, description)
	if err != nil || target.Name != before.Name || target.Upstream != before.Upstream || !current() {
		return reserveActiveBinding{}, false
	}
	after, err := reserve.VerifyListener(identity, before.Upstream)
	return reserveActiveBinding{}, err == nil && after == listener && current()
}

func verifiedReserveSelection(sample reserve.Sample, plan reserveRuntimePlan) bool {
	age := time.Since(sample.FinishedAt)
	return sample.Verified && sample.Verdict == "healthy" && sample.Identity != "" &&
		sample.ServerID == plan.node.ID && sample.Fingerprint == plan.node.Fingerprint &&
		age >= 0 && age <= reserve.DefaultPolicy().ReadinessTTL
}

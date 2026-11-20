package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

// Enable starts from an owned ordinary runtime. Reapplying an enabled setting
// can restore a missing diagnostic inbound without changing the active server.
func (s *svc) enableReserve(ctx context.Context, revision string) (retErr error) {
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
	selected := strings.TrimSpace(string(active))
	endpoint, hasEndpoint := reserve.DiagnosticEndpoint(previousConfig)
	repairing := prefs.Enabled
	if repairing && (hasEndpoint || prefs.Emergency != nil || !reserveRosterContains(prefs, selected)) {
		return errReserveRuntime
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
	if !repairing {
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
	for _, id := range append([]string{prefs.PrimaryServerID}, prefs.BackupServerIDs...) {
		plan, err := s.prepareReserveRuntime(ctx, id, endpoint)
		if err != nil || plan.revision != revision {
			return errReserveRuntime
		}
		plans[id] = plan
	}
	if !reserveRosterContains(prefs, selected) {
		selected = prefs.PrimaryServerID
	}
	next := plans[selected]
	if !reservePrimaryMatches(next.config, before.Upstream) {
		return errReserveRuntime
	}
	if repairing {
		expected, err := reserve.AttachHealthInbound(previousConfig, endpoint)
		if err != nil || !bytes.Equal(expected, next.config) {
			return errReserveRuntime
		}
	}
	inputs := func() bool {
		for _, plan := range plans {
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
		return inputs() && sameFile(RecordPath(s.d, strings.TrimSpace(string(active))), previousRecord, 1<<20) && sameFile(s.d.Root.XrayConfig(), previousConfig, 2<<20) && sameFile(ActiveFile(s.d), active, 4096) && sameFile(s.d.Path("config", "interface.json"), receipt, 64<<10) && s.reserveCoreMatches(identity)
	}
	current := func() bool { return s.d.App.Xray == p && p.RuntimeSnapshot() == snapshot && unchangedFiles() }
	if !current() {
		return errReserveRuntime
	}
	if err := s.d.Svc.ValidateXrayConfig(ctx, next.config); err != nil {
		return err
	}
	afterTarget, err := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, description)
	if err != nil || afterTarget != before || !current() {
		return errReserveRuntime
	}
	afterListener, err := reserve.VerifyListener(identity, before.Upstream)
	if err != nil || afterListener != listener {
		return errReserveRuntime
	}
	nextBytes, nextActive := prefBytes, active
	if !repairing {
		nextPrefs := prefs.reservePreferences
		nextPrefs.Enabled = true
		nextBytes, err = json.Marshal(nextPrefs)
		if err != nil {
			return err
		}
		nextActive = []byte(selected + "\n")
	}
	j, err := reserve.OpenTransactionJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	id, err := j.Begin(ctx, reserve.TransactionFiles{Config: next.config, Preferences: nextBytes, Active: nextActive}, reserve.TransactionIntent{
		Action: "enable", Authority: compat.SHA256Hex([]byte(revision + compat.SHA256Hex(previousConfig))), InterfaceName: before.Name, Upstream: before.Upstream.String(), PreviousDescription: description, PreviousDescriptionLine: before.DescriptionLine, NextDescription: "BROray - mix-auto", ReceiptSHA256: compat.SHA256Hex(receipt),
	})
	var rollback reserveEnableRollback
	if id != "" {
		defer func() {
			if retErr == nil {
				return
			}
			if rollback.authorized {
				rollbackErr := s.rollbackReserveEnable(ctx, j, rollback)
				if rollbackErr == nil {
					retErr = errors.Join(errReserveModeRolledBack, retErr)
					return
				}
				retErr = errors.Join(retErr, rollbackErr)
			}
			retErr = errors.Join(errReserveTransactionPending, retErr)
		}()
	}
	if err != nil {
		return err
	}
	r, err := j.Read(ctx)
	if err != nil || !bytes.Equal(r.Previous.Config.Data, previousConfig) || !bytes.Equal(r.Previous.Active.Data, active) || !bytes.Equal(r.Previous.Preferences.Data, prefBytes) || !current() {
		return errReserveRuntime
	}
	rollback = reserveEnableRollback{record: r, process: p, snapshot: snapshot, identity: identity, plans: plans, previousRecord: previousRecord, authorized: true}
	if reserved != nil {
		if err := reserved.Close(); err != nil {
			return err
		}
	}
	if !bytes.Equal(previousConfig, next.config) {
		rollback.authorized = false // Failed replacement supplies no rollback authority.
		snapshot, err = p.ReplaceIfCurrent(ctx, snapshot, func() error {
			if !unchangedFiles() {
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
	if err := j.InstallSelection(ctx, id); err != nil {
		return err
	}
	for key, plan := range plans {
		plan.revision = reservePreferencesRevision(nextBytes, true)
		plans[key] = plan
	}
	next = plans[selected]
	if !inputs() || !s.reserveCoreMatches(identity) || p.RuntimeSnapshot() != snapshot {
		return errReserveRuntime
	}
	if !repairing {
		if err := s.d.Svc.SyncOwnedInterfaceDescription(ctx); err != nil {
			return err
		}
	}
	return j.Confirm(ctx, id, func(ctx context.Context, r reserve.TransactionRecord) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if r.ID != id || r.Intent.Action != "enable" {
			return errReserveRuntime
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if !inputs() || s.d.App.Xray != p || p.RuntimeSnapshot() != snapshot || !s.reserveCoreMatches(identity) {
				return errReserveRuntime
			}
			binding, e := s.bindReserveRuntime(ctx, next, nil)
			if e == nil && binding.snapshot == snapshot {
				proof, e := s.verifyReserveCommit(ctx, binding, "BROray - mix-auto")
				if e == nil && proof.Interface.Name == before.Name && proof.Interface.Upstream == before.Upstream && inputs() && s.reserveCoreMatches(identity) {
					return nil
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

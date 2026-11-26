package servers

import (
	"context"
	"errors"
	"os"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

// PrepareSourceRefreshCatalog runs before any old profile is overwritten.
// changedIDs are existing members proved by the caller's complete snapshot;
// absent live records are allowed only for its proved disabled, unselected nodes.
// This phase never prunes the roster, selects a server or writes catalog records.
func (s *svc) PrepareSourceRefreshCatalog(ctx context.Context, sourceID string, changedIDs []string) error {
	if !s.reserveRuntimeFence(ctx) || sourceID == "" || len(sourceID) > 256 {
		return errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return err
	}
	active, err := s.sourceRemovalActive()
	if err != nil {
		return err
	}
	affected := false
	seen := map[string]bool{}
	for _, id := range changedIDs {
		if !ValidID(id) || seen[id] {
			return errReserveRuntime
		}
		seen[id] = true
		raw, e := reserveRuntimeFile(RecordPath(s.d, id), 1<<20)
		if errors.Is(e, os.ErrNotExist) && id != active && !reserveRosterContains(prefs, id) {
			continue
		}
		if e != nil {
			return errReserveRuntime
		}
		value, e := strictjson.Decode(raw)
		rec, ok := value.(map[string]any)
		if e != nil || !ok || rec["id"] != id {
			return errReserveRuntime
		}
		source, ok := rec["source"].(map[string]any)
		if !ok || source["type"] != "subscription" || source["subscriptionId"] != sourceID {
			return errReserveRuntime
		}
		affected = affected || id == active || reserveRosterContains(prefs, id)
	}
	if !prefs.Enabled || !affected {
		return nil
	}
	if err := s.disableReserveForSource(ctx, prefs); err != nil {
		return err
	}
	s.resetReserveMonitor()
	return nil
}

// ReapplySourceRefresh updates a retained active profile without changing its
// selected ID or overriding stopped intent. previousName comes from the caller's
// held old catalog snapshot, before its new record is staged.
func (s *svc) ReapplySourceRefresh(ctx context.Context, sourceID, id, previousName string) (string, error) {
	return s.applyRetainedSourceRefresh(ctx, sourceID, id, previousName, false)
}

// RenameSourceRefresh accepts only a display change from the fenced catalog
// transaction. Keep the installed configuration and current lifecycle intent.
func (s *svc) RenameSourceRefresh(ctx context.Context, sourceID, id, previousName string) (string, error) {
	return s.applyRetainedSourceRefresh(ctx, sourceID, id, previousName, true)
}

func (s *svc) applyRetainedSourceRefresh(ctx context.Context, sourceID, id, previousName string, renameOnly bool) (string, error) {
	if !s.reserveRuntimeFence(ctx) || !ValidID(id) {
		return "", errReserveRuntime
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return "", err
	}
	if prefs.Enabled {
		if !renameOnly {
			return "", errReserveRuntime
		}
		if err := s.confirmReserveSourceRename(ctx, sourceID, id, prefs.Revision); err != nil {
			return "", err
		}
		return id, nil
	}
	previousDescription, err := s.d.Svc.PreviewOrdinaryDescription(previousName)
	if err != nil {
		return "", err
	}
	if previousDescription == "" || previousDescription == "BROray - mix-auto" {
		return "", errReserveRuntime
	}
	return s.reconcileSourceOwner(ctx, []string{id}, true, renameOnly, func(ctx context.Context) (string, error) {
		if err := s.replaceSourceCandidateDescription(ctx, sourceID, id, id, true, previousDescription, renameOnly, nil); err != nil {
			return "", err
		}
		return id, nil
	})
}

// An enabled reserve only accepts catalog changes that still describe the
// exact installed runtime. A rename needs no change to the shared interface
// description or to Xray's existing outbound selection.
func (s *svc) confirmReserveSourceRename(ctx context.Context, sourceID, id, revision string) error {
	if sourceID == "" || len(sourceID) > 256 || s.d.App == nil || s.d.App.Xray == nil {
		return errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errors.Join(errReserveTransactionPending, err)
	}
	record, err := reserveRuntimeFile(RecordPath(s.d, id), 1<<20)
	if err != nil {
		return errReserveRuntime
	}
	value, err := strictjson.Decode(record)
	profile, ok := value.(map[string]any)
	if err != nil || !ok || profile["id"] != id {
		return errReserveRuntime
	}
	source, ok := profile["source"].(map[string]any)
	if !ok || source["type"] != "subscription" || source["subscriptionId"] != sourceID {
		return errReserveRuntime
	}
	cfg, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
	if err != nil {
		return errReserveRuntime
	}
	endpoint, ok := reserve.DiagnosticEndpoint(cfg)
	if !ok {
		return errReserveRuntime
	}
	plan, err := s.prepareReserveRuntime(ctx, id, endpoint)
	if err != nil || plan.revision != revision || plan.recordSHA != compat.SHA256Hex(record) || plan.configSHA != compat.SHA256Hex(cfg) {
		return errReserveRuntime
	}
	process := s.d.App.Xray
	snapshot := process.RuntimeSnapshot()
	current := func() bool {
		return s.d.App.Xray == process && process.RuntimeSnapshot() == snapshot && s.reservePlanInputs(ctx, plan, true, true)
	}
	if !current() {
		return errReserveRuntime
	}
	if snapshot.Running {
		binding, err := s.observeReserveRuntime(ctx, plan, nil)
		if err != nil || binding.process != process || binding.snapshot != snapshot {
			return errReserveRuntime
		}
		if _, err := s.verifyReserveCommit(ctx, binding, "BROray - mix-auto"); err != nil || !current() {
			return errReserveRuntime
		}
		return nil
	}

	var cold *reserve.StoppedProfileLease
	if snapshot.ColdStopped() {
		var release func() error
		cold, release, err = s.acquireColdSourceLease(ctx, process, snapshot)
		if err != nil {
			return errReserveRuntime
		}
		defer release()
	} else if snapshot.Wanted || !snapshot.ExitConfirmed || snapshot.PID != 0 || snapshot.LastPID <= 0 ||
		snapshot.Generation == 0 || snapshot.Intent == 0 {
		return errReserveRuntime
	}
	target, err := s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, "BROray - mix-auto")
	if err != nil || target.Name == "" || !reservePrimaryMatches(plan.config, target.Upstream) || !current() ||
		(cold != nil && cold.Check(ctx, s.d.Root) != nil) {
		return errReserveRuntime
	}
	return nil
}

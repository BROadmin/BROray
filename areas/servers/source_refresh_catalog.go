package servers

import (
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
	"os"
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
	if !s.reserveRuntimeFence(ctx) || !ValidID(id) {
		return "", errReserveRuntime
	}
	previousDescription, err := s.d.Svc.PreviewOrdinaryDescription(previousName)
	if err != nil {
		return "", err
	}
	if previousDescription == "" || previousDescription == "BROray - mix-auto" {
		return "", errReserveRuntime
	}
	return s.reconcileSourceOwner(ctx, []string{id}, true, func(ctx context.Context) (string, error) {
		if err := s.replaceSourceCandidateDescription(ctx, sourceID, id, id, true, previousDescription); err != nil {
			return "", err
		}
		return id, nil
	})
}

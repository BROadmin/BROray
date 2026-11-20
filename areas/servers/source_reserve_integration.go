package servers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

// prepareRemovedSource changes only runtime selection and reserve preferences.
// The caller must hold a protected source-removal fence and a complete strict
// catalog snapshot (including disabled source records), preserve the catalog
// on error, reconcile display state and recheck that snapshot before unlink.
func (s *svc) prepareRemovedSource(ctx context.Context, sourceID string, removedIDs, candidates []string) (string, error) {
	return s.prepareSourceChange(ctx, sourceID, removedIDs, candidates, false)
}

func (s *svc) prepareSourceChange(ctx context.Context, sourceID string, removedIDs, candidates []string, refreshed bool) (result string, resultErr error) {
	phase := "change/preconditions"
	defer func() { s.logSourceFailure(phase, resultErr) }()
	if !s.reserveRuntimeFence(ctx) || sourceID == "" || len(sourceID) > 256 {
		return "", errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return "", errors.Join(errReserveTransactionPending, err)
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return "", err
	}
	activeID, err := s.sourceRemovalActive()
	if err != nil {
		return "", err
	}
	removed := map[string]bool{}
	readRecord := func(id string) (map[string]any, error) {
		b, e := reserveRuntimeFile(RecordPath(s.d, id), 1<<20)
		if e != nil {
			return nil, e
		}
		v, e := strictjson.Decode(b)
		m, ok := v.(map[string]any)
		if e != nil || !ok || m["id"] != id {
			return nil, errReserveRuntime
		}
		return m, nil
	}
	belongs := func(m map[string]any) bool {
		source, ok := m["source"].(map[string]any)
		return ok && source["type"] == "subscription" && source["subscriptionId"] == sourceID
	}
	for _, id := range removedIDs {
		if !ValidID(id) {
			return "", errReserveRuntime
		}
		removed[id] = true
		rec, e := readRecord(id)
		// Missing live entries are allowed only because the caller has proved the
		// corresponding disabled source record in its complete deletion snapshot.
		if errors.Is(e, os.ErrNotExist) {
			if id == activeID {
				return "", errReserveRuntime
			}
			continue
		}
		if e != nil || !belongs(rec) {
			return "", errReserveRuntime
		}
	}
	seen := map[string]bool{}
	for _, id := range candidates {
		if !ValidID(id) || removed[id] || seen[id] {
			return "", errReserveRuntime
		}
		seen[id] = true
		rec, e := readRecord(id)
		if e != nil || belongs(rec) != refreshed {
			return "", errReserveRuntime
		}
		if origin, exists := rec["source"]; exists && origin != nil {
			if _, ok := origin.(map[string]any); !ok {
				return "", errReserveRuntime
			}
		}
	}
	affected := removed[activeID]
	for id := range removed {
		affected = affected || reserveRosterContains(prefs, id)
	}
	reset := false
	defer func() {
		if reset {
			s.resetReserveMonitor()
		}
	}()
	phase = "change/disable-reserve"
	if prefs.Enabled && affected {
		if err := s.disableReserveForSource(ctx, prefs); err != nil {
			return "", err
		}
		reset = true
		expected := prefs.reservePreferences
		expected.Enabled = false
		expected.Emergency = nil
		prefs, err = readReservePreferences(s.d)
		if err != nil || !reflect.DeepEqual(prefs.reservePreferences, expected) {
			return "", errReserveRuntime
		}
	}
	phase = "change/selection"
	chosen := activeID
	if removed[activeID] {
		chosen, err = s.selectSourceCandidates(ctx, sourceID, activeID, candidates, refreshed)
		if err != nil {
			return "", err
		}
	}
	// Pruning disabled policy before unlink is resumable: a crash leaves all
	// catalog records present, never an enabled roster pointing at removed nodes.
	phase = "change/prune-roster"
	next := prefs.reservePreferences
	if removed[next.PrimaryServerID] {
		next.PrimaryServerID = ""
	}
	next.BackupServerIDs = make([]string, 0, len(prefs.BackupServerIDs))
	for _, id := range prefs.BackupServerIDs {
		if !removed[id] {
			next.BackupServerIDs = append(next.BackupServerIDs, id)
		}
	}
	if !reflect.DeepEqual(next, prefs.reservePreferences) {
		current, e := readReservePreferences(s.d)
		actual, ae := s.sourceRemovalActive()
		if e != nil || ae != nil || actual != chosen || current.Revision != prefs.Revision || current.Enabled || !s.reserveRuntimeFence(ctx) {
			return "", errReserveRuntime
		}
		data, e := json.Marshal(next)
		if e != nil {
			return "", e
		}
		reset = true
		if e = platform.WriteFileAtomicNoFollow(reservePreferencesFile(s.d), data, 0600); e != nil {
			return "", e
		}
		confirmed, e := readReservePreferences(s.d)
		if e != nil || !confirmed.Exists || !reflect.DeepEqual(confirmed.reservePreferences, next) {
			return "", errReserveRuntime
		}
	}
	phase = "change/confirm"
	actual, err := s.sourceRemovalActive()
	if err != nil || actual != chosen || !s.reserveRuntimeFence(ctx) {
		return "", errReserveRuntime
	}
	return chosen, nil
}

func (s *svc) sourceRemovalActive() (string, error) {
	raw, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(raw))
	if !ValidID(id) {
		return "", errReserveRuntime
	}
	return id, nil
}

func (s *svc) disableReserveForSource(ctx context.Context, prefs reservePreferencesSnapshot) error {
	config, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
	if err != nil {
		return err
	}
	endpoint, ok := reserve.DiagnosticEndpoint(config)
	if !ok {
		return errReserveRuntime
	}
	active, err := s.sourceRemovalActive()
	if err != nil {
		return err
	}
	plan, err := s.prepareReserveRuntime(ctx, active, endpoint)
	if err != nil {
		return err
	}
	if s.d.App == nil || s.d.App.Xray == nil {
		return errReserveRuntime
	}
	if !s.d.App.Xray.RuntimeSnapshot().Running {
		return s.disableReserveStopped(ctx, plan, prefs.Revision)
	}
	binding, err := s.bindReserveRuntime(ctx, plan, nil)
	if err != nil {
		return err
	}
	return s.disableReserve(ctx, binding, prefs.Revision)
}

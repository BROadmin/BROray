package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

var errSourceRemovalState = errors.New("SOURCE_REMOVAL_STATE_UNCONFIRMED")

// PrepareSourceRemoval is the owner service for a protected source deletion.
// It never unlinks catalog records. The caller must retain its complete source
// snapshot on error and verify it again before committing catalog deletion.
func (s *svc) PrepareSourceRemoval(ctx context.Context, sourceID string, removedIDs, candidates []string) (string, error) {
	return s.prepareSourceOwner(ctx, sourceID, removedIDs, candidates, false)
}

// PrepareSourceRefresh uses only candidates from the caller's fully refreshed
// source snapshot. It never disconnects merely because every candidate rejects.
// Old active and candidate records must remain available until this returns.
func (s *svc) PrepareSourceRefresh(ctx context.Context, sourceID string, removedIDs, candidates []string) (string, error) {
	return s.prepareSourceOwner(ctx, sourceID, removedIDs, candidates, true)
}

func (s *svc) prepareSourceOwner(ctx context.Context, sourceID string, removedIDs, candidates []string, refreshed bool) (string, error) {
	return s.reconcileSourceOwner(ctx, removedIDs, refreshed, false, func(ctx context.Context) (string, error) {
		return s.prepareSourceChange(ctx, sourceID, removedIDs, candidates, refreshed)
	})
}

func (s *svc) reconcileSourceOwner(ctx context.Context, affectedIDs []string, refreshed, renameOnly bool, run func(context.Context) (string, error)) (result string, resultErr error) {
	phase := "owner/preconditions"
	defer func() { s.logSourceFailure(phase, resultErr) }()
	if !s.reserveRuntimeFence(ctx) {
		return "", errReserveRuntime
	}
	previous, err := s.sourceRemovalActive()
	if err != nil {
		return "", err
	}
	affected := false
	for _, id := range affectedIDs {
		affected = affected || id == previous
	}
	if !affected {
		return run(ctx)
	}
	if s.d.App == nil || s.d.App.Xray == nil {
		return "", errReserveRuntime
	}
	process := s.d.App.Xray
	before := process.RuntimeSnapshot()
	var cold *reserve.StoppedProfileLease
	if before.ColdStopped() {
		var release func() error
		cold, release, err = s.acquireColdSourceLease(ctx, process, before)
		if err != nil {
			return "", err
		}
		defer release()
		ctx = context.WithValue(ctx, coldSourceKey{}, &coldSourceScope{process, before, cold})
	}
	coldValid := func() bool { return cold == nil || cold.Check(ctx, s.d.Root) == nil }

	autoBytes, policy, err := sourceStateObject(autoSwitchFile(s.d))
	if err != nil {
		return "", err
	}
	enabled := policy["enabled"] == true || policy["enabled"] == "true"
	for _, path := range []string{stateFile(s.d), s.d.RunPath("interface-status.json")} {
		if _, _, err := sourceStateObject(path); err != nil {
			return "", err
		}
	}
	sameAuto := func() bool {
		got, e := reserveRuntimeFile(autoSwitchFile(s.d), 1<<20)
		if autoBytes == nil {
			return errors.Is(e, os.ErrNotExist)
		}
		return e == nil && bytes.Equal(got, autoBytes)
	}
	phase = "owner/execute"
	next, err := run(ctx)
	if err != nil {
		return "", err
	}
	phase = "owner/runtime-reconcile"
	after := process.RuntimeSnapshot()
	retainedRuntime := next == previous && after == before && (renameOnly || (refreshed && before.Running && before.Wanted))
	generation := before.Generation
	if next != "" && before.Wanted && !retainedRuntime {
		generation++
	}
	wanted := next != "" && before.Wanted
	intent := before.Intent + 1
	if retainedRuntime {
		intent = before.Intent
	}
	if cold != nil {
		intent = before.Intent
		if after != before || !coldValid() {
			return "", errSourceRemovalState
		}
	}
	if s.d.App.Xray != process || after.Generation != generation || after.Intent != intent || after.Wanted != wanted || after.Running != wanted {
		return "", errSourceRemovalState
	}
	current := func() bool {
		active, e := s.sourceRemovalActive()
		return coldValid() && e == nil && active == next && s.d.App.Xray == process && process.RuntimeSnapshot() == after && sameAuto() && s.reserveRuntimeFence(ctx)
	}
	if !current() {
		return "", errSourceRemovalState
	}
	if retainedRuntime {
		return next, nil
	}
	phase = "owner/display-state"
	s.mu.Lock()
	defer s.mu.Unlock()
	_, state, err := sourceStateObject(stateFile(s.d))
	if err != nil {
		return "", err
	}
	cacheBytes, _, err := sourceStateObject(s.d.RunPath("interface-status.json"))
	if err != nil {
		return "", err
	}
	state["schemaVersion"], state["enabled"] = 3, enabled
	state["activeHealth"], state["lastProxyContext"], state["consecutiveFailures"] = nil, nil, 0
	state["backgroundOperationId"] = nil
	delete(state, "withdrawal")
	if next == "" {
		state["activeServerId"] = nil
		pending := enabled && before.Wanted
		state["withdrawal"] = map[string]any{"serverId": previous, "pending": pending, "nextAttemptEpoch": 0}
		state["status"], state["lastReason"] = "disabled", "Активный сервер удалён из подписки. Выберите другой сервер."
		if pending {
			state["status"], state["lastReason"] = "waiting-replacement", "Активный сервер удалён из подписки. Автовыбор ожидает замену."
		}
	} else {
		reason := "Выбран другой сервер после удаления источника."
		if refreshed {
			reason = "Выбран сервер из обновлённой подписки."
			if next == previous {
				reason = "Параметры активного сервера обновлены."
			}
		}
		state["activeServerId"], state["status"], state["lastReason"] = next, "switched", reason
		if !wanted {
			if !refreshed {
				reason = "Выбран другой сервер."
			}
			state["status"], state["lastReason"] = "disabled", reason+" Сохранена ручная остановка подключения."
		}
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > 1<<20 {
		return "", errSourceRemovalState
	}
	if !current() {
		return "", errSourceRemovalState
	}
	if cacheBytes != nil {
		if err := platform.RemoveRegularNoFollow(s.d.RunPath("interface-status.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if !current() {
		return "", errSourceRemovalState
	}
	if err := platform.WriteFileAtomicNoFollow(stateFile(s.d), data, 0600); err != nil {
		return "", err
	}
	confirmed, err := reserveRuntimeFile(stateFile(s.d), 1<<20)
	if err != nil || !bytes.Equal(confirmed, data) || !current() {
		return "", errSourceRemovalState
	}
	return next, nil
}

// Missing optional state is distinct from malformed or foreign state. Never
// seed policy or overwrite an unreadable object as a side effect of removal.
func sourceStateObject(path string) ([]byte, map[string]any, error) {
	raw, err := reserveRuntimeFile(path, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return nil, map[string]any{}, nil
	}
	if err != nil {
		return nil, nil, errors.Join(errSourceRemovalState, err)
	}
	value, err := strictjson.Decode(raw)
	object, ok := value.(map[string]any)
	if err != nil || !ok {
		return nil, nil, errSourceRemovalState
	}
	return raw, object, nil
}

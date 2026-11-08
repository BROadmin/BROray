package servers

import (
	"errors"
	"os"
	"sort"

	"github.com/BROadmin/broray/internal/compat"
)

const actionReplacement = "servers:replacement"

type replacementState struct {
	servers  []snapEntry
	policy   autoConfig
	selected map[string]any
}

// This context is invalidated by manual activation/disconnection, policy or
// catalog changes. A replacement must never resurrect a provider-removed node.
func (s *svc) replacementContext() (string, error) {
	if ActiveID(s.d) != "" {
		return "", errors.New("active server already selected")
	}
	ac, ok := s.readAutoConfig()
	if !ok || !ac.enabled {
		return "", errors.New("auto-switch disabled")
	}
	st, ok := s.readState()
	if !ok || get(st, "withdrawal", "pending") != true {
		return "", errors.New("no withdrawal")
	}
	snapshot, err := s.qualitySnapshot("all")
	if err != nil {
		return "", err
	}
	return compat.CanonicalSHA256(map[string]any{"policy": ac.hash, "servers": snapshot, "removed": get(st, "withdrawal", "serverId")})
}

func (s *svc) enqueueReplacement(state map[string]any) error {
	if s.d.Lock.Busy() || get(state, "withdrawal", "pending") != true {
		return nil
	}
	if ep, ok := num(get(state, "withdrawal", "nextAttemptEpoch")); ok && ep > float64(s.now().Unix()) {
		return nil
	}
	ctx, err := s.replacementContext()
	if err != nil {
		return nil
	}
	_, err = s.d.Queue.Enqueue(actionReplacement, "all", "AUTO_SWITCH", ctx)
	return err
}

func (s *svc) replacementVerify(t *compat.Task) error {
	if c, err := s.replacementContext(); err != nil || c != t.Context {
		return changed(t)
	}
	ac, _ := s.readAutoConfig()
	snapshot, err := s.qualitySnapshot("all")
	if err != nil {
		return changed(t)
	}
	preferred := asString(ac.raw["preferredServerId"])
	if ac.raw["selectionRule"] == "preferred" {
		sort.SliceStable(snapshot, func(i, j int) bool { return snapshot[i].ID == preferred && snapshot[j].ID != preferred })
	}
	t.Set("replacement", &replacementState{servers: snapshot, policy: ac})
	return s.updateState(func(st map[string]any) bool {
		w, ok := st["withdrawal"].(map[string]any)
		if !ok {
			return false
		}
		w["nextAttemptEpoch"] = s.now().Unix() + 60*bounded(ac.raw["cooldownMinutes"], 10, 1, 120)
		st["status"], st["lastReason"] = "probing", "Сервер удалён из подписки; проверка замены по настройкам автовыбора."
		return true
	})
}

func (s *svc) replacementProbe(t *compat.Task) error {
	f, ok := t.Get("replacement").(*replacementState)
	if !ok {
		return stateInvalid(t)
	}
	rule, preferred := asString(f.policy.raw["selectionRule"]), asString(f.policy.raw["preferredServerId"])
	minimum := rankOf(f.policy.raw["minimumRating"])
	if minimum == 0 {
		minimum = 2
	}
	var candidates []map[string]any
	for _, e := range f.servers {
		if t.Ctx.Err() != nil {
			return t.Ctx.Err()
		}
		if c, err := s.replacementContext(); err != nil || c != t.Context {
			return changed(t)
		}
		res, err := s.checkServer(t.Ctx, e.ID, "auto-switch", e.SHA256)
		if err != nil {
			return err
		}
		q, _ := res.Object["quality"].(map[string]any)
		metric := func(v any) float64 {
			if n, ok := num(v); ok && n >= 0 {
				return n
			}
			return 999999
		}
		if rank := rankOf(q["rating"]); res.Success && rank >= minimum {
			candidates = append(candidates, map[string]any{"id": e.ID, "sha256": e.SHA256, "ratingRank": rank, "ping": metric(q["ping"]), "jitter": metric(q["jitter"])})
			if rule == "preferred" && e.ID == preferred {
				break
			}
		}
	}
	f.selected = chooseCandidate(candidates, rule, preferred)
	return nil
}

func (s *svc) replacementActivate(t *compat.Task) error {
	if c, err := s.replacementContext(); err != nil || c != t.Context {
		return changed(t)
	}
	f, ok := t.Get("replacement").(*replacementState)
	if !ok {
		return stateInvalid(t)
	}
	if f.selected == nil {
		return s.updateState(func(st map[string]any) bool {
			st["status"], st["lastReason"] = "no-candidates", "Сервер удалён из подписки. Подходящей замены нет; повтор после защитного интервала."
			return true
		})
	}
	id := asString(f.selected["id"])
	rec, err := ReadRecord(s.d, id)
	if err != nil {
		return changed(t)
	}
	cfg, err := XrayConfig(rec, s.genSettings(t.Ctx), RecordPath(s.d, id))
	if err != nil {
		return err
	}
	if err = s.testBytes(t.Ctx, cfg); err != nil {
		return err
	}
	if err = s.applyServer(t.Ctx, id, cfg); err != nil {
		// There is no previous permitted connection to roll back to. The Xray
		// area's apply rollback could restart its old config: stop it explicitly.
		if stopErr := s.stopXray(t.Ctx); stopErr != nil {
			return errors.New("REPLACEMENT_STOP_FAILED")
		}
		if removeErr := os.Remove(ActiveFile(s.d)); removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
		_ = s.updateState(func(st map[string]any) bool {
			st["status"], st["lastReason"] = "no-candidates", "Замена не запущена. VPN отключён; повтор после защитного интервала."
			return true
		})
		return err
	}
	return s.updateState(func(st map[string]any) bool {
		st["withdrawal"] = nil
		st["status"], st["lastReason"] = "switched", "Удалённый сервер заменён по настройкам автовыбора; ожидается проверка подключения."
		st["activeServerId"], st["activeHealth"], st["lastProxyContext"], st["consecutiveFailures"] = id, nil, nil, 0
		st["lastSwitchEpoch"] = s.now().Unix()
		return true
	})
}

// Used by an explicit user disconnect; leaving an automatic replacement intent
// here would cause a later re-enable to connect without a new withdrawal.
func (s *svc) clearWithdrawal() error {
	st, ok := s.readState()
	if !ok || st["withdrawal"] == nil {
		return nil
	}
	return s.updateState(func(st map[string]any) bool { delete(st, "withdrawal"); return true })
}

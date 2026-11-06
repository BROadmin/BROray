package servers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

var (
	jsonN0  any = int64(0)
	jsonN3  any = int64(3)
	jsonN10 any = int64(10)
)

const (
	actionHealth   = "servers:active-health"
	actionQuality  = "servers:quality"
	actionFailover = "servers:failover"
)

// stepError fails a queued request with a user-visible message and a code.
func stepError(t *compat.Task, code, message string) error {
	t.Result(message, code)
	return errors.New(code + ": " + message)
}

func changed(t *compat.Task) error {
	return stepError(t, "SERVER_CATALOG_CHANGED", "Состояние серверов изменилось во время задания. Повторите проверку.")
}

func stateInvalid(t *compat.Task) error {
	return stepError(t, "STATE_UNAVAILABLE", "Состояние автовыбора сервера недоступно.")
}

// ---------- auto-switch state publications (operation-publication.sh) ----------

func (s *svc) readState() (map[string]any, bool) {
	if _, err := os.Lstat(stateFile(s.d)); os.IsNotExist(err) {
		return map[string]any{}, true
	}
	return readObjectFile(stateFile(s.d))
}

func (s *svc) updateState(fn func(st map[string]any) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.readState()
	if !ok {
		st = map[string]any{}
	}
	if !fn(st) {
		return errors.New("state publication refused")
	}
	if err := os.MkdirAll(s.d.Root.Run(), 0o700); err != nil {
		return err
	}
	return compat.WriteJSON(stateFile(s.d), st)
}

// ---------- quality snapshot / context (server-check-job.sh:79-135) ----------

type snapEntry struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

var errSnapshotNotFound = errors.New("server not found")

func (s *svc) qualitySnapshot(target string) ([]snapEntry, error) {
	if target != "all" {
		if !ValidID(target) || target == "." || target == ".." {
			return nil, errors.New("invalid target")
		}
		if st, err := os.Lstat(RecordPath(s.d, target)); err != nil || !st.Mode().IsRegular() {
			return nil, errSnapshotNotFound
		}
	}
	ids, err := RecordIDs(s.d)
	if err != nil {
		return nil, err
	}
	out := []snapEntry{}
	for _, id := range ids {
		if target != "all" && target != id {
			continue
		}
		if !ValidID(id) || id == "." || id == ".." {
			return nil, errors.New("invalid server file name")
		}
		st, err := os.Lstat(RecordPath(s.d, id))
		if err != nil || !st.Mode().IsRegular() {
			return nil, errors.New("server file is not regular")
		}
		h, err := fileSHA256(RecordPath(s.d, id))
		if err != nil {
			return nil, err
		}
		out = append(out, snapEntry{id, h})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > 4096 {
		return nil, errors.New("too many servers")
	}
	return out, nil
}

// qualityContext is broray_quality_context <target> <USER|SERVER_CHECK_AUTO>.
func (s *svc) qualityContext(target, origin string) (string, error) {
	snap, err := s.qualitySnapshot(target)
	if err != nil {
		return "", err
	}
	cfgHash := ""
	if origin == "SERVER_CHECK_AUTO" {
		if _, ok := readObjectFile(autoSwitchFile(s.d)); !ok {
			return "", errors.New("auto-switch config unavailable")
		}
		if cfgHash, err = fileSHA256(autoSwitchFile(s.d)); err != nil {
			return "", err
		}
	}
	servers := make([]any, len(snap))
	for i, e := range snap {
		servers[i] = map[string]any{"id": e.ID, "sha256": e.SHA256}
	}
	return compat.CanonicalSHA256(map[string]any{"servers": servers, "autoConfigSha256": cfgHash})
}

// resolveCheck is the servers-check routing of api/operations/common.sh:47-59.
func (s *svc) resolveCheck(id string) (string, string, *cgi.APIError) {
	if id != "all" {
		if st, err := os.Lstat(RecordPath(s.d, id)); err != nil || !st.Mode().IsRegular() {
			return "", "", cgi.E(404, "SERVER_NOT_FOUND", "Сервер не найден.")
		}
		if ctx := s.runtimeContext(id); ctx != "" {
			return actionHealth, ctx, nil
		}
	}
	ctx, err := s.qualityContext(id, "USER")
	if err != nil {
		return "", "", cgi.E(503, "STATE_UNAVAILABLE", "Не удалось подтвердить список серверов.")
	}
	return actionQuality, ctx, nil
}

func (s *svc) defineActions() {
	s.d.Queue.Define(compat.ActionSpec{Name: actionHealth, Type: "server_operation", Priority: map[string]int{"*": 0},
		Stages: []compat.Stage{{Name: "checking", Phase: "checking", Run: s.healthStep}}})
	s.d.Queue.Define(compat.ActionSpec{Name: actionQuality, Type: "server_operation",
		Priority: map[string]int{"USER": 2, "SERVER_CHECK_AUTO": 3, "*": 3},
		Stages:   []compat.Stage{{Name: "probe", Phase: "checking", Run: s.qualityStep}}})
	s.d.Queue.Define(compat.ActionSpec{Name: actionFailover, Type: "server_operation", Priority: map[string]int{"*": 1},
		Stages: []compat.Stage{
			{Name: "verify", Phase: "checking", Run: s.failoverVerify},
			{Name: "probe", Phase: "checking", Run: s.failoverProbe},
			{Name: "activate", Phase: "switching", Protected: true, Global: true, Run: s.failoverActivate},
		}})
}

// ---------- servers:quality (broray_quality_step) ----------

func intervalOf(cfg map[string]any) int64 {
	v := jqRaw(alt(cfg["qualityRefreshIntervalMinutes"], json60))
	if in(v, "30", "60", "180", "360") {
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 60
}

func (s *svc) qualityStep(t *compat.Task) error {
	origin := t.Source
	if c, err := s.qualityContext(t.Target, origin); err != nil || c != t.Context {
		return changed(t)
	}
	cfg, _ := readObjectFile(autoSwitchFile(s.d))
	if origin == "SERVER_CHECK_AUTO" && cfg["qualityRefreshEnabled"] != true {
		return changed(t)
	}
	interval := intervalOf(cfg)
	source := map[string]string{"USER": "manual", "SERVER_CHECK_AUTO": "scheduled"}[origin]
	if source == "" {
		return stateInvalid(t)
	}
	snap, err := s.qualitySnapshot(t.Target)
	if err != nil {
		return changed(t)
	}
	runStartedAt := compat.Stamp(s.now())
	total, checked, availableN, unavailableN := len(snap), 0, 0, 0
	publish := func() error {
		status := "running"
		if checked >= total {
			status = "success"
		}
		if c, err := s.qualityContext(t.Target, origin); err != nil || c != t.Context {
			return changed(t)
		}
		now := s.now()
		progress := map[string]any{"status": status, "backgroundOperationId": t.Op.ID, "requestId": t.RequestID,
			"intervalMinutes": interval, "runStartedAt": nil, "lastCompletedAt": nil, "nextCheckEpoch": 0,
			"totalCount": total, "checkedCount": checked, "availableCount": availableN, "unavailableCount": unavailableN,
			"errorCount": 0, "lastError": nil, "lastResult": "Проверено: " + strconv.Itoa(checked) + " из " + strconv.Itoa(total)}
		if status == "running" {
			progress["runStartedAt"] = runStartedAt
		} else {
			progress["lastCompletedAt"] = compat.Stamp(now)
			progress["nextCheckEpoch"] = now.Unix() + interval*60
		}
		return s.updateState(func(st map[string]any) bool { st["qualityRefresh"] = progress; return true })
	}
	if total == 0 {
		return publish()
	}
	for _, e := range snap {
		if t.Ctx.Err() != nil {
			return t.Ctx.Err()
		}
		res, err := s.checkServer(t.Ctx, e.ID, source, e.SHA256)
		if err != nil {
			if errors.Is(err, errCatalogChanged) {
				return changed(t)
			}
			if t.Ctx.Err() != nil {
				return t.Ctx.Err()
			}
			return stepError(t, "SERVER_CHECK_FAILED", "Проверка сервера не завершена. Прежняя оценка сохранена.")
		}
		checked++
		if res.Success {
			availableN++
		} else {
			unavailableN++
		}
		if err := publish(); err != nil {
			return err
		}
	}
	return nil
}

// ---------- servers:active-health (broray_auto_health_step) ----------

type autoConfig struct {
	raw       map[string]any
	hash      string
	enabled   bool
	threshold int64
}

// autoSwitchSeed is runtime/state-seed/config/system/server-auto-switch.json
// of 3.x: the installer created it; 3.5.0 creates it on first use.
var autoSwitchSeed = map[string]any{"schemaVersion": 3, "enabled": false, "failureThreshold": 3, "cooldownMinutes": 10,
	"minimumRating": "acceptable", "selectionRule": "best-quality", "preferredServerId": nil,
	"qualityRefreshEnabled": false, "qualityRefreshIntervalMinutes": 60, "updatedAt": nil}

// seedAutoSwitch writes the 3.x seed when the file does not exist (never
// overwrites an existing, even invalid, file).
func (s *svc) seedAutoSwitch() {
	p := autoSwitchFile(s.d)
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = compat.WriteJSON(p, autoSwitchSeed)
}

func (s *svc) readAutoConfig() (autoConfig, bool) {
	s.seedAutoSwitch()
	raw, ok := readObjectFile(autoSwitchFile(s.d))
	if !ok {
		return autoConfig{}, false
	}
	h, err := fileSHA256(autoSwitchFile(s.d))
	if err != nil {
		return autoConfig{}, false
	}
	en := alt(raw["enabled"], false)
	return autoConfig{raw: raw, hash: h, enabled: en == true || en == "true",
		threshold: bounded(alt(raw["failureThreshold"], jsonN3), 3, 1, 10)}, true
}

// bounded is the jq `tostring | digits → tonumber in [lo,hi] else fallback`.
func bounded(v any, fallback, lo, hi int64) int64 {
	t := jqRaw(v)
	if !allDigits(t) {
		return fallback
	}
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil || n < lo || n > hi {
		return fallback
	}
	return n
}

// freshHealth returns the newest current sample among candidates
// (same server, context, method, ≤120 s old, healthy|unhealthy).
func freshHealth(now int64, active, context string, samples ...any) map[string]any {
	var best map[string]any
	bestEpoch, bestHealthy := -1.0, -1
	for _, x := range samples {
		h, ok := x.(map[string]any)
		if !ok {
			continue
		}
		ep, ok := num(h["checkedEpoch"])
		if !ok || h["serverId"] != active || h["context"] != context || h["method"] != "current-socks-https" ||
			ep > float64(now) || float64(now)-ep > 120 || !(h["status"] == "healthy" || h["status"] == "unhealthy") {
			continue
		}
		healthy := 0
		if h["status"] == "healthy" {
			healthy = 1
		}
		if ep > bestEpoch || (ep == bestEpoch && healthy > bestHealthy) {
			best, bestEpoch, bestHealthy = h, ep, healthy
		}
	}
	return best
}

// failoverPending is broray_auto_failover_pending: a bound failover request
// for this failed connection is still queued or running.
func (s *svc) failoverPending(state map[string]any, active, context, hash string) bool {
	h := freshHealth(s.now().Unix(), active, context, state["activeHealth"], get(state, "failover", "activeHealth"))
	if h == nil || state["autoConfigSha256"] != hash || h["status"] != "unhealthy" ||
		get(state, "failover", "serverId") != active || get(state, "failover", "sourceContext") != context ||
		get(state, "failover", "autoConfigSha256") != hash || !in(asString(get(state, "failover", "status")), "probing", "preparing", "prepared") {
		return false
	}
	id, _ := get(state, "failover", "requestId").(string)
	if id == "" {
		return false
	}
	r, ok := s.d.Queue.Request(id)
	return ok && (r.State == "queued" || r.State == "running")
}

func (s *svc) healthStep(t *compat.Task) error {
	active, context := t.Target, t.Context
	if s.runtimeContext(active) != context {
		return changed(t)
	}
	ac, ok := s.readAutoConfig()
	if !ok {
		return stateInvalid(t)
	}
	previous, ok := s.readState()
	if !ok {
		return stateInvalid(t)
	}
	if ac.enabled && t.Source == "AUTO_SWITCH" && s.failoverPending(previous, active, context, ac.hash) {
		return nil
	}
	health, result, hctx := s.activeMeasure(t.Ctx, active)
	if t.Ctx.Err() != nil {
		return t.Ctx.Err()
	}
	if hctx != context {
		return changed(t)
	}
	var count int64
	status, reasonText := "healthy", "Проверка трафика через активный VPN успешна"
	switch {
	case !ac.enabled:
		status, reasonText = "disabled", "Автовыбор выключен"
	case health == "false":
		count = 1
		if previous["lastProxyContext"] == context {
			if f, ok := isInt(previous["consecutiveFailures"]); ok && f >= 0 {
				count = int64(f) + 1
				if count > 2147483647 {
					count = 2147483647
				}
			}
		}
		status = "waiting-threshold"
		reasonText = "Ожидание порога ошибок: " + strconv.FormatInt(count, 10) + " из " + strconv.FormatInt(ac.threshold, 10)
		if count >= ac.threshold {
			reasonText = "Порог отказов достигнут; ожидается проверка резервных серверов"
		}
	case health != "true":
		status, reasonText = "paused", "Не удалось проверить трафик через активный VPN"
	}
	var lastCtx any
	if count > 0 {
		lastCtx = context
	}
	payload := map[string]any{"schemaVersion": 3, "backgroundOperationId": t.Op.ID, "enabled": ac.enabled, "status": status,
		"activeServerId": active, "activeHealth": result, "consecutiveFailures": count, "lastProxyContext": lastCtx,
		"lastEvaluationAt": result["checkedAt"], "lastEvaluationEpoch": result["checkedEpoch"], "lastReason": reasonText,
		"lastError": nil, "autoConfigSha256": ac.hash}
	if err := s.updateState(func(st map[string]any) bool {
		for k, v := range payload {
			st[k] = v
		}
		return true
	}); err != nil {
		return stateInvalid(t)
	}
	if ac.enabled && count >= ac.threshold {
		r, err := s.d.Queue.Enqueue(actionFailover, active, "AUTO_SWITCH", context)
		if err != nil || r.Priority != 1 || !(r.State == "queued" || r.State == "running") {
			return stepError(t, "FAILOVER_UNCONFIRMED", "Не удалось поставить в очередь проверку резервных серверов.")
		}
		s.logf("SERVER_AUTO_SWITCH_FAILOVER=QUEUED SERVER=%s REQUEST=%s", active, r.RequestID)
	}
	// 3.5.0 (DEV5_ROUTER_ACTIVE_SERVER_CHECK_NO_QUALITY): the owner's
	// «Проверить» on the active server also measures its quality, so the card
	// gets ping/jitter/rating like any other server. The 15-s AUTO_SWITCH
	// health loop stays health-only.
	if t.Source == "USER" {
		if _, err := s.checkServer(t.Ctx, active, "manual", ""); err != nil {
			if t.Ctx.Err() != nil {
				return t.Ctx.Err()
			}
			// Traffic health was already checked and saved above. Quality is an
			// additional measurement and must not turn that result into failure.
			s.logf("SERVER_ACTIVE_QUALITY=FAIL SERVER=%s ERROR=%v", active, err)
		}
	}
	return nil
}

// ---------- servers:failover (broray_auto_failover_step) ----------

type failState struct {
	Context, Active, Hash string
	Status                string
	Policy                map[string]any
	Servers               []snapEntry
	Cursor                int
	Candidates            []map[string]any
	ActiveHealth          map[string]any
	AttemptEpoch          any
	Selected              map[string]any
	PrevRuntime, NewBytes []byte
	Prepared              map[string]any
}

func (s *svc) failoverProgress(t *compat.Task, f *failState) error {
	now := s.now()
	var sel any
	if f.Selected != nil {
		sel = f.Selected["id"]
	}
	progress := map[string]any{"schemaVersion": 1, "requestId": t.RequestID, "operationId": t.Op.ID, "sourceContext": f.Context,
		"serverId": f.Active, "autoConfigSha256": f.Hash, "activeHealth": f.ActiveHealth, "status": f.Status,
		"checkedCount": f.Cursor, "totalCount": len(f.Servers), "candidateCount": len(f.Candidates), "selectedServerId": sel,
		"lastAttemptEpoch": f.AttemptEpoch, "updatedAt": compat.StampUTC(now), "updatedEpoch": now.Unix()}
	return s.updateState(func(st map[string]any) bool { st["failover"] = progress; return true })
}

func rankOf(r any) int {
	switch r {
	case "excellent":
		return 4
	case "good":
		return 3
	case "acceptable":
		return 2
	case "poor":
		return 1
	}
	return 0
}

func (s *svc) failoverVerify(t *compat.Task) error {
	active, context := t.Target, t.Context
	if s.runtimeContext(active) != context {
		return changed(t)
	}
	ac, ok := s.readAutoConfig()
	if !ok {
		return stateInvalid(t)
	}
	st, ok := s.readState()
	if !ok {
		return changed(t)
	}
	now := s.now().Unix()
	c := ac.raw
	threshold := ac.threshold
	cooldown := bounded(alt(c["cooldownMinutes"], jsonN10), 10, 1, 120)
	cf, okF := isInt(st["consecutiveFailures"])
	if !((c["enabled"] == true || c["enabled"] == "true") && st["enabled"] == true && st["autoConfigSha256"] == ac.hash &&
		st["activeServerId"] == active && st["lastProxyContext"] == context && get(st, "activeHealth", "context") == context &&
		get(st, "activeHealth", "status") == "unhealthy" && okF && cf >= float64(threshold)) {
		return changed(t)
	}
	attempt := alt(get(st, "failover", "lastAttemptEpoch"), st["lastAttemptEpoch"], jsonN0)
	switched := alt(st["lastSwitchEpoch"], jsonN0)
	a, okA := num(attempt)
	sw, okS := num(switched)
	if !okA || !okS || a < 0 || a > float64(now) || sw < 0 || sw > float64(now) {
		return changed(t)
	}
	minRank := 2
	if r := rankOf(alt(c["minimumRating"], "acceptable")); r > 0 {
		minRank = r
	}
	rule := "best-quality"
	if r, _ := c["selectionRule"].(string); in(r, "best-quality", "lowest-ping", "preferred") {
		rule = r
	}
	preferred := jqRaw(alt(c["preferredServerId"], ""))
	policy := map[string]any{"failureThreshold": threshold, "cooldownMinutes": cooldown, "lastAttemptEpoch": attempt,
		"attemptGuard": a > 0 && float64(now)-a < 60, "cooldown": sw > 0 && float64(now)-sw < float64(cooldown*60),
		"minimumRank": minRank, "rule": rule, "preferredServerId": preferred}
	if policy["attemptGuard"] == true || policy["cooldown"] == true {
		t.Set("done", true)
		return nil
	}
	health, result, hctx := s.activeMeasure(t.Ctx, active)
	if t.Ctx.Err() != nil {
		return t.Ctx.Err()
	}
	if h, _ := fileSHA256(autoSwitchFile(s.d)); hctx != context || h != ac.hash {
		return changed(t)
	}
	var status string
	switch health {
	case "true":
		status = "recovered"
	case "false":
		status = "probing"
	default:
		return changed(t)
	}
	f := &failState{Context: context, Active: active, Hash: ac.hash, Policy: policy, ActiveHealth: result, Candidates: []map[string]any{}}
	if status == "probing" {
		snap, err := s.qualitySnapshot("all")
		if err != nil {
			return changed(t)
		}
		for _, e := range snap {
			if e.ID != active {
				f.Servers = append(f.Servers, e)
			}
		}
		if rule == "preferred" && preferred != "" {
			sort.SliceStable(f.Servers, func(i, j int) bool {
				return f.Servers[i].ID == preferred && f.Servers[j].ID != preferred
			})
		}
		f.AttemptEpoch = s.now().Unix()
		if len(f.Servers) == 0 {
			status = "no-candidates"
		}
	} else {
		f.AttemptEpoch = attempt
	}
	f.Status = status
	if err := s.failoverProgress(t, f); err != nil {
		return stateInvalid(t)
	}
	s.logf("SERVER_AUTO_SWITCH_FAILOVER=%s SERVER=%s CANDIDATES=%d", status, active, len(f.Servers))
	if status != "probing" {
		t.Set("done", true)
		return nil
	}
	t.Set("failover", f)
	return nil
}

// currentFailoverHealth is the newest current sample (state + cache) or nil.
func (s *svc) currentFailoverHealth(f *failState) map[string]any {
	cache, _ := s.readState()
	return freshHealth(s.now().Unix(), f.Active, f.Context, any(f.ActiveHealth), cache["activeHealth"])
}

func (s *svc) failoverProbe(t *compat.Task) error {
	if t.Get("done") == true {
		return nil
	}
	f, _ := t.Get("failover").(*failState)
	if f == nil {
		return stateInvalid(t)
	}
	if s.runtimeContext(f.Active) != f.Context {
		return changed(t)
	}
	if h, _ := fileSHA256(autoSwitchFile(s.d)); h != f.Hash {
		return changed(t)
	}
	preferred := asString(f.Policy["preferredServerId"])
	rule := asString(f.Policy["rule"])
	minRank := f.Policy["minimumRank"].(int)
	for f.Status == "probing" {
		if t.Ctx.Err() != nil {
			return t.Ctx.Err()
		}
		h := s.currentFailoverHealth(f)
		if h == nil {
			return changed(t)
		}
		if h["status"] != "unhealthy" {
			t.Set("done", true)
			return nil
		}
		e := f.Servers[f.Cursor]
		res, err := s.checkServer(t.Ctx, e.ID, "auto-switch", e.SHA256)
		if err != nil {
			if errors.Is(err, errCatalogChanged) {
				return changed(t)
			}
			if t.Ctx.Err() != nil {
				return t.Ctx.Err()
			}
			return stepError(t, "SERVER_CHECK_FAILED", "Проверка резервного сервера не завершена.")
		}
		q, _ := res.Object["quality"].(map[string]any)
		metric := func(v any) float64 {
			if x, ok := isInt(v); ok && x >= 0 {
				return x
			}
			return 999999
		}
		if rank := rankOf(q["rating"]); res.Success && rank >= minRank {
			f.Candidates = append(f.Candidates, map[string]any{"id": e.ID, "sha256": e.SHA256, "ratingRank": rank,
				"ping": metric(q["ping"]), "jitter": metric(q["jitter"])})
		}
		f.Cursor++
		f.ActiveHealth = h
		preferredQualified := false
		for _, c := range f.Candidates {
			if rule == "preferred" && c["id"] == preferred {
				preferredQualified = true
			}
		}
		if f.Cursor == len(f.Servers) || preferredQualified {
			f.Selected = chooseCandidate(f.Candidates, rule, preferred)
			if f.Selected == nil {
				f.Status = "no-candidates"
			} else {
				f.Status = "preparing"
			}
		}
		if err := s.failoverProgress(t, f); err != nil {
			return stateInvalid(t)
		}
	}
	if f.Status == "no-candidates" {
		s.logf("SERVER_AUTO_SWITCH_FAILOVER=no-candidates SERVER=%s", f.Active)
		t.Set("done", true)
		return nil
	}
	// preparing (broray_auto_failover_step, f_status=preparing)
	h := s.currentFailoverHealth(f)
	if h == nil {
		return changed(t)
	}
	if h["status"] != "unhealthy" {
		t.Set("done", true)
		return nil
	}
	id := asString(f.Selected["id"])
	if hh, err := fileSHA256(RecordPath(s.d, id)); err != nil || hh != f.Selected["sha256"] {
		return changed(t)
	}
	prev, err := os.ReadFile(s.d.Root.XrayConfig())
	if err != nil {
		return changed(t)
	}
	settingsHash, _ := fileSHA256(settingsFile(s.d))
	rec, err := ReadRecord(s.d, id)
	if err != nil {
		return changed(t)
	}
	cfg, err := XrayConfig(rec, s.genSettings(t.Ctx), RecordPath(s.d, id))
	if err != nil {
		return stepError(t, "SERVER_PREPARE_FAILED", "Конфигурация резервного сервера не сформирована.")
	}
	if err := s.testBytes(t.Ctx, cfg); err != nil {
		return stepError(t, "SERVER_PREPARE_FAILED", "Xray отклонил конфигурацию резервного сервера.")
	}
	if hh, _ := fileSHA256(RecordPath(s.d, id)); s.runtimeContext(f.Active) != f.Context || hh != f.Selected["sha256"] {
		return changed(t)
	}
	health, result, hctx := s.activeMeasure(t.Ctx, f.Active)
	if t.Ctx.Err() != nil {
		return t.Ctx.Err()
	}
	if ah, _ := fileSHA256(autoSwitchFile(s.d)); hctx != f.Context || ah != f.Hash {
		return changed(t)
	}
	switch health {
	case "false":
		f.Status = "prepared"
	case "true":
		f.Status = "recovered"
	default:
		return changed(t)
	}
	f.ActiveHealth = result
	f.PrevRuntime, f.NewBytes = prev, cfg
	f.Prepared = map[string]any{"previousSha256": compat.SHA256Hex(prev), "newSha256": compat.SHA256Hex(cfg),
		"systemSettingsSha256": settingsHash}
	if err := s.failoverProgress(t, f); err != nil {
		return stateInvalid(t)
	}
	if f.Status != "prepared" {
		t.Set("done", true)
	}
	return nil
}

func chooseCandidate(cands []map[string]any, rule, preferred string) map[string]any {
	if rule == "preferred" {
		for _, c := range cands {
			if c["id"] == preferred {
				return c
			}
		}
	}
	if len(cands) == 0 {
		return nil
	}
	sorted := append([]map[string]any(nil), cands...)
	key := func(c map[string]any) []float64 {
		r := float64(c["ratingRank"].(int))
		if rule == "lowest-ping" {
			return []float64{c["ping"].(float64), -r, c["jitter"].(float64)}
		}
		return []float64{-r, c["ping"].(float64), c["jitter"].(float64)}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := key(sorted[i]), key(sorted[j])
		for k := range a {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return asString(sorted[i]["id"]) < asString(sorted[j]["id"])
	})
	return sorted[0]
}

func (s *svc) failoverActivate(t *compat.Task) error {
	if t.Get("done") == true {
		return nil
	}
	f, _ := t.Get("failover").(*failState)
	if f == nil || f.Status != "prepared" {
		return nil
	}
	h := s.currentFailoverHealth(f)
	if h == nil {
		return changed(t)
	}
	if h["status"] != "unhealthy" {
		return nil
	}
	id := asString(f.Selected["id"])
	if hh, err := fileSHA256(RecordPath(s.d, id)); err != nil || hh != f.Selected["sha256"] {
		return changed(t)
	}
	if cur, err := os.ReadFile(s.d.Root.XrayConfig()); err != nil || compat.SHA256Hex(cur) != f.Prepared["previousSha256"] {
		return changed(t)
	}
	if sh, _ := fileSHA256(settingsFile(s.d)); sh != f.Prepared["systemSettingsSha256"] {
		return changed(t)
	}
	ctx := t.Ctx
	switched := false
	if err := s.d.Svc.ApplyXrayConfig(ctx, f.NewBytes); err == nil {
		if err := setActiveID(s.d, id); err == nil {
			_ = s.ensureInterface(ctx)
			cur, _ := os.ReadFile(s.d.Root.XrayConfig())
			st := s.settings(ctx)
			switched = s.xrayRunning() && s.socksListening(st.socksAddr, st.socksPort) && compat.SHA256Hex(cur) == f.Prepared["newSha256"]
		}
	}
	status := "switched"
	if !switched {
		// roll back the exact previous runtime
		if err := s.d.Svc.ApplyXrayConfig(ctx, f.PrevRuntime); err != nil {
			s.logf("SERVER_AUTO_SWITCH_ROLLBACK=FAIL SERVER=%s ERROR=%v", f.Active, err)
		}
		_ = setActiveID(s.d, f.Active)
		_ = s.ensureInterface(ctx)
		status = "rolled-back"
	}
	name := id
	if rec, err := ReadRecord(s.d, id); err == nil {
		name = jqRaw(alt(rec["name"], rec["id"]))
	}
	now := s.now()
	err := s.updateState(func(st map[string]any) bool {
		st["schemaVersion"] = 3
		st["backgroundOperationId"] = t.Op.ID
		st["status"] = status
		if status == "switched" {
			st["activeServerId"] = id
		} else {
			st["activeServerId"] = f.Active
		}
		st["activeHealth"], st["lastProxyContext"], st["consecutiveFailures"] = nil, nil, 0
		st["candidateCount"], st["lastAttemptEpoch"] = len(f.Candidates), f.AttemptEpoch
		qr := map[string]any{"status": "idle", "totalCount": 0, "checkedCount": 0, "availableCount": 0, "unavailableCount": 0, "errorCount": 0}
		if old, ok := st["qualityRefresh"].(map[string]any); ok {
			for k, v := range old {
				qr[k] = v
			}
		}
		st["qualityRefresh"] = qr
		fo, ok := st["failover"].(map[string]any)
		if !ok {
			fo = map[string]any{}
		}
		fo["status"], fo["operationId"], fo["updatedAt"], fo["updatedEpoch"] = status, t.Op.ID, compat.StampUTC(now), now.Unix()
		st["failover"] = fo
		if status == "switched" {
			st["lastSwitchEpoch"], st["lastSwitchAt"] = now.Unix(), compat.StampUTC(now)
			st["lastSwitchFrom"], st["lastSwitchTo"], st["lastSwitchName"] = f.Active, id, name
			st["lastReason"], st["lastError"] = "Выбран резервный сервер; ожидается проверка активного соединения", nil
		} else {
			st["lastReason"] = "Переключение не подтверждено; прежняя конфигурация восстановлена"
			st["lastError"] = "FAILOVER_ACTIVATION_ROLLED_BACK"
		}
		return true
	})
	if err != nil {
		return stateInvalid(t)
	}
	s.logf("SERVER_AUTO_SWITCH_FAILOVER=%s FROM=%s TO=%s", status, f.Active, id)
	if status != "switched" {
		return stepError(t, "FAILOVER_ACTIVATION_ROLLED_BACK", "Переключение не подтверждено; прежняя конфигурация восстановлена.")
	}
	return nil
}

func (s *svc) ensureInterface(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := s.d.Svc.EnsureInterface(cctx); err != nil {
		s.logf("ПРЕДУПРЕЖДЕНИЕ: описание интерфейса не обновлено: %v", err)
		return err
	}
	return nil
}

package dns

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// schedulerInterval is BRORAY_SUB_SCHEDULER_INTERVAL (the 3.x scheduler tick).
var schedulerInterval = 60 * time.Second

// safeFile is broray_dot_auto_safe_file: regular, not a symlink, ≤64 KiB.
func safeFile(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() <= 65536
}

func (s *service) pathsSafe() bool {
	for _, p := range []string{s.d.Path("routes"), s.dotDir(), s.configPath(), s.statePath(), s.autoPath()} {
		if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

// autoEnabled returns (enabled, valid).
func (s *service) autoEnabled() (bool, bool) {
	if !s.pathsSafe() {
		return false, false
	}
	if !exists(s.autoPath()) {
		return false, true
	}
	if !safeFile(s.autoPath()) {
		return false, false
	}
	m, err := readMap(s.autoPath())
	if err != nil || !numIs(m["schemaVersion"], 1) {
		return false, false
	}
	b, ok := m["enabled"].(bool)
	if !ok {
		return false, false
	}
	return b, true
}

// autoIDs is broray_dot_auto_ids: the saved selection, sorted.
func (s *service) autoIDs() ([]string, bool) {
	if !s.pathsSafe() || !safeFile(s.configPath()) {
		return nil, false
	}
	cfg, err := readMap(s.configPath())
	if err != nil || !numIs(cfg["schemaVersion"], 3) {
		return nil, false
	}
	req, ok1 := cfg["requestedIds"].([]any)
	sel, ok2 := cfg["selectedIds"].([]any)
	if !ok1 || !ok2 || !jsonEqual(req, sel) {
		return nil, false
	}
	ids, ok := stringList(req)
	if !ok || len(ids) > maxServers || len(uniqueSorted(ids)) != len(ids) {
		return nil, false
	}
	return sortedIDs(ids), true
}

// autoView is broray_dot_auto_view (DA:41-58).
func (s *service) autoView() map[string]any {
	enabled, valid := s.autoEnabled()
	ids, ok := s.autoIDs()
	if !ok {
		ids = []string{}
	}
	var paused any = false
	if st, err := os.Lstat(s.automationPath()); err == nil {
		paused = nil
		if st.Mode().IsRegular() {
			if m, err := readMap(s.automationPath()); err == nil {
				if b, ok := m["paused"].(bool); ok {
					paused = b
				}
			}
		}
	}
	last := map[string]any{}
	if safeFile(s.statePath()) {
		if st, err := readMap(s.statePath()); err == nil {
			if ac, ok := st["autoCheck"].(map[string]any); ok {
				for _, k := range []string{"lastAttemptEpoch", "finishedEpoch", "status", "errorCode", "selectedIds"} {
					last[k] = ac[k]
				}
			}
		}
	}
	return map[string]any{"schemaVersion": 1, "enabled": enabled, "settingsValid": valid, "paused": paused, "intervalSeconds": autoInterval,
		"savedIds": strSlice(ids), "last": last, "observedEpoch": s.now().Unix()}
}

// autoSave writes auto-check.json {schemaVersion:1,enabled,updatedAt}.
func (s *service) autoSave(enabled bool) error {
	if !s.pathsSafe() {
		return errors.New("unsafe paths")
	}
	if err := os.MkdirAll(s.dotDir(), 0o700); err != nil {
		return err
	}
	return writeMap(s.autoPath(), map[string]any{"schemaVersion": 1, "enabled": enabled, "updatedAt": stamp(s.now())})
}

// autoDue is broray_dot_auto_due (DA:26-40).
func (s *service) autoDue() bool {
	if en, valid := s.autoEnabled(); !en || !valid {
		return false
	}
	ids, ok := s.autoIDs()
	if !ok || len(ids) == 0 || exists(s.markerPath()) || !safeFile(s.statePath()) {
		return false
	}
	st, err := readMap(s.statePath())
	if err != nil || !numIs(st["schemaVersion"], 1) {
		return false
	}
	if _, ok := st["tests"].([]any); !ok {
		return false
	}
	now := float64(s.now().Unix())
	recent := func(v any) bool {
		t, ok := num(v)
		return ok && t > 0 && now-t >= 0 && now-t < autoInterval
	}
	if ac, ok := st["autoCheck"].(map[string]any); ok {
		if prev, ok := stringList(ac["selectedIds"]); ok && equalStrings(prev, ids) && recent(ac["lastAttemptEpoch"]) {
			return false
		}
	}
	freshIDs := map[string]bool{}
	for _, t := range testRows(st) {
		if contains(ids, t.id) && t.num && recent(t.raw["testedEpoch"]) {
			freshIDs[t.id] = true
		}
	}
	return len(freshIDs) < len(ids)
}

// autoContext binds the selected endpoints and the enable setting
// (broray_dot_auto_context): sha256 of the config, settings and catalog.
func (s *service) autoContext() (string, bool) {
	if _, ok := s.autoIDs(); !ok || !safeFile(s.autoPath()) {
		return "", false
	}
	if en, valid := s.autoEnabled(); !en || !valid || exists(s.markerPath()) {
		return "", false
	}
	cb, err1 := os.ReadFile(s.configPath())
	sb, err2 := os.ReadFile(s.autoPath())
	if err1 != nil || err2 != nil {
		return "", false
	}
	var cat strings.Builder
	for _, c := range catalog {
		cat.WriteString(c.ID + " " + c.Address + " " + c.SNI + "\n")
	}
	return compat.SHA256Hex([]byte(compat.SHA256Hex(cb) + "\n" + compat.SHA256Hex(sb) + "\n" + compat.SHA256Hex([]byte(cat.String())) + "\n")), true
}

// scheduler enqueues dot:auto-check when due (broray_subscription_enqueue_due).
func (s *service) scheduler(ctx context.Context) {
	t := time.NewTicker(schedulerInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.enqueueDue()
		}
	}
}

func (s *service) enqueueDue() {
	if s.d.Queue.Paused() || !s.autoDue() {
		return
	}
	c, ok := s.autoContext()
	if !ok {
		return
	}
	if _, err := s.d.Queue.Enqueue("dot:auto-check", "selected", "SCHEDULER", c); err != nil {
		s.d.Log.Printf("dns: auto-check enqueue: %v", err)
	}
}

func (s *service) defineQueue() {
	s.d.Queue.Define(compat.ActionSpec{Name: "dot:auto-check", Type: "dns_operation", Priority: map[string]int{"*": 5},
		Stages: []compat.Stage{{Name: "probe", Phase: "checking", Run: s.autoRun}}})
}

var errAutoContext = errors.New("DOT_AUTO_CHECK_FAILED: Контекст автопроверки изменился.")

// autoRun probes the saved selection one id at a time and publishes each
// result into state.json (DA:162-262). It never touches Keenetic or the
// selection.
func (s *service) autoRun(t *compat.Task) error {
	if c, ok := s.autoContext(); !ok || c != t.Context {
		return errAutoContext
	}
	ids, ok := s.autoIDs()
	if !ok || len(ids) == 0 {
		return errAutoContext
	}
	started := s.now().Unix()
	checked, failed := 0, 0
	publish := func(status string, result map[string]any, end int64, errorCode any) error {
		s.fileMu.Lock()
		defer s.fileMu.Unlock()
		if !safeFile(s.statePath()) {
			return errors.New("DOT_AUTO_CHECK_FAILED: state unavailable")
		}
		st, err := readMap(s.statePath())
		if err != nil || !numIs(st["schemaVersion"], 1) {
			return errors.New("DOT_AUTO_CHECK_FAILED: state invalid")
		}
		old, ok := st["tests"].([]any)
		if !ok {
			return errors.New("DOT_AUTO_CHECK_FAILED: state invalid")
		}
		at := stamp(s.now())
		if result != nil {
			id := result["id"].(string)
			newer := false
			for _, x := range old {
				if m, ok := x.(map[string]any); ok && m["id"] == id {
					if e, ok := num(m["testedEpoch"]); ok && e > float64(end) {
						newer = true
					}
				}
			}
			if !newer {
				var kept []any
				for _, x := range old {
					if m, ok := x.(map[string]any); ok && m["id"] == id {
						continue
					}
					kept = append(kept, x)
				}
				st["tests"] = append(nonNil(kept), result)
			}
			st["lastTestedAt"], st["lastTestedEpoch"], st["updatedAt"] = at, end, at
		}
		var fin any
		if status != "running" {
			fin = end
		}
		st["autoCheck"] = map[string]any{"requestId": t.RequestID, "context": t.Context, "selectedIds": strSlice(ids),
			"totalCount": len(ids), "checkedCount": checked, "failedCount": failed, "lastAttemptEpoch": started, "status": status,
			"operationId": t.Op.ID, "errorCode": errorCode, "finishedEpoch": fin}
		return writeMap(s.statePath(), st)
	}
	if err := publish("running", nil, started, nil); err != nil {
		return err
	}
	for i, id := range ids {
		if t.Ctx.Err() != nil {
			_ = publish("error", nil, s.now().Unix(), "DOT_AUTO_CHECK_FAILED")
			return t.Ctx.Err()
		}
		c, _ := findCatalog(id)
		pr := s.probe(t.Ctx, c)
		if pr.EndEpoch == 0 {
			pr.EndEpoch = s.now().Unix()
		}
		if cur, ok := s.autoContext(); !ok || cur != t.Context {
			_ = publish("error", nil, s.now().Unix(), "DOT_AUTO_CHECK_FAILED")
			return errAutoContext
		}
		checked++
		if !pr.OK {
			failed++
		}
		status := "running"
		if i == len(ids)-1 {
			status = "success"
			if failed > 0 {
				status = "failed"
			}
		}
		result := map[string]any{"id": c.ID, "address": c.Address, "effectivePort": 853, "sni": c.SNI, "spki": "", "interface": "",
			"domain": "", "ok": pr.OK, "status": pr.Status, "message": pr.Message, "testedAt": stamp(time.Unix(pr.EndEpoch, 0)),
			"testedEpoch": pr.EndEpoch, "latencyMs": pr.LatencyMs}
		if err := publish(status, result, pr.EndEpoch, nil); err != nil {
			return err
		}
	}
	if failed > 0 {
		t.Result("Проверено "+strconv.Itoa(checked-failed)+" из "+strconv.Itoa(checked)+" серверов DNS-over-TLS.", "")
	}
	return nil
}

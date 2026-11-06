package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// Refresh: preparation (fetch + parse) and apply (broray_subscription_update,
// svc:1675-1889), in the legacy direct mode and as the queue action
// subscriptions:refresh (lib/subscription-job.sh).

var (
	fetchTimeout = 180 * time.Second
	parseTimeout = 300 * time.Second
)

// prepared is the outcome of the preparation steps.
type prepared struct {
	OK        bool
	Code, Msg string
	Stage     *staged
	Metadata  map[string]any
	md        *metadata
	body      []byte
}

func (p *prepared) failWith(e *subErr) {
	p.OK, p.Code, p.Msg = false, e.Code, e.Msg
}

func (p *prepared) counts() (int, int, int, int) {
	if p.Stage == nil {
		return 0, 0, 0, 0
	}
	return p.Stage.Received, p.Stage.Parsed, p.Stage.Accepted, p.Stage.Rejected
}

func inputOf(rec map[string]any) fetchInput {
	ua, _ := rec["httpUserAgent"].(string)
	hwid, _ := rec["clientHwid"].(string)
	u, _ := rec["url"].(string)
	return fetchInput{URL: u, Hwid: hwid, UserAgent: ua, SendDevice: rec["sendDeviceInfo"] == true}
}

// runFetch is the fetch helper (180 s). A timeout of the helper is reported
// with timeoutErr; user cancellation returns ctx.Err() of the parent.
func (s *svc) runFetch(ctx context.Context, rec map[string]any, p *prepared, timeoutErr *subErr) error {
	fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	p.md = newMetadata()
	body, err := s.fetch(fctx, inputOf(rec), p.md)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var se *subErr
		if errors.As(err, &se) {
			p.failWith(se)
			return nil
		}
		p.failWith(timeoutErr)
		return nil
	}
	p.OK, p.body = true, body
	return nil
}

// runParse is the parse helper (300 s): extract + stage.
func (s *svc) runParse(ctx context.Context, rec map[string]any, p *prepared, progress func(int, int), timeoutErr *subErr) error {
	if !p.OK {
		return nil
	}
	type result struct {
		st  *staged
		err *subErr
	}
	done := make(chan result, 1)
	id, _ := rec["id"].(string)
	enabled := rec["enabled"] == true
	md := p.md
	body := p.body
	go func() {
		ex, err := extractNodes(body, md)
		if err != nil {
			done <- result{nil, err}
			return
		}
		st, serr := stageNodes(id, ex, enabled, progress)
		done <- result{st, serr}
	}()
	timer := time.NewTimer(parseTimeout)
	defer timer.Stop()
	select {
	case r := <-done:
		p.body = nil
		p.Stage = r.st
		if r.err != nil {
			p.failWith(r.err)
			return nil
		}
		p.Metadata = md.object()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		p.body = nil
		p.failWith(timeoutErr)
		return nil
	}
}

// saveFailed is broray_subscription_save_failed_update.
func (s *svc) saveFailed(id, code, message, trigger string, started time.Time) {
	rec, _, err := s.readRecord(id)
	if err != nil {
		return
	}
	t := now()
	rec["lastUpdateStatus"] = "error"
	rec["lastUpdatedAt"] = compat.Stamp(t)
	rec["lastUpdatedEpoch"] = t.Unix()
	rec["lastError"] = message
	impact := "none"
	if code == "ACTIVE_SERVER_CONFLICT" {
		impact = "requires-decision"
	}
	rec["lastUpdateResult"] = map[string]any{"received": 0, "parsed": 0, "accepted": 0, "rejected": 0, "added": 0, "updated": 0,
		"unchanged": 0, "removed": 0, "warnings": []any{}, "durationMs": (t.Unix() - started.Unix()) * 1000, "trigger": trigger,
		"errorCode": code, "activeServerImpact": impact}
	setSchedule(rec, t)
	rec["updatedAt"] = compat.Stamp(t)
	_ = s.writeRecord(rec)
}

// ensureHwid is broray_subscription_ensure_client_hwid.
func (s *svc) ensureHwid(rec map[string]any) error {
	if h, ok := rec["clientHwid"].(string); ok && hwidValid(h) {
		return nil
	}
	rec["clientHwid"] = newHwid()
	return s.writeRecord(rec)
}

// errCancelled marks a cancelled update (record left "running"; the
// projection overlays the operation state).
var errCancelled = errors.New("OPERATION_CANCELLED: Обновление подписки остановлено.")

// update is broray_subscription_update. prep==nil → legacy preparation inside
// (fetching/parsing phases, cooperative); otherwise the queue outcome is used.
// The caller holds the global fence.
func (s *svc) update(ctx context.Context, op *compat.Operation, id, trigger string, prep *prepared) error {
	if e := validID(id); e != nil {
		return e
	}
	rec, _, err := s.readRecord(id)
	if err != nil {
		if os.IsNotExist(err) {
			return errNotFound
		}
		return errPersistence
	}
	if exists(s.d.RunPath("subscriptions", id+".lock")) {
		return fail("UPDATE_ALREADY_RUNNING", "Состояние прежней операции подписки требует восстановления.")
	}
	if err := s.ensureHwid(rec); err != nil {
		return err
	}
	started := now()
	rec["lastUpdateStatus"] = "running"
	rec["backgroundOperationId"] = op.ID
	rec["lastError"] = nil
	rec["updatedAt"] = compat.Stamp(started)
	if err := s.writeRecord(rec); err != nil {
		return err
	}
	url, _ := rec["url"].(string)

	if prep == nil {
		prep = &prepared{}
		op.Phase("fetching")
		if err := s.runFetch(ctx, rec, prep, fail("DOWNLOAD_TIMEOUT", "Превышено время загрузки подписки. Предыдущий список серверов сохранён.")); err != nil {
			return errCancelled
		}
		if prep.OK {
			op.Phase("parsing")
			var finished atomic.Bool
			defer finished.Store(true)
			progress := func(n, total int) {
				if !finished.Load() && ctx.Err() == nil {
					s.setProgress(op.ID, n, total)
				}
			}
			err := s.runParse(ctx, rec, prep, progress, fail("PARSE_TIMEOUT", "Превышено время разбора серверов подписки. Предыдущий список серверов сохранён."))
			if err != nil {
				return errCancelled
			}
		}
		if ctx.Err() != nil {
			return errCancelled
		}
	}
	op.SetCancelability("protected")
	op.Phase("committing")
	_, _, _, rejected := prep.counts()
	policy := "replace"
	if rejected > 0 {
		policy = "retain-unmatched"
	}
	failCode, failMsg := "", ""
	var res *syncResult
	if !prep.OK {
		failCode, failMsg = prep.Code, prep.Msg
		if failCode == "" {
			failCode = "INTERNAL_ERROR"
		}
		if failMsg == "" {
			failMsg = "Не удалось подготовить подписку."
		}
	} else {
		updateID := fmt.Sprintf("%s-%d-%d", id, started.Unix(), os.Getpid())
		r, err := s.syncCatalog(ctx, id, prep.Stage.Records, rec["enabled"] == true, updateID, policy)
		if err != nil {
			var se *subErr
			if errors.As(err, &se) {
				failCode, failMsg = se.Code, se.Msg
			} else {
				failCode, failMsg = "SERVER_SYNC_ERROR", "Серверный модуль не применил обновление."
			}
		}
		res = r
	}
	if failCode != "" {
		s.saveFailed(id, failCode, failMsg, trigger, started)
		s.logf(fmt.Sprintf("subscription=%s action=update trigger=%s status=error code=%s url=%s", id, trigger, failCode, maskURL(url)))
		return fail(failCode, failMsg)
	}
	finished := now()
	received, parsed, accepted, rejected := prep.counts()
	status := "success"
	if rejected > 0 {
		status = "partial"
	}
	warnings := []any{}
	for _, w := range prep.Stage.Warnings {
		if w != "" {
			warnings = append(warnings, w)
		}
	}
	for _, w := range res.Warnings {
		warnings = append(warnings, w)
	}
	result := map[string]any{"received": received, "parsed": parsed, "accepted": accepted, "rejected": rejected,
		"added": res.Added, "updated": res.Updated, "unchanged": res.Unchanged, "removed": res.Removed, "retained": res.Retained,
		"catalogTotal": res.Total, "deletionPolicy": res.Policy, "warnings": warnings,
		"durationMs": (finished.Unix() - started.Unix()) * 1000, "trigger": trigger, "errorCode": nil, "activeServerImpact": res.Impact}
	cur, _, err := s.readRecord(id)
	if err != nil {
		return fail("PERSISTENCE_ERROR", "Не удалось сохранить состояние подписки.")
	}
	meta := prep.Metadata
	if meta == nil {
		meta = map[string]any{"schemaVersion": 1}
	}
	meta = clone(meta)
	meta["updatedAt"] = compat.Stamp(finished)
	cur["providerMetadata"] = meta
	cur["lastUpdateStatus"] = status
	cur["lastUpdatedAt"] = compat.Stamp(finished)
	cur["lastUpdatedEpoch"] = finished.Unix()
	cur["lastError"] = nil
	cur["serversReceived"] = res.Total
	cur["lastUpdateResult"] = result
	interval, _ := intOf(cur["updateIntervalMinutes"])
	epoch, at := schedule(rec["enabled"] == true, cur["autoUpdateEnabled"] == true, interval, finished)
	cur["nextUpdateEpoch"], cur["nextUpdateAt"] = epoch, at
	cur["updatedAt"] = compat.Stamp(finished)
	if err := s.writeRecord(cur); err != nil {
		return err
	}
	s.logf(fmt.Sprintf("subscription=%s action=update trigger=%s status=%s accepted=%d rejected=%d url=%s", id, trigger, status, accepted, rejected, maskURL(url)))
	return nil
}

// ---------- parse progress (run/subscriptions/progress-<op>.json, kept in memory) ----------

func (s *svc) setProgress(opID string, processed, total int) {
	s.progMu.Lock()
	defer s.progMu.Unlock()
	s.progress[opID] = [2]int{processed, total}
}

func (s *svc) clearProgress(opID string) {
	s.progMu.Lock()
	defer s.progMu.Unlock()
	delete(s.progress, opID)
}

func (s *svc) getProgress(opID string) ([2]int, bool) {
	s.progMu.Lock()
	defer s.progMu.Unlock()
	p, ok := s.progress[opID]
	return p, ok
}

// ---------- legacy direct launch (subscription-web-job.sh) ----------

// launch marks the record running under the CGI's operation and continues
// the update in a background goroutine that keeps the global fence.
// Ownership of release and op passes to the goroutine on success.
func (s *svc) launch(op *compat.Operation, release compat.Release, id, trigger string) (map[string]any, error) {
	rec, _, err := s.readRecord(id)
	if err != nil {
		return nil, errNotFound
	}
	rec["lastUpdateStatus"] = "running"
	rec["backgroundOperationId"] = op.ID
	rec["lastError"] = nil
	if err := s.writeRecord(rec); err != nil {
		return nil, err
	}
	op.Phase("waiting")
	ctx, cancel := context.WithCancel(s.d.Ctx)
	op.SetCancel(cancel)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer release()
		defer cancel()
		err := s.update(ctx, op, id, trigger, nil)
		s.clearProgress(op.ID)
		if errors.Is(err, errCancelled) || (err == nil && ctx.Err() != nil) {
			err = context.Canceled
		}
		op.Finish(err)
	}()
	return map[string]any{"accepted": true, "id": id, "backgroundOperationId": op.ID}, nil
}

// ---------- queue action subscriptions:refresh ----------

const actionRefresh = "subscriptions:refresh"

func stepFail(t *compat.Task, code, message string) error {
	t.Result(message, code)
	return errors.New(code + ": " + message)
}

// checkContext is the record-hash guard of every stage (exit 76).
func (s *svc) checkContext(t *compat.Task) (map[string]any, error) {
	h, ok := s.recordHash(t.Target)
	if !ok || h != t.Context {
		return nil, stepFail(t, "SUBSCRIPTION_CHANGED", "Подписка изменилась до выполнения обновления. Повторите обновление.")
	}
	rec, _, err := s.readRecord(t.Target)
	if err != nil {
		return nil, stepFail(t, "SUBSCRIPTION_CHANGED", "Подписка изменилась до выполнения обновления. Повторите обновление.")
	}
	switch t.Source {
	case "SUBSCRIPTION_AUTO":
		if rec["enabled"] != true || rec["autoUpdateEnabled"] != true {
			return nil, stepFail(t, "SUBSCRIPTION_CHANGED", "Автообновление подписки выключено.")
		}
	case "USER":
	default:
		return nil, stepFail(t, "STATE_UNAVAILABLE", "Источник задания не поддерживается.")
	}
	return rec, nil
}

var errPrepTimeout = fail("PREPARATION_TIMEOUT", "Превышено время подготовки подписки.")

func (s *svc) defineActions() {
	s.d.Queue.Define(compat.ActionSpec{Name: actionRefresh, Type: "subscription_update",
		Priority: map[string]int{"USER": 2, "SUBSCRIPTION_AUTO": 4, "*": 4},
		Stages: []compat.Stage{
			{Name: "fetch", Phase: "fetching", Run: s.stageFetch},
			{Name: "parse", Phase: "parsing", Run: s.stageParse},
			{Name: "apply", Phase: "committing", Protected: true, Global: true, Run: s.stageApply},
		}})
}

func (s *svc) stageFetch(t *compat.Task) error {
	rec, err := s.checkContext(t)
	if err != nil {
		return err
	}
	input := clone(rec)
	if h, ok := input["clientHwid"].(string); !ok || !hwidValid(h) {
		input["clientHwid"] = newHwid()
	}
	p := &prepared{}
	if err := s.runFetch(t.Ctx, input, p, errPrepTimeout); err != nil {
		return err
	}
	if _, err := s.checkContext(t); err != nil {
		return err
	}
	t.Set("input", input)
	t.Set("prep", p)
	return nil
}

func (s *svc) stageParse(t *compat.Task) error {
	if _, err := s.checkContext(t); err != nil {
		return err
	}
	input, _ := t.Get("input").(map[string]any)
	p, _ := t.Get("prep").(*prepared)
	if input == nil || p == nil {
		return stepFail(t, "STATE_UNAVAILABLE", "Подготовка подписки не подтверждена.")
	}
	if err := s.runParse(t.Ctx, input, p, nil, errPrepTimeout); err != nil {
		return err
	}
	if _, err := s.checkContext(t); err != nil {
		return err
	}
	return nil
}

func (s *svc) stageApply(t *compat.Task) error {
	rec, err := s.checkContext(t)
	if err != nil {
		return err
	}
	input, _ := t.Get("input").(map[string]any)
	p, _ := t.Get("prep").(*prepared)
	if input == nil || p == nil {
		return stepFail(t, "STATE_UNAVAILABLE", "Подготовка подписки не подтверждена.")
	}
	if h := input["clientHwid"]; rec["clientHwid"] != h {
		rec["clientHwid"] = h
		if err := s.writeRecord(rec); err != nil {
			var se *subErr
			errors.As(err, &se)
			return stepFail(t, se.Code, se.Msg)
		}
	}
	trigger := "manual"
	if t.Source == "SUBSCRIPTION_AUTO" {
		trigger = "automatic"
	}
	if err := s.update(t.Ctx, t.Op, t.Target, trigger, p); err != nil {
		var se *subErr
		if errors.As(err, &se) {
			return stepFail(t, se.Code, se.Msg)
		}
		return stepFail(t, "INTERNAL_ERROR", "Не удалось обновить подписку.")
	}
	t.Result("Обновление подписки завершено.", "")
	return nil
}

// resolveRefresh is the subscription-refresh routing of api/operations/common.sh:61-70.
func (s *svc) resolveRefresh(id string) (string, string, *cgi.APIError) {
	if validID(id) != nil {
		return "", "", cgi.E(400, "INVALID_REQUEST", "Некорректная подписка.")
	}
	if !regular(s.recPath(id)) {
		return "", "", cgi.E(404, "SUBSCRIPTION_NOT_FOUND", "Подписка не найдена.")
	}
	h, ok := s.recordHash(id)
	if !ok {
		return "", "", cgi.E(503, "STATE_UNAVAILABLE", "Не удалось прочитать подписку.")
	}
	return actionRefresh, h, nil
}

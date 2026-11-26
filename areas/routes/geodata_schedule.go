package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/routepolicy"
	"golang.org/x/sys/unix"
)

const geoUpdateInterval = 24 * time.Hour
const geoRetryInterval = 15 * time.Minute

func (e *engine) geoTime() time.Time {
	if e.geoNow != nil {
		return e.geoNow().UTC()
	}
	return time.Now().UTC()
}

func registerGeodataScheduleHandlers() {
	cgi.Register("/api/routes/geodata-schedule.cgi", wrap(hGeodataSchedule), cgi.Methods(http.MethodGet, http.MethodPost), cgi.Limit(4096))
}

func hGeodataSchedule(e *engine, c *cgi.Req) (any, error) {
	ctx := c.R.Context()
	var s routepolicy.GeoSchedule
	var apply routepolicy.GeoApplySchedule
	var err error
	if c.R.Method == http.MethodPost {
		if err := requestGuard(c); err != nil {
			return nil, err
		}
		if rev, enabled, dailyErr := routepolicy.DecodeGeoScheduleChange(c.Body()); dailyErr == nil {
			apply, err = routepolicy.ReadGeoApplySchedule(ctx, e.root)
			if err == nil {
				s, err = routepolicy.SaveGeoSchedule(ctx, e.root, rev, enabled)
			}
		} else if rev, at, applyErr := routepolicy.DecodeGeoApplyScheduleChange(c.Body()); applyErr == nil {
			s, err = routepolicy.ReadGeoSchedule(ctx, e.root)
			if err == nil {
				apply, err = routepolicy.SaveGeoApplySchedule(ctx, e.root, rev, at)
			}
		} else {
			return nil, cgi.E(400, "GEODATA_SCHEDULE_INVALID", "Некорректные настройки обновления геоданных.")
		}
	} else {
		s, err = routepolicy.ReadGeoSchedule(ctx, e.root)
		if err == nil {
			apply, err = routepolicy.ReadGeoApplySchedule(ctx, e.root)
		}
	}
	if err != nil {
		return nil, geoScheduleError(err)
	}
	paused := e.queue != nil && e.queue.Paused()
	return obj{"enabled": s.Enabled, "revision": routepolicy.GeoScheduleRevision(s), "attempts": s.Attempts,
		"applyAtUTC": apply.AtUTC, "applyRevision": apply.Revision, "intervalHours": 24, "retryMinutes": 15, "automationPaused": paused}, nil
}

func geoScheduleError(err error) error {
	if errors.Is(err, routepolicy.ErrGeoScheduleChanged) {
		return cgi.E(409, "GEODATA_SCHEDULE_CHANGED", "Настройка уже изменилась. Обновите данные.")
	}
	if errors.Is(err, routepolicy.ErrGeoApplyWindow) {
		return cgi.E(409, "GEODATA_APPLY_WINDOW_EXPIRED", "Выбранная минута уже прошла или использована. Подготовленные данные можно применить вручную.")
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return cgi.E(409, "GEODATA_SCHEDULE_BUSY", "Состояние обновляется. Повторите действие.")
	}
	return cgi.E(503, "GEODATA_SCHEDULE_UNCONFIRMED", "Не удалось подтвердить настройки обновления геоданных. Обновите данные.")
}

func geoDue(now time.Time, state routepolicy.State, s routepolicy.GeoSchedule, kind string, checkedAt time.Time) time.Time {
	var due time.Time
	last, _ := time.Parse(time.RFC3339Nano, state.GeoUpdatedAt[kind])
	if !checkedAt.After(now) && checkedAt.After(last) {
		last = checkedAt
	}
	if !last.IsZero() && !last.After(now) {
		due = last.Add(geoUpdateInterval)
	}
	a := s.Attempts[kind]
	if a.Status == "running" || a.Status == "failed" {
		start, _ := time.Parse(time.RFC3339Nano, a.StartedAt)
		if !start.After(now) {
			due, _ = time.Parse(time.RFC3339Nano, a.RetryAt)
		}
	}
	return due
}

func (e *engine) enqueueDueGeodata() *compat.Request {
	return e.enqueueScheduledGeodata(true)
}

// Apply retries only look for prepared changes in the authorized minute.
// Scheduled source checks keep their own cadence.
func (e *engine) enqueueScheduledGeodata(checkSources bool) *compat.Request {
	if e.queue == nil || e.queue.Paused() || e.d.Lock.Busy() {
		return nil
	}
	e.geoScheduleMu.Lock()
	defer e.geoScheduleMu.Unlock()
	if _, live := e.queue.FindLive(geodataAction, "policy"); live {
		return nil
	}
	now := e.geoTime()
	apply, applyErr := routepolicy.ReadGeoApplySchedule(e.ctx, e.root)
	var applyDay string
	if applyErr == nil {
		applyDay = apply.DueDay(now)
	}
	var s routepolicy.GeoSchedule
	if checkSources {
		var err error
		s, err = routepolicy.ReadGeoSchedule(e.ctx, e.root)
		if err != nil {
			return nil
		}
	}
	if !s.Enabled && applyDay == "" {
		return nil
	}
	pending, err := routepolicy.RecoveryPending(e.ctx, e.d.Root)
	if err != nil || pending {
		return nil
	}
	state, err := routepolicy.ReadState(e.ctx, e.root)
	if err != nil {
		return nil
	}
	rev, err := routepolicy.StateRevision(state)
	if err != nil {
		return nil
	}
	ready, err := e.readPendingGeodata(e.ctx, rev, state.GeoRevision)
	if err != nil {
		return nil
	}
	var checkedAt time.Time
	if ready != nil {
		checkedAt = ready.CheckedAt
		if applyDay != "" && !checkedAt.After(now) {
			return e.enqueueGeodataSchedule(obj{"expectedRevision": rev, "operation": "geo-apply", "id": "geodata",
				"expectedPendingRevision": ready.Revision, "expectedScheduleRevision": apply.Revision, "applyDayUTC": applyDay})
		}
	}
	if !s.Enabled {
		return nil
	}
	due := false
	for _, kind := range selectedGeodataKinds(policySelection(state)) {
		deadline := geoDue(now, state, s, kind, checkedAt)
		attempt := s.Attempts[kind]
		if now.Before(e.geoRetryAt[kind]) || ((attempt.Status == "running" || attempt.Status == "failed") && now.Before(deadline)) {
			return nil
		}
		due = due || !now.Before(deadline)
	}
	if !due {
		return nil
	}
	return e.enqueueGeodataSchedule(obj{"expectedRevision": rev, "operation": "geo-check", "id": "geodata"})
}

func (e *engine) enqueueGeodataSchedule(fields obj) *compat.Request {
	body, err := json.Marshal(fields)
	if err != nil {
		return nil
	}
	r, err := e.queue.Enqueue(geodataAction, "policy", "SCHEDULER", string(body))
	if err != nil {
		e.log.Printf("routes: geodata enqueue failed: %v", err)
		return nil
	}
	return r
}

func (e *engine) checkGeoAutomation(ctx context.Context, m routepolicy.Mutation, op *compat.Operation) error {
	if op.Source != "SCHEDULER" {
		if m.ExpectedScheduleRevision != "" {
			return errors.New("manual geodata request has scheduled authority")
		}
		return nil
	}
	if e.queue == nil || e.queue.Paused() {
		return cgi.E(409, "GEODATA_AUTOMATION_PAUSED", "Автоматические операции приостановлены.")
	}
	if m.Operation == "geo-apply" {
		s, err := routepolicy.ReadGeoApplySchedule(ctx, e.root)
		if err != nil {
			return geoScheduleError(err)
		}
		if m.ExpectedScheduleRevision == "" || s.Revision != m.ExpectedScheduleRevision {
			return geoScheduleError(routepolicy.ErrGeoScheduleChanged)
		}
		if m.ApplyDayUTC == "" || s.DueDay(e.geoTime()) != m.ApplyDayUTC {
			return geoScheduleError(routepolicy.ErrGeoApplyWindow)
		}
		return nil
	}
	if m.Operation != "geo-check" {
		return errors.New("invalid scheduled geodata operation")
	}
	s, err := routepolicy.ReadGeoSchedule(ctx, e.root)
	if err != nil {
		return geoScheduleError(err)
	}
	if !s.Enabled {
		return cgi.E(409, "GEODATA_AUTOMATION_DISABLED", "Автоматическое обновление геоданных выключено.")
	}
	return nil
}

func (e *engine) geoBackoff(kind string, at time.Time) {
	e.geoScheduleMu.Lock()
	defer e.geoScheduleMu.Unlock()
	if e.geoRetryAt == nil {
		e.geoRetryAt = map[string]time.Time{}
	}
	e.geoRetryAt[kind] = at
}

func (e *engine) beginGeoAttempt(ctx context.Context, kind string, op *compat.Operation) (routepolicy.GeoAttempt, error) {
	now := e.geoTime()
	a := routepolicy.GeoAttempt{OperationID: op.ID, Source: op.Source, StartedAt: now.Format(time.RFC3339Nano), RetryAt: now.Add(geoRetryInterval).Format(time.RFC3339Nano), Status: "running"}
	_, err := routepolicy.WriteGeoAttempt(ctx, e.root, kind, "", a)
	if err != nil {
		e.geoBackoff(kind, now.Add(geoRetryInterval))
		return a, geoScheduleError(err)
	}
	return a, nil
}

func (e *engine) finishGeoAttempt(ctx context.Context, kind string, a routepolicy.GeoAttempt, cause error) error {
	now := e.geoTime()
	a.CompletedAt = now.Format(time.RFC3339Nano)
	a.Status = "ok"
	if cause != nil {
		a.Status = "failed"
		a.ErrorCode = cgi.MapError(cause).Code
		retry := now.Add(geoRetryInterval)
		// A wall clock adjustment must not produce an invalid persisted record.
		start, _ := time.Parse(time.RFC3339Nano, a.StartedAt)
		if retry.Before(start) {
			retry = start.Add(geoRetryInterval)
		}
		a.RetryAt = retry.Format(time.RFC3339Nano)
		e.geoBackoff(kind, now.Add(geoRetryInterval))
	} else {
		e.geoBackoff(kind, time.Time{})
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := routepolicy.WriteGeoAttempt(ctx, e.root, kind, a.OperationID, a); err != nil {
		e.geoBackoff(kind, now.Add(geoRetryInterval))
		e.log.Printf("routes: geodata result could not be persisted: %v", err)
		return geoScheduleError(err)
	}
	return nil
}

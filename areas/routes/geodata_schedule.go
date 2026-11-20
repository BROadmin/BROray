package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/geodata"
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
	var s routepolicy.GeoSchedule
	var err error
	if c.R.Method == http.MethodPost {
		if err := requestGuard(c); err != nil {
			return nil, err
		}
		rev, enabled, err := routepolicy.DecodeGeoScheduleChange(c.Body())
		if err != nil {
			return nil, cgi.E(400, "GEODATA_SCHEDULE_INVALID", "Некорректные настройки обновления геоданных.")
		}
		s, err = routepolicy.SaveGeoSchedule(c.R.Context(), e.root, rev, enabled)
		if err != nil {
			return nil, geoScheduleError(err)
		}
	} else {
		s, err = routepolicy.ReadGeoSchedule(c.R.Context(), e.root)
		if err != nil {
			return nil, geoScheduleError(err)
		}
	}
	paused := e.queue != nil && e.queue.Paused()
	return obj{"enabled": s.Enabled, "revision": routepolicy.GeoScheduleRevision(s), "attempts": s.Attempts, "intervalHours": 24, "retryMinutes": 15, "automationPaused": paused}, nil
}

func geoScheduleError(err error) error {
	if errors.Is(err, routepolicy.ErrGeoScheduleChanged) {
		return cgi.E(409, "GEODATA_SCHEDULE_CHANGED", "Настройка уже изменилась. Обновите данные.")
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return cgi.E(409, "GEODATA_SCHEDULE_BUSY", "Состояние обновляется. Повторите действие.")
	}
	return cgi.E(503, "GEODATA_SCHEDULE_UNCONFIRMED", "Не удалось подтвердить настройки обновления геоданных. Обновите данные.")
}

func geoDue(now time.Time, state routepolicy.State, s routepolicy.GeoSchedule, kind string) time.Time {
	var due time.Time
	if last, err := time.Parse(time.RFC3339Nano, state.GeoUpdatedAt[kind]); err == nil && !last.After(now) {
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

// Uses the existing minute loop and coordinator; selected disabled rules also
// need their data. Only one kind is queued so the next uses a fresh policy CAS.
func (e *engine) enqueueDueGeodata() *compat.Request {
	if e.queue == nil || e.queue.Paused() || e.d.Lock.Busy() {
		return nil
	}
	e.geoScheduleMu.Lock()
	defer e.geoScheduleMu.Unlock()
	if _, live := e.queue.FindLive(geodataAction, "policy"); live {
		return nil
	}
	s, err := routepolicy.ReadGeoSchedule(e.ctx, e.root)
	if err != nil || !s.Enabled {
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
	sel := policySelection(state)
	now := e.geoTime()
	for _, part := range []struct {
		kind     geodata.Kind
		selected bool
	}{{geodata.GeoIP, len(sel.CountryCodes) > 0}, {geodata.GeoSite, len(sel.GeoSiteTags) > 0}} {
		kind := string(part.kind)
		if !part.selected || now.Before(geoDue(now, state, s, kind)) || now.Before(e.geoRetryAt[kind]) {
			continue
		}
		body, err := json.Marshal(obj{"expectedRevision": rev, "operation": "geo-update", "id": kind, "kind": kind})
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
	return nil
}

func (e *engine) checkGeoAutomation(ctx context.Context, m routepolicy.Mutation, op *compat.Operation) error {
	if op.Source != "SCHEDULER" {
		return nil
	}
	if m.Operation != "geo-update" {
		return errors.New("scheduler may only update selected geodata")
	}
	s, err := routepolicy.ReadGeoSchedule(ctx, e.root)
	if err != nil {
		return geoScheduleError(err)
	}
	if !s.Enabled {
		return cgi.E(409, "GEODATA_AUTOMATION_DISABLED", "Автоматическое обновление геоданных выключено.")
	}
	if e.queue == nil || e.queue.Paused() {
		return cgi.E(409, "GEODATA_AUTOMATION_PAUSED", "Автоматические операции приостановлены.")
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

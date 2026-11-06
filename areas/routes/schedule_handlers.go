package routes

import (
	"encoding/json"
	"net/http"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// Endpoints of the scheduled catalog check (3.5.0, additive):
//
//	GET  /api/routes/catalog-schedule.cgi  settings + last result
//	POST /api/routes/catalog-schedule.cgi  {intervalHours, autoApply[, maxRemovalPercent]}
//	POST /api/routes/catalog-check.cgi     queue a check now (USER)
//
// POSTs carry the operations CSRF header and a same-origin Origin, like
// dot-auto-settings.cgi.
const scheduleBodyMax = 4096

func registerScheduleHandlers() {
	cgi.Register("/api/routes/catalog-schedule.cgi", wrap(hCatalogSchedule),
		cgi.Methods(http.MethodGet, http.MethodPost), cgi.Limit(scheduleBodyMax))
	cgi.Register("/api/routes/catalog-check.cgi", wrap(hCatalogCheck), cgi.POST(), cgi.Limit(scheduleBodyMax))
}

func requestGuard(c *cgi.Req) *cgi.APIError {
	if c.Header("X-BROray-Request") != "operations" {
		return cgi.E(403, "CSRF_REJECTED", "Обновите страницу и повторите действие.")
	}
	if !compat.OriginAllowed(c.R) {
		return cgi.E(403, "ORIGIN_REJECTED", "Источник запроса не подтверждён.")
	}
	return nil
}

func hCatalogSchedule(e *engine, c *cgi.Req) (any, error) {
	if c.R.Method != http.MethodPost {
		return e.scheduleView(), nil
	}
	if aerr := requestGuard(c); aerr != nil {
		return nil, aerr
	}
	var body map[string]json.RawMessage
	if !c.JSONObject(&body) || body == nil {
		return nil, cgi.E(400, "REQUEST_JSON_INVALID", "Запрос должен содержать корректный JSON-объект.")
	}
	invalid := cgi.E(400, "REQUEST_INVALID", "Некорректные настройки проверки обновлений.")
	s := e.scheduleSettings()
	for k, raw := range body {
		switch k {
		case "intervalHours":
			var h int
			if json.Unmarshal(raw, &h) != nil || !validInterval(h) {
				return nil, invalid
			}
			s.IntervalHours = h
		case "autoApply":
			var b bool
			if json.Unmarshal(raw, &b) != nil {
				return nil, invalid
			}
			s.AutoApply = b
		case "maxRemovalPercent":
			var p int
			if json.Unmarshal(raw, &p) != nil || !validRemovalPct(p) {
				return nil, invalid
			}
			s.MaxRemovalPercent = p
		default:
			return nil, invalid
		}
	}
	if _, ok := body["intervalHours"]; !ok {
		return nil, invalid
	}
	if _, ok := body["autoApply"]; !ok {
		return nil, invalid
	}
	if err := e.saveScheduleSettings(s); err != nil {
		return nil, cgi.E(500, "ROUTES_SCHEDULE_SAVE_FAILED", "Настройки проверки обновлений не сохранены.")
	}
	e.log.Printf("routes: catalog schedule: every %d h, auto-apply %v, max removal %d%%", s.IntervalHours, s.AutoApply, s.MaxRemovalPercent)
	return e.scheduleView(), nil
}

func hCatalogCheck(e *engine, c *cgi.Req) (any, error) {
	if aerr := requestGuard(c); aerr != nil {
		return nil, aerr
	}
	if e.queue == nil {
		return nil, cgi.E(503, "ROUTES_SCHEDULE_UNAVAILABLE", "Проверка обновлений сейчас недоступна.")
	}
	r, err := e.queue.Enqueue(actionCatalogUpdate, catalogTarget, "USER", "manual")
	if err != nil {
		return nil, cgi.E(503, "ROUTES_SCHEDULE_UNAVAILABLE", "Не удалось поставить проверку обновлений в очередь.")
	}
	v := e.scheduleView()
	v["running"] = true
	v["requestId"] = r.RequestID
	return v, nil
}

// Package dns ports the 3.x DNS-over-TLS part of the "routes" WebUI area
// (contract: docs/contracts/routes-custom-dns.md Part B): the
// /api/routes/dot-*.cgi endpoints, the receipts and recovery marker of
// lib/routes-dot.sh and the auto-check of lib/dot-auto.sh.
package dns

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

func init() { compat.Area("dns", 50, setup) }

type service struct {
	d      *compat.Deps
	fileMu sync.Mutex // serializes read-modify-write of config.json/state.json
	now    func() time.Time
	probe  func(ctx context.Context, c catEntry) probeResult
}

var (
	current  atomic.Pointer[service]
	regOnce  sync.Once
	bodyMax  int64 = 65536
	busyCode       = "ROUTES_OPERATION_BUSY"
)

func newService(d *compat.Deps) *service {
	return &service{d: d, now: time.Now, probe: tlsProbe}
}

func setup(d *compat.Deps) error {
	s := newService(d)
	current.Store(s)
	regOnce.Do(register)
	s.defineQueue()
	registerBackup(s)
	compat.RegisterSnapshot("dns", func(ctx context.Context) (map[string]any, error) {
		return current.Load().status(ctx)
	})
	compat.RegisterUninstallStep("dot", func(ctx context.Context) error { return current.Load().uninstall(ctx) })
	if d.Maintenance {
		return nil
	}
	go func() {
		if exists(s.markerPath()) {
			if rel, err := d.Lock.Acquire(d.Ctx, "dot", "dot:recover", "dns-over-tls"); err == nil {
				s.recoverMarker(d.Ctx)
				rel()
			}
		}
		s.scheduler(d.Ctx)
	}()
	return nil
}

func register() {
	tooLarge := cgi.TooLarge(cgi.E(413, "REQUEST_TOO_LARGE", "Запрос DNS-over-TLS слишком велик."))
	cgi.Register("/api/routes/dot-status.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleStatus(c) })
	cgi.Register("/api/routes/dot-test.cgi", func(c *cgi.Req) (any, error) {
		return current.Load().handleMutation(c, "test", true, (*service).runTest)
	}, cgi.POST(), cgi.Limit(bodyMax), tooLarge)
	cgi.Register("/api/routes/dot-apply.cgi", func(c *cgi.Req) (any, error) {
		return current.Load().handleMutation(c, "apply", true, (*service).runApply)
	}, cgi.POST(), cgi.Limit(bodyMax), tooLarge)
	cgi.Register("/api/routes/dot-delete.cgi", func(c *cgi.Req) (any, error) {
		return current.Load().handleMutation(c, "delete", true, (*service).runDelete)
	}, cgi.POST(), cgi.Limit(bodyMax), tooLarge)
	cgi.Register("/api/routes/dot-delete-preview.cgi", func(c *cgi.Req) (any, error) {
		return current.Load().handleMutation(c, "delete-preview", false, func(s *service, ctx context.Context, _ map[string]any) (map[string]any, error) {
			return s.runDeletePreview(ctx)
		})
	}, cgi.POST(), cgi.Limit(bodyMax), tooLarge)
	cgi.Register("/api/routes/dot-auto-settings.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleAutoSettings(c) },
		cgi.POST(), cgi.Limit(bodyMax), tooLarge)
}

// toAPI maps a CLI-style error to the DC:81-128 envelope.
func toAPI(err error) *cgi.APIError {
	var de *dotError
	if errors.As(err, &de) {
		return cgi.Ed(dotStatus(de.Code), de.Code, de.Message, de.Details)
	}
	var ae *cgi.APIError
	if errors.As(err, &ae) {
		return ae
	}
	return cgi.Ed(400, "DOT_OPERATION_FAILED", "Операция DNS-over-TLS завершилась ошибкой.", err.Error())
}

func (s *service) withAuto(data map[string]any) map[string]any {
	out := make(map[string]any, len(data)+1)
	for k, v := range data {
		out[k] = v
	}
	out["autoCheck"] = s.autoView()
	return out
}

func (s *service) handleStatus(c *cgi.Req) (any, error) {
	var data map[string]any
	var err error
	if strings.Contains("&"+c.RawQuery+"&", "&force=1&") {
		data, err = s.status(c.R.Context())
	} else {
		data, err = s.statusCached(c.R.Context())
	}
	if err != nil {
		return nil, toAPI(err)
	}
	return s.withAuto(data), nil
}

// readBody is broray_dot_api_read_body (lib/web-request-body.sh + DC:54-65).
func readBody(c *cgi.Req) (map[string]any, *cgi.APIError) {
	cl := c.R.ContentLength
	switch {
	case cl < 0:
		return nil, cgi.E(400, "CONTENT_LENGTH_INVALID", "Некорректный размер запроса.")
	case cl == 0:
		return nil, cgi.E(400, "REQUEST_BODY_REQUIRED", "Тело запроса отсутствует.")
	case cl > bodyMax:
		return nil, cgi.E(413, "REQUEST_TOO_LARGE", "Запрос DNS-over-TLS слишком велик.")
	case int64(len(c.Body())) != cl:
		return nil, cgi.E(400, "REQUEST_BODY_INCOMPLETE", "Тело запроса получено не полностью.")
	}
	m, ok := c.Map()
	if !ok {
		return nil, cgi.E(400, "REQUEST_JSON_INVALID", "Запрос должен содержать корректный JSON-объект.")
	}
	return m, nil
}

type runFn func(s *service, ctx context.Context, req map[string]any) (map[string]any, error)

// handleMutation: body (when the CGI reads one) → global lock dot:<action> → run.
func (s *service) handleMutation(c *cgi.Req, action string, withBody bool, fn runFn) (any, error) {
	var req map[string]any
	if withBody {
		var apiErr *cgi.APIError
		if req, apiErr = readBody(c); apiErr != nil {
			return nil, apiErr
		}
	}
	release, ok := s.d.Lock.TryAcquire("dot", "dot:"+action, "dns-over-tls")
	if !ok {
		return nil, cgi.E(409, busyCode, "Другая конфликтующая операция уже выполняется.")
	}
	defer release()
	op := s.d.Queue.StartOperation("", "dns_operation", "dot:"+action, "USER", "protected", "")
	op.Phase("working")
	// The work continues if the browser goes away: a half-applied DoT
	// transaction must finish or roll back, never stop mid-way.
	ctx := context.WithoutCancel(c.R.Context())
	data, err := fn(s, ctx, req)
	op.Finish(err)
	if err != nil {
		return nil, toAPI(err)
	}
	return s.withAuto(data), nil
}

func (s *service) handleAutoSettings(c *cgi.Req) (any, error) {
	if c.Header("X-BROray-Request") != "operations" {
		return nil, cgi.E(403, "CSRF_REJECTED", "Обновите страницу и повторите действие.")
	}
	if !compat.OriginAllowed(c.R) {
		return nil, cgi.E(403, "ORIGIN_REJECTED", "Источник запроса не подтверждён.")
	}
	req, apiErr := readBody(c)
	if apiErr != nil {
		return nil, apiErr
	}
	release, ok := s.d.Lock.TryAcquire("dot", "dot:auto-settings", "dns-over-tls")
	if !ok {
		return nil, cgi.E(409, busyCode, "Другая операция выполняется. Повторите сохранение позже.")
	}
	defer release()
	op := s.d.Queue.StartOperation("", "dns_operation", "dot:auto-settings", "USER", "protected", "")
	enabled, isBool := req["enabled"].(bool)
	if len(req) != 1 || !isBool {
		op.Finish(errors.New("REQUEST_INVALID: Поле enabled должно быть логическим."))
		return nil, cgi.E(400, "REQUEST_INVALID", "Поле enabled должно быть логическим.")
	}
	op.Phase("committing")
	if err := s.autoSave(enabled); err != nil {
		op.Finish(errors.New("DOT_AUTO_SETTINGS_FAILED: " + err.Error()))
		return nil, cgi.E(500, "DOT_AUTO_SETTINGS_FAILED", "Настройка автопроверки не сохранена.")
	}
	op.Finish(nil)
	if enabled {
		go s.enqueueDue()
	}
	return s.autoView(), nil
}

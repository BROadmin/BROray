package servers

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/BROadmin/broray/internal/cgi"
)

var tooLarge64 = cgi.E(413, "REQUEST_TOO_LARGE", "Тело запроса превышает 64 КБ.")

// readBody is broray_servers_api_read_body_to_file (common.sh:29-46).
func readBody(c *cgi.Req) (map[string]any, *cgi.APIError) {
	switch {
	case c.R.ContentLength < 0:
		return nil, cgi.E(411, "CONTENT_LENGTH_REQUIRED", "Для запроса требуется корректный Content-Length.")
	case c.R.ContentLength == 0:
		return nil, cgi.E(400, "REQUEST_BODY_REQUIRED", "Тело запроса отсутствует.")
	case c.R.ContentLength > 65536:
		return nil, tooLarge64
	case int64(len(c.Body())) != c.R.ContentLength:
		return nil, cgi.E(400, "REQUEST_BODY_INCOMPLETE", "Тело запроса получено не полностью.")
	}
	m, ok := decodeBody(c)
	if !ok {
		return nil, cgi.E(400, "REQUEST_JSON_INVALID", "Тело запроса должно быть корректным JSON-объектом.")
	}
	return m, nil
}

// bodyField is broray_servers_api_body_field (`.[$f] // empty`, jq -r).
func bodyField(m map[string]any, k string) string { return jqRawEmpty(m[k]) }

// detailsID is details.cgi:7-15: the last `id=` in QUERY_STRING, only
// %2D %2E %5F decoded.
func detailsID(raw string) string {
	i := strings.LastIndex(raw, "id=")
	if i < 0 {
		return ""
	}
	v := raw[i+3:]
	if j := strings.IndexByte(v, '&'); j >= 0 {
		v = v[:j]
	}
	return strings.NewReplacer("%2D", "-", "%2E", ".", "%5F", "_").Replace(v)
}

var (
	registerOnce sync.Once
	current      atomic.Pointer[svc]
)

func cur() *svc { return current.Load() }

func register() {
	cgi.Register("/api/servers/summary.cgi", func(c *cgi.Req) (any, error) { return cur().summary(c.R.Context()) })
	cgi.Register("/api/servers/details.cgi", func(c *cgi.Req) (any, error) { return cur().details(detailsID(c.RawQuery)) })
	cgi.Register("/api/servers/import.cgi", func(c *cgi.Req) (any, error) {
		body, e := readBody(c)
		if e != nil {
			return nil, e
		}
		uri := bodyField(body, "uri")
		if uri == "" {
			return nil, cgi.E(400, "URI_REQUIRED", "Не указана конфигурация сервера.")
		}
		if body["preview"] == true {
			return cur().preview(uri)
		}
		return cur().importCommit(uri)
	}, cgi.POST(), cgi.Limit(65536), cgi.TooLarge(tooLarge64))
	cgi.RegisterRaw("/api/servers/activate.cgi", func(c *cgi.Req) {
		if c.Header("X-BROray-Queue") == "1" {
			sv := cur()
			sv.d.ServeQueued(c, sv.resolveActivation)
			return
		}
		body, e := readBody(c)
		if e != nil {
			cgi.WriteErr(c.W, e)
			return
		}
		data, err := cur().activate(bodyField(body, "id"))
		if err != nil {
			cgi.WriteErr(c.W, cgi.MapError(err))
			return
		}
		cgi.WriteOK(c.W, data)
	}, cgi.POST(), cgi.Limit(65536), cgi.TooLarge(tooLarge64))
	cgi.Register("/api/servers/deactivate.cgi", func(c *cgi.Req) (any, error) { return cur().deactivate() }, cgi.POST())
	cgi.Register("/api/servers/delete.cgi", func(c *cgi.Req) (any, error) {
		body, e := readBody(c)
		if e != nil {
			return nil, e
		}
		return cur().deleteServer(bodyField(body, "id"))
	}, cgi.POST(), cgi.Limit(65536), cgi.TooLarge(tooLarge64))
	cgi.RegisterRaw("/api/servers/check.cgi", func(c *cgi.Req) {
		if c.Header("X-BROray-Queue") == "1" {
			sv := cur()
			sv.d.ServeQueued(c, sv.resolveCheck)
			return
		}
		body, e := readBody(c)
		if e != nil {
			cgi.WriteErr(c.W, e)
			return
		}
		data, err := cur().directCheck(bodyField(body, "id"))
		if err != nil {
			cgi.WriteErr(c.W, cgi.MapError(err))
			return
		}
		cgi.WriteOK(c.W, data)
	}, cgi.POST(), cgi.Limit(65536), cgi.TooLarge(tooLarge64))
	cgi.Register("/api/servers/quality-batch-complete.cgi", func(c *cgi.Req) (any, error) { return cur().publishSnapshot() }, cgi.POST())
	cgi.Register("/api/servers/auto-switch-status.cgi", func(c *cgi.Req) (any, error) { return cur().handleAutoSwitchStatus(c) })
	cgi.Register("/api/servers/auto-switch-save.cgi", func(c *cgi.Req) (any, error) { return cur().handleAutoSwitchSave(c) }, cgi.POST(), cgi.Limit(16384),
		cgi.TooLarge(cgi.E(413, "REQUEST_TOO_LARGE", "Запрос слишком большой.")))
	cgi.Register("/api/servers/quality-refresh-status.cgi", func(c *cgi.Req) (any, error) { return cur().handleQualityRefreshStatus(c) })
	cgi.Register("/api/servers/quality-refresh-save.cgi", func(c *cgi.Req) (any, error) { return cur().handleQualityRefreshSave(c) }, cgi.POST(), cgi.Limit(4096),
		cgi.TooLarge(cgi.E(413, "REQUEST_TOO_LARGE", "Запрос слишком большой.")))
}

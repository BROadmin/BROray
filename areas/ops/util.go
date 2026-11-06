package ops

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// writeC writes a raw backend object (envelope C) with the 3.x headers.
func writeC(w http.ResponseWriter, status int, obj map[string]any) { cgi.WriteJSON(w, status, obj) }

// cError is the envelope-C failure {"ok":false,"error":{"code","message"}}.
func cError(code, message string) map[string]any {
	return map[string]any{"ok": false, "error": map[string]any{"code": code, "message": message}}
}

// requireBROrayHeader is the CSRF rule of every /api/broray/* POST:
// X-BROray-Request must be exactly "1" (checked after method and session).
func requireBROrayHeader(c *cgi.Req) bool {
	if c.R.Header.Get("X-BROray-Request") != "1" {
		cgi.WriteErr(c.W, cgi.E(403, "INVALID_REQUEST", "Запрос отклонён."))
		return false
	}
	return true
}

// requireBROrayOrigin applies the same page POST guard to backups and updates.
func requireBROrayOrigin(c *cgi.Req) bool {
	if !requireBROrayHeader(c) {
		return false
	}
	r := c.R
	if c.Header("Origin") == "" && c.Header("X-BROray-Origin") != "" {
		// KeenDNS strips Origin. Reuse the operations guard's fallback to
		// the live, owned publication, after checking this area's marker.
		// Clone headers so the original request keeps its marker "1".
		r = c.R.Clone(c.R.Context())
		r.Header.Set("X-BROray-Request", "operations")
	}
	if !compat.OriginAllowed(r) {
		cgi.WriteErr(c.W, cgi.E(403, "ORIGIN_REJECTED", "Источник запроса не подтверждён."))
		return false
	}
	return true
}

// brorayBody is broray_web_request_body_to_file with the 4096-byte limit:
// missing/zero length → 400 INVALID_CONTENT_LENGTH, larger → 413
// REQUEST_TOO_LARGE, short → 400 REQUEST_BODY_INCOMPLETE.
func brorayBody(c *cgi.Req) ([]byte, bool) {
	switch {
	case c.R.ContentLength <= 0 || !c.HasBody():
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_CONTENT_LENGTH", "Тело запроса пусто или имеет некорректный размер."))
		return nil, false
	case c.R.ContentLength > 4096 || len(c.Body()) > 4096:
		cgi.WriteErr(c.W, cgi.E(413, "REQUEST_TOO_LARGE", "Тело запроса слишком большое."))
		return nil, false
	case int64(len(c.Body())) != c.R.ContentLength:
		cgi.WriteErr(c.W, cgi.E(400, "REQUEST_BODY_INCOMPLETE", "Тело запроса передано не полностью."))
		return nil, false
	}
	return c.Body(), true
}

// tooLarge413 is the framework error for bodies above the read limit on
// /api/broray/* routes (same text as brorayBody).
var tooLarge413 = cgi.E(413, "REQUEST_TOO_LARGE", "Тело запроса слишком большое.")

// jsonObject decodes exactly one JSON object.
func jsonObject(b []byte) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	var m map[string]json.RawMessage
	if dec.Decode(&m) != nil || m == nil {
		return nil, false
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return m, true
}

// jqRaw mirrors `jq -r '.field // empty'`: strings as is, other truthy
// values as their JSON text, null/false/absent empty.
func jqRaw(raw json.RawMessage, present bool) string {
	if !present {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if !x {
			return ""
		}
		return "true"
	case string:
		return x
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return ""
	}
	return buf.String()
}

// jqRawKeep mirrors `jq -r 'if has(k) then .k else true end'`: unlike //,
// false prints "false" and null prints "null".
func jqRawKeep(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return "true"
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return x
	}
	var buf bytes.Buffer
	_ = json.Compact(&buf, raw)
	return buf.String()
}

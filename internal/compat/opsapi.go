package compat

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/cgi"
)

// The 3.x operations HTTP pipeline (web-new/api/operations/common.sh):
// shared by /api/operations/*.cgi and by the queue branch
// (X-BROray-Queue: 1) of /api/servers/check.cgi and
// /api/subscriptions/refresh.cgi.

// KeenDNSOrigin returns the live KeenDNS publication origin
// "https://broray.<name>.<domain>" when BROray's web publication is enabled,
// consistent and owned (operations-origin.sh:106-132). Set by the keenetic
// area; nil → only same-host origins are accepted.
var KeenDNSOrigin func(ctx context.Context) (string, bool)

var reLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// NormalizeOrigin implements operations-origin.sh:8-54 → scheme://host[:port] or "".
func NormalizeOrigin(s string) string {
	if len(s) == 0 || len(s) > 320 || strings.ContainsAny(s, "\r\n") {
		return ""
	}
	var scheme, rest string
	switch {
	case strings.HasPrefix(s, "http://"):
		scheme, rest = "http", s[7:]
	case strings.HasPrefix(s, "https://"):
		scheme, rest = "https", s[8:]
	default:
		return ""
	}
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(".:[]-", r)) {
			return ""
		}
	}
	host, port := rest, ""
	if strings.HasPrefix(rest, "[") {
		i := strings.Index(rest, "]")
		if i < 0 {
			return ""
		}
		host, port = rest[:i+1], strings.TrimPrefix(rest[i+1:], ":")
		if rest[i+1:] != "" && !strings.HasPrefix(rest[i+1:], ":") {
			return ""
		}
		ip := net.ParseIP(host[1 : len(host)-1])
		if ip == nil || ip.To4() != nil {
			return ""
		}
	} else {
		if i := strings.LastIndex(rest, ":"); i >= 0 {
			host, port = rest[:i], rest[i+1:]
			if strings.Contains(host, ":") {
				return ""
			}
		}
		host = strings.ToLower(host)
		if host == "" || len(host) > 253 {
			return ""
		}
		for _, l := range strings.Split(host, ".") {
			if len(l) < 1 || len(l) > 63 || !reLabel.MatchString(l) {
				return ""
			}
		}
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || port[0] == '0' {
			return ""
		}
		if (scheme == "http" && n == 80) || (scheme == "https" && n == 443) {
			port = ""
		}
	} else if strings.HasSuffix(rest, ":") {
		return ""
	}
	if port != "" {
		return scheme + "://" + host + ":" + port
	}
	return scheme + "://" + host
}

// OriginAllowed implements broray_operations_origin_allowed (operations-origin.sh:134-164).
func OriginAllowed(r *http.Request) bool {
	host := r.Host
	if host == "" {
		return false
	}
	var origin string
	fromHeader := false
	if o := r.Header.Get("Origin"); o != "" {
		origin = NormalizeOrigin(o)
		if origin == "" {
			return false
		}
		if x := r.Header.Get("X-BROray-Origin"); x != "" && NormalizeOrigin(x) != origin {
			return false
		}
		fromHeader = true
	} else {
		if r.Header.Get("X-BROray-Request") != "operations" {
			return false
		}
		origin = NormalizeOrigin(r.Header.Get("X-BROray-Origin"))
		if origin == "" {
			return false
		}
	}
	if fromHeader && (origin == NormalizeOrigin("http://"+host) || origin == NormalizeOrigin("https://"+host)) {
		return true
	}
	if !strings.HasPrefix(origin, "https://broray.") || KeenDNSOrigin == nil {
		return false
	}
	want, ok := KeenDNSOrigin(r.Context())
	return ok && origin == want
}

// OpsBody runs the common.sh request checks for the operations pipeline and
// returns the decoded body object (POST) or nil (GET). On failure it has
// already written the envelope-B error and returns ok=false.
func OpsBody(c *cgi.Req) (map[string]json.RawMessage, bool) {
	fail := func(e *cgi.APIError) (map[string]json.RawMessage, bool) { cgi.WriteErr(c.W, e); return nil, false }
	if c.RawQuery != "" {
		return fail(cgi.E(400, "INVALID_REQUEST", "Параметры запроса не поддерживаются."))
	}
	if c.R.Method != http.MethodPost {
		if c.HasBody() {
			return fail(cgi.E(400, "INVALID_REQUEST", "Тело GET-запроса не поддерживается."))
		}
		return nil, true
	}
	if c.Header("X-BROray-Request") != "operations" {
		return fail(cgi.E(403, "CSRF_REJECTED", "Обновите страницу и повторите действие."))
	}
	if !OriginAllowed(c.R) {
		return fail(cgi.E(403, "ORIGIN_REJECTED", "Источник запроса не подтверждён."))
	}
	switch c.Header("Content-Type") {
	case "application/json", "application/json; charset=utf-8":
	default:
		return fail(cgi.E(415, "INVALID_CONTENT_TYPE", "Требуется JSON."))
	}
	if c.R.ContentLength < 0 || c.R.ContentLength >= 1e8 {
		return fail(cgi.E(400, "INVALID_LENGTH", "Некорректная длина запроса."))
	}
	if len(c.Body()) > 4096 {
		return fail(cgi.E(413, "REQUEST_TOO_LARGE", "Запрос превышает 4 КБ."))
	}
	if int64(len(c.Body())) != c.R.ContentLength {
		return fail(cgi.E(400, "INVALID_BODY", "Запрос получен не полностью."))
	}
	var m map[string]json.RawMessage
	if !c.JSONObject(&m) || m == nil {
		return fail(cgi.E(400, "INVALID_BODY", "Требуется один JSON-объект."))
	}
	return m, true
}

// WriteOps writes a coordinator object (D shape) with the common.sh status mapping.
func WriteOps(w http.ResponseWriter, status int, obj map[string]any) { cgi.WriteJSON(w, status, obj) }

// WriteOpsError writes {"ok":false,"errorCode":code} with its mapped status.
func WriteOpsError(w http.ResponseWriter, err error) {
	qe, ok := err.(*QueueError)
	if !ok {
		qe = &QueueError{"STATE_UNAVAILABLE"}
	}
	cgi.WriteJSON(w, qe.HTTPStatus(), map[string]any{"ok": false, "errorCode": qe.Code})
}

var reQueueID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,95}$`)

// QueueResolve maps the submitted id to (action, context) or returns an
// envelope-B error (404 SERVER_NOT_FOUND, …).
type QueueResolve func(id string) (action, context string, err *cgi.APIError)

// ServeQueued handles the queue branch of servers/check.cgi and
// subscriptions/refresh.cgi: common checks, body exactly {"id","nonce"},
// resolve, queue-submit, 202 + receipt with accepted:true.
func (d *Deps) ServeQueued(c *cgi.Req, resolve QueueResolve) {
	m, ok := OpsBody(c)
	if !ok {
		return
	}
	var id, nonce string
	if len(m) != 2 || json.Unmarshal(m["id"], &id) != nil || json.Unmarshal(m["nonce"], &nonce) != nil ||
		!nonceRe.MatchString(nonce) || !reQueueID.MatchString(id) {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректные параметры задания."))
		return
	}
	action, context, apiErr := resolve(id)
	if apiErr != nil {
		cgi.WriteErr(c.W, apiErr)
		return
	}
	r, coalesced, err := d.Queue.Submit(action, id, "USER", context, nonce)
	if err != nil {
		WriteOpsError(c.W, err)
		return
	}
	rec := r.Receipt(coalesced)
	rec["accepted"] = true
	cgi.WriteJSON(c.W, 202, rec)
}

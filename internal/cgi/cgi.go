// Package cgi serves the 3.x WebUI URLs (/api/<area>/<name>.cgi) from Go
// with the 3.x contracts, so the 3.x pages and scripts run unchanged.
//
// Envelope B (runtime/app/web-new/api/auth-common.sh):
//
//	success: {"success":true,"data":<data>,"error":null}
//	failure: {"success":false,"data":null,"error":{"code","message","details"}}
//
// Endpoints with other shapes (flat auth objects, raw coordinator objects,
// raw backend objects, HTTP 202 queue receipts) register a Raw handler and
// write the response themselves with WriteJSON.
//
// Order of the common checks, as in 3.x: method → session → per-route
// header checks → body limit. Area-specific checks (body schema, query
// format, CSRF headers with area-specific codes) belong to the handler.
package cgi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// APIError is a contract error with its HTTP status.
type APIError struct {
	Status  int
	Code    string
	Message string
	Details any // nil → JSON null; "" is also emitted as null (3.x rule)
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// E builds an APIError.
func E(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}

// Ed builds an APIError with details.
func Ed(status int, code, message string, details any) *APIError {
	return &APIError{Status: status, Code: code, Message: message, Details: details}
}

// Session is what the auth layer reports for an authenticated request.
type Session struct {
	User string
}

// Authenticator is implemented by the web server's auth layer.
type Authenticator interface {
	// Authenticate returns the session or ok=false (the caller writes 401).
	Authenticate(w http.ResponseWriter, r *http.Request) (Session, bool)
}

// Req is one CGI-compatible request.
type Req struct {
	W        http.ResponseWriter
	R        *http.Request
	Query    url.Values
	RawQuery string // QUERY_STRING exactly as received (several 3.x CGIs parse it raw)
	User     string
	body     []byte
}

// Body returns the raw request body (bounded by the route limit).
func (c *Req) Body() []byte { return c.body }

// HasBody reports whether a non-empty body was sent.
func (c *Req) HasBody() bool { return len(c.body) > 0 }

// Header returns a request header.
func (c *Req) Header(name string) string { return c.R.Header.Get(name) }

// JSONObject decodes the body as exactly one JSON object into v. It returns
// false when the body is empty, not JSON, or not an object.
func (c *Req) JSONObject(v any) bool {
	b := strings.TrimSpace(string(c.body))
	if b == "" || b[0] != '{' {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return false
	}
	var extra any
	return dec.Decode(&extra) == io.EOF
}

// Map decodes the body as a JSON object into a map (numbers as json.Number).
func (c *Req) Map() (map[string]any, bool) {
	var m map[string]any
	if !c.JSONObject(&m) || m == nil {
		return nil, false
	}
	return m, true
}

// Handler is one envelope-B endpoint. It returns the data object or an
// error; *APIError is written as is, any other error goes through MapError.
type Handler func(c *Req) (any, error)

// Raw is an endpoint that writes its own response.
type Raw func(c *Req)

type route struct {
	methods   []string
	fn        Handler
	raw       Raw
	limit     int64
	tooLarge  *APIError
	anonymous bool
	status    int // success status for Handler routes (default 200)
}

var (
	mu     sync.RWMutex
	routes = map[string]route{}
)

// Opt configures a route.
type Opt func(*route)

// Limit sets the body limit in bytes (default 64 KiB).
func Limit(n int64) Opt { return func(r *route) { r.limit = n } }

// TooLarge sets the error returned when the body exceeds the limit
// (default 413 REQUEST_TOO_LARGE "Размер запроса превышает допустимый предел.").
func TooLarge(e *APIError) Opt { return func(r *route) { r.tooLarge = e } }

// Anonymous skips the session check.
func Anonymous() Opt { return func(r *route) { r.anonymous = true } }

// Status sets the HTTP status of a successful Handler response (e.g. 202).
func Status(code int) Opt { return func(r *route) { r.status = code } }

// Methods sets the allowed methods (default GET). 3.x CGIs accept exactly one.
func Methods(m ...string) Opt { return func(r *route) { r.methods = m } }

// POST is shorthand for Methods("POST").
func POST() Opt { return Methods(http.MethodPost) }

// Register adds an envelope-B endpoint. Registering a path twice panics.
func Register(path string, fn Handler, opts ...Opt) { add(path, route{fn: fn}, opts) }

// RegisterRaw adds an endpoint that writes its own response.
func RegisterRaw(path string, fn Raw, opts ...Opt) { add(path, route{raw: fn}, opts) }

func add(path string, r route, opts []Opt) {
	r.methods = []string{http.MethodGet}
	r.limit = 64 << 10
	r.status = 200
	for _, o := range opts {
		o(&r)
	}
	if !strings.HasPrefix(path, "/api/") || !strings.HasSuffix(path, ".cgi") {
		panic("cgi: bad path " + path)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := routes[path]; dup {
		panic("cgi: duplicate route " + path)
	}
	routes[path] = r
}

// Paths lists the registered endpoints (sorted), for diagnostics and tests.
func Paths() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(routes))
	for p := range routes {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func lookup(path string) (route, bool) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := routes[path]
	return r, ok
}

// SetJSONHeaders sets the six headers of broray_api_print_json_headers.
func SetJSONHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate")
	h.Set("Pragma", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
}

// WriteJSON writes any JSON value with the standard headers.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	SetJSONHeaders(w)
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

// WriteOK writes the envelope-B success.
func WriteOK(w http.ResponseWriter, data any) { WriteOKStatus(w, 200, data) }

// WriteOKStatus writes the envelope-B success with a status (202 for accepted jobs).
func WriteOKStatus(w http.ResponseWriter, status int, data any) {
	WriteJSON(w, status, map[string]any{"success": true, "data": data, "error": nil})
}

// WriteErr writes the envelope-B failure.
func WriteErr(w http.ResponseWriter, e *APIError) {
	status := e.Status
	if status == 0 {
		status = 500
	}
	var details any = e.Details
	if s, ok := details.(string); ok && s == "" {
		details = nil
	}
	WriteJSON(w, status, map[string]any{"success": false, "data": nil, "error": map[string]any{"code": e.Code, "message": e.Message, "details": details}})
}

// MapError turns module errors of the form "CODE: text" into contract errors.
func MapError(err error) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	msg := err.Error()
	code, text := "INTERNAL_ERROR", msg
	if i := strings.Index(msg, ":"); i > 0 && strings.ToUpper(msg[:i]) == msg[:i] && !strings.Contains(msg[:i], " ") {
		code, text = msg[:i], strings.TrimSpace(msg[i+1:])
	}
	status := 400
	switch {
	case code == "INTERNAL_ERROR":
		status = 500
	case strings.HasSuffix(code, "_NOT_FOUND"):
		status = 404
	case strings.HasSuffix(code, "_BUSY") || strings.HasSuffix(code, "_CONFLICT"):
		status = 409
	case strings.HasSuffix(code, "_UNAVAILABLE"):
		status = 503
	}
	if text == "" {
		text = code
	}
	return &APIError{Status: status, Code: code, Message: text}
}

// Serve handles a /api/**.cgi request. It returns false if the path is not
// a registered route. auth may be nil (control socket: no session).
func Serve(w http.ResponseWriter, r *http.Request, path string, auth Authenticator) bool {
	rt, ok := lookup(path)
	if !ok {
		return false
	}
	c := &Req{W: w, R: r, Query: r.URL.Query(), RawQuery: r.URL.RawQuery}
	allowed := false
	for _, m := range rt.methods {
		if r.Method == m {
			allowed = true
		}
	}
	if !allowed {
		WriteErr(w, Ed(405, "METHOD_NOT_ALLOWED", "Для этой операции требуется метод "+rt.methods[0]+".", "Получен метод "+r.Method+"."))
		return true
	}
	if !rt.anonymous && auth != nil {
		s, ok := auth.Authenticate(w, r)
		if !ok {
			WriteErr(w, E(401, "AUTH_REQUIRED", "Требуется авторизация."))
			return true
		}
		c.User = s.User
	}
	if r.Body != nil {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, rt.limit))
		if err != nil {
			e := rt.tooLarge
			if e == nil {
				e = E(413, "REQUEST_TOO_LARGE", "Размер запроса превышает допустимый предел.")
			}
			var mbe *http.MaxBytesError
			if !errors.As(err, &mbe) {
				e = E(400, "REQUEST_BODY_INCOMPLETE", "Тело запроса получено не полностью.")
			}
			WriteErr(w, e)
			return true
		}
		c.body = b
	}
	if rt.raw != nil {
		rt.raw(c)
		return true
	}
	data, err := rt.fn(c)
	if err != nil {
		WriteErr(w, MapError(err))
		return true
	}
	WriteOKStatus(w, rt.status, data)
	return true
}

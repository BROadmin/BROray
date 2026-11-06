// Package web serves the WebUI, the 3.x-compatible auth endpoints and API v1.
// Credential checks go to KeeneticOS; sessions live only in RAM.
package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/session"
)

// Login backoff ported from BROmihomo internal/web/auth.go: KeeneticOS sees
// every BROray login as one client (router-local /auth), so each peer is kept
// below the router's own lockout threshold (5 in 3 min).
const (
	failureWindow = 3 * time.Minute
	failureBudget = 4
	failureMargin = 5 * time.Second
)

type failure struct {
	count  int
	next   time.Time
	last   time.Time
	recent []time.Time
}

type Auth struct {
	verifier keenetic.CredentialVerifier
	sessions *session.Store
	mu       sync.Mutex
	failures map[string]failure
	pending  map[string]bool
	now      func() time.Time
	inFlight chan struct{}
}

func NewAuth(v keenetic.CredentialVerifier, s *session.Store) *Auth {
	return &Auth{verifier: v, sessions: s, failures: map[string]failure{}, pending: map[string]bool{}, now: time.Now, inFlight: make(chan struct{}, 4)}
}

func peer(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a
}

func (h *Auth) reserve(key string) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending[key] {
		return time.Second, false
	}
	now := h.now()
	for k, f := range h.failures {
		if now.Sub(f.last) > 30*time.Minute {
			delete(h.failures, k)
		}
	}
	if _, ok := h.failures[key]; !ok && len(h.failures) >= 1024 {
		return 30 * time.Second, false
	}
	if d := h.failures[key].next.Sub(now); d > 0 {
		return d, false
	}
	if len(h.pending) >= cap(h.inFlight) {
		return time.Second, false
	}
	h.pending[key] = true
	return 0, true
}

func (h *Auth) release(key string) { h.mu.Lock(); defer h.mu.Unlock(); delete(h.pending, key) }

func (h *Auth) failed(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.failures) >= 1024 {
		if _, ok := h.failures[key]; !ok {
			return
		}
	}
	f := h.failures[key]
	if f.count < 6 {
		f.count++
	}
	d := time.Second * time.Duration(1<<uint(f.count-1))
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	f.last = h.now()
	f.next = f.last.Add(d)
	kept := f.recent[:0]
	for _, at := range f.recent {
		if f.last.Sub(at) < failureWindow {
			kept = append(kept, at)
		}
	}
	f.recent = append(kept, f.last)
	if len(f.recent) >= failureBudget {
		if until := f.recent[len(f.recent)-failureBudget].Add(failureWindow + failureMargin); until.After(f.next) {
			f.next = until
		}
	}
	h.failures[key] = f
}

func (h *Auth) succeeded(key string) { h.mu.Lock(); defer h.mu.Unlock(); delete(h.failures, key) }

// setSessionCookie writes the 3.x cookie (api/login.cgi:93, logout.cgi:22):
// BRORAY_SESSION=<token>; Path=/; HttpOnly; SameSite=Strict — a browser
// session cookie without Max-Age (the 1800 s TTL slides server-side).
// Cleared with Max-Age=0. Secure is added when the request arrived over
// HTTPS (KeenDNS), which the 3.x WebUI never sees.
func setSessionCookie(w http.ResponseWriter, token string, secure, expired bool) {
	c := &http.Cookie{Name: session.CookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure}
	if expired {
		c.MaxAge = -1 // "Max-Age=0"
	}
	http.SetCookie(w, c)
}

// sessionToken returns the first BRORAY_SESSION cookie value (lib/web-auth.sh:50-81).
func sessionToken(r *http.Request) string {
	c, err := r.Cookie(session.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// authJSON writes a flat auth (envelope A) object with the 3.x JSON headers.
func authJSON(w http.ResponseWriter, status int, v any) {
	cgi.WriteJSON(w, status, v)
}

// authError is the 3.x flat auth error: {"ok":false,"error":CODE[,"message":...]}.
func authError(w http.ResponseWriter, status int, code, message string) {
	body := map[string]any{"ok": false, "error": code}
	if message != "" {
		body["message"] = message
	}
	authJSON(w, status, body)
}

// jqRawString mirrors `jq -r '.field // empty'` for one JSON value: strings
// as is, numbers/true and containers as their JSON text, null/false empty.
func jqRawString(raw json.RawMessage) string {
	if len(raw) == 0 {
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

// Login handles POST /api/login.cgi (api/login.cgi): body checks and
// response bodies follow 3.x; the credential check (NDW2/NDW4 + admin
// rights, ported from BROmihomo) and the per-peer backoff are 3.5.0's.
func (h *Auth) Login(w http.ResponseWriter, r *http.Request, secure bool) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		authError(w, 415, "INVALID_REQUEST", "Требуется JSON.")
		return
	}
	if r.ContentLength <= 0 || r.ContentLength > 8192 {
		authError(w, 400, "INVALID_REQUEST", "")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	defer clear(body)
	if err != nil || int64(len(body)) != r.ContentLength {
		authError(w, 400, "REQUEST_BODY_INCOMPLETE", "")
		return
	}
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(body))
	if !utf8.Valid(body) || dec.Decode(&fields) != nil || fields == nil || dec.Decode(new(any)) != io.EOF {
		authError(w, 400, "REQUEST_JSON_INVALID", "")
		return
	}
	login := keenetic.NormalizeLogin(jqRawString(fields["login"]))
	password := jqRawString(fields["password"])
	for k := range fields {
		clear(fields[k])
	}
	if login == "" || password == "" {
		authError(w, 400, "FIELDS_REQUIRED", "Введите логин и пароль.")
		return
	}
	if utf8.RuneCountInString(login) > 128 || utf8.RuneCountInString(password) > 512 {
		authError(w, 400, "INVALID_REQUEST", "")
		return
	}
	key := peer(r).String()
	d, ok := h.reserve(key)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(d/time.Second)+1))
		authError(w, 429, "AUTH_BACKOFF", "Подождите перед следующей попыткой.")
		return
	}
	defer h.release(key)
	select {
	case h.inFlight <- struct{}{}:
		defer func() { <-h.inFlight }()
	default:
		authError(w, 429, "AUTH_BUSY", "Повторите позже.")
		return
	}
	err = h.verifier.Verify(r.Context(), login, password)
	password = ""
	clear(body)
	if err != nil {
		switch {
		case errors.Is(err, keenetic.ErrInvalidCredentials):
			h.failed(key)
			authError(w, 401, "INVALID_CREDENTIALS", "Неверный логин или пароль.")
		case errors.Is(err, keenetic.ErrForbidden):
			h.failed(key)
			authError(w, 403, "KEENETIC_ADMIN_RIGHTS_REQUIRED", "Нужны права администратора KeeneticOS, как для раздела OPKG: учётные записи «только чтение» и «менеджер» не допускаются.")
		case errors.Is(err, keenetic.ErrUnavailable):
			authError(w, 503, "KEENETIC_UNAVAILABLE", "Не удалось связаться с KeeneticOS.")
		case errors.Is(err, keenetic.ErrUnsupported):
			authError(w, 503, "KEENETIC_AUTH_UNSUPPORTED", "Эта схема входа KeeneticOS пока не поддерживается.")
		case errors.Is(err, keenetic.ErrResource):
			authError(w, 503, "KEENETIC_AUTH_RESOURCE_LIMIT", "Для безопасной проверки входа недостаточно свободной памяти.")
		case errors.Is(err, keenetic.ErrProtocol):
			authError(w, 502, "KEENETIC_AUTH_PROTOCOL_ERROR", "KeeneticOS не завершила проверку входа.")
		default:
			authError(w, 500, "INTERNAL_ERROR", "Внутренняя ошибка.")
		}
		return
	}
	h.succeeded(key)
	token, view, err := h.sessions.Create(login)
	if err != nil {
		authError(w, 500, "SESSION_CREATE_FAILED", "Не удалось создать сессию.")
		return
	}
	setSessionCookie(w, token, secure, false)
	authJSON(w, 200, map[string]any{"ok": true, "user": view.User, "redirect": "/home.html"})
}

// Session handles GET /api/session.cgi (api/session.cgi): validation slides
// the expiry; expiresAt is the already slid epoch.
func (h *Auth) Session(w http.ResponseWriter, r *http.Request, secure bool) {
	view, ok := h.sessions.Lookup(sessionToken(r))
	if !ok {
		authError(w, 401, "SESSION_REQUIRED", "Требуется авторизация.")
		return
	}
	authJSON(w, 200, map[string]any{"ok": true, "authenticated": true, "user": view.User, "expiresAt": view.Expires.Unix()})
}

// ProtectedTest handles GET /api/protected-test.cgi.
func (h *Auth) ProtectedTest(w http.ResponseWriter, r *http.Request) {
	view, ok := h.sessions.Lookup(sessionToken(r))
	if !ok {
		authError(w, 401, "SESSION_REQUIRED", "Требуется авторизация.")
		return
	}
	authJSON(w, 200, map[string]any{"ok": true, "protected": true, "user": view.User})
}

// Logout handles POST /api/logout.cgi: no session required; an absent or
// invalid token is a success (lib/web-auth.sh:122-125).
func (h *Auth) Logout(w http.ResponseWriter, r *http.Request, secure bool) {
	h.sessions.Delete(sessionToken(r))
	setSessionCookie(w, "", secure, true)
	authJSON(w, 200, map[string]bool{"ok": true})
}

// Require returns the session view (sliding its expiry) or ok=false.
func (h *Auth) Require(w http.ResponseWriter, r *http.Request, secure bool) (session.View, bool) {
	return h.sessions.Lookup(sessionToken(r))
}

// isSecure reports whether the request arrived over HTTPS (directly or via
// the KeenDNS proxy, which terminates TLS and forwards plain HTTP).
func isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

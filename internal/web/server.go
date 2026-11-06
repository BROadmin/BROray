package web

import (
	"encoding/json"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/session"
	"github.com/BROadmin/broray/webui"
)

// WebUIBuild is the build id of the embedded WebUI (<html data-build-id>
// of index.html, the cache-buster of every stylesheet and script). Page
// links and the login redirect use clean URLs (/home.html); 3.x appended
// ?v=<WebUIBuild> to them too (DEV5_WEBUI_PAGE_VERSION_QUERY).
var WebUIBuild = func() string {
	if b, err := fs.ReadFile(webui.Files, "static/index.html"); err == nil {
		if m := reBuildID.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return "WebUI-3.5.0-dev6"
}()

var reBuildID = regexp.MustCompile(`data-build-id="(WebUI-[A-Za-z0-9.\-]+)"`)

// Server is the HTTP handler for the LAN listener (and the control socket,
// where auth is skipped because the socket itself is root-only).
type Server struct {
	App    *app.App
	Auth   *Auth
	NoAuth bool // control socket
	assets fs.FS
}

func NewServer(a *app.App, verifier keenetic.CredentialVerifier) *Server {
	sub, _ := fs.Sub(webui.Files, "static")
	return &Server{App: a, Auth: NewAuth(verifier, session.NewStore()), assets: sub}
}

func NewControlServer(a *app.App) *Server {
	sub, _ := fs.Sub(webui.Files, "static")
	return &Server{App: a, NoAuth: true, assets: sub}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	secure := isSecure(r)
	p := path.Clean(r.URL.Path)
	if strings.HasPrefix(p, "/api/") && strings.HasSuffix(p, ".cgi") {
		var auth cgi.Authenticator
		if !s.NoAuth {
			auth = cgiAuth{s, secure}
		}
		if cgi.Serve(w, r, p, auth) {
			return
		}
	}
	if s.NoAuth {
		apiError(w, 404, "NOT_FOUND", "На управляющем сокете доступны только /api/**/*.cgi.")
		return
	}
	// 3.x auth CGIs (envelope A): one method each, 405 {"ok":false,"error":"METHOD_NOT_ALLOWED"}.
	switch p {
	case "/api/login.cgi", "/api/logout.cgi", "/api/session.cgi", "/api/protected-test.cgi":
		want := http.MethodGet
		if p == "/api/login.cgi" || p == "/api/logout.cgi" {
			want = http.MethodPost
		}
		if r.Method != want {
			authError(w, 405, "METHOD_NOT_ALLOWED", "")
			return
		}
		switch p {
		case "/api/login.cgi":
			s.Auth.Login(w, r, secure)
		case "/api/logout.cgi":
			s.Auth.Logout(w, r, secure)
		case "/api/session.cgi":
			s.Auth.Session(w, r, secure)
		default:
			s.Auth.ProtectedTest(w, r)
		}
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		apiError(w, 405, "METHOD_NOT_ALLOWED", "Метод не поддерживается.")
		return
	}
	// Static: login page and assets are public; pages need a session.
	public := p == "/" || p == "/index.html" || p == "/manifest.json" || strings.HasPrefix(p, "/assets/") || p == "/favicon.ico"
	if !public {
		if _, ok := s.Auth.Require(w, r, secure); !ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	if p == "/" {
		p = "/index.html"
	}
	s.static(w, r, p)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request, p string) {
	name := strings.TrimPrefix(p, "/")
	b, err := fs.ReadFile(s.assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	if strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".css") || strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".svg") {
		if !strings.Contains(ct, "charset") {
			ct += "; charset=utf-8"
		}
	}
	w.Header().Set("Content-Type", ct)
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(200)
		return
	}
	_, _ = w.Write(b)
}

// sameOrigin accepts a mutating request only when its Origin (or Referer)
// host equals the Host the request arrived on. KeenDNS forwards with the
// public host, so a browser on https://broray.<ndns>/ matches too.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return hostOnly(u.Host) == hostOnly(r.Host)
}

func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(h)
}

func apiOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

func apiError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]any{"code": code, "message": message}})
}

// cgiAuth adapts the session layer to cgi.Authenticator.
type cgiAuth struct {
	s      *Server
	secure bool
}

func (a cgiAuth) Authenticate(w http.ResponseWriter, r *http.Request) (cgi.Session, bool) {
	v, ok := a.s.Auth.Require(w, r, a.secure)
	if !ok {
		return cgi.Session{}, false
	}
	return cgi.Session{User: v.User}, true
}

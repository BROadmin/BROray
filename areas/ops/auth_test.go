package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/web"
)

type fakeVerifier struct{ err error }

func (f *fakeVerifier) Verify(_ context.Context, login, password string) error {
	if f.err != nil {
		return f.err
	}
	if login == "admin" && password == "secret" {
		return nil
	}
	return keenetic.ErrInvalidCredentials
}

type webResp struct {
	code   int
	header http.Header
	body   map[string]any
	raw    string
}

func serve(t *testing.T, h http.Handler, method, path, body string, hdr map[string]string, remote string) webResp {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	}
	if remote != "" {
		r.RemoteAddr = remote
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	out := webResp{code: w.Code, header: w.Header(), raw: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &out.body)
	return out
}

var reCookie = regexp.MustCompile(`^BRORAY_SESSION=[0-9a-f]{64}; Path=/; HttpOnly; SameSite=Strict$`)

func TestAuthEndpoints(t *testing.T) {
	testArea(t)
	v := &fakeVerifier{}
	srv := web.NewServer(nil, v)
	js := map[string]string{"Content-Type": "application/json"}
	cases := []struct {
		name, method, path, body string
		code                     int
		want                     string // exact JSON body
	}{
		{"login GET", "GET", "/api/login.cgi", "", 405, `{"error":"METHOD_NOT_ALLOWED","ok":false}`},
		{"session POST", "POST", "/api/session.cgi", "{}", 405, `{"error":"METHOD_NOT_ALLOWED","ok":false}`},
		{"logout GET", "GET", "/api/logout.cgi", "", 405, `{"error":"METHOD_NOT_ALLOWED","ok":false}`},
		{"protected POST", "POST", "/api/protected-test.cgi", "{}", 405, `{"error":"METHOD_NOT_ALLOWED","ok":false}`},
		{"empty body", "POST", "/api/login.cgi", "", 400, `{"error":"INVALID_REQUEST","ok":false}`},
		{"not json", "POST", "/api/login.cgi", `{"login":`, 400, `{"error":"REQUEST_JSON_INVALID","ok":false}`},
		{"array", "POST", "/api/login.cgi", `[]`, 400, `{"error":"REQUEST_JSON_INVALID","ok":false}`},
		{"no fields", "POST", "/api/login.cgi", `{"login":"admin"}`, 400, `{"error":"FIELDS_REQUIRED","message":"Введите логин и пароль.","ok":false}`},
		{"long login", "POST", "/api/login.cgi", `{"login":"` + strings.Repeat("a", 129) + `","password":"x"}`, 400, `{"error":"INVALID_REQUEST","ok":false}`},
		{"session none", "GET", "/api/session.cgi", "", 401, `{"error":"SESSION_REQUIRED","message":"Требуется авторизация.","ok":false}`},
		{"protected none", "GET", "/api/protected-test.cgi", "", 401, `{"error":"SESSION_REQUIRED","message":"Требуется авторизация.","ok":false}`},
		{"logout none", "POST", "/api/logout.cgi", "{}", 200, `{"ok":true}`},
	}
	for _, c := range cases {
		r := serve(t, srv, c.method, c.path, c.body, js, "")
		got, _ := json.Marshal(r.body)
		if r.code != c.code || string(got) != c.want {
			t.Errorf("%s: %d %s", c.name, r.code, r.raw)
		}
		if r.header.Get("Content-Type") != "application/json; charset=utf-8" || r.header.Get("Cache-Control") != "no-store, no-cache, must-revalidate" {
			t.Errorf("%s: headers %v", c.name, r.header)
		}
	}
	// wrong password → 401 INVALID_CREDENTIALS (from its own peer to keep the backoff out of the way)
	r := serve(t, srv, "POST", "/api/login.cgi", `{"login":"admin","password":"bad"}`, js, "192.0.2.10:1000")
	if r.code != 401 || r.body["error"] != "INVALID_CREDENTIALS" || r.body["message"] != "Неверный логин или пароль." {
		t.Fatalf("bad password %d %s", r.code, r.raw)
	}
	// 3.5.0 extra codes stay
	v.err = keenetic.ErrForbidden
	if r := serve(t, srv, "POST", "/api/login.cgi", `{"login":"admin","password":"secret"}`, js, "192.0.2.11:1000"); r.code != 403 || r.body["error"] != "KEENETIC_ADMIN_RIGHTS_REQUIRED" {
		t.Fatalf("rights %d %s", r.code, r.raw)
	}
	v.err = keenetic.ErrUnavailable
	if r := serve(t, srv, "POST", "/api/login.cgi", `{"login":"admin","password":"secret"}`, js, "192.0.2.12:1000"); r.code != 503 || r.body["error"] != "KEENETIC_UNAVAILABLE" ||
		r.body["message"] != "Не удалось связаться с KeeneticOS." {
		t.Fatalf("unavailable %d %s", r.code, r.raw)
	}
	v.err = nil
	r = serve(t, srv, "POST", "/api/login.cgi", `{"login":" admin ","password":"secret"}`, js, "192.0.2.13:1000")
	if r.code != 200 || r.body["ok"] != true || r.body["user"] != "admin" || r.body["redirect"] != "/home.html" || len(r.body) != 3 {
		t.Fatalf("login %d %s", r.code, r.raw)
	}
	set := r.header.Get("Set-Cookie")
	if !reCookie.MatchString(set) {
		t.Fatalf("cookie %q", set)
	}
	cookie := strings.SplitN(set, ";", 2)[0]
	withCookie := map[string]string{"Cookie": cookie}
	r = serve(t, srv, "GET", "/api/session.cgi", "", withCookie, "")
	if r.code != 200 || r.body["authenticated"] != true || r.body["user"] != "admin" || r.body["expiresAt"] == nil || r.header.Get("Set-Cookie") != "" {
		t.Fatalf("session %d %s %v", r.code, r.raw, r.header)
	}
	r = serve(t, srv, "GET", "/api/protected-test.cgi", "", withCookie, "")
	if r.code != 200 || r.body["protected"] != true || r.body["user"] != "admin" {
		t.Fatalf("protected %s", r.raw)
	}
	// session-protected CGI through the real auth layer
	if r := serve(t, srv, "GET", "/api/home/summary.cgi", "", nil, ""); r.code != 401 || r.body["error"].(map[string]any)["code"] != "AUTH_REQUIRED" {
		t.Fatalf("summary without session %d %s", r.code, r.raw)
	}
	if r := serve(t, srv, "GET", "/api/home/summary.cgi", "", withCookie, ""); r.code != 200 || r.body["success"] != true {
		t.Fatalf("summary with session %d", r.code)
	}
	// uppercase token is not a valid session
	up := "BRORAY_SESSION=" + strings.ToUpper(strings.TrimPrefix(cookie, "BRORAY_SESSION="))
	if r := serve(t, srv, "GET", "/api/session.cgi", "", map[string]string{"Cookie": up}, ""); r.code != 401 {
		t.Fatal("uppercase token accepted")
	}
	// logout clears the cookie and revokes the session
	r = serve(t, srv, "POST", "/api/logout.cgi", "{}", map[string]string{"Cookie": cookie, "Content-Type": "application/json"}, "")
	if r.code != 200 || r.header.Get("Set-Cookie") != "BRORAY_SESSION=; Path=/; Max-Age=0; HttpOnly; SameSite=Strict" {
		t.Fatalf("logout %d %v", r.code, r.header)
	}
	if r := serve(t, srv, "GET", "/api/session.cgi", "", withCookie, ""); r.code != 401 {
		t.Fatal("session survived logout")
	}
}

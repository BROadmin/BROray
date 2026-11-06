package keenetic

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeKeenetic struct {
	mu                                            sync.Mutex
	gets, posts                                   int
	login, password, realm, challenge, cookieName string
	missing                                       string
	getDelay, postDelay                           time.Duration
	postStatus                                    int
	cookieOK                                      bool
	inherited                                     bool
	rights                                        rciFake
}

func newFake() *fakeKeenetic {
	return &fakeKeenetic{login: "test", password: "SyntheticPassword", realm: "Synthetic Realm", challenge: "SyntheticChallenge", cookieName: "RANDOM_NDM_COOKIE", postStatus: 200,
		rights: newRCIFake("RANDOM_NDM_COOKIE", "test", []string{"cli", "http"})}
}
func independentDigest(login, realm, password, challenge string) string {
	a := md5.Sum([]byte(login + ":" + realm + ":" + password))
	b := sha256.Sum256([]byte(challenge + hex.EncodeToString(a[:])))
	return hex.EncodeToString(b[:])
}
func (f *fakeKeenetic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	handled := f.rights.serve(w, r)
	f.mu.Unlock()
	if handled {
		return
	}
	if r.URL.Path != "/auth" {
		w.WriteHeader(404)
		return
	}
	if r.Method == "GET" {
		f.mu.Lock()
		f.gets++
		if len(r.Cookies()) != 0 {
			f.inherited = true
		}
		number := f.gets
		f.mu.Unlock()
		time.Sleep(f.getDelay)
		w.Header().Set("WWW-Authenticate", "x-ndw2-interactive")
		if f.missing != "realm" {
			w.Header().Set("X-NDM-Realm", f.realm)
		}
		if f.missing != "challenge" {
			w.Header().Set("X-NDM-Challenge", f.challenge)
		}
		if f.missing != "cookie" {
			http.SetCookie(w, &http.Cookie{Name: f.cookieName, Value: fmt.Sprintf("Synthetic-%d", number), Path: "/", HttpOnly: true})
		}
		w.WriteHeader(401)
		return
	}
	f.mu.Lock()
	f.posts++
	number := f.gets
	f.mu.Unlock()
	time.Sleep(f.postDelay)
	c, e := r.Cookie(f.cookieName)
	hasCookie := e == nil && c.Value == fmt.Sprintf("Synthetic-%d", number)
	f.mu.Lock()
	f.cookieOK = hasCookie
	f.mu.Unlock()
	var data map[string]string
	e = json.NewDecoder(r.Body).Decode(&data)
	if !hasCookie || e != nil || len(data) != 2 || data["login"] != f.login || data["password"] != independentDigest(f.login, f.realm, f.password, f.challenge) || r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(401)
		return
	}
	w.WriteHeader(f.postStatus)
}
func setup(t *testing.T, f *fakeKeenetic) *Verifier {
	t.Helper()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	u, _ := url.Parse(s.URL + "/auth")
	return &Verifier{endpoint: *u, requestTimeout: time.Second, connectTimeout: time.Second}
}
func verify(t *testing.T, f *fakeKeenetic, want error) {
	t.Helper()
	v := setup(t, f)
	err := v.Verify(context.Background(), f.login, f.password)
	if !errors.Is(err, want) {
		t.Fatalf("typed result=%v want=%v", err, want)
	}
}

func TestAUTH001Success(t *testing.T) {
	f := newFake()
	verify(t, f, nil)
	if f.gets != 1 || f.posts != 1 || !f.cookieOK {
		t.Fatal("handshake not continuous")
	}
}
func TestAUTH002Invalid(t *testing.T) {
	f := newFake()
	v := setup(t, f)
	if e := v.Verify(context.Background(), f.login, "SyntheticWrong"); !errors.Is(e, ErrInvalidCredentials) {
		t.Fatal(e)
	}
	if f.posts != 1 {
		t.Fatal("retry")
	}
}
func TestAUTH003CookieRequired(t *testing.T) {
	f := newFake()
	v := setup(t, f)
	b, _ := json.Marshal(map[string]string{"login": f.login, "password": independentDigest(f.login, f.realm, f.password, f.challenge)})
	r, e := http.Post(v.endpoint.String(), "application/json", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("fake server did not reject missing cookie")
	}
	f.missing = "cookie"
	if e = v.Verify(context.Background(), f.login, f.password); !errors.Is(e, ErrProtocol) {
		t.Fatal(e)
	}
	if f.posts != 1 {
		t.Fatal("adapter posted without cookie")
	}
}
func TestAUTH004RandomCookieName(t *testing.T) {
	for _, name := range []string{"NDM_a98Z", "another_731COOKIE"} {
		t.Run(name, func(t *testing.T) {
			f := newFake()
			f.cookieName = name
			verify(t, f, nil)
			if !f.cookieOK {
				t.Fatal("cookie name hardcoded")
			}
		})
	}
}
func TestAUTH005RealmSpaces(t *testing.T) {
	f := newFake()
	f.realm = "Realm  with multiple spaces"
	verify(t, f, nil)
}
func TestAUTH006UnicodeLogin(t *testing.T) {
	f := newFake()
	f.login = "Пользовательé"
	verify(t, f, nil)
}
func TestAUTH007UnicodePassword(t *testing.T) {
	f := newFake()
	f.password = "  Пароль✓e\u0301  "
	verify(t, f, nil)
}
func TestAUTH008MissingRealm(t *testing.T) {
	f := newFake()
	f.missing = "realm"
	verify(t, f, ErrProtocol)
	if f.posts != 0 {
		t.Fatal("POST despite missing realm")
	}
}
func TestAUTH009MissingChallenge(t *testing.T) {
	f := newFake()
	f.missing = "challenge"
	verify(t, f, ErrProtocol)
	if f.posts != 0 {
		t.Fatal("POST despite missing challenge")
	}
}
func TestAUTH010GetTimeout(t *testing.T) {
	f := newFake()
	f.getDelay = 100 * time.Millisecond
	v := setup(t, f)
	v.requestTimeout = 20 * time.Millisecond
	if e := v.Verify(context.Background(), f.login, f.password); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if f.posts != 0 {
		t.Fatal("POST after timeout")
	}
}
func TestAUTH011PostTimeout(t *testing.T) {
	f := newFake()
	f.postDelay = 100 * time.Millisecond
	v := setup(t, f)
	v.requestTimeout = 20 * time.Millisecond
	if e := v.Verify(context.Background(), f.login, f.password); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatal("POST retry")
	}
}
func TestAUTH012UnexpectedStatus(t *testing.T) {
	for _, status := range []int{204, 302, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFake()
			f.postStatus = status
			verify(t, f, ErrProtocol)
			if f.posts != 1 {
				t.Fatal("retry")
			}
		})
	}
}
func TestAUTH013PasswordNotLogged(t *testing.T) {
	f := newFake()
	f.postStatus = 500
	v := setup(t, f)
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	e := v.Verify(context.Background(), f.login, f.password)
	if e == nil || strings.Contains(e.Error(), f.password) || output.Len() != 0 {
		t.Fatal("secret-bearing error/log")
	}
}
func TestAUTH014ProtocolSecretsNotLogged(t *testing.T) {
	f := newFake()
	f.missing = "realm"
	v := setup(t, f)
	e := v.Verify(context.Background(), f.login, f.password)
	if e != ErrProtocol {
		t.Fatal("unexpected error")
	}
	for _, s := range []string{f.challenge, f.cookieName, "Synthetic-1", independentDigest(f.login, f.realm, f.password, f.challenge)} {
		if strings.Contains(e.Error(), s) {
			t.Fatal("secret in error")
		}
	}
}
func TestAUTH015NoCookieReuse(t *testing.T) {
	f := newFake()
	v := setup(t, f)
	for i := 0; i < 2; i++ {
		if e := v.Verify(context.Background(), f.login, f.password); e != nil {
			t.Fatal(e)
		}
	}
	if f.inherited || f.gets != 2 || f.posts != 2 {
		t.Fatal("auth context leaked")
	}
}
func TestAUTH016FreshContextAfterFailure(t *testing.T) {
	f := newFake()
	v := setup(t, f)
	_ = v.Verify(context.Background(), f.login, "SyntheticWrong")
	if e := v.Verify(context.Background(), f.login, f.password); e != nil {
		t.Fatal(e)
	}
	if f.inherited || !f.cookieOK {
		t.Fatal("context was retained")
	}
}
func TestAUTH017GoldenVector(t *testing.T) {
	const expected = "c115872f8dc1852efcc8d1fa157469f319bd7fd8a101282f88a6e3382e14b873"
	if responseDigest("test", "SyntheticП пароль", "Realm with spaces", "SyntheticChallenge") != expected {
		t.Fatal("BROray-compatible UTF-8 golden vector mismatch")
	}
	a := md5.Sum([]byte("test:Realm with spaces:SyntheticП пароль"))
	if hex.EncodeToString(a[:]) != "12c8df0d2b5b3c79748ddd8fc98f46ea" {
		t.Fatal("lowercase MD5 vector mismatch")
	}
}
func TestRedirectAndEnvironmentProxyDisabled(t *testing.T) {
	var proxyCalls, targetCalls int
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls++; w.WriteHeader(500) }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.WriteHeader(500) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	u, _ := url.Parse(redirect.URL + "/auth")
	v := Verifier{endpoint: *u, requestTimeout: time.Second, connectTimeout: time.Second}
	if e := v.Verify(context.Background(), "test", "SyntheticPassword"); e != ErrProtocol {
		t.Fatal(e)
	}
	if targetCalls != 0 || proxyCalls != 0 {
		t.Fatal("unexpected redirect/proxy")
	}
}
func TestInvalidUTF8NoNetwork(t *testing.T) {
	f := newFake()
	v := setup(t, f)
	if e := v.Verify(context.Background(), "test", string([]byte{255})); e != ErrInvalidCredentials {
		t.Fatal(e)
	}
	if f.gets != 0 {
		t.Fatal("unexpected network")
	}
}
func TestResponseBodyNotReturned(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "SyntheticCookieAndPasswordValue")
	}))
	defer s.Close()
	u, _ := url.Parse(s.URL + "/auth")
	v := Verifier{endpoint: *u, requestTimeout: time.Second, connectTimeout: time.Second}
	if e := v.Verify(context.Background(), "test", "SyntheticPassword"); e != ErrProtocol {
		t.Fatal("upstream content exposed")
	}
}

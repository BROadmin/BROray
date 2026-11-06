package ops

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
)

// testArea installs a fresh ops area on a temporary root (no App: no
// KeeneticOS, no Xray) and points the installer prefix at a temp dir.
func testArea(t *testing.T) *area {
	t.Helper()
	a, _ := testAreaCtx(t, platform.Root{Dir: filepath.Join(t.TempDir(), "broray")})
	return a
}

// testAreaCtx is testArea on a given root; cancel simulates the daemon
// stopping (its background goroutines end).
func testAreaCtx(t *testing.T, root platform.Root) (*area, context.CancelFunc) {
	t.Helper()
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d := &compat.Deps{Root: root, Log: log.New(io.Discard, "", 0), Ctx: ctx, Svc: &compat.Services{}}
	d.Lock = compat.NewGlobalLock(d.RunPath("global-operation.json"))
	d.Queue = compat.NewCoordinator(d)
	oldOpt := installer.Opt
	installer.Opt = filepath.Join(t.TempDir(), "opt")
	t.Cleanup(func() { installer.Opt = oldOpt })
	a := newArea(d)
	install(a)
	return a, cancel
}

type resp struct {
	Code   int
	Header http.Header
	Body   map[string]any
	Raw    string
}

// call sends a request through cgi.Serve (no session layer).
func call(t *testing.T, method, path, body string, hdr map[string]string) resp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	target := path
	r := httptest.NewRequest(method, target, rd)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	p := path
	if i := strings.Index(p, "?"); i >= 0 {
		p = p[:i]
	}
	if p == "/api/broray/uninstall.cgi" {
		// Normal uninstall fixtures represent direct LAN access. Public-host
		// rejection is covered separately in uninstall_remote_api_test.go.
		r.Host = "192.168.1.1"
		r.RemoteAddr = "192.168.1.50:12345"
		r.Header.Set("Origin", "http://192.168.1.1")
	}
	if !cgi.Serve(w, r, p, nil) {
		t.Fatalf("route %s not registered", p)
	}
	out := resp{Code: w.Code, Header: w.Header(), Raw: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &out.Body)
	return out
}

var brorayHdr = map[string]string{"X-BROray-Request": "1", "Content-Type": "application/json", "Origin": "http://example.com"}

func opsHdr() map[string]string {
	return map[string]string{"X-BROray-Request": "operations", "Content-Type": "application/json", "Origin": "http://example.com"}
}

// errCode returns the envelope-B, C or D error code of a response.
func errCode(r resp) string {
	switch e := r.Body["error"].(type) {
	case map[string]any:
		s, _ := e["code"].(string)
		return s
	case string:
		return e
	}
	s, _ := r.Body["errorCode"].(string)
	return s
}

func errMessage(r resp) string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		s, _ := e["message"].(string)
		return s
	}
	return ""
}

func data(r resp) map[string]any {
	m, _ := r.Body["data"].(map[string]any)
	return m
}

func writeFile(t *testing.T, path string, b []byte, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		at := time.Now().Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
)

const cloneDir = "/home/claude/rockblack-vpn/ip-address"

// fakeRouter simulates Keenetic running/startup configuration and the
// static-route commands.
type fakeRouter struct {
	mu        sync.Mutex
	header    []string
	routes    []string
	startup   []string
	ifaceUp   bool
	failCmd   func(cmd string, n int) bool // command fails (not applied)
	noEffect  func(cmd string) bool        // command "succeeds" without effect
	rcErr     error
	cmdCount  int
	commands  []string
	saveCount int
}

func newFakeRouter() *fakeRouter {
	f := &fakeRouter{ifaceUp: true, header: []string{
		"! $$$ Model: Keenetic", "! $$$ Last change: today", "!", "interface Proxy0", "    description BROray", "    proxy protocol socks5", "    up", "!",
		"ip route 8.8.8.0 255.255.255.0 Wireguard0 auto",
	}}
	f.startup = f.running()
	return f
}

func (f *fakeRouter) running() []string {
	out := append([]string(nil), f.header...)
	out = append(out, f.routes...)
	return append(out, "!")
}

func (f *fakeRouter) Name() string { return "fake" }

func (f *fakeRouter) RunningConfig(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rcErr != nil {
		return nil, f.rcErr
	}
	return f.running(), nil
}

func (f *fakeRouter) StartupConfig(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rcErr != nil {
		return nil, f.rcErr
	}
	return append([]string(nil), f.startup...), nil
}

func (f *fakeRouter) Show(ctx context.Context, path string, out any) error {
	if path != "show/interface/Proxy0" {
		return errors.New("no such interface")
	}
	st := map[string]string{"state": "down", "connected": "no"}
	if f.ifaceUp {
		st = map[string]string{"state": "up", "connected": "yes"}
	}
	b, _ := json.Marshal(st)
	return json.Unmarshal(b, out)
}

func routeLine(net, mask, iface, metric string) string {
	if mask == "255.255.255.255" {
		return "ip route " + net + " " + iface + " " + metric
	}
	return "ip route " + net + " " + mask + " " + iface + " " + metric
}

func (f *fakeRouter) Parse(ctx context.Context, cmds []string) ([]keenetic.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]keenetic.Result, 0, len(cmds))
	for i, c := range cmds {
		f.cmdCount++
		f.commands = append(f.commands, c)
		if f.failCmd != nil && f.failCmd(c, f.cmdCount) {
			out = append(out, keenetic.Result{Command: c, OK: false, Status: []keenetic.Status{{Status: "error", Message: "simulated failure"}}})
			for _, rest := range cmds[i+1:] {
				out = append(out, keenetic.Result{Command: rest, OK: false, Status: []keenetic.Status{{Status: "error", Code: "NOT_RUN", Message: "not run"}}})
			}
			return out, nil
		}
		if f.noEffect != nil && f.noEffect(c) {
			out = append(out, keenetic.Result{Command: c, OK: true})
			continue
		}
		fs := strings.Fields(c)
		switch {
		case c == "system configuration save":
			f.saveCount++
			f.startup = f.running()
		case len(fs) == 6 && fs[0] == "ip":
			line := routeLine(fs[2], fs[3], fs[4], fs[5])
			exists := false
			for _, r := range f.routes {
				if r == line {
					exists = true
				}
			}
			if !exists {
				f.routes = append(f.routes, line)
			}
		case len(fs) == 6 && fs[0] == "no":
			var keep []string
			for _, r := range f.routes {
				rf := strings.Fields(r)
				hostForm := fs[4] == "255.255.255.255" && len(rf) >= 4 && rf[2] == fs[3] && rf[3] == fs[5]
				netForm := len(rf) >= 5 && rf[2] == fs[3] && rf[3] == fs[4] && rf[4] == fs[5]
				if hostForm || netForm {
					continue
				}
				keep = append(keep, r)
			}
			f.routes = keep
		default:
			out = append(out, keenetic.Result{Command: c, OK: false, Status: []keenetic.Status{{Status: "error", Message: "unknown command"}}})
			continue
		}
		out = append(out, keenetic.Result{Command: c, OK: true})
	}
	return out, nil
}

func (f *fakeRouter) routeCount(iface string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.routes {
		if strings.Contains(r, " "+iface+" ") {
			n++
		}
	}
	return n
}

func (f *fakeRouter) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.routes...)
}

// ---------- test engine ----------

type testEnv struct {
	t      *testing.T
	e      *engine
	d      *compat.Deps
	r      *fakeRouter
	srv    *httptest.Server
	served map[string][]byte // overrides of served catalog files
}

func fastSave(ctx context.Context, t keenetic.Transport) ([]string, error) {
	res, err := t.Parse(ctx, []string{"system configuration save"})
	if err != nil {
		return nil, err
	}
	if len(res) != 1 || !res[0].OK {
		return nil, errors.New("save failed")
	}
	run, err := t.RunningConfig(ctx)
	if err != nil {
		return nil, err
	}
	st, err := t.StartupConfig(ctx)
	if err != nil {
		return nil, err
	}
	if strings.Join(run, "\n") != strings.Join(st, "\n") {
		return nil, errors.New("KEENETIC_SAVE_UNCONFIRMED")
	}
	return run, nil
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	sleepFn = func(ctx context.Context, d time.Duration) error { return nil }
	saveFn = fastSave
	convergeAttempts = 3
	verifyBatchSize = 16
	nowFn = time.Now
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	r := newFakeRouter()
	a := &app.App{Root: root, T: r}
	d := &compat.Deps{Root: root, App: a, Log: log.New(io.Discard, "", 0), Ctx: context.Background(), Svc: &compat.Services{}}
	d.Lock = compat.NewGlobalLock(d.RunPath("global-operation.json"))
	d.Queue = compat.NewCoordinator(d)
	env := &testEnv{t: t, d: d, r: r, served: map[string][]byte{}}
	env.srv = httptest.NewServer(http.HandlerFunc(env.serveRaw))
	t.Cleanup(env.srv.Close)
	e := newEngine(d)
	e.rawBase = env.srv.URL
	e.http = env.srv.Client()
	if err := e.prepare(); err != nil {
		t.Fatal(err)
	}
	env.e = e
	setCurrent(e)
	registerHandlers()
	return env
}

func (env *testEnv) serveRaw(w http.ResponseWriter, r *http.Request) {
	p, _ := url.PathUnescape(r.URL.EscapedPath())
	prefix := "/" + catalogRepository + "/" + catalogCommit + "/"
	if !strings.HasPrefix(p, prefix) {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(p, prefix)
	if b, ok := env.served[rel]; ok {
		_, _ = w.Write(b)
		return
	}
	b, err := os.ReadFile(filepath.Join(cloneDir, rel))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(b)
}

type resp struct {
	status int
	body   map[string]any
	raw    string
}

func (r resp) data() map[string]any {
	d, _ := r.body["data"].(map[string]any)
	return d
}

func (r resp) code() string {
	e, _ := r.body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (r resp) message() string {
	e, _ := r.body["error"].(map[string]any)
	s, _ := e["message"].(string)
	return s
}

func (r resp) details() any {
	e, _ := r.body["error"].(map[string]any)
	return e["details"]
}

func (env *testEnv) call(method, target string, body any) resp {
	env.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req := httptest.NewRequest(method, target, rd)
	w := httptest.NewRecorder()
	path := target
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if !cgi.Serve(w, req, path, nil) {
		env.t.Fatalf("route %s not registered", path)
	}
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return resp{status: w.Code, body: m, raw: w.Body.String()}
}

func (env *testEnv) post(target string) resp { return env.call("POST", target, "{}") }
func (env *testEnv) get(target string) resp  { return env.call("GET", target, nil) }

func (env *testEnv) mustOK(r resp) map[string]any {
	env.t.Helper()
	if r.status != 200 || r.body["success"] != true {
		env.t.Fatalf("expected success, got %d %s", r.status, r.raw)
	}
	return r.data()
}

func (env *testEnv) wantErr(r resp, status int, code string) {
	env.t.Helper()
	if r.status != status || r.code() != code {
		env.t.Fatalf("want %d %s, got %d %s", status, code, r.status, r.raw)
	}
}

// installBundle runs check → download → preflight → export.
func (env *testEnv) installBundle(id string) map[string]any {
	env.t.Helper()
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=" + id))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=" + id))
	return env.exportBundle(id)
}

func (env *testEnv) preflightToken(id, action string) string {
	env.t.Helper()
	pf := env.mustOK(env.post("/api/routes/preflight.cgi?bundleId=" + id + "&action=" + action))
	if pf["ready"] != true {
		env.t.Fatalf("preflight not ready: %v", pf)
	}
	return pf["token"].(string)
}

func (env *testEnv) exportBundle(id string) map[string]any {
	env.t.Helper()
	tok := env.preflightToken(id, "export")
	return env.mustOK(env.post("/api/routes/export.cgi?bundleId=" + id + "&preflightToken=" + tok))
}

func (env *testEnv) state(id string) obj {
	s, _ := readObj(env.e.stateFile(id))
	return s
}

func readCloneFile(dir, name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(cloneDir, dir, name))
}

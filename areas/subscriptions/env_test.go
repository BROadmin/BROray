package subscriptions

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

type env struct {
	d           *compat.Deps
	s           *svc
	mu          sync.Mutex
	reapplied   []string
	reapplyErr  error
	rollbackErr error
	ensured     int
	ensureErr   error
	testErr     error
	dns         map[string][]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &compat.Deps{Root: root, Log: log.New(io.Discard, "", 0), Ctx: ctx, Svc: &compat.Services{}, Maintenance: true}
	d.Lock = compat.NewGlobalLock(d.RunPath("global-operation.json"))
	d.Queue = compat.NewCoordinator(d)
	e := &env{d: d, dns: map[string][]string{}}
	if err := setup(d); err != nil {
		t.Fatal(err)
	}
	e.s = cur()
	t.Cleanup(func() { cancel(); e.s.wg.Wait() })
	d.Svc.ServersReapplyActive = func(ctx context.Context, reason string) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.reapplied = append(e.reapplied, reason)
		if reason == "subscription-rollback" {
			return e.rollbackErr
		}
		return e.reapplyErr
	}
	d.Svc.ServersWithdrawActive = func(ctx context.Context) (func(context.Context) error, error) {
		previous := servers.ActiveID(d)
		undo := func(context.Context) error { return e.s.writeActive(previous) }
		return undo, os.Remove(servers.ActiveFile(d))
	}
	d.Svc.KeeneticEnsureInterface = func(ctx context.Context) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.ensured++
		err := e.ensureErr
		e.ensureErr = nil // one failed change; rollback uses the restored state
		return err
	}
	oldAllowed, oldLookup, oldTest, oldShow := addrAllowed, lookupIP, testActiveConfig, showVersion
	addrAllowed = func(netip.Addr) bool { return true }
	lookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
		var out []netip.Addr
		for _, a := range e.dns[host] {
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}
	testActiveConfig = func(context.Context, *compat.Deps, servers.Record) error { return e.testErr }
	showVersion = func(context.Context, *svc) (map[string]any, bool) {
		return map[string]any{"hw_id": "KN-1010", "vendor": "Keenetic", "title": "4.2.3"}, true
	}
	t.Cleanup(func() { addrAllowed, lookupIP, testActiveConfig, showVersion = oldAllowed, oldLookup, oldTest, oldShow })
	return e
}

type resp struct {
	Status int
	Body   map[string]any
	Raw    string
}

func (r resp) data() map[string]any { m, _ := r.Body["data"].(map[string]any); return m }
func (r resp) errCode() string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}
func (r resp) errMsg() string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		return e["message"].(string)
	}
	return ""
}

func call(t *testing.T, method, target, body string, hdr map[string]string) resp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, target, rd)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	if cl, ok := hdr["X-Test-Content-Length"]; ok {
		if cl == "-1" {
			r.ContentLength = -1
		} else {
			r.ContentLength = int64(len(body) + len(cl))
		}
	}
	w := httptest.NewRecorder()
	if !cgi.Serve(w, r, r.URL.Path, nil) {
		t.Fatalf("route %s not registered", r.URL.Path)
	}
	out := resp{Status: w.Code, Raw: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &out.Body)
	return out
}

// provider is a fake subscription server.
type provider struct {
	*httptest.Server
	mu      sync.Mutex
	body    string
	headers map[string]string
	status  int
	seen    []http.Header
	block   chan struct{}
}

func newProvider(t *testing.T, body string) *provider {
	p := &provider{body: body, headers: map[string]string{}, status: 200}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.seen = append(p.seen, r.Header.Clone())
		body, status, block := p.body, p.status, p.block
		for k, v := range p.headers {
			w.Header().Set(k, v)
		}
		p.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *provider) set(body string) { p.mu.Lock(); p.body = body; p.mu.Unlock() }

func (p *provider) setStatus(code int) { p.mu.Lock(); p.status = code; p.mu.Unlock() }

func (p *provider) setBlock(ch chan struct{}) { p.mu.Lock(); p.block = ch; p.mu.Unlock() }

func (p *provider) header(i int) http.Header {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen[i]
}

func (e *env) create(t *testing.T, url string, extra string) string {
	t.Helper()
	body := `{"name":"Мой провайдер","url":"` + url + `","updateImmediately":false` + extra + `}`
	r := call(t, "POST", "/api/subscriptions/create.cgi", body, nil)
	if r.Status != 200 {
		t.Fatalf("create: %d %s", r.Status, r.Raw)
	}
	return r.data()["id"].(string)
}

func (e *env) record(t *testing.T, id string) map[string]any {
	t.Helper()
	rec, _, err := e.s.readRecord(id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// refreshSync runs the update directly under the fence (as the apply stage does).
func (e *env) refreshSync(t *testing.T, id string) error {
	t.Helper()
	release, ok := e.d.Lock.TryAcquire("subscriptions", "test", "")
	if !ok {
		t.Fatal("fence busy")
	}
	defer release()
	op := e.d.Queue.StartOperation("", "subscription_update", "subscriptions:refresh", "USER", "cooperative", "")
	err := e.s.update(e.d.Ctx, op, id, "manual", nil)
	op.Finish(err)
	return err
}

func liveIDs(t *testing.T, e *env) []string {
	ids, _ := servers.RecordIDs(e.d)
	return ids
}

func disabledIDs(e *env, sub string) []string {
	var out []string
	for _, f := range e.s.disabledServers(sub) {
		out = append(out, f.id)
	}
	return out
}

func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func exists2(p string) bool { _, err := os.Lstat(p); return err == nil }

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// startWorker runs the coordinator worker until the returned stop is called.
// Submits happen while it is stopped (compat.ServeQueued reads the receipt
// after Submit released the coordinator lock).
func (e *env) startWorker() func() {
	ctx, cancel := context.WithCancel(e.d.Ctx)
	done := make(chan struct{})
	go func() { e.d.Queue.Run(ctx); close(done) }()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }
}

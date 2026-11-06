package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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

// fakeKeenetic models the DoT part of a router: a flat list of upstream lines.
type fakeKeenetic struct {
	mu        sync.Mutex
	dot       []string // "dns-proxy tls upstream ..." lines (running)
	doh       []string
	extra     []string // other running-config lines
	commands  []string
	reject    map[string]bool // command → rejected
	failRead  bool
	failShow  bool
	runtimeFn func(dot []string) []string // override show dns-proxy
}

func (f *fakeKeenetic) Name() string { return "fake" }

func (f *fakeKeenetic) Parse(ctx context.Context, cmds []string) ([]keenetic.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []keenetic.Result
	for _, c := range cmds {
		f.commands = append(f.commands, c)
		if f.reject[c] {
			out = append(out, keenetic.Result{Command: c, OK: false, Status: []keenetic.Status{{Status: "error", Message: "rejected"}}})
			continue
		}
		switch {
		case strings.HasPrefix(c, "no dns-proxy tls upstream "):
			p := strings.Fields(c)
			var kept []string
			for _, l := range f.dot {
				q := strings.Fields(l)
				port := "853"
				if len(q) > 4 && reDigits.MatchString(q[4]) {
					port = q[4]
				}
				if q[3] == p[4] && port == p[5] {
					continue
				}
				kept = append(kept, l)
			}
			f.dot = kept
		case strings.HasPrefix(c, "dns-proxy tls upstream "):
			f.dot = append(f.dot, c)
		}
		out = append(out, keenetic.Result{Command: c, OK: true})
	}
	return out, nil
}

func (f *fakeKeenetic) RunningConfig(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRead {
		return nil, errors.New("rci down")
	}
	lines := []string{"! $$$ Model: Test", "system", "    hostname test", "!"}
	lines = append(lines, f.extra...)
	lines = append(lines, "dns-proxy")
	for _, l := range f.dot {
		lines = append(lines, "    "+strings.TrimPrefix(l, "dns-proxy "))
	}
	for _, l := range f.doh {
		lines = append(lines, "    "+strings.TrimPrefix(l, "dns-proxy "))
	}
	lines = append(lines, "!")
	return lines, nil
}

func (f *fakeKeenetic) StartupConfig(ctx context.Context) ([]string, error) {
	return f.RunningConfig(ctx)
}

func (f *fakeKeenetic) Show(ctx context.Context, path string, out any) error {
	return errors.New("no rci")
}

func (f *fakeKeenetic) ShowText(ctx context.Context, cmd string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cmd != "show dns-proxy" || f.failShow {
		return nil, errors.New("show failed")
	}
	if f.runtimeFn != nil {
		return f.runtimeFn(f.dot), nil
	}
	lines := []string{"proxy-status:", "    proxy-name: System", "    proxy-tls:"}
	for _, l := range f.dot {
		q := strings.Fields(l)
		lines = append(lines, "        server-tls:", "            address: "+q[3])
		i := 4
		if i < len(q) && reDigits.MatchString(q[i]) {
			lines = append(lines, "            port: "+q[i])
			i++
		}
		for ; i+1 < len(q); i += 2 {
			key := q[i]
			if key == "on" {
				key = "interface"
			}
			lines = append(lines, "            "+key+": "+q[i+1])
		}
	}
	lines = append(lines, "proxy-status:", "    proxy-name: Policy0", "    proxy-tls:")
	return lines, nil
}

func (f *fakeKeenetic) cmds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

type harness struct {
	t   *testing.T
	s   *service
	d   *compat.Deps
	k   *fakeKeenetic
	now time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	a := app.New(root, platform.DefaultConfig(), logger)
	k := &fakeKeenetic{reject: map[string]bool{}}
	a.T = k
	d := compat.NewDeps(context.Background(), a)
	s := newService(d)
	h := &harness{t: t, s: s, d: d, k: k, now: time.Unix(1793610000, 0)}
	s.now = func() time.Time { return h.now }
	s.probe = func(ctx context.Context, c catEntry) probeResult {
		return probeResult{OK: true, Status: "ok", Message: msgProbeOK, LatencyMs: int64(0), EndEpoch: h.now.Unix()}
	}
	s.defineQueue()
	current.Store(s)
	regOnce.Do(register)
	convergenceAttempts, convergenceDelay = 3, 0
	return h
}

type resp struct {
	Status  int
	Success bool
	Data    map[string]any
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details any    `json:"details"`
	}
}

func (h *harness) call(method, path string, body any, hdr map[string]string) resp {
	h.t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Host = "192.168.1.1:8080"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	u := r.URL.Path
	if !cgi.Serve(w, r, u, nil) {
		h.t.Fatalf("route %s not registered", u)
	}
	var out resp
	out.Status = w.Code
	var raw struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		h.t.Fatalf("bad json: %s", w.Body.String())
	}
	out.Success = raw.Success
	_ = json.Unmarshal(raw.Data, &out.Data)
	_ = json.Unmarshal(raw.Error, &out.Error)
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		h.t.Fatalf("content-type %q", w.Header().Get("Content-Type"))
	}
	return out
}

func (h *harness) expectErr(r resp, status int, code string) {
	h.t.Helper()
	if r.Status != status || r.Error.Code != code {
		h.t.Fatalf("want %d %s, got %d %s (%s)", status, code, r.Status, r.Error.Code, r.Error.Message)
	}
}

var _ = http.MethodGet

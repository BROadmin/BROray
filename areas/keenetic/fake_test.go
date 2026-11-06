package keenetic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/BROadmin/broray/areas/xray"
	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	kn "github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
)

type fakeIface struct {
	desc     string // raw description line ("" = none)
	security bool
	protocol string
	udp      bool // "proxy socks5-udp" (KeeneticOS 4.1+)
	upstream string
	up       bool
	extra    []string // raw extra lines (foreign content)
}

type fakeRouter struct {
	mu          sync.Mutex
	ifaces      map[string]*fakeIface
	web         []string // http proxy block body lines (running)
	startup     []string
	cmds        []string
	reject      map[string]bool
	connected   bool
	noSave      bool
	ndns        []string
	rci         map[string]string   // RCI Show path -> JSON (IPv6 reads)
	text        map[string][]string // extra CLI text answers (IPv6 reads)
	shows       int                 // Show + ShowText calls for rci/text answers
	ipv6Block   <-chan struct{}     // optional blocked IPv6 read for shutdown/status tests
	ipv6Entered chan<- struct{}
	routes      []string // running "ip route …" lines, KeeneticOS order of addition
	noUDP       bool     // KeeneticOS before 4.1: "proxy socks5-udp" is an unknown command
}

func newFakeRouter() *fakeRouter {
	r := &fakeRouter{ifaces: map[string]*fakeIface{}, reject: map[string]bool{}, connected: true,
		ndns: []string{"name: myrouter", "domain: keenetic.pro", "access: cloud"}}
	r.startup = r.render()
	return r
}

func (r *fakeRouter) render() []string {
	lines := []string{"! $$$ Model: Test", "system", "    hostname test", "!", "interface Bridge0", "    ip address 192.168.1.1 255.255.255.0", "!"}
	names := make([]string, 0, len(r.ifaces))
	for n := range r.ifaces {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := r.ifaces[n]
		lines = append(lines, "interface "+n)
		if f.desc != "" {
			lines = append(lines, f.desc)
		}
		if f.security {
			lines = append(lines, "    security-level public")
		}
		if f.protocol != "" {
			lines = append(lines, "    proxy protocol "+f.protocol)
		}
		if f.upstream != "" {
			lines = append(lines, "    proxy upstream "+f.upstream)
		}
		if f.udp {
			// Observed on KN-1811 / KeeneticOS 5.02.B.2.0-2 (CODEX-REPORT-DEV5
			// U2): after "proxy upstream", before "up", in running and startup.
			lines = append(lines, "    proxy socks5-udp")
		}
		lines = append(lines, f.extra...)
		if f.up {
			lines = append(lines, "    up")
		}
		lines = append(lines, "!")
	}
	lines = append(lines, r.routes...)
	if r.web != nil {
		lines = append(lines, "ip http proxy broray")
		for _, l := range r.web {
			lines = append(lines, "    "+l)
		}
		lines = append(lines, "!")
	}
	return lines
}

func (r *fakeRouter) Name() string { return "fake" }

func (r *fakeRouter) Parse(ctx context.Context, cmds []string) ([]kn.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []kn.Result
	for _, c := range cmds {
		r.cmds = append(r.cmds, c)
		if r.reject[c] || (r.noSave && c == "system configuration save") {
			out = append(out, kn.Result{Command: c, OK: false, Status: []kn.Status{{Status: "error", Message: "rejected"}}})
			continue
		}
		f := strings.Fields(c)
		switch {
		case c == "system configuration save":
			r.startup = r.render()
		case len(f) == 3 && f[0] == "no" && f[1] == "interface":
			delete(r.ifaces, f[2])
		case len(f) == 2 && f[0] == "interface":
			if r.ifaces[f[1]] == nil {
				r.ifaces[f[1]] = &fakeIface{}
			}
		case f[0] == "interface":
			x := r.ifaces[f[1]]
			if x == nil {
				out = append(out, kn.Result{Command: c, OK: false})
				continue
			}
			rest := strings.TrimPrefix(c, "interface "+f[1]+" ")
			if r.noUDP && strings.HasSuffix(rest, "proxy socks5-udp") {
				// the answer of a KeeneticOS without the command
				out = append(out, kn.Result{Command: c, OK: false, Status: []kn.Status{{Status: "error", Code: "7405600",
					Ident: "Command::Base", Message: "no such command: socks5-udp"}}})
				continue
			}
			switch {
			case rest == "proxy protocol socks5":
				x.protocol = "socks5"
			case rest == "proxy socks5-udp":
				// KeeneticOS requires the SOCKS5 protocol first.
				if x.protocol != "socks5" {
					out = append(out, kn.Result{Command: c, OK: false, Status: []kn.Status{{Status: "error", Message: "proxy protocol is not socks5"}}})
					continue
				}
				x.udp = true
			case rest == "no proxy socks5-udp":
				x.udp = false
			case strings.HasPrefix(rest, "proxy upstream "):
				x.upstream = strings.TrimPrefix(rest, "proxy upstream ")
			case rest == "proxy connect":
			case strings.HasPrefix(rest, "description "):
				x.desc = "    " + rest
			case rest == "no description":
				x.desc = ""
			case rest == "security-level public":
				x.security = true
			case rest == "up":
				x.up = true
			}
		case len(f) == 6 && f[0] == "ip" && f[1] == "route":
			if !contains(r.routes, c) {
				r.routes = append(r.routes, c)
			}
		case len(f) == 6 && f[0] == "no" && f[1] == "ip" && f[2] == "route":
			prefix, found := "ip route "+f[3]+" "+f[4]+" "+f[5]+" ", false
			for i, l := range r.routes {
				if strings.HasPrefix(l, prefix) {
					r.routes, found = append(r.routes[:i:i], r.routes[i+1:]...), true
					break
				}
			}
			if !found {
				out = append(out, kn.Result{Command: c, OK: false, Status: []kn.Status{{Status: "error", Message: "no such route"}}})
				continue
			}
		case c == "no ip http proxy broray":
			r.web = nil
		case strings.HasPrefix(c, "ip http proxy broray"):
			rest := strings.TrimSpace(strings.TrimPrefix(c, "ip http proxy broray"))
			if r.web == nil {
				r.web = []string{}
			}
			if rest != "" {
				if strings.HasPrefix(rest, "upstream ") {
					var kept []string
					for _, l := range r.web {
						if !strings.HasPrefix(l, "upstream ") {
							kept = append(kept, l)
						}
					}
					r.web = append([]string{rest}, kept...)
				} else if !contains(r.web, rest) {
					r.web = append(r.web, rest)
				}
			}
		}
		out = append(out, kn.Result{Command: c, OK: true})
	}
	return out, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func (r *fakeRouter) RunningConfig(ctx context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.render(), nil
}

func (r *fakeRouter) StartupConfig(ctx context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.startup...), nil
}

func (r *fakeRouter) Show(ctx context.Context, path string, out any) error {
	if strings.HasPrefix(path, "show/ipv6/") && r.ipv6Block != nil {
		select {
		case r.ipv6Entered <- struct{}{}:
		default:
		}
		select {
		case <-r.ipv6Block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if raw, ok := r.rci[path]; ok {
		r.shows++
		return json.Unmarshal([]byte(raw), out)
	}
	return errors.New("no rci")
}

func (r *fakeRouter) ShowText(ctx context.Context, cmd string) ([]string, error) {
	switch {
	case cmd == "show running-config":
		return r.RunningConfig(ctx)
	case cmd == "more startup-config":
		return r.StartupConfig(ctx)
	case cmd == "show ndns":
		return r.ndns, nil
	case r.text[cmd] != nil:
		r.mu.Lock()
		defer r.mu.Unlock()
		r.shows++
		return r.text[cmd], nil
	case strings.HasPrefix(cmd, "show interface "):
		r.mu.Lock()
		defer r.mu.Unlock()
		name := strings.TrimPrefix(cmd, "show interface ")
		f := r.ifaces[name]
		if f == nil {
			return nil, errors.New("Command::Base error[7405602]: argument parse error.")
		}
		desc := ""
		if d, ok := decodeDescriptionLine(f.desc); ok {
			desc = d
		}
		conn := "no"
		if r.connected && f.up && f.upstream != "" {
			conn = "yes"
		}
		return []string{"id: " + name, "type: Proxy", "description: " + desc, "link: up", "connected: " + conn, "state: up", "mtu: 1500"}, nil
	}
	return nil, errors.New("unknown")
}

func (r *fakeRouter) commands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.cmds...)
}

func (r *fakeRouter) resetCommands() { r.mu.Lock(); r.cmds = nil; r.mu.Unlock() }

type harness struct {
	t *testing.T
	s *service
	d *compat.Deps
	r *fakeRouter
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := platform.DefaultConfig()
	cfg.ListenAddress = "192.168.1.1"
	a := app.New(root, cfg, log.New(io.Discard, "", 0))
	r := newFakeRouter()
	a.T = r
	d := compat.NewDeps(context.Background(), a)
	d.Svc.XrayRunning = func() bool { return true }
	s := newService(d)
	current.Store(s)
	regOnce.Do(register)
	settleDelay, convergenceDelay, postActionDelay, webVerifyDelay = 0, 0, 0, 0
	convergenceAttempts, webVerifyAttempts = 2, 2
	statusCacheTTL = 0
	h := &harness{t: t, s: s, d: d, r: r}
	h.setSocks("192.168.1.1", 2080)
	h.setServer("nl", "Netherlands")
	return h
}

func (h *harness) setSocks(host string, port int) {
	cfg := map[string]any{"inbounds": []any{map[string]any{"protocol": "socks", "listen": host, "port": port}}}
	b, _ := json.Marshal(cfg)
	_ = os.WriteFile(h.d.Root.XrayConfig(), b, 0o600)
	dir := h.t.TempDir()
	hex := map[string]string{"192.168.1.1": "0101A8C0"}[host]
	_ = os.WriteFile(filepath.Join(dir, "tcp"), []byte("hdr\n 0: "+hex+":"+strings.ToUpper(strings.TrimLeft(hex4(port), ""))+" 00000000:0000 0A\n"), 0o600)
	xray.ProcNet = dir
}

func hex4(p int) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[p>>12&15], digits[p>>8&15], digits[p>>4&15], digits[p&15]})
}

func (h *harness) setServer(id, name string) {
	_ = os.MkdirAll(h.d.Path("config"), 0o700)
	_ = os.MkdirAll(h.d.Path("servers"), 0o700)
	if id == "" {
		_ = os.Remove(h.d.Path("config", "active-server"))
		return
	}
	_ = os.WriteFile(h.d.Path("config", "active-server"), []byte(id+"\n"), 0o600)
	_ = compat.WriteJSON(h.d.Path("servers", id+".json"), map[string]any{"id": id, "name": name})
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

func (h *harness) call(method, path string) resp {
	var headers map[string]string
	if method == "POST" {
		headers = map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"}
	}
	return h.callH(method, path, headers)
}

func (h *harness) callH(method, path string, headers map[string]string) resp {
	h.t.Helper()
	var body io.Reader
	if method == "POST" {
		body = strings.NewReader("{}")
	}
	req := httptest.NewRequest(method, path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	if !cgi.Serve(w, req, req.URL.Path, nil) {
		h.t.Fatalf("route %s missing", path)
	}
	var out resp
	out.Status = w.Code
	var raw struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		h.t.Fatalf("json %s", w.Body.String())
	}
	out.Success = raw.Success
	_ = json.Unmarshal(raw.Data, &out.Data)
	_ = json.Unmarshal(raw.Error, &out.Error)
	return out
}

func (h *harness) expectErr(r resp, status int, code string) {
	h.t.Helper()
	if r.Status != status || r.Error.Code != code {
		h.t.Fatalf("want %d %s, got %d %s %q %v", status, code, r.Status, r.Error.Code, r.Error.Message, r.Error.Details)
	}
}

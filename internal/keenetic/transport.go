package keenetic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Transport is the single place BROray reads from and writes to KeeneticOS
// configuration. Writes are CLI command strings; every caller must have
// checked them against its own allowlist first (see allow.go).
type Transport interface {
	// Parse executes CLI commands in order and returns one result per command.
	Parse(ctx context.Context, commands []string) ([]Result, error)
	// RunningConfig returns the current running configuration lines.
	RunningConfig(ctx context.Context) ([]string, error)
	// StartupConfig returns the saved startup configuration lines.
	StartupConfig(ctx context.Context) ([]string, error)
	// Show returns a parsed RCI "show ..." object by path (e.g. "interface/Bridge0").
	Show(ctx context.Context, path string, out any) error
	Name() string
}

// Result is the status of one executed command.
type Result struct {
	Command string   `json:"command"`
	OK      bool     `json:"ok"`
	Status  []Status `json:"status,omitempty"`
}

type Status struct {
	Status  string `json:"status"` // message | warning | error
	Code    string `json:"code"`
	Ident   string `json:"ident"`
	Message string `json:"message"`
}

func (r Result) Error() string {
	for _, s := range r.Status {
		if s.Status == "error" {
			return s.Message
		}
	}
	return ""
}

var ErrTransport = errors.New("KEENETIC_TRANSPORT_FAILED")

// ---- RCI over the router-local HTTP port (default http://127.0.0.1:79) ----

type RCI struct {
	Base   string
	Client *http.Client
}

func NewRCI(base string) *RCI {
	tr := &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 1, ResponseHeaderTimeout: 60 * time.Second}
	return &RCI{Base: strings.TrimRight(base, "/"), Client: &http.Client{Transport: tr, Timeout: 120 * time.Second}}
}

func (r *RCI) Name() string { return "rci" }

func (r *RCI) Parse(ctx context.Context, commands []string) ([]Result, error) {
	if len(commands) == 0 {
		return nil, nil
	}
	type req struct {
		Parse string `json:"parse"`
	}
	body := make([]req, 0, len(commands))
	for _, c := range commands {
		body = append(body, req{Parse: c})
	}
	b, _ := json.Marshal(body)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Base+"/rci/", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	resp, err := r.Client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("%w: rci post: %v", ErrTransport, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: rci read: %v", ErrTransport, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: rci status %d", ErrTransport, resp.StatusCode)
	}
	var raw []struct {
		Parse struct {
			Status []Status `json:"status"`
		} `json:"parse"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: rci json: %v", ErrTransport, err)
	}
	if len(raw) != len(commands) {
		return nil, fmt.Errorf("%w: rci answered %d of %d commands", ErrTransport, len(raw), len(commands))
	}
	out := make([]Result, len(commands))
	for i, c := range commands {
		out[i] = Result{Command: c, OK: true, Status: raw[i].Parse.Status}
		for _, s := range raw[i].Parse.Status {
			if s.Status == "error" {
				out[i].OK = false
			}
		}
	}
	return out, nil
}

func (r *RCI) showLines(ctx context.Context, path string) ([]string, error) {
	var v struct {
		Message []string `json:"message"`
	}
	if err := r.Show(ctx, path, &v); err != nil {
		return nil, err
	}
	return v.Message, nil
}

func (r *RCI) RunningConfig(ctx context.Context) ([]string, error) {
	return r.showLines(ctx, "show/running-config")
}

func (r *RCI) StartupConfig(ctx context.Context) ([]string, error) {
	return r.showLines(ctx, "more/startup-config")
}

func (r *RCI) Show(ctx context.Context, path string, out any) error {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Base+"/rci/"+strings.TrimLeft(path, "/"), nil)
	if err != nil {
		return err
	}
	hreq.Header.Set("Accept", "application/json")
	resp, err := r.Client.Do(hreq)
	if err != nil {
		return fmt.Errorf("%w: rci get %s: %v", ErrTransport, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("%w: rci read: %v", ErrTransport, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: rci get %s: status %d", ErrTransport, path, resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: rci json %s: %v", ErrTransport, path, err)
	}
	return nil
}

// ---- ndmc fallback: one command per process, as BROray 3.x did ----

// NDMC runs /bin/ndmc -c "<command>". The Entware loader environment
// (LD_LIBRARY_PATH, LD_PRELOAD) breaks ndmc ("initialization failure"), so it
// is stripped for the child only.
type NDMC struct {
	Binary  string
	Timeout time.Duration
}

func NewNDMC() *NDMC { return &NDMC{Binary: "/bin/ndmc", Timeout: 60 * time.Second} }

func (n *NDMC) Name() string { return "ndmc" }

func cleanEnv() []string {
	env := os.Environ()
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") || strings.HasPrefix(kv, "LD_PRELOAD=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (n *NDMC) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, n.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, n.Binary, args...)
	cmd.Env = cleanEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("%w: ndmc %q: %v: %s", ErrTransport, args, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (n *NDMC) Parse(ctx context.Context, commands []string) ([]Result, error) {
	out := make([]Result, 0, len(commands))
	for _, c := range commands {
		b, err := n.run(ctx, "-c", c)
		text := strings.TrimSpace(string(b))
		res := Result{Command: c, OK: err == nil}
		if text != "" {
			st := Status{Status: "message", Message: text}
			if err != nil || strings.Contains(text, "error") {
				st.Status = "error"
				res.OK = false
			}
			res.Status = []Status{st}
		}
		if err != nil && text == "" {
			res.Status = []Status{{Status: "error", Message: err.Error()}}
		}
		out = append(out, res)
		if !res.OK {
			// Stop at the first failure so the caller sees exactly where the
			// plan diverged; remaining commands are reported as not run.
			for _, rest := range commands[len(out):] {
				out = append(out, Result{Command: rest, OK: false, Status: []Status{{Status: "error", Code: "NOT_RUN", Message: "not run"}}})
			}
			return out, nil
		}
	}
	return out, nil
}

func (n *NDMC) lines(ctx context.Context, command string) ([]string, error) {
	b, err := n.run(ctx, "-c", command)
	if err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines, nil
}

func (n *NDMC) RunningConfig(ctx context.Context) ([]string, error) {
	return n.lines(ctx, "show running-config")
}

func (n *NDMC) StartupConfig(ctx context.Context) ([]string, error) {
	return n.lines(ctx, "more startup-config")
}

// Show through ndmc is not structured JSON; only RCI supports Show. Callers
// that need Show fall back to parsing running-config.
func (n *NDMC) Show(ctx context.Context, path string, out any) error {
	return fmt.Errorf("%w: ndmc has no structured show for %s", ErrTransport, path)
}

// ---- Auto: RCI first, ndmc when RCI is unreachable ----

type Auto struct {
	Primary  Transport
	Fallback Transport
}

func (a *Auto) Name() string { return a.Primary.Name() + "+" + a.Fallback.Name() }

func (a *Auto) Parse(ctx context.Context, commands []string) ([]Result, error) {
	res, err := a.Primary.Parse(ctx, commands)
	if err != nil && errors.Is(err, ErrTransport) {
		return a.Fallback.Parse(ctx, commands)
	}
	return res, err
}

// configFromNDMC: running/startup configuration are read with ndmc text
// whenever /bin/ndmc exists — the form BROray 3.x validated, and the only
// one where running and startup are directly comparable (RCI renders
// descriptions with non-ASCII characters differently from the CLI, and
// has no "more startup-config"). Writes keep using RCI batches.
func (a *Auto) configFromNDMC() bool {
	n, ok := a.Fallback.(*NDMC)
	if !ok {
		return false
	}
	st, err := os.Stat(n.Binary)
	return err == nil && st.Mode().IsRegular()
}

func (a *Auto) RunningConfig(ctx context.Context) ([]string, error) {
	if a.configFromNDMC() {
		return a.Fallback.RunningConfig(ctx)
	}
	res, err := a.Primary.RunningConfig(ctx)
	if err != nil && errors.Is(err, ErrTransport) {
		return a.Fallback.RunningConfig(ctx)
	}
	return res, err
}

func (a *Auto) StartupConfig(ctx context.Context) ([]string, error) {
	if a.configFromNDMC() {
		return a.Fallback.StartupConfig(ctx)
	}
	res, err := a.Primary.StartupConfig(ctx)
	if err != nil && errors.Is(err, ErrTransport) {
		return a.Fallback.StartupConfig(ctx)
	}
	return res, err
}

func (a *Auto) Show(ctx context.Context, path string, out any) error {
	return a.Primary.Show(ctx, path, out)
}

// NewTransport builds the transport named by config: rci | ndmc | auto.
func NewTransport(mode, rciBase string) Transport {
	switch mode {
	case "rci":
		return NewRCI(rciBase)
	case "ndmc":
		return NewNDMC()
	default:
		return &Auto{Primary: NewRCI(rciBase), Fallback: NewNDMC()}
	}
}

// Package xray owns the external Xray binary: pinned download, config
// rendering and test, the kernel /32 route to the server, and the child
// process the daemon supervises.
package xray

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/servers"
	"golang.org/x/sys/unix"
)

// Asset maps the build architecture to the XTLS release asset and Entware arch.
type Asset struct {
	Entware string
	Zip     string
}

func CurrentAsset() (Asset, error) {
	switch runtime.GOARCH {
	case "arm64":
		return Asset{"aarch64-3.10", "Xray-linux-arm64-v8a.zip"}, nil
	case "arm":
		return Asset{"armv7-3.2", "Xray-linux-arm32-v7a.zip"}, nil
	case "mipsle":
		return Asset{"mipsel-3.4", "Xray-linux-mips32le.zip"}, nil
	case "mips":
		return Asset{"mips-3.4", "Xray-linux-mips32.zip"}, nil
	case "amd64":
		return Asset{"x86_64-dev", "Xray-linux-64.zip"}, nil
	}
	return Asset{}, fmt.Errorf("XRAY_ARCH_UNSUPPORTED: %s", runtime.GOARCH)
}

// Pinned holds official XTLS archive digests, verified before extraction.
// 26.11.9 was verified on 2026-11-10. Pinning is not hardware evidence.
var Pinned = map[string]map[string]string{
	"v26.11.9": {
		"Xray-linux-arm64-v8a.zip": "64ed6858982a7f5eb85d88b811089063bd4f17561f4c8f44e0f1dc8e6f6430be",
		"Xray-linux-arm32-v7a.zip": "19c51e08a6562705fb1cb032a84fd87df21d7e27b194f035be7e754dbe3a97b3",
		"Xray-linux-mips32le.zip":  "793dc40029283eb9ccead188b05abf0d34b64aab16ad1f6eadd204963a54837b",
		"Xray-linux-mips32.zip":    "4ceb213ad7cd8c1fc9250bb886e730665cddb9dfe891291b09231a204f8ba6ef",
		"Xray-linux-64.zip":        "b651f7e1d9966ee2c1b1ee1bf8b52c72771824d3065dce96bbc8bcc19ed95f82", // host/CI
	},
	"v26.10.31": {
		"Xray-linux-arm64-v8a.zip": "1811c8f1da0c3b6dc707ebaf1c8390e987ba39859a8f0a7aadd8526157f2ec8d",
		"Xray-linux-arm32-v7a.zip": "2387f55239ee7799fd67d5382e79f589c1da07a97d993e86b058f2290b8e456b",
		"Xray-linux-mips32le.zip":  "7f3e56ea73f2e2ce12556ba3720e8894abd89c17aec5c70ea70ae6f48552cfb5",
		"Xray-linux-mips32.zip":    "45a84c48da48e7f7663c4c0030977ba0d56ba24a5c855de6a1d898be904e16db",
		"Xray-linux-64.zip":        "e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214", // host/CI only
	},
}

// ResolveTag returns the tag to install: the configured one if it is pinned,
// the newest pinned tag when nothing is configured. An explicitly configured
// tag that is not pinned is refused: digests are never skipped.
func ResolveTag(configured string) (string, error) {
	if configured != "" {
		if _, ok := Pinned[configured]; !ok {
			return "", fmt.Errorf("XRAY_VERSION_NOT_PINNED: %s", configured)
		}
		return configured, nil
	}
	best := ""
	for tag := range Pinned {
		if best == "" || compareTags(tag, best) > 0 {
			best = tag
		}
	}
	if best == "" {
		return "", errors.New("XRAY_NO_PINNED_VERSION")
	}
	return best, nil
}

// PinnedTags lists the pinned release tags, newest first.
func PinnedTags() []string {
	out := make([]string, 0, len(Pinned))
	for tag := range Pinned {
		out = append(out, tag)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && compareTags(out[j], out[j-1]) > 0; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// PinnedDigest returns the pinned archive SHA-256 of this build's asset for tag.
func PinnedDigest(tag string) (Asset, string, bool) {
	a, err := CurrentAsset()
	if err != nil {
		return a, "", false
	}
	d, ok := Pinned[tag][a.Zip]
	return a, d, ok
}

func compareTags(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

var reVersion = regexp.MustCompile(`^Xray (\d+\.\d+\.\d+)`)

// Version runs "<binary> version" and returns "x.y.z".
func Version(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version")
	cmd.Env = childEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("XRAY_VERSION_FAILED: %v", err)
	}
	m := reVersion.FindSubmatch(bytes.TrimSpace(out))
	if m == nil {
		return "", errors.New("XRAY_VERSION_UNPARSED")
	}
	return string(m[1]), nil
}

func childEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") || strings.HasPrefix(kv, "LD_PRELOAD=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Test runs "xray run -test -c config" and returns its combined output on failure.
func Test(ctx context.Context, binary, config string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "run", "-test", "-c", config)
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if len(text) > 2000 {
			text = text[len(text)-2000:]
		}
		return fmt.Errorf("XRAY_CONFIG_TEST_FAILED: %s", text)
	}
	return nil
}

// WriteConfig renders and tests the config for the server; on success it
// replaces config.json atomically (keeping config.json.previous).
func WriteConfig(ctx context.Context, root platform.Root, cfg platform.Config, srv servers.Server, listen string) error {
	obj := servers.XrayConfig(srv, listen, cfg.SocksPort, cfg.LogLevel, filepath.Join(root.Logs(), "access.log"), filepath.Join(root.Logs(), "error.log"))
	b, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	candidate := filepath.Join(root.Tmp(), "config.candidate.json")
	if err := platform.WriteFileAtomic(candidate, b, 0o600); err != nil {
		return err
	}
	defer os.Remove(candidate)
	if _, err := os.Stat(root.XrayBinary()); err == nil {
		if err := Test(ctx, root.XrayBinary(), candidate); err != nil {
			return err
		}
	}
	if old, err := os.ReadFile(root.XrayConfig()); err == nil {
		_ = platform.WriteFileAtomic(root.XrayConfig()+".previous", old, 0o600)
	}
	return platform.WriteFileAtomic(root.XrayConfig(), b, 0o600)
}

// ---------- kernel route to the server through WAN ----------

type defaultRoute struct {
	Iface   string
	Gateway string
}

// DefaultRoute reads the lowest-metric IPv4 default route from /proc/net/route.
func DefaultRoute() (defaultRoute, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return defaultRoute{}, err
	}
	defer f.Close()
	best := defaultRoute{}
	bestMetric := -1
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 8 || fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		flags, _ := strconv.ParseInt(fields[3], 16, 32)
		if flags&1 == 0 { // RTF_UP
			continue
		}
		metric, _ := strconv.Atoi(fields[6])
		if bestMetric >= 0 && metric >= bestMetric {
			continue
		}
		gw := ""
		if g, err := strconv.ParseUint(fields[2], 16, 32); err == nil && g != 0 {
			gw = fmt.Sprintf("%d.%d.%d.%d", byte(g), byte(g>>8), byte(g>>16), byte(g>>24))
		}
		best, bestMetric = defaultRoute{Iface: fields[0], Gateway: gw}, metric
	}
	if bestMetric < 0 {
		return defaultRoute{}, errors.New("WAN_DEFAULT_ROUTE_NOT_FOUND")
	}
	return best, nil
}

// ResolveIPv4 resolves a hostname (or returns a literal IPv4).
func ResolveIPv4(ctx context.Context, host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return "", errors.New("SERVER_ADDRESS_IPV6_UNSUPPORTED")
		}
		return ip.String(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("SERVER_ADDRESS_UNRESOLVED: %s", host)
	}
	return ips[0].String(), nil
}

func ipCmd(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ip", args...)
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// EnsureServerRoute installs a /32 route to the server address through the
// WAN default route with metric 1 (as 3.x broray-xray-start did) and records
// it in run/xray-server-route so a changed server can be cleaned up.
func EnsureServerRoute(ctx context.Context, root platform.Root, host string) (string, error) {
	ip, err := ResolveIPv4(ctx, host)
	if err != nil {
		return "", err
	}
	dr, err := DefaultRoute()
	if err != nil {
		return "", err
	}
	if b, err := os.ReadFile(root.ServerRouteState()); err == nil {
		f := strings.Fields(string(b))
		if len(f) == 2 && (f[0] != ip || f[1] != dr.Iface) {
			_ = ipCmd(ctx, "route", "del", f[0]+"/32", "dev", f[1])
		}
	}
	args := []string{"route", "replace", ip + "/32"}
	if dr.Gateway != "" {
		args = append(args, "via", dr.Gateway)
	}
	args = append(args, "dev", dr.Iface, "metric", "1")
	if err := ipCmd(ctx, args...); err != nil {
		return "", err
	}
	return ip, platform.WriteFileAtomic(root.ServerRouteState(), []byte(ip+" "+dr.Iface+"\n"), 0o600)
}

// RemoveServerRoute deletes the recorded /32 route, if any.
func RemoveServerRoute(ctx context.Context, root platform.Root) {
	if b, err := os.ReadFile(root.ServerRouteState()); err == nil {
		f := strings.Fields(string(b))
		if len(f) == 2 {
			_ = ipCmd(ctx, "route", "del", f[0]+"/32", "dev", f[1])
		}
		_ = os.Remove(root.ServerRouteState())
	}
}

// ---------- supervised process ----------

// child is one launched Xray. Its PID cannot be reused while the child is
// unreaped (alive or zombie), so signals are sent only under Process.mu and
// only while exited is false. The wait goroutine first observes the exit
// with waitid(WNOWAIT) — the child stays a zombie — sets exited under the
// same mutex, and only then reaps. No signal can therefore reach a reused PID
// (DEV1_XRAY_STOP_PID_ONLY_GROUP_SIGNAL).
type child struct {
	cmd    *exec.Cmd
	exited bool
	done   chan struct{}
}

// Process supervises one Xray child. The child is restarted after a crash
// while it is wanted.
type Process struct {
	mu        sync.Mutex
	root      platform.Root
	cur       *child
	wanted    bool
	startedAt time.Time
	restarts  int
	lastErr   string
	onExit    func()
	// Binary overrides root.XrayBinary() (tests).
	Binary string
	// Args overrides the "run -c <config>" arguments (tests).
	Args []string
}

func NewProcess(root platform.Root) *Process { return &Process{root: root} }

type Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Running   bool   `json:"running"`
	Wanted    bool   `json:"wanted"`
	PID       int    `json:"pid"`
	UptimeSec int64  `json:"uptimeSec"`
	Restarts  int    `json:"restarts"`
	LastError string `json:"lastError,omitempty"`
	Binary    string `json:"binary"`
	Size      int64  `json:"sizeBytes"`
}

func (p *Process) Status(ctx context.Context) Status {
	p.mu.Lock()
	st := Status{Wanted: p.wanted, Restarts: p.restarts, LastError: p.lastErr, Binary: p.root.XrayBinary()}
	if p.cur != nil && !p.cur.exited {
		st.Running, st.PID = true, p.cur.cmd.Process.Pid
		st.UptimeSec = int64(time.Since(p.startedAt).Seconds())
	}
	p.mu.Unlock()
	if fi, err := os.Stat(st.Binary); err == nil && fi.Mode().IsRegular() {
		st.Installed, st.Size = true, fi.Size()
		if v, err := Version(ctx, st.Binary); err == nil {
			st.Version = v
		}
	}
	return st
}

// Start launches the child (config must already be tested and written).
func (p *Process) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.wanted = true
	if p.cur != nil {
		return nil
	}
	return p.launchLocked()
}

func (p *Process) launchLocked() error {
	bin, cfg := p.root.XrayBinary(), p.root.XrayConfig()
	args := []string{"run", "-c", cfg}
	if p.Binary != "" {
		bin = p.Binary
	}
	if p.Args != nil {
		args = p.Args
	} else if _, err := os.Stat(cfg); err != nil {
		return errors.New("XRAY_CONFIG_MISSING")
	}
	if _, err := os.Stat(bin); err != nil {
		return errors.New("XRAY_NOT_INSTALLED")
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(childEnv(), "XRAY_LOCATION_ASSET="+p.root.Runtime())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var logf *os.File
	if f, err := os.OpenFile(filepath.Join(p.root.Logs(), "xray.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		logf = f
		cmd.Stdout, cmd.Stderr = logf, logf
	}
	if err := cmd.Start(); err != nil {
		if logf != nil {
			logf.Close()
		}
		p.lastErr = err.Error()
		return fmt.Errorf("XRAY_START_FAILED: %v", err)
	}
	c := &child{cmd: cmd, done: make(chan struct{})}
	p.cur, p.startedAt, p.lastErr = c, time.Now(), ""
	go p.wait(c, logf)
	return nil
}

func waitExitNoReap(pid int) {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err == nil || err != unix.EINTR {
			return
		}
	}
}

func (p *Process) wait(c *child, logf *os.File) {
	waitExitNoReap(c.cmd.Process.Pid)
	p.mu.Lock()
	c.exited = true // from here on nobody signals this PID
	p.mu.Unlock()
	err := c.cmd.Wait() // reap
	if logf != nil {
		logf.Close()
	}
	close(c.done)
	p.mu.Lock()
	if p.cur != c {
		p.mu.Unlock()
		return
	}
	p.cur = nil
	if err != nil {
		p.lastErr = "xray exited: " + err.Error()
	}
	restart := p.wanted
	uptime := time.Since(p.startedAt)
	p.mu.Unlock()
	if p.onExit != nil {
		p.onExit()
	}
	if restart {
		delay := 2 * time.Second
		if uptime < 10*time.Second {
			delay = 10 * time.Second
		}
		time.Sleep(delay)
		p.mu.Lock()
		if p.wanted && p.cur == nil {
			p.restarts++
			_ = p.launchLocked()
		}
		p.mu.Unlock()
	}
}

// signalLocked sends sig to the child's process group only if the child has
// not been observed as exited (its PID is still owned by us). p.mu held.
func (p *Process) signalLocked(c *child, sig syscall.Signal) {
	if c.exited {
		return
	}
	_ = syscall.Kill(-c.cmd.Process.Pid, sig)
}

// StopTimeouts are the TERM grace period and the post-KILL confirmation wait.
var StopTimeouts = [2]time.Duration{10 * time.Second, 5 * time.Second}

// Stop sends SIGTERM to the owned child, waits for it to be reaped, escalates
// to SIGKILL after the grace period and returns XRAY_STOP_UNCONFIRMED if the
// child was not reaped after that.
func (p *Process) Stop(ctx context.Context) error {
	p.mu.Lock()
	p.wanted = false
	c := p.cur
	if c == nil {
		p.mu.Unlock()
		return nil
	}
	p.signalLocked(c, syscall.SIGTERM)
	p.mu.Unlock()
	select {
	case <-c.done:
		return nil
	case <-time.After(StopTimeouts[0]):
	}
	p.mu.Lock()
	p.signalLocked(c, syscall.SIGKILL)
	p.mu.Unlock()
	select {
	case <-c.done:
		return nil
	case <-time.After(StopTimeouts[1]):
		return errors.New("XRAY_STOP_UNCONFIRMED: process not reaped after SIGKILL")
	}
}

func (p *Process) Restart(ctx context.Context) error {
	if err := p.Stop(ctx); err != nil {
		return err
	}
	return p.Start(ctx)
}

// PID returns the PID of the running child, 0 when none.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur != nil && !p.cur.exited {
		return p.cur.cmd.Process.Pid
	}
	return 0
}

func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cur != nil && !p.cur.exited
}

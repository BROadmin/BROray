package servers

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// svc is the servers area runtime (one per daemon).
type svc struct {
	d *compat.Deps

	// mu serializes the read-modify-write publications of the auto-switch
	// state file (3.x coordinator field publications).
	mu sync.Mutex

	// hooks (replaced in tests)
	now            func() time.Time
	xrayTest       func(ctx context.Context, configPath string) error
	measure        func(ctx context.Context, rec Record, cfg []byte) probeResult
	activeProbe    func(ctx context.Context, configPath, serverID string) map[string]any
	ping           func(ctx context.Context, address string, count int, wait int) string
	xrayProcess    func() (pid int, start string, ok bool)
	socksListening func(address string, port int) bool
	stopXray       func(ctx context.Context) error

	loopRunning    atomic.Bool
	monitorRunning atomic.Bool
	probePIDs      sync.Map // pids of temporary probe Xray processes
}

func newSvc(d *compat.Deps) *svc {
	s := &svc{d: d, now: time.Now}
	s.xrayTest = s.execXrayTest
	s.measure = s.isolatedProbe
	s.activeProbe = s.socksProbe
	s.ping = execPing
	s.xrayProcess = s.findXray
	s.socksListening = procSocksListening
	s.stopXray = func(ctx context.Context) error {
		if d.App != nil && d.App.Xray != nil {
			return d.App.Xray.Stop(ctx)
		}
		return nil
	}
	return s
}

// ---------- settings.json (lib/xray.sh:207-248, generator inputs) ----------

type settings struct {
	raw       map[string]any
	socksAddr string
	socksPort int
}

func (s *svc) settings(ctx context.Context) settings {
	raw, ok := readObjectFile(settingsFile(s.d))
	if !ok {
		// 3.5.0: settings.json may not exist; take the daemon configuration.
		raw = map[string]any{}
		if s.d.App != nil {
			cfg := s.d.App.Cfg
			listen := cfg.ListenAddress
			if listen == "" && s.d.App.K != nil {
				cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if ip, err := s.d.App.LAN(cctx); err == nil {
					listen = ip
				}
				cancel()
			}
			if listen != "" {
				raw["listenAddress"] = listen
			}
			if cfg.SocksPort > 0 {
				raw["socksPort"] = cfg.SocksPort
			}
			if cfg.LogLevel != "" {
				raw["logLevel"] = cfg.LogLevel
			}
		}
	}
	st := settings{raw: raw, socksAddr: "127.0.0.1", socksPort: 2080}
	if v := alt(get(raw, "xray", "socksAddress"), raw["listenAddress"], raw["socksAddress"], nil); v != nil {
		if a := jqRaw(v); a != "" {
			st.socksAddr = a
		}
	}
	if v := alt(get(raw, "xray", "socksPort"), raw["socksPort"], nil); v != nil {
		if p := jqRaw(v); allDigits(p) {
			st.socksPort, _ = strconv.Atoi(p)
		}
	}
	return st
}

func (s *svc) genSettings(ctx context.Context) GenSettings {
	st := s.settings(ctx)
	return GenSettings{Listen: st.raw["listenAddress"], SocksPort: st.raw["socksPort"], LogLevel: st.raw["logLevel"],
		AccessLog: filepath.Join(s.d.Root.Logs(), "access.log"), ErrorLog: filepath.Join(s.d.Root.Logs(), "error.log")}
}

// GenerateConfig renders the persistent Xray config (config/config.json) for
// a record with the current settings (broray_generate_server_config).
func GenerateConfig(ctx context.Context, d *compat.Deps, rec Record) ([]byte, error) {
	s := newSvc(d)
	return XrayConfig(rec, s.genSettings(ctx), RecordPath(d, asString(rec["id"])))
}

// TestConfig runs `xray run -test -c <file>` on config bytes
// (broray_xray_test_file); the error text is the Xray output.
func TestConfig(ctx context.Context, d *compat.Deps, cfg []byte) error {
	s := newSvc(d)
	return s.testBytes(ctx, cfg)
}

func (s *svc) testBytes(ctx context.Context, cfg []byte) error {
	if err := s.prepareNativeConfig(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(s.d.Root.Tmp(), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.d.Root.Tmp(), "server-config-*.json")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(cfg); err != nil {
		f.Close()
		return err
	}
	f.Close()
	err = s.xrayTest(ctx, name)
	if err != nil && PrivateTransportConfig(cfg) {
		return fmt.Errorf("%s", SafeCoreOutput(cfg, err.Error(), false))
	}
	return err
}

func (s *svc) execXrayTest(ctx context.Context, configPath string) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.d.Root.XrayBinary(), "run", "-test", "-c", configPath)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Join(s.d.Root.Runtime()))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ---------- Xray process identity (pidof xray, broray_xray_runtime_identity) ----------

// findXray finds the persistent Xray: a process whose command line runs the
// BROray Xray config (temporary probe instances use other configs).
func (s *svc) findXray() (int, string, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, "", false
	}
	cfg := s.d.Root.XrayConfig()
	fallback := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if _, probe := s.probePIDs.Load(pid); probe {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		if filepath.Base(args[0]) != "xray" {
			continue
		}
		for _, a := range args[1:] {
			if a == cfg {
				if start, ok := procStart(pid); ok {
					return pid, start, true
				}
			}
		}
		if fallback == 0 {
			fallback = pid
		}
	}
	if fallback != 0 {
		if start, ok := procStart(fallback); ok {
			return fallback, start, true
		}
	}
	return 0, "", false
}

// procStart is the start time (clock ticks, field 22) of /proc/<pid>/stat.
func procStart(pid int) (string, bool) {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return "", false
	}
	return f[19], true
}

func (s *svc) xrayRunning() bool {
	if s.d.Svc != nil && s.d.Svc.XrayRunning != nil {
		return s.d.Svc.IsXrayRunning()
	}
	_, _, ok := s.xrayProcess()
	return ok
}

// runtimeContext is broray_active_proxy_context (active-proxy-health.sh:4-16).
func (s *svc) runtimeContext(id string) string {
	cfg := s.d.Root.XrayConfig()
	st, err := os.Lstat(cfg)
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	active := ActiveID(s.d)
	if active == "" || active != id {
		return ""
	}
	pid, start, ok := s.xrayProcess()
	if !ok {
		return ""
	}
	sum, err := fileSHA256(cfg)
	if err != nil {
		return ""
	}
	text := fmt.Sprintf("%s\n%d:%s\n%s\n%s  %s\n", active, pid, start, cfg, sum, cfg)
	return compat.SHA256Hex([]byte(text))
}

// procSocksListening is broray_xray_socks_active over /proc/net/tcp{,6}.
func procSocksListening(address string, port int) bool {
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		file, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(file)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 4 || fields[3] != "0A" {
				continue
			}
			ip, p, ok := parseProcAddr(fields[1])
			if !ok || p != port {
				continue
			}
			if ip.IsUnspecified() || ip.String() == address {
				file.Close()
				return true
			}
		}
		file.Close()
	}
	return false
}

func parseProcAddr(s string) (net.IP, int, bool) {
	h, p, ok := strings.Cut(s, ":")
	if !ok {
		return nil, 0, false
	}
	port, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return nil, 0, false
	}
	raw, err := hex.DecodeString(h)
	if err != nil {
		return nil, 0, false
	}
	// /proc stores each 32-bit word in host (little-endian on all BROray targets but mips) order.
	ip := make(net.IP, len(raw))
	for i := 0; i+4 <= len(raw); i += 4 {
		if littleEndian {
			ip[i], ip[i+1], ip[i+2], ip[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
		} else {
			copy(ip[i:i+4], raw[i:i+4])
		}
	}
	return ip, int(port), true
}

var littleEndian = binary.NativeEndian.Uint16([]byte{1, 0}) == 1

// ---------- ICMP via BusyBox ping ----------

func execPing(ctx context.Context, address string, count, wait int) string {
	args := []string{"-c", strconv.Itoa(count)}
	if wait > 0 {
		args = append(args, "-W", strconv.Itoa(wait))
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(count*(wait+2)+5)*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, "ping", append(args, address)...).CombinedOutput()
	return string(out)
}

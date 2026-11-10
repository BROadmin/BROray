package xray

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
	"golang.org/x/sys/unix"
)

// ---------- SOCKS inbound of the active config (shared with the keenetic area) ----------

// SocksInbound returns the first socks inbound of an Xray config file:
// listen ("" when absent) and port (2080 when absent, like 3.x).
func SocksInbound(configPath string) (listen string, port int, found bool) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return "", 0, false
	}
	return socksInboundBytes(b)
}

func socksInboundBytes(b []byte) (string, int, bool) {
	var cfg struct {
		Inbounds []struct {
			Protocol string          `json:"protocol"`
			Listen   any             `json:"listen"`
			Port     json.RawMessage `json:"port"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return "", 0, false
	}
	for _, in := range cfg.Inbounds {
		if in.Protocol != "socks" {
			continue
		}
		listen, _ := in.Listen.(string)
		port := 2080
		if len(in.Port) > 0 && string(in.Port) != "null" {
			var n int
			if json.Unmarshal(in.Port, &n) == nil {
				port = n
			} else {
				var s string
				if json.Unmarshal(in.Port, &s) == nil {
					if v, err := strconv.Atoi(s); err == nil {
						port = v
					} else {
						port = 0
					}
				}
			}
		}
		return listen, port, true
	}
	return "", 0, false
}

// ProcNet is the directory holding tcp/tcp6 listing files (tests).
var ProcNet = "/proc/net"

// SocksListening reports whether a TCP socket listens on host:port, on
// 0.0.0.0:port or on [::]:port (3.x `netstat -lnt` match, xray.sh:250-283).
func SocksListening(host string, port int) bool {
	if port <= 0 {
		return false
	}
	want := net.ParseIP(host)
	for _, name := range []string{"tcp", "tcp6"} {
		f, err := os.Open(filepath.Join(ProcNet, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		first := true
		for sc.Scan() {
			if first {
				first = false
				continue
			}
			fl := strings.Fields(sc.Text())
			if len(fl) < 4 || fl[3] != "0A" {
				continue
			}
			addr, p, ok := parseProcAddr(fl[1])
			if !ok || p != port {
				continue
			}
			if addr.IsUnspecified() || (want != nil && addr.Equal(want)) {
				f.Close()
				return true
			}
		}
		f.Close()
	}
	return false
}

func parseProcAddr(s string) (net.IP, int, bool) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return nil, 0, false
	}
	hb, err := hex.DecodeString(s[:i])
	if err != nil || (len(hb) != 4 && len(hb) != 16) {
		return nil, 0, false
	}
	p, err := strconv.ParseUint(s[i+1:], 16, 16)
	if err != nil {
		return nil, 0, false
	}
	ip := make(net.IP, len(hb))
	for w := 0; w < len(hb); w += 4 { // each 32-bit word is little-endian
		ip[w], ip[w+1], ip[w+2], ip[w+3] = hb[w+3], hb[w+2], hb[w+1], hb[w]
	}
	return ip, int(p), true
}

// ---------- binary helpers ----------

var reVersionToken = regexp.MustCompile(`^[vV]?[0-9]+(\.[0-9A-Za-z_-]+)+$`)
var reArch = regexp.MustCompile(`linux/([A-Za-z0-9_-]+)`)

func childEnv(assetDir string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") || strings.HasPrefix(kv, "LD_PRELOAD=") || strings.HasPrefix(kv, "XRAY_LOCATION_ASSET=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "XRAY_LOCATION_ASSET="+assetDir)
}

// versionOutput runs "<bin> version" (combined output).
func (s *service) versionOutput(ctx context.Context, bin string) string {
	if !executable(bin) {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "version")
	cmd.Env = childEnv(s.d.Root.Runtime())
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// parseVersion is broray_xray_version: the first token of line 1 that looks
// like a version (leading v dropped), else the second field.
func parseVersion(out string) string {
	line := strings.SplitN(out, "\n", 2)[0]
	f := strings.Fields(line)
	for _, tok := range f {
		if reVersionToken.MatchString(tok) {
			if tok[0] == 'v' || tok[0] == 'V' {
				return tok[1:]
			}
			return tok
		}
	}
	if len(f) >= 2 {
		return strings.TrimPrefix(strings.TrimPrefix(f[1], "v"), "V")
	}
	return ""
}

func machine() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return ""
	}
	return unix.ByteSliceToString(u.Machine[:])
}

func archOf(versionOut string) string {
	if m := reArch.FindStringSubmatch(versionOut); m != nil {
		return m[1]
	}
	switch m := machine(); m {
	case "aarch64", "arm64":
		return "arm64"
	case "armv7l", "armv7":
		return "arm"
	case "mipsel", "mipsle":
		return "mipsle"
	case "mips":
		return "mips"
	case "x86_64", "amd64":
		return "amd64"
	case "i386", "i486", "i586", "i686":
		return "386"
	case "":
		return "unknown"
	default:
		return m
	}
}

func executable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0
}

func regular(path string) (os.FileInfo, bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	return st, true
}

type shaCacheEntry struct {
	size int64
	mod  time.Time
	sum  string
}

var (
	shaMu    sync.Mutex
	shaCache = map[string]shaCacheEntry{}
)

// fileSHA256 hashes a file, reusing the previous digest while size and
// mtime are unchanged (the 30 MB binary is hashed once per version).
func fileSHA256(path string) string {
	st, ok := regular(path)
	if !ok {
		return ""
	}
	shaMu.Lock()
	if e, ok := shaCache[path]; ok && e.size == st.Size() && e.mod.Equal(st.ModTime()) {
		shaMu.Unlock()
		return e.sum
	}
	shaMu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	sum := hex.EncodeToString(h.Sum(nil))
	shaMu.Lock()
	shaCache[path] = shaCacheEntry{st.Size(), st.ModTime(), sum}
	shaMu.Unlock()
	return sum
}

// freeKB is `df -k <path>` available KB.
func freeKB(path string) int64 {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize) / 1024
}

// ---------- validation (broray xray validate) ----------

type validation struct {
	Success bool   `json:"success"`
	Config  string `json:"config"`
	Output  any    `json:"output"`
}

// runTest runs "<bin> run -test -c <config>" with XRAY_LOCATION_ASSET.
func (s *service) runTest(ctx context.Context, bin, config string) (string, bool) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "run", "-test", "-c", config)
	cmd.Env = childEnv(s.d.Root.Runtime())
	out, err := cmd.CombinedOutput()
	if cfg, readErr := os.ReadFile(config); readErr == nil && servers.PrivateTransportConfig(cfg) {
		return servers.SafeCoreOutput(cfg, string(out), err == nil), err == nil
	}
	return strings.TrimRight(string(out), "\n"), err == nil
}

func (s *service) validate(ctx context.Context) validation {
	bin, cfg := s.d.Root.XrayBinary(), s.d.Root.XrayConfig()
	v := validation{Config: cfg}
	switch {
	case !executable(bin):
		v.Output = "Исполняемый файл Xray не найден."
	case !fileExists(cfg):
		v.Output = "Файл конфигурации Xray не найден."
	default:
		out, ok := s.runTest(ctx, bin, cfg)
		v.Success = ok
		if out != "" {
			v.Output = out
		}
	}
	return v
}

func fileExists(p string) bool { _, ok := regular(p); return ok }

func tailLines(s string, n int) string {
	l := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

// ---------- status (X1, xray.sh:318-576) ----------

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// socksEndpoint is what status reports: the socks inbound of the active
// config, 127.0.0.1:2080 when there is none (3.x settings defaults).
func (s *service) socksEndpoint() (string, int) {
	listen, port, ok := SocksInbound(s.d.Root.XrayConfig())
	if !ok {
		return "127.0.0.1", 2080
	}
	if listen == "" {
		listen = "0.0.0.0"
	}
	return listen, port
}

func (s *service) computeStatus(ctx context.Context) map[string]any {
	bin, cfg := s.d.Root.XrayBinary(), s.d.Root.XrayConfig()
	now := time.Now()
	st, installed := regular(bin)
	exe := executable(bin)
	var size int64
	if installed {
		size = st.Size()
	}
	sum := ""
	if installed {
		sum = fileSHA256(bin)
	}
	vout := s.versionOutput(ctx, bin)
	version := parseVersion(vout)
	arch := ""
	if installed {
		arch = archOf(vout)
	}
	running := s.d.App.Xray.Running()
	var pid any
	if n := s.d.App.Xray.PID(); running && n > 0 {
		pid = n
	}
	cst, cfgExists := regular(cfg)
	var cfgSize int64
	cfgSum := ""
	if cfgExists {
		cfgSize = cst.Size()
		if b, err := os.ReadFile(cfg); err == nil {
			cfgSum = compat.SHA256Hex(b)
		}
	}
	cfgValid := false
	checkOut := ""
	if exe && cfgExists {
		out, ok := s.runTest(ctx, bin, cfg)
		cfgValid = ok
		if !ok {
			checkOut = tailLines(out, 20)
		}
	}
	addr, port := s.socksEndpoint()
	socksActive := running && SocksListening(addr, port)
	lastErr := ""
	switch {
	case !installed:
		lastErr = "Бинарный файл Xray не найден."
	case version == "":
		lastErr = "Не удалось определить версию Xray."
	case !exe:
		lastErr = "Бинарный файл Xray не является исполняемым."
	case cfgExists && !cfgValid:
		lastErr = "Конфигурация Xray не прошла проверку."
	case !cfgExists:
		lastErr = "Активная конфигурация Xray не найдена."
	}
	updatedAt := compat.Stamp(now)
	var reasons []any
	add := func(cond bool, code, msg string) {
		if cond {
			reasons = append(reasons, map[string]any{"code": code, "message": msg, "details": nil})
		}
	}
	add(!installed, "XRAY_BINARY_MISSING", "Бинарный файл Xray не найден.")
	add(installed && !exe, "XRAY_NOT_EXECUTABLE", "Бинарный файл Xray нельзя запустить.")
	add(!cfgExists, "XRAY_CONFIG_MISSING", "Активная конфигурация Xray не найдена.")
	add(cfgExists && !cfgValid, "XRAY_CONFIG_INVALID", "Конфигурация Xray не прошла проверку.")
	add(!running, "XRAY_PROCESS_STOPPED", "Процесс Xray остановлен.")
	add(running && !socksActive, "XRAY_SOCKS_INACTIVE", "Локальный SOCKS-интерфейс не слушает настроенный адрес и порт.")
	if reasons == nil {
		reasons = []any{}
	}
	severity := "ok"
	if !installed || !exe || !cfgExists || !cfgValid {
		severity = "error"
	} else if !running || !socksActive {
		severity = "warning"
	}
	all := installed && exe && cfgExists && cfgValid && running && socksActive
	health := map[string]any{"schemaVersion": 1, "module": "xray", "availability": "available", "severity": severity,
		"operational": all, "consistent": all || cfgValid, "actionRequired": !all,
		"freshness": map[string]any{"state": "fresh", "checkedAt": updatedAt}, "reasons": reasons,
		"facts": map[string]any{"processRunning": running, "configValid": cfgValid, "socksActive": socksActive, "socksAddress": addr,
			"socksPort": port}, "lastOperation": nil}
	return map[string]any{
		"installed": installed, "executable": exe, "running": running, "pid": pid, "version": nullStr(version),
		"architecture": nullStr(arch), "deviceArchitecture": nullStr(machine()), "binaryPath": bin, "binarySizeBytes": size,
		"binarySha256": nullStr(sum), "configPath": cfg, "configExists": cfgExists, "configSizeBytes": cfgSize,
		"configSha256": nullStr(cfgSum), "configValid": cfgValid, "configCheckOutput": nullStr(checkOut),
		"socksAddress": addr, "socksPort": port, "socksActive": socksActive,
		"socks":  map[string]any{"address": addr, "listen": addr, "port": port, "active": socksActive},
		"health": health, "storagePath": s.d.Root.Dir, "storageFreeKb": freeKB(s.d.Root.Dir), "updateAvailable": false,
		"lastError": nullStr(lastErr), "updatedAt": updatedAt,
	}
}

// statusCached is broray_xray_status_json_cached: reuse a status younger
// than 15 s while the verified PID is unchanged.
func (s *service) statusCached(ctx context.Context) map[string]any {
	pid := ""
	if n := s.d.App.Xray.PID(); n > 0 {
		pid = strconv.Itoa(n)
	}
	s.cacheMu.Lock()
	if s.cache != nil && time.Since(s.cacheAt) < statusCacheTTL && s.cachePID == pid {
		c := s.cache
		s.cacheMu.Unlock()
		return c
	}
	s.cacheMu.Unlock()
	st := s.computeStatus(ctx)
	s.cacheMu.Lock()
	s.cache, s.cacheAt, s.cachePID = st, time.Now(), pid
	s.cacheMu.Unlock()
	return st
}

func (s *service) invalidateStatus() {
	s.cacheMu.Lock()
	s.cache = nil
	s.cacheMu.Unlock()
}

var statusCacheTTL = 15 * time.Second

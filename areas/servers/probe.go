package servers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// probeResult is the bin/broray-server-probe verdict.
type probeResult struct {
	Success   bool
	Stage     string
	Error     string
	WarmupMs  *int64
	LatencyMs *int64
	JitterMs  *int64
}

var qualityProbeURLs = []string{
	"https://cp.cloudflare.com/generate_204",
	"https://www.gstatic.com/generate_204",
	"https://connectivitycheck.gstatic.com/generate_204",
	"https://www.cloudflare.com/cdn-cgi/trace",
}

var activeProbeURLs = []string{
	"https://cp.cloudflare.com/generate_204",
	"https://www.gstatic.com/generate_204",
}

// curlLike classifies a Go HTTP error into a curl exit code (for the 3.x
// stage mapping and the curlCode field).
func curlLike(err error) int {
	if err == nil {
		return 0
	}
	var ne net.Error
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return 28
	case strings.Contains(msg, "no such host"):
		return 6
	case strings.Contains(msg, "socks") && strings.Contains(msg, "connection refused"):
		return 7
	case strings.Contains(msg, "socks"):
		return 97
	case strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate"):
		return 35
	case strings.Contains(msg, "connection refused"):
		return 7
	}
	return 56
}

func stageOfCurl(code int) string {
	switch code {
	case 5, 6:
		return "proxy-dns"
	case 7:
		return "proxy-connect"
	case 28:
		return "proxy-timeout"
	case 35, 51, 53, 58, 59, 60, 64, 66, 77, 80, 82, 83, 90, 91:
		return "proxy-tls"
	case 97:
		return "socks-handshake"
	}
	return "proxy-https"
}

// socksGet performs one curl-like HTTPS GET through a local SOCKS5 proxy
// (remote DNS). It returns the HTTP status (0 if none) and the error.
func socksGet(ctx context.Context, proxy, target string, connectTimeout, maxTime time.Duration, follow bool, maxBody int64) (int, error) {
	pu, _ := url.Parse("socks5h://" + proxy)
	tr := &http.Transport{
		Proxy:               http.ProxyURL(pu),
		DialContext:         (&net.Dialer{Timeout: connectTimeout}).DialContext,
		TLSHandshakeTimeout: maxTime,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		DisableKeepAlives:   true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: maxTime}
	if !follow {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	return resp.StatusCode, nil
}

// ---------- isolated probe (bin/broray-server-probe) ----------

func tailText(b []byte, lines, max int) string {
	ls := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(ls) > lines {
		ls = ls[len(ls)-lines:]
	}
	t := strings.Join(ls, " ") + " "
	if strings.TrimSpace(string(b)) == "" {
		return ""
	}
	if len(t) > max {
		t = t[:max]
	}
	return t
}

func portFree(port int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func (s *svc) isolatedProbe(ctx context.Context, rec Record, cfg []byte) probeResult {
	fail := func(stage, msg string) probeResult {
		return probeResult{Stage: stage, Error: SafeCoreOutput(cfg, msg, false)}
	}
	if err := s.prepareNativeConfig(cfg); err != nil {
		return fail("probe-workdir", err.Error())
	}
	var c map[string]any
	if err := json.Unmarshal(cfg, &c); err != nil {
		return fail("probe-config", "Временная конфигурация сервера не найдена.")
	}
	if obs, ok := c["outbounds"].([]any); !ok || len(obs) == 0 {
		return fail("probe-config", "Сформирована некорректная конфигурация Xray.")
	}
	if err := os.MkdirAll(s.d.Root.Tmp(), 0o700); err != nil {
		return fail("probe-workdir", "Не удалось создать временный каталог.")
	}
	work, err := os.MkdirTemp(s.d.Root.Tmp(), "server-probe-")
	if err != nil {
		return fail("probe-workdir", "Не удалось создать временный каталог.")
	}
	defer os.RemoveAll(work)
	bin := s.d.Root.XrayBinary()
	if st, err := os.Stat(bin); err != nil || st.Mode()&0o111 == 0 {
		return fail("probe-xray", "Бинарный файл Xray недоступен.")
	}
	port := 35000 + os.Getpid()%18000
	for attempt := 0; !portFree(port); {
		port++
		attempt++
		if port > 54999 {
			port = 35000
		}
		if attempt >= 200 {
			return fail("probe-port", "Не удалось подобрать свободный порт.")
		}
	}
	tag := "broray-test-socks"
	if ins, ok := c["inbounds"].([]any); ok && len(ins) > 0 {
		if t := jqRaw(alt(get(ins[0], "tag"), "broray-test-socks")); t != "" {
			tag = t
		}
	}
	c["log"] = map[string]any{"loglevel": "warning"}
	if PrivateTransportConfig(cfg) {
		c["log"] = privateCoreLogSettings()
	}
	c["inbounds"] = []any{map[string]any{"tag": tag, "listen": "127.0.0.1", "port": port, "protocol": "socks",
		"settings": map[string]any{"auth": "noauth", "udp": false},
		"sniffing": map[string]any{"enabled": true, "destOverride": []any{"http", "tls", "quic"}, "routeOnly": true}}}
	data, err := Marshal(c)
	if err != nil {
		return fail("probe-config", "Не удалось подготовить конфигурацию пробника.")
	}
	cfgPath := filepath.Join(work, "config.json")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		return fail("probe-config", "Не удалось подготовить конфигурацию пробника.")
	}
	if err := s.xrayTest(ctx, cfgPath); err != nil {
		msg := tailText([]byte(err.Error()), 25, 800)
		if msg == "" {
			msg = "Конфигурация пробника не прошла проверку."
		}
		return fail("probe-xray-config", msg)
	}
	var logBuf bytes.Buffer
	cmd := exec.Command(bin, "run", "-c", cfgPath)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+s.d.Root.Runtime())
	cmd.Stdout, cmd.Stderr = &logBuf, &logBuf
	if err := cmd.Start(); err != nil {
		return fail("probe-xray-start", "Тестовый Xray завершился после запуска.")
	}
	s.probePIDs.Store(cmd.Process.Pid, true)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	defer func() {
		_ = cmd.Process.Kill()
		<-done
		s.probePIDs.Delete(cmd.Process.Pid)
	}()
	ready := false
	for attempt := 0; attempt < 12 && !ready; attempt++ {
		select {
		case <-done:
			msg := tailText(logBuf.Bytes(), 25, 800)
			if msg == "" {
				msg = "Тестовый Xray завершился после запуска."
			}
			return fail("probe-xray-start", msg)
		default:
		}
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 300*time.Millisecond); err == nil {
			conn.Close()
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			return fail("probe-socks-listen", "Тестовый SOCKS-порт не начал принимать соединения.")
		case <-time.After(time.Second):
		}
	}
	if !ready {
		return fail("probe-socks-listen", "Тестовый SOCKS-порт не начал принимать соединения.")
	}
	if rec["network"] == "xdrive" {
		return measureCloudPath(ctx, func(c context.Context) (int, error) {
			return socksGet(c, "127.0.0.1:"+strconv.Itoa(port), qualityProbeURLs[0], 6*time.Second, 16*time.Second, true, 1<<20)
		})
	}
	lastStage, lastError := "proxy-https", "HTTPS-запрос через сервер не выполнен."
	for _, u := range qualityProbeURLs {
		if ctx.Err() != nil {
			break
		}
		code, err := socksGet(ctx, "127.0.0.1:"+strconv.Itoa(port), u, 6*time.Second, 16*time.Second, true, 1<<20)
		if err == nil && code >= 200 && code < 400 {
			return probeResult{Success: true, Stage: "proxy-https"}
		}
		cc := curlLike(err)
		msg := ""
		if err != nil {
			msg = tailText([]byte(err.Error()), 8, 600)
		} else {
			msg = "curl завершился с кодом 0"
		}
		lastStage, lastError = stageOfCurl(cc), msg
	}
	return probeResult{Stage: lastStage, Error: "Реальный HTTPS-запрос через сервер не прошёл: " + lastError}
}

// ---------- current SOCKS probe (lib/active-proxy-probe.sh) ----------

func localAddress(host string) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	n := 0
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err == nil && ip.String() == host {
			n++
		}
	}
	return n == 1
}

func (s *svc) socksProbe(ctx context.Context, configPath, serverID string) map[string]any {
	endpoint, attempts, code := "", 0, 0
	emit := func(status, errorCode string) map[string]any {
		now := s.now()
		return map[string]any{"method": "current-socks-https", "serverId": serverID, "status": status, "errorCode": errorCode,
			"endpoint": endpoint, "attempts": attempts, "curlCode": code, "checkedAt": compat.StampUTC(now), "checkedEpoch": now.Unix()}
	}
	cfg, ok := readObjectFile(configPath)
	if !ok {
		return emit("unknown", "CONFIG_UNAVAILABLE")
	}
	var socks []map[string]any
	if ins, ok := cfg["inbounds"].([]any); ok {
		for _, in := range ins {
			if m, ok := in.(map[string]any); ok && m["protocol"] == "socks" {
				socks = append(socks, m)
			}
		}
	}
	if len(socks) != 1 {
		return emit("unknown", "SOCKS_ENDPOINT_UNSUPPORTED")
	}
	in := socks[0]
	p, ok := isInt(in["port"])
	if !ok || p < 1 || p > 65535 || alt(get(in, "settings", "auth"), "noauth") != "noauth" {
		return emit("unknown", "SOCKS_ENDPOINT_UNSUPPORTED")
	}
	host, ok := alt(in["listen"], "0.0.0.0").(string)
	if !ok {
		return emit("unknown", "SOCKS_ENDPOINT_UNSUPPORTED")
	}
	switch host {
	case "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	port := strconv.Itoa(int(p))
	if strings.Contains(host, ":") {
		for _, r := range host {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r == ':') {
				return emit("unknown", "SOCKS_ENDPOINT_UNSUPPORTED")
			}
		}
		endpoint = "[" + host + "]:" + port
	} else {
		parts := strings.Split(host, ".")
		valid := len(parts) == 4
		for _, part := range parts {
			n, err := strconv.Atoi(part)
			if len(part) < 1 || len(part) > 3 || !allDigits(part) || err != nil || n > 255 {
				valid = false
			}
		}
		if !valid {
			return emit("unknown", "SOCKS_ENDPOINT_UNSUPPORTED")
		}
		endpoint = host + ":" + port
	}
	if !localAddress(host) {
		return emit("unknown", "SOCKS_ADDRESS_NOT_LOCAL")
	}
	for _, u := range activeProbeURLs {
		attempts++
		status, err := socksGet(ctx, endpoint, u, 3*time.Second, 6*time.Second, false, 4096)
		code = curlLike(err)
		if err == nil && status == 204 {
			return emit("healthy", "")
		}
	}
	return emit("unhealthy", "PROXY_HTTPS_FAILED")
}

// activeMeasure is broray_active_proxy_measure: health "true"|"false"|"unknown".
func (s *svc) activeMeasure(ctx context.Context, id string) (health string, result map[string]any, context string) {
	result = map[string]any{"method": "current-socks-https", "status": "unknown", "errorCode": "ACTIVE_CONTEXT_UNAVAILABLE"}
	before := s.runtimeContext(id)
	if before == "" {
		return "unknown", result, ""
	}
	pctx, cancel := contextWithTimeout(ctx, 25*time.Second)
	r := s.activeProbe(pctx, s.d.Root.XrayConfig(), id)
	cancel()
	after := s.runtimeContext(id)
	status, _ := r["status"].(string)
	_, epochOK := num(r["checkedEpoch"])
	_, atOK := r["checkedAt"].(string)
	if after != before || r["method"] != "current-socks-https" || r["serverId"] != id || !in(status, "healthy", "unhealthy", "unknown") || !epochOK || !atOK {
		return "unknown", map[string]any{"method": "current-socks-https", "status": "unknown", "errorCode": "ACTIVE_CONTEXT_CHANGED_OR_INCOMPLETE"}, ""
	}
	r["context"] = before
	switch status {
	case "healthy":
		return "true", r, before
	case "unhealthy":
		return "false", r, before
	}
	return "unknown", r, before
}

func contextWithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

// ---------- ping parsing (server-service.sh:704-758) ----------

func awkNum(s string) float64 {
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '.' || (end == 0 && (s[end] == '-' || s[end] == '+'))) {
		end++
	}
	f, _ := strconv.ParseFloat(s[:end], 64)
	return f
}

// parsePing returns round(avg) and round(max)-round(min) of a ping summary.
func parsePing(out string) (ping, jitter any) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "min/avg/max") {
			continue
		}
		parts := strings.Split(line, "=")
		if len(parts) < 2 {
			return nil, nil
		}
		v := strings.Join(strings.Fields(parts[1]), "")
		vals := strings.Split(v, "/")
		get := func(i int) int64 {
			if i >= len(vals) {
				return int64(math.Floor(0.5))
			}
			return int64(math.Floor(awkNum(vals[i]) + 0.5))
		}
		avg, mn, mx := get(1), get(0), get(2)
		if avg < 0 {
			return nil, nil
		}
		if mn < 0 || mx < 0 {
			return avg, nil
		}
		return avg, mx - mn
	}
	return nil, nil
}

// rating is server-service.sh:793-803.
func rating(ping any) string {
	p, ok := num(ping)
	switch {
	case !ok:
		return "acceptable"
	case p <= 80:
		return "excellent"
	case p <= 150:
		return "good"
	case p <= 300:
		return "acceptable"
	}
	return "poor"
}

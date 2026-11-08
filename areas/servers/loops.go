package servers

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func (s *svc) logPath() string { return filepath.Join(s.d.Root.Logs(), "server-auto-switch.log") }

// logf appends one line to logs/server-auto-switch.log (bounded like 3.x logs).
func (s *svc) logf(format string, args ...any) {
	appendLog(s.logPath(), s.now(), fmt.Sprintf(format, args...))
}

func appendLog(path string, now time.Time, line string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if st, err := os.Stat(path); err == nil && st.Size() > 262144 {
		if b, err := os.ReadFile(path); err == nil {
			lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
			if len(lines) > 600 {
				lines = lines[len(lines)-600:]
			}
			_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", compat.Stamp(now), line)
}

// ---------- auto-switch daemon (bin/broray-server-auto-switch, 15 s) ----------

func (s *svc) autoSwitchLoop(ctx context.Context) {
	s.loopRunning.Store(true)
	defer s.loopRunning.Store(false)
	s.logf("SERVER_AUTO_SWITCH_PROCESS=START PID=%d", os.Getpid())
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		if err := s.enqueueDue(); err != nil {
			s.logf("Цикл очереди завершился ошибкой: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// enqueueDue is broray_auto_enqueue_due (active-proxy-health.sh:224-302).
func (s *svc) enqueueDue() error {
	if s.d.Queue.Paused() {
		return nil
	}
	ac, ok := s.readAutoConfig()
	if !ok {
		if _, err := os.Stat(autoSwitchFile(s.d)); os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("server-auto-switch.json is not a JSON object")
	}
	state, ok := s.readState()
	if !ok {
		return fmt.Errorf("server-auto-switch-state.json is not a JSON object")
	}
	now := s.now().Unix()
	active := ActiveID(s.d)
	if active == "" && ac.enabled {
		if err := s.enqueueReplacement(state); err != nil {
			return err
		}
	}
	if active != "" {
		if context := s.runtimeContext(active); context != "" {
			ep, okE := num(get(state, "activeHealth", "checkedEpoch"))
			recent := state["autoConfigSha256"] == ac.hash && get(state, "activeHealth", "serverId") == active &&
				get(state, "activeHealth", "context") == context && get(state, "activeHealth", "method") == "current-socks-https" &&
				okE && ep <= float64(now) && float64(now)-ep < 15
			if !recent && !s.failoverPending(state, active, context, ac.hash) {
				if _, err := s.d.Queue.Enqueue(actionHealth, active, "AUTO_SWITCH", context); err != nil {
					return fmt.Errorf("active-health: %v", err)
				}
			}
		}
	}
	if ac.raw["qualityRefreshEnabled"] == true {
		interval := intervalOf(ac.raw)
		qr, _ := state["qualityRefresh"].(map[string]any)
		next, okN := num(get(qr, "nextCheckEpoch"))
		due := !(jsonEq(get(qr, "intervalMinutes"), interval) && okN && next > float64(now) &&
			in(asString(get(qr, "status")), "success", "scheduled", "partial", "error"))
		if due {
			context, err := s.qualityContext("all", "SERVER_CHECK_AUTO")
			if err != nil {
				return fmt.Errorf("quality context: %v", err)
			}
			if _, err := s.d.Queue.Enqueue(actionQuality, "all", "SERVER_CHECK_AUTO", context); err != nil {
				return fmt.Errorf("quality: %v", err)
			}
		}
	}
	return nil
}

// ---------- connection monitor (bin/broray-connection-monitor, 10 s) ----------

var (
	reLoss  = regexp.MustCompile(` ([0-9]+)% packet loss`)
	reAvg   = regexp.MustCompile(`= [^/]*/([^/]*)/`)
	reTimes = regexp.MustCompile(`time=([0-9.]+) ms`)
)

func connectionQuality(avg, jitter, loss float64) float64 {
	score := 100 - loss
	switch {
	case avg > 300:
		score -= 45
	case avg > 200:
		score -= 30
	case avg > 120:
		score -= 15
	case avg > 80:
		score -= 5
	}
	switch {
	case jitter > 50:
		score -= 30
	case jitter > 30:
		score -= 20
	case jitter > 15:
		score -= 10
	case jitter > 8:
		score -= 5
	}
	return math.Max(0, math.Min(100, score))
}

func qualityLabel(score float64) string {
	switch {
	case score >= 90:
		return "Отлично"
	case score >= 75:
		return "Хорошо"
	case score >= 50:
		return "Средне"
	}
	return "Плохо"
}

// monitorStep computes the next connection-status.json document.
func (s *svc) monitorStep(ctx context.Context) map[string]any {
	now := s.now().Unix()
	address := ""
	if id := ActiveID(s.d); id != "" {
		if rec, err := ReadRecord(s.d, id); err == nil {
			address = jqRawEmpty(rec["address"])
		}
	}
	breaks, previousUp, previousAddress := int64(0), any(nil), ""
	if prev, ok := readObjectFile(s.d.RunPath("connection-status.json")); ok {
		if b, ok := isInt(prev["breaks"]); ok && b >= 0 {
			breaks = int64(b)
		}
		if u, ok := prev["up"].(bool); ok {
			previousUp = u
		}
		previousAddress, _ = prev["address"].(string)
	}
	if address == "" {
		return map[string]any{"available": false, "up": false, "address": "", "ping_ms": nil, "jitter_ms": nil,
			"packet_loss_percent": nil, "quality_score": 0, "quality": "Нет сервера", "checked_at": now, "breaks": breaks}
	}
	if address != previousAddress {
		previousUp = nil
	}
	out := s.ping(ctx, address, 5, 2)
	loss := int64(100)
	if m := reLoss.FindAllStringSubmatch(out, -1); len(m) > 0 {
		loss, _ = strconv.ParseInt(m[len(m)-1][1], 10, 64)
	}
	avgText := ""
	if m := reAvg.FindAllStringSubmatch(out, -1); len(m) > 0 {
		avgText = m[len(m)-1][1]
	}
	if avgText != "" && loss < 100 {
		avg, _ := strconv.ParseFloat(avgText, 64)
		var times []float64
		for _, m := range reTimes.FindAllStringSubmatch(out, -1) {
			f, _ := strconv.ParseFloat(m[1], 64)
			times = append(times, f)
		}
		jitter := 0.0
		if len(times) > 1 {
			total := 0.0
			for i := 1; i < len(times); i++ {
				total += math.Abs(times[i] - times[i-1])
			}
			jitter = total / float64(len(times)-1)
		}
		jitter = math.Round(jitter*10) / 10
		score := math.Round(connectionQuality(avg, jitter, float64(loss)))
		return map[string]any{"available": true, "up": true, "address": address, "ping_ms": math.Round(avg),
			"jitter_ms": jitter, "packet_loss_percent": loss, "quality_score": score, "quality": qualityLabel(score),
			"checked_at": now, "breaks": breaks}
	}
	if previousUp == true {
		breaks++
	}
	return map[string]any{"available": true, "up": false, "address": address, "ping_ms": nil, "jitter_ms": nil,
		"packet_loss_percent": loss, "quality_score": 0, "quality": "Нет связи", "checked_at": now, "breaks": breaks}
}

func (s *svc) monitorLoop(ctx context.Context) {
	s.monitorRunning.Store(true)
	defer s.monitorRunning.Store(false)
	logPath := filepath.Join(s.d.Root.Logs(), "connection-monitor.log")
	appendLog(logPath, s.now(), fmt.Sprintf("MONITOR_PROCESS=START PID=%d", os.Getpid()))
	for {
		doc := s.monitorStep(ctx)
		if ctx.Err() != nil {
			return
		}
		if err := compat.WriteJSON(s.d.RunPath("connection-status.json"), doc); err != nil {
			appendLog(logPath, s.now(), "MONITOR_STATUS_PUBLICATION=FAIL REASON=atomic-rename")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

package servers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func TestCloudMonitorUsesExistingTunnelHealth(t *testing.T) {
	e, r := cloudTestEnv(t)
	id := asString(r["id"])
	if _, err := e.s.activate(id); err != nil {
		t.Fatal(err)
	}
	pings, probes := 0, 0
	e.s.ping = func(context.Context, string, int, int) string { pings++; return pingOut }
	e.s.measure = func(context.Context, Record, []byte) probeResult { probes++; return probeResult{} }
	e.s.activeProbe = func(context.Context, string, string) map[string]any { probes++; return nil }
	for _, status := range []string{"healthy", "unhealthy", "expired", "different-context"} {
		t.Run(status, func(t *testing.T) {
			h := map[string]any{"serverId": id, "context": e.s.runtimeContext(id), "method": "current-socks-https", "checkedEpoch": e.s.now().Unix(), "status": status}
			if status == "expired" {
				h["status"], h["checkedEpoch"] = "healthy", e.s.now().Unix()-121
			}
			if status == "different-context" {
				h["status"], h["context"] = "healthy", "obsolete-runtime"
			}
			if err := e.s.updateState(func(st map[string]any) bool { st["activeHealth"] = h; return true }); err != nil {
				t.Fatal(err)
			}
			got := e.s.monitorStep(context.Background())
			if pings != 0 || probes != 0 {
				t.Error("cloud monitor probed formal endpoint or started another probe")
			}
			if got["measurementMethod"] != "current-socks-https" || got["ping_ms"] != nil || got["quality_score"] != nil {
				t.Error("monitor fabricated ICMP quality")
			}
			if got["up"] != (status == "healthy") || got["available"] != (status == "healthy" || status == "unhealthy") {
				t.Errorf("wrong freshness/health: %v", got)
			}
		})
	}
}

func TestCloudScheduledChecksCoalesce(t *testing.T) {
	e, _ := cloudTestEnv(t)
	if err := compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"schemaVersion": 3, "enabled": false, "qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 30}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := e.s.enqueueDue(); err != nil {
			t.Fatal(err)
		}
	}
	if rows := e.d.Queue.QueueRows(); len(rows) != 1 || rows[0]["source"] != "SERVER_CHECK_AUTO" {
		t.Fatalf("repeated ticks duplicated cloud jobs: %v", rows)
	}
}

func cloudTestEnv(t *testing.T) (*fakeEnv, Record) {
	t.Helper()
	e := newEnv(t)
	if err := os.WriteFile(e.d.Root.XrayBinary(), []byte("#!/bin/sh\necho 'Xray 26.11.9 (test core)'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	rec := parseNativeFixture(t, nativeTestOutbound("xdrive"))
	if err := WriteRecord(e.d, rec); err != nil {
		t.Fatal(err)
	}
	return e, rec
}

func TestCloudProbeBounds(t *testing.T) {
	for _, status := range []int{401, 403, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			got := measureCloudPath(context.Background(), func(context.Context) (int, error) { calls++; return status, nil })
			want := 2
			if status == 401 || status == 403 {
				want = 1
			}
			if got.Success || calls != want {
				t.Fatalf("status=%d calls=%d want=%d", status, calls, want)
			}
		})
	}
	t.Run("cancel-backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		start := time.Now()
		got := measureCloudPath(ctx, func(context.Context) (int, error) { calls++; cancel(); return 429, nil })
		if got.Success || calls != 1 || got.Stage != "proxy-cancelled" || time.Since(start) > 500*time.Millisecond {
			t.Fatal("cancel ignored during retry pause")
		}
	})
	t.Run("warm-samples", func(t *testing.T) {
		calls := 0
		got := measureCloudPath(context.Background(), func(context.Context) (int, error) { calls++; return 204, nil })
		if !got.Success || calls != 4 || got.WarmupMs == nil || got.LatencyMs == nil || got.JitterMs == nil {
			t.Fatal("warmup/samples contract changed")
		}
	})
	t.Run("sample-failure", func(t *testing.T) {
		calls := 0
		got := measureCloudPath(context.Background(), func(context.Context) (int, error) {
			calls++
			if calls == 1 {
				return 204, nil
			}
			return 0, errors.New("synthetic-private-token")
		})
		if got.Success || calls != 2 || strings.Contains(got.Error, "synthetic-private") {
			t.Fatal("sample error unbounded or leaked")
		}
	})
}

func TestCloudProbePolicy(t *testing.T) {
	e, r := cloudTestEnv(t)
	pings := 0
	e.s.ping = func(context.Context, string, int, int) string { pings++; return pingOut }
	e.s.measure = func(context.Context, Record, []byte) probeResult {
		return probeResult{Success: true, Stage: "proxy-https"}
	}
	result, err := e.s.checkServer(context.Background(), asString(r["id"]), "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	q := result.Object["quality"].(map[string]any)
	if pings != 0 || q["ping"] != nil || q["jitter"] != nil {
		t.Fatal("cloud quality used ICMP of formal endpoint")
	}
	if q["measurementMethod"] != "https-proxy-warm" {
		t.Fatal("cloud measurement method not distinguished")
	}
}

func TestXDriveSecrets(t *testing.T) {
	for _, stage := range []string{"core-check", "probe"} {
		t.Run(stage, func(t *testing.T) {
			e, r := cloudTestEnv(t)
			if stage == "core-check" {
				e.s.xrayTest = func(context.Context, string) error { return errors.New("synthetic-private-token from core validation") }
			} else {
				e.s.measure = func(context.Context, Record, []byte) probeResult {
					return probeResult{Error: "synthetic-private-token from cloud response", Stage: "proxy-timeout"}
				}
			}
			result, err := e.s.checkServer(context.Background(), asString(r["id"]), "manual", "")
			b, _ := Marshal(result.Object)
			if err != nil && strings.Contains(err.Error(), "synthetic-private") || strings.Contains(string(b), "synthetic-private") {
				t.Fatal("private backend output exposed in quality/API")
			}
		})
	}
}

func TestNativePrivateCoreLogPolicy(t *testing.T) {
	for _, kind := range []string{"xdrive", "masque"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			r := parseNativeFixture(t, nativeTestOutbound(kind))
			cfg, err := XrayConfig(r, GenSettings{Listen: "127.0.0.1", SocksPort: 2080, LogLevel: "debug", AccessLog: "/private/access.log", ErrorLog: "/private/error.log"}, "test")
			if err != nil {
				t.Fatal(err)
			}
			check := func(b []byte) {
				m, ok := decodeObject(b)
				if !ok || get(m, "log", "loglevel") != "none" || get(m, "log", "access") != "none" || get(m, "log", "error") != "none" {
					t.Error("core could persist cloud response credentials in raw logs")
				}
			}
			check(cfg)
			if err := os.WriteFile(e.d.Root.XrayBinary(), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
				t.Fatal(err)
			}
			called := false
			e.s.xrayTest = func(_ context.Context, path string) error {
				called = true
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				check(b)
				return errors.New("synthetic-private-check-failed")
			}
			got := e.s.isolatedProbe(context.Background(), r, cfg)
			if !called || strings.Contains(got.Error, "synthetic-private") {
				t.Fatal("isolated probe validation absent or leaked")
			}
		})
	}
}

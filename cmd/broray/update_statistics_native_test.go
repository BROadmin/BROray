//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/installstats"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

func TestSignedArchivesUpdateStatisticsThroughNativeDaemon(t *testing.T) {
	binaries := os.Getenv("BRORAY_STATS_NATIVE_BIN_DIR")
	if binaries == "" {
		t.Skip("requires the three isolated native-daemon fixture builds")
	}
	base := t.TempDir()
	e := &e2e{t: t, root: platform.Root{Dir: filepath.Join(base, "broray")},
		opt: filepath.Join(base, "opt"), ch: filepath.Join(base, "channel")}
	key, err := update.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	e.key = key
	e.env = append(os.Environ(), "BRORAY_ROOT="+e.root.Dir, "BRORAY_OPT="+e.opt,
		"BRORAY_E2E_USE_NATIVE_DAEMON=1", "BRORAY_FAKE_TRUSTED_KEY="+key.Public().Encode(),
		"BRORAY_UPDATE_STOP_TIMEOUT=12", "BRORAY_UPDATE_READY_TIMEOUT=12")
	if err := os.MkdirAll(e.ch, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"etc/init.d", "bin", "var/log"} {
		if err := os.MkdirAll(filepath.Join(e.opt, path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	previous, next := "3.5.9", "3.5.10"
	if output, code := e.run(filepath.Join(binaries, "legacy"), "installer", "install", "--no-start"); code != 0 {
		t.Fatalf("initial native install: %s", output)
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	cfg := platform.DefaultConfig()
	cfg.ListenAddress, cfg.WebPort = "127.0.0.1", port
	cfg.Keenetic.WriteTransport, cfg.Keenetic.RCIURL = "rci", "http://127.0.0.1:1"
	cfg.Update.Channel, cfg.Update.ChannelURL = "test", "file://"+filepath.Join(e.ch, "manifest.json")
	if err := platform.SaveConfig(e.root, cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !platform.Held(e.root.DaemonLock()) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := control.Shutdown(ctx, e.root); err != nil {
			t.Error(err)
			return
		}
		lock, err := control.WaitLock(e.root.DaemonLock(), 7*time.Second)
		if err != nil {
			t.Error(err)
			return
		}
		_ = lock.Release()
	})
	e.initd("start")
	wantStatisticsNativeBuild(t, e.root, port, filepath.Join(binaries, "legacy"), previous, "r9")
	queueDir := filepath.Join(e.root.State(), "install-statistics")
	if err := os.Mkdir(queueDir, 0700); err != nil {
		t.Fatal(err)
	}
	queueLock, err := platform.TryLockNoFollow(filepath.Join(queueDir, ".queue.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer queueLock.Release()
	e.bins = map[string]string{next: filepath.Join(binaries, "target")}
	e.publish(next)
	observed, raw := e.launch("--version", next)
	if !*observed.OK || observed.State != installer.JCommitted {
		t.Fatalf("first signed archive update failed: %s", raw)
	}
	wantStatisticsNativeBuild(t, e.root, port, e.bins[next], next, "r14")
	first := e.journal()
	if first == nil || first.Statistics == nil || !first.StatisticsReady {
		t.Fatal("first confirmed observation was not kept while the queue was held")
	}
	e.bins[next] = filepath.Join(binaries, "revision")
	e.publish(next)
	observed, raw = e.launch("--reinstall")
	if !*observed.OK || observed.State != installer.JCommitted {
		t.Fatalf("same-version revision update failed: %s", raw)
	}
	wantStatisticsNativeBuild(t, e.root, port, e.bins[next], next, "r15")
	second := e.journal()
	if second == nil || second.Statistics == nil || !second.StatisticsReady ||
		len(second.DeferredStatistics) != 1 || second.DeferredStatistics[0].Event != *first.Statistics {
		t.Fatal("second committed journal did not retain the first frozen observation")
	}
	if err := queueLock.Release(); err != nil {
		t.Fatal(err)
	}
	if output, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover"); code != 0 {
		t.Fatalf("cannot recover committed local statistics: %s", output)
	}
	wantQueuedNativeStatistics(t, queueDir, *first.Statistics, *second.Statistics)
	observed, raw = e.launch("--reinstall")
	if !*observed.OK || observed.State != installer.JCommitted {
		t.Fatalf("identical signed archive could not be reinstalled: %s", raw)
	}
	wantQueuedNativeStatistics(t, queueDir, *first.Statistics, *second.Statistics)
}

func wantStatisticsNativeBuild(t *testing.T, root platform.Root, port int, fixture, version, revision string) {
	t.Helper()
	hash, err := installstats.ExecutableDigest(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if live, err := control.ObserveBuild(context.Background(), root, hash); err != nil ||
		live.SHA256 != hash || live.Version != version || live.Revision != revision || live.Architecture != update.Platform() {
		t.Fatalf("unexpected native observation for %s %s: %v", version, revision, err)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	page, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	if err != nil || response.StatusCode != 200 || !strings.Contains(string(page), "<title>BROray") {
		t.Fatalf("running native WebUI did not confirm startup: %v", err)
	}
}

func wantQueuedNativeStatistics(t *testing.T, dir string, events ...installstats.Event) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "event-*.json"))
	if err != nil || len(files) != len(events) {
		t.Fatalf("expected %d observations, found %d: %v", len(events), len(files), err)
	}
	for _, expected := range events {
		raw, err := os.ReadFile(filepath.Join(dir, "event-"+expected.EventID+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var body struct{ Event json.RawMessage }
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		observed, err := installstats.ParseEvent(body.Event)
		if err != nil || observed != expected {
			t.Fatalf("archive event differs from the exact frozen native record: %v", err)
		}
	}
	intent, err := os.Stat(filepath.Join(dir, "first-install.json"))
	if !errors.Is(err, os.ErrNotExist) || intent != nil {
		t.Fatal("no-start legacy install unexpectedly created install statistics")
	}
}

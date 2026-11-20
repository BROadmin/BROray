package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
	"golang.org/x/sys/unix"
)

func TestReserveBackupProbeOwnsIsolatedCoreAndPreservesActive(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	before := f.process.RuntimeSnapshot()
	config, _ := os.ReadFile(f.e.d.Root.XrayConfig())
	active, _ := os.ReadFile(ActiveFile(f.e.d))
	var target reserve.ProbeTarget
	calls := 0
	got, err := f.e.s.probeReserveBackupWith(f.ctx, f.backup, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
		calls++
		target = p
		env, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", p.Process.PID))
		if err != nil || !bytes.Contains(env, []byte("XRAY_LOCATION_ASSET="+f.e.d.Root.Runtime()+"\x00")) {
			t.Fatal("installed assets not retained", err)
		}
		if p.Process.PID == before.PID || p.ConfigPath == f.e.d.Root.XrayConfig() || !p.Endpoint.Addr().IsLoopback() {
			t.Fatal("probe not isolated")
		}
		data, err := os.ReadFile(p.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		in := cfg["inbounds"].([]any)
		if len(in) != 2 {
			t.Fatal("unexpected isolated inbounds")
		}
		for _, entry := range in {
			if entry.(map[string]any)["listen"] != "127.0.0.1" {
				t.Fatal("non-loopback listener")
			}
		}
		return reserveVerifiedOutcome(ctx, p)
	})
	if err != nil || calls != 1 || !got.Verified || got.Verdict != "healthy" || got.ServerID != f.backup || got.Identity == "" {
		t.Fatalf("owned isolated result: %+v %v calls%d", got, err, calls)
	}
	plan, err := f.e.s.prepareReserveRuntime(f.ctx, f.backup, netip.MustParseAddrPort("127.0.0.1:45678"))
	if err != nil || got.Fingerprint != plan.node.Fingerprint {
		t.Fatal("profile fingerprint drift", err)
	}
	if f.process.RuntimeSnapshot() != before {
		t.Fatal("active process changed")
	}
	after, _ := os.ReadFile(f.e.d.Root.XrayConfig())
	afterActive, _ := os.ReadFile(ActiveFile(f.e.d))
	if !bytes.Equal(after, config) || !bytes.Equal(afterActive, active) {
		t.Fatal("active files changed")
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(target.ConfigPath))); !os.IsNotExist(err) {
		t.Fatal("owned work directory retained", err)
	}
	if identity, err := reserve.CaptureProcess(target.Process.PID, target.Process.Executable); err == nil && identity == target.Process {
		t.Fatal("temporary child still live")
	}
}

func TestReserveBackupProbeRevalidatesAfterChildCleanup(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	old := xray.StopTimeouts
	xray.StopTimeouts = [2]time.Duration{250 * time.Millisecond, 2 * time.Second}
	defer func() { xray.StopTimeouts = old }()
	recordPath := RecordPath(f.e.d, f.backup)
	record, readErr := os.ReadFile(recordPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	changed := make(chan error, 1)
	called := false
	got, err := f.e.s.probeReserveBackupWith(f.ctx, f.backup, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
		called = true
		out := reserveVerifiedOutcome(ctx, p)
		fd, e := unix.PidfdOpen(p.Process.PID, 0)
		if e != nil {
			t.Fatal(e)
		}
		defer unix.Close(fd)
		if _, e := reserve.VerifyProbeTarget(p); e != nil {
			t.Fatal(e)
		}
		if e := unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0); e != nil {
			t.Fatal(e)
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			changed <- os.WriteFile(recordPath, append(record, ' '), 0600)
		}()
		return out
	})
	if !called {
		t.Fatal("cleanup fixture callback not reached", err)
	}
	select {
	case changeErr := <-changed:
		if changeErr != nil {
			t.Fatal(changeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup fixture mutation did not finish")
	}
	if err != nil || got.Verified || got.Verdict != "unknown" {
		t.Fatalf("authority changed while stopping child: %+v %v", got, err)
	}
}

func TestReserveBackupProbeAllowsFencedDisabledPreflight(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
	f.e.d.Svc.RoutesConfigureXray = func(context.Context, []byte) ([]byte, error) {
		t.Error("backup invoked active routing compiler")
		return nil, errors.New("no user routing")
	}
	got, err := f.e.s.probeReserveBackupWith(f.ctx, f.backup, reserveVerifiedOutcome)
	if err != nil || !got.Verified || got.Verdict != "healthy" {
		t.Fatalf("configured OFF preflight: %+v %v", got, err)
	}
	prefs, err := readReservePreferences(f.e.d)
	if err != nil || prefs.Enabled {
		t.Fatal("preflight enabled mode", err)
	}
}

func TestReserveBackupProbeUsesRealUnhealthyTrafficVerdict(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	got, err := f.e.s.probeReserveBackup(f.ctx, f.backup)
	if err != nil || !got.Verified || got.Verdict != "unhealthy" {
		t.Fatalf("dead upstream is not healthy: %+v %v", got, err)
	}
}

func TestReserveBackupProbeRejectsChangedAuthority(t *testing.T) {
	for _, kind := range []string{"record", "roster", "active", "main-stop", "config", "cancel", "foreign-proof"} {
		t.Run(kind, func(t *testing.T) {
			f := newReserveRuntimeFixture(t)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			got, err := f.e.s.probeReserveBackupWith(ctx, f.backup, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
				out := reserveVerifiedOutcome(ctx, p)
				switch kind {
				case "record":
					path := RecordPath(f.e.d, f.backup)
					b, e := os.ReadFile(path)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(path, append(b, ' '), 0600); e != nil {
						t.Fatal(e)
					}
				case "roster":
					writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
				case "active":
					if e := setActiveID(f.e.d, f.backup); e != nil {
						t.Fatal(e)
					}
				case "main-stop":
					if e := f.process.Stop(context.Background()); e != nil {
						t.Fatal(e)
					}
				case "config":
					if e := os.WriteFile(p.ConfigPath, []byte(`{}`), 0600); e != nil {
						t.Fatal(e)
					}
				case "cancel":
					cancel()
				case "foreign-proof":
					out.Identity = strings.Repeat("0", 64)
				}
				return out
			})
			if err != nil || got.Verified || got.Verdict != "unknown" {
				t.Fatalf("stale evidence: %+v %v", got, err)
			}
		})
	}
}

func TestReserveBackupProbeRefusesUnfencedOrUnconfiguredWork(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	for _, tc := range []struct {
		ctx context.Context
		id  string
	}{{context.Background(), f.backup}, {f.ctx, "missing"}, {f.ctx, f.plan.node.ID}} {
		called := false
		got, err := f.e.s.probeReserveBackupWith(tc.ctx, tc.id, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
			called = true
			return reserveVerifiedOutcome(ctx, p)
		})
		if called || got.Verified || err == nil {
			t.Fatal("invalid probe admitted")
		}
	}
}

func TestReserveBackupProbeRetainsForeignCleanupEntry(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	var work string
	var target reserve.ProbeTarget
	got, err := f.e.s.probeReserveBackupWith(f.ctx, f.backup, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
		target = p
		out := reserveVerifiedOutcome(ctx, p)
		work = filepath.Dir(filepath.Dir(p.ConfigPath))
		if e := os.Rename(work, work+"-retained"); e != nil {
			t.Fatal(e)
		}
		if e := os.Mkdir(work, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(work, "foreign"), []byte("keep"), 0600); e != nil {
			t.Fatal(e)
		}
		return out
	})
	if err == nil || got.Verified || work == "" {
		t.Fatalf("unsafe cleanup: %+v %v", got, err)
	}
	b, e := os.ReadFile(filepath.Join(work, "foreign"))
	if e != nil || string(b) != "keep" {
		t.Fatal("foreign entry removed", e)
	}
	if identity, e := reserve.CaptureProcess(target.Process.PID, target.Process.Executable); e == nil && identity == target.Process {
		t.Fatal("temporary child survived cleanup refusal")
	}
}

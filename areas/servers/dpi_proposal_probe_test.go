package servers

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type dpiProposalProber interface {
	probeDPIProposalWith(context.Context, string, string, Record, func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (reserve.Sample, error)
}

func TestDPIProposalProbeCapability(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	id := f.e.addServer(t, "trojan://synthetic@127.0.0.1:3#Outside")
	path := RecordPath(f.e.d, id)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := ReadRecord(f.e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := prepareDPIRecord(rec, true)
	if err != nil {
		t.Fatal(err)
	}
	prober, ok := any(f.e.s).(dpiProposalProber)
	if !ok {
		t.Fatal("owned DPI proposal validation capability absent")
	}
	snapshot := f.process.RuntimeSnapshot()
	sample, err := prober.probeDPIProposalWith(f.ctx, path, compat.SHA256Hex(before), candidate, reserveVerifiedOutcome)
	if err != nil || !sample.Verified || sample.Verdict != "healthy" || sample.ServerID != id {
		t.Fatal("proposal ownership unconfirmed", sample, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || f.process.RuntimeSnapshot() != snapshot {
		t.Fatal("proposal changed main or record", err)
	}
}

func dpiProposalInput(t *testing.T, f reserveRuntimeFixture, id string) (string, []byte, Record) {
	t.Helper()
	path := RecordPath(f.e.d, id)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := ReadRecord(f.e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	next, err := prepareDPIRecord(rec, true)
	if err != nil {
		t.Fatal(err)
	}
	return path, raw, next
}

func TestDPIProposalProbePreservesMainIntent(t *testing.T) {
	for _, mode := range []string{"active", "stopped", "no-selection", "disabled-source"} {
		t.Run(mode, func(t *testing.T) {
			f := newReserveRuntimeFixture(t)
			id := f.plan.node.ID
			if mode != "active" {
				id = f.e.addServer(t, "trojan://pw@127.0.0.1:3#Outside")
			}
			path, raw, next := dpiProposalInput(t, f, id)
			switch mode {
			case "stopped", "no-selection":
				if err := f.process.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				if mode == "no-selection" {
					if err := os.Remove(ActiveFile(f.e.d)); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(f.e.d.Root.XrayConfig()); err != nil {
						t.Fatal(err)
					}
				}
			case "disabled-source":
				old, err := ReadRecord(f.e.d, id)
				if err != nil {
					t.Fatal(err)
				}
				old["source"] = map[string]any{"type": "subscription", "subscriptionId": "synthetic-sub", "enabled": false}
				raw, err = Marshal(old)
				if err != nil {
					t.Fatal(err)
				}
				next, err = prepareDPIRecord(old, true)
				if err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(f.e.d.Path("config", "disabled-subscription-servers"), "synthetic-sub", id+".json")
				if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(RecordPath(f.e.d, id)); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := f.process.RuntimeSnapshot()
			active, e1 := dpiReadProbeImage(ActiveFile(f.e.d), 4096)
			config, e2 := dpiReadProbeImage(f.e.d.Root.XrayConfig(), 2<<20)
			if e1 != nil || e2 != nil {
				t.Fatal(e1, e2)
			}
			got, err := f.e.s.probeDPIProposalWith(f.ctx, path, compat.SHA256Hex(raw), next, reserveVerifiedOutcome)
			if err != nil || !got.Verified || got.Verdict != "healthy" {
				t.Fatal(got, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, raw) || snapshot != f.process.RuntimeSnapshot() {
				t.Fatal("persistent mutation", err)
			}
			a, e1 := dpiReadProbeImage(ActiveFile(f.e.d), 4096)
			c, e2 := dpiReadProbeImage(f.e.d.Root.XrayConfig(), 2<<20)
			if e1 != nil || e2 != nil || a != active || c != config {
				t.Fatal("selection/config changed", e1, e2)
			}
		})
	}
}

func TestDPIProposalProbeRefusesInvalidAdmission(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	for _, kind := range []string{"unfenced", "hash", "path", "id", "source", "ordinary", "nil-probe", "paused", "maintenance", "pending", "orphan"} {
		t.Run(kind, func(t *testing.T) {
			path, raw, next := dpiProposalInput(t, f, f.backup)
			ctx := f.ctx
			hash := compat.SHA256Hex(raw)
			called := false
			probe := func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
				called = true
				return reserveVerifiedOutcome(ctx, p)
			}
			switch kind {
			case "unfenced":
				ctx = context.Background()
			case "hash":
				hash = strings.Repeat("0", 64)
			case "path":
				path = filepath.Join(t.TempDir(), "foreign.json")
			case "id":
				next["id"] = f.plan.node.ID
			case "source":
				next["source"] = map[string]any{"type": "subscription", "subscriptionId": "foreign"}
			case "ordinary":
				var err error
				next, err = ReadRecord(f.e.d, f.backup)
				if err != nil {
					t.Fatal(err)
				}
			case "nil-probe":
				probe = nil
			case "paused":
				if err := f.e.d.Queue.SetPaused(true); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := f.e.d.Queue.SetPaused(false); err != nil {
						t.Error(err)
					}
				}()
			case "maintenance", "pending":
				marker := filepath.Join(f.e.d.Root.State(), "xray-maintenance.json")
				if kind == "pending" {
					marker = filepath.Join(f.e.d.Root.Config(), reserve.TransactionJournalFile)
				}
				if err := os.WriteFile(marker, []byte(`{"schemaVersion":1,"status":"removed"}`), 0600); err != nil {
					t.Fatal(err)
				}
				defer os.Remove(marker)
			case "orphan":
				work := filepath.Join(f.e.d.Root.Tmp(), "reserve-probe-foreign")
				if err := os.Mkdir(work, 0700); err != nil {
					t.Fatal(err)
				}
				defer os.Remove(work)
			}
			got, err := f.e.s.probeDPIProposalWith(ctx, path, hash, next, probe)
			if err == nil || got.Verified || got.Verdict != "unknown" || called {
				t.Fatal("invalid admission", got, err, called)
			}
		})
	}
}

func TestDPIProposalProbeRejectsDrift(t *testing.T) {
	for _, kind := range []string{"record", "record-inode", "parent-inode", "settings", "preferences", "selection", "main-config", "main-stop", "core-inode", "config", "cancel", "proof", "timing", "pending"} {
		t.Run(kind, func(t *testing.T) {
			f := newReserveRuntimeFixture(t)
			path, raw, next := dpiProposalInput(t, f, f.backup)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			called := false
			got, err := f.e.s.probeDPIProposalWith(ctx, path, compat.SHA256Hex(raw), next, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
				called = true
				out := reserveVerifiedOutcome(ctx, p)
				write := func(path string, b []byte) {
					t.Helper()
					if err := os.WriteFile(path, b, 0600); err != nil {
						t.Fatal(err)
					}
				}
				switch kind {
				case "record":
					write(path, append(raw, ' '))
				case "record-inode":
					if err := os.Rename(path, path+".retained"); err != nil {
						t.Fatal(err)
					}
					write(path, raw)
				case "parent-inode":
					dir := filepath.Dir(path)
					if err := os.Rename(dir, dir+".retained"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dir, 0700); err != nil {
						t.Fatal(err)
					}
					write(path, raw)
				case "settings":
					write(settingsFile(f.e.d), []byte(`{"socksPort":40123}`))
				case "preferences":
					writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
				case "selection":
					if err := setActiveID(f.e.d, f.backup); err != nil {
						t.Fatal(err)
					}
				case "main-config":
					write(f.e.d.Root.XrayConfig(), []byte(`{}`))
				case "main-stop":
					if err := f.process.Stop(context.Background()); err != nil {
						t.Fatal(err)
					}
				case "core-inode":
					core := f.e.d.Root.XrayBinary()
					b, err := os.ReadFile(core)
					if err != nil {
						t.Fatal(err)
					}
					if err = os.Rename(core, core+".retained"); err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(core, b, 0700); err != nil {
						t.Fatal(err)
					}
				case "config":
					write(p.ConfigPath, []byte(`{}`))
				case "cancel":
					cancel()
				case "proof":
					out.Identity = strings.Repeat("0", 64)
				case "timing":
					out.FinishedAt = time.Now().Add(time.Hour)
				case "pending":
					write(filepath.Join(f.e.d.Root.Config(), reserve.TransactionJournalFile), []byte(`{}`))
				}
				return out
			})
			if !called || err != nil || got.Verified || got.Verdict != "unknown" {
				t.Fatal("drift accepted", kind, got, err, called)
			}
		})
	}
}

func TestDPIProposalProbeRevalidatesAfterCleanup(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	path, raw, next := dpiProposalInput(t, f, f.backup)
	old := xray.StopTimeouts
	xray.StopTimeouts = [2]time.Duration{250 * time.Millisecond, 2 * time.Second}
	defer func() { xray.StopTimeouts = old }()
	changed := make(chan error, 1)
	called := false
	got, err := f.e.s.probeDPIProposalWith(f.ctx, path, compat.SHA256Hex(raw), next, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
		called = true
		out := reserveVerifiedOutcome(ctx, p)
		fd, e := unix.PidfdOpen(p.Process.PID, 0)
		if e != nil {
			t.Fatal(e)
		}
		defer unix.Close(fd)
		if _, e = reserve.VerifyProbeTarget(p); e != nil {
			t.Fatal(e)
		}
		if e = unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0); e != nil {
			t.Fatal(e)
		}
		go func() { time.Sleep(100 * time.Millisecond); changed <- os.WriteFile(path, append(raw, ' '), 0600) }()
		return out
	})
	if !called {
		t.Fatal("callback missing", err)
	}
	select {
	case e := <-changed:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mutation absent")
	}
	if err != nil || got.Verified || got.Verdict != "unknown" {
		t.Fatal("cleanup drift accepted", got, err)
	}
}

func TestDPIProposalProbePreservesForeignCleanup(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	path, raw, next := dpiProposalInput(t, f, f.backup)
	var work string
	var target reserve.ProbeTarget
	got, err := f.e.s.probeDPIProposalWith(f.ctx, path, compat.SHA256Hex(raw), next, func(ctx context.Context, p reserve.ProbeTarget) reserve.ProbeOutcome {
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
	if !errors.Is(err, errReserveProbeCleanup) || got.Verified || work == "" {
		t.Fatal(got, err)
	}
	b, e := os.ReadFile(filepath.Join(work, "foreign"))
	if e != nil || string(b) != "keep" {
		t.Fatal("foreign removed", e)
	}
	if identity, e := reserve.CaptureProcess(target.Process.PID, target.Process.Executable); e == nil && identity == target.Process {
		t.Fatal("owned child survived")
	}
	if err = reserve.CheckProbeWorkspaces(f.e.d.Root); err == nil {
		t.Fatal("orphan startup gate bypassed")
	}
}

func TestDPIProposalProbeUsesProductionUnhealthyVerdict(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	path, raw, next := dpiProposalInput(t, f, f.backup)
	snapshot := f.process.RuntimeSnapshot()
	got, err := f.e.s.probeDPIProposal(f.ctx, path, compat.SHA256Hex(raw), next)
	if err != nil || !got.Verified || got.Verdict != "unhealthy" {
		t.Fatal("dead upstream", got, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, raw) || f.process.RuntimeSnapshot() != snapshot {
		t.Fatal("live mutation", err)
	}
}

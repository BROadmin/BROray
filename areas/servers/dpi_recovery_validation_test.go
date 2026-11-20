package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type dpiRecoveryValidator interface {
	recoverDPIProfile(context.Context, *platform.Lock) error
}

func writeDPIRecoveryFixtureJournal(t *testing.T, f dpiRecoveryFixture) {
	t.Helper()
	raw, err := json.Marshal(f.r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.e.d.Root.Config(), reserve.ProfileTransactionJournalFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func assertDPIRecoveryPending(t *testing.T, f dpiRecoveryFixture, want bool) {
	t.Helper()
	got, err := reserve.TransactionPending(context.Background(), f.e.d.Root)
	if err != nil || got != want {
		t.Fatal("wrong recovery pending state", got, err)
	}
}

func TestDPIRecoveryNativeDecisionMatrix(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, committed := range []bool{false, true} {
				t.Run(fmt.Sprintf("active-%t/on-%t/committed-%t", active, enabled, committed), func(t *testing.T) {
					f := newDPIRecoveryFixture(t, active, enabled, committed)
					calls := 0
					f.e.s.xrayTest = func(ctx context.Context, path string) error { calls++; return f.e.s.execXrayTest(ctx, path) }
					before := f.e.d.App.Xray.RuntimeSnapshot()
					if err := f.e.s.recoverDPIProfile(f.ctx, f.guard); err != nil {
						t.Fatal(err)
					}
					if calls != 2 || f.e.d.App.Xray.RuntimeSnapshot() != before {
						t.Fatal("native proof count or main process changed", calls)
					}
					want := f.r.Previous
					if committed {
						want = f.r.Next
					}
					for p, w := range map[string][]byte{RecordPath(f.e.d, f.r.ServerID): want.Record.Data, f.e.d.Root.XrayConfig(): want.Config.Data} {
						got, err := os.ReadFile(p)
						if err != nil || !bytes.Equal(got, w) {
							t.Fatal("wrong native recovery decision", err)
						}
					}
					assertDPIRecoveryPending(t, f, false)
					_, err := os.Lstat(filepath.Join(f.e.d.Root.Config(), reserve.ProfileBootHoldFile))
					if active && err != nil {
						t.Fatal("stopped intent lost", err)
					}
					if !active && !os.IsNotExist(err) {
						t.Fatal("inactive recovery created stopped intent", err)
					}
					if err := reserve.CheckProbeWorkspaces(f.e.d.Root); err != nil {
						t.Fatal("native workspace leaked", err)
					}
				})
			}
		}
	}
}

func TestDPIRecoveryKeepsHistoricalRunningIntentDetached(t *testing.T) {
	f := newDPIRecoveryFixture(t, true, true, true)
	f.r.WasRunning = true
	writeDPIRecoveryFixtureJournal(t, f)
	if err := RecoverDPIProfile(f.ctx, f.e.d, f.guard); err != nil {
		t.Fatal(err)
	}
	assertDPIRecoveryPending(t, f, false)
	if s := f.e.d.App.Xray.RuntimeSnapshot(); s.Running || s.Wanted || s.Generation != 0 {
		t.Fatal("verifier started main", s)
	}
	if _, err := os.Lstat(filepath.Join(f.e.d.Root.Config(), reserve.ProfileBootHoldFile)); !os.IsNotExist(err) {
		t.Fatal("historical running intent blocked", err)
	}
}

func TestDPIRecoveryRefusesAdmission(t *testing.T) {
	for _, mode := range []string{"unfenced", "nil-daemon", "lost-daemon", "canceled", "wanted-main", "missing-app", "missing-core", "old-core"} {
		t.Run(mode, func(t *testing.T) {
			f := newDPIRecoveryFixture(t, true, true, false)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			guard := f.guard
			switch mode {
			case "unfenced":
				ctx = context.Background()
			case "nil-daemon":
				guard = nil
			case "lost-daemon":
				f.guard.Release()
			case "canceled":
				cancel()
			case "missing-app":
				f.e.d.App = nil
			case "missing-core":
				if err := os.Remove(f.e.d.Root.XrayBinary()); err != nil {
					t.Fatal(err)
				}
			case "old-core":
				if err := os.WriteFile(f.e.d.Root.XrayBinary(), []byte("#!/bin/sh\necho 'Xray 26.10.31'\n"), 0700); err != nil {
					t.Fatal(err)
				}
			case "wanted-main":
				p := f.e.d.App.Xray
				p.Binary = filepath.Join(t.TempDir(), "absent")
				if err := p.Start(ctx); err == nil {
					t.Fatal("fixture missing core launched")
				}
				if !p.RuntimeSnapshot().Wanted {
					t.Fatal("wanted fixture absent")
				}
				t.Cleanup(func() { _ = p.Stop(context.Background()) })
			}
			calls := 0
			f.e.s.xrayTest = func(context.Context, string) error { calls++; return nil }
			if err := f.e.s.recoverDPIProfile(ctx, guard); err == nil {
				t.Fatal("invalid recovery admitted")
			}
			assertDPIRecoveryPending(t, f, true)
			if calls != 0 {
				t.Fatal("invalid admission reached config test")
			}
		})
	}
}

func TestDPIRecoveryRejectsNonToggleOrWrongConfig(t *testing.T) {
	for _, mode := range []string{"credential-change", "no-toggle", "wrong-proxy", "direct-default", "invalid-native-main", "duplicate-proxy"} {
		t.Run(mode, func(t *testing.T) {
			f := newDPIRecoveryFixture(t, true, true, true)
			image := func(raw []byte) reserve.TransactionImage {
				return reserve.TransactionImage{Exists: true, Data: raw, SHA256: compat.SHA256Hex(raw)}
			}
			switch mode {
			case "credential-change":
				var rec Record
				if err := json.Unmarshal(f.r.Next.Record.Data, &rec); err != nil {
					t.Fatal(err)
				}
				rec["password"] = "changed"
				raw, err := Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				f.r.Next.Record = image(raw)
			case "no-toggle":
				f.r.Next = f.r.Previous
			default:
				var cfg map[string]any
				if err := json.Unmarshal(f.r.Next.Config.Data, &cfg); err != nil {
					t.Fatal(err)
				}
				obs := cfg["outbounds"].([]any)
				switch mode {
				case "wrong-proxy":
					obs[0].(map[string]any)["protocol"] = "freedom"
				case "direct-default":
					cfg["outbounds"] = append([]any{map[string]any{"tag": "other", "protocol": "freedom"}}, obs...)
				case "duplicate-proxy":
					cfg["outbounds"] = append(obs, obs[0])
				case "invalid-native-main":
					cfg["inbounds"] = []any{map[string]any{"protocol": "invalid-native-protocol"}}
				}
				raw, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				f.r.Next.Config = image(raw)
			}
			writeDPIRecoveryFixtureJournal(t, f)
			if err := os.WriteFile(RecordPath(f.e.d, f.r.ServerID), f.r.Next.Record.Data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.e.d.Root.XrayConfig(), f.r.Next.Config.Data, 0600); err != nil {
				t.Fatal(err)
			}
			err := f.e.s.recoverDPIProfile(f.ctx, f.guard)
			if err == nil || strings.Contains(err.Error(), "synthetic-recovery") {
				t.Fatal("unsafe recovery result", err)
			}
			assertDPIRecoveryPending(t, f, true)
			if err := reserve.CheckProbeWorkspaces(f.e.d.Root); err != nil {
				t.Fatal("ordinary rejection leaked native workspace", err)
			}
		})
	}
}

func TestDPIRecoveryNativeProofRejectsDrift(t *testing.T) {
	for _, mode := range []string{"core-inode", "settings", "record", "config", "main-owner", "daemon", "cancel", "native-error", "workspace-inode"} {
		t.Run(mode, func(t *testing.T) {
			f := newDPIRecoveryFixture(t, true, true, false)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			calls := 0
			f.e.s.xrayTest = func(ctx context.Context, path string) error {
				calls++
				if err := f.e.s.execXrayTest(ctx, path); err != nil {
					return err
				}
				switch mode {
				case "core-inode":
					core := f.e.d.Root.XrayBinary()
					raw, err := os.ReadFile(core)
					if err != nil {
						return err
					}
					if err = os.Rename(core, core+".old"); err != nil {
						return err
					}
					return os.WriteFile(core, raw, 0700)
				case "settings":
					return os.WriteFile(settingsFile(f.e.d), []byte(`{"socksPort":3000}`), 0600)
				case "record":
					return os.WriteFile(RecordPath(f.e.d, f.r.ServerID), []byte(`{"foreign":true}`), 0600)
				case "config":
					return os.WriteFile(f.e.d.Root.XrayConfig(), []byte(`{"foreign":true}`), 0600)
				case "main-owner":
					f.e.d.App.Xray = xray.NewProcess(f.e.d.Root)
				case "daemon":
					f.guard.Release()
				case "cancel":
					cancel()
				case "native-error":
					return errors.New("synthetic-recovery credential must stay private")
				case "workspace-inode":
					dir := filepath.Dir(path)
					if err := os.Rename(dir, dir+"-original"); err != nil {
						return err
					}
					if err := os.Mkdir(dir, 0700); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(dir, "foreign"), []byte("keep"), 0600)
				}
				return nil
			}
			err := f.e.s.recoverDPIProfile(ctx, f.guard)
			if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-recovery") {
				t.Fatal("drift accepted or private error leaked", calls, err)
			}
			assertDPIRecoveryPending(t, f, true)
			if mode == "workspace-inode" {
				if err := reserve.CheckProbeWorkspaces(f.e.d.Root); err == nil {
					t.Fatal("ambiguous workspace deleted")
				}
			} else if err := reserve.CheckProbeWorkspaces(f.e.d.Root); err != nil {
				t.Fatal("owned workspace not cleaned", err)
			}
		})
	}
}

func TestDPIRecoveryNoJournalAndNilDependencies(t *testing.T) {
	if err := RecoverDPIProfile(context.Background(), nil, nil); err == nil {
		t.Fatal("nil dependencies accepted")
	}
	f := newDPIRecoveryFixture(t, true, true, false)
	if err := os.Remove(filepath.Join(f.e.d.Root.Config(), reserve.ProfileTransactionJournalFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.e.d.Root.XrayBinary()); err != nil {
		t.Fatal(err)
	}
	if err := RecoverDPIProfile(f.ctx, f.e.d, f.guard); err != nil {
		t.Fatal("no-journal recovery should not test core", err)
	}
}

func TestDPIRecoveryNativeConfigOwnership(t *testing.T) {
	for _, mode := range []string{"bytes", "inode", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := newDPIRecoveryFixture(t, true, true, false)
			var changedPath string
			var changedBytes []byte
			f.e.s.xrayTest = func(ctx context.Context, path string) error {
				changedPath = path
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				switch mode {
				case "bytes":
					raw = []byte(`{"inbounds":[{"protocol":"socks","listen":"127.0.0.1","port":2080}],"outbounds":[{"protocol":"freedom"}]}`)
					if err := os.WriteFile(path, raw, 0600); err != nil {
						return err
					}
				case "inode":
					if err := os.Rename(path, path+".original"); err != nil {
						return err
					}
					if err := os.WriteFile(path, raw, 0600); err != nil {
						return err
					}
				case "symlink":
					target := filepath.Join(t.TempDir(), "foreign.json")
					if err := os.WriteFile(target, raw, 0600); err != nil {
						return err
					}
					if err := os.Remove(path); err != nil {
						return err
					}
					if err := os.Symlink(target, path); err != nil {
						return err
					}
				}
				changedBytes = raw
				return f.e.s.execXrayTest(ctx, path)
			}
			if err := f.e.s.recoverDPIProfile(f.ctx, f.guard); err == nil {
				t.Fatal("unconfirmed native test image released recovery journal")
			}
			assertDPIRecoveryPending(t, f, true)
			if err := reserve.CheckProbeWorkspaces(f.e.d.Root); err == nil {
				t.Fatal("ambiguous native workspace removed")
			}
			got, err := os.ReadFile(changedPath)
			if err != nil || !bytes.Equal(got, changedBytes) {
				t.Fatal("foreign native config removed or changed", err)
			}
		})
	}
}

func TestDPIRecoveryNativeWorkspaceFinalOwnership(t *testing.T) {
	for _, mode := range []string{"earlier-image", "foreign-entry"} {
		t.Run(mode, func(t *testing.T) {
			f := newDPIRecoveryFixture(t, true, true, false)
			calls := 0
			var changedPath string
			foreign := []byte("preserve foreign content")
			f.e.s.xrayTest = func(ctx context.Context, path string) error {
				calls++
				if calls == 2 {
					name := "profile.json"
					if mode == "foreign-entry" {
						name = "foreign.txt"
					}
					changedPath = filepath.Join(filepath.Dir(path), name)
					if err := os.WriteFile(changedPath, foreign, 0600); err != nil {
						return err
					}
				}
				return f.e.s.execXrayTest(ctx, path)
			}
			if err := f.e.s.recoverDPIProfile(f.ctx, f.guard); err == nil {
				t.Fatal("lost final native workspace ownership accepted")
			}
			if calls != 2 {
				t.Fatal("fixture did not reach second native validation", calls)
			}
			assertDPIRecoveryPending(t, f, true)
			got, err := os.ReadFile(changedPath)
			if err != nil || !bytes.Equal(got, foreign) {
				t.Fatal("foreign workspace content removed", err)
			}
		})
	}
}

type dpiRecoveryFixture struct {
	e     *fakeEnv
	ctx   context.Context
	guard *platform.Lock
	r     reserve.ProfileTransactionRecord
}

func newDPIRecoveryFixture(t *testing.T, active, enabled, committed bool) dpiRecoveryFixture {
	t.Helper()
	e := newEnv(t)
	id := e.addServer(t, "trojan://synthetic-recovery@127.0.0.1:1?security=tls&sni=localhost#Recovery")
	if err := compat.WriteJSON(settingsFile(e.d), map[string]any{"listenAddress": "127.0.0.1", "socksPort": 2080, "logLevel": "none"}); err != nil {
		t.Fatal(err)
	}
	core, err := os.ReadFile(os.Getenv("BRORAY_XRAY_BIN"))
	if err != nil {
		t.Fatal("native core fixture required", err)
	}
	if err := os.WriteFile(e.d.Root.XrayBinary(), core, 0700); err != nil {
		t.Fatal(err)
	}
	e.d.App = &app.App{Root: e.d.Root, Xray: xray.NewProcess(e.d.Root)}
	e.s.xrayTest = e.s.execXrayTest
	ctx := compat.WithFence(e.d.Ctx)
	old, err := ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		old, err = prepareDPIRecord(old, true)
		if err != nil {
			t.Fatal(err)
		}
		if err = WriteRecord(e.d, old); err != nil {
			t.Fatal(err)
		}
		old, err = ReadRecord(e.d, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	next, err := prepareDPIRecord(old, enabled)
	if err != nil {
		t.Fatal(err)
	}
	nextRaw, err := Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(RecordPath(e.d, id))
	if err != nil {
		t.Fatal(err)
	}
	main := old
	if !active {
		other := e.addServer(t, "trojan://other@127.0.0.1:2?security=tls&sni=localhost#Other")
		main, err = ReadRecord(e.d, other)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := setActiveID(e.d, asString(main["id"])); err != nil {
		t.Fatal(err)
	}
	config, err := XrayConfig(main, e.s.genSettings(ctx), "recovery fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.d.Root.XrayConfig(), config, 0600); err != nil {
		t.Fatal(err)
	}
	nextConfig := config
	if active {
		nextConfig, err = XrayConfig(next, e.s.genSettings(ctx), "recovery fixture")
		if err != nil {
			t.Fatal(err)
		}
	}
	j, err := reserve.OpenProfileJournal(e.d.Root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := j.Begin(ctx, id, compat.SHA256Hex(before), nextRaw, nextConfig, false, strings.Repeat("a", 64))
	if err != nil {
		j.Close()
		t.Fatal(err)
	}
	if err := j.Install(ctx, r); err != nil {
		j.Close()
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if committed {
		r.Phase = "committed"
		raw, e2 := json.Marshal(r)
		if e2 != nil {
			t.Fatal(e2)
		}
		if e2 = os.WriteFile(filepath.Join(e.d.Root.Config(), reserve.ProfileTransactionJournalFile), raw, 0600); e2 != nil {
			t.Fatal(e2)
		}
	}
	guard, err := platform.TryLock(e.d.Root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guard.Release() })
	return dpiRecoveryFixture{e, ctx, guard, r}
}

func TestDPIRecoveryNativeValidation(t *testing.T) {
	f := newDPIRecoveryFixture(t, true, true, false)
	v, ok := any(f.e.s).(dpiRecoveryValidator)
	if !ok {
		t.Fatal("native guarded DPI recovery validation absent")
	}
	if err := v.recoverDPIProfile(f.ctx, f.guard); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string][]byte{RecordPath(f.e.d, f.r.ServerID): f.r.Previous.Record.Data, f.e.d.Root.XrayConfig(): f.r.Previous.Config.Data} {
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("wrong recovered native image", err)
		}
	}
	if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
		t.Fatal("validated recovery stayed pending", pending, err)
	}
	if s := f.e.d.App.Xray.RuntimeSnapshot(); s.Running || s.Wanted || s.Generation != 0 {
		t.Fatal("native validation started main process", s)
	}
}

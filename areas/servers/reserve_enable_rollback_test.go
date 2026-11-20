package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"golang.org/x/sys/unix"
)

func waitReserveEnableRollback(t *testing.T, e *fakeEnv, id string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if r, ok := e.d.Queue.Request(id); ok && r.State != "queued" && r.State != "running" {
			if r.State != "failed" {
				t.Fatal("expected failed operation", r.State, r.ErrorCode)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("enable rollback exceeded bounded terminal wait", id)
}

func TestReserveEnableQueuedRollbackRestoresOrdinary(t *testing.T) {
	for _, kind := range []string{"ordinary-before", "ordinary-canonical", "outside-roster", "unchanged-config", "replacement-exited"} {
		t.Run(kind, func(t *testing.T) {
			f, desc := reserveModeQueueFixture(t)
			if kind == "outside-roster" {
				c := f.e.addServer(t, "trojan://pw@127.0.0.1:3#C")
				writeReserveAutomatic(t, f.e, f.backup, c, false)
			}
			if kind == "unchanged-config" {
				snap, err := f.process.ReplaceIfCurrent(f.e.d.Ctx, f.process.RuntimeSnapshot(), func() error { return platform.WriteFileAtomic(f.e.d.Root.XrayConfig(), f.plan.config, 0600) })
				if err != nil {
					t.Fatal(err)
				}
				target := commitFixtureTarget(t, f)
				deadline := time.Now().Add(5 * time.Second)
				for {
					identity, e := reserve.CaptureProcess(snap.PID, f.e.d.Root.XrayBinary())
					if e == nil {
						if _, e = reserve.VerifyListener(identity, target.Upstream); e == nil {
							break
						}
					}
					if time.Now().After(deadline) {
						t.Fatal("fixture listener not ready", e)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			before := f.process.RuntimeSnapshot()
			prefs, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			config, err := os.ReadFile(f.e.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			active, err := os.ReadFile(ActiveFile(f.e.d))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(ctx context.Context) error {
				calls++
				if calls == 1 {
					if kind == "replacement-exited" {
						terminateReserveEnableReplacement(t, f)
					}
					if kind != "ordinary-before" {
						*desc = "BROray - mix-auto"
						if err := os.WriteFile(f.e.d.Path("config", "interface.json"), []byte(`{"fixture":"canonical-enabled"}`), 0600); err != nil {
							return err
						}
					}
					return errors.New("enable metadata unconfirmed")
				}
				restored, e := readReservePreferences(f.e.d)
				if e != nil || restored.Enabled {
					return errors.New("old disabled preferences absent")
				}
				*desc = "BROray - A"
				return os.WriteFile(f.e.d.Path("config", "interface.json"), []byte(`{"fixture":"canonical-ordinary"}`), 0600)
			}
			nonce := strings.Repeat("91", 16)
			reply := reserveModeRequest(t, true, prefs.Revision, nonce)
			if reply.Code != 202 {
				t.Fatal(reply.Code, reply.Raw)
			}
			runReserveQueue(t, f.e)
			waitReserveEnableRollback(t, f.e, "q-"+nonce)
			result, _ := f.e.d.Queue.Request("q-" + nonce)
			if result.ErrorCode != "RESERVE_MODE_ROLLED_BACK" {
				t.Fatal("ordinary restoration unconfirmed", result.ErrorCode, result.Message)
			}
			after, e := readReservePreferences(f.e.d)
			if e != nil || after.Enabled || after.Revision != prefs.Revision {
				t.Fatal("old preferences not restored", after, e)
			}
			for p, want := range map[string][]byte{f.e.d.Root.XrayConfig(): config, ActiveFile(f.e.d): active} {
				got, e := os.ReadFile(p)
				if e != nil || !bytes.Equal(got, want) {
					t.Fatal("old image changed", p, e)
				}
			}
			snap := f.process.RuntimeSnapshot()
			if !snap.Running || !snap.Wanted || snap.ExitConfirmed || *desc != "BROray - A" || calls != 2 {
				t.Fatal("old runtime/description absent", calls, *desc)
			}
			if kind == "unchanged-config" {
				if snap != before {
					t.Fatal("unchanged config restarted")
				}
			} else if snap.Generation != before.Generation+2 || snap.Intent != before.Intent+2 {
				t.Fatal("guarded two replacements missing")
			}
			target := commitFixtureTarget(t, f)
			identity, e := reserve.CaptureProcess(snap.PID, f.e.d.Root.XrayBinary())
			if e != nil {
				t.Fatal(e)
			}
			if _, e = reserve.VerifyListener(identity, target.Upstream); e != nil {
				t.Fatal(e)
			}
			if pending, e := reserve.TransactionPending(context.Background(), f.e.d.Root); e != nil || pending || f.e.d.Lock.Busy() {
				t.Fatal("proven rollback retained fence", pending, e)
			}
		})
	}
}

func TestReserveEnableRollbackRetainsUncertainAuthority(t *testing.T) {
	for _, kind := range []string{"manual-stop", "foreign-receipt", "foreign-profile", "foreign-preferences", "foreign-config", "committed", "restore-proof", "foreign-core", "later-generation"} {
		t.Run(kind, func(t *testing.T) {
			f, desc := reserveModeQueueFixture(t)
			before := f.process.RuntimeSnapshot()
			prefs, err := readReservePreferences(f.e.d)
			if err != nil {
				t.Fatal(err)
			}
			verify := f.e.d.Svc.KeeneticVerifyInterfaceConfiguration
			calls := 0
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				if kind == "foreign-receipt" && calls > 0 {
					return compat.OwnedInterfaceTarget{}, errors.New("foreign receipt")
				}
				return verify(ctx, want)
			}
			live := f.e.d.Svc.KeeneticVerifyInterfaceTarget
			f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
				if kind == "restore-proof" && calls > 1 {
					return compat.OwnedInterfaceTarget{}, errors.New("old runtime proof refused")
				}
				return live(ctx, want)
			}
			var path string
			var protected []byte
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(ctx context.Context) error {
				calls++
				if calls > 1 {
					*desc = "BROray - A"
					return nil
				}
				*desc = "BROray - mix-auto"
				switch kind {
				case "manual-stop":
					if e := f.process.Stop(ctx); e != nil {
						return e
					}
				case "later-generation":
					if _, e := f.process.ReplaceIfCurrent(ctx, f.process.RuntimeSnapshot(), func() error { return nil }); e != nil {
						return e
					}
				case "foreign-core":
					path = f.e.d.Root.XrayBinary()
					raw, e := os.ReadFile(path)
					if e != nil {
						return e
					}
					protected = raw
					returnErr := platform.WriteFileAtomic(path, raw, 0755)
					if returnErr != nil {
						return returnErr
					}
					return errors.New("original core inode replaced")
				case "foreign-receipt":
					path = f.e.d.Path("config", "interface.json")
					protected = []byte(`{"foreign":true}`)
				case "foreign-profile":
					path = RecordPath(f.e.d, f.backup)
				case "foreign-preferences":
					path = reservePreferencesFile(f.e.d)
				case "foreign-config":
					path = f.e.d.Root.XrayConfig()
				case "committed":
					path = filepath.Join(f.e.d.Root.Config(), reserve.TransactionJournalFile)
					raw, e := os.ReadFile(path)
					if e != nil {
						return e
					}
					var r reserve.TransactionRecord
					if e = json.Unmarshal(raw, &r); e != nil {
						return e
					}
					r.Phase = "committed"
					protected, e = json.Marshal(r)
					if e != nil {
						return e
					}
				}
				if path != "" {
					if protected == nil {
						raw, e := os.ReadFile(path)
						if e != nil {
							return e
						}
						protected = append(raw, byte(' '))
					}
					if e := os.WriteFile(path, protected, 0600); e != nil {
						return e
					}
				}
				return errors.New("enable metadata failed")
			}
			nonce := strings.Repeat("92", 16)
			reply := reserveModeRequest(t, true, prefs.Revision, nonce)
			if reply.Code != 202 {
				t.Fatal(reply.Code, reply.Raw)
			}
			runReserveQueue(t, f.e)
			waitReserveEnableRollback(t, f.e, "q-"+nonce)
			result, _ := f.e.d.Queue.Request("q-" + nonce)
			if result.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" {
				t.Fatal("uncertain outcome lost fence", result.ErrorCode)
			}
			snap := f.process.RuntimeSnapshot()
			if kind == "manual-stop" {
				if snap.Running || snap.Wanted {
					t.Fatal("manual stop resurrected")
				}
			}
			wantGeneration := before.Generation + 1
			if kind == "later-generation" {
				wantGeneration++
			}
			if kind != "restore-proof" && snap.Generation != wantGeneration {
				t.Fatal("foreign rollback restarted process")
			}
			if path != "" {
				raw, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(raw, protected) {
					t.Fatal("protected evidence changed", path, e)
				}
			}
			if pending, e := reserve.TransactionPending(context.Background(), f.e.d.Root); e != nil || !pending || !f.e.d.Lock.Busy() {
				t.Fatal("recovery authority absent", pending, e)
			}
		})
	}
}

// The test signals only a pidfd whose exact fresh process and listener were
// verified after opening it. This never treats a numeric PID as authority.
func terminateReserveEnableReplacement(t *testing.T, f reserveRuntimeFixture) {
	t.Helper()
	snap := f.process.RuntimeSnapshot()
	target := commitFixtureTarget(t, f)
	deadline := time.Now().Add(3 * time.Second)
	for {
		identity, e := reserve.CaptureProcess(snap.PID, f.e.d.Root.XrayBinary())
		if e == nil {
			if _, e = reserve.VerifyListener(identity, target.Upstream); e == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("replacement listener not ready", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	fd, err := unix.PidfdOpen(snap.PID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	identity, err := reserve.CaptureProcess(snap.PID, f.e.d.Root.XrayBinary())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reserve.VerifyListener(identity, target.Upstream); err != nil {
		t.Fatal(err)
	}
	if f.process.RuntimeSnapshot() != snap {
		t.Fatal("replacement changed before signal")
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f.process.RuntimeSnapshot().ConfirmsUnexpectedExit(snap) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("replacement exit not confirmed")
}

package reserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func writeProfileRecoveryRecord(t *testing.T, f profileFixture, r ProfileTransactionRecord) {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	transactionWrite(t, filepath.Join(f.f.root.Config(), ProfileTransactionJournalFile), b)
}

type profileRecovery interface {
	Recover(context.Context, *platform.Lock, func(context.Context, ProfileTransactionRecord) error) error
}

func profileRecoverer(t *testing.T, j *ProfileJournal) profileRecovery {
	t.Helper()
	r, ok := any(j).(profileRecovery)
	if !ok {
		t.Fatal("guarded atomic profile cold recovery capability absent")
	}
	return r
}

func TestDPIProfileColdRecoveryRestoresUnderExclusions(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "inactive", true: "active"}[active], func(t *testing.T) {
			f := newProfileFixture(t, active)
			ctx := context.Background()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			called := false
			err = profileRecoverer(t, f.j).Recover(ctx, guard, func(_ context.Context, r ProfileTransactionRecord) error {
				called = true
				if !reflect.DeepEqual(r, f.r) {
					t.Fatal("recovery evidence differs from exact pending record")
				}
				f.assertState(t, false)
				f.assertPending(t, true)
				for _, path := range []string{filepath.Join(f.f.root.Run(), "xray-child.lock"), f.f.root.InstallerLock(), f.f.root.LauncherLock()} {
					lock, e := platform.TryLock(path)
					if e == nil {
						lock.Release()
						t.Fatal("recovery kernel exclusion not held", path)
					}
				}
				return nil
			})
			if err != nil || !called {
				t.Fatal("guarded profile recovery", err)
			}
			f.assertState(t, false)
			f.assertPending(t, false)
		})
	}
}

func TestDPIProfileColdRecoveryPartialAndCommitted(t *testing.T) {
	for _, state := range []string{"prepared", "record", "config", "both", "committed", "stopped"} {
		t.Run(state, func(t *testing.T) {
			f := newProfileFixture(t, true)
			if state == "stopped" {
				f.r.WasRunning = false
				writeProfileRecoveryRecord(t, f, f.r)
			}
			if state == "record" || state == "both" || state == "committed" {
				transactionWrite(t, f.recordPath, f.r.Next.Record.Data)
			}
			if state == "config" || state == "both" || state == "committed" {
				transactionWrite(t, f.f.root.XrayConfig(), f.r.Next.Config.Data)
			}
			if state == "committed" {
				f.r.Phase = "committed"
				writeProfileRecoveryRecord(t, f, f.r)
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			called := false
			err = profileRecoverer(t, f.j).Recover(context.Background(), guard, func(_ context.Context, r ProfileTransactionRecord) error {
				called = true
				if !reflect.DeepEqual(r, f.r) {
					t.Fatal("phase/intent authority changed")
				}
				f.assertState(t, state == "committed")
				f.assertPending(t, true)
				// A verifier cannot mutate the journal through aliased buffers.
				r.Previous.Record.Data[0] = ' '
				r.Next.Record.Data[0] = ' '
				return nil
			})
			if err != nil || !called {
				t.Fatal("recover", err)
			}
			f.assertState(t, state == "committed")
			f.assertPending(t, false)
		})
	}
}

func TestDPIProfileColdRecoveryRefusesUntrustedEntry(t *testing.T) {
	for _, kind := range []string{"nil-daemon", "lost-daemon", "child", "installer", "launcher", "child-link", "nil-verifier", "committed-nil-verifier", "foreign-record", "foreign-config", "ordinary-journal", "journal-symlink", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := newProfileFixture(t, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			useGuard := guard
			var foreignPath string
			foreign := []byte(`{"foreign":true}`)
			switch kind {
			case "nil-daemon":
				useGuard = nil
			case "lost-daemon":
				guard.Release()
			case "child", "installer", "launcher":
				path := filepath.Join(f.f.root.Run(), "xray-child.lock")
				if kind == "installer" {
					path = f.f.root.InstallerLock()
				}
				if kind == "launcher" {
					path = f.f.root.LauncherLock()
				}
				lock, e := platform.TryLock(path)
				if e != nil {
					t.Fatal(e)
				}
				defer lock.Release()
			case "child-link":
				foreignPath = filepath.Join(t.TempDir(), "foreign")
				transactionWrite(t, foreignPath, foreign)
				if e := os.Symlink(foreignPath, filepath.Join(f.f.root.Run(), "xray-child.lock")); e != nil {
					t.Fatal(e)
				}
			case "committed-nil-verifier":
				f.r.Phase = "committed"
				writeProfileRecoveryRecord(t, f, f.r)
			case "foreign-record":
				foreignPath = f.recordPath
				transactionWrite(t, foreignPath, foreign)
			case "foreign-config":
				foreignPath = f.f.root.XrayConfig()
				transactionWrite(t, foreignPath, foreign)
			case "ordinary-journal":
				transactionWrite(t, filepath.Join(f.f.root.Config(), TransactionJournalFile), foreign)
			case "journal-symlink":
				foreignPath = filepath.Join(t.TempDir(), "foreign")
				transactionWrite(t, foreignPath, foreign)
				path := filepath.Join(f.f.root.Config(), ProfileTransactionJournalFile)
				if e := os.Rename(path, path+".old"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(foreignPath, path); e != nil {
					t.Fatal(e)
				}
			case "cancelled":
				cancel()
			}
			called := false
			var verify func(context.Context, ProfileTransactionRecord) error = func(context.Context, ProfileTransactionRecord) error { called = true; return nil }
			if kind == "nil-verifier" || kind == "committed-nil-verifier" {
				verify = nil
			}
			if err := profileRecoverer(t, f.j).Recover(ctx, useGuard, verify); err == nil || called {
				t.Fatal("untrusted recovery accepted", err)
			}
			if _, e := os.Lstat(filepath.Join(f.f.root.Config(), ProfileTransactionJournalFile)); e != nil {
				t.Fatal("pending evidence lost", e)
			}
			if foreignPath != "" {
				got, e := os.ReadFile(foreignPath)
				if e != nil || !bytes.Equal(got, foreign) {
					t.Fatal("foreign image changed", e)
				}
			}
		})
	}
}

func TestDPIProfileColdRecoveryRetainsUnconfirmedProof(t *testing.T) {
	for _, phase := range []string{"prepared", "committed"} {
		for _, kind := range []string{"proof", "cancel", "daemon", "child-entry", "installer-entry", "launcher-entry", "run-parent", "record-parent", "owner-inode", "owner-replaced", "settings", "profile"} {
			t.Run(phase+"/"+kind, func(t *testing.T) {
				f := newProfileFixture(t, true)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if e := f.j.Install(ctx, f.r); e != nil {
					t.Fatal(e)
				}
				f.r.Phase = phase
				writeProfileRecoveryRecord(t, f, f.r)
				ownerPath := filepath.Join(f.f.root.Config(), ProfileTransactionJournalFile)
				owner, e := os.ReadFile(ownerPath)
				if e != nil {
					t.Fatal(e)
				}
				guard, e := platform.TryLock(f.f.root.DaemonLock())
				if e != nil {
					t.Fatal(e)
				}
				defer guard.Release()
				called := false
				err := profileRecoverer(t, f.j).Recover(ctx, guard, func(context.Context, ProfileTransactionRecord) error {
					called = true
					f.assertState(t, phase == "committed")
					switch kind {
					case "proof":
						return errors.New("durable lifecycle proof absent")
					case "cancel":
						cancel()
					case "daemon":
						guard.Release()
					case "child-entry", "installer-entry", "launcher-entry":
						path := filepath.Join(f.f.root.Run(), "xray-child.lock")
						if kind == "installer-entry" {
							path = f.f.root.InstallerLock()
						}
						if kind == "launcher-entry" {
							path = f.f.root.LauncherLock()
						}
						if e := os.Rename(path, path+".old"); e != nil {
							t.Fatal(e)
						}
						transactionWrite(t, path, []byte("foreign"))
					case "run-parent", "record-parent":
						path := f.f.root.Run()
						if kind == "record-parent" {
							path = filepath.Dir(f.recordPath)
						}
						if e := os.Rename(path, path+".old"); e != nil {
							t.Fatal(e)
						}
						if e := os.Mkdir(path, 0700); e != nil {
							t.Fatal(e)
						}
					case "owner-inode":
						if e := os.Rename(ownerPath, ownerPath+".old"); e != nil {
							t.Fatal(e)
						}
						transactionWrite(t, ownerPath, owner)
					case "owner-replaced":
						transactionWrite(t, ownerPath, []byte(`{"foreign":true}`))
					case "settings":
						transactionWrite(t, filepath.Join(f.f.root.Data(), "config", "system", "settings.json"), []byte(`{"foreign":true}`))
					case "profile":
						transactionWrite(t, f.recordPath, []byte(`{"foreign":true}`))
					}
					return nil
				})
				if err == nil || !called {
					t.Fatal("uncertain recovery accepted", err)
				}
				if _, e := os.Lstat(ownerPath); e != nil {
					t.Fatal("pending evidence lost", e)
				}
				if kind == "cancel" {
					if _, e := f.j.Read(context.Background()); e != nil {
						t.Fatal("temporary recovery guard leaked", e)
					}
				}
			})
		}
	}
}

func TestDPIProfileColdRecoveryNoJournal(t *testing.T) {
	f := newProfileFixture(t, false)
	if e := os.Remove(filepath.Join(f.f.root.Config(), ProfileTransactionJournalFile)); e != nil {
		t.Fatal(e)
	}
	guard, e := platform.TryLock(f.f.root.DaemonLock())
	if e != nil {
		t.Fatal(e)
	}
	defer guard.Release()
	if e := profileRecoverer(t, f.j).Recover(context.Background(), guard, nil); e != nil {
		t.Fatal(e)
	}
	f.assertState(t, false)
	f.assertPending(t, false)
}

func TestDPIProfileColdRecoveryRetainsProbeWorkspaces(t *testing.T) {
	for _, when := range []string{"before", "during-verification"} {
		t.Run(when, func(t *testing.T) {
			f := newProfileFixture(t, true)
			if err := f.j.Install(context.Background(), f.r); err != nil {
				t.Fatal(err)
			}
			workspace := filepath.Join(f.f.root.Tmp(), "reserve-probe-unconfirmed")
			create := func() {
				if err := os.MkdirAll(workspace, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if when == "before" {
				create()
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			called := false
			err = profileRecoverer(t, f.j).Recover(context.Background(), guard, func(context.Context, ProfileTransactionRecord) error {
				called = true
				if when == "during-verification" {
					create()
				}
				return nil
			})
			if err == nil || (when == "before" && called) {
				t.Fatal("ambiguous isolated probe admitted recovery", err)
			}
			f.assertPending(t, true)
			f.assertState(t, when == "before")
			if _, err := os.Lstat(workspace); err != nil {
				t.Fatal("workspace removed", err)
			}
		})
	}
}

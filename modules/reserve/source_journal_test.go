package reserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func sourceTransactionFixture(t *testing.T, action string, absentPrefs bool) *transactionFixture {
	t.Helper()
	f := newTransactionFixture(t)
	f.intent.Action = action
	f.intent.NextDescription = f.intent.PreviousDescription
	f.next.Preferences = append([]byte(nil), f.before.Preferences...)
	if action == "source-withdraw" {
		f.next.Config = nil
		f.next.Active = nil
	}
	if absentPrefs {
		if err := os.Remove(filepath.Join(f.root.Data(), "config/system/server-reserve.json")); err != nil {
			t.Fatal(err)
		}
		f.before.Preferences = nil
		f.next.Preferences = nil
	}
	return &f
}
func assertSourceFiles(t *testing.T, f *transactionFixture, want TransactionFiles) {
	t.Helper()
	for path, data := range map[string][]byte{f.root.XrayConfig(): want.Config, filepath.Join(f.root.Data(), "config/system/server-reserve.json"): want.Preferences, filepath.Join(f.root.Data(), "config/active-server"): want.Active} {
		actual, err := os.ReadFile(path)
		if data == nil {
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected explicit absence %s, got %v", path, err)
			}
		} else if err != nil || !bytes.Equal(actual, data) {
			t.Fatalf("source image changed %s: %v", path, err)
		}
	}
}
func TestSourceJournalDurableTransitionAndCommit(t *testing.T) {
	for _, action := range []string{"source-withdraw", "source-replace"} {
		t.Run(action, func(t *testing.T) {
			for _, absent := range []bool{false, true} {
				f := sourceTransactionFixture(t, action, absent)
				f.begin(t)
				f.assertPending(t, true)
				assertSourceFiles(t, f, f.before)
				r, err := f.j.Read(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if r.Next.Config.Exists != (f.next.Config != nil) || r.Next.Active.Exists != (f.next.Active != nil) || r.Next.Preferences.Exists != !absent {
					t.Fatal("absence contract lost")
				}
				if err := f.j.InstallSelection(context.Background(), f.id); err == nil {
					t.Fatal("selected before config transition")
				}
				if err := f.j.InstallConfig(context.Background(), f.id); err != nil {
					t.Fatal(err)
				}
				mid := f.before
				mid.Config = f.next.Config
				assertSourceFiles(t, f, mid)
				if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
					t.Fatal(err)
				}
				assertSourceFiles(t, f, f.next)
				if err := f.j.Confirm(context.Background(), f.id, func(context.Context, TransactionRecord) error { return errors.New("runtime proof failed") }); err == nil {
					t.Fatal("unproved transition committed")
				}
				f.assertPending(t, true)
				if err := f.j.Confirm(context.Background(), f.id, func(_ context.Context, r TransactionRecord) error { assertSourceFiles(t, f, f.next); return nil }); err != nil {
					t.Fatal(err)
				}
				f.assertPending(t, false)
				assertSourceFiles(t, f, f.next)
			}
		})
	}
}
func TestSourceJournalColdRecoveryAtEveryCut(t *testing.T) {
	for _, action := range []string{"source-withdraw", "source-replace"} {
		t.Run(action, func(t *testing.T) {
			for _, cut := range []string{"prepared", "config", "selection", "committed"} {
				t.Run(cut, func(t *testing.T) {
					f := sourceTransactionFixture(t, action, true)
					f.begin(t)
					if cut != "prepared" {
						if err := f.j.InstallConfig(context.Background(), f.id); err != nil {
							t.Fatal(err)
						}
					}
					if cut == "selection" || cut == "committed" {
						if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
							t.Fatal(err)
						}
					}
					if cut == "committed" {
						r, owner, err := f.j.loadID(f.id)
						if err != nil {
							t.Fatal(err)
						}
						r.Phase = "committed"
						if _, err := f.j.persist(r, owner); err != nil {
							t.Fatal(err)
						}
					}
					guard, err := platform.TryLock(f.root.DaemonLock())
					if err != nil {
						t.Fatal(err)
					}
					defer guard.Release()
					called := false
					err = RecoverTransaction(context.Background(), f.root, guard, func(_ context.Context, r TransactionRecord, current []byte) error {
						called = true
						assertSourceFiles(t, f, f.before)
						f.assertPending(t, true)
						for _, path := range []string{filepath.Join(f.root.Run(), "xray-child.lock"), f.root.InstallerLock(), f.root.LauncherLock()} {
							lock, err := platform.TryLock(path)
							if err == nil {
								lock.Release()
								t.Fatal("missing kernel exclusion", path)
							}
						}
						return nil
					})
					if err != nil || called == (cut == "committed") {
						t.Fatal("wrong recovery decision", called, err)
					}
					f.assertPending(t, false)
					want := f.before
					if cut == "committed" {
						want = f.next
					}
					assertSourceFiles(t, f, want)
				})
			}
		})
	}
}
func TestSourceJournalForeignFileAfterWithdrawalRetainsRecovery(t *testing.T) {
	for _, name := range []string{"config", "active", "preferences"} {
		t.Run(name, func(t *testing.T) {
			f := sourceTransactionFixture(t, "source-withdraw", true)
			f.begin(t)
			if err := f.j.InstallConfig(context.Background(), f.id); err != nil {
				t.Fatal(err)
			}
			if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
				t.Fatal(err)
			}
			path := f.root.XrayConfig()
			if name == "active" {
				path = filepath.Join(f.root.Data(), "config/active-server")
			}
			if name == "preferences" {
				path = filepath.Join(f.root.Data(), "config/system/server-reserve.json")
			}
			foreign := []byte("foreign replacement")
			transactionWrite(t, path, foreign)
			called := false
			callback := func(context.Context, TransactionRecord) error { called = true; return nil }
			if err := f.j.Confirm(context.Background(), f.id, callback); err == nil || called {
				t.Fatal("foreign entry committed")
			}
			if err := f.j.Rollback(context.Background(), f.id, callback); err == nil || called {
				t.Fatal("foreign entry overwritten")
			}
			f.assertPending(t, true)
			raw, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(raw, foreign) {
				t.Fatal("foreign bytes lost", err)
			}
		})
	}
}
func TestSourceJournalRejectsIncoherentTransition(t *testing.T) {
	for _, kind := range []string{"enabled-before", "enabled-after", "unknown-enabled", "mix-before", "mix-after", "description-change", "partial-config", "partial-active", "empty-config", "empty-active", "missing-previous-config", "missing-previous-active"} {
		t.Run(kind, func(t *testing.T) {
			f := sourceTransactionFixture(t, "source-withdraw", false)
			switch kind {
			case "enabled-before":
				f.before.Preferences = []byte(`{"enabled":true}`)
				transactionWrite(t, filepath.Join(f.root.Data(), "config/system/server-reserve.json"), f.before.Preferences)
			case "enabled-after":
				f.next.Preferences = []byte(`{"enabled":true}`)
			case "unknown-enabled":
				f.next.Preferences = []byte(`{"enabled":"false"}`)
			case "mix-before":
				f.intent.PreviousDescription = "BROray - mix-auto"
				f.intent.NextDescription = f.intent.PreviousDescription
			case "mix-after":
				f.intent.NextDescription = "BROray - mix-auto"
			case "description-change":
				f.intent.NextDescription = "Different description"
			case "partial-config":
				f.next.Config = []byte(`{}`)
			case "partial-active":
				f.next.Active = []byte("remaining-server\n")
			case "empty-config":
				f.next.Config = []byte{}
			case "empty-active":
				f.next.Active = []byte{}
			case "missing-previous-config":
				if err := os.Remove(f.root.XrayConfig()); err != nil {
					t.Fatal(err)
				}
				f.before.Config = nil
			case "missing-previous-active":
				if err := os.Remove(filepath.Join(f.root.Data(), "config/active-server")); err != nil {
					t.Fatal(err)
				}
				f.before.Active = nil
			}
			if _, err := f.j.Begin(context.Background(), f.next, f.intent); err == nil {
				t.Fatal("incoherent source transition accepted")
			}
			f.assertPending(t, false)
			assertSourceFiles(t, f, f.before)
		})
	}
	for _, action := range []string{"enable", "disable", "switch", "source-replace"} {
		t.Run(action+"-absence", func(t *testing.T) {
			f := sourceTransactionFixture(t, "source-withdraw", false)
			f.intent.Action = action
			if action == "enable" {
				f.intent.NextDescription = "BROray - mix-auto"
			}
			if action == "disable" || action == "switch" {
				f.intent.PreviousDescription = "BROray - mix-auto"
			}
			if action == "switch" {
				f.intent.NextDescription = "BROray - mix-auto"
			}
			if _, err := f.j.Begin(context.Background(), f.next, f.intent); err == nil {
				t.Fatal("absence accepted outside withdrawal")
			}
			f.assertPending(t, false)
			assertSourceFiles(t, f, f.before)
		})
	}
}
func TestSourceJournalRejectsMalformedAbsenceAfterRestart(t *testing.T) {
	f := sourceTransactionFixture(t, "source-withdraw", true)
	f.begin(t)
	path := filepath.Join(f.root.Config(), TransactionJournalFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	image := r["next"].(map[string]any)["config"].(map[string]any)
	delete(image, "exists")
	changed, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	transactionWrite(t, path, changed)
	if _, err := f.j.Read(context.Background()); err == nil {
		t.Fatal("omitted existence became deletion authority")
	}
	if err := f.j.Rollback(context.Background(), f.id, func(context.Context, TransactionRecord) error { t.Error("malformed journal recovered"); return nil }); err == nil {
		t.Fatal("malformed journal cleared")
	}
	f.assertPending(t, true)
	assertSourceFiles(t, f, f.before)
}

package reserve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

type transactionFixture struct {
	root         platform.Root
	j            *TransactionJournal
	before, next TransactionFiles
	intent       TransactionIntent
	id           string
}

func transactionWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func transactionHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func newTransactionFixture(t *testing.T) transactionFixture {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root.Data(), "config", "system"), 0700); err != nil {
		t.Fatal(err)
	}
	before := TransactionFiles{[]byte(`{"old":true}`), []byte(`{"enabled":false}`), []byte("old-server\n")}
	next := TransactionFiles{[]byte(`{"next":true}`), []byte(`{"enabled":true}`), []byte("next-server\n")}
	transactionWrite(t, root.XrayConfig(), before.Config)
	transactionWrite(t, filepath.Join(root.Data(), "config", "system/server-reserve.json"), before.Preferences)
	transactionWrite(t, filepath.Join(root.Data(), "config", "active-server"), before.Active)
	receipt := []byte(`{"owner":"BROray","schemaVersion":2,"interfaceName":"Proxy3"}`)
	transactionWrite(t, filepath.Join(root.Data(), "config", "interface.json"), receipt)
	intent := TransactionIntent{Action: "enable", Authority: strings.Repeat("a", 64), InterfaceName: "Proxy3", Upstream: "192.168.1.1:2080", PreviousDescription: "BROray - old", PreviousDescriptionLine: `    description "BROray - old"`, NextDescription: "BROray - mix-auto", ReceiptSHA256: transactionHash(receipt)}
	j, err := OpenTransactionJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return transactionFixture{root: root, j: j, before: before, next: next, intent: intent}
}
func (f *transactionFixture) begin(t *testing.T) {
	t.Helper()
	id, err := f.j.Begin(context.Background(), f.next, f.intent)
	if err != nil || len(id) != 64 {
		t.Fatalf("durable begin: %q %v", id, err)
	}
	f.id = id
}
func (f transactionFixture) assertFiles(t *testing.T, want TransactionFiles) {
	t.Helper()
	for path, data := range map[string][]byte{f.root.XrayConfig(): want.Config, filepath.Join(f.root.Data(), "config", "system/server-reserve.json"): want.Preferences, filepath.Join(f.root.Data(), "config", "active-server"): want.Active} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("file %s changed: %q %v", path, got, err)
		}
	}
}
func (f transactionFixture) assertPending(t *testing.T, want bool) {
	t.Helper()
	got, err := TransactionPending(context.Background(), f.root)
	if err != nil || got != want {
		t.Fatalf("pending=%v err=%v", got, err)
	}
}
func (f transactionFixture) restore(t *testing.T) func(context.Context, TransactionRecord) error {
	return func(_ context.Context, r TransactionRecord) error {
		if !bytes.Equal(r.Previous.Config.Data, f.before.Config) || r.Intent != f.intent {
			t.Fatal("wrong recovery snapshot")
		}
		for name, data := range map[string][]byte{"active-server": f.before.Active, "system/server-reserve.json": f.before.Preferences} {
			got, err := os.ReadFile(filepath.Join(f.root.Data(), "config", name))
			if err != nil || !bytes.Equal(got, data) {
				t.Fatal("selection must precede config/interface restore")
			}
		}
		return platform.WriteFileAtomicNoFollow(f.root.XrayConfig(), r.Previous.Config.Data, 0600)
	}
}

func TestReserveTransactionDurableIntentAndConfirmedCommit(t *testing.T) {
	f := newTransactionFixture(t)
	f.assertPending(t, false)
	f.begin(t)
	f.assertPending(t, true)
	f.assertFiles(t, f.before)
	if _, err := f.j.Begin(context.Background(), f.next, f.intent); err == nil {
		t.Fatal("second transaction overwrote intent")
	}
	if err := f.j.InstallSelection(context.Background(), f.id); err == nil {
		t.Fatal("selected unapplied config")
	}
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	f.assertFiles(t, f.next)
	if err := f.j.Confirm(context.Background(), f.id, nil); err == nil {
		t.Fatal("confirmed without runtime/interface proof")
	}
	if err := f.j.Confirm(context.Background(), f.id, func(_ context.Context, r TransactionRecord) error { f.assertFiles(t, f.next); return nil }); err != nil {
		t.Fatal(err)
	}
	f.assertPending(t, false)
	f.assertFiles(t, f.next)
}

func TestReserveTransactionRollbackAfterRestartAndFailedProof(t *testing.T) {
	f := newTransactionFixture(t)
	f.begin(t)
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	if err := f.j.Rollback(context.Background(), f.id, func(context.Context, TransactionRecord) error { return errors.New("interface unconfirmed") }); err == nil {
		t.Fatal("failed restoration accepted")
	}
	f.assertPending(t, true)
	if err := f.j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err := OpenTransactionJournal(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	f.j = j
	r, err := j.Read(context.Background())
	if err != nil || r.ID != f.id {
		t.Fatal("restart lost intent", err)
	}
	if err := j.Rollback(context.Background(), f.id, f.restore(t)); err != nil {
		t.Fatal(err)
	}
	f.assertFiles(t, f.before)
	f.assertPending(t, false)
}

func TestReserveTransactionRefusesDriftAndStaleAuthority(t *testing.T) {
	for _, name := range []string{"config.json", "active-server", "system/server-reserve.json"} {
		t.Run(name, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			path := filepath.Join(f.root.Data(), "config", name)
			if name == "config.json" {
				path = f.root.XrayConfig()
			}
			foreign := []byte("foreign-value")
			transactionWrite(t, path, foreign)
			called := false
			if err := f.j.Rollback(context.Background(), f.id, func(context.Context, TransactionRecord) error { called = true; return nil }); err == nil || called {
				t.Fatal("foreign mutation overwritten")
			}
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, foreign) {
				t.Fatal("foreign bytes lost")
			}
			f.assertPending(t, true)
		})
	}
	f := newTransactionFixture(t)
	f.begin(t)
	if err := f.j.Rollback(context.Background(), strings.Repeat("b", 64), f.restore(t)); err == nil {
		t.Fatal("wrong transaction ID accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.j.Rollback(ctx, f.id, f.restore(t)); err == nil {
		t.Fatal("cancelled recovery accepted")
	}
	f.assertPending(t, true)
}

func TestReserveTransactionRejectsUnsafeFilesystem(t *testing.T) {
	for _, kind := range []string{"journal-link", "config-link", "prefs-fifo", "parent-swap"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			outside := filepath.Join(t.TempDir(), "outside")
			transactionWrite(t, outside, []byte("outside"))
			switch kind {
			case "journal-link":
				if err := os.Symlink(outside, filepath.Join(f.root.Config(), TransactionJournalFile)); err != nil {
					t.Fatal(err)
				}
			case "config-link":
				if err := os.Remove(f.root.XrayConfig()); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, f.root.XrayConfig()); err != nil {
					t.Fatal(err)
				}
			case "prefs-fifo":
				path := filepath.Join(f.root.Data(), "config", "system/server-reserve.json")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "parent-swap":
				path := filepath.Join(f.root.Data(), "config", "system")
				if err := os.Rename(path, path+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.j.Begin(context.Background(), f.next, f.intent); err == nil {
				t.Fatal("unsafe path accepted")
			}
			got, _ := os.ReadFile(outside)
			if string(got) != "outside" {
				t.Fatal("outside file changed")
			}
		})
	}
}

func TestReserveTransactionRejectsMalformedJournal(t *testing.T) {
	for _, kind := range []string{"duplicate", "missing-before", "null-exists", "wrong-hash", "unknown", "wrong-id"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			path := filepath.Join(f.root.Config(), TransactionJournalFile)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "duplicate":
				raw = append([]byte(`{"schemaVersion":1,`), raw[1:]...)
			case "missing-before":
				delete(m, "previous")
			case "null-exists":
				m["previous"].(map[string]any)["config"].(map[string]any)["exists"] = nil
			case "wrong-hash":
				m["previous"].(map[string]any)["config"].(map[string]any)["sha256"] = strings.Repeat("0", 64)
			case "unknown":
				m["previousConfig"] = nil
			case "wrong-id":
				m["id"] = ""
			}
			if kind != "duplicate" {
				raw, err = json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
			}
			transactionWrite(t, path, raw)
			if _, err := f.j.Read(context.Background()); err == nil {
				t.Fatal("malformed record accepted")
			}
			if err := f.j.Rollback(context.Background(), f.id, f.restore(t)); err == nil {
				t.Fatal("malformed recovery accepted")
			}
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, raw) {
				t.Fatal("failed evidence replaced")
			}
			f.assertFiles(t, f.before)
		})
	}
}

func TestReserveTransactionCallbackCannotClearChangedJournal(t *testing.T) {
	f := newTransactionFixture(t)
	f.begin(t)
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root.Config(), TransactionJournalFile)
	foreign := []byte(`{"foreign":true}`)
	if err := f.j.Confirm(context.Background(), f.id, func(context.Context, TransactionRecord) error { transactionWrite(t, path, foreign); return nil }); err == nil {
		t.Fatal("changed journal confirmed")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, foreign) {
		t.Fatal("foreign journal deleted")
	}
}

func TestReserveTransactionCommittedDecisionSurvivesCleanupCrash(t *testing.T) {
	f := newTransactionFixture(t)
	f.begin(t)
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	// Reproduce a durable committed record left behind by interrupted cleanup.
	r, owner, err := f.j.loadID(f.id)
	if err != nil {
		t.Fatal(err)
	}
	r.Phase = "committed"
	if _, err := f.j.persist(r, owner); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := f.j.Rollback(context.Background(), f.id, func(context.Context, TransactionRecord) error {
		called = true
		return errors.New("must not undo committed operation")
	}); err != nil || called {
		t.Fatal("committed decision reversed", err)
	}
	f.assertFiles(t, f.next)
	f.assertPending(t, false)
}

func TestReserveTransactionLastMomentSelectionDriftIsPreserved(t *testing.T) {
	f := newTransactionFixture(t)
	f.begin(t)
	r, owner, err := f.j.loadID(f.id)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.j.allowed(r); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root.Data(), "config", "system/server-reserve.json")
	foreign := []byte(`{"foreign":true}`)
	transactionWrite(t, path, foreign)
	if err := f.j.selection(context.Background(), r.Next, r.Previous, owner); err == nil {
		t.Fatal("changed predecessor accepted after earlier preflight")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, foreign) {
		t.Fatal("foreign preference bytes overwritten")
	}
	f.assertPending(t, true)
}

func TestReserveTransactionRequiresActualRestoreAndRejectsOtherWriter(t *testing.T) {
	f := newTransactionFixture(t)
	f.begin(t)
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.Rollback(context.Background(), f.id, func(context.Context, TransactionRecord) error { return nil }); err == nil {
		t.Fatal("callback without restored bytes accepted")
	}
	f.assertPending(t, true)
	other, err := OpenTransactionJournal(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := f.j.locked(context.Background(), func() error {
		if _, err := other.Read(context.Background()); err == nil {
			t.Fatal("another journal bypassed held directory lock")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.j.Rollback(context.Background(), f.id, f.restore(t)); err != nil {
		t.Fatal(err)
	}
}

func TestReserveTransactionLegacyPendingCheckIsReadOnly(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := os.Mkdir(root.Config(), 0700); err != nil {
		t.Fatal(err)
	}
	if pending, err := TransactionPending(context.Background(), root); err != nil || pending {
		t.Fatal("legacy absence", pending, err)
	}
	if _, err := os.Lstat(filepath.Join(root.Data(), "config", "system")); !os.IsNotExist(err) {
		t.Fatal("read-only check created preferences directory")
	}
	path := filepath.Join(root.Config(), TransactionJournalFile)
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := TransactionPending(context.Background(), root); err == nil {
		t.Fatal("FIFO journal accepted")
	}
}

func TestReserveTransactionAbsentPredecessorsRestoredExplicitly(t *testing.T) {
	f := newTransactionFixture(t)
	for _, path := range []string{f.root.XrayConfig(), filepath.Join(f.root.Data(), "config", "active-server"), filepath.Join(f.root.Data(), "config", "system/server-reserve.json")} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	f.begin(t)
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	if err := f.j.Rollback(context.Background(), f.id, func(_ context.Context, r TransactionRecord) error {
		if r.Previous.Config.Exists || r.Previous.Active.Exists || r.Previous.Preferences.Exists {
			t.Fatal("absence not recorded")
		}
		return os.Remove(f.root.XrayConfig())
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{f.root.XrayConfig(), filepath.Join(f.root.Data(), "config", "active-server"), filepath.Join(f.root.Data(), "config", "system/server-reserve.json")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("predecessor absence not restored", path, err)
		}
	}
	f.assertPending(t, false)
}

func TestReserveTransactionLargeBoundedRoundtripAndSnapshotIsolation(t *testing.T) {
	f := newTransactionFixture(t)
	f.before.Config = []byte(`{"padding":"` + strings.Repeat("a", 1100000) + `"}`)
	f.next.Config = []byte(`{"padding":"` + strings.Repeat("b", 1100000) + `"}`)
	transactionWrite(t, f.root.XrayConfig(), f.before.Config)
	f.begin(t)
	raw, err := os.ReadFile(filepath.Join(f.root.Config(), TransactionJournalFile))
	if err != nil || len(raw) <= 2<<20 {
		t.Fatal("large journal fixture", err)
	}
	r, err := f.j.Read(context.Background())
	if err != nil || !bytes.Equal(r.Previous.Config.Data, f.before.Config) || !bytes.Equal(r.Next.Config.Data, f.next.Config) {
		t.Fatal("bounded record roundtrip", err)
	}
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	if err := f.j.Confirm(context.Background(), f.id, func(_ context.Context, r TransactionRecord) error {
		r.Next.Config.Data[0] = '!'
		r.Previous.Config.Data[0] = '!'
		r.Receipt.Data[0] = '!'
		return nil
	}); err != nil {
		t.Fatal("callback could mutate journal snapshot", err)
	}
	f.assertFiles(t, f.next)
	f.assertPending(t, false)
}

func TestReserveTransactionRejectsUnboundOrOversizeInputs(t *testing.T) {
	for _, kind := range []string{"receipt", "authority", "interface", "description", "config", "preferences"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			switch kind {
			case "receipt":
				f.intent.ReceiptSHA256 = strings.Repeat("0", 64)
			case "authority":
				f.intent.Authority = ""
			case "interface":
				f.intent.InterfaceName = "Proxy3;save"
			case "description":
				f.intent.NextDescription = "BROray - mix-auto\n"
			case "config":
				f.next.Config = bytes.Repeat([]byte(" "), (2<<20)+1)
			case "preferences":
				f.next.Preferences = bytes.Repeat([]byte(" "), 4097)
			}
			if _, err := f.j.Begin(context.Background(), f.next, f.intent); err == nil {
				t.Fatal("unbounded/unbound intent accepted")
			}
			f.assertPending(t, false)
			f.assertFiles(t, f.before)
		})
	}
}

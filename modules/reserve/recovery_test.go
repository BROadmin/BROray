package reserve

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func TestReserveColdRecoveryRestoresUnderKernelExclusions(t *testing.T) {
	f := newTransactionFixture(t)
	f.begin(t)
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	previousReceipt, err := os.ReadFile(filepath.Join(f.root.Data(), "config", "interface.json"))
	if err != nil {
		t.Fatal(err)
	}
	currentReceipt := []byte(`{"owner":"BROray","next":true}`)
	transactionWrite(t, filepath.Join(f.root.Data(), "config", "interface.json"), currentReceipt)
	guard, err := platform.TryLock(f.root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	called := false
	if err := RecoverTransaction(context.Background(), f.root, guard, func(_ context.Context, r TransactionRecord, current []byte) error {
		called = true
		f.assertFiles(t, f.before)
		f.assertPending(t, true)
		if r.ID != f.id || !bytes.Equal(current, currentReceipt) || !bytes.Equal(r.Receipt.Data, previousReceipt) {
			t.Fatal("wrong recovery evidence")
		}
		for _, path := range []string{filepath.Join(f.root.Run(), "xray-child.lock"), f.root.InstallerLock(), f.root.LauncherLock()} {
			lock, err := platform.TryLock(path)
			if err == nil {
				lock.Release()
				t.Fatal("kernel exclusion not held", path)
			}
		}
		return nil
	}); err != nil || !called {
		t.Fatal("cold restore", err)
	}
	f.assertPending(t, false)
	f.assertFiles(t, f.before)
	got, _ := os.ReadFile(filepath.Join(f.root.Data(), "config", "interface.json"))
	if !bytes.Equal(got, previousReceipt) {
		t.Fatal("receipt not restored after proof")
	}
}

func TestReserveColdRecoveryRefusesLiveAndAmbiguousOwners(t *testing.T) {
	for _, kind := range []string{"daemon", "child", "installer", "launcher", "child-link", "missing-callback"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			guard, err := platform.TryLock(f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			path := filepath.Join(f.root.Run(), "xray-child.lock")
			switch kind {
			case "daemon":
				guard.Release()
			case "child", "installer", "launcher":
				if kind == "installer" {
					path = f.root.InstallerLock()
				}
				if kind == "launcher" {
					path = f.root.LauncherLock()
				}
				lock, err := platform.TryLock(path)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Release()
			case "child-link":
				outside := filepath.Join(t.TempDir(), "foreign")
				transactionWrite(t, outside, []byte("untouched"))
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			var restore RestoreTransactionInterface = func(context.Context, TransactionRecord, []byte) error { called = true; return nil }
			if kind == "missing-callback" {
				restore = nil
			}
			if err := RecoverTransaction(context.Background(), f.root, guard, restore); err == nil || called {
				t.Fatal("untrusted recovery entered")
			}
			f.assertPending(t, true)
			f.assertFiles(t, f.before)
		})
	}
}

func TestReserveColdRecoveryPreservesFailedProofAndForeignReceipt(t *testing.T) {
	for _, kind := range []string{"proof", "receipt-changed", "guard-lost", "child-lock-replaced"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			transactionWrite(t, f.root.XrayConfig(), f.next.Config)
			guard, err := platform.TryLock(f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			receiptPath := filepath.Join(f.root.Data(), "config", "interface.json")
			foreign := []byte(`{"foreign":true}`)
			if err := RecoverTransaction(context.Background(), f.root, guard, func(context.Context, TransactionRecord, []byte) error {
				switch kind {
				case "proof":
					return errors.New("router did not restore")
				case "receipt-changed":
					transactionWrite(t, receiptPath, foreign)
				case "guard-lost":
					guard.Release()
				case "child-lock-replaced":
					p := filepath.Join(f.root.Run(), "xray-child.lock")
					if err := os.Rename(p, p+"-old"); err != nil {
						t.Fatal(err)
					}
					transactionWrite(t, p, []byte("foreign"))
				}
				return nil
			}); err == nil {
				t.Fatal("uncertain restoration accepted")
			}
			f.assertPending(t, true)
			if kind == "receipt-changed" {
				got, _ := os.ReadFile(receiptPath)
				if !bytes.Equal(got, foreign) {
					t.Fatal("foreign receipt overwritten")
				}
			}
		})
	}
}

func TestReserveColdRecoveryNoJournalAndCommittedCleanup(t *testing.T) {
	f := newTransactionFixture(t)
	guard, err := platform.TryLock(f.root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	if err := RecoverTransaction(context.Background(), f.root, guard, nil); err != nil {
		t.Fatal("absent intent", err)
	}
	f.begin(t)
	transactionWrite(t, f.root.XrayConfig(), f.next.Config)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	r, owner, err := f.j.loadID(f.id)
	if err != nil {
		t.Fatal(err)
	}
	r.Phase = "committed"
	if _, err := f.j.persist(r, owner); err != nil {
		t.Fatal(err)
	}
	if err := RecoverTransaction(context.Background(), f.root, guard, nil); err != nil {
		t.Fatal("committed cleanup", err)
	}
	f.assertPending(t, false)
	f.assertFiles(t, f.next)
}

func TestReserveTransactionRecordsFullUnicodeDescription(t *testing.T) {
	f := newTransactionFixture(t)
	f.intent.PreviousDescription = "BROray - " + strings.Repeat("🛰", 90)
	f.intent.PreviousDescriptionLine = `    description "` + f.intent.PreviousDescription + `"`
	f.begin(t)
	r, err := f.j.Read(context.Background())
	if err != nil || r.Intent.PreviousDescriptionLine != f.intent.PreviousDescriptionLine {
		t.Fatal("valid existing Unicode description not retained", err)
	}
}

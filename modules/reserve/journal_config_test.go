package reserve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTransactionConfigInstallPreservesSelectionAndJournal(t *testing.T) {
	f := newTransactionFixture(t)
	f.begin(t)
	if err := f.j.InstallConfig(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	want := f.before
	want.Config = f.next.Config
	f.assertFiles(t, want)
	f.assertPending(t, true)
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	f.assertFiles(t, f.next)
	if err := f.j.Confirm(context.Background(), f.id, func(context.Context, TransactionRecord) error { return nil }); err != nil {
		t.Fatal(err)
	}
	f.assertPending(t, false)
}

func TestTransactionConfigInstallRejectsChangedAuthority(t *testing.T) {
	for _, kind := range []string{"config", "preferences", "receipt", "owner", "parent", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := f.id
			switch kind {
			case "config":
				transactionWrite(t, f.root.XrayConfig(), []byte(`{"foreign":true}`))
			case "preferences":
				transactionWrite(t, filepath.Join(f.root.Data(), "config", "system/server-reserve.json"), []byte(`{"foreign":true}`))
			case "receipt":
				transactionWrite(t, filepath.Join(f.root.Data(), "config", "interface.json"), []byte(`{"foreign":true}`))
			case "owner":
				id = "foreign"
			case "parent":
				if err := os.Rename(f.root.Config(), f.root.Config()+".held"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(f.root.Config(), 0700); err != nil {
					t.Fatal(err)
				}
				transactionWrite(t, f.root.XrayConfig(), []byte(`{"foreign":true}`))
			case "cancel":
				cancel()
			}
			before, err := os.ReadFile(f.root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			if err := f.j.InstallConfig(ctx, id); err == nil {
				t.Fatal("drift accepted")
			}
			after, err := os.ReadFile(f.root.XrayConfig())
			if err != nil || string(after) != string(before) {
				t.Fatal("foreign configuration changed", err)
			}
		})
	}
}

func TestTransactionConfigUsesDataMetadataAndPreservesDecoys(t *testing.T) {
	f := newTransactionFixture(t)
	if err := os.MkdirAll(filepath.Join(f.root.Config(), "system"), 0700); err != nil {
		t.Fatal(err)
	}
	decoy := []byte(`{"decoy":"must-not-be-read-or-written"}`)
	for _, name := range []string{"active-server", "interface.json", "system/server-reserve.json"} {
		transactionWrite(t, filepath.Join(f.root.Config(), name), decoy)
	}
	f.begin(t)
	r, err := f.j.Read(context.Background())
	if err != nil || string(r.Previous.Active.Data) != string(f.before.Active) || string(r.Previous.Preferences.Data) != string(f.before.Preferences) {
		t.Fatal("wrong metadata layout", err)
	}
	if err := f.j.InstallConfig(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	if err := f.j.InstallSelection(context.Background(), f.id); err != nil {
		t.Fatal(err)
	}
	f.assertFiles(t, f.next)
	if err := f.j.Rollback(context.Background(), f.id, f.restore(t)); err != nil {
		t.Fatal(err)
	}
	f.assertFiles(t, f.before)
	for _, name := range []string{"active-server", "interface.json", "system/server-reserve.json"} {
		got, err := os.ReadFile(filepath.Join(f.root.Config(), name))
		if err != nil || string(got) != string(decoy) {
			t.Fatal("decoy metadata changed", name, err)
		}
	}
}

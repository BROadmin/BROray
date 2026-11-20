package reserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type previousConfigInstaller interface {
	RestoreConfig(context.Context, string) error
}

func liveConfigInstaller(t *testing.T, j *TransactionJournal) previousConfigInstaller {
	t.Helper()
	installer, ok := any(j).(previousConfigInstaller)
	if !ok {
		t.Fatal("guarded previous-config installer absent")
	}
	return installer
}

func TestTransactionLiveConfigRestoresOnlyConfig(t *testing.T) {
	for _, phase := range []string{"prepared", "config-installed", "selected"} {
		t.Run(phase, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			install := liveConfigInstaller(t, f.j)
			ctx := context.Background()
			if phase != "prepared" {
				if err := f.j.InstallConfig(ctx, f.id); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "selected" {
				if err := f.j.InstallSelection(ctx, f.id); err != nil {
					t.Fatal(err)
				}
			}
			journal, err := os.ReadFile(filepath.Join(f.root.Config(), TransactionJournalFile))
			if err != nil {
				t.Fatal(err)
			}
			want := f.before
			if phase == "selected" {
				want.Preferences = f.next.Preferences
				want.Active = f.next.Active
			}
			for i := 0; i < 2; i++ {
				if err := install.RestoreConfig(ctx, f.id); err != nil {
					t.Fatal(err)
				}
				f.assertFiles(t, want)
				f.assertPending(t, true)
			}
			after, err := os.ReadFile(filepath.Join(f.root.Config(), TransactionJournalFile))
			if err != nil || !bytes.Equal(journal, after) {
				t.Fatal("installer altered transaction evidence", err)
			}
			refusal := errors.New("live restoration not proven")
			if err := f.j.Rollback(ctx, f.id, func(context.Context, TransactionRecord) error { return refusal }); !errors.Is(err, refusal) {
				t.Fatal("failed proof ignored", err)
			}
			f.assertFiles(t, f.before)
			f.assertPending(t, true)
			if err := f.j.Rollback(ctx, f.id, func(context.Context, TransactionRecord) error { return nil }); err != nil {
				t.Fatal(err)
			}
			f.assertFiles(t, f.before)
			f.assertPending(t, false)
		})
	}
}

func TestTransactionLiveConfigRejectsForeignAuthority(t *testing.T) {
	for _, kind := range []string{"config", "preferences", "active", "receipt", "owner", "journal", "parent", "config-link", "receipt-link", "preferences-link", "committed", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			install := liveConfigInstaller(t, f.j)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := f.j.InstallConfig(ctx, f.id); err != nil {
				t.Fatal(err)
			}
			if err := f.j.InstallSelection(ctx, f.id); err != nil {
				t.Fatal(err)
			}
			metadata := filepath.Join(f.root.Data(), "config")
			paths := map[string]string{"config": f.root.XrayConfig(), "preferences": filepath.Join(metadata, "system/server-reserve.json"), "active": filepath.Join(metadata, "active-server"), "receipt": filepath.Join(metadata, "interface.json"), "journal": filepath.Join(f.root.Config(), TransactionJournalFile)}
			id := f.id
			switch kind {
			case "config", "preferences", "active", "receipt", "journal":
				transactionWrite(t, paths[kind], []byte(`{"foreign":true}`))
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
				paths["journal"] = filepath.Join(f.root.Config()+".held", TransactionJournalFile)
			case "config-link", "receipt-link", "preferences-link":
				name := kind[:len(kind)-5]
				value, err := os.ReadFile(paths[name])
				if err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "protected.json")
				transactionWrite(t, outside, value)
				if err := os.Remove(paths[name]); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, paths[name]); err != nil {
					t.Fatal(err)
				}
				paths["outside"] = outside
			case "committed":
				record, err := f.j.Read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				record.Phase = "committed"
				raw, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				transactionWrite(t, paths["journal"], raw)
			case "cancel":
				cancel()
			}
			before := map[string][]byte{}
			for name, path := range paths {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(name, err)
				}
				before[name] = raw
			}
			if err := install.RestoreConfig(ctx, id); err == nil {
				t.Fatal("foreign/committed authority accepted")
			}
			for name, path := range paths {
				raw, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, before[name]) {
					t.Fatal("refusal mutated", name, err)
				}
			}
		})
	}
}

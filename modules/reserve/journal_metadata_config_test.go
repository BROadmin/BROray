package reserve

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type metadataConfigInstaller interface {
	RestoreConfigAfterMetadataProof(context.Context, TransactionRecord, []byte) error
}

func TestTransactionMetadataConfigRestore(t *testing.T) {
	for _, kind := range []string{"restore", "receipt-drift", "empty-proof", "oversized-proof", "record-drift", "record-image", "config", "preferences", "active", "committed", "cancel", "receipt-link", "parent"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			installer, ok := any(f.j).(metadataConfigInstaller)
			if !ok {
				t.Fatal("exact verified-receipt config installer absent")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := f.j.InstallConfig(ctx, f.id); err != nil {
				t.Fatal(err)
			}
			if err := f.j.InstallSelection(ctx, f.id); err != nil {
				t.Fatal(err)
			}
			record, err := f.j.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			receiptPath := filepath.Join(f.root.Data(), "config/interface.json")
			canonical := []byte(`{"owner":"BROray","canonical":true}`)
			transactionWrite(t, receiptPath, canonical)
			if err := f.j.RestoreConfig(ctx, f.id); err == nil {
				t.Fatal("original receipt guard bypassed")
			}
			proof := append([]byte(nil), canonical...)
			paths := map[string]string{"config": f.root.XrayConfig(), "preferences": filepath.Join(f.root.Data(), "config/system/server-reserve.json"), "active": filepath.Join(f.root.Data(), "config/active-server"), "receipt": receiptPath, "journal": filepath.Join(f.root.Config(), TransactionJournalFile)}
			switch kind {
			case "receipt-drift":
				transactionWrite(t, receiptPath, []byte(`{"foreign":true}`))
			case "empty-proof":
				proof = nil
			case "oversized-proof":
				proof = bytes.Repeat([]byte("x"), maxTransactionReceiptBytes+1)
			case "record-drift":
				record.Intent.Authority = strings.Repeat("b", 64)
			case "record-image":
				record.Previous.Config.Data = append(record.Previous.Config.Data, byte(' '))
			case "config", "preferences", "active":
				transactionWrite(t, paths[kind], []byte(`{"foreign":true}`))
			case "committed":
				changed := cloneTransactionRecord(record)
				changed.Phase = "committed"
				raw, e := json.Marshal(changed)
				if e != nil {
					t.Fatal(e)
				}
				transactionWrite(t, paths["journal"], raw)
			case "cancel":
				cancel()
			case "receipt-link":
				outside := filepath.Join(t.TempDir(), "receipt.json")
				transactionWrite(t, outside, canonical)
				if e := os.Remove(receiptPath); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(outside, receiptPath); e != nil {
					t.Fatal(e)
				}
				paths["outside"] = outside
			case "parent":
				if e := os.Rename(f.root.Config(), f.root.Config()+".held"); e != nil {
					t.Fatal(e)
				}
				if e := os.Mkdir(f.root.Config(), 0700); e != nil {
					t.Fatal(e)
				}
				transactionWrite(t, f.root.XrayConfig(), []byte(`{"foreign":true}`))
				paths["journal"] = filepath.Join(f.root.Config()+".held", TransactionJournalFile)
			}
			before := map[string][]byte{}
			for n, p := range paths {
				b, e := os.ReadFile(p)
				if e != nil {
					t.Fatal(e)
				}
				before[n] = b
			}
			err = installer.RestoreConfigAfterMetadataProof(ctx, record, proof)
			if kind != "restore" {
				if err == nil {
					t.Fatal("foreign authority accepted")
				}
				for n, p := range paths {
					b, e := os.ReadFile(p)
					if e != nil || !bytes.Equal(b, before[n]) {
						t.Fatal("refusal mutated", n, e)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := f.next
			want.Config = f.before.Config
			f.assertFiles(t, want)
			f.assertPending(t, true)
			if err := installer.RestoreConfigAfterMetadataProof(ctx, record, proof); err != nil {
				t.Fatal("idempotent restore", err)
			}
			for _, n := range []string{"receipt", "journal", "preferences", "active"} {
				b, e := os.ReadFile(paths[n])
				if e != nil || !bytes.Equal(b, before[n]) {
					t.Fatal("config installer changed", n, e)
				}
			}
			if err := f.j.Rollback(ctx, f.id, func(context.Context, TransactionRecord) error { return nil }); err != nil {
				t.Fatal(err)
			}
			f.assertFiles(t, f.before)
			f.assertPending(t, false)
		})
	}
}

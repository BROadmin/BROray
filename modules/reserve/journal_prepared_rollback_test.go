package reserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type preparedRollback interface {
	RollbackPrepared(context.Context, TransactionRecord, func(context.Context, TransactionRecord) error) error
}

func TestTransactionPreparedRollbackExactRecord(t *testing.T) {
	for _, kind := range []string{"prepared", "committed", "missing", "expected-record", "stored-record", "verifier-failure"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransactionFixture(t)
			f.begin(t)
			ctx := context.Background()
			api, ok := any(f.j).(preparedRollback)
			if !ok {
				t.Fatal("atomic prepared-record rollback capability absent")
			}
			record, err := f.j.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(f.root.Config(), TransactionJournalFile)
			want := f.before
			switch kind {
			case "committed":
				if err := f.j.InstallConfig(ctx, f.id); err != nil {
					t.Fatal(err)
				}
				if err := f.j.InstallSelection(ctx, f.id); err != nil {
					t.Fatal(err)
				}
				changed := cloneTransactionRecord(record)
				changed.Phase = "committed"
				raw, e := json.Marshal(changed)
				if e != nil {
					t.Fatal(e)
				}
				transactionWrite(t, path, raw)
				want = f.next
			case "missing":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
			case "expected-record":
				record.Intent.Authority = strings.Repeat("b", 64)
			case "stored-record":
				changed := cloneTransactionRecord(record)
				changed.Intent.Authority = strings.Repeat("b", 64)
				raw, e := json.Marshal(changed)
				if e != nil {
					t.Fatal(e)
				}
				transactionWrite(t, path, raw)
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil && kind != "missing" {
				t.Fatal(readErr)
			}
			called := 0
			refused := errors.New("old runtime unconfirmed")
			err = api.RollbackPrepared(ctx, record, func(_ context.Context, r TransactionRecord) error {
				called++
				if !reflect.DeepEqual(r, record) {
					t.Fatal("wrong record")
				}
				if kind == "verifier-failure" {
					return refused
				}
				return nil
			})
			f.assertFiles(t, want)
			if kind == "prepared" {
				if err != nil || called != 1 {
					t.Fatal(err, called)
				}
				f.assertPending(t, false)
				return
			}
			if err == nil {
				t.Fatal("unproven decision cleared")
			}
			if kind == "verifier-failure" {
				if !errors.Is(err, refused) || called != 1 {
					t.Fatal(err, called)
				}
			} else if called != 0 {
				t.Fatal("foreign verifier invoked")
			}
			after, e := os.ReadFile(path)
			if kind == "missing" {
				if !errors.Is(e, os.ErrNotExist) {
					t.Fatal("missing journal recreated", e)
				}
			} else if e != nil || !bytes.Equal(after, raw) {
				t.Fatal("uncertain journal changed", e)
			}
		})
	}
}

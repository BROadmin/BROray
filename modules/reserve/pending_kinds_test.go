package reserve

import (
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/platform"
	"os"
	"path/filepath"
	"testing"
)

func TestPendingTransactionKinds(t *testing.T) {
	for _, selection := range []bool{false, true} {
		for _, profile := range []bool{false, true} {
			root := platform.Root{Dir: t.TempDir()}
			if err := root.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			for name, present := range map[string]bool{TransactionJournalFile: selection, ProfileTransactionJournalFile: profile} {
				if present {
					if err := os.WriteFile(filepath.Join(root.Config(), name), []byte(`{"unfinished":true}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			a, b, err := PendingTransactions(context.Background(), root)
			if err != nil || a != selection || b != profile {
				t.Fatal("wrong journal classification", a, b, err)
			}
			any, err := TransactionPending(context.Background(), root)
			if err != nil || any != (selection || profile) {
				t.Fatal("aggregate contract changed", any, err)
			}
		}
	}
}

func TestPendingTransactionKindsRejectForeignEntries(t *testing.T) {
	for _, name := range []string{TransactionJournalFile, ProfileTransactionJournalFile} {
		for _, kind := range []string{"symlink", "directory", "oversized", "cancelled"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				root := platform.Root{Dir: t.TempDir()}
				if err := root.EnsureDirs(); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root.Config(), name)
				foreign := filepath.Join(root.Dir, "foreign")
				if err := os.WriteFile(foreign, []byte("preserved"), 0600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch kind {
				case "symlink":
					if err := os.Symlink(foreign, path); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				case "oversized":
					f, err := os.Create(path)
					if err != nil {
						t.Fatal(err)
					}
					err = f.Truncate(maxProfileJournalBytes + maxTransactionJournalBytes + 1)
					closeErr := f.Close()
					if err != nil || closeErr != nil {
						t.Fatal(err, closeErr)
					}
				case "cancelled":
					cancel()
				}
				a, b, err := PendingTransactions(ctx, root)
				if err == nil || a || b {
					t.Fatal("unsafe journal classified", a, b, err)
				}
				if kind == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
				raw, err := os.ReadFile(foreign)
				if err != nil || string(raw) != "preserved" {
					t.Fatal("foreign entry changed", err)
				}
			})
		}
	}
}

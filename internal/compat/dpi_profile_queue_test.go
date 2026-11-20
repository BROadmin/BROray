package compat

import (
	"bytes"
	"context"
	"github.com/BROadmin/broray/modules/reserve"
	"os"
	"path/filepath"
	"testing"
)

func TestDPIProfilePendingRetainsQueueOwner(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "symlink"}[linked], func(t *testing.T) {
			d := reserveQueueDeps(t)
			path := filepath.Join(d.Root.Config(), reserve.ProfileTransactionJournalFile)
			foreign := filepath.Join(d.Root.Dir, "foreign-profile-journal")
			raw := []byte(`{"unfinished":true}`)
			got := runReserveQueue(t, d, context.Background(), ReserveSwitchAction, ReserveOperationTarget, []Stage{{Name: "switch", Global: true, Protected: true, Run: func(*Task) error {
				if linked {
					if err := os.WriteFile(foreign, raw, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(foreign, path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				return nil
			}}})
			owner, ok := d.Lock.Current()
			if got.State != "failed" || got.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" || !ok || !owner.Pending || !owner.DaemonOwnedReserve {
				t.Fatal("profile owner released", got, owner, ok)
			}
			if release, ok := d.Lock.TryAcquire("operations", "servers:activate", "other"); ok {
				release()
				t.Fatal("competing operation admitted")
			}
			if !NewGlobalLock(d.RunPath("global-operation.json")).Busy() {
				t.Fatal("restart lost owner")
			}
			read := path
			if linked {
				read = foreign
			}
			after, err := os.ReadFile(read)
			if err != nil || !bytes.Equal(after, raw) {
				t.Fatal("evidence changed", err)
			}
		})
	}
}

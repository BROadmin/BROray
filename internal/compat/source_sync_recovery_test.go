package compat

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sourceSyncFixtureMarker(t *testing.T, root platform.Root, kind string) {
	t.Helper()
	work := filepath.Join(root.Tmp(), "server-subscription-sync.1.fixture")
	if kind == "legacy-lock" {
		if err := os.WriteFile(filepath.Join(root.Run(), "server-subscription.lock"), []byte("pending"), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if kind == "foreign-work" {
		foreign := t.TempDir()
		if err := os.WriteFile(filepath.Join(foreign, "rollback-required"), []byte("foreign preserved"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, work); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(work, "rollback-required")
	switch kind {
	case "empty":
		return
	case "fifo":
		if err := unix.Mkfifo(marker, 0600); err != nil {
			t.Fatal(err)
		}
	case "directory":
		if err := os.Mkdir(marker, 0700); err != nil {
			t.Fatal(err)
		}
	case "foreign-marker":
		foreign := filepath.Join(t.TempDir(), "private")
		if err := os.WriteFile(foreign, []byte("preserved"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, marker); err != nil {
			t.Fatal(err)
		}
	default:
		if err := os.WriteFile(marker, []byte("source backup preserved"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSourceSyncQueueRetainsCatalogRecovery(t *testing.T) {
	for _, kind := range []string{"marker", "error", "panic", "fifo", "directory", "foreign-work", "foreign-marker", "legacy-lock", "empty", "preexisting"} {
		t.Run(kind, func(t *testing.T) {
			d := reserveQueueDeps(t)
			if kind == "preexisting" {
				sourceSyncFixtureMarker(t, d.Root, "marker")
			}
			ran := false
			got := runReserveQueue(t, d, context.Background(), SourceRefreshAction, "source-a", refreshQueueStages(func(*Task) error { return nil }, func(*Task) error {
				ran = true
				sourceSyncFixtureMarker(t, d.Root, kind)
				if kind == "panic" {
					panic("catalog cleanup incomplete")
				}
				if kind == "error" {
					return errors.New("catalog rollback incomplete")
				}
				return nil
			}))
			if kind == "empty" {
				if got.State != "completed" || d.Lock.Busy() {
					t.Fatal("empty workspace blocked", got)
				}
				return
			}
			if kind == "preexisting" && ran {
				t.Error("apply entered pending source workspace")
			}
			owner, ok := d.Lock.Current()
			if got.State != "failed" || got.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" || !ok || !owner.Pending || !owner.DaemonOwnedReserve {
				t.Fatalf("catalog owner lost: %+v %+v", got, owner)
			}
			if !NewGlobalLock(d.RunPath("global-operation.json")).Busy() {
				t.Fatal("persisted source owner lost")
			}
			if release, ok := d.Lock.TryAcquire("operations", "servers:activate", "other"); ok {
				release()
				t.Fatal("competing mutation admitted")
			}
			if kind != "legacy-lock" {
				if _, err := os.Lstat(filepath.Join(d.Root.Tmp(), "server-subscription-sync.1.fixture")); err != nil {
					t.Fatal("workspace removed", err)
				}
			}
		})
	}
}
func TestSourceSyncRecoveryNeverDiscardsCatalogMarker(t *testing.T) {
	for _, kind := range []string{"existing", "orphan", "callback-marker"} {
		t.Run(kind, func(t *testing.T) {
			root := platform.Root{Dir: t.TempDir()}
			if err := root.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			guard, err := platform.TryLock(root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			p := filepath.Join(root.Run(), "global-operation.json")
			var before []byte
			if kind != "orphan" {
				now := time.Now()
				h := Holder{Scope: "operations", Action: SourceRefreshAction, BundleID: ReserveOperationTarget, StartedAt: now, UpdatedAt: now, Pending: true, DaemonOwnedReserve: true}
				if err := platform.WriteJSONAtomic(p, h, 0600); err != nil {
					t.Fatal(err)
				}
				before, err = os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
			}
			if kind != "callback-marker" {
				sourceSyncFixtureMarker(t, root, "marker")
			}
			g := NewGlobalLock(p)
			called := false
			err = g.ReconcileDaemonReserve(root, guard, false, func() error {
				called = true
				if kind == "callback-marker" {
					sourceSyncFixtureMarker(t, root, "marker")
				}
				return nil
			})
			if err == nil || called != (kind == "callback-marker") {
				t.Fatal("unsafe reconciliation", called, err)
			}
			if kind == "orphan" {
				if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("orphan check rewrote ownership", err)
				}
			} else {
				after, err := os.ReadFile(p)
				if err != nil || !bytes.Equal(before, after) || !g.Busy() {
					t.Fatal("owner changed before source recovery", err)
				}
			}
			marker := filepath.Join(root.Tmp(), "server-subscription-sync.1.fixture", "rollback-required")
			b, err := os.ReadFile(marker)
			if err != nil || string(b) != "source backup preserved" {
				t.Fatal("marker changed", err)
			}
		})
	}
}

func TestSourceSyncWorkspaceParentBoundaries(t *testing.T) {
	for _, kind := range []string{"absent-parents", "foreign-run", "foreign-tmp", "unrelated"} {
		t.Run(kind, func(t *testing.T) {
			root := platform.Root{Dir: filepath.Join(t.TempDir(), "isolated")}
			if err := os.MkdirAll(root.Dir, 0700); err != nil {
				t.Fatal(err)
			}
			foreign := t.TempDir()
			canary := filepath.Join(foreign, "private")
			if err := os.WriteFile(canary, []byte("preserve foreign bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "foreign-run" || kind == "foreign-tmp" {
				target := root.Run()
				if kind == "foreign-tmp" {
					target = root.Tmp()
				}
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, target); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "unrelated" {
				if err := os.MkdirAll(root.Tmp(), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root.Tmp(), "ordinary-file"), []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := CheckSourceSyncWorkspaces(root)
			wantError := kind == "foreign-run" || kind == "foreign-tmp"
			if (err != nil) != wantError {
				t.Fatal("parent boundary", kind, err)
			}
			b, err := os.ReadFile(canary)
			if err != nil || string(b) != "preserve foreign bytes" {
				t.Fatal("foreign bytes changed", err)
			}
		})
	}
}

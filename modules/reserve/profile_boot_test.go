package reserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

func TestDPIStoppedRecoveryRetainsBootIntent(t *testing.T) {
	f := newProfileFixture(t, true)
	f.r.WasRunning = false
	writeProfileRecoveryRecord(t, f, f.r)
	if err := f.j.Install(context.Background(), f.r); err != nil {
		t.Fatal(err)
	}
	guard, err := platform.TryLock(f.f.root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	if err := f.j.Recover(context.Background(), guard, func(context.Context, ProfileTransactionRecord) error { return nil }); err != nil {
		t.Fatal(err)
	}
	f.assertPending(t, false)
	path := filepath.Join(f.f.root.Config(), "server-dpi-boot-hold.v1.json")
	before, err := os.ReadFile(path)
	if err != nil || len(before) == 0 {
		t.Fatal("stopped intent lost when pending journal cleared", err)
	}
	other, err := OpenProfileJournal(f.f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.Recover(context.Background(), guard, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("second recovery lost stopped intent", err)
	}
}

func bootHoldBytes(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(profileBootHold{1, strings.Repeat("a", 64), "old-server", strings.Repeat("b", 64), strings.Repeat("c", 64)})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDPIBootHoldStrictSchema(t *testing.T) {
	valid := string(bootHoldBytes(t))
	if _, err := decodeProfileBootHold([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"missing": `{}`, "null": `null`, "array": `[]`, "invalid-utf8": string([]byte{255}),
		"oversize":      strings.Repeat(" ", maxProfileBootHoldBytes+1),
		"duplicate":     strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1),
		"unknown":       strings.Replace(valid, `"schemaVersion":1`, `"unknown":1,"schemaVersion":1`, 1),
		"null-field":    strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":null`, 1),
		"fraction":      strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1.5`, 1),
		"wrong-version": strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":2`, 1),
		"bad-hash":      strings.Replace(valid, strings.Repeat("a", 64), "bad", 1),
		"path":          strings.Replace(valid, "old-server", "../old-server", 1),
		"trailing":      valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProfileBootHold([]byte(raw)); err == nil {
				t.Fatal("accepted invalid hold")
			}
		})
	}
}

func TestDPIBootStartDecision(t *testing.T) {
	for _, mode := range []string{"automatic", "manual", "manual-fails", "absent", "canceled", "nil-callback", "malformed", "symlink", "directory", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			f := newTransactionFixture(t)
			path := filepath.Join(f.root.Config(), ProfileBootHoldFile)
			raw := bootHoldBytes(t)
			if mode == "malformed" {
				raw = []byte(`{"foreign":true}`)
			}
			if mode == "oversize" {
				raw = bytes.Repeat([]byte("x"), maxProfileBootHoldBytes+1)
			}
			switch mode {
			case "absent":
			case "symlink":
				target := filepath.Join(t.TempDir(), "foreign")
				transactionWrite(t, target, raw)
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			default:
				transactionWrite(t, path, raw)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			called := false
			launchErr := errors.New("launch failure")
			callback := func() error {
				called = true
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("hold not retired before intentional launch", err)
				}
				parent, err := platform.OpenDirNoFollow(f.root.Config())
				if err != nil {
					t.Fatal(err)
				}
				defer parent.Close()
				if lock, err := reserveRecoveryGuard(parent, profileBootLockFile); err == nil {
					lock.Close()
					t.Fatal("launch callback lost boot-intent exclusion")
				}
				if mode == "manual-fails" {
					return launchErr
				}
				return nil
			}
			if mode == "nil-callback" {
				callback = nil
			}
			err := WithProfileStart(ctx, f.root, mode == "automatic", callback)
			wantCall := mode == "manual" || mode == "manual-fails" || mode == "absent"
			if called != wantCall {
				t.Fatal("unexpected launch decision", called, err)
			}
			if mode == "manual-fails" {
				if !errors.Is(err, launchErr) {
					t.Fatal(err)
				}
			} else if wantCall {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid start accepted")
			}
			if mode == "automatic" && !errors.Is(err, ErrProfileBootHeld) {
				t.Fatal(err)
			}
			if !wantCall && mode != "directory" {
				after, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(after, raw) {
					t.Fatal("refused launch changed foreign/held bytes", e)
				}
			}
		})
	}
}

func TestDPIBootIntentLockDoesNotBorrowOrdinaryTransactionLock(t *testing.T) {
	f := newTransactionFixture(t)
	parent, err := platform.OpenDirNoFollow(f.root.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := unix.Flock(int(parent.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(parent.Fd()), unix.LOCK_UN)
	called := false
	if err := WithProfileStart(context.Background(), f.root, true, func() error { called = true; return nil }); err != nil || !called {
		t.Fatal("ordinary transaction blocked absent-marker launch", err)
	}
	lock, err := reserveRecoveryGuard(parent, profileBootLockFile)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	called = false
	if err := WithProfileStart(context.Background(), f.root, false, func() error { called = true; return nil }); err == nil || called {
		t.Fatal("foreign boot-intent lock allowed launch", err)
	}
}

func TestDPIRecoveryRetainsBootLock(t *testing.T) {
	for _, mode := range []string{"launch-blocked", "inode-replaced", "foreign-live", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := newProfileFixture(t, true)
			f.r.WasRunning = false
			writeProfileRecoveryRecord(t, f, f.r)
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			parent := f.j.base.dirs[f.f.root.Config()]
			path := filepath.Join(f.f.root.Config(), profileBootLockFile)
			if mode == "foreign-live" {
				lock, e := reserveRecoveryGuard(parent, profileBootLockFile)
				if e != nil {
					t.Fatal(e)
				}
				defer lock.Close()
			}
			if mode == "symlink" {
				target := filepath.Join(t.TempDir(), "foreign")
				transactionWrite(t, target, []byte("owner"))
				if e := os.Symlink(target, path); e != nil {
					t.Fatal(e)
				}
			}
			verified := false
			err = f.j.Recover(context.Background(), guard, func(context.Context, ProfileTransactionRecord) error {
				verified = true
				if mode == "inode-replaced" {
					if e := os.Remove(path); e != nil {
						return e
					}
					return os.WriteFile(path, []byte("foreign"), 0600)
				}
				called := false
				if e := WithProfileStart(context.Background(), f.f.root, false, func() error { called = true; return nil }); e == nil || called {
					t.Fatal("recovery let intentional launch consume stopped hold", e)
				}
				return nil
			})
			if mode == "launch-blocked" {
				if err != nil || !verified {
					t.Fatal(err)
				}
				f.assertPending(t, false)
			} else {
				if err == nil {
					t.Fatal("lost boot lock accepted")
				}
				f.assertPending(t, true)
			}
			if (mode == "foreign-live" || mode == "symlink") && verified {
				t.Fatal("verifier entered without boot exclusion")
			}
		})
	}
}

func TestDPIStoppedRecoveryDecisionHashesAndRetry(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "committed"}[committed], func(t *testing.T) {
			f := newProfileFixture(t, true)
			f.r.WasRunning = false
			writeProfileRecoveryRecord(t, f, f.r)
			if err := f.j.Install(context.Background(), f.r); err != nil {
				t.Fatal(err)
			}
			want := f.r.Previous
			if committed {
				f.r.Phase = "committed"
				writeProfileRecoveryRecord(t, f, f.r)
				want = f.r.Next
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
			proofErr := errors.New("verification failed")
			if err := f.j.Recover(context.Background(), guard, func(context.Context, ProfileTransactionRecord) error { return proofErr }); !errors.Is(err, proofErr) {
				t.Fatal(err)
			}
			f.assertPending(t, true)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			hold, err := decodeProfileBootHold(before)
			if err != nil {
				t.Fatal(err)
			}
			if hold.Operation != f.r.ID || hold.ServerID != f.r.ServerID || hold.RecordSHA256 != want.Record.SHA256 || hold.ConfigSHA256 != want.Config.SHA256 {
				t.Fatal("wrong durable recovery decision", hold)
			}
			if err := f.j.Recover(context.Background(), guard, func(context.Context, ProfileTransactionRecord) error { return nil }); err != nil {
				t.Fatal(err)
			}
			f.assertPending(t, false)
			f.assertState(t, committed)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("retry changed held decision", err)
			}
		})
	}
}

func TestDPIStoppedRecoveryPreservesPendingOnHoldDrift(t *testing.T) {
	for _, kind := range []string{"inode", "bytes", "removed"} {
		t.Run(kind, func(t *testing.T) {
			f := newProfileFixture(t, true)
			f.r.WasRunning = false
			writeProfileRecoveryRecord(t, f, f.r)
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
			err = f.j.Recover(context.Background(), guard, func(context.Context, ProfileTransactionRecord) error {
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if kind == "bytes" {
					return os.WriteFile(path, bootHoldBytes(t), 0600)
				}
				if e := os.Remove(path); e != nil {
					return e
				}
				if kind == "inode" {
					return os.WriteFile(path, raw, 0600)
				}
				return nil
			})
			if err == nil {
				t.Fatal("cleared pending after hold ownership changed")
			}
			f.assertPending(t, true)
		})
	}
}

func TestDPIBootHoldAppliesOnlyToStoppedActiveProfile(t *testing.T) {
	for _, mode := range []string{"inactive", "running", "inactive-existing", "running-existing"} {
		t.Run(mode, func(t *testing.T) {
			f := newProfileFixture(t, strings.HasPrefix(mode, "running"))
			path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
			existing := strings.HasSuffix(mode, "existing")
			if existing {
				transactionWrite(t, path, bootHoldBytes(t))
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			err = f.j.Recover(context.Background(), guard, func(context.Context, ProfileTransactionRecord) error { return nil })
			if mode == "running-existing" {
				if err == nil {
					t.Fatal("contradictory running intent accepted")
				}
				f.assertPending(t, true)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				f.assertPending(t, false)
			}
			if existing {
				raw, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(raw, bootHoldBytes(t)) {
					t.Fatal("foreign hold changed", e)
				}
			} else if _, e := os.Lstat(path); !os.IsNotExist(e) {
				t.Fatal("unnecessary hold created", e)
			}
		})
	}
}

func TestDPIBootIntentWriteRequiresExactJournalOwner(t *testing.T) {
	f := newProfileFixture(t, true)
	f.r.WasRunning = false
	writeProfileRecoveryRecord(t, f, f.r)
	r, owner, held, err := f.j.load()
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	path := filepath.Join(f.f.root.Config(), ProfileTransactionJournalFile)
	transactionWrite(t, path, []byte(`{"foreign":true}`))
	_, file, err := f.j.preserveBootIntent(r, r.Previous, owner, held)
	if file != nil {
		file.Close()
	}
	if err == nil {
		t.Fatal("foreign recovery journal authorized a boot-hold write")
	}
	if _, err := os.Lstat(filepath.Join(f.f.root.Config(), "server-dpi-boot-hold.v1.json")); !os.IsNotExist(err) {
		t.Fatal("hold created without exact recovery owner", err)
	}
}

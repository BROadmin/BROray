package reserve

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

type stoppedProfileFinisher interface {
	ConfirmStopped(context.Context, ProfileTransactionRecord, *platform.Lock, func(context.Context, ProfileTransactionRecord) error) error
	RollbackStopped(context.Context, ProfileTransactionRecord, *platform.Lock, func(context.Context, ProfileTransactionRecord) error) error
}

func TestDPIStoppedLiveFinishPreservesIntent(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			f := newProfileFixture(t, true)
			ctx := context.Background()
			f.r.WasRunning = false
			writeProfileRecoveryRecord(t, f, f.r)
			if commit {
				if err := f.j.Install(ctx, f.r); err != nil {
					t.Fatal(err)
				}
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			verify := func(context.Context, ProfileTransactionRecord) error { return nil }
			if finish, ok := any(f.j).(stoppedProfileFinisher); ok {
				if commit {
					err = finish.ConfirmStopped(ctx, f.r, guard, verify)
				} else {
					err = finish.RollbackStopped(ctx, f.r, guard, verify)
				}
			} else if commit {
				err = f.j.Confirm(ctx, f.r, verify)
			} else {
				err = f.j.RollbackPrepared(ctx, f.r, verify)
			}
			if err != nil {
				t.Fatal(err)
			}
			f.assertState(t, commit)
			f.assertPending(t, false)
			raw, err := os.ReadFile(filepath.Join(f.f.root.Config(), ProfileBootHoldFile))
			if err != nil {
				t.Fatal("ordinary stopped finish lost durable intent", err)
			}
			hold, err := decodeProfileBootHold(raw)
			if err != nil {
				t.Fatal(err)
			}
			want := f.r.Previous
			if commit {
				want = f.r.Next
			}
			if hold.Operation != f.r.ID || hold.RecordSHA256 != want.Record.SHA256 || hold.ConfigSHA256 != want.Config.SHA256 {
				t.Fatal("wrong stopped decision", hold)
			}
			if err = WithProfileStart(ctx, f.f.root, true, func() error { t.Fatal("implicit start allowed"); return nil }); err != ErrProfileBootHeld {
				t.Fatal(err)
			}
			after, err := os.ReadFile(filepath.Join(f.f.root.Config(), ProfileBootHoldFile))
			if err != nil || !bytes.Equal(after, raw) {
				t.Fatal("automatic start changed hold", err)
			}
		})
	}
}

func TestDPIStoppedLiveFinishCannotBypassGuard(t *testing.T) {
	for _, mode := range []string{"commit", "rollback", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			f := newProfileFixture(t, true)
			ctx := context.Background()
			f.r.WasRunning = false
			writeProfileRecoveryRecord(t, f, f.r)
			if mode != "rollback" {
				if err := f.j.Install(ctx, f.r); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			verify := func(context.Context, ProfileTransactionRecord) error { called = true; return nil }
			var err error
			switch mode {
			case "commit":
				err = f.j.Confirm(ctx, f.r, verify)
			case "rollback":
				err = f.j.RollbackPrepared(ctx, f.r, verify)
			case "cleanup":
				f.r.Phase = "committed"
				writeProfileRecoveryRecord(t, f, f.r)
				err = f.j.CleanupCommitted(ctx)
			}
			if err == nil || called {
				t.Fatal("unguarded stopped decision cleared", mode, err, called)
			}
			f.assertPending(t, true)
		})
	}
}

func TestDPIStoppedLiveFinishPriorLease(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			f, hold := successiveHoldFixture(t, true)
			ctx := context.Background()
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			lease, err := OpenStoppedProfile(ctx, f.f.root, guard)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if commit {
				if err = f.j.Install(ctx, f.r); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			verify := func(context.Context, ProfileTransactionRecord) error {
				calls++
				called := false
				if err := WithProfileStart(ctx, f.f.root, false, func() error { called = true; return nil }); err == nil || called {
					t.Fatal("finish released boot exclusion")
				}
				return lease.Check(ctx, f.f.root)
			}
			if commit {
				err = f.j.ConfirmStoppedLeased(ctx, f.r, lease, verify)
			} else {
				err = f.j.RollbackStoppedLeased(ctx, f.r, lease, verify)
			}
			if err != nil || calls != 2 {
				t.Fatal("leased finish", err, calls)
			}
			after, err := os.Stat(path)
			raw, e := os.ReadFile(path)
			if err != nil || e != nil || !os.SameFile(before, after) || !bytes.Equal(raw, hold) {
				t.Fatal("prior hold replaced", err, e)
			}
			if err = lease.Check(ctx, f.f.root); err != nil {
				t.Fatal("borrowed lease closed", err)
			}
			f.assertPending(t, false)
			f.assertState(t, commit)
		})
	}
}

func TestDPIStoppedLiveFinishInterruptedProof(t *testing.T) {
	for _, commit := range []bool{false, true} {
		for _, failAt := range []int{1, 2} {
			name := map[bool]string{false: "rollback", true: "commit"}[commit] + map[int]string{1: "-before", 2: "-after"}[failAt]
			t.Run(name, func(t *testing.T) {
				f := newProfileFixture(t, true)
				ctx := context.Background()
				f.r.WasRunning = false
				writeProfileRecoveryRecord(t, f, f.r)
				if commit {
					if err := f.j.Install(ctx, f.r); err != nil {
						t.Fatal(err)
					}
				}
				guard, err := platform.TryLock(f.f.root.DaemonLock())
				if err != nil {
					t.Fatal(err)
				}
				defer guard.Release()
				sentinel := errors.New("proof interrupted")
				calls := 0
				verify := func(context.Context, ProfileTransactionRecord) error {
					calls++
					if calls == failAt {
						return sentinel
					}
					return nil
				}
				if commit {
					err = f.j.ConfirmStopped(ctx, f.r, guard, verify)
				} else {
					err = f.j.RollbackStopped(ctx, f.r, guard, verify)
				}
				if !errors.Is(err, sentinel) {
					t.Fatal(err)
				}
				f.assertPending(t, true)
				r, err := f.j.Read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				accepted := commit && failAt == 2
				if (r.Phase == "committed") != accepted {
					t.Fatal("wrong durable decision", r.Phase)
				}
				if err = f.j.Recover(ctx, guard, func(context.Context, ProfileTransactionRecord) error { return nil }); err != nil {
					t.Fatal(err)
				}
				f.assertState(t, accepted)
				f.assertPending(t, false)
				if err = WithProfileStart(ctx, f.f.root, true, func() error { t.Fatal("recovery started stopped profile"); return nil }); err != ErrProfileBootHeld {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestDPIStoppedLiveFinishRefusesOwnershipDrift(t *testing.T) {
	for _, mode := range []string{"nil-daemon", "closed-daemon", "child", "installer", "launcher", "boot", "hold-inode", "hold-bytes", "context", "journal"} {
		t.Run(mode, func(t *testing.T) {
			f, hold := successiveHoldFixture(t, true)
			ctx := context.Background()
			if err := f.j.Install(ctx, f.r); err != nil {
				t.Fatal(err)
			}
			guard, err := platform.TryLock(f.f.root.DaemonLock())
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Release()
			switch mode {
			case "nil-daemon":
				guard = nil
			case "closed-daemon":
				guard.Release()
			}
			lockName := map[string]string{"child": filepath.Join(f.f.root.Run(), "xray-child.lock"), "installer": f.f.root.InstallerLock(), "launcher": f.f.root.LauncherLock(), "boot": filepath.Join(f.f.root.Config(), profileBootLockFile)}[mode]
			if lockName != "" {
				parent, e := platform.OpenDirNoFollow(filepath.Dir(lockName))
				if e != nil {
					t.Fatal(e)
				}
				defer parent.Close()
				lock, e := reserveRecoveryGuard(parent, filepath.Base(lockName))
				if e != nil {
					t.Fatal(e)
				}
				defer lock.Close()
			}
			calls := 0
			err = f.j.ConfirmStopped(ctx, f.r, guard, func(context.Context, ProfileTransactionRecord) error {
				calls++
				path := filepath.Join(f.f.root.Config(), ProfileBootHoldFile)
				switch mode {
				case "hold-inode":
					if e := os.Rename(path, path+".original"); e != nil {
						return e
					}
					return os.WriteFile(path, hold, 0600)
				case "hold-bytes":
					return os.WriteFile(path, bytes.Replace(hold, []byte("old-server"), []byte("new-server"), 1), 0600)
				case "context":
					return os.WriteFile(filepath.Join(f.f.root.Data(), "config", "active-server"), []byte("foreign\n"), 0600)
				case "journal":
					return os.WriteFile(filepath.Join(f.f.root.Config(), ProfileTransactionJournalFile), []byte(`{"foreign":true}`), 0600)
				}
				t.Fatal("unowned exclusion reached verifier")
				return nil
			})
			if err == nil {
				t.Fatal("lost ownership accepted")
			}
			f.assertPending(t, true)
			if lockName != "" || mode == "nil-daemon" || mode == "closed-daemon" {
				if calls != 0 {
					t.Fatal("unexpected verifier", calls)
				}
			}
		})
	}
}

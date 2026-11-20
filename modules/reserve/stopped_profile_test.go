package reserve

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func stoppedProfileFixture(t *testing.T) (platform.Root, *platform.Lock) {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Config(), ProfileBootHoldFile), bootHoldBytes(t), 0600); err != nil {
		t.Fatal(err)
	}
	guard, err := platform.TryLock(root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Release() })
	return root, guard
}

func stoppedProfileLocks(root platform.Root) []string {
	return []string{filepath.Join(root.Config(), profileBootLockFile), filepath.Join(root.Run(), "xray-child.lock"), root.InstallerLock(), root.LauncherLock()}
}

func TestStoppedProfileLeaseExclusionsAndRelease(t *testing.T) {
	root, guard := stoppedProfileFixture(t)
	ctx := context.Background()
	lease, err := OpenStoppedProfile(ctx, root, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	for _, path := range stoppedProfileLocks(root) {
		other, err := platform.TryLock(path)
		if err == nil {
			other.Release()
			t.Fatal("exclusion absent", path)
		}
	}
	for _, automatic := range []bool{false, true} {
		called := false
		err := WithProfileStart(ctx, root, automatic, func() error { called = true; return nil })
		if err == nil || called {
			t.Fatal("launch entered while lease held")
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if lease.Check(ctx, root) == nil {
		t.Fatal("closed lease accepted")
	}
	for _, path := range stoppedProfileLocks(root) {
		other, err := platform.TryLock(path)
		if err != nil {
			t.Fatal("lock leaked", err)
		}
		other.Release()
	}
	got, err := os.ReadFile(filepath.Join(root.Config(), ProfileBootHoldFile))
	if err != nil || !bytes.Equal(got, bootHoldBytes(t)) {
		t.Fatal("hold changed", err)
	}
}

func TestStoppedProfileLeaseRejectsAdmission(t *testing.T) {
	for _, mode := range []string{"nil-daemon", "lost-daemon", "missing-hold", "bad-hold", "symlink-hold", "directory-hold", "cancelled", "boot-held", "child-held", "installer-held", "launcher-held", "boot-symlink", "child-directory"} {
		t.Run(mode, func(t *testing.T) {
			root, guard := stoppedProfileFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hold := filepath.Join(root.Config(), ProfileBootHoldFile)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "nil-daemon":
				guard = nil
			case "lost-daemon":
				must(guard.Release())
			case "missing-hold":
				must(os.Remove(hold))
			case "bad-hold":
				must(os.WriteFile(hold, []byte(`{}`), 0600))
			case "symlink-hold":
				must(os.Rename(hold, hold+".foreign"))
				must(os.Symlink(hold+".foreign", hold))
			case "directory-hold":
				must(os.Remove(hold))
				must(os.Mkdir(hold, 0700))
			case "cancelled":
				cancel()
			case "boot-symlink":
				must(os.Symlink(hold, stoppedProfileLocks(root)[0]))
			case "child-directory":
				must(os.Mkdir(stoppedProfileLocks(root)[1], 0700))
			default:
				index := map[string]int{"boot-held": 0, "child-held": 1, "installer-held": 2, "launcher-held": 3}[mode]
				lock, err := platform.TryLock(stoppedProfileLocks(root)[index])
				must(err)
				defer lock.Release()
			}
			lease, err := OpenStoppedProfile(ctx, root, guard)
			if lease != nil {
				lease.Close()
			}
			if err == nil {
				t.Fatal("unsafe admission", mode)
			}
		})
	}
}

func TestStoppedProfileLeaseRejectsOwnershipDrift(t *testing.T) {
	for _, mode := range []string{"hold-bytes", "hold-inode", "hold-missing", "boot-inode", "child-inode", "installer-inode", "launcher-inode", "config-root", "run-root", "daemon-inode", "daemon-release", "wrong-root", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root, guard := stoppedProfileFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lease, err := OpenStoppedProfile(ctx, root, guard)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			hold := filepath.Join(root.Config(), ProfileBootHoldFile)
			var foreign string
			switch mode {
			case "hold-bytes":
				foreign = hold
				must(os.WriteFile(hold, []byte("foreign"), 0600))
			case "hold-inode":
				foreign = hold
				must(os.Rename(hold, hold+".previous"))
				must(os.WriteFile(hold, bootHoldBytes(t), 0600))
			case "hold-missing":
				must(os.Remove(hold))
			case "config-root", "run-root":
				path := root.Config()
				if mode == "run-root" {
					path = root.Run()
				}
				must(os.Rename(path, path+".previous"))
				must(os.Mkdir(path, 0700))
			case "daemon-inode":
				foreign = root.DaemonLock()
				must(os.Rename(foreign, foreign+".previous"))
				must(os.WriteFile(foreign, []byte("foreign"), 0600))
			case "daemon-release":
				must(guard.Release())
			case "wrong-root":
				root = platform.Root{Dir: t.TempDir()}
			case "cancelled":
				cancel()
			default:
				index := map[string]int{"boot-inode": 0, "child-inode": 1, "installer-inode": 2, "launcher-inode": 3}[mode]
				foreign = stoppedProfileLocks(root)[index]
				must(os.Rename(foreign, foreign+".previous"))
				must(os.WriteFile(foreign, []byte("foreign"), 0600))
			}
			if lease.Check(ctx, root) == nil {
				t.Fatal("drift accepted", mode)
			}
			var before []byte
			if foreign != "" {
				before, err = os.ReadFile(foreign)
				must(err)
			}
			must(lease.Close())
			if foreign != "" {
				after, err := os.ReadFile(foreign)
				must(err)
				if !bytes.Equal(before, after) {
					t.Fatal("foreign data removed/changed")
				}
			}
		})
	}
}

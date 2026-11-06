package compat

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/platform"
)

// A new daemon starts while the detached updater still owns a kernel lock.
// The restarted daemon must refuse all other global mutations until that
// external owner finishes, even though its old in-memory fence is gone.
func TestNewDepsFencesDetachedUpdaterAfterRestart(t *testing.T) {
	for _, lockName := range []string{"launcher", "installer"} {
		t.Run(lockName, func(t *testing.T) {
			root := platform.Root{Dir: filepath.Join(t.TempDir(), "broray")}
			if err := root.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			lockPath := root.LauncherLock()
			if lockName == "installer" {
				lockPath = root.InstallerLock()
			}
			owner, err := platform.TryLock(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Release()

			a := &app.App{Root: root, Log: log.New(io.Discard, "", 0)}
			d := NewDeps(context.Background(), a)
			if !d.Lock.Busy() || d.Queue.GlobalFence() != "managed_active" {
				t.Fatal("detached updater is not reflected by the global fence")
			}
			if release, ok := d.Lock.TryAcquire("routes", "export", "netflix"); ok {
				release()
				t.Fatal("routes acquired the fence while detached updater is running")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			if release, err := d.Lock.Acquire(ctx, "dot", "dot:apply", ""); err == nil {
				release()
				t.Fatal("waiting DNS operation entered before updater finished")
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected bounded wait, got %v", err)
			}

			if err := owner.Release(); err != nil {
				t.Fatal(err)
			}
			if d.Lock.Busy() || d.Queue.GlobalFence() != "absent" {
				t.Fatal("global fence remained busy after updater released its lock")
			}
			release, ok := d.Lock.TryAcquire("routes", "export", "netflix")
			if !ok {
				t.Fatal("routes remained blocked after updater finished")
			}
			release()
		})
	}
}

func TestExternalFenceProbeErrorFailsClosed(t *testing.T) {
	root := platform.Root{Dir: filepath.Join(t.TempDir(), "broray")}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root.LauncherLock(), 0o700); err != nil {
		t.Fatal(err)
	}
	a := &app.App{Root: root, Log: log.New(io.Discard, "", 0)}
	d := NewDeps(context.Background(), a)
	if !d.Lock.Busy() {
		t.Fatal("unreadable external lock path allowed a global mutation")
	}
	if release, ok := d.Lock.TryAcquire("routes", "export", "netflix"); ok {
		release()
		t.Fatal("routes acquired despite uncertain external lock ownership")
	}
}

func TestExternalFenceSymlinkFailsClosed(t *testing.T) {
	root := platform.Root{Dir: filepath.Join(t.TempDir(), "broray")}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign-lock")
	if err := os.WriteFile(foreign, []byte("foreign sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, root.LauncherLock()); err != nil {
		t.Fatal(err)
	}
	a := &app.App{Root: root, Log: log.New(io.Discard, "", 0)}
	d := NewDeps(context.Background(), a)
	if !d.Lock.Busy() {
		t.Fatal("symlinked external lock path allowed a global mutation")
	}
	if release, ok := d.Lock.TryAcquire("routes", "export", "netflix"); ok {
		release()
		t.Fatal("routes acquired through an ambiguous external lock path")
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "foreign sentinel" {
		t.Fatalf("foreign lock target changed: %q, %v", got, err)
	}
}

package installer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// These fixtures exercise the actual rendered init script. The trusted shell
// binary records its invocation; Go recovery itself has separate e2e coverage.
func TestInitRecoversReinstallGap(t *testing.T) {
	for _, kind := range []string{
		"owned_gap", "current_present", "wrong_hash", "old_symlink",
		"journal_symlink", "state_symlink", "slot_symlink", "wrong_mode",
		"wrong_state", "wrong_target", "no_journal", "old_nonexecutable", "old_fifo", "state_swap_after_check",
	} {
		t.Run(kind, func(t *testing.T) {
			root, opt := ownedUninstallFixture(t)
			const target = "versions/3.5.0-test"
			slot := filepath.Join(root.Dir, target)
			current := filepath.Join(slot, "broray")
			old := current + ".old"
			stub := []byte(`#!/bin/sh
set -eu
case "$1:${2-}" in
  ctl:recover)
    /bin/busybox sh -c '
      for descriptor in 3 4 5 6 7 8; do
        if [ -e "/proc/self/fd/$descriptor" ]; then
          echo "bootstrap descriptor inherited: $descriptor" >&2
          exit 46
        fi
      done
    ' || exit 46
    printf '%s\n' recovered >> "$BRORAY_ROOT/init-events"
    if [ ! -e "$BRORAY_ROOT/current/broray" ]; then
      mv "$BRORAY_ROOT/current/broray.old" "$BRORAY_ROOT/current/broray"
    fi ;;
  start:)
    printf '%s\n' started >> "$BRORAY_ROOT/init-events" ;;
  *) exit 44 ;;
esac
`)
			if err := os.Remove(current); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(old, stub, 0o755); err != nil {
				t.Fatal(err)
			}
			j := Journal{SchemaVersion: 1, OperationID: "gap-fixture", Operation: "reinstall",
				Mode: "replace", From: "3.5.0-test", To: "3.5.0-test", Channel: "test",
				State: JSwitching, PreviousTarget: target, NewTarget: target,
				PreviousSHA256: fmt.Sprintf("%x", sha256.Sum256(stub))}
			switch kind {
			case "wrong_hash":
				j.PreviousSHA256 = strings.Repeat("0", 64)
			case "wrong_mode":
				j.Mode = "switch"
			case "wrong_state":
				j.State = JCommitted
			case "wrong_target":
				j.NewTarget = "versions/foreign"
			case "old_nonexecutable":
				if err := os.Chmod(old, 0o644); err != nil {
					t.Fatal(err)
				}
			case "old_symlink":
				external := filepath.Join(t.TempDir(), "external-binary")
				if err := os.Rename(old, external); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, old); err != nil {
					t.Fatal(err)
				}
			case "old_fifo":
				if err := os.Remove(old); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(old, 0o700); err != nil {
					t.Fatal(err)
				}
			case "current_present":
				if err := os.Rename(old, current); err != nil {
					t.Fatal(err)
				}
			}
			if err := platform.WriteJSONAtomic(root.UpdateJournal(), j, 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "state_swap_after_check" {
				outside := t.TempDir()
				unowned := []byte("#!/bin/sh\nprintf '%s\\n' unowned >> \"$BRORAY_ROOT/init-events\"\nexit 0\n")
				foreign := filepath.Join(outside, "unowned-binary")
				if err := os.WriteFile(foreign, unowned, 0o755); err != nil {
					t.Fatal(err)
				}
				forged := j
				forged.PreviousSHA256 = fmt.Sprintf("%x", sha256.Sum256(unowned))
				if err := platform.WriteJSONAtomic(filepath.Join(outside, "update-journal.json"), forged, 0o600); err != nil {
					t.Fatal(err)
				}
				// Deterministically replace the state parent after its precheck,
				// at the intervening readlink call. A forged journal must not
				// authorize the newly substituted executable.
				wrapper := fmt.Sprintf(`#!/bin/sh
set -eu
case "$1" in */current)
  if [ ! -e "$BRORAY_ROOT/attack-applied" ]; then
    mv "$BRORAY_ROOT/state" "$BRORAY_ROOT/state.original"
    ln -s %q "$BRORAY_ROOT/state"
    cp %q "$BRORAY_ROOT/current/broray.old"
    : > "$BRORAY_ROOT/attack-applied"
  fi ;;
esac
exec /bin/busybox readlink "$@"
`, outside, foreign)
				hook := filepath.Join(opt, "bin", "readlink")
				if err := os.WriteFile(hook, []byte(wrapper), 0o755); err != nil {
					t.Fatal(err)
				}
				// Standalone BusyBox shells may prefer compiled-in applets to
				// PATH entries. Inject this one dependency explicitly so the
				// same deterministic fault also runs in those emulators.
				body, err := os.ReadFile(InitScript())
				if err != nil || strings.Count(string(body), "slot=$(readlink ") != 1 {
					t.Fatalf("init readlink injection site: %v", err)
				}
				instrumented := strings.Replace(string(body), "slot=$(readlink ", "slot=$("+fmt.Sprintf("%q", hook)+" ", 1)
				if err := os.WriteFile(InitScript(), []byte(instrumented), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "no_journal":
				if err := os.Remove(root.UpdateJournal()); err != nil {
					t.Fatal(err)
				}
			case "journal_symlink":
				external := filepath.Join(t.TempDir(), "external-journal")
				if err := os.Rename(root.UpdateJournal(), external); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, root.UpdateJournal()); err != nil {
					t.Fatal(err)
				}
			case "state_symlink", "slot_symlink":
				path := root.State()
				if kind == "slot_symlink" {
					path = slot
				}
				external := filepath.Join(t.TempDir(), "external-directory")
				if err := os.Rename(path, external); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "/bin/sh", InitScript(), "start").CombinedOutput()
			events, _ := os.ReadFile(filepath.Join(root.Dir, "init-events"))
			if kind == "state_swap_after_check" {
				if _, hookErr := os.Stat(filepath.Join(root.Dir, "attack-applied")); hookErr != nil {
					t.Fatalf("parent-swap fixture hook was not reached: %v output=%s", hookErr, out)
				}
			}
			if kind == "owned_gap" || kind == "current_present" {
				if err != nil || string(events) != "recovered\nstarted\n" {
					t.Fatalf("init failed to recover/start: %v events=%q output=%s", err, events, out)
				}
			} else if err == nil || len(events) != 0 {
				t.Fatalf("unproven recovery binary executed: err=%v events=%q output=%s", err, events, out)
			}
			if ctx.Err() != nil {
				t.Fatalf("init blocked on an unsafe entry: %s", out)
			}
		})
	}
}

func TestInstallAcceptsExactDev7Init(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	body := strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev7InitTemplateFixture)
	if err := os.WriteFile(InitScript(), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, "3.5.0-test2", false); err != nil {
		t.Fatalf("exact dev7 init refused: %v", err)
	}
}

func TestInstallRejectsModifiedDev7Init(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	body := strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev7InitTemplateFixture) + "\n# foreign modification\n"
	if err := os.WriteFile(InitScript(), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, "3.5.0-test2", false); err == nil {
		t.Fatal("modified dev7 init accepted")
	}
	got, err := os.ReadFile(InitScript())
	if err != nil || string(got) != body {
		t.Fatal("foreign init changed")
	}
}

const dev7InitTemplateFixture = `#!/bin/sh
# BROray 3.5.0 daemon. Stop goes through the control socket and waits for the
# ownership lock; nothing here kills by PID.
BRORAY=@ROOT@/current/broray
BRORAY_ROOT=@ROOT@
PATH=@OPT@/sbin:@OPT@/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH BRORAY_ROOT
case "$1" in
  start)
    # Finish or roll back an update interrupted by a power loss (journal
    # in state/), so exactly one version is current; then start it. The
    # binary detaches itself (no setsid/start-stop-daemon needed).
    "$BRORAY" ctl recover || echo "BROray: recover failed; starting the current version" >&2
    "$BRORAY" start ;;
  stop)
    "$BRORAY" ctl stop ;;
  restart)
    "$0" stop; "$0" start ;;
  status)
    "$BRORAY" ctl ping ;;
  check)
    "$BRORAY" ctl ping >/dev/null 2>&1 && echo alive || echo dead ;;
  *)
    echo "Usage: $0 {start|stop|restart|status}" >&2; exit 1 ;;
esac
`

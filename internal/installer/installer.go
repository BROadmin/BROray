// Package installer performs the clean install, status and uninstall of
// BROray 3.5.0 on Entware. It never reboots and never touches foreign
// Keenetic objects; everything it removes is listed by receipts.
package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
	"github.com/BROadmin/broray/modules/routes"
	"github.com/BROadmin/broray/modules/xray"
)

// Opt is the Entware prefix; BRORAY_OPT overrides it for host tests only.
var Opt = func() string {
	if v := os.Getenv("BRORAY_OPT"); v != "" {
		return v
	}
	return "/opt"
}()

// InitScript and BinLink are derived from Opt at call time so tests can
// point Opt at a fixture prefix.
func InitScript() string { return Opt + "/etc/init.d/S24broray" }
func BinLink() string    { return Opt + "/bin/broray" }

type Report struct {
	Steps    []string `json:"steps"`
	Warnings []string `json:"warnings"`
}

// Progress, when set, receives every step and warning as it happens (UTC
// timestamped), so an interrupted uninstall still leaves a trace.
var Progress io.Writer

func (r *Report) step(format string, a ...any) {
	m := fmt.Sprintf(format, a...)
	r.Steps = append(r.Steps, m)
	progress("step", m)
}
func (r *Report) warning(format string, a ...any) {
	m := fmt.Sprintf(format, a...)
	r.Warnings = append(r.Warnings, m)
	progress("warning", m)
}

func progress(kind, m string) {
	if Progress != nil {
		fmt.Fprintf(Progress, "%s %s %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), kind, m)
	}
}

// Preflight checks the target before anything is written.
func Preflight(root platform.Root, version string) (Report, error) {
	var rep Report
	if os.Geteuid() != 0 {
		return rep, errors.New("INSTALL_ROOT_REQUIRED: run as root")
	}
	if runtime.GOOS != "linux" {
		return rep, errors.New("INSTALL_LINUX_REQUIRED")
	}
	if _, err := os.Stat(Opt + "/etc/init.d"); err != nil {
		return rep, errors.New("INSTALL_ENTWARE_REQUIRED: " + Opt + "/etc/init.d not found")
	}
	if err := preflightInstallDirs(root); err != nil {
		return rep, err
	}
	asset, err := xray.CurrentAsset()
	if err != nil {
		return rep, err
	}
	if arch := entwareArch(); arch != "" && !strings.HasPrefix(asset.Entware, strings.Split(arch, "-")[0]) {
		return rep, fmt.Errorf("INSTALL_ARCH_MISMATCH: Entware %s, binary built for %s", arch, asset.Entware)
	}
	rep.step("architecture: %s (binary %s/%s)", asset.Entware, runtime.GOOS, runtime.GOARCH)
	for _, p := range []string{Opt + "/etc/init.d/S22broray-updater", Opt + "/bin/broray-system", Opt + "/libexec/broray-updater", filepath.Join(root.Dir, "current", "app")} {
		if _, err := os.Lstat(p); err == nil {
			return rep, fmt.Errorf("INSTALL_LEGACY_PRESENT: %s exists — BROray 3.x is installed; remove it first (migrate-legacy is not part of dev1)", p)
		}
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(Opt, &st); err == nil {
		free := st.Bavail * uint64(st.Bsize)
		rep.step("free space in /opt: %d MiB", free>>20)
		if free < 60<<20 {
			return rep, fmt.Errorf("INSTALL_SPACE_REQUIRED: %d MiB free in /opt, need 60 MiB (BROray + Xray + download)", free>>20)
		}
	}
	return rep, nil
}

func entwareArch() string {
	b, err := os.ReadFile(Opt + "/etc/opkg.conf")
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "arch" && f[1] != "all" {
			return f[1]
		}
	}
	return ""
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func preflightInstallDirs(root platform.Root) error {
	paths := make([]string, 0, len(root.Dirs()))
	for p := range root.Dirs() {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("INSTALL_DIRECTORY_UNOWNED: %s is not a directory", p)
		}
	}
	return nil
}

const initScriptTemplate = `#!/bin/sh
# BROray 3.5.0 daemon. Stop goes through the control socket and waits for the
# ownership lock; nothing here kills by PID.
BRORAY=@ROOT@/current/broray
BRORAY_ROOT=@ROOT@
PATH=@OPT@/sbin:@OPT@/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH BRORAY_ROOT

# During a journalled reinstall, current/broray may be temporarily absent.
# Only the exact previous executable attested by that replace journal may
# bootstrap recovery. Pin the file descriptor through hashing and execution.
journal_field() {
  sed -n 's/^  "'"$1"'": "\([^"]*\)",\{0,1\}$/\1/p' /proc/self/fd/4
}
select_recovery() {
  RECOVERY=$BRORAY
  RECOVERY_FALLBACK=0
  [ -x "$BRORAY" ] && return 0
  [ ! -e "$BRORAY" ] && [ ! -L "$BRORAY" ] || return 1
  for directory in "$BRORAY_ROOT" "$BRORAY_ROOT/versions" "$BRORAY_ROOT/state"; do
    [ -d "$directory" ] && [ ! -L "$directory" ] || return 1
  done
  exec 5<"$BRORAY_ROOT" || return 1
  [ "$BRORAY_ROOT" -ef /proc/self/fd/5 ] && [ ! -L "$BRORAY_ROOT" ] || return 1
  state_dir=/proc/self/fd/5/state
  versions_dir=/proc/self/fd/5/versions
  [ -d "$state_dir" ] && [ ! -L "$state_dir" ] || return 1
  exec 6<"$state_dir" || return 1
  [ "$state_dir" -ef /proc/self/fd/6 ] && [ ! -L "$state_dir" ] || return 1
  [ -d "$versions_dir" ] && [ ! -L "$versions_dir" ] || return 1
  exec 7<"$versions_dir" || return 1
  [ "$versions_dir" -ef /proc/self/fd/7 ] && [ ! -L "$versions_dir" ] || return 1
  [ -L /proc/self/fd/5/current ] || return 1
  slot=$(readlink /proc/self/fd/5/current) || return 1
  case "$slot" in versions/*) version=${slot#versions/} ;; *) return 1 ;; esac
  case "$version" in ''|*[!0-9A-Za-z.-]*) return 1 ;; esac
  case "$version" in [0-9]*) ;; *) return 1 ;; esac
  slot_dir=/proc/self/fd/7/$version
  [ -d "$slot_dir" ] && [ ! -L "$slot_dir" ] || return 1
  exec 8<"$slot_dir" || return 1
  [ "$slot_dir" -ef /proc/self/fd/8 ] && [ ! -L "$slot_dir" ] || return 1
  journal=/proc/self/fd/6/update-journal.json
  [ -f "$journal" ] && [ ! -L "$journal" ] || return 1
  exec 4<"$journal" || return 1
  schema=$(sed -n 's/^  "schemaVersion": \([0-9]*\),$/\1/p' /proc/self/fd/4)
  [ "$schema" = 1 ] || return 1
  [ "$(journal_field mode)" = replace ] || return 1
  [ "$(journal_field state)" = switching ] || return 1
  [ "$(journal_field from)" = "$version" ] || return 1
  [ "$(journal_field to)" = "$version" ] || return 1
  [ "$(journal_field previousTarget)" = "$slot" ] || return 1
  [ "$(journal_field newTarget)" = "$slot" ] || return 1
  previous_sha=$(journal_field previousSha256)
  [ "${#previous_sha}" -eq 64 ] || return 1
  case "$previous_sha" in *[!0-9a-f]*) return 1 ;; esac
  previous=/proc/self/fd/8/broray.old
  [ -f "$previous" ] && [ ! -L "$previous" ] && [ -x "$previous" ] || return 1
  exec 3<"$previous" || return 1
  actual=$(sha256sum /proc/self/fd/3) || return 1
  actual=${actual%% *}
  [ "$actual" = "$previous_sha" ] || return 1
  # The Go recovery command uses the active installation paths. Refuse a
  # parent or file replaced since selection, even when held FDs remain valid.
  [ "$BRORAY_ROOT" -ef /proc/self/fd/5 ] && [ ! -L "$BRORAY_ROOT" ] || return 1
  [ "$BRORAY_ROOT/state" -ef /proc/self/fd/6 ] && [ ! -L "$BRORAY_ROOT/state" ] || return 1
  [ "$BRORAY_ROOT/versions" -ef /proc/self/fd/7 ] && [ ! -L "$BRORAY_ROOT/versions" ] || return 1
  [ "$BRORAY_ROOT/$slot" -ef /proc/self/fd/8 ] && [ ! -L "$BRORAY_ROOT/$slot" ] || return 1
  [ -L "$BRORAY_ROOT/current" ] && [ "$BRORAY_ROOT/current" -ef /proc/self/fd/8 ] || return 1
  [ "$BRORAY_ROOT/state/update-journal.json" -ef /proc/self/fd/4 ] &&
    [ ! -L "$BRORAY_ROOT/state/update-journal.json" ] || return 1
  [ "$BRORAY_ROOT/$slot/broray.old" -ef /proc/self/fd/3 ] &&
    [ ! -L "$BRORAY_ROOT/$slot/broray.old" ] || return 1
  RECOVERY=/proc/$$/fd/3
  RECOVERY_FALLBACK=1
}

case "$1" in
  start)
    # Finish or roll back an update interrupted by a power loss (journal
    # in state/), so exactly one version is current; then start it. The
    # binary detaches itself (no setsid/start-stop-daemon needed).
    if ! select_recovery; then
      echo "BROray: no attested recovery executable; installation left untouched" >&2
      exit 1
    fi
    # Keep the attested FD in this shell, without leaking bootstrap FDs
    # into a recovery command that may restart the daemon.
    if ! (exec 3<&- 4<&- 5<&- 6<&- 7<&- 8<&-; exec "$RECOVERY" ctl recover); then
      echo "BROray: recover failed; starting the current version" >&2
      [ "$RECOVERY_FALLBACK" = 0 ] || exit 1
    fi
    exec 3<&- 4<&- 5<&- 6<&- 7<&- 8<&-
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

func renderInitScript(root platform.Root) string {
	return strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(initScriptTemplate)
}

// Install copies the running binary into versions/<version>, points current
// at it, writes /opt/bin/broray and S24broray, seeds the config and starts
// the daemon. An existing target version is left for the updater's
// journalled reinstall path; an installer invocation only stages a new one.
func Install(root platform.Root, version string, start bool) (Report, error) {
	rep, err := Preflight(root, version)
	if err != nil {
		return rep, err
	}
	self, err := os.Executable()
	if err != nil {
		return rep, err
	}
	if err := preflightInstallPaths(root, version); err != nil {
		return rep, err
	}
	_, currentErr := os.Lstat(root.Current())
	freshInstall := errors.Is(currentErr, os.ErrNotExist)
	newConfig := false
	if err := root.EnsureDirs(); err != nil {
		return rep, err
	}
	verDir := filepath.Join(root.Versions(), version)
	if err := os.Mkdir(verDir, 0o755); err != nil {
		return rep, err
	}
	bin := filepath.Join(verDir, "broray")
	if err := copyFile(self, bin+".new", 0o755); err != nil {
		return rep, err
	}
	if err := os.Rename(bin+".new", bin); err != nil {
		return rep, err
	}
	rep.step("binary: %s", bin)
	// current -> versions/<version> (atomic symlink swap)
	tmpLink := root.Current() + ".new"
	if err := os.Symlink(filepath.Join("versions", version), tmpLink); err != nil {
		return rep, err
	}
	if err := os.Rename(tmpLink, root.Current()); err != nil {
		return rep, err
	}
	_ = platform.SyncDir(root.Dir)
	rep.step("current -> versions/%s", version)
	if err := os.MkdirAll(filepath.Dir(BinLink()), 0o755); err != nil {
		return rep, err
	}
	if err := removeOwnedBinLink(root); err != nil {
		return rep, err
	}
	if err := os.Symlink(filepath.Join(root.Current(), "broray"), BinLink()); err != nil {
		return rep, err
	}
	rep.step("%s -> %s", BinLink(), filepath.Join(root.Current(), "broray"))
	if _, err := os.Stat(root.ConfigFile()); errors.Is(err, os.ErrNotExist) {
		if err := platform.SaveConfig(root, platform.DefaultConfig()); err != nil {
			return rep, err
		}
		newConfig = true
		rep.step("config: %s (defaults)", root.ConfigFile())
	} else if err != nil {
		return rep, err
	} else {
		rep.step("config: %s (kept)", root.ConfigFile())
	}
	initScript := renderInitScript(root)
	if err := os.MkdirAll(filepath.Dir(InitScript()), 0o755); err != nil {
		return rep, err
	}
	if err := os.MkdirAll(filepath.Dir(BinLink()), 0o755); err != nil {
		return rep, err
	}
	if err := platform.WriteFileAtomic(InitScript(), []byte(initScript), 0o755); err != nil {
		return rep, err
	}
	rep.step("init: %s", InitScript())
	if start {
		out, err := exec.Command(InitScript(), "start").CombinedOutput()
		if err != nil {
			return rep, fmt.Errorf("INSTALL_START_FAILED: %v: %s", err, strings.TrimSpace(string(out)))
		}
		rep.step("daemon: %s", strings.TrimSpace(string(out)))
	}
	if freshInstall && newConfig {
		if start {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			publishInitialWebUI(ctx, root, &rep)
		} else {
			rep.warning("WEB_ACCESS_DEFERRED: daemon not started; after starting BROray run: %s ctl post /api/keenetic/web-access-enable.cgi '{}'", BinLink())
		}
	}
	return rep, nil
}

func preflightInstallPaths(root platform.Root, version string) error {
	if !update.ValidVersion(version) {
		return fmt.Errorf("INSTALL_INVALID_VERSION: %q", version)
	}
	for _, p := range []string{filepath.Join(root.Versions(), version), root.Current() + ".new"} {
		if _, err := os.Lstat(p); err == nil {
			return fmt.Errorf("INSTALL_TARGET_EXISTS: %s", p)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if target, err := os.Readlink(root.Current()); err == nil {
		prefix := "versions" + string(os.PathSeparator)
		if !strings.HasPrefix(target, prefix) || !update.ValidVersion(strings.TrimPrefix(target, prefix)) {
			return fmt.Errorf("INSTALL_CURRENT_UNOWNED: %q", target)
		}
		info, err := os.Lstat(filepath.Join(root.Dir, target, "broray"))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("INSTALL_CURRENT_UNOWNED: %q", target)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("INSTALL_CURRENT_UNOWNED: %w", err)
	}
	if err := checkOwnedBinLink(root); err != nil {
		return err
	}
	if err := checkOwnedUninstallInit(root); err != nil {
		return fmt.Errorf("INSTALL_INIT_UNOWNED: %s", InitScript())
	}
	return nil
}

func checkOwnedBinLink(root platform.Root) error {
	target, err := os.Readlink(BinLink())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || target != filepath.Join(root.Current(), "broray") {
		return fmt.Errorf("INSTALL_BIN_UNOWNED: %s", BinLink())
	}
	return nil
}

func removeOwnedBinLink(root platform.Root) error {
	if err := checkOwnedBinLink(root); err != nil {
		return err
	}
	if err := os.Remove(BinLink()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Status reports what is on disk and whether the daemon holds its lock.
func Status(root platform.Root) map[string]any {
	out := map[string]any{"root": root.Dir}
	if target, err := os.Readlink(root.Current()); err == nil {
		out["current"] = target
	}
	out["daemonRunning"] = platform.Held(root.DaemonLock())
	if _, err := os.Stat(InitScript()); err == nil {
		out["initScript"] = InitScript()
	}
	if fi, err := os.Stat(root.XrayBinary()); err == nil {
		out["xrayBytes"] = fi.Size()
	}
	out["entwareArch"] = entwareArch()
	return out
}

// Uninstall stops the daemon, removes every owned Keenetic object by its
// receipt, deletes the kernel server route, and removes files. With keepData
// the data directory is moved to /opt/broray-preserved/<timestamp>.
//
// AreaCleanup, when set (cmd/broray sets it), removes the Keenetic objects
// the WebUI areas own (3.x-format receipts) before the installer's own
// receipts; it runs after the daemon has stopped.
var AreaCleanup func(ctx context.Context, root platform.Root) error

func Uninstall(ctx context.Context, root platform.Root, keepData bool) (rep Report, err error) {
	if os.Geteuid() != 0 {
		return rep, errors.New("UNINSTALL_ROOT_REQUIRED")
	}
	if err := preflightUninstallPaths(root, keepData); err != nil {
		return rep, err
	}
	stopped := false
	removed := false
	defer func() {
		// Before root removal, a failed uninstall starts the old daemon again.
		// A post-removal purge failure is reported without trying to start a
		// binary that no longer exists.
		if err != nil && stopped && !removed {
			if serr := restartDaemon(root); serr != nil {
				rep.warning("daemon restart: %v", serr)
			} else {
				rep.step("daemon restarted after failed uninstall")
			}
		}
	}()
	held, lockErr := platform.HeldChecked(root.DaemonLock())
	if lockErr != nil {
		return rep, fmt.Errorf("UNINSTALL_DAEMON_LOCK_UNVERIFIED: %w", lockErr)
	}
	if held {
		bin := filepath.Join(root.Current(), "broray")
		if out, err := exec.Command(bin, "ctl", "stop").CombinedOutput(); err != nil {
			rep.warning("daemon stop: %v: %s", err, strings.TrimSpace(string(out)))
		} else {
			rep.step("daemon stopped")
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			held, lockErr = platform.HeldChecked(root.DaemonLock())
			if lockErr != nil {
				return rep, fmt.Errorf("UNINSTALL_DAEMON_LOCK_UNVERIFIED: %w", lockErr)
			}
			if !held {
				break
			}
			if !time.Now().Before(deadline) {
				return rep, errors.New("UNINSTALL_DAEMON_STILL_RUNNING")
			}
			time.Sleep(300 * time.Millisecond)
		}
		stopped = true
	}
	cfg, err := platform.LoadConfig(root)
	if err != nil {
		rep.warning("config: %v (defaults used)", err)
		cfg = platform.DefaultConfig()
	}
	t := keenetic.NewTransport(cfg.Keenetic.WriteTransport, cfg.Keenetic.RCIURL)
	k := keenetic.NewAdapter(t, root.Receipts(), cfg.Keenetic.RouteMetric)
	if AreaCleanup != nil {
		if err := AreaCleanup(ctx, root); err != nil {
			return rep, fmt.Errorf("UNINSTALL_KEENETIC_CLEANUP_INCOMPLETE: %v; nothing was deleted, run uninstall again", err)
		}
		rep.step("area Keenetic objects removed")
	}
	if err := CleanupKeenetic(ctx, root, cfg, k, &rep); err != nil {
		// Receipts, data and binaries stay in place so the uninstall can be
		// repeated once KeeneticOS answers again. Nothing has been deleted.
		return rep, err
	}
	if err := preflightUninstallPaths(root, keepData); err != nil {
		return rep, err
	}
	xray.RemoveServerRoute(ctx, root)
	if keepData {
		dst, err := preserveData(root, time.Now())
		if err != nil {
			return rep, err
		}
		rep.step("data preserved: %s", dst)
	}
	if err := preflightUninstallPaths(root, keepData); err != nil {
		return rep, err
	}
	if err := removeOwnedBinLink(root); err != nil {
		return rep, fmt.Errorf("UNINSTALL_BIN_UNOWNED: %w", err)
	}
	rep.step("removed owned %s", BinLink())
	if err := checkOwnedUninstallInit(root); err != nil {
		return rep, err
	}
	if err := os.Remove(InitScript()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return rep, err
	}
	rep.step("removed owned %s", InitScript())
	if err := os.RemoveAll(root.Dir); err != nil {
		return rep, err
	}
	removed = true
	rep.step("removed %s", root.Dir)
	if !keepData {
		if err := purgeOwnedPreserved(); err != nil {
			return rep, fmt.Errorf("UNINSTALL_PRESERVED_PURGE_INCOMPLETE: BROray removed, preserved copies remain or removal failed: %w", err)
		}
		rep.step("removed owned preserved copies")
	}
	return rep, nil
}

// CleanupKeenetic removes every owned Keenetic object by its receipt. Any
// failure is returned: the caller must not delete receipts afterwards.
func CleanupKeenetic(ctx context.Context, root platform.Root, cfg platform.Config, k *keenetic.Adapter, rep *Report) error {
	var failed []string
	rs := routes.NewStore(root, cfg.Routes.Repository, cfg.Routes.Commit)
	in := rs.LoadInstalled()
	if in.Interface != "" && len(in.Bundles) > 0 {
		var all []routes.Route
		seen := map[string]bool{}
		for _, list := range in.Bundles {
			for _, r := range list {
				if !seen[r.Key()] {
					seen[r.Key()] = true
					all = append(all, r)
				}
			}
		}
		if err := k.RouteBatch(ctx, in.Interface, nil, all, 100, nil); err != nil {
			failed = append(failed, "routes: "+err.Error())
		} else {
			rep.step("routes removed: %d", len(all))
			in.Bundles = map[string][]routes.Route{}
			if err := rs.SaveInstalled(in); err != nil {
				failed = append(failed, "routes receipt: "+err.Error())
			}
		}
	}
	if rec := k.LoadDoT(); len(rec.Managed) > 0 {
		if _, err := k.ApplyDoT(ctx, nil); err != nil {
			failed = append(failed, "dot: "+err.Error())
		} else {
			rep.step("DoT upstreams removed: %d", len(rec.Managed))
		}
	}
	if _, ok := k.LoadWeb(); ok {
		if err := k.DisableWeb(ctx); err != nil {
			failed = append(failed, "web: "+err.Error())
		} else {
			rep.step("KeenDNS app removed")
		}
	}
	if rec, ok := k.LoadInterface(); ok {
		if len(failed) > 0 {
			// Routes still point at the interface: keep it.
			failed = append(failed, "interface "+rec.InterfaceName+": kept because earlier steps failed")
		} else if err := k.DeleteInterface(ctx); err != nil {
			failed = append(failed, "interface: "+err.Error())
		} else {
			rep.step("interface %s removed", rec.InterfaceName)
		}
	}
	if len(failed) > 0 {
		for _, f := range failed {
			rep.warning("%s", f)
		}
		return fmt.Errorf("UNINSTALL_KEENETIC_CLEANUP_INCOMPLETE: %s; nothing was deleted, run uninstall again", strings.Join(failed, "; "))
	}
	return nil
}

// preserveData moves data/ and copies config.v1.json into
// <Opt>/broray-preserved/<timestamp>/ and verifies the result. On any error
// the original data is put back and the error is returned, so the caller
// never reaches the destructive step without a confirmed copy.
func preserveData(root platform.Root, now time.Time) (string, error) {
	if st, err := os.Lstat(root.Data()); err == nil {
		if !st.IsDir() {
			return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: data is not a real directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: data: %w", err)
	}
	parent := filepath.Join(Opt, "broray-preserved")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: create %s: %v", parent, err)
	}
	if st, err := os.Lstat(parent); err != nil || !st.IsDir() {
		return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: %s is not a directory", parent)
	}
	dst := filepath.Join(parent, now.UTC().Format("20060102-150405"))
	if _, err := os.Lstat(dst); err == nil {
		return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: %s already exists", dst)
	}
	if err := os.Mkdir(dst, 0o700); err != nil {
		return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: create %s: %v", dst, err)
	}
	if err := platform.WriteFileAtomic(filepath.Join(dst, "owner.json"), []byte(`{"schemaVersion":1,"owner":"BROray"}`), 0o600); err != nil {
		_ = os.RemoveAll(dst)
		return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: owner receipt: %w", err)
	}
	if b, err := os.ReadFile(root.ConfigFile()); err == nil {
		if err := platform.WriteFileAtomic(filepath.Join(dst, "config.v1.json"), b, 0o600); err != nil {
			_ = os.RemoveAll(dst)
			return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: config: %v", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.RemoveAll(dst)
		return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: read config: %v", err)
	}
	if _, err := os.Lstat(root.Data()); err == nil {
		if err := os.Rename(root.Data(), filepath.Join(dst, "data")); err != nil {
			_ = os.RemoveAll(dst)
			return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: move data: %v", err)
		}
		if st, err := os.Lstat(filepath.Join(dst, "data")); err != nil || !st.IsDir() {
			if restoreErr := os.Rename(filepath.Join(dst, "data"), root.Data()); restoreErr != nil {
				return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: data verification failed; restore from %s failed: %w", dst, restoreErr)
			}
			_ = os.RemoveAll(dst)
			return "", fmt.Errorf("UNINSTALL_PRESERVE_FAILED: data not found in %s after move", dst)
		}
	}
	_ = platform.SyncDir(dst)
	_ = platform.SyncDir(parent)
	return dst, nil
}

// restartDaemon starts the daemon again through its init script (or the
// binary directly when the init script is missing).
func restartDaemon(root platform.Root) error {
	if _, err := os.Stat(InitScript()); err == nil {
		return exec.Command(InitScript(), "start").Run()
	}
	cmd := exec.Command(filepath.Join(root.Current(), "broray"), "daemon")
	cmd.SysProcAttr = detachAttr()
	return cmd.Start()
}

func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

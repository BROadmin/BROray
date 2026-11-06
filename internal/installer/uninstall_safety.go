package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

// preflightUninstallPaths proves ownership before Keenetic cleanup or local deletion.
// An interrupted or ambiguous layout must be recovered, not recursively removed.
func preflightUninstallPaths(root platform.Root, keepData bool) error {
	st, err := os.Lstat(root.Dir)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: %s is not a real directory", root.Dir)
	}
	if err := preflightInstallDirs(root); err != nil {
		return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: %w", err)
	}
	allowed := map[string]bool{"current": true, "launcher": true}
	for p := range root.Dirs() {
		if filepath.Dir(p) == root.Dir {
			allowed[filepath.Base(p)] = true
		}
	}
	children, err := os.ReadDir(root.Dir)
	if err != nil {
		return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: read root: %w", err)
	}
	for _, ent := range children {
		if !allowed[ent.Name()] {
			return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: unknown entry %s", ent.Name())
		}
	}
	if st, err := os.Lstat(root.Launcher()); err == nil {
		if !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: launcher is not a regular executable")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: launcher: %w", err)
	}
	target, err := os.Readlink(root.Current())
	if err != nil {
		return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: current link: %w", err)
	}
	version := strings.TrimPrefix(target, "versions"+string(os.PathSeparator))
	if !update.ValidVersion(version) || target != filepath.Join("versions", version) {
		return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: current link target %q", target)
	}
	versions, err := os.ReadDir(root.Versions())
	if err != nil {
		return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: versions: %w", err)
	}
	found := false
	for _, ent := range versions {
		if !ownedVersionDir(root, ent) {
			return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: unverified version %s", ent.Name())
		}
		if ent.Name() == version {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("UNINSTALL_ROOT_UNOWNED: current version %s missing", version)
	}
	if err := checkOwnedUninstallInit(root); err != nil {
		return err
	}
	if err := checkOwnedBinLink(root); err != nil {
		return fmt.Errorf("UNINSTALL_BIN_UNOWNED: %w", err)
	}
	if !keepData {
		if _, err := validatePreservedForPurge(filepath.Join(Opt, "broray-preserved")); err != nil {
			return err
		}
	}
	return nil
}

func checkOwnedUninstallInit(root platform.Root) error {
	st, err := os.Lstat(InitScript())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("UNINSTALL_INIT_UNOWNED: %s", InitScript())
	}
	b, err := os.ReadFile(InitScript())
	if err != nil || !isOwnedInitTemplate(root, b) {
		return fmt.Errorf("UNINSTALL_INIT_UNOWNED: %s", InitScript())
	}
	return nil
}

// Installation and journal recovery must accept the same exact legacy bytes.
// A header or partial signature does not establish ownership.
func isOwnedInitTemplate(root platform.Root, b []byte) bool {
	if string(b) == renderInitScript(root) {
		return true
	}
	for _, template := range []string{dev5InitScriptTemplate, dev7InitScriptTemplate} {
		legacy := strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(template)
		if string(b) == legacy {
			return true
		}
	}
	return false
}

// Exact init template from the dev5 source archive. A mixed-version update
// can leave this file in place, so it remains an explicitly owned layout.
const dev5InitScriptTemplate = `#!/bin/sh
# BROray 3.5.0 daemon. Stop goes through the control socket and waits for the
# ownership lock; nothing here kills by PID.
BRORAY=@ROOT@/current/broray
PATH=@OPT@/sbin:@OPT@/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
case "$1" in
  start)
    # The binary detaches itself (no setsid/start-stop-daemon needed).
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

type preservedOwner struct {
	SchemaVersion int    `json:"schemaVersion"`
	Owner         string `json:"owner"`
}

// validatePreservedForPurge accepts only directories bearing BROray's receipt.
// Old unmarked copies remain available for manual review instead of being guessed
// from a timestamp.
func validatePreservedForPurge(parent string) (bool, error) {
	st, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !st.IsDir() {
		return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: %s is not a real directory", parent)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: read %s: %w", parent, err)
	}
	for _, ent := range entries {
		name := ent.Name()
		when, err := time.Parse("20060102-150405", name)
		if err != nil || when.Format("20060102-150405") != name || !ent.IsDir() {
			return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: unknown entry %s", name)
		}
		dir := filepath.Join(parent, name)
		marker := filepath.Join(dir, "owner.json")
		markerStat, err := os.Lstat(marker)
		if err != nil || !markerStat.Mode().IsRegular() {
			return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: missing owner receipt in %s", name)
		}
		b, err := os.ReadFile(marker)
		var owner preservedOwner
		if err != nil || json.Unmarshal(b, &owner) != nil || owner.SchemaVersion != 1 || owner.Owner != "BROray" {
			return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: invalid owner receipt in %s", name)
		}
		contents, err := os.ReadDir(dir)
		if err != nil {
			return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: read %s: %w", name, err)
		}
		for _, child := range contents {
			switch child.Name() {
			case "owner.json":
				if !child.Type().IsRegular() {
					return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: owner receipt type in %s", name)
				}
			case "config.v1.json":
				if !child.Type().IsRegular() {
					return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: config type in %s", name)
				}
			case "data":
				if !child.IsDir() {
					return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: data type in %s", name)
				}
			default:
				return false, fmt.Errorf("UNINSTALL_PRESERVED_UNOWNED: unknown entry %s/%s", name, child.Name())
			}
		}
	}
	return true, nil
}

func purgeOwnedPreserved() error {
	parent := filepath.Join(Opt, "broray-preserved")
	exists, err := validatePreservedForPurge(parent)
	if err != nil || !exists {
		return err
	}
	return os.RemoveAll(parent)
}

// Exact dev6/dev7 template remains owned after the bootstrap recovery fix.
const dev7InitScriptTemplate = `#!/bin/sh
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

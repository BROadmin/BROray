package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

func ownedUninstallFixture(t *testing.T) (platform.Root, string) {
	t.Helper()
	root, opt := fixture(t)
	version := filepath.Join(root.Versions(), "3.5.0-test")
	if err := os.MkdirAll(version, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(version, "broray"), []byte("owned binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("versions", "3.5.0-test"), root.Current()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(BinLink()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root.Current(), "broray"), BinLink()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(InitScript(), []byte(renderInitScript(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, opt
}

func TestUninstallRejectsForeignInitBeforeCleanup(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	if err := os.WriteFile(InitScript(), []byte("foreign init"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Uninstall(context.Background(), root, false)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_INIT_UNOWNED") {
		t.Fatalf("want ownership refusal, got %v", err)
	}
	if b, err := os.ReadFile(InitScript()); err != nil || string(b) != "foreign init" {
		t.Fatalf("foreign init changed: %q %v", b, err)
	}
	if _, err := os.Stat(root.Data()); err != nil {
		t.Fatalf("data changed after refusal: %v", err)
	}
}

func TestUninstallRejectsModifiedInitWithOwnedHeader(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	modified := renderInitScript(root) + "\n# Foreign modification\n"
	if err := os.WriteFile(InitScript(), []byte(modified), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Uninstall(context.Background(), root, false)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_INIT_UNOWNED") {
		t.Fatalf("modified init accepted: %v", err)
	}
	if b, err := os.ReadFile(InitScript()); err != nil || string(b) != modified {
		t.Fatalf("modified init changed: %q %v", b, err)
	}
	if _, err := os.Stat(root.Data()); err != nil {
		t.Fatalf("data changed after refusal: %v", err)
	}
}

// This is the exact init template from the dev5 source archive. An update may
// encounter it before the new init script is installed.
const dev5InitTemplateFixture = `#!/bin/sh
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

func TestUninstallAcceptsExactDev5InitScript(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	init := strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev5InitTemplateFixture)
	if err := os.WriteFile(InitScript(), []byte(init), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(context.Background(), root, false); err != nil {
		t.Fatalf("exact dev5 init refused: %v", err)
	}
	if _, err := os.Lstat(root.Dir); !os.IsNotExist(err) {
		t.Fatalf("root remains after exact dev5 uninstall: %v", err)
	}
}

func TestUninstallRejectsModifiedDev5InitScript(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	modified := strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev5InitTemplateFixture) + "\n# Foreign modification\n"
	if err := os.WriteFile(InitScript(), []byte(modified), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Uninstall(context.Background(), root, false)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_INIT_UNOWNED") {
		t.Fatalf("modified dev5 init accepted: %v", err)
	}
	if b, err := os.ReadFile(InitScript()); err != nil || string(b) != modified {
		t.Fatalf("modified dev5 init changed: %q %v", b, err)
	}
}

func TestUninstallRejectsForeignBinBeforeCleanup(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	if err := os.Remove(BinLink()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BinLink(), []byte("foreign bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Uninstall(context.Background(), root, false)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_BIN_UNOWNED") {
		t.Fatalf("want ownership refusal, got %v", err)
	}
	if b, err := os.ReadFile(BinLink()); err != nil || string(b) != "foreign bin" {
		t.Fatalf("foreign bin changed: %q %v", b, err)
	}
	if _, err := os.Stat(root.Data()); err != nil {
		t.Fatalf("data changed after refusal: %v", err)
	}
}

func TestUninstallRejectsUnknownRootEntryBeforeCleanup(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	foreign := filepath.Join(root.Dir, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Uninstall(context.Background(), root, false)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_ROOT_UNOWNED") {
		t.Fatalf("want ownership refusal, got %v", err)
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "foreign" {
		t.Fatalf("foreign root entry changed: %q %v", b, err)
	}
}

func TestPreserveDataRejectsSymlinkWithoutMovingIt(t *testing.T) {
	root, opt := fixture(t)
	external := filepath.Join(opt, "external-data")
	if err := os.Rename(root.Data(), external); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, root.Data()); err != nil {
		t.Fatal(err)
	}
	_, err := preserveData(root, time.Unix(1762128000, 0))
	if err == nil {
		t.Fatal("symlink data accepted")
	}
	st, err := os.Lstat(root.Data())
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("data symlink moved on failure: %v %v", st, err)
	}
	if b, err := os.ReadFile(filepath.Join(external, "owner-data.txt")); err != nil || string(b) != "SENTINEL" {
		t.Fatalf("external data changed: %q %v", b, err)
	}
}

func TestUninstallFullPurgesOwnedPreservedAfterRemoval(t *testing.T) {
	root, opt := ownedUninstallFixture(t)
	parent := filepath.Join(opt, "broray-preserved")
	dst := filepath.Join(parent, "20261103-000000")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "owner.json"), []byte(`{"schemaVersion":1,"owner":"BROray"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "config.v1.json"), []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(context.Background(), root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Fatalf("owned preserved copies remain after full removal: %v", err)
	}
}

func TestUninstallFullRejectsForeignPreservedBeforeCleanup(t *testing.T) {
	root, opt := ownedUninstallFixture(t)
	parent := filepath.Join(opt, "broray-preserved")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(parent, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Uninstall(context.Background(), root, false)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_PRESERVED_UNOWNED") {
		t.Fatalf("want preserved ownership refusal, got %v", err)
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "foreign" {
		t.Fatalf("foreign preserved entry changed: %q %v", b, err)
	}
	if _, err := os.Stat(root.Data()); err != nil {
		t.Fatalf("app data changed before refusal: %v", err)
	}
}

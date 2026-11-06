package installer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func TestUninstallRejectsAmbiguousDaemonLockBeforeCleanup(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	if err := os.Mkdir(root.DaemonLock(), 0o700); err != nil {
		t.Fatal(err)
	}

	previousCleanup := AreaCleanup
	cleanupCalled := false
	AreaCleanup = func(context.Context, platform.Root) error {
		cleanupCalled = true
		return errors.New("cleanup must not run")
	}
	t.Cleanup(func() { AreaCleanup = previousCleanup })

	_, err := Uninstall(context.Background(), root, true)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_DAEMON_LOCK_UNVERIFIED") {
		t.Fatalf("ambiguous daemon lock must block uninstall before cleanup, got %v", err)
	}
	if cleanupCalled {
		t.Fatal("Keenetic area cleanup ran with an ambiguous daemon lock")
	}
	if _, err := os.Stat(root.Data()); err != nil {
		t.Fatalf("data changed after refusal: %v", err)
	}
	if _, err := os.Lstat(root.Current()); err != nil {
		t.Fatalf("current link changed after refusal: %v", err)
	}
}

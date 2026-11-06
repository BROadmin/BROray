package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
)

func malformedGlobalLockRoot(t *testing.T) (platform.Root, []byte) {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	content := []byte("{broken")
	if err := os.WriteFile(root.Run()+"/global-operation.json", content, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, content
}

func TestRunDaemonStopsBeforeSocketOnAmbiguousGlobalLock(t *testing.T) {
	root, content := malformedGlobalLockRoot(t)
	if got := runDaemon(root); got != 1 {
		t.Fatalf("daemon exit=%d, want startup failure 1", got)
	}
	if _, err := os.Lstat(root.DaemonSocket()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control socket created over ambiguous ownership: %v", err)
	}
	if _, err := os.Lstat(root.ControlToken()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control token created over ambiguous ownership: %v", err)
	}
	got, err := os.ReadFile(root.Run() + "/global-operation.json")
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("ambiguous ownership record changed: %q, %v", got, err)
	}
}

func TestMaintenanceCleanupStopsOnAmbiguousGlobalLock(t *testing.T) {
	root, content := malformedGlobalLockRoot(t)
	err := installer.AreaCleanup(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "ambiguous global operation lock") {
		t.Fatalf("maintenance cleanup did not reject ambiguous ownership: %v", err)
	}
	got, readErr := os.ReadFile(root.Run() + "/global-operation.json")
	if readErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("ambiguous ownership record changed: %q, %v", got, readErr)
	}
}

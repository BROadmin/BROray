package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
)

func missingBackupManifestRoot(t *testing.T) (platform.Root, string) {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	copyDir := filepath.Join(root.Data(), "backup", "settings-before-import-20261104-120000-deadbeef")
	if err := os.MkdirAll(filepath.Join(copyDir, "root"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root, copyDir
}

func TestRunDaemonStopsBeforeSocketOnAmbiguousBackup(t *testing.T) {
	root, copyDir := missingBackupManifestRoot(t)
	if got := runDaemon(root); got != 1 {
		t.Fatalf("daemon exit=%d, want startup failure 1", got)
	}
	if _, err := os.Lstat(root.DaemonSocket()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control socket created after ambiguous backup: %v", err)
	}
	if _, err := os.Lstat(root.ControlToken()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control token created after ambiguous backup: %v", err)
	}
	if _, err := os.Lstat(copyDir); err != nil {
		t.Fatalf("ambiguous backup copy was altered: %v", err)
	}
}

func TestMaintenanceCleanupStopsOnAmbiguousBackup(t *testing.T) {
	root, copyDir := missingBackupManifestRoot(t)
	if installer.AreaCleanup == nil {
		t.Fatal("area cleanup hook missing")
	}
	err := installer.AreaCleanup(context.Background(), root)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "manifest") {
		t.Fatalf("cleanup did not return missing-manifest error: %v", err)
	}
	if _, err := os.Lstat(copyDir); err != nil {
		t.Fatalf("ambiguous backup copy was altered: %v", err)
	}
}

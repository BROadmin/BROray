package xray

import (
	mxray "github.com/BROadmin/broray/modules/xray"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceDownloadRecoveryKeepsRollback(t *testing.T) {
	h := newHarness(t, false)
	dir, err := os.MkdirTemp("/tmp", "broray-xray-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	a := &mxray.Archive{Path: filepath.Join(dir, "archive.zip")}
	if err := h.s.recordDownload(a); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(a.Path, []byte("partial download"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "rollback"), []byte("preserve"), 0700)
	newService(h.d).recoverInstall()
	if exists(a.Path) || exists(h.s.downloadPath()) {
		t.Fatal("partial archive still owns RAM")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "rollback")); string(b) != "preserve" {
		t.Fatal("rollback copy changed")
	}
}

package xray

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceExplicitLowSpaceConsent(t *testing.T) {
	body := `{"tag":"v26.11.9","currentVersion":"26.10.31","archiveSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","allowUntested":true,"allowPrerelease":false,"allowDowngrade":false,"allowLowSpace":true}`
	if _, ok := parseInstallRequest([]byte(body)); !ok {
		t.Fatal("explicit low-space mode is rejected")
	}
}

func TestMaintenanceRemovedBlocksBoot(t *testing.T) {
	h := newHarness(t, true)
	if err := os.MkdirAll(filepath.Dir(h.d.Path("config", "active-server")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.d.Path("config", "active-server"), []byte("preserved-server"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.d.Root.State(), "xray-maintenance.json"), []byte(`{"schemaVersion":1,"status":"removed","previousVersion":"26.10.31"}`), 0600); err != nil {
		t.Fatal(err)
	}
	h.s.bootStart(context.Background())
	if h.d.App.Xray.Running() {
		t.Fatal("boot ignored intentional removal marker")
	}
}

func TestMaintenanceUninstallAndExplicitInstall(t *testing.T) {
	h := newHarness(t, true)
	config, _ := os.ReadFile(h.d.Root.XrayConfig())
	dataPath := filepath.Join(h.d.Root.Servers(), "owner-record.json")
	if err := os.WriteFile(dataPath, []byte(`{"owner":"preserve"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := h.call("POST", "/api/xray/uninstall.cgi", `{"confirmed":false,"currentVersion":"26.10.31"}`)
	h.expectErr(r, 400, "XRAY_REMOVE_CONFIRMATION_REQUIRED")
	r = h.call("POST", "/api/xray/uninstall.cgi", `{"confirmed":true,"currentVersion":"26.10.31"}`)
	if r.Status != 202 {
		t.Fatalf("uninstall: %+v", r)
	}
	final := h.waitJob()
	if final["result"].(map[string]any)["success"] != true {
		t.Fatalf("remove failed: %v", final)
	}
	if _, err := os.Stat(h.d.Root.XrayBinary()); !os.IsNotExist(err) {
		t.Fatal("core not removed")
	}
	if got, _ := os.ReadFile(dataPath); string(got) != `{"owner":"preserve"}` {
		t.Fatal("user record changed")
	}
	if got, _ := os.ReadFile(h.d.Root.XrayConfig()); string(got) != string(config) {
		t.Fatal("config changed")
	}
	restarted := newService(h.d)
	restarted.recoverInstall()
	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { defer close(done); restarted.bootstrapInstall(ctx) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("bootstrap retries an intentionally removed core")
	}
	status := h.call("GET", "/api/xray/status.cgi", "")
	if status.Status != 200 || status.Data["maintenanceStatus"] != "removed" {
		t.Fatalf("removed state not visible: %+v", status)
	}
	archive := zipWith(t, "26.12.1")
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	body, _ := json.Marshal(map[string]any{"tag": "v26.12.1", "currentVersion": "", "archiveSha256": fmt.Sprintf("%x", sha256.Sum256(archive)), "allowUntested": true, "allowPrerelease": false, "allowDowngrade": false})
	r = h.call("POST", "/api/xray/install.cgi", string(body))
	if r.Status != 202 {
		t.Fatal(r)
	}
	final = h.waitJob()
	if final["result"].(map[string]any)["success"] != true {
		t.Fatal(final)
	}
	status = h.call("GET", "/api/xray/status.cgi", "")
	if status.Data["installed"] != true || status.Data["maintenanceStatus"] != "ready" {
		t.Fatal(status.Data)
	}
}

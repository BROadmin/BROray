package xray

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/BROadmin/broray/internal/compat"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceInterruptedStagingCanBeReplacedExplicitly(t *testing.T) {
	h := newHarness(t, false)
	state := `{"schemaVersion":1,"status":"interrupted","mode":"ram-rollback","phase":"switching","previousVersion":"26.10.31","targetVersion":"26.12.1","backupPath":"/tmp/broray-xray-lost/rollback","backupSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","stagingOwned":true}`
	if err := os.WriteFile(h.s.maintenancePath(), []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.d.Root.XrayBinary()+".new", []byte("incomplete candidate"), 0700); err != nil {
		t.Fatal(err)
	}
	archive := zipWith(t, "26.12.1")
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	body, _ := json.Marshal(map[string]any{"tag": "v26.12.1", "currentVersion": "", "archiveSha256": fmt.Sprintf("%x", sha256.Sum256(archive)), "allowUntested": true, "allowPrerelease": false, "allowDowngrade": false})
	if r := h.call("POST", "/api/xray/install.cgi", string(body)); r.Status != 202 {
		t.Fatal(r)
	}
	final := h.waitJob()["result"].(map[string]any)
	if final["success"] != true {
		t.Fatalf("explicit installation cannot recover lost RAM backup: %v", final)
	}
}

func TestMaintenanceRecoveryClosesOperationPointer(t *testing.T) {
	h := newHarness(t, false)
	_ = os.WriteFile(h.s.maintenancePath(), []byte(`{"schemaVersion":1,"status":"removed"}`), 0600)
	_ = compat.WriteJSON(h.s.pointerPath(), map[string]any{"backgroundOperationId": "op-20261102101530-1234-abcdefabcdef", "operation": "uninstall", "running": true, "operationRunning": true})
	h.s.recoverInstall()
	r := h.call("GET", "/api/xray/operation-status.cgi", "")
	if r.Status != 200 || r.Data["operationRunning"] != false {
		t.Fatalf("recovered removal left indeterminate operation: %+v", r)
	}
}

func TestMaintenanceStorageBudgets(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		flash, ram, newSize, oldSize int64
		low, backup, ok              bool
	}{
		{"normal-exact", 48, 32, 40, 40, false, false, true},
		{"normal-short", 47, 32, 40, 40, false, false, false},
		{"low-exact", 8, 72, 40, 40, true, false, true},
		{"low-ram-short", 8, 71, 40, 40, true, false, false},
		{"low-copy-already-charged", 8, 32, 40, 40, true, true, true},
		{"low-larger-target", 28, 72, 60, 40, true, false, true},
		{"low-larger-short", 27, 72, 60, 40, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkStorage(storageResources{FlashFree: tc.flash << 20, RAMFree: tc.ram << 20}, tc.newSize<<20, tc.oldSize<<20, tc.low, tc.backup)
			if (err == nil) != tc.ok {
				t.Fatalf("budget result: %v", err)
			}
		})
	}
}

func TestMaintenanceRechecksNormalSpaceBeforeStop(t *testing.T) {
	h := newHarness(t, true)
	old := fileSHA256(h.d.Root.XrayBinary())
	if err := h.d.App.Xray.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	probes := 0
	h.s.resourceProbe = func() (storageResources, error) {
		probes++
		free := int64(1 << 30)
		if probes > 1 {
			free = 0
		}
		return storageResources{FlashFree: free, RAMFree: 1 << 30}, nil
	}
	archive := zipWith(t, "26.12.1")
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	body, _ := json.Marshal(map[string]any{"tag": "v26.12.1", "currentVersion": "26.10.31", "archiveSha256": fmt.Sprintf("%x", sha256.Sum256(archive)), "allowUntested": true, "allowPrerelease": false, "allowDowngrade": false})
	if r := h.call("POST", "/api/xray/install.cgi", string(body)); r.Status != 202 {
		t.Fatal(r)
	}
	final := h.waitJob()["result"].(map[string]any)
	if final["success"] != false || fileSHA256(h.d.Root.XrayBinary()) != old || !h.d.App.Xray.Running() {
		t.Fatalf("resource change did not preserve old core: %v", final)
	}
}

func TestMaintenanceSymlinkCoreRejected(t *testing.T) {
	h := newHarness(t, true)
	bin := h.d.Root.XrayBinary()
	external := filepath.Join(t.TempDir(), "external")
	if err := os.Rename(bin, external); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, bin); err != nil {
		t.Fatal(err)
	}
	archive := zipWith(t, "26.12.1")
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	body, _ := json.Marshal(map[string]any{"tag": "v26.12.1", "currentVersion": "26.10.31", "archiveSha256": fmt.Sprintf("%x", sha256.Sum256(archive)), "allowUntested": true, "allowPrerelease": false, "allowDowngrade": false})
	if r := h.call("POST", "/api/xray/install.cgi", string(body)); r.Status != 202 {
		t.Fatal(r)
	}
	final := h.waitJob()["result"].(map[string]any)
	st, _ := os.Lstat(bin)
	if final["success"] != false || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink core was replaced: %v", final)
	}
}

func TestMaintenanceLowSpaceStartupFailureRestoresRunning(t *testing.T) {
	h := newHarness(t, true)
	bin := h.d.Root.XrayBinary()
	old := fileSHA256(bin)
	if err := h.d.App.Xray.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	startWait = 100000000
	t.Cleanup(func() { startWait = 180000000000 })
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("xray")
	_, _ = w.Write([]byte("#!/bin/sh\ncase \"$1\" in\nversion) echo 'Xray 26.12.1';exit 0;;\nrun) if [ \"$2\" = '-test' ]; then echo 'Configuration OK.';exit 0;fi;exit 45;;\nesac\n"))
	_ = z.Close()
	archive := b.Bytes()
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	body, _ := json.Marshal(map[string]any{"tag": "v26.12.1", "currentVersion": "26.10.31", "archiveSha256": fmt.Sprintf("%x", sha256.Sum256(archive)), "allowUntested": true, "allowPrerelease": false, "allowDowngrade": false, "allowLowSpace": true})
	if r := h.call("POST", "/api/xray/install.cgi", string(body)); r.Status != 202 {
		t.Fatal(r)
	}
	final := h.waitJob()["result"].(map[string]any)
	if final["success"] != false || fileSHA256(bin) != old || !h.d.App.Xray.Running() {
		t.Fatalf("running core not restored: %v", final)
	}
	if final["result"].(map[string]any)["rollbackSuccess"] != true {
		t.Fatal(final)
	}
}

func TestMaintenanceCatalogShowsRAM(t *testing.T) {
	h := newHarness(t, true)
	fakeGitHub(t, []fakeRel{{tag: "v26.10.31", published: "2026-10-31T00:00:00Z", archive: zipWith(t, "26.10.31")}})
	r := h.call("POST", "/api/xray/update-check.cgi", "{}")
	if r.Status != 200 {
		t.Fatal(r)
	}
	tmp := r.Data["temporaryStorage"].(map[string]any)
	if tmp["kind"] != "ram" || tmp["reserveBytes"] != float64(32<<20) {
		t.Fatalf("RAM budget absent: %v", tmp)
	}
}

func TestMaintenanceLowSpaceInstall(t *testing.T) {
	h := newHarness(t, true)
	archive := zipWith(t, "26.12.1")
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	body, _ := json.Marshal(map[string]any{"tag": "v26.12.1", "currentVersion": "26.10.31", "archiveSha256": fmt.Sprintf("%x", sha256.Sum256(archive)), "allowUntested": true, "allowPrerelease": false, "allowDowngrade": false, "allowLowSpace": true})
	r := h.call("POST", "/api/xray/install.cgi", string(body))
	if r.Status != 202 {
		t.Fatal(r)
	}
	final := h.waitJob()["result"].(map[string]any)
	if final["success"] != true {
		t.Fatal(final)
	}
	if result := final["result"].(map[string]any); result["storageMode"] != "ram-rollback" {
		t.Fatalf("confirmed mode not executed: %v", result)
	}
	if h.d.App.Xray.Running() {
		t.Fatal("previously stopped core was started")
	}
}

func TestMaintenanceRecoveryPrefersUncommittedOld(t *testing.T) {
	h := newHarness(t, true)
	bin := h.d.Root.XrayBinary()
	oldHash := fileSHA256(bin)
	if err := os.Rename(bin, bin+".old"); err != nil {
		t.Fatal(err)
	}
	writeBinary(t, bin, "26.12.1")
	state := map[string]any{"schemaVersion": 1, "status": "installing", "mode": "normal", "phase": "switching", "previousVersion": "26.10.31", "targetVersion": "26.12.1", "backupSha256": oldHash, "candidateSha256": fileSHA256(bin)}
	b, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(h.d.Root.State(), "xray-maintenance.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	h.s.recoverInstall()
	if got := installedVersion(h.s.versionOutput(context.Background(), bin)); got != "26.10.31" {
		t.Fatalf("unconfirmed core kept after interrupted swap: %s", got)
	}
}

func TestMaintenanceLostRAMCopyRemainsVisible(t *testing.T) {
	h := newHarness(t, false)
	state := `{"schemaVersion":1,"status":"installing","mode":"ram-rollback","phase":"switching","previousVersion":"26.10.31","targetVersion":"26.12.1","backupPath":"/tmp/broray-xray-missing/rollback","backupSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	if err := os.WriteFile(filepath.Join(h.d.Root.State(), "xray-maintenance.json"), []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	h.s.recoverInstall()
	r := h.call("GET", "/api/xray/status.cgi", "")
	if r.Data["maintenanceStatus"] != "interrupted" {
		t.Fatalf("missing RAM recovery copy not reported: %v", r.Data)
	}
	if !h.s.maintenanceBlocksAuto() {
		t.Fatal("lost rollback copy triggered first installation")
	}
}

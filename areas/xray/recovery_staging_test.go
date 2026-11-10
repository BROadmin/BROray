package xray

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestMaintenanceRollbackReclaimsOwnedPartialFiles(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			h := newHarness(t, false)
			// Real bounded filesystem, so retaining the 6 MiB candidate makes copying
			// the 6 MiB old executable fail. No synthetic free-space probe here.
			if err := syscall.Mount("tmpfs", h.d.Root.Runtime(), "tmpfs", 0, "size=8m"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Unmount(h.d.Root.Runtime(), 0) })
			dir, err := os.MkdirTemp("/tmp", "broray-xray-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			old := bytes.Repeat([]byte{0x41}, 6<<20)
			backup := filepath.Join(dir, "rollback")
			if err := os.WriteFile(backup, old, 0700); err != nil {
				t.Fatal(err)
			}
			bin := h.d.Root.XrayBinary()
			if complete {
				if err := os.WriteFile(bin+".recover", old, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(bin+".new", bytes.Repeat([]byte{0x42}, 6<<20), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(bin+".recover", []byte("partial"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "status": "installing", "mode": "ram-rollback", "phase": "switching", "previousVersion": "26.10.31", "backupPath": backup, "backupSha256": fmt.Sprintf("%x", sha256.Sum256(old)), "stagingOwned": true, "recoveryOwned": true})
			var m maintenanceState
			_ = json.Unmarshal(raw, &m)
			if err := h.s.restoreMaintenance(context.Background(), m); err != nil {
				t.Fatalf("RAM rollback with owned staging failed: %v", err)
			}
			if got, _ := os.ReadFile(bin); !bytes.Equal(got, old) {
				t.Fatal("old core not restored")
			}
		})
	}
}

func TestMaintenanceLostRAMReclaimsBeforeResourceCheck(t *testing.T) {
	h := newHarness(t, false)
	bin := h.d.Root.XrayBinary()
	raw := `{"schemaVersion":1,"status":"interrupted","mode":"ram-rollback","phase":"switching","previousVersion":"26.10.31","backupPath":"/tmp/broray-xray-lost/rollback","stagingOwned":true}`
	_ = os.WriteFile(h.s.maintenancePath(), []byte(raw), 0600)
	_ = os.WriteFile(bin+".new", []byte("partial"), 0700)
	h.s.resourceProbe = func() (storageResources, error) {
		free := int64(1 << 30)
		if b, _ := os.ReadFile(bin + ".new"); string(b) == "partial" {
			free = 1
		}
		return storageResources{FlashFree: free, RAMFree: 1 << 30}, nil
	}
	archive := zipWith(t, "26.12.1")
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	body, _ := json.Marshal(map[string]any{"tag": "v26.12.1", "currentVersion": "", "archiveSha256": fmt.Sprintf("%x", sha256.Sum256(archive)), "allowUntested": true, "allowPrerelease": false, "allowDowngrade": false})
	if r := h.call("POST", "/api/xray/install.cgi", string(body)); r.Status != 202 {
		t.Fatal(r)
	}
	final := h.waitJob()["result"].(map[string]any)
	if final["success"] != true {
		t.Fatal(final)
	}
}

func TestMaintenanceRecordsOwnershipBeforeStaging(t *testing.T) {
	h := newHarness(t, false)
	_ = os.WriteFile(h.s.maintenancePath(), []byte(`{"schemaVersion":1,"status":"removed"}`), 0600)
	observed := false
	h.s.resourceProbe = func() (storageResources, error) {
		if exists(h.d.Root.XrayBinary() + ".new") {
			observed = true
			b, _ := os.ReadFile(h.s.maintenancePath())
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			if m["status"] != "installing" || m["stagingOwned"] != true || m["previousStatus"] != "removed" {
				t.Errorf("candidate exists before durable ownership journal: %s", b)
			}
		}
		return storageResources{FlashFree: 1 << 30, RAMFree: 1 << 30}, nil
	}
	archive := zipWith(t, "26.12.1")
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	body, _ := json.Marshal(map[string]any{"tag": "v26.12.1", "currentVersion": "", "archiveSha256": fmt.Sprintf("%x", sha256.Sum256(archive)), "allowUntested": true, "allowPrerelease": false, "allowDowngrade": false})
	if r := h.call("POST", "/api/xray/install.cgi", string(body)); r.Status != 202 {
		t.Fatal(r)
	}
	h.waitJob()
	if !observed {
		t.Fatal("no staged candidate was observed")
	}
}

func TestMaintenanceUninstallPollingName(t *testing.T) {
	h := newHarness(t, true)
	if r := h.call("POST", "/api/xray/uninstall.cgi", `{"confirmed":true,"currentVersion":"26.10.31"}`); r.Status != 202 {
		t.Fatal(r)
	}
	h.waitJob()
	r := h.call("GET", "/api/xray/operation-status.cgi", "")
	if r.Data["operation"] != "uninstall" {
		t.Fatalf("incorrect operation: %+v", r)
	}
}

func TestMaintenanceRecoveryKeepsForeignStaging(t *testing.T) {
	h := newHarness(t, false)
	p := h.d.Root.XrayBinary() + ".new"
	if err := os.WriteFile(p, []byte("foreign"), 0700); err != nil {
		t.Fatal(err)
	}
	m := maintenanceState{Status: "installing", PreviousStatus: "removed"}
	if err := h.s.restoreMaintenance(context.Background(), m); err == nil {
		t.Fatal("unowned staging accepted")
	}
	if b, _ := os.ReadFile(p); string(b) != "foreign" {
		t.Fatal("unowned staging modified")
	}
}

func TestMaintenanceRecoversReinstallFromRemoved(t *testing.T) {
	h := newHarness(t, false)
	p := h.d.Root.XrayBinary() + ".new"
	m := maintenanceState{Status: "installing", Mode: "normal", Phase: "staging", PreviousStatus: "removed", StagingOwned: true, RecoveryOwned: true}
	if err := h.s.writeMaintenance(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("partial"), 0700); err != nil {
		t.Fatal(err)
	}
	h.s.recoverInstall()
	got, err := h.s.readMaintenance()
	if err != nil || got.Status != "removed" || exists(p) {
		t.Fatalf("incomplete reinstall not recovered: %v %v", got, err)
	}
}

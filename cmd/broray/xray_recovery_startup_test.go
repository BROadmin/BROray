//go:build linux && !brorayfake

package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/platform"
)

// The real daemon must reach WebUI even when the old core's RAM backup was
// lost. This tests the startup gate, not a direct recoverInstall call.
func TestMaintenanceDaemonStartsWithOwnedInterruptedFence(t *testing.T) {
	dir, err := os.MkdirTemp("", "br-maint-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	root := platform.Root{Dir: dir}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := platform.DefaultConfig()
	cfg.ListenAddress = "127.0.0.1"
	cfg.WebPort = port
	cfg.Keenetic.RCIURL = "http://127.0.0.1:1"
	cfg.Keenetic.WriteTransport = "rci"
	if err := platform.SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	state := `{"schemaVersion":1,"status":"installing","mode":"ram-rollback","phase":"switching","previousVersion":"26.10.31","targetVersion":"26.11.9","backupPath":"/tmp/broray-xray-lost/rollback","backupSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	if err := os.WriteFile(filepath.Join(root.State(), "xray-maintenance.json"), []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	fence := map[string]any{"scope": "xray", "action": "xray:install", "bundleId": "xray", "pending": false, "startedAt": time.Now().UTC(), "updatedAt": time.Now().UTC(), "daemonOwnedXray": true}
	if err := platform.WriteJSONAtomic(filepath.Join(root.Run(), "global-operation.json"), fence, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAvailabilityDaemonHelper$")
	cmd.Env = append(os.Environ(), "BRORAY_AVAILABILITY_CHILD=1", "BRORAY_AVAILABILITY_ROOT="+dir, "BRORAY_AVAILABILITY_PORT="+strconv.Itoa(port))
	out, err := os.Create(filepath.Join(dir, "daemon-output"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.Shutdown(ctx, root)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/login.html")
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == 200 {
				b, _ := os.ReadFile(filepath.Join(root.State(), "xray-maintenance.json"))
				var got map[string]any
				_ = json.Unmarshal(b, &got)
				if got["status"] != "interrupted" {
					t.Fatalf("lost RAM was not reported: %s", b)
				}
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "daemon-output"))
	t.Fatalf("WebUI unavailable after interrupted Xray job: %s", b)
}

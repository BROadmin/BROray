//go:build linux && !brorayfake

package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/platform"
)

func TestDPIProfilePendingStopsDaemonBeforeAreas(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cfg := platform.DefaultConfig()
	cfg.ListenAddress = "127.0.0.1"
	cfg.WebPort = port
	cfg.Keenetic.RCIURL = "http://127.0.0.1:1"
	cfg.Keenetic.WriteTransport = "rci"
	if err := platform.SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.Config(), "server-dpi-pending.v1.json")
	raw := []byte(`{"unfinished":true}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(root.Dir, "daemon-output"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAvailabilityDaemonHelper$")
	cmd.Env = append(os.Environ(), "BRORAY_AVAILABILITY_CHILD=1", "BRORAY_AVAILABILITY_ROOT="+root.Dir, "BRORAY_AVAILABILITY_PORT="+strconv.Itoa(port))
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("pending operation accepted")
		}
	case <-time.After(3 * time.Second):
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = control.Shutdown(ctx, root)
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			// This is our unreaped child object, never a discovered numeric PID.
			_ = cmd.Process.Kill()
			<-done
		}
		t.Fatal("daemon continued startup with pending reserve transaction")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("pending evidence changed")
	}
	if _, err := os.Lstat(root.DaemonSocket()); !os.IsNotExist(err) {
		t.Fatal("control listener started before reserve recovery")
	}
	logs, err := os.ReadFile(filepath.Join(root.Dir, "daemon-output"))
	if err != nil || !bytes.Contains(logs, []byte("reserve recovery required")) {
		t.Fatalf("wrong startup failure: %s %v", logs, err)
	}
}

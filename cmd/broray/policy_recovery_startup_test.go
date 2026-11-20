//go:build linux && !brorayfake

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/routepolicy"
)

func TestPolicyDaemonRecoversBeforeWebUI(t *testing.T) {
	for _, marked := range []bool{true, false} {
		t.Run(map[bool]string{true: "owned", false: "legacy-refused"}[marked], func(t *testing.T) {
			dir, err := os.MkdirTemp("", "br-policy-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			root := platform.Root{Dir: dir}
			if err := root.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			cfg := platform.DefaultConfig()
			cfg.ListenAddress = "127.0.0.1"
			cfg.WebPort = port
			cfg.Keenetic.RCIURL = "http://127.0.0.1:1"
			cfg.Keenetic.WriteTransport = "rci"
			if err := platform.SaveConfig(root, cfg); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(root.XrayBinary(), []byte("#!/bin/sh\necho 'Xray 26.11.9'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			old, next := []byte(`{"old":true}`), []byte(`{"next":true}`)
			if err := os.WriteFile(root.XrayConfig(), old, 0600); err != nil {
				t.Fatal(err)
			}
			j, err := routepolicy.OpenJournal(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := j.Begin(context.Background(), routepolicy.State{SchemaVersion: 1, Rules: []routepolicy.Rule{}}, next, ""); err != nil {
				j.Close()
				t.Fatal(err)
			}
			if err := platform.WriteFileAtomicNoFollow(root.XrayConfig(), next, 0600); err != nil {
				j.Close()
				t.Fatal(err)
			}
			if err := j.InstallState(context.Background()); err != nil {
				j.Close()
				t.Fatal(err)
			}
			if err := j.Close(); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			fencePath := filepath.Join(root.Run(), "global-operation.json")
			if err := platform.WriteJSONAtomic(fencePath, compat.Holder{Scope: "routes", Action: "routes:policy", BundleID: "policy", StartedAt: now, UpdatedAt: now, DaemonOwnedPolicy: marked}, 0600); err != nil {
				t.Fatal(err)
			}
			ownerBefore, err := os.ReadFile(fencePath)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestAvailabilityDaemonHelper$")
			cmd.Env = append(os.Environ(), "BRORAY_AVAILABILITY_CHILD=1", "BRORAY_AVAILABILITY_ROOT="+dir, "BRORAY_AVAILABILITY_PORT="+strconv.Itoa(port))
			output, err := os.Create(filepath.Join(dir, "daemon-output"))
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			cmd.Stdout = output
			cmd.Stderr = output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			exited := false
			defer func() {
				if exited {
					return
				}
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
			if !marked {
				select {
				case err := <-done:
					exited = true
					if err == nil {
						t.Fatal("legacy owner startup accepted")
					}
				case <-time.After(8 * time.Second):
					t.Fatal("legacy owner did not stop startup")
				}
				got, err := os.ReadFile(root.XrayConfig())
				if err != nil || string(got) != string(next) {
					t.Fatal("legacy recovery mutated config")
				}
				ownerAfter, err := os.ReadFile(fencePath)
				if err != nil || string(ownerAfter) != string(ownerBefore) {
					t.Fatal("legacy owner changed")
				}
				return
			}
			client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case err := <-done:
					exited = true
					data, _ := os.ReadFile(filepath.Join(dir, "daemon-output"))
					t.Fatalf("daemon exited: %v %s", err, data)
				default:
				}
				res, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/login.html")
				if err == nil {
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
					if res.StatusCode == 200 {
						got, err := os.ReadFile(root.XrayConfig())
						if err != nil || string(got) != string(old) {
							t.Fatal("WebUI opened before prior config restored")
						}
						for _, path := range []string{fencePath, filepath.Join(root.Routes(), routepolicy.JournalFile), filepath.Join(root.Routes(), routepolicy.StateFile)} {
							if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
								t.Fatalf("recovery left %s", path)
							}
						}
						return
					}
				}
				time.Sleep(30 * time.Millisecond)
			}
			data, _ := os.ReadFile(filepath.Join(dir, "daemon-output"))
			t.Fatalf("WebUI unavailable: %s", data)
		})
	}
}

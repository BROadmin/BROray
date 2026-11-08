//go:build linux && !brorayfake

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/platform"
)

type availabilityVerifier struct{}

func (availabilityVerifier) Verify(context.Context, string, string) error { return nil }

// This helper runs the real daemon in an isolated child process. Only its own
// listening TCP socket is shut down, without closing/reusing Go's file descriptor.
func TestAvailabilityDaemonHelper(t *testing.T) {
	if os.Getenv("BRORAY_AVAILABILITY_CHILD") != "1" {
		return
	}
	root := platform.Root{Dir: os.Getenv("BRORAY_AVAILABILITY_ROOT")}
	port, err := strconv.Atoi(os.Getenv("BRORAY_AVAILABILITY_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	devVerifierOverride = availabilityVerifier{}
	go func() {
		for {
			if _, err := os.Stat(filepath.Join(root.Dir, "fail-listener")); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		fds, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			panic(err)
		}
		for _, f := range fds {
			fd, err := strconv.Atoi(f.Name())
			if err != nil {
				continue
			}
			sa, err := syscall.Getsockname(fd)
			addr, ok := sa.(*syscall.SockaddrInet4)
			if err != nil || !ok || addr.Port != port || addr.Addr != [4]byte{127, 0, 0, 1} {
				continue
			}
			listening, _ := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ACCEPTCONN)
			if listening != 1 {
				continue
			}
			if err := syscall.Shutdown(fd, syscall.SHUT_RDWR); err != nil {
				panic(err)
			}
			if err := os.WriteFile(filepath.Join(root.Dir, "listener-failed"), []byte("injected"), 0600); err != nil {
				panic(err)
			}
			return
		}
		panic("owned test listener not found")
	}()
	os.Exit(runDaemon(root))
}

func TestWebUIRecoversAfterListenerFailure(t *testing.T) {
	rootDir, err := os.MkdirTemp("", "br-web-") // Unix socket path must stay short.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(rootDir) })
	root := platform.Root{Dir: rootDir}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	rci := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer rci.Close()
	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reserved.Addr().(*net.TCPAddr).Port
	reserved.Close()
	cfg := platform.DefaultConfig()
	cfg.ListenAddress, cfg.WebPort = "127.0.0.1", port
	cfg.Keenetic.RCIURL, cfg.Keenetic.WriteTransport = rci.URL, "rci"
	if err := platform.SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	// No WAN download and no active VPN: a stopped local version fixture.
	if err := os.WriteFile(root.XrayBinary(), []byte("#!/bin/sh\necho 'Xray 26.10.31'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAvailabilityDaemonHelper$")
	cmd.Env = append(os.Environ(), "BRORAY_AVAILABILITY_CHILD=1", "BRORAY_AVAILABILITY_ROOT="+rootDir,
		"BRORAY_AVAILABILITY_PORT="+strconv.Itoa(port))
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := control.Shutdown(ctx, root); err != nil {
			t.Errorf("shutdown request: %v", err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon shutdown: %v", err)
			}
		case <-time.After(12 * time.Second):
			_ = cmd.Process.Kill() // exact child created by this test
			<-done
			t.Error("daemon did not shut down")
		}
		t.Log(output.String())
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	wait := func(label string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatalf("%s did not recover while daemon stayed alive", label)
	}
	get := func(path string, cookie *http.Cookie) int {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	wait("initial login page with unavailable RCI and stopped Xray", func() bool { return get("/", nil) == 200 })
	resp, err := client.Post(base+"/api/login.cgi", "application/json", strings.NewReader(`{"login":"synthetic","password":"synthetic"}`))
	if err != nil {
		t.Fatal(err)
	}
	cookies := resp.Cookies()
	resp.Body.Close()
	if resp.StatusCode != 200 || len(cookies) != 1 {
		t.Fatal("fixture login failed")
	}
	for _, page := range []string{"home", "servers", "subscriptions", "dns", "routes", "routes-custom", "routes-import", "keenetic", "xray", "broray"} {
		if code := get("/"+page+".html", cookies[0]); code != 200 {
			t.Fatalf("page %s: %d", page, code)
		}
	}
	if err := os.WriteFile(filepath.Join(rootDir, "fail-listener"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	wait("fault acknowledgement", func() bool { _, err := os.Stat(filepath.Join(rootDir, "listener-failed")); return err == nil })
	if _, err := control.Ping(context.Background(), root); err != nil {
		t.Fatalf("control unexpectedly down: %v", err)
	}
	wait("WebUI after kernel listener failure", func() bool { return get("/api/session.cgi", cookies[0]) == 200 })
	if code := get("/home.html", cookies[0]); code != 200 {
		t.Fatalf("authenticated page after recovery: %d", code)
	}
}

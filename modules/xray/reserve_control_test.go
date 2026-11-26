package xray

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

func startReserveControlCore(t *testing.T, privateAPI bool) (*Process, RuntimeSnapshot) {
	t.Helper()
	binary := os.Getenv("BRORAY_XRAY_BIN")
	if binary == "" {
		t.Skip("set BRORAY_XRAY_BIN to the tagged core for the control integration")
	}
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.Run(), "reserve-control", "api.sock")
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	listen := path
	tag := "broray-reserve-control"
	if !privateAPI {
		listen = filepath.Join(root.Run(), "unrelated.sock")
		tag = "fixture-other-control"
	}
	cfg := map[string]any{
		"api": map[string]any{"tag": tag, "listen": listen, "services": []string{"RoutingService"}},
		"outbounds": []map[string]any{
			{"tag": "broray-reserve-blocked", "protocol": "blackhole"},
			{"tag": "broray-reserve-slot-1", "protocol": "blackhole"},
			{"tag": "broray-reserve-slot-2", "protocol": "blackhole"},
		},
		"routing": map[string]any{"rules": []any{}, "balancers": []map[string]any{{
			"tag": "broray-reserve", "selector": []string{"broray-reserve-blocked"},
		}}},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.XrayConfig(), data, 0600); err != nil {
		t.Fatal(err)
	}
	p := NewProcess(root)
	p.Binary = binary
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	return p, awaitReserveControlCore(t, p, listen)
}

func TestReserveControlRefusesMisconfiguredLayout(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	config := root.XrayConfig()
	data := []byte(`{"api":{"tag":"broray-reserve-control","listen":"127.0.0.1:10085","services":["RoutingService"]}}`)
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareReserveControlLaunch(root, config); !errors.Is(err, ErrReserveSelectorUnconfirmed) {
		t.Fatalf("reserved API exposed through TCP: %v", err)
	}
}

func awaitReserveControlCore(t *testing.T, p *Process, socket string) RuntimeSnapshot {
	t.Helper()
	want := p.RuntimeSnapshot()
	deadline := time.Now().Add(4 * time.Second)
	for {
		conn, err := net.DialTimeout("unix", socket, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return want
		}
		if time.Now().After(deadline) || p.RuntimeSnapshot() != want {
			log, _ := os.ReadFile(filepath.Join(p.root.Logs(), "xray.log"))
			t.Fatalf("control did not become ready: %v, %s", err, log)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func TestReserveControlPrivateSelection(t *testing.T) {
	p, snap := startReserveControlCore(t, true)
	ctx := context.Background()
	if err := p.SelectReserveOutboundIfCurrent(ctx, snap, "broray-reserve-slot-2"); err != nil {
		check, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		raw, readErr := reserveAPICommand(check, p.Binary, "bi", "--server=unix://"+ReserveControlSocket(p.root), reserveSelector)
		t.Fatalf("selection failed: %v, test readback: %q (%v)", err, raw, readErr)
	}
	if p.RuntimeSnapshot() != snap {
		t.Fatal("select replaced the child")
	}
	if err := p.SelectReserveOutboundIfCurrent(ctx, snap, "outside-roster"); err == nil {
		t.Fatal("arbitrary override accepted")
	}
	dir := filepath.Join(p.root.Run(), "reserve-control")
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := p.SelectReserveOutboundIfCurrent(ctx, snap, "broray-reserve-slot-1"); err == nil {
		t.Fatal("public control directory was trusted")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	restarted, err := p.ReplaceIfCurrent(ctx, snap, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := awaitReserveControlCore(t, p, filepath.Join(dir, "api.sock")); got != restarted {
		t.Fatal("new generation was not retained")
	}
	if err := p.SelectReserveOutboundIfCurrent(ctx, snap, "broray-reserve-slot-1"); !errors.Is(err, ErrRuntimeChanged) {
		t.Fatalf("old authority accepted: %v", err)
	}
	if err := p.SelectReserveOutboundIfCurrent(ctx, restarted, "broray-reserve-slot-1"); err != nil {
		t.Fatal(err)
	}
}

func TestReserveControlRefusesForeignSocket(t *testing.T) {
	p, snap := startReserveControlCore(t, false)
	path := filepath.Join(p.root.Run(), "reserve-control", "api.sock")
	foreign, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	if err := p.SelectReserveOutboundIfCurrent(context.Background(), snap, "broray-reserve-slot-1"); err == nil {
		t.Fatal("unowned peer was trusted")
	}
	if p.RuntimeSnapshot() != snap {
		t.Fatal("foreign peer check changed the child")
	}
}

func TestReserveControlRejectsAmbiguousReadback(t *testing.T) {
	const valid = "  - Selecting Override:\n    1   broray-reserve-slot-1\n  - Selects:\n    1   broray-reserve-blocked\n"
	if !reserveReadbackMatches([]byte(valid), "broray-reserve-slot-1") {
		t.Fatal("pinned output rejected")
	}
	for _, output := range []string{
		"  - Selects:\n    1   broray-reserve-slot-1\n",
		valid + "    2   direct\n",
		"  - Selecting Override:\n    1   broray-reserve-slot-1\n  - Selects:\n    1   direct\n",
	} {
		if reserveReadbackMatches([]byte(output), "broray-reserve-slot-1") {
			t.Fatalf("accepted ambiguous readback %q", output)
		}
	}
}

func TestReserveControlAfterAbruptCoreExit(t *testing.T) {
	p, old := startReserveControlCore(t, true)
	ctx := context.Background()
	if err := p.SelectReserveOutboundIfCurrent(ctx, old, "broray-reserve-slot-2"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	if p.runtimeSnapshotLocked() != old || p.cur == nil {
		p.mu.Unlock()
		t.Fatal("fixture no longer owns child")
	}
	p.signalLocked(p.cur, syscall.SIGKILL)
	p.mu.Unlock()
	deadline := time.Now().Add(18 * time.Second)
	for {
		fresh := p.RuntimeSnapshot()
		if fresh != old && fresh.Running && fresh.Generation > old.Generation {
			fresh = awaitReserveControlCore(t, p, ReserveControlSocket(p.root))
			if err := p.SelectReserveOutboundIfCurrent(ctx, old, "broray-reserve-slot-1"); !errors.Is(err, ErrRuntimeChanged) {
				t.Fatalf("dead child could still select: %v", err)
			}
			if err := p.SelectReserveOutboundIfCurrent(ctx, fresh, "broray-reserve-slot-1"); err != nil {
				t.Fatal(err)
			}
			return
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(filepath.Join(p.root.Logs(), "xray.log"))
			t.Fatalf("steady replacement not reached: %+v, %s", fresh, log)
		}
		time.Sleep(35 * time.Millisecond)
	}
}

func TestReserveControlRetainsUnconfirmedSocket(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	path := ReserveControlSocket(root)
	if err := preparePrivateReserveSocket(root, path); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrUnix{Name: path}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateReserveSocket(root, path); !errors.Is(err, ErrReserveSelectorUnconfirmed) {
		t.Fatalf("bound socket was reclaimed before listen: %v", err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) {
		t.Fatalf("original socket disappeared: %v", err)
	}
}

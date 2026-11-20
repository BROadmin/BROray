//go:build linux && !brorayfake

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDPIProfileDaemonRecovery(t *testing.T) {
	for _, kind := range []string{"prepared-stopped", "committed-stopped", "committed-running", "volatile-owner-lost", "reserve-owner", "source-owner", "mixed-journals", "selection-owner-mismatch", "bad-native-config", "prepared-running", "inactive-profile", "reconciled-stopped"} {
		t.Run(kind, func(t *testing.T) { testDPIProfileDaemonRecovery(t, kind) })
	}
}

func testDPIProfileDaemonRecovery(t *testing.T, kind string) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, b []byte) { t.Helper(); must(os.WriteFile(path, b, 0600)) }
	root := platform.Root{Dir: t.TempDir()}
	must(root.EnsureDirs())
	must(os.MkdirAll(filepath.Join(root.Data(), "config", "system"), 0700))
	port := dpiStartupPort(t)
	cfg := platform.DefaultConfig()
	cfg.ListenAddress = "127.0.0.1"
	cfg.WebPort = port
	cfg.Keenetic.RCIURL = "http://127.0.0.1:1"
	cfg.Keenetic.WriteTransport = "rci"
	must(platform.SaveConfig(root, cfg))
	d := compat.NewDeps(context.Background(), app.New(root, cfg, log.New(io.Discard, "", 0)))
	rec, err := servers.ParseLink("trojan://synthetic-startup@127.0.0.1:1?security=tls&sni=localhost#Recovery", "startup-profile", nil)
	must(err)
	must(servers.WriteRecord(d, rec))
	rec, err = servers.ReadRecord(d, "startup-profile")
	must(err)
	oldRaw, err := os.ReadFile(servers.RecordPath(d, "startup-profile"))
	must(err)
	var next servers.Record
	must(json.Unmarshal(oldRaw, &next))
	transport, ok := next["transport"].(map[string]any)
	if !ok {
		t.Fatal("fixture transport absent")
	}
	original, present := transport["finalMask"]
	next["dpi"] = map[string]any{"schema": 1, "preset": "tlshello-v1", "originalMaskPresent": present, "originalMask": original}
	transport["finalMask"] = map[string]any{"tcp": []any{map[string]any{"type": "fragment", "settings": map[string]any{"packets": "tlshello", "length": "100-200", "delay": "1-2", "maxSplit": 64}}}}
	nextRaw, err := servers.Marshal(next)
	must(err)
	gen := servers.GenSettings{Listen: "127.0.0.1", SocksPort: 2080, LogLevel: "none"}
	activeID := "startup-profile"
	mainRecord := rec
	if kind == "inactive-profile" {
		activeID = "other-profile"
		mainRecord, err = servers.ParseLink("trojan://other@127.0.0.1:2?security=tls&sni=localhost#Other", activeID, nil)
		must(err)
		must(servers.WriteRecord(d, mainRecord))
	}
	beforeConfig, err := servers.XrayConfig(mainRecord, gen, "startup fixture")
	must(err)
	nextConfig, err := servers.XrayConfig(next, gen, "startup fixture")
	must(err)
	if kind == "inactive-profile" {
		nextConfig = beforeConfig
	}
	write(root.XrayConfig(), beforeConfig)
	write(servers.ActiveFile(d), []byte(activeID+"\n"))
	must(compat.WriteJSON(d.Path("config", "system", "settings.json"), map[string]any{"listenAddress": "127.0.0.1", "socksPort": 2080, "logLevel": "none"}))
	core, err := os.ReadFile(os.Getenv("BRORAY_XRAY_BIN"))
	must(err)
	must(os.WriteFile(root.XrayBinary(), core, 0700))
	j, err := reserve.OpenProfileJournal(root)
	must(err)
	record, err := j.Begin(context.Background(), "startup-profile", compat.SHA256Hex(oldRaw), nextRaw, nextConfig, kind == "committed-running" || kind == "prepared-running", strings.Repeat("a", 64))
	must(err)
	must(j.Install(context.Background(), record))
	must(j.Close())
	if strings.HasPrefix(kind, "committed-") {
		record.Phase = "committed"
	}
	if kind == "bad-native-config" {
		var bad map[string]any
		must(json.Unmarshal(record.Previous.Config.Data, &bad))
		bad["inbounds"] = []any{map[string]any{"protocol": "invalid-native-protocol"}}
		raw, e := json.Marshal(bad)
		must(e)
		record.Previous.Config = reserve.TransactionImage{Exists: true, Data: raw, SHA256: compat.SHA256Hex(raw)}
	}
	journal := filepath.Join(root.Config(), reserve.ProfileTransactionJournalFile)
	must(platform.WriteJSONAtomic(journal, record, 0600))
	if kind == "reconciled-stopped" {
		guard, e := platform.TryLock(root.DaemonLock())
		must(e)
		e = servers.RecoverDPIProfile(compat.WithFence(context.Background()), d, guard)
		releaseErr := guard.Release()
		must(e)
		must(releaseErr)
	}
	if kind == "mixed-journals" || kind == "selection-owner-mismatch" {
		selection := filepath.Join(root.Config(), reserve.TransactionJournalFile)
		write(selection, []byte(`{"unfinished":true}`))
		if kind == "selection-owner-mismatch" {
			must(os.Remove(journal))
			journal = selection
		}
	}
	now := time.Now()
	action := compat.DPIProfileAction
	if kind == "reserve-owner" {
		action = compat.ReserveModeAction
	}
	if kind == "source-owner" {
		action = compat.SourceRefreshAction
	}
	owner := filepath.Join(root.Run(), "global-operation.json")
	if kind != "volatile-owner-lost" {
		must(platform.WriteJSONAtomic(owner, compat.Holder{Scope: "operations", Action: action, BundleID: compat.ReserveOperationTarget, StartedAt: now, UpdatedAt: now, DaemonOwnedReserve: true}, 0600))
	}
	oldOwner, _ := os.ReadFile(owner)
	oldJournal, err := os.ReadFile(journal)
	if kind == "reconciled-stopped" {
		if !os.IsNotExist(err) {
			t.Fatal("fixture recovery did not clear journal", err)
		}
	} else {
		must(err)
	}
	outputPath := filepath.Join(root.Dir, "daemon-output")
	output, err := os.Create(outputPath)
	must(err)
	defer output.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAvailabilityDaemonHelper$")
	cmd.Env = append(os.Environ(), "BRORAY_AVAILABILITY_CHILD=1", "BRORAY_AVAILABILITY_ROOT="+root.Dir, "BRORAY_AVAILABILITY_PORT="+strconv.Itoa(port))
	cmd.Stdout = output
	cmd.Stderr = output
	must(cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	defer func() {
		if exited {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = control.Shutdown(ctx, root)
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	refused := kind == "reserve-owner" || kind == "source-owner" || kind == "mixed-journals" || kind == "selection-owner-mismatch" || kind == "bad-native-config"
	if refused {
		select {
		case err := <-done:
			exited = true
			if err == nil {
				t.Fatal("untrusted recovery accepted")
			}
		case <-time.After(15 * time.Second):
			t.Fatal("untrusted recovery did not stop startup")
		}
		if _, err := os.Lstat(root.DaemonSocket()); !os.IsNotExist(err) {
			t.Fatal("untrusted recovery opened listener", err)
		}
		got, err := os.ReadFile(owner)
		if err != nil || !bytes.Equal(got, oldOwner) {
			t.Fatal("untrusted owner changed", err)
		}
		got, err = os.ReadFile(journal)
		if err != nil || !bytes.Equal(got, oldJournal) {
			t.Fatal("untrusted journal changed", err)
		}
		if kind != "bad-native-config" {
			got, err = os.ReadFile(servers.RecordPath(d, "startup-profile"))
			if err != nil || !bytes.Equal(got, record.Next.Record.Data) {
				t.Fatal("wrong owner restored profile", err)
			}
		}
		return
	}
	running := kind == "committed-running" || kind == "prepared-running" || kind == "inactive-profile"
	bootMessage := []byte("DPI_RECOVERED_PROFILE_STOPPED")
	if running {
		bootMessage = []byte("xray: started at boot")
	}
	deadline := time.Now().Add(25 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			exited = true
			raw, _ := os.ReadFile(outputPath)
			t.Fatalf("recovery failed before startup: %v %s", err, raw)
		default:
		}
		raw, e := os.ReadFile(outputPath)
		if _, se := os.Lstat(root.DaemonSocket()); se == nil && e == nil && bytes.Contains(raw, bootMessage) {
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		raw, _ := os.ReadFile(outputPath)
		t.Fatalf("daemon recovery/boot decision unconfirmed: %s", raw)
	}
	want := record.Previous
	if record.Phase == "committed" {
		want = record.Next
	}
	for path, expected := range map[string][]byte{servers.RecordPath(d, "startup-profile"): want.Record.Data, root.XrayConfig(): want.Config.Data, servers.ActiveFile(d): []byte(activeID + "\n")} {
		got, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(got, expected) {
			t.Fatal("wrong recovered bytes", path, e)
		}
	}
	pending, err := reserve.TransactionPending(context.Background(), root)
	if err != nil || pending {
		t.Fatal("journal retained", err)
	}
	if _, err := os.Lstat(owner); !os.IsNotExist(err) {
		t.Fatal("recovered owner retained", err)
	}
	_, err = os.Lstat(filepath.Join(root.Config(), reserve.ProfileBootHoldFile))
	if running && !os.IsNotExist(err) || !running && err != nil {
		t.Fatal("wrong durable boot intent", err)
	}
	child, err := platform.TryLock(filepath.Join(root.Run(), "xray-child.lock"))
	if child != nil {
		must(child.Release())
	}
	if running && !errors.Is(err, platform.ErrLocked) || !running && err != nil {
		t.Fatal("wrong actual main child state", err)
	}
}

var dpiStartupPorts = struct {
	sync.Mutex
	next int
}{next: 10000}

// These isolated Linux fixtures hand the port to a subsequently started core.
// Automatic port allocation must not steal it in between. Do not reuse an old
// fixture port, which may still belong to a previous connection or TIME_WAIT.
func dpiStartupPort(t *testing.T) int {
	t.Helper()
	dpiStartupPorts.Lock()
	defer dpiStartupPorts.Unlock()
	raw, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		t.Fatal(err)
	}
	var low, high int
	if n, err := fmt.Sscanf(string(raw), "%d %d", &low, &high); err != nil || n != 2 || low < 1 || high > 65535 || low > high {
		t.Fatal("invalid automatic port range", err)
	}
	for dpiStartupPorts.next <= 65535 {
		port := dpiStartupPorts.next
		dpiStartupPorts.next++
		if port >= low && port <= high {
			continue
		}
		l, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		return port
	}
	t.Fatal("isolated fixture port pool exhausted")
	return 0
}

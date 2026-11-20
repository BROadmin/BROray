//go:build linux && !brorayfake

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
)

func TestReserveDaemonRecoversBeforeAreaStartup(t *testing.T) {
	for _, action := range []string{"enable", "disable"} {
		t.Run(action, func(t *testing.T) { testReserveDaemonRecovery(t, action) })
	}
}

func TestSourceDaemonRecoversBeforeAreaStartup(t *testing.T) {
	for _, action := range []string{"source-replace", "source-withdraw"} {
		t.Run(action, func(t *testing.T) { testReserveDaemonRecovery(t, action) })
	}
}

func TestSourceRefreshDaemonRecoversBeforeAreaStartup(t *testing.T) {
	for _, action := range []string{"source-replace", "source-withdraw"} {
		t.Run(action, func(t *testing.T) { testReserveDaemonRecovery(t, action, compat.SourceRefreshAction) })
	}
}

func TestSourceRefreshSameIDDaemonRecoversBeforeAreaStartup(t *testing.T) {
	testReserveDaemonRecovery(t, "source-reconfigure", compat.SourceRefreshAction)
}

func TestSourceCatalogPendingBlocksDaemonStartup(t *testing.T) {
	testReserveDaemonRecovery(t, "source-orphan")
}

func TestReserveDaemonOrphanProbeBlocksStartup(t *testing.T) {
	testReserveDaemonRecovery(t, "orphan")
}

func testReserveDaemonRecovery(t *testing.T, action string, ownerOverride ...string) {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root.Data(), "config", "system"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	oldConfig := []byte(`{"old":true}`)
	oldPrefs := []byte(`{"schemaVersion":1,"enabled":false,"primaryServerId":"old","backupServerIds":["next"]}`)
	nextPrefs := []byte(`{"schemaVersion":1,"enabled":true,"primaryServerId":"old","backupServerIds":["next"]}`)
	oldDescription, nextDescription := "BROray - old", "BROray - mix-auto"
	nextConfig, nextActive := []byte(`{"next":true}`), []byte("next\n")
	if action == "disable" {
		oldDescription, nextDescription = "BROray - mix-auto", "BROray - old"
		oldPrefs = []byte(`{"schemaVersion":1,"enabled":true,"primaryServerId":"next","backupServerIds":["old"]}`)
		nextPrefs = []byte(`{"schemaVersion":1,"enabled":false,"primaryServerId":"next","backupServerIds":["old"]}`)
		nextConfig, nextActive = oldConfig, []byte("old\n")
	}
	sourceAction := action == "source-replace" || action == "source-withdraw" || action == "source-reconfigure"
	if sourceAction {
		nextPrefs = oldPrefs
		nextDescription = "BROray - next"
		if action == "source-withdraw" {
			nextDescription = oldDescription
			nextConfig, nextActive = nil, nil
		}
	}
	if action == "source-reconfigure" {
		nextActive = []byte("old\n")
	}
	write(root.XrayConfig(), oldConfig)
	write(filepath.Join(root.Data(), "config", "system/server-reserve.json"), oldPrefs)
	write(filepath.Join(root.Data(), "config", "active-server"), []byte("old\n"))
	block := func(desc string) []string {
		return []string{"interface Proxy3", `    description "` + desc + `"`, "    security-level public", "    proxy protocol socks5", "    proxy upstream 192.168.1.1 2080", "    up"}
	}
	oldBlock := block(oldDescription)
	sum := sha256.Sum256([]byte(strings.Join(oldBlock, "\n") + "\n"))
	hash := hex.EncodeToString(sum[:])
	receipt, err := json.Marshal(map[string]any{"schemaVersion": 2, "contract": "r14c38-proxy-owned-interface/1", "owner": "BROray", "interfaceName": "Proxy3", "protocol": "socks5", "upstream": map[string]any{"host": "192.168.1.1", "port": 2080}, "description": oldDescription, "selectionMode": "auto", "writeProtocolSha256": "dc8501856b27260738828e9959e10512496410ff44adfceaa8564056bed1ddfa", "runningBlockSha256": hash, "startupBlockSha256": hash})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(root.Data(), "config", "interface.json"), receipt)
	j, err := reserve.OpenTransactionJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	intentAction := action
	if action == "source-reconfigure" {
		intentAction = "source-replace"
	}
	if action == "orphan" || action == "source-orphan" {
		intentAction = "enable"
	}
	id, err := j.Begin(context.Background(), reserve.TransactionFiles{Config: nextConfig, Preferences: nextPrefs, Active: nextActive}, reserve.TransactionIntent{Action: intentAction, Authority: strings.Repeat("a", 64), InterfaceName: "Proxy3", Upstream: "192.168.1.1:2080", PreviousDescription: oldDescription, PreviousDescriptionLine: oldBlock[1], NextDescription: nextDescription, ReceiptSHA256: compat.SHA256Hex(receipt)})
	if err != nil {
		t.Fatal(err)
	}
	if sourceAction {
		if err := j.InstallConfig(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	} else {
		write(root.XrayConfig(), nextConfig)
	}
	if err := j.InstallSelection(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	global := filepath.Join(root.Run(), "global-operation.json")
	ownerAction := compat.ReserveModeAction
	if sourceAction {
		ownerAction = compat.SourceRemovalAction
	}
	if len(ownerOverride) == 1 {
		ownerAction = ownerOverride[0]
	}
	if err := platform.WriteJSONAtomic(global, compat.Holder{Scope: "operations", Action: ownerAction, BundleID: compat.ReserveOperationTarget, StartedAt: now, UpdatedAt: now, DaemonOwnedReserve: true}, 0600); err != nil {
		t.Fatal(err)
	}
	oldOwner, _ := os.ReadFile(global)
	var mu sync.Mutex
	running, saved := nextDescription, nextDescription
	var commands []string
	premature := false
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rci/show/running-config":
			_ = json.NewEncoder(w).Encode(map[string]any{"message": append(block(running), "!")})
		case "/rci/more/startup-config":
			_ = json.NewEncoder(w).Encode(map[string]any{"message": append(block(saved), "!")})
		case "/rci/":
			var req []struct {
				Parse string `json:"parse"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				http.Error(w, "bad", 400)
				return
			}
			var out []any
			for _, item := range req {
				commands = append(commands, item.Parse)
				if _, err := os.Lstat(root.DaemonSocket()); err == nil {
					premature = true
				}
				owner, err := os.ReadFile(global)
				if err != nil || !bytes.Equal(owner, oldOwner) {
					premature = true
				}
				lock, err := platform.TryLock(filepath.Join(root.Run(), "xray-child.lock"))
				if err == nil {
					lock.Release()
					premature = true
				}
				status := []any{}
				switch item.Parse {
				case `interface Proxy3 description "` + oldDescription + `"`:
					running = oldDescription
				case "system configuration save":
					saved = running
				default:
					status = append(status, map[string]any{"status": "error", "message": "unexpected write"})
				}
				out = append(out, map[string]any{"parse": map[string]any{"status": status}})
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer router.Close()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cfg := platform.DefaultConfig()
	cfg.ListenAddress = "127.0.0.1"
	cfg.WebPort = port
	cfg.Keenetic.RCIURL = router.URL
	cfg.Keenetic.WriteTransport = "rci"
	if err := platform.SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.XrayBinary(), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(root.Dir, "daemon-output"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	orphan := filepath.Join(root.Tmp(), "reserve-probe-uncertain")
	if action == "source-orphan" {
		orphan = filepath.Join(root.Tmp(), "server-subscription-sync.1.orphan")
	}
	if action == "orphan" || action == "source-orphan" {
		if err := os.Mkdir(orphan, 0700); err != nil {
			t.Fatal(err)
		}
		if action == "source-orphan" {
			write(filepath.Join(orphan, "rollback-required"), []byte("source backup retained"))
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAvailabilityDaemonHelper$")
	cmd.Env = append(os.Environ(), "BRORAY_AVAILABILITY_CHILD=1", "BRORAY_AVAILABILITY_ROOT="+root.Dir, "BRORAY_AVAILABILITY_PORT="+strconv.Itoa(port))
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
		_ = control.Shutdown(ctx, root)
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	ready := false
	if action == "orphan" || action == "source-orphan" {
		select {
		case err := <-done:
			exited = true
			if err == nil {
				t.Fatal("orphan startup accepted")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("orphan startup did not fail closed")
		}
		if _, err := os.Lstat(root.DaemonSocket()); !os.IsNotExist(err) {
			t.Fatal("orphan startup opened socket", err)
		}
		if _, err := os.Stat(orphan); err != nil {
			t.Fatal("orphan deleted", err)
		}
		got, err := os.ReadFile(global)
		if err != nil || !bytes.Equal(got, oldOwner) {
			t.Fatal("orphan recovery changed fence", err)
		}
		return
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			exited = true
			log, _ := os.ReadFile(filepath.Join(root.Dir, "daemon-output"))
			t.Fatalf("pending recovery did not reach startup: %v %s", err, log)
		default:
		}
		if _, err := os.Lstat(root.DaemonSocket()); err == nil {
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("recovered daemon did not open control socket")
	}
	for path, want := range map[string][]byte{root.XrayConfig(): oldConfig, filepath.Join(root.Data(), "config", "system/server-reserve.json"): oldPrefs, filepath.Join(root.Data(), "config", "active-server"): []byte("old\n"), filepath.Join(root.Data(), "config", "interface.json"): receipt} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("startup preceded restoration", path, err)
		}
	}
	if pending, err := reserve.TransactionPending(context.Background(), root); err != nil || pending {
		t.Fatal("recovered journal retained", err)
	}
	mu.Lock()
	defer mu.Unlock()
	wantCommands := 2
	if action == "source-withdraw" {
		wantCommands = 0
	}
	if premature || running != oldDescription || saved != oldDescription || len(commands) != wantCommands {
		t.Fatalf("recovery ordering or writes: early=%v running=%s saved=%s commands=%v", premature, running, saved, commands)
	}
}

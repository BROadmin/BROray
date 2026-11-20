package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDPIQueueNativeHTTPS(t *testing.T) {
	mode := os.Getenv("BRORAY_DPI_QUEUE_CASE")
	if mode == "" {
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		for _, kind := range []string{"on", "rollback", "unknown"} {
			t.Run(kind, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, exe, "-test.run=^TestDPIQueueNativeHTTPS$", "-test.v")
				cmd.Env = append(os.Environ(), "BRORAY_DPI_QUEUE_CASE="+kind)
				data, e := cmd.CombinedOutput()
				t.Logf("isolated production probe child:\n%s", data)
				if e != nil {
					t.Fatal(e)
				}
			})
		}
		return
	}
	family := "tcp4"
	f := newReserveRuntimeFixture(t)
	control, _ := dpiProposalControl(t)
	cert, key, pin := selfSigned(t)
	port := freeTCP(t)
	cfg, err := json.Marshal(map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"inbounds":  []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "trojan", "settings": map[string]any{"clients": []any{map[string]any{"password": "synthetic-dpi"}}}, "streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}}}}}},
		"outbounds": []any{map[string]any{"protocol": "freedom", "sendThrough": "127.0.0.3", "settings": map[string]any{"redirect": control.Listener.Addr().String(), "finalRules": []any{map[string]any{"action": "allow", "ip": []any{"127.0.0.0/8"}}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	startXray(t, os.Getenv("BRORAY_XRAY_BIN"), filepath.Join(t.TempDir(), "server.json"), cfg, port, nil)
	host := "127.0.0.1"
	if family == "tcp6" {
		host = "::1"
	}
	endpoint, records := dpiTLSRecordRelay(t, family, net.JoinHostPort(host, "0"), net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	id := f.e.addServer(t, "trojan://synthetic-dpi@"+endpoint+"?type=tcp&security=tls&sni=localhost&pcs="+pin+"#DPI")
	rec, err := ReadRecord(f.e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "off" {
		rec, err = prepareDPIRecord(rec, true)
		if err != nil {
			t.Fatal(err)
		}
		if err = WriteRecord(f.e.d, rec); err != nil {
			t.Fatal(err)
		}
		rec, err = ReadRecord(f.e.d, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	path := RecordPath(f.e.d, id)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	guard, e := platform.TryLock(f.e.d.Root.DaemonLock())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { guard.Release() })
	f.e.d.DaemonGuard = guard
	writeReserveAutomatic(t, f.e, id, f.backup, false)
	if e = os.WriteFile(ActiveFile(f.e.d), []byte(id+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = compat.WriteJSON(f.e.d.Path("config", "interface.json"), map[string]any{"fixture": "owned"}); e != nil {
		t.Fatal(e)
	}
	initial, e := XrayConfig(rec, f.e.s.genSettings(f.ctx), path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.process.ReplaceIfCurrent(f.ctx, f.process.RuntimeSnapshot(), func() error { return platform.WriteFileAtomic(f.e.d.Root.XrayConfig(), initial, 0600) }); e != nil {
		t.Fatal(e)
	}
	endpointPrimary, e := netip.ParseAddrPort("127.0.0.1:" + fmt.Sprint(f.e.s.genSettings(f.ctx).SocksPort))
	if e != nil {
		t.Fatal(e)
	}
	targetPrimary := compat.OwnedInterfaceTarget{Name: "Proxy0", Upstream: endpointPrimary, DescriptionLine: `    description "BROray - DPI"`}
	f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) { return "BROray - DPI", nil }
	f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(context.Context, string) (compat.OwnedInterfaceTarget, error) { return targetPrimary, nil }
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = f.e.d.Svc.KeeneticVerifyInterfaceConfiguration
	deadline := time.Now().Add(5 * time.Second)
	for {
		identity, e := reserve.CaptureProcess(f.process.RuntimeSnapshot().PID, f.e.d.Root.XrayBinary())
		if e == nil {
			if _, e = reserve.VerifyListener(identity, endpointPrimary); e == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("initial binding", e)
		}
		time.Sleep(20 * time.Millisecond)
	}

	rootsPath := filepath.Join(t.TempDir(), "fixture-ca.pem")
	if e = os.WriteFile(rootsPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: control.TLS.Certificates[0].Certificate[0]}), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SSL_CERT_FILE", rootsPath)
	t.Setenv("SSL_CERT_DIR", t.TempDir())
	original := control.Config.Handler
	var hits atomic.Int32
	control.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Host == "www.gstatic.com" && hits.Add(1) == 2 {
			if mode == "rollback" {
				w.WriteHeader(503)
				return
			}
			if mode == "unknown" {
				if e := platform.WriteFileAtomic(f.e.d.Path("config", "interface.json"), []byte(`{"fixture":"replaced"}`), 0600); e != nil {
					t.Error(e)
				}
			}
		}
		original.ServeHTTP(w, request)
	})
	f.release()
	if e = f.e.d.Lock.ReconcileDaemonReserve(f.e.d.Root, guard, false, nil); e != nil {
		t.Fatal(e)
	}
	snapshot := f.process.RuntimeSnapshot()
	nonce := strings.Repeat("e", 32)
	request, _ := json.Marshal(map[string]any{"id": id, "enabled": true, "expectedRevision": compat.SHA256Hex(before), "nonce": nonce})
	response := call(t, "POST", "http://example.com/api/servers/dpi-profile.cgi", string(request), map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"})
	if response.Code != 202 || f.process.RuntimeSnapshot() != snapshot {
		t.Fatal("admission", response.Code, response.Raw)
	}
	go f.e.d.Queue.Run(f.e.d.Ctx)
	deadline = time.Now().Add(30 * time.Second)
	for {
		row, ok := f.e.d.Queue.Request("q-" + nonce)
		if ok && row.State != "queued" && row.State != "running" {
			if mode == "on" && row.State != "completed" {
				t.Fatal("public apply", row)
			}
			if mode == "rollback" && (row.State != "failed" || row.ErrorCode != "DPI_ROLLED_BACK") {
				t.Fatal("public rollback", row)
			}
			if mode == "unknown" && (row.State != "failed" || row.ErrorCode != "RESERVE_TRANSACTION_RECOVERY_REQUIRED") {
				t.Fatal("public unknown", row)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("public queue timed out")
		}
		time.Sleep(25 * time.Millisecond)
	}
	pending, e := reserve.TransactionPending(context.Background(), f.e.d.Root)
	if e != nil || pending != (mode == "unknown") || f.e.d.Lock.Busy() != (mode == "unknown") {
		t.Fatal("owner", pending, e, f.e.d.Lock.Busy())
	}
	if hits.Load() != 2 || records.Load() < 2 {
		t.Fatal("production HTTPS probe absent", hits.Load(), records.Load())
	}
	after, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if mode == "rollback" {
		config, e := os.ReadFile(f.e.d.Root.XrayConfig())
		if e != nil || !bytes.Equal(after, before) || !bytes.Equal(config, initial) {
			t.Fatal("rollback bytes", e)
		}
	}
	current, e := ReadRecord(f.e.d, id)
	if e != nil {
		t.Fatal(e)
	}
	enabled, e := ManagedDPI(current)
	if e != nil || enabled != (mode != "rollback") {
		t.Fatal("saved mode", enabled, e)
	}
	t.Logf("actual registered DPI handler + protected queue + production reserve.Probe: %s", mode)
}

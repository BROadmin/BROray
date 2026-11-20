package servers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestDPILiveNativeHTTPS(t *testing.T) {
	for _, family := range []string{"tcp4", "tcp6"} {
		for _, mode := range []string{"on", "off", "rollback"} {
			t.Run(family+"/"+mode, func(t *testing.T) {
				f := newReserveRuntimeFixture(t)
				control, roots := dpiProposalControl(t)
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

				snapshot := f.process.RuntimeSnapshot()
				calls := 0
				err = f.e.s.applyDPIWith(f.ctx, id, compat.SHA256Hex(before), mode != "off", func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
					calls++
					start := time.Now()
					proof, err := reserve.VerifyProbeTarget(target)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "rollback" && calls == 2 {
						control.Close()
					}
					conn, err := dialTestSOCKS(target.Endpoint.String(), "www.gstatic.com:443")
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					if err = conn.SetDeadline(time.Now().Add(8 * time.Second)); err != nil {
						t.Fatal(err)
					}
					secure := tls.Client(conn, &tls.Config{RootCAs: roots, ServerName: "www.gstatic.com", MinVersion: tls.VersionTLS12})
					if err = secure.HandshakeContext(ctx); err != nil {
						if mode == "rollback" && calls == 2 {
							after, e := reserve.VerifyProbeTarget(target)
							if e != nil || after != proof {
								t.Fatal("failed connection proof", e)
							}
							return reserve.ProbeOutcome{Verdict: "unhealthy", Identity: proof, StartedAt: start, FinishedAt: time.Now()}
						}
						t.Fatal("real proxied TLS", err)
					}
					req, err := http.NewRequestWithContext(ctx, "GET", "https://www.gstatic.com/generate_204", nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Close = true
					if err = req.Write(secure); err != nil {
						t.Fatal(err)
					}
					response, err := http.ReadResponse(bufio.NewReader(secure), req)
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					if response.StatusCode != 204 || response.Header.Get("X-DPI-Egress") != "127.0.0.3" {
						t.Fatal("proxy HTTPS/egress absent", response.Status, response.Header)
					}
					after, err := reserve.VerifyProbeTarget(target)
					if err != nil || after != proof {
						t.Fatal("proof changed", err)
					}
					verdict := "healthy"
					if mode == "rollback" && calls == 2 {
						t.Fatal("closed HTTPS fixture unexpectedly accepted live TLS")
					}
					return reserve.ProbeOutcome{Verdict: verdict, Identity: proof, StartedAt: start, FinishedAt: time.Now()}
				})
				if mode == "rollback" {
					if !errors.Is(err, errDPIApplyRolledBack) {
						t.Fatal("rollback", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if calls != 2 {
					t.Fatal("detached and live HTTPS proof required", calls)
				}
				pending, e := reserve.TransactionPending(f.ctx, f.e.d.Root)
				if e != nil || pending {
					t.Fatal("pending", e, pending)
				}
				after, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				current, e := ReadRecord(f.e.d, id)
				if e != nil {
					t.Fatal(e)
				}
				enabled, e := ManagedDPI(current)
				if e != nil {
					t.Fatal(e)
				}
				if mode == "rollback" {
					cfg, e := os.ReadFile(f.e.d.Root.XrayConfig())
					if e != nil || !bytes.Equal(cfg, initial) || !bytes.Equal(before, after) || enabled {
						t.Fatal("rollback images", e)
					}
				} else if enabled != (mode == "on") {
					t.Fatal("saved mode", enabled)
				}
				if f.process.RuntimeSnapshot() == snapshot {
					t.Fatal("main generation not replaced")
				}
				if (mode == "off" && records.Load() != 1) || (mode != "off" && records.Load() < 2) {
					t.Fatal("actual TLS traffic absent")
				}
				t.Logf("native %s %s: real detached HTTPS204; live HTTPS204 or confirmed TLS failure with rollback, no physical router", family, mode)
			})
		}
	}
}

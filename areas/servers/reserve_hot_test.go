package servers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

func reserveHotPeer(t *testing.T, marker string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, marker)
	}))
	t.Cleanup(server.Close)
	destination := server.Listener.Addr().String()
	_, targetPort, err := net.SplitHostPort(destination)
	if err != nil {
		t.Fatal(err)
	}
	const uuid = "947dee4b-019c-4489-8b63-82853bb5f08c"
	port := freeTCP(t)
	config, err := json.Marshal(map[string]any{
		"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "vless",
			"settings": map[string]any{"decryption": "none", "clients": []any{map[string]any{"id": uuid}}}}},
		"outbounds": []any{map[string]any{"protocol": "freedom", "settings": map[string]any{
			"redirect": destination,
			"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": targetPort},
				map[string]any{"action": "block"}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/peer.json"
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, os.Getenv("BRORAY_XRAY_BIN"), "run", "-c", path)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("loopback VLESS peer did not start", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Sprintf("vless://%s@127.0.0.1:%d?security=none&type=tcp#%s", uuid, port, marker)
}

func reserveHotPayload(ctx context.Context, endpoint netip.AddrPort) (string, error) {
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "socks5h", Host: endpoint.String()}), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://cp.cloudflare.com:443/check", nil)
	if err != nil {
		return "", err
	}
	response, err := (&http.Client{Transport: transport, Timeout: time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8))
	return string(raw), err
}

func TestHotReserveQueue(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "commit"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			a := reserveHotPeer(t, "A")
			b := reserveHotPeer(t, "B")
			f, _ := reserveModeQueueFromRuntime(t, newReserveRuntimeFixtureWithLinks(t, a, b))
			f.e.d.Log = log.New(os.Stderr, "hot queue fixture ", 0)
			primary := f.plan.node.ID
			f.e.s.reserveHotExperiment = true
			f.e.s.reserveHotProbe = func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
				start := time.Now()
				identity, err := reserve.VerifyProbeTarget(target)
				if err != nil {
					t.Logf("owned probe target unavailable: %v", err)
					return reserve.ProbeOutcome{Verdict: "unknown", StartedAt: start, FinishedAt: time.Now()}
				}
				expected := "A"
				if ActiveID(f.e.d) == f.backup {
					expected = "B"
					if rollback {
						expected = "wrong"
					}
				}
				payload, err := reserveHotPayload(ctx, target.Endpoint)
				t.Logf("hot slot probe want=%q got=%q err=%v", expected, payload, err)
				verdict := "unhealthy"
				if err == nil && payload == expected {
					verdict = "healthy"
				}
				return reserve.ProbeOutcome{Verdict: verdict, Identity: identity, StartedAt: start, FinishedAt: time.Now()}
			}
			// The manual preflight sample is synthetic. Transaction confirmation
			// above must pass an actual request through the selected Xray slot.
			f.e.s.reserveSelectionProbe = func(ctx context.Context, id string) (reserve.Sample, error) {
				data, err := os.ReadFile(f.e.d.Root.XrayConfig())
				if err != nil {
					return reserve.Sample{}, err
				}
				endpoint, ok := reserve.DiagnosticEndpoint(data)
				if !ok {
					return reserve.Sample{}, errReserveRuntime
				}
				plan, err := f.e.s.prepareReserveRuntime(ctx, id, endpoint)
				now := time.Now()
				return reserve.Sample{ServerID: id, Fingerprint: plan.node.Fingerprint, Identity: "ready",
					Verdict: "healthy", Verified: true, StartedAt: now, FinishedAt: now}, err
			}
			prefs, _ := readReservePreferences(f.e.d)
			nonce := strings.Repeat("2f", 16)
			if r := reserveModeRequest(t, true, prefs.Revision, nonce); r.Code != http.StatusAccepted {
				t.Fatal(r.Raw)
			}
			go f.e.d.Queue.Run(f.e.d.Ctx)
			waitReq(t, f.e, "q-"+nonce, "completed")
			snapshot := f.process.RuntimeSnapshot()
			path := f.e.d.Root.XrayConfig()
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			config, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			endpoint, ok := reserve.DiagnosticEndpoint(config)
			if _, hot := reserve.HotLayoutSocket(config); !ok || !hot {
				t.Fatal("expected a managed selector")
			}
			if got, err := reserveHotPayload(f.e.d.Ctx, endpoint); err != nil || got != "A" {
				t.Fatal("initial real payload", got, err)
			}
			verifier, ok := any(f.process).(interface {
				VerifyReserveOutboundIfCurrent(context.Context, xray.RuntimeSnapshot, string) error
			})
			if !ok {
				t.Fatal("missing read-only verification of the hot selector")
			}
			if err := verifier.VerifyReserveOutboundIfCurrent(f.e.d.Ctx, snapshot, reserve.HotSlot(0)); err != nil {
				t.Fatal("owned selector was not observed", err)
			}
			if err := verifier.VerifyReserveOutboundIfCurrent(f.e.d.Ctx, snapshot, reserve.HotSlot(1)); err == nil {
				t.Fatal("mismatched hot slot was accepted")
			}
			stale := snapshot
			stale.Intent++
			if err := verifier.VerifyReserveOutboundIfCurrent(f.e.d.Ctx, stale, reserve.HotSlot(0)); err == nil {
				t.Fatal("obsolete supervisor snapshot was accepted")
			}
			if got, err := reserveHotPayload(f.e.d.Ctx, endpoint); err != nil || got != "A" || f.process.RuntimeSnapshot() != snapshot {
				t.Fatal("read-back changed the selected VPN or the core", got, err)
			}
			prefs, _ = readReservePreferences(f.e.d)
			hash, err := fileSHA256(RecordPath(f.e.d, f.backup))
			if err != nil {
				t.Fatal(err)
			}
			r, err := f.e.d.Queue.Enqueue(compat.ReserveSelectAction, f.backup, "USER", prefs.Revision+":"+hash)
			if err != nil {
				t.Fatal(err)
			}
			state, selected, marker := "completed", f.backup, "B"
			if rollback {
				state, selected, marker = "failed", primary, "A"
			}
			deadline := time.Now().Add(25 * time.Second)
			for {
				if row, ok := f.e.d.Queue.Request(r.RequestID); ok && row.State != "queued" && row.State != "running" {
					if row.State != state {
						t.Fatal("unexpected queue result", row.State, row.Message)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("hot selection did not finish")
				}
				time.Sleep(15 * time.Millisecond)
			}
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(before, after) || f.process.RuntimeSnapshot() != snapshot || ActiveID(f.e.d) != selected {
				t.Fatal("active slot changed without preserving owned runtime", err)
			}
			if pending, err := reserve.TransactionPending(f.e.d.Ctx, f.e.d.Root); err != nil || pending {
				t.Fatal("transaction retained after proven commit or rollback", pending, err)
			}
			if got, err := reserveHotPayload(f.e.d.Ctx, endpoint); err != nil || got != marker {
				t.Fatal("resulting real payload", got, err)
			}
			if rollback {
				failures, _ := f.e.d.Queue.Journal().ReportEvents()["failures"].([]map[string]any)
				if len(failures) != 1 || failures[0]["operationId"] != "op-"+r.RequestID ||
					failures[0]["configurationRollback"] != "restored" || failures[0]["vpnRecovery"] != "verified" {
					t.Fatal("actual rollback proof missing from journal", failures)
				}
				return
			}
			if err := f.process.Restart(f.e.d.Ctx); err != nil {
				t.Fatal(err)
			}
			deadline = time.Now().Add(4 * time.Second)
			for {
				snap := f.process.RuntimeSnapshot()
				identity, err := reserve.CaptureProcess(snap.PID, f.e.d.Root.XrayBinary())
				if err == nil {
					if _, err := reserve.VerifyListener(identity, endpoint); err == nil {
						break
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("new generation did not expose diagnostic listener")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if got, err := reserveHotPayload(f.e.d.Ctx, endpoint); err == nil || got != "" {
				t.Fatal("cold generation sent VPN traffic before rebind", got, err)
			}
			probe, err := f.e.d.Queue.Enqueue(compat.ReserveSwitchAction, compat.ReserveOperationTarget, "RESERVE_MONITOR", prefs.Revision)
			if err != nil {
				t.Fatal(err)
			}
			waitReq(t, f.e, probe.RequestID, "completed")
			if got, err := reserveHotPayload(f.e.d.Ctx, endpoint); err != nil || got != marker {
				t.Fatal("cold monitor failed to restore the durable server", got, err)
			}
			snapshot = f.process.RuntimeSnapshot()
			nonce = strings.Repeat("3c", 16)
			if r := reserveModeRequest(t, false, prefs.Revision, nonce); r.Code != http.StatusAccepted {
				t.Fatal(r.Raw)
			}
			waitReq(t, f.e, "q-"+nonce, "completed")
			prefs, err = readReservePreferences(f.e.d)
			if err != nil || prefs.Enabled || ActiveID(f.e.d) != selected || f.process.RuntimeSnapshot() == snapshot {
				t.Fatal("live disable did not return to the same ordinary server", err)
			}
			config, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, hot := reserve.HotLayoutSocket(config); hot {
				t.Fatal("ordinary configuration retained the hot selector")
			}
			if err := f.process.Restart(f.e.d.Ctx); err != nil {
				t.Fatal(err)
			}
			primaryEndpoint := commitFixtureTarget(t, f).Upstream
			deadline = time.Now().Add(4 * time.Second)
			for {
				if got, err := reserveHotPayload(f.e.d.Ctx, primaryEndpoint); err == nil && got == marker {
					break
				} else if time.Now().After(deadline) {
					t.Fatal("ordinary restart did not restore user traffic", got, err)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

func TestHotReserveAutomaticSwitch(t *testing.T) {
	f := newReserveRuntimeFixtureWithLinks(t, reserveHotPeer(t, "A"), reserveHotPeer(t, "B"))
	s := f.e.s
	primary := f.plan.node.ID
	backup, err := s.prepareReserveStockRuntime(f.ctx, f.backup, f.plan.endpoint, false)
	if err != nil {
		t.Fatal(err)
	}
	hot, err := s.buildReserveHotPlan(f.plan, []string{primary, f.backup},
		map[string]reserveRuntimePlan{primary: f.plan, f.backup: backup})
	if err != nil {
		t.Fatal(err)
	}
	path := f.e.d.Root.XrayConfig()
	snap, err := f.process.ReplaceIfCurrent(f.ctx, f.process.RuntimeSnapshot(), func() error {
		return platform.WriteFileAtomicNoFollow(path, hot.config, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.plan = hot
	deadline := time.Now().Add(4 * time.Second)
	for {
		f.binding, err = s.bindReserveRuntime(f.ctx, hot, nil)
		if err == nil && f.binding.snapshot == snap {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic switch fixture slot did not bind", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	_, monitor, result := reserveSwitchFixtureFromRuntime(t, f)
	s.reserveHotProbe = func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
		start := time.Now()
		identity, err := reserve.VerifyProbeTarget(target)
		if err != nil {
			return reserve.ProbeOutcome{Verdict: "unknown", StartedAt: start, FinishedAt: time.Now()}
		}
		payload, err := reserveHotPayload(ctx, target.Endpoint)
		verdict := "unhealthy"
		if err == nil && payload == "B" {
			verdict = "healthy"
		}
		return reserve.ProbeOutcome{Verdict: verdict, Identity: identity, StartedAt: start, FinishedAt: time.Now()}
	}
	if err := s.switchReserve(f.ctx, monitor, result); err != nil {
		t.Fatal("automatic hot switch", err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || f.process.RuntimeSnapshot() != snap ||
		ActiveID(f.e.d) != f.backup || monitor.state.Pending != nil || monitor.state.Reason != "switched" {
		t.Fatal("automatic switch did not commit with the same process and config", err)
	}
	if pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root); err != nil || pending {
		t.Fatal("automatic switch journal", pending, err)
	}
}

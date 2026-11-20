package servers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func commitFixtureTarget(t *testing.T, f reserveRuntimeFixture) compat.OwnedInterfaceTarget {
	t.Helper()
	var settings struct {
		SocksPort uint16 `json:"socksPort"`
	}
	raw, err := os.ReadFile(settingsFile(f.e.d))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	return compat.OwnedInterfaceTarget{Name: "Proxy3", Upstream: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), settings.SocksPort)}
}

func TestReserveCommitBindsMainSocketAndRouterWithoutRestart(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	target := commitFixtureTarget(t, f)
	before := f.process.RuntimeSnapshot()
	calls := 0
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(ctx context.Context, description string) (compat.OwnedInterfaceTarget, error) {
		if !compat.FenceHeld(ctx) || description != "BROray - mix-auto" {
			t.Fatal("wrong verification request")
		}
		calls++
		return target, nil
	}
	got, err := f.e.s.verifyReserveCommit(f.ctx, f.binding, "BROray - mix-auto")
	if err != nil || got.Interface != target || got.Listener.Process != f.binding.target.Process || got.Listener.Endpoint != target.Upstream || got.Listener.SocketInode == 0 {
		t.Fatalf("main socket commit proof: %+v %v", got, err)
	}
	if calls < 2 || f.process.RuntimeSnapshot() != before {
		t.Fatal("missing repeated readback or verification restarted core")
	}
	// Verification proves composition, not Internet health: this core's actual
	// upstream is unavailable. The production traffic check must still fail.
	if sample := f.e.s.probeReserveRuntime(f.ctx, f.binding); !sample.Verified || sample.Verdict != "unhealthy" {
		t.Fatalf("traffic evidence: %+v", sample)
	}
}

func TestReserveCommitRefusesUnownedAndMixedTargets(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	good := commitFixtureTarget(t, f)
	foreign, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	foreignTarget := netip.MustParseAddrPort(foreign.Addr().String())
	for _, tc := range []string{"missing", "error", "empty", "diagnostic", "foreign", "changed-name", "changed-endpoint", "late-error", "unfenced"} {
		t.Run(tc, func(t *testing.T) {
			calls := 0
			f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(context.Context, string) (compat.OwnedInterfaceTarget, error) {
				calls++
				target := good
				switch tc {
				case "error":
					return target, errors.New("router unavailable")
				case "empty":
					return compat.OwnedInterfaceTarget{}, nil
				case "diagnostic":
					target.Upstream = f.plan.endpoint
				case "foreign":
					target.Upstream = foreignTarget
				case "changed-name":
					if calls > 1 {
						target.Name = "Proxy8"
					}
				case "changed-endpoint":
					if calls > 1 {
						target.Upstream = f.plan.endpoint
					}
				case "late-error":
					if calls > 1 {
						return target, errors.New("lost router")
					}
				}
				return target, nil
			}
			ctx := f.ctx
			if tc == "missing" {
				f.e.d.Svc.KeeneticVerifyInterfaceTarget = nil
			}
			if tc == "unfenced" {
				ctx = context.Background()
			}
			if got, err := f.e.s.verifyReserveCommit(ctx, f.binding, "BROray - mix-auto"); err == nil || got != (reserveCommitProof{}) {
				t.Fatalf("unconfirmed %s accepted: %+v %v", tc, got, err)
			}
		})
	}
}

func TestReserveCommitRejectsAuthorityChangedDuringRouterRead(t *testing.T) {
	for _, tc := range []string{"pointer", "preferences", "config", "settings", "stop", "cancel", "pause"} {
		t.Run(tc, func(t *testing.T) {
			f := newReserveRuntimeFixture(t)
			target := commitFixtureTarget(t, f)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(context.Context, string) (compat.OwnedInterfaceTarget, error) {
				switch tc {
				case "pointer":
					if err := setActiveID(f.e.d, f.backup); err != nil {
						t.Fatal(err)
					}
				case "preferences":
					writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
				case "config":
					if err := os.WriteFile(f.e.d.Root.XrayConfig(), []byte("{}"), 0600); err != nil {
						t.Fatal(err)
					}
				case "settings":
					if err := compat.WriteJSON(settingsFile(f.e.d), map[string]any{"socksPort": 1}); err != nil {
						t.Fatal(err)
					}
				case "stop":
					if err := f.process.Stop(context.Background()); err != nil {
						t.Fatal(err)
					}
				case "cancel":
					cancel()
				case "pause":
					if err := f.e.d.Queue.SetPaused(true); err != nil {
						t.Fatal(err)
					}
				}
				return target, nil
			}
			if got, err := f.e.s.verifyReserveCommit(ctx, f.binding, "BROray - mix-auto"); err == nil || got != (reserveCommitProof{}) {
				t.Fatalf("stale %s accepted: %+v %v", tc, got, err)
			}
		})
	}
}

func TestReserveCommitPrimaryEndpointSelection(t *testing.T) {
	for _, tc := range []struct {
		name, listen, port, target string
		want                       bool
	}{
		{"exact", "192.168.1.1", "2080", "192.168.1.1:2080", true},
		{"wildcard", "0.0.0.0", "2080", "192.168.1.1:2080", true},
		{"different-address", "127.0.0.1", "2080", "127.0.0.2:2080", false},
		{"different-port", "192.168.1.1", "2080", "192.168.1.1:2081", false},
		{"unspecified-target", "0.0.0.0", "2080", "0.0.0.0:2080", false},
		{"wrong-family", "::", "2080", "192.168.1.1:2080", false},
		{"ipv6", "::1", "2080", "[::1]:2080", true},
		{"zone", "fe80::1%eth0", "2080", "[fe80::1%eth0]:2080", false},
		{"hostname", "localhost", "2080", "127.0.0.1:2080", false},
		{"string-port", "127.0.0.1", `"2080"`, "127.0.0.1:2080", false},
		{"overflow", "127.0.0.1", "67616", "127.0.0.1:2080", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := []byte(fmt.Sprintf(`{"inbounds":[{"protocol":"socks","listen":%q,"port":%s},{"protocol":"socks","tag":"broray-reserve-health","listen":"127.0.0.1","port":2100}]}`, tc.listen, tc.port))
			if got := reservePrimaryMatches(config, netip.MustParseAddrPort(tc.target)); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	for _, config := range []string{
		`{"inbounds":[{"protocol":"socks","tag":"broray-reserve-health","listen":"127.0.0.1","port":2080}]}`,
		`{"inbounds":[{"protocol":"socks","listen":"127.0.0.1","port":2000},{"protocol":"socks","listen":"127.0.0.1","port":2080}]}`,
		`{"inbounds":[{"protocol":"socks","listen":"127.0.0.1","port":2080,"port":2080}]}`,
	} {
		if reservePrimaryMatches([]byte(config), netip.MustParseAddrPort("127.0.0.1:2080")) {
			t.Fatal("diagnostic, second inbound or ambiguous JSON accepted")
		}
	}
}

func TestReserveCommitSupportsDisabledModeWithoutChangingConnection(t *testing.T) {
	f := newReserveRuntimeFixture(t)
	target := commitFixtureTarget(t, f)
	before := f.process.RuntimeSnapshot()
	writeReserveAutomatic(t, f.e, f.plan.node.ID, f.backup, false)
	plan, err := f.e.s.prepareReserveRuntime(f.ctx, f.plan.node.ID, f.plan.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := f.e.s.bindReserveRuntime(f.ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(_ context.Context, description string) (compat.OwnedInterfaceTarget, error) {
		if description != "BROray - A" {
			t.Fatal("wrong OFF description")
		}
		return target, nil
	}
	if _, err := f.e.s.verifyReserveCommit(f.ctx, binding, "BROray - A"); err != nil {
		t.Fatal(err)
	}
	if f.process.RuntimeSnapshot() != before {
		t.Fatal("OFF verification restarted core")
	}
	if _, err := f.e.s.verifyReserveCommit(f.ctx, f.binding, "BROray - A"); err == nil {
		t.Fatal("old mode binding survived disable")
	}
}

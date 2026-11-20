package keenetic

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func onlyDescriptionCommands(t *testing.T, h *harness, name string) {
	t.Helper()
	for _, cmd := range h.r.cmds {
		if cmd != "system configuration save" && !strings.HasPrefix(cmd, "interface "+name+" description ") {
			t.Fatalf("description change reconfigured connection: %s", cmd)
		}
	}
}

func TestReserveInterfaceOrdinaryRenameDoesNotReconnect(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.s.EnsureInterface(ctx); err != nil {
		t.Fatal(err)
	}
	name := h.s.ownerName(h.s.capture(ctx))
	h.setServer("de", "Germany")
	h.r.resetCommands()
	if err := h.s.EnsureInterface(ctx); err != nil {
		t.Fatal(err)
	}
	onlyDescriptionCommands(t, h, name)
}

func TestReserveInterfaceModeKeepsIDAndConnection(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.r.ifaces["Proxy7"] = &fakeIface{desc: `    description "Personal"`, protocol: "socks5", upstream: "192.0.2.7 1080", security: true, up: true}
	h.r.startup = h.r.render()
	foreign := *h.r.ifaces["Proxy7"]
	if err := h.s.EnsureInterface(ctx); err != nil {
		t.Fatal(err)
	}
	name := h.s.ownerName(h.s.capture(ctx))
	if name == "" || name == "Proxy7" {
		t.Fatal("owned interface not established")
	}
	before := *h.r.ifaces[name]
	config, err := os.ReadFile(h.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	mode := "BROray - mix-auto"
	h.d.Svc.ServersInterfaceDescription = func() (string, error) { return mode, nil }
	if desc, ok := h.s.expectedDescription(); !ok || desc != mode {
		t.Fatalf("expected description: %q", desc)
	}
	for _, step := range []string{"enable", "switch", "disable"} {
		if step == "switch" {
			h.setServer("de", "Germany")
		}
		if step == "disable" {
			mode = ""
		}
		h.r.resetCommands()
		if err := h.s.EnsureInterface(ctx); err != nil {
			t.Fatal(err)
		}
		onlyDescriptionCommands(t, h, name)
		desired := "BROray - mix-auto"
		if step == "disable" {
			desired = "BROray - Germany"
		}
		if h.s.ownerName(h.s.capture(ctx)) != name || h.s.readExpected(ctx, name).Description != desired {
			t.Fatal("interface identity/expected description changed incorrectly")
		}
		after := *h.r.ifaces[name]
		after.desc = before.desc
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(foreign, *h.r.ifaces["Proxy7"]) {
			t.Fatal("connection or foreign interface changed")
		}
		current, err := os.ReadFile(h.d.Root.XrayConfig())
		if err != nil || string(current) != string(config) {
			t.Fatal("rename changed Xray config")
		}
	}
}

func TestReserveInterfaceFreshOwnedVerification(t *testing.T) {
	for _, mutation := range []string{"none", "unfenced", "wrong-name", "foreign", "startup", "upstream", "disconnected", "stopped", "provider-error"} {
		t.Run(mutation, func(t *testing.T) {
			h := newHarness(t)
			if err := h.s.EnsureInterface(context.Background()); err != nil {
				t.Fatal(err)
			}
			name := h.s.ownerName(h.s.capture(context.Background()))
			release, ok := h.d.Lock.TryAcquire("test", "verify-interface", name)
			if !ok {
				t.Fatal("fence")
			}
			defer release()
			ctx := compat.WithFence(context.Background())
			desired := "BROray - Netherlands"
			switch mutation {
			case "unfenced":
				ctx = context.Background()
			case "wrong-name":
				desired = "BROray - mix-auto"
			case "foreign":
				h.r.ifaces[name].extra = []string{"    mtu 1400"}
			case "startup":
				h.r.startup = nil
			case "upstream":
				h.setSocks("192.168.1.1", 2081)
			case "disconnected":
				h.r.connected = false
			case "stopped":
				h.d.Svc.XrayRunning = func() bool { return false }
			case "provider-error":
				h.d.Svc.ServersInterfaceDescription = func() (string, error) { return "", errors.New("unavailable") }
			}
			h.r.resetCommands()
			got, err := h.s.VerifyOwnedInterface(ctx, desired)
			if mutation == "none" {
				if err != nil || got != name {
					t.Fatalf("valid owner: %q %v", got, err)
				}
			} else if err == nil || got != "" {
				t.Fatalf("unconfirmed %s accepted", mutation)
			}
			if len(h.r.cmds) != 0 {
				t.Fatal("verification mutated router")
			}
		})
	}
}

func TestReserveInterfaceTargetReturnsExactOwnedUpstream(t *testing.T) {
	h := newHarness(t)
	if err := h.s.EnsureInterface(context.Background()); err != nil {
		t.Fatal(err)
	}
	name := h.s.ownerName(h.s.capture(context.Background()))
	release, ok := h.d.Lock.TryAcquire("test", "verify-target", name)
	if !ok {
		t.Fatal("fence")
	}
	defer release()
	h.r.resetCommands()
	got, err := h.s.VerifyOwnedInterfaceTarget(compat.WithFence(context.Background()), "BROray - Netherlands")
	want := compat.OwnedInterfaceTarget{Name: name, Upstream: netip.MustParseAddrPort("192.168.1.1:2080"), DescriptionLine: descriptionLine("BROray - Netherlands")}
	if err != nil || got != want {
		t.Fatalf("target: %+v %v", got, err)
	}
	if len(h.r.cmds) != 0 {
		t.Fatal("verification wrote router")
	}
	h.r.ifaces[name].extra = []string{"    mtu 1400"}
	if got, err := h.s.VerifyOwnedInterfaceTarget(compat.WithFence(context.Background()), "BROray - Netherlands"); err == nil || got != (compat.OwnedInterfaceTarget{}) {
		t.Fatalf("foreign target accepted: %+v %v", got, err)
	}
}

func TestReserveInterfaceUnavailableServicesRefuseVerification(t *testing.T) {
	s := &compat.Services{}
	if _, err := s.VerifyOwnedInterface(context.Background(), "BROray"); !errors.Is(err, compat.ErrServiceUnavailable) {
		t.Fatal("missing verifier accepted")
	}
	if d, err := s.InterfaceDescription(); err != nil || d != "" {
		t.Fatal("ordinary fallback changed")
	}
}

func TestReserveInterfaceFallbackDescriptionKeepsConnection(t *testing.T) {
	h := newHarness(t)
	if err := h.s.EnsureInterface(context.Background()); err != nil {
		t.Fatal(err)
	}
	name := h.s.ownerName(h.s.capture(context.Background()))
	config, err := os.ReadFile(h.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	h.setServer("", "")
	h.r.resetCommands()
	if err := h.s.EnsureInterface(context.Background()); err != nil {
		t.Fatal(err)
	}
	onlyDescriptionCommands(t, h, name)
	release, ok := h.d.Lock.TryAcquire("test", "verify-fallback", name)
	if !ok {
		t.Fatal("fence")
	}
	defer release()
	if got, err := h.s.VerifyOwnedInterface(compat.WithFence(context.Background()), "BROray"); err != nil || got != name {
		t.Fatalf("fallback ownership unconfirmed: %q %v", got, err)
	}
	after, err := os.ReadFile(h.d.Root.XrayConfig())
	if err != nil || string(after) != string(config) {
		t.Fatal("fallback changed config")
	}
}

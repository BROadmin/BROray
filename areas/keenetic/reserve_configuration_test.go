package keenetic

import (
	"context"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestReserveConfigurationProofWorksWhileCoreDisconnected(t *testing.T) {
	h := newHarness(t)
	h.d.Svc.ServersInterfaceDescription = func() (string, error) { return "BROray - mix-auto", nil }
	if err := h.s.EnsureInterface(context.Background()); err != nil {
		t.Fatal(err)
	}
	name := h.s.ownerName(h.s.capture(context.Background()))
	h.d.Svc.XrayRunning = func() bool { return false }
	h.r.connected = false
	h.r.resetCommands()
	ctx := compat.WithFence(context.Background())
	target, err := h.s.VerifyOwnedInterfaceConfiguration(ctx, "BROray - mix-auto")
	if err != nil || target.Name != name || target.DescriptionLine == "" {
		t.Fatal("owned disconnected configuration refused", target, err)
	}
	if _, err := h.s.VerifyOwnedInterfaceTarget(ctx, "BROray - mix-auto"); err == nil {
		t.Fatal("configuration proof confused with live readiness")
	}
	if len(h.r.cmds) != 0 {
		t.Fatal("read-only proof mutated router", h.r.cmds)
	}
	h.r.ifaces[name].upstream = "192.0.2.8 9999"
	if _, err := h.s.VerifyOwnedInterfaceConfiguration(ctx, "BROray - mix-auto"); err == nil {
		t.Fatal("foreign upstream accepted")
	}
	if _, err := h.s.VerifyOwnedInterfaceConfiguration(context.Background(), "BROray - mix-auto"); err == nil {
		t.Fatal("unfenced proof accepted")
	}
}

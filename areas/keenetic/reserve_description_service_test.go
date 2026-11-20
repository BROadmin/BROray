package keenetic

import (
	"context"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestReserveDescriptionOnlyService(t *testing.T) {
	h := newHarness(t)
	registerReserveDescriptionServices(h.s)
	mode := "BROray - mix-auto"
	h.d.Svc.ServersInterfaceDescription = func() (string, error) { return mode, nil }
	if err := h.s.EnsureInterface(context.Background()); err != nil {
		t.Fatal(err)
	}
	name := h.s.ownerName(h.s.capture(context.Background()))
	h.r.resetCommands()
	ordinary, err := h.d.Svc.OrdinaryInterfaceDescription()
	if err != nil || ordinary != "BROray - Netherlands" || len(h.r.cmds) != 0 {
		t.Fatal("read-only preview", ordinary, err)
	}
	mode = ""
	if err := h.d.Svc.SyncOwnedInterfaceDescription(context.Background()); err == nil {
		t.Fatal("unfenced rename accepted")
	}
	if len(h.r.cmds) != 0 {
		t.Fatal("unfenced rename wrote router")
	}
	if err := h.d.Svc.SyncOwnedInterfaceDescription(compat.WithFence(context.Background())); err != nil {
		t.Fatal(err)
	}
	onlyDescriptionCommands(t, h, name)
	if h.s.ownerName(h.s.capture(context.Background())) != name {
		t.Fatal("owner changed")
	}
	h.r.ifaces[name].upstream = "192.0.2.9 9999"
	h.r.resetCommands()
	mode = "BROray - mix-auto"
	if err := h.d.Svc.SyncOwnedInterfaceDescription(compat.WithFence(context.Background())); err == nil {
		t.Fatal("foreign configuration repaired")
	}
	if len(h.r.cmds) != 0 {
		t.Fatal("metadata service repaired foreign connection", h.r.cmds)
	}
}

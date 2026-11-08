package xray

import (
	"context"
	"os"
	"strings"
	"testing"

	_ "github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
)

// Production Services + ApplyConfig + supervised process; only the executable
// is the existing deterministic fakeXray fixture. No apply rollback hook.
func TestWithdrawnConfigCannotRestartOrRollback(t *testing.T) {
	h := newHarness(t, true)
	h.d.Maintenance = true
	if err := compat.StartAreas(h.d); err != nil {
		t.Fatal(err)
	}
	h.s = current.Load()
	if err := os.MkdirAll(h.d.Path("config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.d.Path("config", "active-server"), []byte("manual-old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := h.s.ApplyConfig(ctx, []byte(goodConfig)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.d.Svc.WithdrawActiveServer(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.lifecycle(ctx, "restart"); err == nil {
		t.Error("manual restart resurrected withdrawn config")
	}
	if err := h.d.App.Xray.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	err := h.s.ApplyConfig(ctx, []byte(strings.Replace(goodConfig, "2080", "2090", 1)))
	if err == nil {
		t.Fatal("replacement fixture must fail SOCKS readiness")
	}
	if h.d.App.Xray.Running() {
		t.Error("failed replacement restarted withdrawn config")
	}
	if _, err := os.Stat(h.d.Root.XrayConfig()); !os.IsNotExist(err) {
		t.Error("withdrawn runtime still usable")
	}
}

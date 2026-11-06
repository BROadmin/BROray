package keenetic

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestUninstallRefusesAmbiguousUDPRecovery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt string
	}{
		{name: "malformed-receipt", corrupt: "receipt"},
		{name: "unreadable-route-registry", corrupt: "registry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			if err := h.s.EnsureInterface(ctx); err != nil {
				t.Fatal(err)
			}
			ip := netip.MustParseAddr("1.1.1.1")
			add, _ := udpRouteCommands("Proxy0", ip)
			h.r.routes = append(h.r.routes, add)
			receipt := h.s.udpReceiptPath()
			if tc.corrupt == "receipt" {
				if err := os.WriteFile(receipt, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := compat.WriteJSON(receipt, udpRouteReceipt{
					SchemaVersion: 1, Purpose: udpReceiptPurpose,
					Network: ip.String(), Mask: udpHostMask, Interface: "Proxy0", Metric: 1200,
				}); err != nil {
					t.Fatal(err)
				}
				installed := h.d.Path("routes", "installed")
				if err := os.MkdirAll(installed, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(installed, "routes.json"), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.s.uninstallInterface(ctx); err == nil {
				t.Error("uninstall accepted failed temporary UDP route recovery")
			}
			if len(h.r.ifaces) != 1 {
				t.Fatalf("managed ProxyN changed while recovery was ambiguous: %v", h.r.ifaces)
			}
			if !hostRoutePresent(h.r.render(), ip, "Proxy0") {
				t.Fatal("temporary route changed while recovery was ambiguous")
			}
			if _, err := os.Lstat(receipt); err != nil {
				t.Fatalf("ambiguous receipt lost: %v", err)
			}
		})
	}
}

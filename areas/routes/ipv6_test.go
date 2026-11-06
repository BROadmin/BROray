package routes

import (
	"context"
	"reflect"
	"testing"

	kn "github.com/BROadmin/broray/internal/keenetic"
)

// The routes overview carries the keenetic area's IPv6 fact (additive):
// unknown when that area is absent, the warning when the router has global
// IPv6. The overview never asks the router itself.
func TestOverviewIPv6(t *testing.T) {
	env := newEnv(t)
	ov := env.mustOK(env.get("/api/routes/overview.cgi"))
	v, ok := ov["ipv6"].(map[string]any)
	if !ok || v["state"] != "unknown" || v["warning"] != nil {
		t.Fatalf("ipv6 without keenetic area: %v", ov["ipv6"])
	}
	calls := 0
	env.d.Svc.KeeneticIPv6 = func(ctx context.Context) map[string]any {
		calls++
		return kn.IPv6Facts{State: kn.IPv6Global, WAN: []string{"PPPoE0"}, LANPrefix: true}.Object("2026-11-02T10:00:00+0300")
	}
	ov = env.mustOK(env.get("/api/routes/overview.cgi"))
	v = ov["ipv6"].(map[string]any)
	if v["state"] != "global" || v["warning"] != kn.IPv6WarningText || v["lanPrefix"] != true || !reflect.DeepEqual(v["wan"], []any{"PPPoE0"}) || calls != 1 {
		t.Fatalf("ipv6 %v calls %d", v, calls)
	}
	// The 3.x fields are unchanged.
	for _, k := range []string{"schemaVersion", "generatedAt", "managedInterface", "managedInterfaceDisplay", "globalOperation", "operation",
		"totalManagedRoutes", "sharedRoutes", "custom", "catalog", "lastUpdatedAt", "health"} {
		if _, ok := ov[k]; !ok {
			t.Fatalf("overview lost %q", k)
		}
	}
}

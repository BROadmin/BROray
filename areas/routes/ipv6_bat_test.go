package routes

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func ipv6TestBundle(t *testing.T, env *testEnv, bat string) string {
	t.Helper()
	rep := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("IPv6", [2]string{"routes.bat", bat})))
	res := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": rep["token"], "name": "IPv6", "bundleId": nil}))
	return res["bundleId"].(string)
}

func TestIPv6LifecyclePreservesForeign(t *testing.T) {
	env := newEnv(t)
	foreign := "ipv6 route 2001:db8:1::/64 Proxy0"
	env.r.routes = append(env.r.routes, foreign, "ipv6 route 2001:db8:9::/64 Wireguard0")
	before := append([]string(nil), env.r.snapshot()...)
	id := ipv6TestBundle(t, env, "route -6 add 2001:db8::/64 ::\n")
	env.exportBundle(id)
	st := env.mustOK(env.get("/api/routes/custom-status.cgi?bundleId=" + id))
	if sub(st, "routerPresence")["status"] != "complete" {
		t.Fatalf("presence: %v", st["routerPresence"])
	}
	token := env.preflightToken(id, "delete")
	env.mustOK(env.post("/api/routes/delete.cgi?bundleId=" + id + "&preflightToken=" + token))
	if !reflect.DeepEqual(env.r.snapshot(), before) {
		t.Fatalf("foreign route changed: %v", env.r.snapshot())
	}
}

func TestIPv6LifecycleRejectsUnownedMatch(t *testing.T) {
	env := newEnv(t)
	env.r.routes = append(env.r.routes, "ipv6 route 2001:db8::/64 Proxy0")
	before := env.r.snapshot()
	id := ipv6TestBundle(t, env, "route -6 add 2001:db8::/64 ::\n")
	p, err := env.e.runPlan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.CanApply || len(p.Blocking) != 1 {
		t.Fatalf("unowned route admitted: %+v", p)
	}
	if !reflect.DeepEqual(before, env.r.snapshot()) {
		t.Fatal("preflight mutated foreign route")
	}
}

func TestIPv6LifecycleSharedOwnership(t *testing.T) {
	env := newEnv(t)
	before := env.r.snapshot()
	first := ipv6TestBundle(t, env, "route -6 add 2001:db8::/64 ::\n")
	second := ipv6TestBundle(t, env, "route -6 add 2001:db8::/64 ::\nroute -6 add 2001:db8:2::/64 ::\n")
	env.exportBundle(first)
	firstRoutes := env.r.snapshot()
	env.exportBundle(second)
	token := env.preflightToken(second, "delete")
	env.mustOK(env.post("/api/routes/delete.cgi?bundleId=" + second + "&preflightToken=" + token))
	if !reflect.DeepEqual(env.r.snapshot(), firstRoutes) {
		t.Fatal("shared route removed while first owner remains")
	}
	token = env.preflightToken(first, "delete")
	env.mustOK(env.post("/api/routes/delete.cgi?bundleId=" + first + "&preflightToken=" + token))
	if !reflect.DeepEqual(env.r.snapshot(), before) {
		t.Fatal("route remains after last owner removed")
	}
}

func TestIPv6DeletePreservesChangedGateway(t *testing.T) {
	env := newEnv(t)
	id := ipv6TestBundle(t, env, "route -6 add 2001:db8::/64 ::\n")
	env.exportBundle(id)
	token := env.preflightToken(id, "delete")
	for i, line := range env.r.routes {
		if line == "ipv6 route 2001:db8::/64 Proxy0" {
			env.r.routes[i] += " 2001:db8::1"
		}
	}
	before := env.r.snapshot()
	response := env.mustOK(env.post("/api/routes/delete.cgi?bundleId=" + id + "&preflightToken=" + token))
	if intOr(sub(response, "deleteResult")["deleted"], -1) != 0 || !reflect.DeepEqual(env.r.snapshot(), before) {
		t.Fatal("foreign route changed after original owned identity disappeared")
	}
}

func TestIPv6DeleteRefusesChangedMetric(t *testing.T) {
	env := newEnv(t)
	id := ipv6TestBundle(t, env, "route -6 add 2001:db8::/64 ::\n")
	env.exportBundle(id)
	token := env.preflightToken(id, "delete")
	for i, line := range env.r.routes {
		if line == "ipv6 route 2001:db8::/64 Proxy0" {
			env.r.routes[i] += " 100"
		}
	}
	before := env.r.snapshot()
	response := env.post("/api/routes/delete.cgi?bundleId=" + id + "&preflightToken=" + token)
	if response.status < 400 || !reflect.DeepEqual(env.r.snapshot(), before) {
		t.Fatal("changed metric accepted for physical deletion")
	}
}

func TestIPv6LifecycleRollback(t *testing.T) {
	env := newEnv(t)
	var bat strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&bat, "route -6 add 2001:db8:%x::/64 ::\n", i*2)
	}
	id := ipv6TestBundle(t, env, bat.String())
	before := env.r.snapshot()
	adds := 0
	env.r.failCmd = func(cmd string, _ int) bool {
		if strings.HasPrefix(cmd, "ipv6 route ") {
			adds++
			return adds == 18
		}
		return false
	}
	token := env.preflightToken(id, "export")
	env.wantErr(env.post("/api/routes/export.cgi?bundleId="+id+"&preflightToken="+token), 502, "ROUTES_EXPORT_FAILED")
	if !reflect.DeepEqual(before, env.r.snapshot()) {
		t.Fatal("IPv6 partial batch not rolled back")
	}
	if fileExists(env.e.rollbackMarker()) || env.d.Lock.Busy() {
		t.Fatal("rollback marker/fence left")
	}
}

func TestIPv6BATMixedAndStrict(t *testing.T) {
	r := parseBat([]byte("route add 1.2.3.0 mask 255.255.255.0 0.0.0.0\r\nroute -6 add 2001:0DB8:1::ff/64 :: & rem example\r\n"), false, 20)
	if r.ErrorCount != 0 || r.RouteLines != 2 || r.NormalizedLines != 1 || !reflect.DeepEqual(r.Routes, []string{"1.2.3.0/24", "2001:db8:1::/64"}) {
		t.Fatalf("mixed: %+v", r)
	}
	for _, line := range []string{"route -6 add ::/0 ::", "route -6 add 2001:db8::/129 ::", "route -6 add 2001:db8::/64 :: & reboot", "route -6 add 2001:db8::/64 2001:db8::1", "route -6 add ::ffff:1.2.3.4/128 ::", "route -6 add fe80::1%eth0/64 ::", "route -6 add 2001:db8::/64 ::;reboot"} {
		if p := parseBat([]byte(line), false, 20); p.ErrorCount != 1 || len(p.Routes) != 0 {
			t.Errorf("accepted %q: %+v", line, p)
		}
	}
}

func TestIPv6BATAggregationAndAlignment(t *testing.T) {
	got := collapseCIDR([]string{"1.2.3.0/24", "2001:db8::/65", "2001:db8:0:0:8000::/65", "2001:db8::1/128", "2001:db8::/65"})
	want := []string{"1.2.3.0/24", "2001:db8::/64"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregate %v", got)
	}
	if !networkAligned("2001:db8::", 64) || networkAligned("2001:db8::1", 64) {
		t.Fatal("IPv6 canonical alignment")
	}
}

func TestIPv6RouteCommandIdentity(t *testing.T) {
	r := planRoute{Network: "2001:db8::", Prefix: 64, Iface: "Proxy0", Metric: 0, Comment: "BROray"}
	r.Key = routeKey(r.Network, r.Prefix, r.Iface)
	if !strings.HasPrefix(r.Key, "ipv6|") || r.object()["family"] != "ipv6" {
		t.Fatalf("family: %v", r.object())
	}
	if addCmd(r) != "ipv6 route 2001:db8::/64 Proxy0" || removeCmd(r) != "no ipv6 route 2001:db8::/64 Proxy0" {
		t.Fatal("wrong IPv6 command grammar")
	}
	if !allowRouteCommand(addCmd(r)) || !allowRouteCommand(removeCmd(r)) {
		t.Fatal("valid exact shape refused")
	}
	for _, cmd := range []string{"no ipv6 route", "ipv6 route ::/0 Proxy0", "ipv6 route 2001:db8::/64 Proxy0;reboot", "no ipv6 route 2001:db8::/64 ISP", "ipv6 route 2001:db8::/64 Proxy0 1200"} {
		if allowRouteCommand(cmd) {
			t.Errorf("unsafe %q", cmd)
		}
	}
	s := parseRunningConfig([]string{addCmd(r), "ipv6 route 2001:db8:1::/64 Proxy0 2001:db8::1", "ip route 1.2.3.0 255.255.255.0 Proxy0 1200"})
	if !s.complete() || s.identityCount(r.dest(), r.Iface) != 1 || s.attributeCount(r.dest(), r.Iface, 0) != 1 || s.attributeCount(r.dest(), r.Iface, 1200) != 0 {
		t.Fatalf("snapshot %+v", s)
	}
	if s.identityCount("2001:db8:1::/64", "Proxy0") != 0 {
		t.Fatal("foreign gateway treated as direct")
	}
}

func TestIPv6CustomStoredFamily(t *testing.T) {
	env := newEnv(t)
	rep := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("IPv6", [2]string{"mixed.bat", "route -6 add 2001:db8::/64 ::\nroute add 1.2.3.0 mask 255.255.255.0 0.0.0.0\n"})))
	res := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": rep["token"], "name": "IPv6", "bundleId": nil}))
	id := res["bundleId"].(string)
	_, routes, _, err := env.e.runBuildExport(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Fatalf("routes=%v", routes)
	}
	found := false
	for _, r := range routes {
		if strings.Contains(r.Network, ":") {
			found = true
			if r.object()["family"] != "ipv6" || r.Metric != 0 || r.Mask != "" {
				t.Fatalf("bad IPv6 route %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("IPv6 discarded")
	}
}

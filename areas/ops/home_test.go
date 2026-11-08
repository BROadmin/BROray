package ops

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
)

func okHealth(module string) map[string]any {
	return map[string]any{"schemaVersion": 1, "module": module, "availability": "available", "severity": "ok",
		"operational": true, "consistent": true, "actionRequired": false,
		"freshness": map[string]any{"state": "fresh", "checkedAt": nil}, "reasons": []any{}, "facts": map[string]any{}, "lastOperation": nil}
}

func writeSnapshot(t *testing.T, a *area, module string, ageSec int64, data map[string]any) {
	t.Helper()
	now := a.now().Unix() - ageSec
	if err := os.MkdirAll(a.d.RunPath("home-snapshots"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(a.snapshotPath(module), map[string]any{"schemaVersion": 1, "module": module,
		"capturedAt": compat.StampUTC(time.Unix(now, 0)), "capturedEpoch": now, "data": data}); err != nil {
		t.Fatal(err)
	}
}

func TestHomeSummaryNoSnapshots(t *testing.T) {
	testArea(t)
	r := call(t, "GET", "/api/home/summary.cgi", "", nil)
	if r.Code != 200 || r.Body["success"] != true {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	d := data(r)
	want := []any{"xray", "keenetic", "servers", "subscriptions", "dns", "routes", "broray"}
	if !reflect.DeepEqual(d["errors"], want) {
		t.Fatalf("errors %v", d["errors"])
	}
	for _, m := range []string{"xray", "servers", "subscriptions", "dns", "keenetic", "routes", "broray"} {
		if v, ok := d[m]; !ok || v != nil {
			t.Fatalf("module %s must be present and null", m)
		}
	}
	h := d["health"].(map[string]any)
	if h["severity"] != "error" || h["availability"] != "partial" || d["healthy"] != false {
		t.Fatalf("health %v", h)
	}
	mods := d["modules"].([]any)
	if len(mods) != 7 || mods[0].(map[string]any)["id"] != "xray" || mods[5].(map[string]any)["id"] != "keenetic" {
		t.Fatalf("modules order %v", mods)
	}
	mh := mods[0].(map[string]any)["health"].(map[string]any)
	if mh["availability"] != "unavailable" || mh["reasons"].([]any)[0].(map[string]any)["code"] != "MODULE_SNAPSHOT_UNAVAILABLE" {
		t.Fatalf("%v", mh)
	}
	if ir := d["installedRelease"].(map[string]any); ir["version"] != app.Version || ir["webUIBuild"] != webUIBuild {
		t.Fatalf("installedRelease %v", ir)
	}
	if webUIBuild != "WebUI-3.5.1" {
		t.Fatalf("webUIBuild %q", webUIBuild)
	}
	if fr := h["facts"].(map[string]any); fr["unavailableCount"] != float64(7) || fr["moduleCount"] != float64(7) {
		t.Fatalf("facts %v", fr)
	}
	// method check
	if r := call(t, "POST", "/api/home/summary.cgi", "{}", nil); r.Code != 405 || errCode(r) != "METHOD_NOT_ALLOWED" ||
		errMessage(r) != "Для этой операции требуется метод GET." {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
}

func TestHomeSummaryFreshnessAndAggregation(t *testing.T) {
	a := testArea(t)
	fixed := time.Now()
	a.now = func() time.Time { return fixed }
	for _, m := range homeModules {
		writeSnapshot(t, a, m, 10, map[string]any{"health": okHealth(m)})
	}
	r := call(t, "GET", "/api/home/summary.cgi", "", nil)
	d := data(r)
	h := d["health"].(map[string]any)
	if h["severity"] != "ok" || d["healthy"] != true || h["availability"] != "available" ||
		h["freshness"].(map[string]any)["state"] != "fresh" || len(d["issues"].([]any)) != 0 {
		t.Fatalf("all fresh: %v", h)
	}
	x := d["xray"].(map[string]any)["_snapshot"].(map[string]any)
	if x["freshness"] != "fresh" || x["ageSeconds"] != float64(10) || x["module"] != "xray" {
		t.Fatalf("_snapshot %v", x)
	}
	// raw module object keeps its own (unadjusted) health
	if d["xray"].(map[string]any)["health"].(map[string]any)["freshness"].(map[string]any)["checkedAt"] != nil {
		t.Fatal("raw module health must stay unmodified")
	}

	// stale servers, expired routes, keenetic without contract, dns reason patch
	writeSnapshot(t, a, "servers", 200, map[string]any{"health": okHealth("servers")})
	writeSnapshot(t, a, "routes", 700, map[string]any{"health": okHealth("routes")})
	writeSnapshot(t, a, "keenetic", 5, map[string]any{"state": "up"})
	dnsHealth := okHealth("dns")
	dnsHealth["severity"] = "warning"
	dnsHealth["reasons"] = []any{map[string]any{"code": "DNS_TEST_REQUIRED", "message": "x", "details": nil}}
	writeSnapshot(t, a, "dns", 5, map[string]any{"health": dnsHealth, "selectedCount": 3, "selectedTestedCount": 1})
	r = call(t, "GET", "/api/home/summary.cgi", "", nil)
	d = data(r)
	h = d["health"].(map[string]any)
	if h["severity"] != "warning" || h["freshness"].(map[string]any)["state"] != "expired" || d["healthy"] != false {
		t.Fatalf("aggregate %v", h)
	}
	mods := map[string]map[string]any{}
	for _, m := range d["modules"].([]any) {
		mm := m.(map[string]any)
		mods[mm["id"].(string)] = mm["health"].(map[string]any)
	}
	sh := mods["servers"]
	if sh["severity"] != "warning" || sh["actionRequired"] != true || sh["freshness"].(map[string]any)["state"] != "stale" {
		t.Fatalf("stale %v", sh)
	}
	last := sh["reasons"].([]any)[0].(map[string]any)
	if last["code"] != "MODULE_SNAPSHOT_STALE" || last["details"].(map[string]any)["ageSeconds"] != float64(200) {
		t.Fatalf("stale reason %v", last)
	}
	if rr := mods["routes"]["reasons"].([]any)[0].(map[string]any); rr["code"] != "MODULE_SNAPSHOT_EXPIRED" {
		t.Fatalf("expired %v", rr)
	}
	kh := mods["keenetic"]
	if kh["severity"] != "unknown" || kh["reasons"].([]any)[0].(map[string]any)["code"] != "STATUS_CONTRACT_MISSING" ||
		kh["freshness"].(map[string]any)["state"] != "fresh" {
		t.Fatalf("contract missing %v", kh)
	}
	dr := d["dns"].(map[string]any)["health"].(map[string]any)["reasons"].([]any)[0].(map[string]any)
	if dr["message"] != "Проверено 1 из 3 выбранных серверов DNS-over-TLS." {
		t.Fatalf("dns patch %v", dr["message"])
	}
	// issues: servers, dns, routes, keenetic in module order with module tag on reasons
	issues := d["issues"].([]any)
	var ids []string
	for _, i := range issues {
		ids = append(ids, i.(map[string]any)["module"].(string))
	}
	if !reflect.DeepEqual(ids, []string{"servers", "dns", "routes", "keenetic"}) {
		t.Fatalf("issues %v", ids)
	}
	if rs := h["reasons"].([]any); rs[0].(map[string]any)["module"] != "servers" {
		t.Fatalf("reasons %v", rs)
	}
	// an error module dominates
	eh := okHealth("xray")
	eh["severity"] = "error"
	writeSnapshot(t, a, "xray", 5, map[string]any{"health": eh})
	if h := data(call(t, "GET", "/api/home/summary.cgi", "", nil))["health"].(map[string]any); h["severity"] != "error" {
		t.Fatal(h["severity"])
	}
	// invalid snapshot file → null + error
	if err := os.WriteFile(a.snapshotPath("broray"), []byte(`{"schemaVersion":1,"module":"xray","capturedAt":"x","capturedEpoch":1,"data":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d = data(call(t, "GET", "/api/home/summary.cgi", "", nil))
	if d["broray"] != nil || !reflect.DeepEqual(d["errors"], []any{"broray"}) {
		t.Fatalf("invalid snapshot: %v %v", d["broray"], d["errors"])
	}
}

var testProducerErr error

func TestCollectorProducersAndRoutesProjection(t *testing.T) {
	a := testArea(t)
	compat.RegisterSnapshot("subscriptions", func(ctx context.Context) (map[string]any, error) {
		if testProducerErr != nil {
			return nil, testProducerErr
		}
		return map[string]any{"total": 2, "health": okHealth("subscriptions")}, nil
	})
	compat.RegisterSnapshot("routes", func(ctx context.Context) (map[string]any, error) {
		return map[string]any{"totals": map[string]any{"bundleCount": 12, "verifiedInstalledCount": 3, "recordedInstalledCount": 4,
			"presentRouteCount": 2150, "attentionCount": 0}, "health": okHealth("routes"), "managedInterface": "Proxy0",
			"globalOperation": map[string]any{"running": false}, "generatedAt": "2026-11-02T09:59:40Z"}, nil
	})
	compat.RegisterSnapshot("dns", func(ctx context.Context) (map[string]any, error) { panic("boom") })
	a.collectAll(context.Background())
	d := data(call(t, "GET", "/api/home/summary.cgi", "", nil))
	if s := d["subscriptions"].(map[string]any); s["total"] != float64(2) {
		t.Fatalf("subscriptions %v", s)
	}
	rt := d["routes"].(map[string]any)
	if rt["schemaVersion"] != float64(2) || rt["availableBundles"] != float64(12) || rt["recordedInstalledBundles"] != float64(4) ||
		rt["managedInterface"] != "Proxy0" || rt["updatedAt"] != "2026-11-02T09:59:40Z" || rt["actionRequired"] != true {
		t.Fatalf("routes projection %v", rt)
	}
	if d["dns"] != nil || d["xray"] != nil {
		t.Fatal("panicking/missing producers must leave the module null")
	}
	a.collMu.Lock()
	if a.collLast["dns"].OK || a.collLast["xray"].Error != errNoProducer.Error() || !a.collLast["routes"].OK {
		t.Fatalf("%+v", a.collLast)
	}
	a.collMu.Unlock()
	// a failing refresh keeps the previous snapshot
	testProducerErr = errors.New("X: fail")
	defer func() { testProducerErr = nil }()
	a.collectAll(context.Background())
	if s := data(call(t, "GET", "/api/home/summary.cgi", "", nil))["subscriptions"]; s == nil {
		t.Fatal("previous snapshot must be kept")
	}
}

// The keenetic snapshot's additive IPv6 fact reaches home.js unchanged and
// does not raise the summary severity (warning only, owner decision).
func TestHomeSummaryCarriesKeeneticIPv6(t *testing.T) {
	a := testArea(t)
	for _, m := range []string{"xray", "servers", "subscriptions", "dns", "routes", "broray"} {
		writeSnapshot(t, a, m, 0, map[string]any{"health": okHealth(m)})
	}
	ipv6 := map[string]any{"state": "global", "wan": []any{"GigabitEthernet1"}, "lanPrefix": true,
		"warning": "У провайдера включён IPv6: сервисы с IPv6-адресами могут работать мимо BROray (наборы маршрутов только IPv4).", "checkedAt": "2026-11-02T10:00:00+0300"}
	writeSnapshot(t, a, "keenetic", 0, map[string]any{"health": okHealth("keenetic"), "ipv6": ipv6})
	d := data(call(t, "GET", "/api/home/summary.cgi", "", nil))
	if got := d["keenetic"].(map[string]any)["ipv6"]; !reflect.DeepEqual(got, ipv6) {
		t.Fatalf("ipv6 %v", got)
	}
	if h := d["health"].(map[string]any); h["severity"] != "ok" || d["healthy"] != true {
		t.Fatalf("IPv6 must not change the summary health: %v", h)
	}
}

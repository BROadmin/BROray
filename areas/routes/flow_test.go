package routes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func telegramRouteCount(t *testing.T) int {
	b, _ := findBuiltin("telegram")
	data, err := readCloneFile(b.Directory, b.Files[0].Name)
	if err != nil {
		t.Skip("clone unavailable")
	}
	return len(sortedUnique(parseBat(data, true, 0).Routes))
}

func TestCatalogFullLifecycle(t *testing.T) {
	env := newEnv(t)
	n := telegramRouteCount(t)

	// catalog summary before anything: one alphabetical list of 12 bundles
	sum := env.mustOK(env.get("/api/routes/catalog-summary.cgi"))
	bundles := sum["bundles"].([]any)
	var order []string
	for _, b := range bundles {
		order = append(order, b.(map[string]any)["id"].(string))
	}
	if strings.Join(order, ",") != "chatgpt,claude,facebook,instagram,meta,netflix,speedtest,telegram,tiktok,whatsapp,wikipedia,youtube" {
		t.Fatalf("bundles order: %v", order)
	}
	if sum["totals"].(map[string]any)["bundleCount"].(float64) != 12 {
		t.Fatal(sum["totals"])
	}
	if sum["managedInterface"] != "Proxy0" || sum["managedInterfaceDisplay"] != "BROray" {
		t.Fatal(sum["managedInterface"])
	}

	// check
	st := env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	if st["status"] != "available" || st["checkResult"].(map[string]any)["result"] != "initial_available" || int(st["routeCount"].(float64)) != n {
		t.Fatalf("check: %v", st["checkResult"])
	}
	if op := st["operation"].(map[string]any); op["type"] != "check" || !strings.Contains(op["output"].(string), "Набор: telegram") {
		t.Fatal(op)
	}
	// download
	st = env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	if st["status"] != "downloaded" || st["downloadResult"].(map[string]any)["result"] != "downloaded" {
		t.Fatal(st["downloadResult"])
	}
	// second check: no changes
	st = env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	if st["checkResult"].(map[string]any)["result"] != "no_changes" {
		t.Fatal(st["checkResult"])
	}
	// verify: not installed
	st = env.mustOK(env.post("/api/routes/verify.cgi?bundleId=telegram"))
	vr := st["verifyResult"].(map[string]any)
	if vr["result"] != "not_installed" || vr["success"] != true {
		t.Fatal(vr)
	}
	// export
	st = env.exportBundle("telegram")
	if st["status"] != "installed" || st["exportResult"].(map[string]any)["message"] != "Маршруты установлены в Keenetic" {
		t.Fatal(st["exportResult"])
	}
	if got := env.r.routeCount("Proxy0"); got != n {
		t.Fatalf("router has %d routes, want %d", got, n)
	}
	if env.d.Lock.Busy() {
		t.Fatal("fence must be released")
	}
	prog := st["operationProgress"].(map[string]any)
	if prog["phase"] != "completed" || prog["success"] != true || int(prog["percent"].(float64)) != 100 {
		t.Fatal(prog)
	}
	// startup saved
	run, _ := env.r.RunningConfig(context.Background())
	stc, _ := env.r.StartupConfig(context.Background())
	if strings.Join(run, "\n") != strings.Join(stc, "\n") {
		t.Fatal("not saved")
	}
	// summary: verified installed
	sum = env.mustOK(env.get("/api/routes/catalog-summary.cgi"))
	tot := sum["totals"].(map[string]any)
	if tot["installedCount"].(float64) != 1 || int(tot["presentRouteCount"].(float64)) != n {
		t.Fatalf("totals %v", tot)
	}
	ov := env.mustOK(env.get("/api/routes/overview.cgi"))
	if int(ov["totalManagedRoutes"].(float64)) != n || ov["catalog"].(map[string]any)["installed"].(float64) != 1 {
		t.Fatal(ov)
	}
	// re-export is idempotent (mode none → install message "проверены")
	st = env.exportBundle("telegram")
	if st["exportResult"].(map[string]any)["operation"] != "none" {
		t.Fatal(st["exportResult"])
	}
	// drift: remove a route manually, summary shows drift, export restores
	env.r.mu.Lock()
	env.r.routes = env.r.routes[1:]
	env.r.mu.Unlock()
	env.e.invalidateCache()
	sum = env.mustOK(env.get("/api/routes/catalog-summary.cgi"))
	for _, b := range sum["bundles"].([]any) {
		bm := b.(map[string]any)
		if bm["id"] == "telegram" && bm["routerPresence"].(map[string]any)["drift"] != true {
			t.Fatal("drift not detected")
		}
	}
	st = env.exportBundle("telegram")
	if st["exportResult"].(map[string]any)["operation"] != "restore" || env.r.routeCount("Proxy0") != n {
		t.Fatal(st["exportResult"])
	}
	// status and summary endpoints
	stat := env.mustOK(env.get("/api/routes/status.cgi?bundleId=telegram"))
	if stat["routerPresence"].(map[string]any)["status"] != "complete" {
		t.Fatal(stat["routerPresence"])
	}
	one := env.mustOK(env.get("/api/routes/summary.cgi?bundleId=telegram"))
	if one["installed"] != true || one["state"] != "installed" {
		t.Fatalf("summary one: %v %v", one["state"], one["error"])
	}
	all := env.mustOK(env.get("/api/routes/summary.cgi"))
	if all["installedBundles"].(float64) != 1 {
		t.Fatal(all["installedBundles"])
	}
	// delete
	tok := env.preflightToken("telegram", "delete")
	st = env.mustOK(env.post("/api/routes/delete.cgi?bundleId=telegram&preflightToken=" + tok))
	if st["status"] != "downloaded" || st["deleteResult"].(map[string]any)["result"] != "removed" || env.r.routeCount("Proxy0") != 0 {
		t.Fatal(st["deleteResult"])
	}
	// other routes untouched
	if !strings.Contains(strings.Join(env.r.running(), "\n"), "ip route 8.8.8.0 255.255.255.0 Wireguard0 auto") {
		t.Fatal("foreign route touched")
	}
	// operations panel shows route operations
	found := false
	for _, op := range env.d.Queue.Operations() {
		if op["type"] == "route_operation" && op["state"] == "completed" {
			found = true
		}
	}
	if !found {
		t.Fatal("route operations not registered")
	}
}

func TestBatchingRereadsRunningConfigEvery16(t *testing.T) {
	env := newEnv(t)
	n := telegramRouteCount(t)
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	before := env.r.cmdCount
	env.exportBundle("telegram")
	adds := 0
	for _, c := range env.r.commands {
		if strings.HasPrefix(c, "ip route ") {
			adds++
		}
	}
	if adds != n || env.r.cmdCount-before != n+2 { // routes + 2 saves
		t.Fatalf("adds=%d cmds=%d", adds, env.r.cmdCount-before)
	}
}

func TestExportFailureRollsBack(t *testing.T) {
	env := newEnv(t)
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	before := env.r.snapshot()
	adds := 0
	env.r.failCmd = func(cmd string, _ int) bool {
		if strings.HasPrefix(cmd, "ip route ") {
			adds++
			return adds == 20
		}
		return false
	}
	tok := env.preflightToken("telegram", "export")
	r := env.post("/api/routes/export.cgi?bundleId=telegram&preflightToken=" + tok)
	env.wantErr(r, 502, "ROUTES_EXPORT_FAILED")
	if !strings.Contains(r.details().(string), "Не удалось добавить маршрут") {
		t.Fatal(r.details())
	}
	if strings.Join(env.r.snapshot(), "|") != strings.Join(before, "|") {
		t.Fatalf("router not rolled back: %d routes", len(env.r.snapshot()))
	}
	p, _ := env.e.readProgress("telegram")
	if p["phase"] != "rolled_back" || p["rolledBack"] != true || !strings.HasSuffix(p["message"].(string), "Изменения операции отменены.") {
		t.Fatal(p)
	}
	if fileExists(env.e.rollbackMarker()) || env.d.Lock.Busy() {
		t.Fatal("marker or fence left")
	}
	s := env.state("telegram")
	if s["installedVersion"] != nil || s["status"] != "downloaded" {
		t.Fatal("local state not restored")
	}
}

func TestRollbackFailureWritesBlockingMarker(t *testing.T) {
	env := newEnv(t)
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	adds := 0
	env.r.failCmd = func(cmd string, _ int) bool {
		if strings.HasPrefix(cmd, "ip route ") {
			adds++
			return adds == 20
		}
		return strings.HasPrefix(cmd, "no ip route ") // rollback cannot remove
	}
	tok := env.preflightToken("telegram", "export")
	env.wantErr(env.post("/api/routes/export.cgi?bundleId=telegram&preflightToken="+tok), 502, "ROUTES_EXPORT_FAILED")
	m, ok := readObj(env.e.rollbackMarker())
	if !ok || m["kind"] != "routes-rollback-required" || m["blocksNewMutations"] != true {
		t.Fatal("marker missing")
	}
	p, _ := env.e.readProgress("telegram")
	if p["phase"] != "failed" || !strings.Contains(p["message"].(string), "новые изменения маршрутов заблокированы") {
		t.Fatal(p["message"])
	}
	if h, ok := env.d.Lock.Current(); !ok || !h.Pending || h.Resumable {
		t.Fatalf("failed rollback released its non-resumable lease: holder=%+v ok=%t", h, ok)
	}
	env.r.failCmd = nil
	// mutations are blocked
	r := env.post("/api/routes/check.cgi?bundleId=whatsapp")
	env.wantErr(r, 409, "ROUTES_OPERATION_BUSY")
	r = env.post("/api/routes/verify.cgi?bundleId=telegram")
	env.wantErr(r, 409, "ROUTES_OPERATION_BUSY")
}

func TestVerifyReasonsAndConflicts(t *testing.T) {
	env := newEnv(t)
	env.wantErr(env.post("/api/routes/verify.cgi?bundleId=telegram"), 409, "ROUTES_SET_NOT_DOWNLOADED")
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	// unreadable running-config → keenetic_read_failed
	env.r.rcErr = context.DeadlineExceeded
	env.e.invalidateCache()
	r := env.post("/api/routes/verify.cgi?bundleId=telegram")
	env.wantErr(r, 502, "ROUTES_VERIFY_ROUTER_FAILED")
	if r.message() != "Keenetic не ответил на безопасное чтение конфигурации. Повторите проверку." {
		t.Fatal(r.message())
	}
	env.r.rcErr = nil
	// a foreign route on Proxy0 that collides with a desired route → conflict
	plan, _ := readObj(env.e.catalogDir("telegram") + "/export-plan.json")
	first := anyList(plan["routes"])[0].(obj)
	env.r.mu.Lock()
	env.r.routes = append(env.r.routes, routeLine(str(first, "network"), str(first, "mask"), "Proxy0", "5"))
	env.r.mu.Unlock()
	st := env.mustOK(env.post("/api/routes/verify.cgi?bundleId=telegram"))
	vr := st["verifyResult"].(map[string]any)
	if vr["result"] != "conflict" || vr["success"] != false {
		t.Fatal(vr)
	}
	pf := env.mustOK(env.post("/api/routes/preflight.cgi?bundleId=telegram&action=export"))
	if pf["ready"] != false || pf["message"] != "Операция заблокирована: обнаружены конфликты маршрутов или владения." {
		t.Fatal(pf["message"])
	}
	// unsupported serialization
	env.r.mu.Lock()
	env.r.routes = append(env.r.routes, "ip route 1.1.1.0 255.255.255.0 Proxy0 1200 weird")
	env.r.mu.Unlock()
	r = env.post("/api/routes/verify.cgi?bundleId=telegram")
	env.wantErr(r, 502, "ROUTES_VERIFY_ROUTER_FAILED")
	if !strings.Contains(r.message(), "неподдерживаемого формата") {
		t.Fatal(r.message())
	}
}

func TestPreflightTokenRules(t *testing.T) {
	env := newEnv(t)
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=meta"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=meta"))
	env.wantErr(env.post("/api/routes/export.cgi?bundleId=meta"), 409, "ROUTES_PREFLIGHT_REQUIRED")
	env.wantErr(env.post("/api/routes/export.cgi?bundleId=meta&preflightToken=xyz"), 409, "ROUTES_PREFLIGHT_REQUIRED")
	tok := env.preflightToken("meta", "export")
	env.wantErr(env.post("/api/routes/delete.cgi?bundleId=meta&preflightToken="+tok), 409, "ROUTES_PREFLIGHT_REQUIRED")
	// expired
	nowFn = func() time.Time { return time.Now().Add(121 * time.Second) }
	env.wantErr(env.post("/api/routes/export.cgi?bundleId=meta&preflightToken="+tok), 409, "ROUTES_PREFLIGHT_EXPIRED")
	nowFn = time.Now
	// changed
	tok = env.preflightToken("meta", "export")
	s := env.state("meta")
	sub(s, "downloadedVersion")["contentSha256"] = strings.Repeat("0", 64)
	_ = writeObj(env.e.stateFile("meta"), s)
	env.wantErr(env.post("/api/routes/export.cgi?bundleId=meta&preflightToken="+tok), 409, "ROUTES_PREFLIGHT_CHANGED")
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=meta"))
	// single use
	tok = env.preflightToken("meta", "export")
	env.mustOK(env.post("/api/routes/export.cgi?bundleId=meta&preflightToken=" + tok))
	env.wantErr(env.post("/api/routes/export.cgi?bundleId=meta&preflightToken="+tok), 409, "ROUTES_PREFLIGHT_REQUIRED")
	// interface down → not ready
	env.r.ifaceUp = false
	pf := env.mustOK(env.post("/api/routes/preflight.cgi?bundleId=meta&action=delete"))
	if pf["ready"] != false || pf["message"] != "Операция заблокирована: Keenetic или управляемый интерфейс недоступен." {
		t.Fatal(pf["message"])
	}
}

func TestDeletePauseAndResume(t *testing.T) {
	env := newEnv(t)
	verifyBatchSize = 4
	env.installBundle("telegram")
	n := env.r.routeCount("Proxy0")
	removes := 0
	env.r.failCmd = func(cmd string, _ int) bool {
		if strings.HasPrefix(cmd, "no ip route ") {
			removes++
			return removes == 6
		}
		return false
	}
	tok := env.preflightToken("telegram", "delete")
	st := env.mustOK(env.post("/api/routes/delete.cgi?bundleId=telegram&preflightToken=" + tok))
	prog := st["operationProgress"].(map[string]any)
	if prog["resumable"] != true || prog["phase"] != "failed_resumable" || st["status"] != "installed" {
		t.Fatalf("pause: %v", prog)
	}
	if prog["current"].(float64) != 5 || env.r.routeCount("Proxy0") != n-5 {
		t.Fatalf("current=%v router=%d", prog["current"], env.r.routeCount("Proxy0"))
	}
	if !env.d.Lock.Busy() {
		t.Fatal("fence must stay pending")
	}
	os := env.mustOK(env.get("/api/routes/operation-status.cgi"))
	if os["resumable"] != true || os["active"] != true || os["bundleId"] != "telegram" || os["bundleName"] != "Telegram" {
		t.Fatal(os)
	}
	if g := os["globalOperation"].(map[string]any); g["action"] != "resume-required" || g["pending"] != true {
		t.Fatal(g)
	}
	// everything else is busy
	env.wantErr(env.post("/api/routes/check.cgi?bundleId=whatsapp"), 409, "ROUTES_OPERATION_BUSY")
	env.wantErr(env.post("/api/routes/preflight.cgi?bundleId=telegram&action=export"), 409, "ROUTES_OPERATION_BUSY")
	// resume works (3.x resume.cgi was broken)
	env.r.failCmd = nil
	pf := env.mustOK(env.post("/api/routes/preflight.cgi?bundleId=telegram&action=resume"))
	if pf["operation"] != "delete" || pf["resolvedAction"] != "delete" || pf["resume"].(map[string]any)["current"].(float64) != 5 {
		t.Fatal(pf)
	}
	if !env.d.Lock.Busy() {
		t.Fatal("fence must stay pending after resume preflight")
	}
	st = env.mustOK(env.post("/api/routes/resume.cgi?bundleId=telegram&preflightToken=" + pf["token"].(string)))
	if st["status"] != "downloaded" || env.r.routeCount("Proxy0") != 0 {
		t.Fatal(st["status"])
	}
	if dr := st["deleteResult"].(map[string]any); int(dr["deleted"].(float64)) != n {
		t.Fatalf("deleted %v want %d", dr["deleted"], n)
	}
	if env.d.Lock.Busy() {
		t.Fatal("fence must be released")
	}
	env.wantErr(env.post("/api/routes/preflight.cgi?bundleId=telegram&action=resume"), 409, "ROUTES_RESUME_NOT_READY")
}

func TestSharedRoutesBetweenBundles(t *testing.T) {
	env := newEnv(t)
	env.installBundle("facebook")
	fb := env.r.routeCount("Proxy0")
	env.installBundle("meta")
	total := env.r.routeCount("Proxy0")
	reg, _ := readObj(env.e.globalRegistryFile())
	shared := 0
	for _, x := range anyList(reg["routes"]) {
		if len(anyList(x.(obj)["owners"])) > 1 {
			shared++
		}
	}
	if len(anyList(reg["routes"])) != total {
		t.Fatalf("registry %d vs router %d", len(anyList(reg["routes"])), total)
	}
	tok := env.preflightToken("meta", "delete")
	env.mustOK(env.post("/api/routes/delete.cgi?bundleId=meta&preflightToken=" + tok))
	if env.r.routeCount("Proxy0") != fb {
		t.Fatalf("after meta delete %d, want facebook's %d (shared %d)", env.r.routeCount("Proxy0"), fb, shared)
	}
}

func TestBusyAndValidationErrors(t *testing.T) {
	env := newEnv(t)
	cases := []struct {
		method, url string
		status      int
		code        string
	}{
		{"GET", "/api/routes/check.cgi?bundleId=telegram", 405, "METHOD_NOT_ALLOWED"},
		{"POST", "/api/routes/check.cgi", 400, "ROUTES_BUNDLE_REQUIRED"},
		{"POST", "/api/routes/check.cgi?x=1&bundleId=telegram", 400, "ROUTES_BUNDLE_REQUIRED"},
		{"POST", "/api/routes/check.cgi?bundleId=telegram&x=1", 400, "ROUTES_BUNDLE_INVALID"},
		{"POST", "/api/routes/check.cgi?bundleId=Telegram", 400, "ROUTES_BUNDLE_INVALID"},
		{"POST", "/api/routes/check.cgi?bundleId=" + strings.Repeat("a", 64), 400, "ROUTES_BUNDLE_INVALID"},
		{"POST", "/api/routes/check.cgi?bundleId=discord", 404, "ROUTES_BUNDLE_NOT_FOUND"},
		{"POST", "/api/routes/download.cgi?bundleId=telegram", 502, "ROUTES_DOWNLOAD_FAILED"},
		{"POST", "/api/routes/plan.cgi?bundleId=telegram", 409, "ROUTES_PLAN_FAILED"},
		{"POST", "/api/routes/preflight.cgi?bundleId=&action=export", 400, "ROUTES_BUNDLE_INVALID"},
		{"POST", "/api/routes/preflight.cgi?bundleId=telegram&action=install", 400, "ROUTES_PREFLIGHT_ACTION_INVALID"},
		{"POST", "/api/routes/preflight.cgi?bundleId=nope&action=export", 404, "ROUTES_BUNDLE_NOT_FOUND"},
		{"POST", "/api/routes/preflight.cgi?bundleId=telegram&action=resume", 409, "ROUTES_RESUME_NOT_READY"},
		{"POST", "/api/routes/preflight.cgi?bundleId=telegram&action=export", 409, "ROUTES_PREFLIGHT_PLAN_FAILED"},
		{"POST", "/api/routes/preflight.cgi?bundleId=telegram&action=delete", 409, "ROUTES_PREFLIGHT_DELETE_FAILED"},
		{"POST", "/api/routes/export.cgi?preflightToken=x", 400, "ROUTES_BUNDLE_REQUIRED"},
		{"POST", "/api/routes/export.cgi?bundleId=A", 400, "ROUTES_BUNDLE_INVALID"},
		{"POST", "/api/routes/export.cgi?bundleId=zzz", 404, "ROUTES_BUNDLE_NOT_FOUND"},
		{"POST", "/api/routes/resume.cgi?bundleId=telegram&preflightToken=" + strings.Repeat("a", 64), 409, "ROUTES_PREFLIGHT_REQUIRED"},
		{"POST", "/api/routes/stop.cgi", 400, "ROUTES_BUNDLE_REQUIRED"},
		{"GET", "/api/routes/summary.cgi?foo=1", 400, "ROUTES_QUERY_INVALID"},
		{"GET", "/api/routes/summary.cgi?bundleId=A", 400, "ROUTES_BUNDLE_INVALID"},
		{"GET", "/api/routes/progress.cgi?bundleId=nope", 404, "ROUTES_BUNDLE_NOT_FOUND"},
		{"GET", "/api/routes/status.cgi", 400, "ROUTES_BUNDLE_REQUIRED"},
		{"POST", "/api/routes/catalog-summary.cgi", 405, "METHOD_NOT_ALLOWED"},
	}
	for _, tc := range cases {
		r := env.call(tc.method, tc.url, "{}")
		if r.status != tc.status || r.code() != tc.code {
			t.Errorf("%s %s: got %d %s", tc.method, tc.url, r.status, r.raw)
		}
	}
	if r := env.post("/api/routes/stop.cgi"); r.message() != "Не указан идентификатор набора маршрутов." {
		t.Fatal(r.message())
	}
	// another area holds the global fence
	rel, _ := env.d.Lock.TryAcquire("dot", "dot:apply", "dns-over-tls")
	r := env.post("/api/routes/check.cgi?bundleId=telegram")
	env.wantErr(r, 409, "ROUTES_OPERATION_BUSY")
	if r.message() != "Другая конфликтующая операция уже выполняется." {
		t.Fatal(r.message())
	}
	g := env.mustOK(env.get("/api/routes/operation-status.cgi"))["globalOperation"].(map[string]any)
	if g["active"] != true || g["scope"] != "dot" {
		t.Fatal(g)
	}
	rel()
	// interface policy
	cfg, _ := readObj(env.e.configFile())
	cfg["managedInterface"] = "Wireguard0"
	_ = writeObj(env.e.configFile(), cfg)
	env.wantErr(env.post("/api/routes/check.cgi?bundleId=telegram"), 500, "ROUTES_INTERFACE_POLICY_INVALID")
	env.wantErr(env.get("/api/routes/summary.cgi"), 500, "ROUTES_INTERFACE_POLICY_INVALID")
}

func TestSourceIntegrityPinned(t *testing.T) {
	env := newEnv(t)
	b, _ := findBuiltin("wikipedia")
	env.served[b.Directory+"/"+b.Files[0].Name] = []byte("route add 1.2.3.0 mask 255.255.255.0 0.0.0.0\n")
	r := env.post("/api/routes/check.cgi?bundleId=wikipedia")
	env.wantErr(r, 502, "ROUTES_CHECK_FAILED")
	if !strings.Contains(r.details().(string), "не совпал с закреплённой версией") {
		t.Fatal(r.details())
	}
	s := env.state("wikipedia")
	if s["status"] != "check_error" || sub(s, "lastError")["message"] == nil {
		t.Fatal(s["status"])
	}
}

func TestInterruptedProgressAndHomeSnapshot(t *testing.T) {
	env := newEnv(t)
	_ = writeObj(env.e.progressFile("youtube"), obj{"schemaVersion": 2, "kind": "routes", "bundleId": "youtube", "operation": "install",
		"phase": "applying", "current": 3, "total": 10, "percent": 30, "running": true, "success": nil, "rolledBack": false,
		"resumable": false, "updatedAt": "x"})
	p, ok := env.e.readProgress("youtube")
	if !ok || p["phase"] != "interrupted" || p["running"] != false {
		t.Fatal(p)
	}
	fn, ok := compat.SnapshotProducer("routes")
	if !ok {
		t.Fatal("snapshot not registered")
	}
	snap, err := fn(context.Background())
	if err != nil || snap["schemaVersion"] != 2 || snap["availableBundles"] != 12 || snap["health"] == nil {
		t.Fatalf("snapshot %v %v", err, snap["availableBundles"])
	}
	h := snap["health"].(obj)
	if h["module"] != "routes" || h["severity"] != "ok" {
		t.Fatal(h)
	}
}

func TestUninstallStepRemovesAllRoutes(t *testing.T) {
	env := newEnv(t)
	if err := env.e.uninstallRoutes(context.Background()); err != nil {
		t.Fatal("nothing installed must be nil:", err)
	}
	env.installBundle("facebook")
	env.installBundle("meta")
	// a paused routes operation keeps a pending fence: uninstall clears it
	rel, _ := env.d.Lock.TryAcquire("routes", "delete", "meta")
	env.d.Lock.MarkPending(true)
	rel()
	if err := env.e.uninstallRoutes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if env.r.routeCount("Proxy0") != 0 || len(env.e.installedBundles()) != 0 {
		t.Fatal("routes left")
	}
	if !strings.Contains(strings.Join(env.r.running(), "\n"), "Wireguard0") {
		t.Fatal("foreign route removed")
	}
	// failure aborts with an error
	env.installBundle("meta")
	env.r.failCmd = func(cmd string, _ int) bool { return strings.HasPrefix(cmd, "no ip route") }
	if err := env.e.uninstallRoutes(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestNetflixBogonDroppedAndReported(t *testing.T) {
	env := newEnv(t)
	st := env.mustOK(env.post("/api/routes/check.cgi?bundleId=netflix"))
	cr := st["checkResult"].(map[string]any)
	dropped := cr["bogonRoutesDropped"].([]any)
	if len(dropped) != 1 || dropped[0] != "100.64.0.0/10" || cr["bogonRouteCount"].(float64) != 1 {
		t.Fatalf("bogons: %v", cr["bogonRoutesDropped"])
	}
	if !strings.Contains(st["operation"].(map[string]any)["output"].(string), "Отброшено частных и служебных сетей: 1 (100.64.0.0/10)") {
		t.Fatal(st["operation"])
	}
	sf := st["availableVersion"].(map[string]any)["sourceFiles"].([]any)[0].(map[string]any)
	if sf["bogonRoutesDropped"].([]any)[0] != "100.64.0.0/10" {
		t.Fatal(sf)
	}
	st = env.mustOK(env.post("/api/routes/download.cgi?bundleId=netflix"))
	if st["downloadResult"].(map[string]any)["bogonRouteCount"].(float64) != 1 {
		t.Fatal(st["downloadResult"])
	}
	lines, _ := readLines(env.e.catalogDir("netflix") + "/normalized.txt")
	for _, l := range lines {
		if strings.HasPrefix(l, "100.64.") {
			t.Fatal("bogon kept in catalog")
		}
	}
	env.exportBundle("netflix")
	if strings.Contains(strings.Join(env.r.running(), "\n"), "100.64.0.0") {
		t.Fatal("bogon installed in Keenetic")
	}
	if env.r.routeCount("Proxy0") != len(lines) {
		t.Fatal("route count")
	}
	// claude installs cleanly and has nothing to drop
	st = env.mustOK(env.post("/api/routes/check.cgi?bundleId=claude"))
	if st["checkResult"].(map[string]any)["bogonRouteCount"].(float64) != 0 {
		t.Fatal(st["checkResult"])
	}
}

package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
)

// installLayout creates the installer layout of the running version.
func installLayout(t *testing.T, a *area, version string) {
	t.Helper()
	bin := filepath.Join(a.d.Root.Versions(), version, "broray")
	writeFile(t, bin, []byte("#!/bin/sh\n"), 0)
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("versions", version), a.d.Root.Current()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, installer.InitScript(), []byte("#!/bin/sh\n"), 0)
	if err := os.MkdirAll(filepath.Dir(installer.BinLink()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(a.d.Root.Current(), "broray"), installer.BinLink()); err != nil {
		t.Fatal(err)
	}
}

func TestInfoShapeAndLayout(t *testing.T) {
	a := testArea(t)
	r := call(t, "GET", "/api/broray/info.cgi", "", nil)
	if r.Code != 200 || r.Body["ok"] != true {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	b := r.Body
	for _, k := range []string{"version", "installedPackageVersion", "installedPackageRevision", "installedReleaseId", "installedWebUIBuild",
		"reinstallSupported", "opkgRegistrationHealthy", "build", "releaseId", "candidateId", "releaseSource", "releaseCommit",
		"releaseBuiltAt", "webUIBuild", "architecture", "updateChannel", "packageTrack", "updateAvailable", "updateCheckFresh",
		"availableVersion", "availablePackageVersion", "availableCandidateId", "releaseRelation", "candidateRelation", "lastCheckedAt",
		"lastBackup", "backupValid", "backupProblem", "installationHealthy", "versionsConsistent", "components", "protocols", "health",
		"lastOperation", "capabilities", "links", "updatedAt", "universalUpdaterReady", "platformHandoff"} {
		if _, ok := b[k]; !ok {
			t.Errorf("info lacks %s", k)
		}
	}
	if b["version"] != app.Version || b["universalUpdaterReady"] != true || b["updateAvailable"] != false || b["reinstallSupported"] != false {
		t.Fatalf("%v", b)
	}
	if ph := b["platformHandoff"].(map[string]any); ph["state"] != "success" || ph["running"] != false || ph["code"] != "UNIVERSAL_PLATFORM_READY" {
		t.Fatalf("handoff %v", ph)
	}
	// not installed through the installer: honest error
	if b["installationHealthy"] != false || b["versionsConsistent"] != false || b["health"].(map[string]any)["severity"] != "error" {
		t.Fatalf("uninstalled layout must be unhealthy: %v %v", b["installationHealthy"], b["versionsConsistent"])
	}
	if len(b["capabilities"].([]any)) != 9 || len(b["protocols"].([]any)) != 5 {
		t.Fatal("capabilities/protocols")
	}
	// installed layout of the running version → consistent
	installLayout(t, a, app.Version)
	b = call(t, "GET", "/api/broray/info.cgi", "", nil).Body
	if b["opkgRegistrationHealthy"] != true || b["versionsConsistent"] != true {
		t.Fatalf("layout: %v %v", b["opkgRegistrationHealthy"], b["versionsConsistent"])
	}
	// the home snapshot omits the handoff enrichment (like `broray-system info`)
	snap := a.info(context.Background(), false)
	if _, ok := snap["universalUpdaterReady"]; ok {
		t.Fatal("snapshot must not carry universalUpdaterReady")
	}
	if r := call(t, "POST", "/api/broray/info.cgi", "{}", brorayHdr); r.Code != 405 {
		t.Fatal(r.Code)
	}
}

func TestUpdaterMutationsRequireOrigin(t *testing.T) {
	testArea(t)
	for _, path := range []string{
		"/api/broray/update-check.cgi", "/api/broray/update-start.cgi",
		"/api/broray/reinstall.cgi", "/api/broray/restore.cgi",
	} {
		if r := call(t, "POST", path, "{}", map[string]string{"Origin": "http://example.com"}); r.Code != 403 || errCode(r) != "INVALID_REQUEST" {
			t.Errorf("%s missing CSRF header: %d %s", path, r.Code, r.Raw)
		}
		if r := call(t, "POST", path, "{}", map[string]string{"X-BROray-Request": "1"}); r.Code != 403 || errCode(r) != "ORIGIN_REJECTED" {
			t.Errorf("%s missing Origin: %d %s", path, r.Code, r.Raw)
		}
		if r := call(t, "POST", path, "{}", map[string]string{"X-BROray-Request": "1", "Origin": "http://evil.example"}); r.Code != 403 || errCode(r) != "ORIGIN_REJECTED" {
			t.Errorf("%s foreign Origin: %d %s", path, r.Code, r.Raw)
		}
	}
}

func TestUpdaterEndpoints(t *testing.T) {
	a := testArea(t)
	// This case exercises an explicitly unconfigured channel, never the live default.
	cfg := platform.DefaultConfig()
	cfg.Update.ChannelURL = "https://updates.broray.invalid/channels/stable/manifest.json"
	if err := platform.SaveConfig(a.d.Root, cfg); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path   string
		hdr    map[string]string
		method string
		code   int
		err    string
	}{
		{"/api/broray/update-check.cgi", nil, "POST", 403, "INVALID_REQUEST"},
		{"/api/broray/update-check.cgi", map[string]string{"X-BROray-Request": "true"}, "POST", 403, "INVALID_REQUEST"},
		{"/api/broray/update-check.cgi", brorayHdr, "GET", 405, "METHOD_NOT_ALLOWED"},
		{"/api/broray/update-check.cgi", brorayHdr, "POST", 503, "CHANNEL_UNAVAILABLE"},
		{"/api/broray/update-start.cgi", nil, "POST", 403, "INVALID_REQUEST"},
		{"/api/broray/update-start.cgi", brorayHdr, "POST", 409, "UPDATE_CHECK_REQUIRED"},
		{"/api/broray/reinstall.cgi", brorayHdr, "POST", 503, "REINSTALL_SOURCE_UNAVAILABLE"},
		{"/api/broray/restore.cgi", nil, "POST", 403, "INVALID_REQUEST"},
		{"/api/broray/restore.cgi", brorayHdr, "POST", 409, "RESTORE_UNAVAILABLE"},
		{"/api/broray/update-status.cgi", nil, "POST", 405, "METHOD_NOT_ALLOWED"},
	}
	for _, c := range cases {
		r := call(t, c.method, c.path, "{}", c.hdr)
		if r.Code != c.code || errCode(r) != c.err {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
	if r := call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr); errMessage(r) != "Канал обновлений не настроен." || r.Body["ok"] != false {
		t.Fatal(r.Raw)
	}
	if r := call(t, "POST", "/api/broray/restore.cgi", "{}", brorayHdr); r.Body["success"] != false ||
		errMessage(r) != "Восстановление резервной копии недоступно для компактной схемы." {
		t.Fatal(r.Raw)
	}
	// busy global fence → OPERATION_BUSY first
	rel, _ := a.d.Lock.TryAcquire("routes", "export", "x")
	for _, p := range []string{"/api/broray/update-start.cgi", "/api/broray/reinstall.cgi"} {
		if r := call(t, "POST", p, "{}", brorayHdr); r.Code != 409 || errCode(r) != "OPERATION_BUSY" {
			t.Errorf("%s busy: %d %s", p, r.Code, r.Raw)
		}
	}
	rel()
	// idle status
	r := call(t, "GET", "/api/broray/update-status.cgi", "", nil)
	if r.Code != 200 || r.Body["state"] != "idle" || r.Body["running"] != false || r.Body["operationId"] != nil ||
		r.Body["message"] != "Операции ещё не выполнялись." || r.Body["logTail"] != "" {
		t.Fatal(r.Raw)
	}
}

// fakeUninstall replaces every side-effecting uninstall step.
type fakeUninstall struct {
	stages   []string
	failAt   string
	handoff  []bool
	purged   bool
	xrayUp   bool
	xrayDown bool
}

func (f *fakeUninstall) hooks() uninstallHooks {
	step := func(name string) error {
		f.stages = append(f.stages, name)
		if name == f.failAt {
			return errors.New("UNINSTALL_TEST_FAILED: отказ теста")
		}
		return nil
	}
	return uninstallHooks{
		preflight: func(ctx context.Context, full bool) error { return step("preflight") },
		stage:     func(ctx context.Context, s string) error { return step(s) },
		server:    func(ctx context.Context) { _ = step("servers") },
		stopXray:  func(ctx context.Context) (bool, error) { f.xrayDown = true; return true, step("xray") },
		startXray: func(ctx context.Context) error { f.xrayUp = true; return nil },
		purge:     func() error { f.purged = true; return nil },
		handoff:   func(keep bool) error { f.handoff = append(f.handoff, keep); return step("handoff") },
	}
}

func TestUninstallValidation(t *testing.T) {
	a := testArea(t)
	f := &fakeUninstall{}
	a.uninstall = f.hooks()
	big := `{"mode":"normal","confirmation":"` + strings.Repeat("x", 4100) + `"}`
	cases := []struct {
		name, body string
		hdr        map[string]string
		code       int
		err        string
	}{
		{"no header", `{"mode":"normal"}`, map[string]string{"Content-Type": "application/json"}, 403, "INVALID_REQUEST"},
		{"empty", "", brorayHdr, 400, "INVALID_CONTENT_LENGTH"},
		{"too large", big, brorayHdr, 413, "REQUEST_TOO_LARGE"},
		{"not json", `{"mode":`, brorayHdr, 400, "REQUEST_JSON_INVALID"},
		{"array", `[]`, brorayHdr, 400, "REQUEST_JSON_INVALID"},
		{"bad mode", `{"mode":"fast","confirmation":"УДАЛИТЬ BROray"}`, brorayHdr, 200, "INVALID_MODE"},
		{"no mode", `{}`, brorayHdr, 200, "INVALID_MODE"},
		{"phrase mismatch", `{"mode":"full","confirmation":"УДАЛИТЬ BROray"}`, brorayHdr, 200, "CONFIRMATION_REQUIRED"},
		{"phrase trailing space", `{"mode":"normal","confirmation":"УДАЛИТЬ BROray "}`, brorayHdr, 200, "CONFIRMATION_REQUIRED"},
	}
	for _, c := range cases {
		r := call(t, "POST", "/api/broray/uninstall.cgi", c.body, c.hdr)
		if r.Code != c.code || errCode(r) != c.err {
			t.Errorf("%s: %d %s", c.name, r.Code, r.Raw)
		}
	}
	if r := call(t, "POST", "/api/broray/uninstall.cgi", `{"mode":"x"}`, brorayHdr); errMessage(r) != "Неизвестный режим удаления." || r.Body["ok"] != false {
		t.Fatal(r.Raw)
	}
	// conflict with the global fence
	rel, _ := a.d.Lock.TryAcquire("dot", "dot:apply", "")
	r := call(t, "POST", "/api/broray/uninstall.cgi", `{"mode":"normal","confirmation":"УДАЛИТЬ BROray"}`, brorayHdr)
	if r.Code != 200 || errCode(r) != "OPERATION_CONFLICT" || errMessage(r) != "Другая операция BROray выполняется или требует восстановления." {
		t.Fatal(r.Raw)
	}
	rel()
	if len(f.stages) != 0 {
		t.Fatal("nothing may run on rejected requests")
	}
}

func pollStatus(t *testing.T, until func(map[string]any) bool) map[string]any {
	t.Helper()
	var last map[string]any
	waitFor(t, "uninstall status", func() bool {
		last = call(t, "GET", "/api/broray/update-status.cgi", "", nil).Body
		return until(last)
	})
	return last
}

func TestUninstallNormalHandsOffAtTheEnd(t *testing.T) {
	a := testArea(t)
	f := &fakeUninstall{}
	a.uninstall = f.hooks()
	var areaStep []string
	compat.RegisterUninstallStep("publish", func(ctx context.Context) error { areaStep = append(areaStep, "publish"); return nil })
	r := call(t, "POST", "/api/broray/uninstall.cgi", `{"mode":"normal","confirmation":"УДАЛИТЬ BROray"}`, brorayHdr)
	if r.Code != 200 || r.Body["ok"] != true || r.Body["accepted"] != true || r.Body["operation"] != "uninstall" ||
		r.Body["engine"] != "canonical-current-operation" || !strings.HasPrefix(r.Body["operationId"].(string), "uninstall-") {
		t.Fatal(r.Raw)
	}
	id := r.Body["operationId"]
	st := pollStatus(t, func(s map[string]any) bool {
		return strings.Contains(s["logTail"].(string), "Установщик запущен")
	})
	if st["operationId"] != id || st["operation"] != "uninstall" || st["state"] != "running" || st["stage"] != "remove" ||
		st["progress"] != float64(82) || st["running"] != true {
		t.Fatalf("status %v", st)
	}
	want := "preflight routes dot keenetic servers xray publish handoff"
	if got := strings.Join(f.stages, " "); got != want {
		t.Fatalf("order %q", got)
	}
	if len(f.handoff) != 1 || f.handoff[0] != true || f.purged {
		t.Fatalf("normal mode must keep data: %v purged=%v", f.handoff, f.purged)
	}
	if len(areaStep) != 1 {
		t.Fatal("registered area step not run")
	}
	if !a.d.Lock.Busy() || !a.d.Queue.Paused() {
		t.Fatal("the fence stays held and automation paused until the daemon stops")
	}
	// a second request conflicts
	if r := call(t, "POST", "/api/broray/uninstall.cgi", `{"mode":"normal","confirmation":"УДАЛИТЬ BROray"}`, brorayHdr); errCode(r) != "OPERATION_CONFLICT" {
		t.Fatal(r.Raw)
	}
	// info reports the busy operation
	if h := call(t, "GET", "/api/broray/info.cgi", "", nil).Body["health"].(map[string]any); h["lastOperation"] == nil {
		t.Fatal("lastOperation missing")
	}
	// recover is blocked by the running page operation… after the fence check
	// a restart reports the interrupted operation
	p := newPageOps(a)
	if s := p.status(); s["state"] != "error" || s["running"] != false {
		t.Fatalf("after restart %v", s)
	}
}

func TestUninstallFullAndFailure(t *testing.T) {
	a := testArea(t)
	f := &fakeUninstall{}
	a.uninstall = f.hooks()
	r := call(t, "POST", "/api/broray/uninstall.cgi", `{"mode":"full","confirmation":"УДАЛИТЬ BROray ПОЛНОСТЬЮ"}`, brorayHdr)
	if r.Body["ok"] != true {
		t.Fatal(r.Raw)
	}
	pollStatus(t, func(s map[string]any) bool {
		return strings.Contains(s["logTail"].(string), "Установщик запущен")
	})
	if f.handoff[0] != false || f.purged {
		t.Fatal("full mode: no preservation, purge deferred to installer after handoff")
	}

	// failure in the Keenetic cleanup: nothing deleted, fence released, pause restored
	b := testArea(t)
	g := &fakeUninstall{failAt: "dot"}
	b.uninstall = g.hooks()
	r = call(t, "POST", "/api/broray/uninstall.cgi", `{"mode":"normal","confirmation":"УДАЛИТЬ BROray"}`, brorayHdr)
	if r.Body["ok"] != true {
		t.Fatal(r.Raw)
	}
	st := pollStatus(t, func(s map[string]any) bool { return s["running"] == false })
	if st["state"] != "error" || st["stage"] != "dot" || st["progress"] != float64(100) ||
		!strings.Contains(st["message"].(string), "отказ теста.") || st["error"] != st["message"] {
		t.Fatalf("failure status %v", st)
	}
	if strings.Join(g.stages, " ") != "preflight routes dot" || len(g.handoff) != 0 || g.xrayDown {
		t.Fatalf("stopped too late: %v", g.stages)
	}
	if b.d.Lock.Busy() || b.d.Queue.Paused() {
		t.Fatal("fence must be released and automation resumed")
	}
	// failure after Xray stop restarts Xray
	c := testArea(t)
	h := &fakeUninstall{failAt: "handoff"}
	c.uninstall = h.hooks()
	call(t, "POST", "/api/broray/uninstall.cgi", `{"mode":"normal","confirmation":"УДАЛИТЬ BROray"}`, brorayHdr)
	pollStatus(t, func(s map[string]any) bool { return s["running"] == false })
	if !h.xrayUp {
		t.Fatal("xray must be restarted after an aborted handoff")
	}
	if info := call(t, "GET", "/api/broray/info.cgi", "", nil).Body; info["health"].(map[string]any)["reasons"] == nil {
		t.Fatal("reasons")
	}
}

func TestCleanupPlanAndExecute(t *testing.T) {
	a := testArea(t)
	day := 24 * time.Hour
	root := a.d.Root.Dir
	writeFile(t, filepath.Join(root, "tmp", "broray-old"), []byte("12345"), 2*day)
	writeFile(t, filepath.Join(root, "tmp", "broray-new"), []byte("1"), 0)
	writeFile(t, filepath.Join(root, "logs", "xray.log.1"), []byte("0123456789"), 8*day)
	writeFile(t, filepath.Join(root, "logs", "xray.log.2"), []byte("0123456789"), 3*day)
	for i, n := range []string{"user-before-20260101-000000", "user-before-20260201-000000", "user-before-20260301-000000", "user-before-20260401-000000"} {
		writeFile(t, filepath.Join(root, "data", "backup", n+".tar.gz"), []byte("b"), time.Duration(10-i)*day)
	}
	writeFile(t, filepath.Join(root, "data", "routes", "backup", "catalog-1", "x.json"), []byte("{}"), 0)
	cases := []struct {
		name, body string
		code       int
		err        string
	}{
		{"empty", "", 400, "INVALID_CONTENT_LENGTH"},
		{"bad json", "{", 400, "INVALID_JSON"},
		{"null option", `{"temp":null}`, 400, "CLEANUP_OPTIONS_INVALID"},
		{"number option", `{"temp":1}`, 400, "CLEANUP_OPTIONS_INVALID"},
		{"nothing", `{"temp":false,"backups":false,"routeBackups":false,"logs":false}`, 400, "CLEANUP_NOTHING_SELECTED"},
	}
	for _, c := range cases {
		if r := call(t, "POST", "/api/broray/cleanup-plan.cgi", c.body, brorayHdr); r.Code != c.code || errCode(r) != c.err {
			t.Errorf("%s: %d %s", c.name, r.Code, r.Raw)
		}
	}
	if r := call(t, "POST", "/api/broray/cleanup-plan.cgi", "{}", map[string]string{"Content-Type": "application/json"}); r.Code != 403 {
		t.Fatal(r.Code)
	}
	// string "true" is accepted like jq -r
	r := call(t, "POST", "/api/broray/cleanup-plan.cgi", `{"temp":"true","backups":true,"routeBackups":true,"logs":true}`, brorayHdr)
	if r.Code != 200 {
		t.Fatal(r.Raw)
	}
	plan := data(r)
	if plan["candidateCount"] != float64(3) || plan["ttlSeconds"] != float64(120) || len(plan["token"].(string)) != 32 {
		t.Fatalf("plan %v", plan)
	}
	paths := map[string]bool{}
	for _, c := range plan["candidates"].([]any) {
		paths[c.(map[string]any)["path"].(string)] = true
	}
	if !paths["tmp/broray-old"] || !paths["logs/xray.log.1"] || !paths["data/backup/user-before-20260101-000000.tar.gz"] {
		t.Fatalf("candidates %v", paths)
	}
	if plan["estimatedBytes"] != float64(16) || plan["categories"].(map[string]any)["logs"].(map[string]any)["count"] != float64(1) {
		t.Fatalf("bytes/categories %v %v", plan["estimatedBytes"], plan["categories"])
	}
	token := plan["token"].(string)
	// execute validations
	for _, c := range []struct{ body, err string }{
		{`{}`, "CLEANUP_TOKEN_REQUIRED"}, {`{"token":""}`, "CLEANUP_TOKEN_REQUIRED"},
		{`{"token":"XYZ"}`, "CLEANUP_TOKEN_INVALID"}, {`{"token":"0123456789abcdef0123456789abcdef"}`, "CLEANUP_PLAN_NOT_FOUND"},
	} {
		r := call(t, "POST", "/api/broray/cleanup.cgi", c.body, brorayHdr)
		if errCode(r) != c.err {
			t.Errorf("%s: %s", c.body, r.Raw)
		}
	}
	// busy fence
	rel, _ := a.d.Lock.TryAcquire("routes", "export", "x")
	if r := call(t, "POST", "/api/broray/cleanup.cgi", `{"token":"`+token+`"}`, brorayHdr); r.Code != 409 || errCode(r) != "CLEANUP_OPERATION_BUSY" {
		t.Fatal(r.Raw)
	}
	if r := call(t, "POST", "/api/broray/cleanup-plan.cgi", `{}`, brorayHdr); r.Code != 409 || errCode(r) != "CLEANUP_OPERATION_BUSY" {
		t.Fatal(r.Raw)
	}
	rel()
	r = call(t, "POST", "/api/broray/cleanup.cgi", `{"token":"`+token+`"}`, brorayHdr)
	if r.Code != 200 || data(r)["deletedCount"] != float64(3) || data(r)["freedBytes"] != float64(16) || data(r)["ok"] != true {
		t.Fatal(r.Raw)
	}
	if _, err := os.Stat(filepath.Join(root, "tmp", "broray-old")); err == nil {
		t.Fatal("not deleted")
	}
	if _, err := os.Stat(filepath.Join(root, "tmp", "broray-new")); err != nil {
		t.Fatal("young temp file must stay")
	}
	// the plan is single-use
	if r := call(t, "POST", "/api/broray/cleanup.cgi", `{"token":"`+token+`"}`, brorayHdr); errCode(r) != "CLEANUP_PLAN_NOT_FOUND" {
		t.Fatal(r.Raw)
	}
	// the cleanup appears as a finished background operation
	ops := a.d.Queue.Operations()
	if len(ops) != 1 || ops[0]["state"] != "completed" || ops[0]["type"] != "unknown" || ops[0]["cancelability"] != "protected" {
		t.Fatalf("ops %v", ops)
	}
	// changed plan
	writeFile(t, filepath.Join(root, "tmp", "broray-x"), []byte("1"), 2*day)
	token = data(call(t, "POST", "/api/broray/cleanup-plan.cgi", `{"temp":true,"backups":false,"routeBackups":false,"logs":false}`, brorayHdr))["token"].(string)
	writeFile(t, filepath.Join(root, "tmp", "broray-y"), []byte("1"), 2*day)
	if r := call(t, "POST", "/api/broray/cleanup.cgi", `{"token":"`+token+`"}`, brorayHdr); r.Code != 409 || errCode(r) != "CLEANUP_PLAN_CHANGED" {
		t.Fatal(r.Raw)
	}
	// expired plan
	token = data(call(t, "POST", "/api/broray/cleanup-plan.cgi", `{"temp":true}`, brorayHdr))["token"].(string)
	base := time.Now()
	a.now = func() time.Time { return base.Add(5 * time.Minute) }
	if r := call(t, "POST", "/api/broray/cleanup.cgi", `{"token":"`+token+`"}`, brorayHdr); r.Code != 409 || errCode(r) != "CLEANUP_PLAN_EXPIRED" {
		t.Fatal(r.Raw)
	}
	if !cleanupAllowed("temp", "file", "tmp/broray-a") || cleanupAllowed("temp", "file", "data/servers/x.json") ||
		cleanupAllowed("logs", "directory", "logs/a.gz") || cleanupAllowed("temp", "file", "tmp/../x") {
		t.Fatal("allow-list")
	}
}

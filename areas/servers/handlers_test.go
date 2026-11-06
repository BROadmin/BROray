package servers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

type fakeEnv struct {
	d          *compat.Deps
	s          *svc
	applied    [][]byte
	applyErr   error
	ensured    atomic.Int32
	running    atomic.Bool
	health     atomic.Value // "healthy" | "unhealthy"
	probeOK    map[string]bool
	pingOutput string
}

const pingOut = "PING x (1.2.3.4): 56 data bytes\n64 bytes from 1.2.3.4: seq=0 ttl=57 time=40.1 ms\n\n--- x ping statistics ---\n3 packets transmitted, 3 packets received, 0% packet loss\nround-trip min/avg/max = 39.6/42.4/45.5 ms\n"

func newEnv(t *testing.T) *fakeEnv {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root.Data(), "config", "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d := &compat.Deps{Root: root, Log: log.New(io.Discard, "", 0), Ctx: ctx, Svc: &compat.Services{}, Maintenance: true}
	d.Lock = compat.NewGlobalLock(d.RunPath("global-operation.json"))
	d.Queue = compat.NewCoordinator(d)
	e := &fakeEnv{d: d, probeOK: map[string]bool{}, pingOutput: pingOut}
	e.health.Store("healthy")
	d.Svc.XrayApplyConfig = func(ctx context.Context, cfg []byte) error {
		if e.applyErr != nil {
			return e.applyErr
		}
		e.applied = append(e.applied, cfg)
		e.running.Store(true)
		return platform.WriteFileAtomic(root.XrayConfig(), cfg, 0o600)
	}
	d.Svc.XrayRunning = func() bool { return e.running.Load() }
	d.Svc.KeeneticEnsureInterface = func(ctx context.Context) error { e.ensured.Add(1); return nil }
	if err := setup(d); err != nil {
		t.Fatal(err)
	}
	s := cur()
	e.s = s
	s.xrayTest = func(ctx context.Context, path string) error { return nil }
	s.measure = func(ctx context.Context, rec Record, cfg []byte) probeResult {
		if e.probeOK[asString(rec["id"])] {
			return probeResult{Success: true, Stage: "proxy-https"}
		}
		return probeResult{Stage: "proxy-timeout", Error: "Реальный HTTPS-запрос через сервер не прошёл: timeout"}
	}
	s.ping = func(ctx context.Context, a string, c, w int) string { return e.pingOutput }
	s.xrayProcess = func() (int, string, bool) {
		if !e.running.Load() {
			return 0, "", false
		}
		return 4242, "1000", true
	}
	s.socksListening = func(string, int) bool { return e.running.Load() }
	s.stopXray = func(ctx context.Context) error { e.running.Store(false); return nil }
	s.activeProbe = func(ctx context.Context, cfgPath, id string) map[string]any {
		now := s.now()
		st := e.health.Load().(string)
		code := ""
		if st != "healthy" {
			code = "PROXY_HTTPS_FAILED"
		}
		return map[string]any{"method": "current-socks-https", "serverId": id, "status": st, "errorCode": code,
			"endpoint": "127.0.0.1:2080", "attempts": 1, "curlCode": 0, "checkedAt": compat.StampUTC(now), "checkedEpoch": now.Unix()}
	}
	return e
}

func (e *fakeEnv) addServer(t *testing.T, link string) string {
	t.Helper()
	id, err := NewManualID(e.d)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := ParseLink(link, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRecord(e.d, rec); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	return id
}

type resp struct {
	Code int
	Body map[string]any
	Raw  string
}

func call(t *testing.T, method, target, body string, hdr map[string]string) resp {
	t.Helper()
	var r *http.Request
	if body == "\x00nolen" {
		r = httptest.NewRequest(method, target, strings.NewReader("{}"))
		r.ContentLength = -1
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	path := r.URL.Path
	if !cgi.Serve(w, r, path, nil) {
		t.Fatalf("route %s not registered", path)
	}
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return resp{w.Code, m, w.Body.String()}
}

func errCode(r resp) string { return asString(get(r.Body, "error", "code")) }

func TestRoutesRegistered(t *testing.T) {
	newEnv(t)
	want := []string{"summary", "details", "import", "activate", "deactivate", "delete", "check", "quality-batch-complete",
		"auto-switch-status", "auto-switch-save", "quality-refresh-status", "quality-refresh-save"}
	have := strings.Join(cgi.Paths(), " ")
	for _, w := range want {
		if !strings.Contains(have, "/api/servers/"+w+".cgi") {
			t.Errorf("missing %s", w)
		}
	}
}

func TestMethodAndBodyErrors(t *testing.T) {
	newEnv(t)
	cases := []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"POST", "/api/servers/summary.cgi", "", 405, "METHOD_NOT_ALLOWED"},
		{"GET", "/api/servers/import.cgi", "", 405, "METHOD_NOT_ALLOWED"},
		{"POST", "/api/servers/import.cgi", "\x00nolen", 411, "CONTENT_LENGTH_REQUIRED"},
		{"POST", "/api/servers/import.cgi", "", 400, "REQUEST_BODY_REQUIRED"},
		{"POST", "/api/servers/import.cgi", "[1]", 400, "REQUEST_JSON_INVALID"},
		{"POST", "/api/servers/import.cgi", strings.Repeat(" ", 70000), 413, "REQUEST_TOO_LARGE"},
		{"POST", "/api/servers/import.cgi", `{"uri":""}`, 400, "URI_REQUIRED"},
		{"POST", "/api/servers/import.cgi", `{"uri":null,"preview":true}`, 400, "URI_REQUIRED"},
		{"POST", "/api/servers/activate.cgi", `{}`, 400, "SERVER_OPERATION_FAILED"},
		{"POST", "/api/servers/activate.cgi", `{"id":"nope"}`, 400, "SERVER_OPERATION_FAILED"},
		{"POST", "/api/servers/delete.cgi", `{"id":"a/b"}`, 400, "SERVER_OPERATION_FAILED"},
		{"GET", "/api/servers/details.cgi?id=missing", "", 400, "SERVER_OPERATION_FAILED"},
		{"POST", "/api/servers/check.cgi", `{"id":"missing"}`, 400, "SERVER_OPERATION_FAILED"},
	}
	for _, c := range cases {
		r := call(t, c.method, c.path, c.body, nil)
		if r.Code != c.status || errCode(r) != c.code {
			t.Errorf("%s %s %.20q: got %d %s want %d %s", c.method, c.path, c.body, r.Code, errCode(r), c.status, c.code)
		}
	}
	r := call(t, "POST", "/api/servers/activate.cgi", `{"id":"nope"}`, nil)
	if get(r.Body, "error", "details") != "Ошибка: сервер nope не найден" ||
		get(r.Body, "error", "message") != "Операция с сервером завершилась ошибкой." {
		t.Errorf("details: %s", r.Raw)
	}
	r = call(t, "POST", "/api/servers/delete.cgi", `{"id":"a/b"}`, nil)
	if get(r.Body, "error", "details") != "Ошибка: идентификатор сервера содержит недопустимые символы" {
		t.Errorf("delete details: %s", r.Raw)
	}
}

func TestImportPreviewCommitDuplicate(t *testing.T) {
	e := newEnv(t)
	link := "vless://0f6c1b2a-1111-2222-3333-444455556666@nl.example.net:443?type=tcp&security=reality&pbk=PBK&sni=a.com&flow=xtls-rprx-vision#NL"
	r := call(t, "POST", "/api/servers/import.cgi", `{"uri":`+jsonString(link)+`,"preview":true}`, nil)
	if r.Code != 200 || get(r.Body, "data", "duplicate") != nil || get(r.Body, "data", "host") != "nl.example.net" ||
		get(r.Body, "data", "transport") != "raw" || len(asString(get(r.Body, "data", "canonical"))) != 64 {
		t.Fatalf("preview: %s", r.Raw)
	}
	r = call(t, "POST", "/api/servers/import.cgi", `{"uri":`+jsonString(link)+`}`, nil)
	id := asString(get(r.Body, "data", "id"))
	if r.Code != 200 || get(r.Body, "data", "imported") != true || !strings.HasPrefix(id, "manual-") || !RecordExists(e.d, id) {
		t.Fatalf("commit: %s", r.Raw)
	}
	r = call(t, "POST", "/api/servers/import.cgi", `{"uri":`+jsonString(strings.Replace(link, "#NL", "#Other", 1))+`,"preview":true}`, nil)
	if get(r.Body, "data", "duplicate") != "Такая конфигурация уже сохранена: NL." {
		t.Fatalf("duplicate: %s", r.Raw)
	}
	r = call(t, "POST", "/api/servers/import.cgi", `{"uri":"vless://u@h:1?sni=a&sni=b","preview":true}`, nil)
	if r.Code != 400 || errCode(r) != "SERVER_PREVIEW_FAILED" || get(r.Body, "error", "message") != "Ошибка: повторяющийся параметр URI: sni" {
		t.Fatalf("preview error: %s", r.Raw)
	}
	r = call(t, "POST", "/api/servers/import.cgi", `{"uri":"hy2://s@h?header=%7B%7D","preview":true}`, nil)
	if get(r.Body, "error", "message") != "Конфигурация не прошла проверку." {
		t.Fatalf("silent preview error: %s", r.Raw)
	}
	r = call(t, "POST", "/api/servers/import.cgi", `{"uri":"trojan://x"}`, nil)
	if r.Code != 400 || errCode(r) != "SERVER_OPERATION_FAILED" || !strings.Contains(asString(get(r.Body, "error", "details")), "Trojan") {
		t.Fatalf("commit error: %s", r.Raw)
	}
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestDetailsMasksSecretsAndParsesRawID(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "vless://secret-uuid@h.example:443?security=reality&sni=a&pbk=ABCDEFGHIJKLMNOPQRST&sid=01#N")
	r := call(t, "GET", "/api/servers/details.cgi?x=1&id="+strings.ReplaceAll(id, "-", "%2D"), "", nil)
	d, _ := r.Body["data"].(map[string]any)
	if r.Code != 200 || d["id"] != id || d["uuid"] != nil || d["uri"] != nil || get(d, "reality", "publicKey") != "ABCDEF…QRST" ||
		d["active"] != false || get(d, "quality", "freshness") != "unknown" || get(d, "quality", "stale") != true {
		t.Fatalf("details: %s", r.Raw)
	}
	if detailsID("id=a&xid=b") != "b" || detailsID("id=a%2Eb%41") != "a.b%41" {
		t.Fatal("detailsID")
	}
	short := MaskRecord(Record{"reality": map[string]any{"publicKey": "short"}})
	if get(short, "reality", "publicKey") != "••••••" {
		t.Fatal("short key mask")
	}
}

func TestActivateDeactivateDelete(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	b := e.addServer(t, "trojan://pw@b.example:443#B")
	r := call(t, "POST", "/api/servers/activate.cgi", `{"id":"`+a+`"}`, nil)
	if r.Code != 200 || !strings.Contains(asString(get(r.Body, "data", "message")), "Активирован сервер:\nНазвание: A") {
		t.Fatalf("activate: %s", r.Raw)
	}
	if ActiveID(e.d) != a || len(e.applied) != 1 || e.ensured.Load() != 1 {
		t.Fatalf("active=%s applied=%d ensured=%d", ActiveID(e.d), len(e.applied), e.ensured.Load())
	}
	var cfg map[string]any
	_ = json.Unmarshal(e.applied[0], &cfg)
	if get(cfg, "outbounds") == nil || get(cfg["outbounds"].([]any)[0], "protocol") != "trojan" {
		t.Fatalf("config: %s", e.applied[0])
	}
	// delete of the active server is refused, the other one works
	r = call(t, "POST", "/api/servers/delete.cgi", `{"id":"`+a+`"}`, nil)
	if get(r.Body, "error", "details") != "Ошибка: активный сервер сначала необходимо отключить или заменить" {
		t.Fatalf("delete active: %s", r.Raw)
	}
	_ = writeQuality(e.d, b, map[string]any{"status": "available"})
	r = call(t, "POST", "/api/servers/delete.cgi", `{"id":"`+b+`"}`, nil)
	if r.Code != 200 || get(r.Body, "data", "deleted") != true || RecordExists(e.d, b) {
		t.Fatalf("delete: %s", r.Raw)
	}
	if _, err := os.Stat(QualityPath(e.d, b)); !os.IsNotExist(err) {
		t.Fatal("quality file must be removed")
	}
	// apply failure keeps the previous active server
	c := e.addServer(t, "trojan://pw@c.example:443#C")
	e.applyErr = errors.New("restart failed")
	r = call(t, "POST", "/api/servers/activate.cgi", `{"id":"`+c+`"}`, nil)
	if r.Code != 400 || !strings.Contains(asString(get(r.Body, "error", "details")), "не удалось применить сервер") || ActiveID(e.d) != a {
		t.Fatalf("apply failure: %s active=%s", r.Raw, ActiveID(e.d))
	}
	e.applyErr = nil
	// busy fence
	rel, _ := e.d.Lock.TryAcquire("routes", "export", "")
	r = call(t, "POST", "/api/servers/activate.cgi", `{"id":"`+c+`"}`, nil)
	rel()
	if r.Code != 409 || errCode(r) != "OPERATION_BUSY" {
		t.Fatalf("busy: %s", r.Raw)
	}
	_ = compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"schemaVersion": 3, "enabled": true, "selectionRule": "lowest-ping"})
	r = call(t, "POST", "/api/servers/deactivate.cgi", `{}`, nil)
	if r.Code != 200 || get(r.Body, "data", "previousActiveServerId") != a || get(r.Body, "data", "connectionState") != "disabled" {
		t.Fatalf("deactivate: %s", r.Raw)
	}
	cfgAS, _ := readObjectFile(autoSwitchFile(e.d))
	if ActiveID(e.d) != "" || e.running.Load() || cfgAS["enabled"] != false || cfgAS["selectionRule"] != "lowest-ping" {
		t.Fatalf("after deactivate: active=%q running=%v cfg=%v", ActiveID(e.d), e.running.Load(), cfgAS)
	}
	os.Remove(autoSwitchFile(e.d))
	_, _ = e.s.deactivate()
	cfgAS, _ = readObjectFile(autoSwitchFile(e.d))
	if cfgAS["selectionRule"] != "manual" || !jsonEq(cfgAS["schemaVersion"], 1) {
		t.Fatalf("created config: %v", cfgAS)
	}
}

func TestSummaryAndHealth(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	b := e.addServer(t, "vless://u@b.example:443?type=ws&security=tls#B")
	_ = writeQuality(e.d, a, map[string]any{"status": "available", "ping": 42, "rating": "excellent", "measurementSource": "manual"})
	r := call(t, "GET", "/api/servers/summary.cgi", "", nil)
	d := r.Body["data"].(map[string]any)
	if r.Code != 200 || !jsonEq(d["total"], 2) || !jsonEq(d["available"], 1) || d["connectionState"] != "disabled" || d["activeServer"] != nil {
		t.Fatalf("summary: %s", r.Raw)
	}
	rows := d["servers"].([]any)
	if get(rows[0], "id") != a {
		rows[0], rows[1] = rows[1], rows[0]
	}
	if get(rows[0], "id") != a || get(rows[0], "quality", "freshness") != "fresh" || get(rows[1], "transport") != "ws" ||
		get(rows[1], "quality", "status") != "unknown" || get(rows[1], "sourceType") != "manual" {
		t.Fatalf("rows: %v", rows)
	}
	if get(d, "autoSwitch", "selectionRuleLabel") != "Ручной выбор" || get(d, "health", "severity") != "warning" ||
		get(d, "health", "module") != "servers" {
		t.Fatalf("autoSwitch/health: %s", r.Raw)
	}
	// active + running + healthy evidence → connected
	if _, err := e.s.activate(b); err != nil {
		t.Fatal(err)
	}
	ctx := e.s.runtimeContext(b)
	_, ah, _ := e.s.activeMeasure(context.Background(), b)
	_ = e.s.updateState(func(st map[string]any) bool { st["activeHealth"] = ah; return true })
	if ctx == "" || ah["context"] != ctx {
		t.Fatal("context")
	}
	sum, _ := e.s.summary(context.Background())
	if sum["connectionState"] != "connected" || get(sum, "activeConnection", "up") != true || get(sum, "activeServer", "id") != b {
		t.Fatalf("connected: %v %v", sum["connectionState"], sum["activeConnection"])
	}
	e.running.Store(false)
	sum, _ = e.s.summary(context.Background())
	if sum["connectionState"] != "error" || get(sum, "health", "severity") != "error" {
		t.Fatalf("error state: %v", sum["connectionState"])
	}
	// id/filename mismatch fails the whole summary
	rec, _ := ReadRecord(e.d, a)
	rec["id"] = "other"
	_ = compat.WriteJSON(RecordPath(e.d, a), rec)
	r = call(t, "GET", "/api/servers/summary.cgi", "", nil)
	if r.Code != 400 || get(r.Body, "error", "details") != "Ошибка: не удалось сформировать данные сервера "+a {
		t.Fatalf("mismatch: %s", r.Raw)
	}
	if fn, ok := compat.SnapshotProducer("servers"); !ok || fn == nil {
		t.Fatal("snapshot producer")
	}
}

func TestDirectCheck(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	e.probeOK[a] = true
	r := call(t, "POST", "/api/servers/check.cgi", `{"id":"`+a+`"}`, nil)
	if r.Code != 200 || get(r.Body, "data", "success") != true || get(r.Body, "data", "quality", "rating") != "excellent" ||
		!jsonEq(get(r.Body, "data", "quality", "ping"), 42) || !jsonEq(get(r.Body, "data", "quality", "jitter"), 6) {
		t.Fatalf("check: %s", r.Raw)
	}
	e.probeOK[a] = false
	r = call(t, "POST", "/api/servers/check.cgi", `{"id":"`+a+`"}`, nil)
	if r.Code != 400 || errCode(r) != "SERVER_OPERATION_FAILED" || !strings.Contains(asString(get(r.Body, "error", "details")), `"success": false`) {
		t.Fatalf("negative: %s", r.Raw)
	}
	q, _ := readObjectFile(QualityPath(e.d, a))
	if q["status"] != "unavailable" || !jsonEq(q["successfulChecks"], 1) || !jsonEq(q["failedChecks"], 1) || q["lastSuccessAt"] == nil ||
		q["rating"] != "unavailable" || q["measurementSource"] != "manual" {
		t.Fatalf("quality: %v", q)
	}
}

func TestQueuedCheck(t *testing.T) {
	e := newEnv(t)
	go e.d.Queue.Run(e.d.Ctx)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	b := e.addServer(t, "trojan://pw@b.example:443#B")
	e.probeOK[a] = true
	hdr := map[string]string{"X-BROray-Queue": "1", "X-BROray-Request": "operations", "Origin": "http://example.com",
		"Content-Type": "application/json"}
	nonce := strings.Repeat("ab", 16)
	r := call(t, "POST", "http://example.com/api/servers/check.cgi", `{"id":"all","nonce":"`+nonce+`"}`, hdr)
	if r.Code != 202 || r.Body["ok"] != true || r.Body["requestId"] != "q-"+nonce || r.Body["accepted"] != true || !jsonEq(r.Body["priority"], 2) {
		t.Fatalf("queued: %d %s", r.Code, r.Raw)
	}
	waitReq(t, e, "q-"+nonce, "completed")
	st, _ := readObjectFile(stateFile(e.d))
	qr := st["qualityRefresh"].(map[string]any)
	if qr["status"] != "success" || !jsonEq(qr["checkedCount"], 2) || !jsonEq(qr["availableCount"], 1) || qr["lastResult"] != "Проверено: 2 из 2" {
		t.Fatalf("progress: %v", qr)
	}
	q, _ := readObjectFile(QualityPath(e.d, b))
	if q["status"] != "unavailable" || len(asString(q["serverFingerprint"])) != 64 {
		t.Fatalf("quality b: %v", q)
	}
	for _, c := range []struct {
		body, code string
		status     int
	}{
		{`{"id":"missing","nonce":"` + strings.Repeat("cd", 16) + `"}`, "SERVER_NOT_FOUND", 404},
		{`{"id":"all","nonce":"XYZ"}`, "INVALID_REQUEST", 400},
		{`{"id":"all","nonce":"` + nonce + `","x":1}`, "INVALID_REQUEST", 400},
	} {
		r := call(t, "POST", "http://example.com/api/servers/check.cgi", c.body, hdr)
		if r.Code != c.status || errCode(r) != c.code {
			t.Errorf("%s: %d %s", c.body, r.Code, r.Raw)
		}
	}
	// active server with identifiable Xray → servers:active-health (P0)
	if _, err := e.s.activate(a); err != nil {
		t.Fatal(err)
	}
	action, ctx, apiErr := e.s.resolveCheck(a)
	if apiErr != nil || action != actionHealth || ctx != e.s.runtimeContext(a) {
		t.Fatalf("resolve active: %s %v", action, apiErr)
	}
	action, _, _ = e.s.resolveCheck(b)
	if action != actionQuality {
		t.Fatalf("resolve other: %s", action)
	}
	// queue-status view of the quality refresh
	r = call(t, "GET", "/api/servers/quality-refresh-status.cgi", "", nil)
	if get(r.Body, "data", "state", "queueState") != "completed" || get(r.Body, "data", "automation", "paused") != false {
		t.Fatalf("qr status: %s", r.Raw)
	}
}

func waitReq(t *testing.T, e *fakeEnv, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r, ok := e.d.Queue.Request(id); ok && r.State != "queued" && r.State != "running" {
			if r.State != want {
				t.Fatalf("request %s: %s (%s)", id, r.State, r.Message)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request %s did not finish", id)
}

func TestFailoverSwitches(t *testing.T) {
	e := newEnv(t)
	go e.d.Queue.Run(e.d.Ctx)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	b := e.addServer(t, "trojan://pw@b.example:443#B")
	c := e.addServer(t, "trojan://pw@c.example:443#C")
	e.probeOK[b], e.probeOK[c] = true, true
	if _, err := e.s.activate(a); err != nil {
		t.Fatal(err)
	}
	_ = compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"schemaVersion": 3, "enabled": true, "failureThreshold": 1,
		"cooldownMinutes": 10, "minimumRating": "acceptable", "selectionRule": "preferred", "preferredServerId": c,
		"qualityRefreshEnabled": false, "qualityRefreshIntervalMinutes": 60})
	e.health.Store("unhealthy")
	if err := e.s.enqueueDue(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ActiveID(e.d) == a {
		time.Sleep(20 * time.Millisecond)
	}
	if ActiveID(e.d) != c {
		st, _ := readObjectFile(stateFile(e.d))
		t.Fatalf("active %s, state %v", ActiveID(e.d), st)
	}
	time.Sleep(50 * time.Millisecond)
	st, _ := readObjectFile(stateFile(e.d))
	if st["status"] != "switched" || st["lastSwitchTo"] != c || st["lastSwitchName"] != "C" || get(st, "failover", "status") != "switched" ||
		!jsonEq(st["consecutiveFailures"], 0) {
		t.Fatalf("state: %v", st)
	}
	r := call(t, "GET", "/api/servers/auto-switch-status.cgi", "", nil)
	if get(r.Body, "data", "state", "status") != "switched" || get(r.Body, "data", "config", "enabled") != true {
		t.Fatalf("status: %s", r.Raw)
	}
}

func TestEnqueueDueQualitySchedule(t *testing.T) {
	e := newEnv(t)
	e.addServer(t, "trojan://pw@a.example:443#A")
	_ = compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"schemaVersion": 3, "enabled": false, "qualityRefreshEnabled": true,
		"qualityRefreshIntervalMinutes": 30})
	if err := e.s.enqueueDue(); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.d.Queue.FindLive(actionQuality, "all"); !ok {
		t.Fatal("scheduled quality check must be queued")
	}
	rows := e.d.Queue.QueueRows()
	if len(rows) != 1 || !jsonEq(rows[0]["priority"], 3) || rows[0]["source"] != "SERVER_CHECK_AUTO" {
		t.Fatalf("rows %v", rows)
	}
	// not due when the last run is recent
	e2 := newEnv(t)
	_ = compat.WriteJSON(autoSwitchFile(e2.d), map[string]any{"qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 30})
	_ = e2.s.updateState(func(st map[string]any) bool {
		st["qualityRefresh"] = map[string]any{"status": "success", "intervalMinutes": 30, "nextCheckEpoch": time.Now().Unix() + 600}
		return true
	})
	_ = e2.s.enqueueDue()
	if _, ok := e2.d.Queue.FindLive(actionQuality, "all"); ok {
		t.Fatal("not due")
	}
	// paused automation enqueues nothing
	e3 := newEnv(t)
	_ = compat.WriteJSON(autoSwitchFile(e3.d), map[string]any{"qualityRefreshEnabled": true})
	_ = e3.d.Queue.SetPaused(true)
	_ = e3.s.enqueueDue()
	if _, ok := e3.d.Queue.FindLive(actionQuality, "all"); ok {
		t.Fatal("paused")
	}
}

func TestAutoSwitchSaveValidation(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	ok := `{"enabled":true,"failureThreshold":3,"cooldownMinutes":10,"minimumRating":"acceptable","selectionRule":"best-quality","preferredServerId":null}`
	cases := []struct {
		body, code string
		status     int
	}{
		{"", "INVALID_CONTENT_LENGTH", 400},
		{"[1]", "INVALID_JSON", 400},
		{strings.Replace(ok, `"enabled":true`, `"enabled":1`, 1), "INVALID_ENABLED", 400},
		{strings.Replace(ok, `"failureThreshold":3`, `"failureThreshold":"x"`, 1), "INVALID_THRESHOLD", 400},
		{strings.Replace(ok, `"failureThreshold":3`, `"failureThreshold":11`, 1), "INVALID_THRESHOLD", 400},
		{strings.Replace(ok, `"cooldownMinutes":10`, `"cooldownMinutes":0`, 1), "INVALID_COOLDOWN", 400},
		{strings.Replace(ok, `"cooldownMinutes":10`, `"cooldownMinutes":1.5`, 1), "INVALID_COOLDOWN", 400},
		{strings.Replace(ok, `"acceptable"`, `"great"`, 1), "INVALID_MINIMUM_RATING", 400},
		{strings.Replace(ok, `"best-quality"`, `"manual"`, 1), "INVALID_SELECTION_RULE", 400},
		{strings.Replace(ok, `"best-quality"`, `"preferred"`, 1), "PREFERRED_SERVER_REQUIRED", 400},
		{strings.Replace(ok, `null`, `"a/b"`, 1), "INVALID_SERVER_ID", 400},
		{strings.Replace(ok, `null`, `"missing"`, 1), "SERVER_NOT_FOUND", 400},
		{strings.Repeat(" ", 17000), "REQUEST_TOO_LARGE", 413},
	}
	for _, c := range cases {
		r := call(t, "POST", "/api/servers/auto-switch-save.cgi", c.body, nil)
		if r.Code != c.status || errCode(r) != c.code {
			t.Errorf("%.60s: %d %s", c.body, r.Code, r.Raw)
		}
	}
	_ = compat.WriteJSON(autoSwitchFile(e.d), map[string]any{"qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 180})
	r := call(t, "POST", "/api/servers/auto-switch-save.cgi", strings.Replace(strings.Replace(ok, `null`, `"`+id+`"`, 1), `"enabled":true`, `"enabled":"true"`, 1), nil)
	d := r.Body["data"].(map[string]any)
	if r.Code != 200 || d["enabled"] != true || d["preferredServerId"] != id || d["qualityRefreshEnabled"] != true ||
		!jsonEq(d["qualityRefreshIntervalMinutes"], 180) || !jsonEq(d["schemaVersion"], 3) {
		t.Fatalf("saved: %s", r.Raw)
	}
	rel, _ := e.d.Lock.TryAcquire("dot", "dot:apply", "")
	r = call(t, "POST", "/api/servers/auto-switch-save.cgi", ok, nil)
	rel()
	if r.Code != 409 || errCode(r) != "OPERATION_BUSY" {
		t.Fatalf("busy: %s", r.Raw)
	}
}

func TestQualityRefreshSave(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		body, code string
		status     int
	}{
		{`{"enabled":true}`, "INVALID_JSON", 400},
		{`{"enabled":"true","intervalMinutes":60}`, "INVALID_JSON", 400},
		{`{"enabled":true,"intervalMinutes":45}`, "INVALID_INTERVAL", 400},
		{`{"enabled":true,"intervalMinutes":60}`, "CONFIG_INVALID", 500},
	}
	for _, c := range cases {
		r := call(t, "POST", "/api/servers/quality-refresh-save.cgi", c.body, nil)
		if r.Code != c.status || errCode(r) != c.code {
			t.Errorf("%s: %d %s", c.body, r.Code, r.Raw)
		}
	}
	base := map[string]any{"enabled": false, "failureThreshold": 3, "cooldownMinutes": 10, "minimumRating": "acceptable",
		"selectionRule": "manual", "preferredServerId": nil}
	_ = compat.WriteJSON(autoSwitchFile(e.d), base)
	r := call(t, "POST", "/api/servers/quality-refresh-save.cgi", `{"enabled":true,"intervalMinutes":60}`, nil)
	if get(r.Body, "error", "message") != "Текущее правило автовыбора некорректно." {
		t.Fatalf("manual rule: %s", r.Raw)
	}
	base["selectionRule"] = "best-quality"
	_ = compat.WriteJSON(autoSwitchFile(e.d), base)
	r = call(t, "POST", "/api/servers/quality-refresh-save.cgi", `{"enabled":true,"intervalMinutes":180}`, nil)
	if r.Code != 200 || get(r.Body, "data", "enabled") != true || !jsonEq(get(r.Body, "data", "intervalMinutes"), 180) {
		t.Fatalf("save: %s", r.Raw)
	}
	r = call(t, "GET", "/api/servers/quality-refresh-status.cgi", "", nil)
	if get(r.Body, "data", "config", "enabled") != true || !jsonEq(get(r.Body, "data", "config", "intervalMinutes"), 180) ||
		get(r.Body, "data", "state") != nil || get(r.Body, "data", "service", "service") != "auto-switch" {
		t.Fatalf("status: %s", r.Raw)
	}
	// freshness thresholds follow the refresh interval
	stale, expired := e.s.qualityPolicy()
	if stale != 180*60+900 || expired != 180*60*3 {
		t.Fatalf("policy %d %d", stale, expired)
	}
}

func TestAutoSwitchStatusDefaults(t *testing.T) {
	e := newEnv(t)
	r := call(t, "GET", "/api/servers/auto-switch-status.cgi", "", nil)
	d := r.Body["data"].(map[string]any)
	if r.Code != 200 || get(d, "config", "enabled") != false || d["state"] != nil || d["monitor"] != nil ||
		get(d, "connectionMonitor", "running") != false || get(d, "service", "state") != "stopped" || d["logTail"] != nil {
		t.Fatalf("status: %s", r.Raw)
	}
	if e.s.loopRunning.Load() || e.s.monitorRunning.Load() {
		t.Fatal("maintenance mode must not start loops")
	}
}

func TestReapplyActiveService(t *testing.T) {
	e := newEnv(t)
	if err := e.d.Svc.ReapplyActiveServer(context.Background(), "x"); err == nil || !strings.HasPrefix(err.Error(), "ACTIVE_SERVER_MISSING") {
		t.Fatalf("no active: %v", err)
	}
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	_ = setActiveID(e.d, a)
	if err := e.d.Svc.ReapplyActiveServer(context.Background(), "subscription"); err != nil || len(e.applied) != 1 {
		t.Fatalf("reapply: %v %d", err, len(e.applied))
	}
}

// dev3 KN-1811: the first active check on a clean install failed with
// STATE_UNAVAILABLE because 3.x's seeded server-auto-switch.json was missing.
func TestActiveHealthOnCleanInstall(t *testing.T) {
	e := newEnv(t)
	go e.d.Queue.Run(e.d.Ctx)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	if _, err := e.s.activate(a); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(autoSwitchFile(e.d))
	e.health.Store("healthy")
	action, ctx, apiErr := e.s.resolveCheck(a)
	if apiErr != nil || action != actionHealth {
		t.Fatalf("resolve: %s %v", action, apiErr)
	}
	r, _, err := e.d.Queue.Submit(action, a, "USER", ctx, strings.Repeat("ab", 16))
	if err != nil {
		t.Fatal(err)
	}
	waitReq(t, e, r.RequestID, "completed")
	if _, ok := readObjectFile(autoSwitchFile(e.d)); !ok {
		t.Fatal("auto-switch config must be seeded")
	}
}

// DEV5_ROUTER_ACTIVE_SERVER_CHECK_NO_QUALITY (KN-1811): «Проверить» on the
// active server reported success but left ping/jitter/rating empty — 3.x
// routes it to servers:active-health only. 3.5.0: a USER check of the active
// server also refreshes its quality.
func TestActiveCheckRefreshesQuality(t *testing.T) {
	e := newEnv(t)
	go e.d.Queue.Run(e.d.Ctx)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	if _, err := e.s.activate(a); err != nil {
		t.Fatal(err)
	}
	e.probeOK[a] = true
	e.health.Store("healthy")
	action, ctx, apiErr := e.s.resolveCheck(a)
	if apiErr != nil || action != actionHealth {
		t.Fatalf("resolve: %s %v", action, apiErr)
	}
	r, _, err := e.d.Queue.Submit(action, a, "USER", ctx, strings.Repeat("cd", 16))
	if err != nil {
		t.Fatal(err)
	}
	waitReq(t, e, r.RequestID, "completed")
	q, _ := readObjectFile(QualityPath(e.d, a))
	if q["rating"] != "excellent" || !jsonEq(q["ping"], 42) || q["lastCheckedAt"] == nil || q["measurementSource"] != "manual" {
		t.Fatalf("active check left quality empty: %v", q)
	}
	st, _ := e.s.readState()
	if get(st, "activeHealth", "status") != "healthy" {
		t.Fatalf("active health not recorded: %v", st["activeHealth"])
	}
}

func TestActiveCheckQualityWriteErrorKeepsHealth(t *testing.T) {
	e := newEnv(t)
	go e.d.Queue.Run(e.d.Ctx)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	if _, err := e.s.activate(a); err != nil {
		t.Fatal(err)
	}
	e.probeOK[a] = true
	e.health.Store("healthy")
	// A directory at the quality file path makes only the optional quality
	// record write fail. The active traffic check still succeeds.
	if err := os.MkdirAll(QualityPath(e.d, a), 0o700); err != nil {
		t.Fatal(err)
	}
	action, ctx, apiErr := e.s.resolveCheck(a)
	if apiErr != nil || action != actionHealth {
		t.Fatalf("resolve: %s %v", action, apiErr)
	}
	r, _, err := e.d.Queue.Submit(action, a, "USER", ctx, strings.Repeat("ef", 16))
	if err != nil {
		t.Fatal(err)
	}
	waitReq(t, e, r.RequestID, "completed")
	st, _ := e.s.readState()
	if get(st, "activeHealth", "status") != "healthy" {
		t.Fatalf("active health lost after optional quality failure: %v", st["activeHealth"])
	}
	logBytes, err := os.ReadFile(e.s.logPath())
	if err != nil || !strings.Contains(string(logBytes), "SERVER_ACTIVE_QUALITY=FAIL SERVER="+a) {
		t.Fatalf("optional quality failure was not logged: %v %q", err, logBytes)
	}
}

// DEV5_SERVER_MANUAL_SUBSCRIPTION_DUPLICATE (owner decision 2026-11-02): the
// manual record and the same server from a subscription stay two cards, and
// each names the other.
func TestSummaryMarksSameServer(t *testing.T) {
	e := newEnv(t)
	manual := e.addServer(t, "trojan://pw@a.example:443#Manual")
	other := e.addServer(t, "trojan://pw@b.example:443#Other")
	sub, err := ParseLink("trojan://pw@a.example:443#FromSub", "sub-abc-1", map[string]any{"type": "subscription", "subscriptionId": "abc", "nodeIndex": 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRecord(e.d, sub); err != nil {
		t.Fatal(err)
	}
	sum, err := e.s.summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	same := map[string][]any{}
	for _, r := range sum["servers"].([]any) {
		row := r.(map[string]any)
		same[row["id"].(string)], _ = row["sameAs"].([]any)
	}
	if len(same[manual]) != 1 || same[manual][0].(map[string]any)["id"] != "sub-abc-1" ||
		same[manual][0].(map[string]any)["sourceType"] != "subscription" || same[manual][0].(map[string]any)["name"] != "FromSub" {
		t.Fatalf("manual: %v", same[manual])
	}
	if len(same["sub-abc-1"]) != 1 || same["sub-abc-1"][0].(map[string]any)["id"] != manual {
		t.Fatalf("subscription: %v", same["sub-abc-1"])
	}
	if len(same[other]) != 0 {
		t.Fatalf("distinct server marked: %v", same[other])
	}
}

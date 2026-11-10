package xray

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	mxray "github.com/BROadmin/broray/modules/xray"
)

func fakeXray(version string) string {
	return `#!/bin/sh
case "$1" in
version) echo "Xray ` + version + ` (Xray, Penetrates Everything.) 0000000 (go1.25 linux/amd64)"; echo "A unified platform"; exit 0;;
run)
  if [ "$2" = "-test" ]; then
    if grep -q INVALID "$4"; then echo "Failed to start: main: failed to load config"; exit 23; fi
    echo "Configuration OK."; exit 0
  fi
  exec sleep 1000;;
esac
exit 1
`
}

const goodConfig = `{"inbounds":[{"protocol":"socks","listen":"127.0.0.1","port":2080}],"outbounds":[{"protocol":"freedom"}]}`

type harness struct {
	t *testing.T
	s *service
	d *compat.Deps
}

func newHarness(t *testing.T, withBinary bool) *harness {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	a := app.New(root, platform.DefaultConfig(), log.New(io.Discard, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	d := compat.NewDeps(ctx, a)
	s := newService(d)
	s.archiveHeaderValidator = func(h []byte) error {
		if !bytes.HasPrefix(h, []byte("#!/bin/sh\n")) {
			return fmt.Errorf("invalid synthetic executable")
		}
		return nil
	}
	current.Store(s)
	regOnce.Do(register)
	if withBinary {
		writeBinary(t, root.XrayBinary(), "26.10.31")
		if err := os.WriteFile(root.XrayConfig(), []byte(goodConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// a /proc/net with the SOCKS port listening
	pn := t.TempDir()
	_ = os.WriteFile(filepath.Join(pn, "tcp"), []byte("  sl  local_address rem_address   st\n   0: 0100007F:0820 00000000:0000 0A 0\n"), 0o600)
	ProcNet = pn
	startSettle, pollEvery, socksWait, installStartWait = 100*time.Millisecond, 20*time.Millisecond, 2*time.Second, 2*time.Second
	statusCacheTTL = 15 * time.Second
	GitHubAPI = ""
	t.Cleanup(func() {
		_ = a.Xray.Stop(context.Background())
		s.jobs.Wait()
		cancel()
	})
	return &harness{t: t, s: s, d: d}
}

func writeBinary(t *testing.T, path, version string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(fakeXray(version)), 0o755); err != nil {
		t.Fatal(err)
	}
}

type resp struct {
	Status  int
	Success bool
	Data    map[string]any
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details any    `json:"details"`
	}
}

func (h *harness) call(method, path, body string) resp {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	w := httptest.NewRecorder()
	if !cgi.Serve(w, r, r.URL.Path, nil) {
		h.t.Fatalf("route %s missing", path)
	}
	var out resp
	out.Status = w.Code
	var raw struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		h.t.Fatalf("json: %s", w.Body.String())
	}
	out.Success = raw.Success
	_ = json.Unmarshal(raw.Data, &out.Data)
	_ = json.Unmarshal(raw.Error, &out.Error)
	return out
}

func (h *harness) expectErr(r resp, status int, code string) {
	h.t.Helper()
	if r.Status != status || r.Error.Code != code {
		h.t.Fatalf("want %d %s got %d %s %q", status, code, r.Status, r.Error.Code, r.Error.Message)
	}
}

func TestStatusMissingBinary(t *testing.T) {
	h := newHarness(t, false)
	r := h.call("GET", "/api/xray/status.cgi", "")
	if !r.Success || r.Data["installed"] != false || r.Data["lastError"] != "Бинарный файл Xray не найден." {
		t.Fatalf("%v", r.Data)
	}
	health := r.Data["health"].(map[string]any)
	if health["severity"] != "error" || health["reasons"].([]any)[0].(map[string]any)["code"] != "XRAY_BINARY_MISSING" {
		t.Fatal(health)
	}
	h.expectErr(h.call("POST", "/api/xray/status.cgi", ""), 405, "METHOD_NOT_ALLOWED")
}

func TestStatusLifecycleAndCheck(t *testing.T) {
	h := newHarness(t, true)
	r := h.call("GET", "/api/xray/status.cgi", "")
	if r.Data["version"] != "26.10.31" || r.Data["configValid"] != true || r.Data["running"] != false || r.Data["architecture"] != "amd64" {
		t.Fatalf("%v", r.Data)
	}
	if r.Data["health"].(map[string]any)["severity"] != "warning" {
		t.Fatal(r.Data["health"])
	}
	c := h.call("POST", "/api/xray/check-config.cgi", "{}")
	if !c.Success || c.Data["valid"] != true || c.Data["output"] != "Configuration OK." {
		t.Fatalf("%+v", c)
	}
	r = h.call("POST", "/api/xray/start.cgi", "{}")
	if !r.Success || r.Data["running"] != true || r.Data["action"] != "start" {
		t.Fatalf("start %+v", r)
	}
	st := h.call("GET", "/api/xray/status.cgi", "")
	if st.Data["running"] != true || st.Data["socksActive"] != true || st.Data["health"].(map[string]any)["severity"] != "ok" {
		t.Fatalf("status after start %v", st.Data)
	}
	rel, _ := h.d.Lock.TryAcquire("routes", "export", "x")
	h.expectErr(h.call("POST", "/api/xray/stop.cgi", "{}"), 409, "OPERATION_BUSY")
	rel()
	r = h.call("POST", "/api/xray/restart.cgi", "{}")
	if !r.Success || r.Data["running"] != true {
		t.Fatalf("restart %+v", r)
	}
	r = h.call("POST", "/api/xray/stop.cgi", "{}")
	if !r.Success || r.Data["running"] != false {
		t.Fatalf("stop %+v", r)
	}
	_ = os.WriteFile(h.d.Root.XrayConfig(), []byte(`{"INVALID":1}`), 0o600)
	h.expectErr(h.call("POST", "/api/xray/check-config.cgi", "{}"), 422, "XRAY_CONFIG_INVALID")
	r = h.call("POST", "/api/xray/start.cgi", "{}")
	h.expectErr(r, 500, "XRAY_START_FAILED")
	if r.Error.Message != "Xray не был запущен." {
		t.Fatal(r.Error.Message)
	}
	d := h.call("GET", "/api/xray/diagnostics.cgi", "")
	if !d.Success || len(d.Data["checks"].([]any)) != 10 {
		t.Fatalf("%+v", d)
	}
}

func TestApplyConfigRollback(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()
	if err := h.s.ApplyConfig(ctx, []byte(goodConfig)); err != nil {
		t.Fatal(err)
	}
	if !h.d.App.Xray.Running() {
		t.Fatal("xray must run after apply")
	}
	err := h.s.ApplyConfig(ctx, []byte(`{"INVALID":true}`))
	if err == nil || !strings.HasPrefix(err.Error(), "XRAY_CONFIG_INVALID") {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(h.d.Root.XrayConfig()); string(b) != goodConfig {
		t.Fatal("previous config must stay")
	}
	// a config that tests fine but never opens the SOCKS port → rollback
	other := strings.Replace(goodConfig, "2080", "2090", 1)
	err = h.s.ApplyConfig(ctx, []byte(other))
	if err == nil || !strings.Contains(err.Error(), "восстановлена предыдущая") {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(h.d.Root.XrayConfig()); string(b) != goodConfig {
		t.Fatal("rollback must restore the previous config")
	}
}

// fakeGitHub serves the releases API, the .dgst files and the archives.
type fakeRel struct {
	tag, published string
	pre            bool
	archive        []byte
	apiDigest      bool // digest in the API (else only in .dgst)
}

func fakeGitHub(t *testing.T, rels []fakeRel) *httptest.Server {
	t.Helper()
	asset, err := mxray.CurrentAsset()
	if err != nil {
		t.Skip("unsupported host arch")
	}
	var srv *httptest.Server
	ghRel := func(r fakeRel) map[string]any {
		sum := sha256.Sum256(r.archive)
		hexsum := hex.EncodeToString(sum[:])
		base := srv.URL + "/XTLS/Xray-core/releases/download/" + r.tag + "/"
		zip := map[string]any{"id": 1, "name": asset.Zip, "size": len(r.archive), "state": "uploaded", "browser_download_url": base + asset.Zip}
		if r.apiDigest {
			zip["digest"] = "sha256:" + hexsum
		}
		return map[string]any{"tag_name": r.tag, "draft": false, "prerelease": r.pre, "published_at": r.published,
			"assets": []any{zip, map[string]any{"id": 2, "name": asset.Zip + ".dgst", "size": 300, "state": "uploaded", "browser_download_url": base + asset.Zip + ".dgst"}}}
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/api/releases":
			var list []any
			if r.URL.Query().Get("page") == "1" {
				for _, x := range rels {
					list = append(list, ghRel(x))
				}
			}
			_ = json.NewEncoder(w).Encode(list)
			return
		case strings.HasPrefix(p, "/api/releases/tags/"):
			for _, x := range rels {
				if x.tag == strings.TrimPrefix(p, "/api/releases/tags/") {
					_ = json.NewEncoder(w).Encode(ghRel(x))
					return
				}
			}
		default:
			for _, x := range rels {
				base := "/XTLS/Xray-core/releases/download/" + x.tag + "/" + asset.Zip
				sum := sha256.Sum256(x.archive)
				if p == base {
					_, _ = w.Write(x.archive)
					return
				}
				if p == base+".dgst" {
					_, _ = w.Write([]byte("MD5= 0\nSHA2-256= " + hex.EncodeToString(sum[:]) + "\n"))
					return
				}
			}
		}
		http.NotFound(w, r)
	}))
	GitHubAPI = srv.URL + "/api"
	GitHubDownload = srv.URL + "/XTLS/Xray-core/releases/download"
	DownloadBase = srv.URL
	t.Cleanup(func() {
		srv.Close()
		GitHubAPI, GitHubDownload, DownloadBase = "", "https://github.com/XTLS/Xray-core/releases/download", ""
	})
	return srv
}

func TestUpdateCheckCatalog(t *testing.T) {
	h := newHarness(t, true)
	asset, err := mxray.CurrentAsset()
	if err != nil {
		t.Skip("unsupported host arch")
	}
	tested := zipWith(t, "26.10.31")
	sum := sha256.Sum256(tested)
	_ = asset
	old := Registry
	Registry = append([]Compatibility{}, Compatibility{CandidateID: "x", Architecture: archName(), XrayTag: "v26.10.31",
		ArchiveSHA256: hex.EncodeToString(sum[:]), TestedAt: "2026-11-01T00:00:00Z", Status: "compatible", Evidence: "test"},
		Compatibility{CandidateID: "x", Architecture: archName(), XrayTag: "v26.9.1",
			ArchiveSHA256: func() string { s := sha256.Sum256(zipWith(t, "26.9.1")); return hex.EncodeToString(s[:]) }(), TestedAt: "2026-10-01T00:00:00Z", Status: "incompatible", Evidence: "test", Limitations: []string{"xhttp"}})
	defer func() { Registry = old }()
	fakeGitHub(t, []fakeRel{
		{tag: "v27.1.1", published: "2027-01-01T00:00:00Z", archive: zipWith(t, "27.1.1"), apiDigest: true},
		{tag: "v27.2.0-beta", published: "2027-02-01T00:00:00Z", pre: true, archive: zipWith(t, "27.2.0")},
		{tag: "v26.10.31", published: "2026-10-31T00:00:00Z", archive: tested},
		{tag: "v26.9.1", published: "2026-09-01T00:00:00Z", archive: zipWith(t, "26.9.1")},
	})
	r := h.call("POST", "/api/xray/update-check.cgi", "{}")
	if !r.Success || r.Data["currentVersion"] != "26.10.31" || r.Data["updateAvailable"] != true || r.Data["latestVersion"] != "27.1.1" || r.Data["catalogComplete"] != true {
		t.Fatalf("%+v", r.Data)
	}
	byTag := map[string]map[string]any{}
	for _, x := range r.Data["releases"].([]any) {
		m := x.(map[string]any)
		byTag[m["tagName"].(string)] = m
	}
	// 3.x selection: newest 2 stable + up to 5 pre-releases (v26.9.1 is the 3rd stable)
	if len(byTag) != 3 || byTag["v26.9.1"] != nil {
		t.Fatalf("releases %v", byTag)
	}
	st := func(tag string) string { return byTag[tag]["compatibility"].(map[string]any)["status"].(string) }
	if st("v27.1.1") != "untested" || st("v26.10.31") != "compatible" || st("v27.2.0-beta") != "untested" {
		t.Fatalf("compat %v %v %v", st("v27.1.1"), st("v26.10.31"), st("v27.2.0-beta"))
	}
	inc := sha256.Sum256(zipWith(t, "26.9.1"))
	if c := compatibilityFor("v26.9.1", hex.EncodeToString(inc[:])); c["status"] != "incompatible" || c["label"] != "Частично совместима с BROray" {
		t.Fatal(c)
	}
	if byTag["v26.10.31"]["installed"] != true || byTag["v27.2.0-beta"]["prerelease"] != true || len(byTag["v27.2.0-beta"]["archiveSha256"].(string)) != 64 {
		t.Fatal(byTag)
	}
	// Xray missing (first install in 3.5.0): the catalog stays available.
	_ = os.Remove(h.d.Root.XrayBinary())
	r2 := h.call("POST", "/api/xray/update-check.cgi", "{}")
	if r2.Status != 200 || r2.Data["currentVersion"] != "" || r2.Data["updateAvailable"] != true {
		t.Fatalf("missing xray: %d %v", r2.Status, r2.Data)
	}
	// GitHub unreachable → 502 like 3.x
	GitHubAPI = "http://127.0.0.1:1/api"
	h.expectErr(h.call("POST", "/api/xray/update-check.cgi", "{}"), 502, "XRAY_UPDATE_CHECK_FAILED")
}

func zipWith(t *testing.T, version string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("xray")
	_, _ = w.Write([]byte(fakeXray(version)))
	_ = zw.Close()
	return buf.Bytes()
}

func TestInstallJob(t *testing.T) {
	h := newHarness(t, true)
	if _, err := mxray.CurrentAsset(); err != nil {
		t.Skip("unsupported host arch")
	}
	archive := zipWith(t, "26.12.1")
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	if err := h.d.App.Xray.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"", `{"tag":"v26.12.1"}`, `{"tag":"26.12.1","currentVersion":"26.10.31","archiveSha256":"` + digest + `","allowUntested":false,"allowPrerelease":false,"allowDowngrade":false}`,
		`{"tag":"v26.12.1","currentVersion":"26.10.31","archiveSha256":"` + digest + `","allowUntested":"no","allowPrerelease":false,"allowDowngrade":false}`} {
		h.expectErr(h.call("POST", "/api/xray/install.cgi", bad), 400, "XRAY_INSTALL_REQUEST_INVALID")
	}
	// an untested official version needs the owner's consent
	noConsent := `{"tag":"v26.12.1","currentVersion":"26.10.31","archiveSha256":"` + digest + `","allowUntested":false,"allowPrerelease":false,"allowDowngrade":false,"allowPartial":false}`
	if r := h.call("POST", "/api/xray/install.cgi", noConsent); r.Status != 202 {
		t.Fatal(r)
	}
	if res := h.waitJob()["result"].(map[string]any); res["success"] != false || res["error"] != "Подтвердите установку версии, не проверявшейся на совместимость с BROray." {
		t.Fatalf("%v", res)
	}
	body := strings.Replace(noConsent, `"allowUntested":false`, `"allowUntested":true`, 1)
	r := h.call("POST", "/api/xray/install.cgi", body)
	if r.Status != 202 || r.Data["accepted"] != true || r.Data["operation"] != "install" {
		t.Fatalf("%+v", r)
	}
	h.expectErr(h.call("POST", "/api/xray/install.cgi", body), 409, "OPERATION_BUSY")
	final := h.waitJob()
	res := final["result"].(map[string]any)
	if res["success"] != true || res["result"].(map[string]any)["version"] != "26.12.1" {
		t.Fatalf("%v", final)
	}
	if exists(h.d.Root.XrayBinary()+".old") || exists(h.d.Root.XrayBinary()+".new") {
		t.Fatal("no backup copy may remain")
	}
	if v := installedVersion(h.s.versionOutput(context.Background(), h.d.Root.XrayBinary())); v != "26.12.1" || !h.d.App.Xray.Running() {
		t.Fatalf("installed %s running %v", v, h.d.App.Xray.Running())
	}
	// DEV5_XRAY_DIAG_PHYSICAL_TRIAL_UNRECORDED: the started-and-verified
	// install is the device trial the diagnostics look for.
	d := h.call("GET", "/api/xray/diagnostics.cgi", "")
	for _, c := range d.Data["checks"].([]any) {
		if m := c.(map[string]any); m["id"] == "real-device-test" &&
			(m["status"] != "ok" || !strings.Contains(m["details"].(string), "26.12.1: версия, запуск и SOCKS подтверждены")) {
			t.Fatalf("device trial not recorded: %v", m)
		}
	}

	// stale checksum: refused before download
	stale := strings.Replace(body, digest, strings.Repeat("a", 64), 1)
	stale = strings.Replace(stale, `"currentVersion":"26.10.31"`, `"currentVersion":"26.12.1"`, 1)
	r = h.call("POST", "/api/xray/install.cgi", stale)
	if r.Status != 202 {
		t.Fatal(r)
	}
	final = h.waitJob()
	res = final["result"].(map[string]any)
	if res["success"] != false || res["error"] != "Контрольная сумма выбранного релиза изменилась. Повторите проверку версий." {
		t.Fatalf("%v", res)
	}
}

func (h *harness) waitJob() map[string]any {
	h.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		r := h.call("GET", "/api/xray/operation-status.cgi", "")
		if !r.Success {
			h.t.Fatalf("operation-status %+v", r)
		}
		if r.Data["operationRunning"] == false {
			return r.Data
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatal("job did not finish")
	return nil
}

func TestOperationStatusInterrupted(t *testing.T) {
	h := newHarness(t, true)
	r := h.call("GET", "/api/xray/operation-status.cgi", "")
	if !r.Success || r.Data["operation"] != "reinstall" || r.Data["operationRunning"] != false || r.Data["result"] != nil {
		t.Fatalf("%v", r.Data)
	}
	_ = compat.WriteJSON(h.s.pointerPath(), map[string]any{"backgroundOperationId": "op-20261102101530-1234-abcdefabcdef", "operation": "install",
		"operationRunning": true, "running": true, "success": nil, "result": nil, "error": nil})
	h.expectErr(h.call("GET", "/api/xray/operation-status.cgi", ""), 503, "XRAY_OWNER_UNCONFIRMED")
	h.s.recoverInstall()
	r = h.call("GET", "/api/xray/operation-status.cgi", "")
	if !r.Success || r.Data["result"].(map[string]any)["error"] != "Операция Xray прервана. Откройте диагностику." {
		t.Fatalf("%v", r.Data)
	}
}

func TestHelpers(t *testing.T) {
	for tag, ok := range map[string]bool{"v26.10.31": true, "v1.8.24-beta": true, "26.1.1": false, "v1.2": false, "v12345.1.1": false, "v1.1.1_x": false} {
		if validTag(tag) != ok {
			t.Errorf("validTag(%s)", tag)
		}
	}
	if versionKey("25.10.15") >= versionKey("25.12.8") || versionKey("v26.1.1-pre") != versionKey("26.1.1") {
		t.Fatal("versionKey")
	}
	if parseVersion("Xray 26.10.31 (Xray) x") != "26.10.31" {
		t.Fatal("parseVersion")
	}
	l, p, ok := socksInboundBytes([]byte(goodConfig))
	if !ok || l != "127.0.0.1" || p != 2080 {
		t.Fatal(l, p, ok)
	}
	dir := t.TempDir()
	ProcNet = dir
	_ = os.WriteFile(filepath.Join(dir, "tcp6"), []byte("hdr\n 0: 00000000000000000000000000000000:0820 00000000000000000000000000000000:0000 0A\n"), 0o600)
	if !SocksListening("192.168.1.1", 2080) || SocksListening("192.168.1.1", 2081) {
		t.Fatal("tcp6 wildcard")
	}
}

func TestRotateFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "error.log")
	_ = os.WriteFile(p, bytes.Repeat([]byte("x"), 300), 0o600)
	_ = os.WriteFile(p+".1.gz", []byte("old"), 0o600)
	rotateFile(p, 100, 2)
	if st, _ := os.Stat(p); st.Size() != 0 {
		t.Fatal("live file must be truncated")
	}
	if b, _ := os.ReadFile(p + ".2.gz"); string(b) != "old" {
		t.Fatal("archive must shift")
	}
	if _, err := os.Stat(p + ".1.gz"); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapInstallPinned(t *testing.T) {
	h := newHarness(t, false)
	asset, err := mxray.CurrentAsset()
	if err != nil {
		t.Skip("unsupported host arch")
	}
	archive := zipWith(t, "26.12.1")
	sum := sha256.Sum256(archive)
	mxray.Pinned["v26.12.1"] = map[string]string{asset.Zip: hex.EncodeToString(sum[:])}
	defer delete(mxray.Pinned, "v26.12.1")
	fakeGitHub(t, []fakeRel{{tag: "v26.12.1", published: "2026-12-01T00:00:00Z", archive: archive}})
	GitHubAPI = "" // the first install must not depend on the GitHub API
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.s.bootstrapInstall(ctx)
	if v := installedVersion(h.s.versionOutput(ctx, h.d.Root.XrayBinary())); v != "26.12.1" {
		t.Fatalf("bootstrap installed %q", v)
	}
}

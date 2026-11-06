package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

func TestPruneUpdateFilesPreservesForeignLog(t *testing.T) {
	dir := t.TempDir()
	foreign := filepath.Join(dir, "notes.log")
	if err := os.WriteFile(foreign, []byte("owner sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 12; i++ {
		id := fmt.Sprintf("update-202611010000%02d-123", i)
		if err := os.WriteFile(filepath.Join(dir, id+".log"), []byte(id), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pruneUpdateFiles(dir, "update-20261101000012-123")
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "owner sentinel" {
		t.Fatalf("foreign log removed or changed: %q, %v", b, err)
	}
}

func TestPruneUpdateFilesKeepsNewestAcrossOperations(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("update-202611010000%02d-123", i)
		if err := os.WriteFile(filepath.Join(dir, id+".log"), []byte(id), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	newest := filepath.Join(dir, "reinstall-20261103000001-123.log")
	if err := os.WriteFile(newest, []byte("newest"), 0o600); err != nil {
		t.Fatal(err)
	}
	pruneUpdateFiles(dir, "none")
	if b, err := os.ReadFile(newest); err != nil || string(b) != "newest" {
		t.Fatalf("newest cross-operation log was pruned: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "update-20261101000001-123.log")); !os.IsNotExist(err) {
		t.Fatalf("oldest operation log not pruned: %v", err)
	}
}

// testChannel publishes a signed file:// test channel listing versions and
// points the area's config at it. The key is trusted for this test only.
type testChannel struct {
	t   *testing.T
	a   *area
	dir string
	key update.SecretKey
}

func newTestChannel(t *testing.T, a *area) *testChannel {
	t.Helper()
	sk, err := update.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(update.TrustExtra(update.TrustedKey{Name: "ops", PublicKey: sk.Public().Encode(), Channels: []string{"test"}, Test: true}))
	c := &testChannel{t: t, a: a, dir: t.TempDir(), key: sk}
	c.config("test", "file://"+filepath.Join(c.dir, "manifest.json"))
	return c
}

func (c *testChannel) config(channel, url string) {
	cfg := platform.DefaultConfig()
	cfg.Update.Channel, cfg.Update.ChannelURL = channel, url
	if err := platform.SaveConfig(c.a.d.Root, cfg); err != nil {
		c.t.Fatal(err)
	}
}

func (c *testChannel) publish(mut func(m *update.Manifest), versions ...string) {
	c.t.Helper()
	sum := sha256.Sum256([]byte("bin"))
	_ = os.WriteFile(filepath.Join(c.dir, "broray"), []byte("bin"), 0o644)
	now := time.Now().UTC()
	m := update.Manifest{SchemaVersion: 1, Channel: "test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Revoked: []string{}}
	for _, v := range versions {
		m.Releases = append(m.Releases, update.Release{Version: v, Notes: "notes " + v, Installer: map[string]update.Artifact{
			update.Platform(): {URL: "broray", Size: 3, SHA256: hex.EncodeToString(sum[:])}}})
	}
	if mut != nil {
		mut(&m)
	}
	raw, _ := json.Marshal(m)
	p := filepath.Join(c.dir, "manifest.json")
	_ = os.WriteFile(p, raw, 0o644)
	_ = os.WriteFile(p+".sig", c.key.Sign(raw, "ops", "ops"), 0o644)
}

// fakeSpawn records launcher starts and writes the given progress lines.
type fakeSpawn struct {
	mu    sync.Mutex
	args  [][]string
	lines []string
	err   error
}

func (f *fakeSpawn) spawn(args []string, out, errOut string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.args = append(f.args, args)
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(out, []byte(strings.Join(f.lines, "")), 0o600)
}

func ev(e update.Event) string {
	b, _ := json.Marshal(e)
	return string(b) + "\n"
}

const newer = "9.9.9"

func TestUpdateCheckAgainstSignedChannel(t *testing.T) {
	a := testArea(t)
	installLayout(t, a, app.Version)
	c := newTestChannel(t, a)
	c.publish(nil, app.Version, newer)

	r := call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr)
	if r.Code != 200 || r.Body["ok"] != true || r.Body["updateAvailable"] != true || r.Body["availableVersion"] != newer ||
		r.Body["candidateId"] != newer || r.Body["candidateRelation"] != "newer" || r.Body["currentRelease"] != app.Version ||
		r.Body["engine"] != "broray-launcher/1" || r.Body["reinstallAvailable"] != true || r.Body["checkedAt"] == nil {
		t.Fatal(r.Raw)
	}
	info := call(t, "GET", "/api/broray/info.cgi", "", nil).Body
	if info["updateAvailable"] != true || info["availableVersion"] != newer || info["updateCheckFresh"] != true ||
		info["reinstallSupported"] != true || info["updateChannel"] != "test" || info["lastCheckedAt"] == nil {
		t.Fatalf("info %v %v %v %v", info["updateAvailable"], info["reinstallSupported"], info["updateChannel"], info["health"])
	}
	// the cache ages out after an hour
	base := time.Now()
	a.now = func() time.Time { return base.Add(2 * time.Hour) }
	if info := call(t, "GET", "/api/broray/info.cgi", "", nil).Body; info["updateAvailable"] != false || info["updateCheckFresh"] != false || info["reinstallSupported"] != false {
		t.Fatal("stale cache must not offer an update")
	}
	a.now = time.Now

	// current version is the newest
	c.publish(nil, app.Version)
	r = call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr)
	if r.Code != 200 || r.Body["updateAvailable"] != false || r.Body["candidateRelation"] != "same" {
		t.Fatal(r.Raw)
	}
	if r := call(t, "POST", "/api/broray/update-start.cgi", "{}", brorayHdr); r.Code != 409 || errCode(r) != "UPDATE_NOT_AVAILABLE" {
		t.Fatal(r.Raw)
	}
}

func TestUpdateCheckRefusals(t *testing.T) {
	a := testArea(t)
	c := newTestChannel(t, a)
	cases := []struct {
		name   string
		setup  func()
		status int
		code   string
		text   string
	}{
		{"unreachable", func() { _ = os.Remove(filepath.Join(c.dir, "manifest.json")) }, 503, "CHANNEL_UNAVAILABLE", "недоступен"},
		{"http scheme", func() { c.config("test", "http://example.com/manifest.json") }, 503, "CHANNEL_INVALID", "https://"},
		{"no signature", func() { c.publish(nil, newer); _ = os.Remove(filepath.Join(c.dir, "manifest.json.sig")) }, 503, "UPDATE_SIGNATURE_MISSING", "Подпись"},
		{"unknown key", func() {
			other, _ := update.GenerateKey()
			c.publish(nil, newer)
			raw, _ := os.ReadFile(filepath.Join(c.dir, "manifest.json"))
			_ = os.WriteFile(filepath.Join(c.dir, "manifest.json.sig"), other.Sign(raw, "x", "x"), 0o644)
		}, 503, "UPDATE_KEY_UNKNOWN", "не доверяет"},
		{"test key, stable config", func() {
			c.publish(nil, newer)
			c.config("stable", "file://"+filepath.Join(c.dir, "manifest.json"))
		}, 503, "UPDATE_KEY_UNKNOWN", "не доверяет"},
		{"expired", func() {
			c.publish(func(m *update.Manifest) {
				m.IssuedAt, m.ExpiresAt = m.IssuedAt.Add(-48*time.Hour), m.IssuedAt.Add(-24*time.Hour)
			}, newer)
		}, 503, "UPDATE_MANIFEST_EXPIRED", "истёк"},
		{"revoked only", func() {
			c.publish(func(m *update.Manifest) { m.Revoked = []string{newer} }, newer)
		}, 200, "", ""},
		{"wrong arch", func() {
			c.publish(func(m *update.Manifest) {
				a := m.Releases[0].Installer[update.Platform()]
				wrongArch := "mips/softfloat"
				if wrongArch == update.Platform() {
					wrongArch = "arm64"
				}
				m.Releases[0].Installer = map[string]update.Artifact{wrongArch: a}
			}, newer)
		}, 503, "UPDATE_ARCH_UNSUPPORTED", "архитектуры"},
	}
	for _, tc := range cases {
		c.config("test", "file://"+filepath.Join(c.dir, "manifest.json"))
		tc.setup()
		r := call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr)
		if r.Code != tc.status || errCode(r) != tc.code || !strings.Contains(errMessage(r), tc.text) {
			t.Errorf("%s: %d %s", tc.name, r.Code, r.Raw)
		}
		if tc.name == "revoked only" && r.Body["updateAvailable"] != false {
			t.Errorf("revoked release offered: %s", r.Raw)
		}
	}
	// unset clock: refuse instead of trusting the validity period
	c.publish(nil, newer)
	a.now = func() time.Time { return time.Unix(86400, 0) }
	r := call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr)
	if r.Code != 503 || errCode(r) != "UPDATE_CLOCK_UNSET" || !strings.Contains(errMessage(r), "Часы роутера не установлены") {
		t.Fatal(r.Raw)
	}
}

func TestUpdateStartRunsLauncherAndReportsProgress(t *testing.T) {
	a, stopA := testAreaCtx(t, platform.Root{Dir: filepath.Join(t.TempDir(), "broray")})
	installLayout(t, a, app.Version)
	c := newTestChannel(t, a)
	c.publish(nil, app.Version, newer)
	f := &fakeSpawn{lines: []string{
		ev(update.Event{Event: "stage", Stage: "check", Progress: 5, Message: "Проверяется манифест."}),
		ev(update.Event{Event: "stage", Stage: "download", Progress: 10, Message: "Скачивается 1."}),
		ev(update.Event{Event: "stage", Stage: "download", Progress: 20, Message: "Скачивается 2."}),
		"not json\n",
		ev(update.Event{Event: "stage", Stage: "stopping", Progress: 60, Message: "Останавливается."}),
	}}
	a.updater.spawn = f.spawn
	if r := call(t, "POST", "/api/broray/update-start.cgi", "{}", brorayHdr); r.Code != 409 || errCode(r) != "UPDATE_CHECK_REQUIRED" {
		t.Fatal(r.Raw)
	}
	call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr)
	r := call(t, "POST", "/api/broray/update-start.cgi", "{}", brorayHdr)
	if r.Code != 202 || r.Body["accepted"] != true || r.Body["operation"] != "update" || r.Body["state"] != "queued" ||
		!strings.HasPrefix(r.Body["operationId"].(string), "update-") {
		t.Fatal(r.Raw)
	}
	id := r.Body["operationId"].(string)
	want := "launcher apply --channel test --version " + newer + " --operation-id " + id
	if len(f.args) != 1 || strings.Join(f.args[0], " ") != want {
		t.Fatalf("args %v", f.args)
	}
	st := pollStatus(t, func(s map[string]any) bool { return s["stage"] == "stopping" })
	if st["running"] != true || st["progress"] != float64(60) || st["message"] != "Останавливается." || st["operationId"] != id {
		t.Fatalf("%v", st)
	}
	logTail := st["logTail"].(string)
	if strings.Count(logTail, "Скачивается") != 1 {
		t.Fatalf("one log line per stage: %q", logTail)
	}
	// busy while the launcher runs
	if r := call(t, "POST", "/api/broray/reinstall.cgi", "{}", brorayHdr); r.Code != 409 || errCode(r) != "OPERATION_BUSY" {
		t.Fatal(r.Raw)
	}

	// The installer stops this daemon here. The next daemon (new version)
	// reads the persisted operation, resumes at the saved offset and
	// records the launcher's result.
	stopA()
	time.Sleep(3 * watchPollPeriod) // the old daemon's watcher has ended
	b, _ := testAreaCtx(t, a.d.Root)
	pf := b.page.snapshot().ProgressFile
	fh, _ := os.OpenFile(pf, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = fh.WriteString(ev(update.Event{Event: "stage", Stage: "health", Progress: 92, Message: "Проверяется готовность."}) +
		ev(update.Event{Event: "result", Source: "installer", OK: update.Bool(true), Message: "installer"}) + // relayed lines are not final
		ev(update.Event{Event: "result", Source: "launcher", OK: update.Bool(true), State: "committed", Message: "BROray обновлён до версии " + newer + "."}))
	fh.Close()
	if s := b.page.status(); s["running"] != true || s["state"] != "running" {
		t.Fatalf("after restart the update must stay running: %v", s)
	}
	b.resumeWatch(b.d.Ctx)
	st = pollStatus(t, func(s map[string]any) bool { return s["running"] == false })
	if st["state"] != "success" || st["progress"] != float64(100) || st["message"] != "BROray обновлён до версии "+newer+"." || st["error"] != nil {
		t.Fatalf("%v", st)
	}
	if lt := st["logTail"].(string); strings.Count(lt, "Проверяется манифест.") != 1 || !strings.Contains(lt, "Проверяется готовность.") {
		t.Fatalf("log replayed or lost: %q", lt)
	}
}

func TestUpdateWatchReleasesFenceBeforeTerminalStatus(t *testing.T) {
	a := testArea(t)
	progress := filepath.Join(t.TempDir(), "progress.jsonl")
	result := ev(update.Event{Event: "result", Source: "launcher", OK: update.Bool(false), State: "rolled-back", Message: "rolled back"})
	if err := os.WriteFile(progress, []byte(result), 0o600); err != nil {
		t.Fatal(err)
	}
	id := "watch-release-order"
	if !a.page.begin(&pageOp{OperationID: id, Operation: "update", State: "running", Running: true,
		ProgressFile: progress, WatchLock: a.d.Root.InstallerLock()}) {
		t.Fatal("could not begin page operation")
	}
	entered, unblock, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	}()
	go func() {
		a.watch(context.Background(), id, func() { close(entered); <-unblock }, time.Now())
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not reach release callback")
	}
	if !a.page.running() {
		t.Fatal("terminal status published before operation fence release")
	}
	close(unblock)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not complete")
	}
	if a.page.running() {
		t.Fatal("terminal status was not published")
	}
}

func TestUpdateStartFailures(t *testing.T) {
	a := testArea(t)
	a.updater.spawnGrace = 50 * time.Millisecond
	installLayout(t, a, app.Version)
	c := newTestChannel(t, a)
	c.publish(nil, app.Version, newer)
	call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr)

	// the launcher reports a rollback
	f := &fakeSpawn{lines: []string{ev(update.Event{Event: "result", Source: "launcher", OK: update.Bool(false), State: "rolled-back",
		Code: "UPDATE_HEALTH_FAILED", Message: "Не подтвердил готовность. Восстановлена версия " + app.Version + "."})}}
	a.updater.spawn = f.spawn
	if r := call(t, "POST", "/api/broray/update-start.cgi", "{}", brorayHdr); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	st := pollStatus(t, func(s map[string]any) bool { return s["running"] == false })
	if st["state"] != "error" || st["stage"] != "rolled-back" || !strings.Contains(st["error"].(string), "Восстановлена версия") {
		t.Fatalf("%v", st)
	}
	if a.d.Lock.Busy() {
		t.Fatal("fence not released")
	}
	// the launcher vanishes without a result
	f.lines = []string{ev(update.Event{Event: "stage", Stage: "download", Progress: 10, Message: "x"})}
	if r := call(t, "POST", "/api/broray/update-start.cgi", "{}", brorayHdr); r.Code != 202 {
		t.Fatal(r.Raw)
	}
	st = pollStatus(t, func(s map[string]any) bool { return s["running"] == false })
	if st["state"] != "error" || !strings.Contains(st["message"].(string), "без итогового результата") {
		t.Fatalf("%v", st)
	}
	// reinstall: needs the current version in a fresh check
	f.lines = nil
	if r := call(t, "POST", "/api/broray/reinstall.cgi", "{}", brorayHdr); r.Code != 202 || r.Body["operation"] != "reinstall" {
		t.Fatal(r.Raw)
	}
	if got := strings.Join(f.args[len(f.args)-1][:5], " "); got != "launcher apply --channel test --reinstall" {
		t.Fatal(got)
	}
	pollStatus(t, func(s map[string]any) bool { return s["running"] == false })
	c.publish(nil, newer)
	call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr)
	if r := call(t, "POST", "/api/broray/reinstall.cgi", "{}", brorayHdr); r.Code != 503 || errCode(r) != "REINSTALL_SOURCE_UNAVAILABLE" {
		t.Fatal(r.Raw)
	}
	// a launcher already running (terminal) → busy
	l, _ := platform.TryLock(a.d.Root.LauncherLock())
	if r := call(t, "POST", "/api/broray/update-start.cgi", "{}", brorayHdr); r.Code != 409 || errCode(r) != "OPERATION_BUSY" {
		t.Fatal(r.Raw)
	}
	l.Release()
	// spawn failure: error published, fence released
	f.err = os.ErrPermission
	if r := call(t, "POST", "/api/broray/update-start.cgi", "{}", brorayHdr); r.Code != 503 || errCode(r) != "UPDATER_START_FAILED" {
		t.Fatal(r.Raw)
	}
	if a.d.Lock.Busy() || a.page.running() {
		t.Fatal("state after spawn failure")
	}
}

func TestUpdateStartNeedsInstallerLayout(t *testing.T) {
	a := testArea(t)
	c := newTestChannel(t, a)
	c.publish(nil, app.Version, newer)
	call(t, "POST", "/api/broray/update-check.cgi", "{}", brorayHdr)
	if r := call(t, "POST", "/api/broray/update-start.cgi", "{}", brorayHdr); r.Code != 503 || errCode(r) != "UPDATE_LAYOUT_INVALID" {
		t.Fatal(r.Raw)
	}
}

func TestRestoreRunsRecoverForInterruptedJournal(t *testing.T) {
	a := testArea(t)
	f := &fakeSpawn{lines: []string{ev(update.Event{Event: "result", Source: "recover", OK: update.Bool(true), State: "rolled-back",
		Message: "Прерванная операция откатана; восстановлена предыдущая версия."})}}
	a.updater.spawn = f.spawn
	// terminal journal → 3.x behaviour
	j := map[string]any{"schemaVersion": 1, "operationId": "update-1", "from": "3.5.0-dev5", "to": "3.5.1", "state": installer.JCommitted}
	_ = platform.WriteJSONAtomic(a.d.Root.UpdateJournal(), j, 0o600)
	if r := call(t, "POST", "/api/broray/restore.cgi", "{}", brorayHdr); r.Code != 409 || errCode(r) != "RESTORE_UNAVAILABLE" {
		t.Fatal(r.Raw)
	}
	j["state"] = installer.JSwitching
	_ = platform.WriteJSONAtomic(a.d.Root.UpdateJournal(), j, 0o600)
	if reasons := call(t, "GET", "/api/broray/info.cgi", "", nil).Body["health"].(map[string]any)["reasons"].([]any); !strings.Contains(asJSON(reasons), "UPDATE_RECOVERY_REQUIRED") {
		t.Fatalf("%v", reasons)
	}
	r := call(t, "POST", "/api/broray/restore.cgi", "{}", brorayHdr)
	if r.Code != 202 || r.Body["operation"] != "restore" {
		t.Fatal(r.Raw)
	}
	if strings.Join(f.args[0], " ") != "ctl recover --restart --progress json" {
		t.Fatal(f.args)
	}
	st := pollStatus(t, func(s map[string]any) bool { return s["running"] == false })
	if st["state"] != "success" || !strings.Contains(st["message"].(string), "откатана") {
		t.Fatalf("%v", st)
	}
	// an installer at work: left alone
	l, _ := platform.TryLock(a.d.Root.InstallerLock())
	defer l.Release()
	if r := call(t, "POST", "/api/broray/restore.cgi", "{}", brorayHdr); r.Code != 409 || errCode(r) != "RESTORE_UNAVAILABLE" {
		t.Fatal(r.Raw)
	}
}

func asJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestInfoAdvertisesInterruptedRecovery(t *testing.T) {
	a := testArea(t)
	j := map[string]any{
		"schemaVersion": 1, "operationId": "update-1",
		"from": "3.5.0-dev5", "to": "3.5.0-dev6", "state": installer.JSwitching,
	}
	if err := platform.WriteJSONAtomic(a.d.Root.UpdateJournal(), j, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := call(t, "GET", "/api/broray/info.cgi", "", nil).Body["recoveryAvailable"]; got != true {
		t.Fatalf("interrupted journal must advertise recovery, got %v", got)
	}
	j["state"] = installer.JCommitted
	if err := platform.WriteJSONAtomic(a.d.Root.UpdateJournal(), j, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := call(t, "GET", "/api/broray/info.cgi", "", nil).Body["recoveryAvailable"]; got != false {
		t.Fatalf("terminal journal must not advertise recovery, got %v", got)
	}
	j["state"] = installer.JSwitching
	if err := platform.WriteJSONAtomic(a.d.Root.UpdateJournal(), j, 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := platform.TryLock(a.d.Root.InstallerLock())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if got := call(t, "GET", "/api/broray/info.cgi", "", nil).Body["recoveryAvailable"]; got != false {
		t.Fatalf("active installer must not advertise recovery, got %v", got)
	}
}

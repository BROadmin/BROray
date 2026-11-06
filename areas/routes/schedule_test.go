package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/keenetic"
)

// fakeGitHub serves the GitHub API (branches, contents) and raw downloads
// for a set of commits of RockBlack-VPN/ip-address.
type fakeGitHub struct {
	mu          sync.Mutex
	srv         *httptest.Server
	commits     map[string]map[string][]byte // sha → path → content
	head        string
	headDate    string
	apiDown     bool
	listingDown string
	apiCalls    int
	rawCalls    int
}

const commit2 = "2222222222222222222222222222222222222222"

type pauseDuringVerify struct {
	keenetic.Transport
	once  sync.Once
	pause func()
}

func (p *pauseDuringVerify) RunningConfig(ctx context.Context) ([]string, error) {
	p.once.Do(p.pause)
	return p.Transport.RunningConfig(ctx)
}

// pinnedTree reads every file of the catalog directories at the pinned commit.
func pinnedTree(t *testing.T) map[string][]byte {
	t.Helper()
	tree := map[string][]byte{}
	for _, b := range builtins {
		entries, err := os.ReadDir(filepath.Join(cloneDir, b.Directory))
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range entries {
			body, err := os.ReadFile(filepath.Join(cloneDir, b.Directory, en.Name()))
			if err != nil {
				t.Fatal(err)
			}
			tree[b.Directory+"/"+en.Name()] = body
		}
	}
	return tree
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{commits: map[string]map[string][]byte{catalogCommit: pinnedTree(t)}, head: catalogCommit, headDate: catalogCommitDate}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

// addCommit makes a new head commit from the pinned tree with changes
// (path → content; nil removes the file).
func (g *fakeGitHub) addCommit(sha, date string, changes map[string][]byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	tree := map[string][]byte{}
	for k, v := range g.commits[catalogCommit] {
		tree[k] = v
	}
	for k, v := range changes {
		if v == nil {
			delete(tree, k)
		} else {
			tree[k] = v
		}
	}
	g.commits[sha] = tree
	g.head, g.headDate = sha, date
}

func (g *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, _ := url.PathUnescape(r.URL.EscapedPath())
	repo := "/repos/" + catalogRepository + "/"
	switch {
	case strings.HasPrefix(p, repo):
		g.apiCalls++
		if g.apiDown {
			http.Error(w, `{"message":"Server Error"}`, http.StatusBadGateway)
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			http.Error(w, "accept", http.StatusBadRequest)
			return
		}
		rest := strings.TrimPrefix(p, repo)
		switch {
		case rest == "branches/"+catalogBranch:
			_ = json.NewEncoder(w).Encode(map[string]any{"name": catalogBranch, "commit": map[string]any{"sha": g.head,
				"commit": map[string]any{"committer": map[string]any{"date": g.headDate}}}})
		case strings.HasPrefix(rest, "contents/"):
			dir := strings.TrimPrefix(rest, "contents/")
			if dir == g.listingDown {
				http.Error(w, "listing unavailable", http.StatusBadGateway)
				return
			}
			tree, ok := g.commits[r.URL.Query().Get("ref")]
			if !ok {
				http.Error(w, `{"message":"No commit found"}`, http.StatusNotFound)
				return
			}
			items := []map[string]any{}
			for path, body := range tree {
				if filepath.Dir(path) == dir {
					items = append(items, map[string]any{"type": "file", "name": filepath.Base(path), "size": len(body), "sha": gitBlobSHA(body)})
				}
			}
			sort.Slice(items, func(i, j int) bool { return items[i]["name"].(string) < items[j]["name"].(string) })
			_ = json.NewEncoder(w).Encode(items)
		default:
			http.NotFound(w, r)
		}
	default: // raw: /<repo>/<sha>/<path>
		g.rawCalls++
		rest := strings.TrimPrefix(p, "/"+catalogRepository+"/")
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			http.NotFound(w, r)
			return
		}
		body, ok := g.commits[rest[:i]][rest[i+1:]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}
}

func (g *fakeGitHub) setDown(v bool)          { g.mu.Lock(); g.apiDown = v; g.mu.Unlock() }
func (g *fakeGitHub) setListingDown(v string) { g.mu.Lock(); g.listingDown = v; g.mu.Unlock() }
func (g *fakeGitHub) calls() int              { g.mu.Lock(); defer g.mu.Unlock(); return g.apiCalls }

// schedEnv is a routes test env whose raw host and API host are the fake GitHub.
func schedEnv(t *testing.T) (*testEnv, *fakeGitHub) {
	env := newEnv(t)
	g := newFakeGitHub(t)
	env.e.rawBase, env.e.apiBase = g.srv.URL, g.srv.URL
	env.e.http = g.srv.Client()
	env.e.defineScheduleQueue()
	return env, g
}

func telegramPath() string {
	b, _ := findBuiltin("telegram")
	return b.Directory + "/" + b.Files[0].Name
}

// telegramV2 drops the first route of the pinned file and adds two.
func telegramV2(t *testing.T) []byte {
	body, err := readCloneFile("Global/Telegram", "telegram.bat")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	var out []string
	dropped := false
	for _, l := range lines {
		if !dropped && strings.HasPrefix(l, "route add ") {
			dropped = true
			continue
		}
		out = append(out, l)
	}
	out = append(out, "route add 9.9.9.0 mask 255.255.255.0 0.0.0.0", "route add 149.112.112.0 mask 255.255.255.128 0.0.0.0", "")
	return []byte(strings.Join(out, "\n"))
}

func (env *testEnv) setSchedule(hours int, auto bool) {
	env.t.Helper()
	if err := env.e.saveScheduleSettings(schedSettings{IntervalHours: hours, AutoApply: auto, MaxRemovalPercent: 50}); err != nil {
		env.t.Fatal(err)
	}
}

func (env *testEnv) runUpdate() obj {
	env.t.Helper()
	res, _ := env.e.runCatalogUpdate(context.Background(), "SCHEDULER", nil)
	return res
}

func TestPinnedGitBlobSHA(t *testing.T) {
	for _, b := range builtins {
		for _, f := range b.Files {
			body, err := readCloneFile(b.Directory, f.Name)
			if err != nil {
				t.Fatal(err)
			}
			if gitBlobSHA(body) != f.GitSHA || sha256Hex(body) != f.SHA256 || len(body) != f.Size {
				t.Fatalf("%s: pinned data mismatch", f.Name)
			}
		}
	}
}

func TestScheduleNoChange(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	before := env.state("telegram")
	cmds := env.r.cmdCount
	res := env.runUpdate()
	if res["result"] != "no_changes" {
		t.Fatalf("result %v", res)
	}
	// the head equals the pinned commit: the pinned listing stays in use
	if src := env.e.sourceFor(builtins[0]); !src.Pinned {
		t.Fatal("pinned listing replaced")
	}
	after := env.state("telegram")
	if !sameJSON(before["availableVersion"], after["availableVersion"]) || after["scheduledUpdate"] != nil || env.r.cmdCount != cmds {
		t.Fatal("state or Keenetic changed")
	}
	// a second check of the same head does not list the directories again
	calls := g.apiCalls
	env.runUpdate()
	if g.apiCalls != calls+1 {
		t.Fatalf("api calls %d → %d", calls, g.apiCalls)
	}
	st := env.e.scheduleView()
	if st["lastResult"] != "no_changes" || st["lastCheckAt"] == nil {
		t.Fatal(st)
	}
	// a newer commit that does not touch the telegram directory changes nothing either
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{"README.md": []byte("x")})
	if res := env.runUpdate(); res["result"] != "no_changes" {
		t.Fatal(res)
	}
	if !env.e.sourceFor(builtins[0]).Pinned {
		t.Fatal("unchanged directory got a new listing")
	}
}

func subMustComplete(path string) bool {
	rc, ok := readObj(path)
	return ok && rc["complete"] == true
}
func TestScheduleUpdateInstalledBundle(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=whatsapp")) // checked, not installed
	routesBefore := env.r.snapshot()
	cmds := env.r.cmdCount
	b, _ := findBuiltin("whatsapp")
	wa, _ := readCloneFile(b.Directory, b.Files[0].Name)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{
		telegramPath():                      telegramV2(t),
		b.Directory + "/" + b.Files[0].Name: append(append([]byte{}, wa...), []byte("route add 192.0.2.0 mask 255.255.255.0 0.0.0.0\r\nroute add 203.0.114.0 mask 255.255.255.0 0.0.0.0\r\n")...),
		"Global/Youtube/Youtube.bat":        []byte("route add 8.8.8.0 mask 255.255.255.0 0.0.0.0\n"),
	})
	res := env.runUpdate()
	if res["result"] != "updates_found" {
		t.Fatalf("result %v", res)
	}
	s := env.state("telegram")
	av, dv, iv := sub(s, "availableVersion"), sub(s, "downloadedVersion"), sub(s, "installedVersion")
	if str(av, "sourceCommit") != commit2 || str(dv, "sourceCommit") != commit2 || str(iv, "contentSha256") == str(dv, "contentSha256") {
		t.Fatalf("versions av=%v dv=%v", av["sourceCommit"], dv["sourceCommit"])
	}
	rc := sub(sub(s, "checkResult"), "routeChanges")
	if intOr(rc["added"], 0) != 2 || intOr(rc["removed"], 0) != 1 || intOr(rc["after"], 0) != 34 {
		t.Fatalf("route changes %v", rc)
	}
	// prepared: the card logic shows «Готово обновление» (update_pending, verified)
	vr := sub(s, "verifyResult")
	if str(sub(vr, "keenetic"), "status") != "update_pending" || str(vr, "contentSha256") != str(dv, "contentSha256") {
		t.Fatalf("verify %v", vr)
	}
	if su := sub(s, "scheduledUpdate"); str(su, "status") != "ready" || str(su, "sourceCommit") != commit2 {
		t.Fatalf("scheduledUpdate %v", su)
	}
	// nothing written to Keenetic without automatic installation
	if env.r.cmdCount != cmds || strings.Join(env.r.snapshot(), "\n") != strings.Join(routesBefore, "\n") {
		t.Fatal("Keenetic changed")
	}
	// the checked, not installed bundle: only the available version
	w := env.state("whatsapp")
	if str(sub(w, "availableVersion"), "sourceCommit") != commit2 || w["downloadedVersion"] != nil {
		t.Fatalf("whatsapp %v", w["availableVersion"])
	}
	// never checked bundle: untouched (its first check uses the new listing)
	if y := env.state("youtube"); y["availableVersion"] != nil {
		t.Fatal("youtube touched")
	}
	yb, _ := findBuiltin("youtube")
	for attempt := 0; attempt < 4 && env.e.sourceFor(yb).Commit != commit2; attempt++ {
		before := g.calls()
		env.runUpdate()
		if used := g.calls() - before; used > 4 {
			t.Fatalf("catalog progress exceeded API budget: %d", used)
		}
	}
	if env.e.sourceFor(yb).Commit != commit2 || subMustComplete(env.e.remoteCatalogFile()) != true {
		t.Fatal("youtube listing not updated after bounded catalog progress")
	}
	if env.r.cmdCount != cmds || strings.Join(env.r.snapshot(), "\n") != strings.Join(routesBefore, "\n") {
		t.Fatal("Keenetic changed during catalog progress")
	}
	// catalog summary: the card needs the owner's action
	sum := env.mustOK(env.get("/api/routes/catalog-summary.cgi"))
	for _, x := range sum["bundles"].([]any) {
		c := x.(map[string]any)
		if c["id"] == "telegram" && (c["keeneticUpdateRequired"] != true || c["scheduledUpdate"] == nil) {
			t.Fatalf("card %v", c["keeneticUpdateRequired"])
		}
	}
	// history logged
	h := anyList(env.e.scheduleView()["history"])
	if len(h) == 0 || !strings.Contains(str(h[len(h)-1].(obj), "message"), "Готово обновление") {
		t.Fatalf("history %v", h)
	}
	// the owner installs it with the normal manual flow
	env.exportBundle("telegram")
	if env.r.routeCount("Proxy0") != 34 {
		t.Fatalf("routes %d", env.r.routeCount("Proxy0"))
	}
	// a second run has nothing to do
	if res := env.runUpdate(); res["result"] != "no_changes" {
		t.Fatalf("second run %v", res)
	}
}

func TestScheduleAutoApplyThroughQueue(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.setSchedule(6, true)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): telegramV2(t)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go env.d.Queue.Run(ctx)
	if !env.e.enqueueDueCheck() {
		t.Fatal("not due")
	}
	if _, live := env.d.Queue.FindLive(actionCatalogUpdate, catalogTarget); !live {
		t.Fatal("not queued")
	}
	waitForWithin(t, time.Minute, func() bool {
		_, live := env.d.Queue.FindLive(actionCatalogUpdate, catalogTarget)
		return !live && env.e.scheduleView()["lastResult"] != nil
	})
	s := env.state("telegram")
	if str(sub(s, "installedVersion"), "sourceCommit") != commit2 || str(sub(s, "scheduledUpdate"), "status") != "applied" {
		t.Fatalf("not applied: %v %v", sub(s, "installedVersion")["sourceCommit"], s["scheduledUpdate"])
	}
	if env.r.routeCount("Proxy0") != 34 {
		t.Fatalf("routes %d", env.r.routeCount("Proxy0"))
	}
	have := map[string]bool{}
	for _, r := range env.r.snapshot() {
		have[r] = true
	}
	if !have[routeLine("9.9.9.0", "255.255.255.0", "Proxy0", "1200")] {
		t.Fatal("new route missing")
	}
	// journal: a SCHEDULER route operation for the automatic export
	found := false
	for _, op := range env.d.Queue.Operations() {
		if op["source"] == "SCHEDULER" && op["type"] == "route_operation" && op["state"] == "completed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no SCHEDULER operation: %v", env.d.Queue.Operations())
	}
	evs := env.d.Queue.Journal().Events()["events"].([]map[string]any)
	if len(evs) == 0 {
		t.Fatal("journal empty")
	}
	// the fence is free again; not due until the interval passes
	if env.d.Lock.Busy() || env.e.enqueueDueCheck() {
		t.Fatal("fence held or due again")
	}
	h := anyList(env.e.scheduleView()["history"])
	if !strings.Contains(str(h[len(h)-1].(obj), "message"), "автоматически") {
		t.Fatalf("history %v", h)
	}
}

func TestScheduleMassRemovalGuard(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.setSchedule(6, true)
	before := env.state("telegram")
	cmds := env.r.cmdCount
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): []byte("route add 91.108.4.0 mask 255.255.252.0 0.0.0.0\r\n")})
	env.runUpdate()
	s := env.state("telegram")
	su := sub(s, "scheduledUpdate")
	if str(su, "status") != "review_required" || str(su, "reason") != "mass_removal" || !strings.Contains(str(su, "message"), "удалит 32 из 33") {
		t.Fatalf("scheduledUpdate %v", su)
	}
	if !sameJSON(before["downloadedVersion"], s["downloadedVersion"]) || !sameJSON(before["installedVersion"], s["installedVersion"]) || env.r.cmdCount != cmds {
		t.Fatal("guarded update was downloaded or applied")
	}
	if str(sub(s, "availableVersion"), "sourceCommit") != commit2 {
		t.Fatal("available version not shown")
	}
	sum := env.mustOK(env.get("/api/routes/catalog-summary.cgi"))
	for _, x := range sum["bundles"].([]any) {
		if c := x.(map[string]any); c["id"] == "telegram" && c["attention"] != true {
			t.Fatal("no attention")
		}
	}
	// the next run waits for the owner and does not log again
	n := len(anyList(env.e.scheduleView()["history"]))
	env.runUpdate()
	if len(anyList(env.e.scheduleView()["history"])) != n || env.r.cmdCount != cmds {
		t.Fatal("guard re-run")
	}
	// the owner accepts it manually: download clears the mark
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	if env.state("telegram")["scheduledUpdate"] != nil {
		t.Fatal("mark kept after manual download")
	}
}

func TestScheduleRejectsInvalidSource(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.setSchedule(6, true)
	before := env.state("telegram")
	bad := append(telegramV2(t), []byte("route add 10.0.0.300 mask 255.0.0.0 0.0.0.0\r\n")...)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): bad})
	env.runUpdate()
	s := env.state("telegram")
	su := sub(s, "scheduledUpdate")
	if str(su, "status") != "review_required" || str(su, "reason") != "invalid_source" || !strings.Contains(str(su, "message"), "Строка") {
		t.Fatalf("scheduledUpdate %v", su)
	}
	if !sameJSON(before["availableVersion"], s["availableVersion"]) || s["lastError"] != nil {
		t.Fatal("rejected version became available or an error")
	}
	// an empty listing (no .bat) keeps the previous listing
	g.addCommit("3333333333333333333333333333333333333333", "2026-11-01T11:00:00Z", map[string][]byte{telegramPath(): nil})
	res := env.runUpdate()
	if src := env.e.sourceFor(builtins[len(builtins)-1]); src.Commit == "3333333333333333333333333333333333333333" {
		t.Fatal("empty listing accepted")
	}
	tb, _ := findBuiltin("telegram")
	if env.e.sourceFor(tb).Commit != commit2 {
		t.Fatalf("telegram listing %s", env.e.sourceFor(tb).Commit)
	}
	if !strings.Contains(str(res, "message"), "Ошибка") {
		t.Fatal(res)
	}
}

func TestScheduleGitHubHeadFailureRetriesSoon(t *testing.T) {
	env, g := schedEnv(t)
	env.setSchedule(6, false)
	at := time.Now().UTC().Truncate(time.Second)
	nowFn = func() time.Time { return at }
	g.setDown(true)
	res, err := env.e.runCatalogUpdate(context.Background(), "SCHEDULER", nil)
	if err == nil || res["result"] != "failed" {
		t.Fatalf("expected failed check: %v %v", res, err)
	}
	st := env.e.readScheduleStatus()
	retry, ok := intOf(st["retryEpoch"])
	if !ok || retry != int(at.Add(30*time.Minute).Unix()) {
		t.Fatalf("GitHub failure waits full interval instead of 30 minutes: %v", st)
	}
	if g.calls() != 1 {
		t.Fatalf("head failure made %d API requests", g.calls())
	}
}

func TestScheduleGitHubListingRetryAndProgressWithinBudget(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.setSchedule(6, false)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): telegramV2(t)})
	g.setListingDown("Global/Telegram")
	before := g.calls()
	env.runUpdate()
	if used := g.calls() - before; used > 4 || used < 2 {
		t.Fatalf("initial listing used %d API requests; budget is 4", used)
	}
	if env.e.readScheduleStatus()["retryEpoch"] == nil {
		t.Fatal("failed listing did not schedule a short retry")
	}
	g.setListingDown("")
	before = g.calls()
	env.runUpdate()
	if used := g.calls() - before; used != catalogAPIBudget {
		t.Fatalf("retry and independent listings used %d requests instead of budget %d", used, catalogAPIBudget)
	}
	tb, _ := findBuiltin("telegram")
	if got := env.e.sourceFor(tb).Commit; got != commit2 {
		t.Fatalf("failed listing did not recover: %s", got)
	}
}

func TestSchedulePersistentFailedListingDoesNotStarveOtherBundles(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): telegramV2(t)})
	g.setListingDown("Global/Telegram")
	for i := 0; i < 5; i++ {
		before := g.calls()
		env.runUpdate()
		if used := g.calls() - before; used > catalogAPIBudget {
			t.Fatalf("check %d exceeded API budget: %d", i, used)
		}
	}
	rc, ok := readObj(env.e.remoteCatalogFile())
	if !ok {
		t.Fatal("catalog receipt missing")
	}
	listed := sub(rc, "listedBundles")
	if len(listed) < 8 {
		t.Fatalf("persistent Telegram failure starved independent bundles: listed=%d", len(listed))
	}
	if sub(rc, "failedListings")["telegram"] != true {
		t.Fatal("failing bundle was dropped from retry set")
	}
}
func TestScheduleAPIFailureKeepsListing(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	g.setDown(true)
	res, err := env.e.runCatalogUpdate(context.Background(), "SCHEDULER", nil)
	if err == nil || res["result"] != "failed" || !strings.Contains(str(res, "message"), "прежняя версия") {
		t.Fatalf("result %v %v", res, err)
	}
	if fileExists(env.e.remoteCatalogFile()) {
		t.Fatal("remote listing written on failure")
	}
	// manual check still works with the pinned listing
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	// an accepted newer listing is kept while the API is down
	g.setDown(false)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): telegramV2(t)})
	env.runUpdate()
	g.setDown(true)
	env.runUpdate()
	tb, _ := findBuiltin("telegram")
	if env.e.sourceFor(tb).Commit != commit2 {
		t.Fatal("listing lost")
	}
	if v := env.e.scheduleView(); v["lastResult"] != "failed" {
		t.Fatal(v["lastResult"])
	}
}

func TestScheduleViewPausedQueuedCheckIsNotRunning(t *testing.T) {
	env, _ := schedEnv(t)
	if err := env.d.Queue.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if _, err := env.d.Queue.Enqueue(actionCatalogUpdate, catalogTarget, "SCHEDULER", "scheduled"); err != nil {
		t.Fatal(err)
	}
	if req, ok := env.d.Queue.FindLive(actionCatalogUpdate, catalogTarget); !ok || req.State != "queued" {
		t.Fatalf("expected paused queued request: %v %v", req, ok)
	}
	if view := env.mustOK(env.get("/api/routes/catalog-schedule.cgi")); view["running"] != false || view["automationPaused"] != true {
		t.Fatalf("paused queued scheduler check shown as running: %v", view)
	}

	userEnv, _ := schedEnv(t)
	if err := userEnv.d.Queue.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if _, err := userEnv.d.Queue.Enqueue(actionCatalogUpdate, catalogTarget, "USER", "manual"); err != nil {
		t.Fatal(err)
	}
	if view := userEnv.mustOK(userEnv.get("/api/routes/catalog-schedule.cgi")); view["running"] != true {
		t.Fatalf("queued user check hidden: %v", view)
	}
}
func TestSchedulePauseBeforeAutoExport(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.setSchedule(6, true)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): telegramV2(t)})
	initialRoutes := env.r.routeCount("Proxy0")
	initialCommands := env.r.cmdCount
	pauseCalled := false
	var pauseErr error
	env.e.T = &pauseDuringVerify{Transport: env.r, pause: func() {
		pauseCalled = true
		pauseErr = env.d.Queue.SetPaused(true)
	}}
	env.e.cacheMu.Lock()
	env.e.cacheAt = time.Time{}
	env.e.cacheMu.Unlock()
	env.runUpdate()
	if !pauseCalled || pauseErr != nil {
		t.Fatalf("verify pause hook: called=%v err=%v", pauseCalled, pauseErr)
	}
	if got := env.r.routeCount("Proxy0"); got != initialRoutes || env.r.cmdCount != initialCommands {
		t.Fatalf("automatic export after pause: routes=%d/%d commands=%d/%d", got, initialRoutes, env.r.cmdCount, initialCommands)
	}
	if su := sub(env.state("telegram"), "scheduledUpdate"); str(su, "status") != "ready" {
		t.Fatalf("expected ready for manual apply after pause: %v", su)
	}
}
func TestSchedulePauseAndBusy(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.setSchedule(6, true)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): telegramV2(t)})
	// automation paused: nothing is queued, and a run does not install
	if err := env.d.Queue.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if env.e.enqueueDueCheck() {
		t.Fatal("queued while paused")
	}
	cmds := env.r.cmdCount
	res := env.runUpdate()
	s := env.state("telegram")
	if str(sub(s, "scheduledUpdate"), "status") != "ready" || env.r.cmdCount != cmds || !strings.Contains(str(res, "message"), "приостановлена") {
		t.Fatalf("paused run: %v %v", s["scheduledUpdate"], res)
	}
	_ = env.d.Queue.SetPaused(false)
	// another operation holds the fence: the step is postponed, a retry is planned
	rel, ok := env.d.Lock.TryAcquire("operations", "servers:quality", "x")
	if !ok {
		t.Fatal("lock")
	}
	res = env.runUpdate()
	rel()
	if !strings.Contains(str(res, "message"), "Отложено") || env.r.cmdCount != cmds {
		t.Fatalf("busy run %v", res)
	}
	if st := env.e.readScheduleStatus(); st["retryEpoch"] == nil {
		t.Fatal("no retry")
	}
	// the retry installs it
	env.runUpdate()
	if str(sub(env.state("telegram"), "installedVersion"), "sourceCommit") != commit2 {
		t.Fatal("not applied after retry")
	}
	if env.d.Lock.Busy() {
		t.Fatal("fence left")
	}
}

func TestScheduleDueLogic(t *testing.T) {
	env, _ := schedEnv(t)
	if env.e.enqueueDueCheck() {
		t.Fatal("off by default")
	}
	defer func() { nowFn = time.Now }()
	base := time.Now()
	nowFn = func() time.Time { return base }
	env.setSchedule(12, false)
	env.e.writeScheduleStatus(func(m obj) { m["lastAttemptEpoch"] = base.Add(-11 * time.Hour).Unix() })
	if env.e.enqueueDueCheck() {
		t.Fatal("due too early")
	}
	nowFn = func() time.Time { return base.Add(2 * time.Hour) }
	if !env.e.enqueueDueCheck() {
		t.Fatal("not due")
	}
}

func (env *testEnv) callH(method, target, body string, hdr map[string]string) resp {
	env.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	path := target
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if !cgi.Serve(w, req, path, nil) {
		env.t.Fatalf("route %s not registered", path)
	}
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return resp{status: w.Code, body: m, raw: w.Body.String()}
}

func TestScheduleHandlers(t *testing.T) {
	env, _ := schedEnv(t)
	v := env.mustOK(env.get("/api/routes/catalog-schedule.cgi"))
	if v["intervalHours"] != float64(0) || v["autoApply"] != false || v["maxRemovalPercent"] != float64(50) {
		t.Fatalf("defaults %v", v)
	}
	ok := map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"}
	env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", `{"intervalHours":6,"autoApply":true}`, map[string]string{"Origin": "http://example.com"}), 403, "CSRF_REJECTED")
	env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", `{"intervalHours":6,"autoApply":true}`, map[string]string{"X-BROray-Request": "operations"}), 403, "ORIGIN_REJECTED")
	env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", `{"intervalHours":6,"autoApply":true}`,
		map[string]string{"X-BROray-Request": "operations", "Origin": "http://evil.example"}), 403, "ORIGIN_REJECTED")
	for _, bad := range []string{`{"intervalHours":5,"autoApply":true}`, `{"intervalHours":6}`, `{"intervalHours":6,"autoApply":"yes"}`,
		`{"intervalHours":6,"autoApply":true,"maxRemovalPercent":95}`, `{"intervalHours":6,"autoApply":true,"url":"https://x"}`, `[]`} {
		env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", bad, ok), 400, map[bool]string{true: "REQUEST_JSON_INVALID", false: "REQUEST_INVALID"}[bad == `[]`])
	}
	d := env.mustOK(env.callH("POST", "/api/routes/catalog-schedule.cgi", `{"intervalHours":24,"autoApply":true,"maxRemovalPercent":30}`, ok))
	if d["intervalHours"] != float64(24) || d["autoApply"] != true || d["maxRemovalPercent"] != float64(30) || d["nextCheckAt"] == nil {
		t.Fatalf("saved %v", d)
	}
	if s := env.e.scheduleSettings(); s.IntervalHours != 24 || !s.AutoApply || s.MaxRemovalPercent != 30 {
		t.Fatal(s)
	}
	env.wantErr(env.callH("POST", "/api/routes/catalog-check.cgi", `{}`, map[string]string{"Origin": "http://example.com"}), 403, "CSRF_REJECTED")
	env.wantErr(env.callH("POST", "/api/routes/catalog-check.cgi", `{}`, map[string]string{"X-BROray-Request": "operations"}), 403, "ORIGIN_REJECTED")
	c := env.mustOK(env.callH("POST", "/api/routes/catalog-check.cgi", `{}`, ok))
	if c["running"] != true || !strings.HasPrefix(str(c, "requestId"), "q-") {
		t.Fatalf("check %v", c)
	}
	if r, live := env.d.Queue.FindLive(actionCatalogUpdate, catalogTarget); !live || r.Source != "USER" {
		t.Fatal("not queued as USER")
	}
	env.wantErr(env.call("GET", "/api/routes/catalog-check.cgi", nil), 405, "METHOD_NOT_ALLOWED")
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	waitForWithin(t, 20*time.Second, cond)
}

// Queued work can legitimately take longer on slow targets; callers keep a bounded budget.
func waitForWithin(t *testing.T, budget time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func TestScheduleAutoApplyFailureRollsBackOnce(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.setSchedule(6, true)
	before := env.r.snapshot()
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): telegramV2(t)})
	env.r.failCmd = func(cmd string, _ int) bool { return strings.HasPrefix(cmd, "ip route 9.9.9.0") }
	env.runUpdate()
	s := env.state("telegram")
	su := sub(s, "scheduledUpdate")
	if str(su, "status") != "apply_failed" || str(sub(s, "installedVersion"), "sourceCommit") != catalogCommit {
		t.Fatalf("scheduledUpdate %v", su)
	}
	if strings.Join(env.r.snapshot(), "|") != strings.Join(before, "|") {
		t.Fatal("router not rolled back")
	}
	failed := false
	for _, op := range env.d.Queue.Operations() {
		if op["source"] == "SCHEDULER" && op["state"] == "failed" {
			failed = true
		}
	}
	if !failed || env.d.Lock.Busy() {
		t.Fatal("no failed SCHEDULER operation or fence left")
	}
	// not repeated automatically
	env.r.failCmd = nil
	cmds := env.r.cmdCount
	env.runUpdate()
	if env.r.cmdCount != cmds {
		t.Fatal("failed installation repeated")
	}
	// the owner can still install it manually
	env.exportBundle("telegram")
	if str(sub(env.state("telegram"), "installedVersion"), "sourceCommit") != commit2 {
		t.Fatal("manual install failed")
	}
}

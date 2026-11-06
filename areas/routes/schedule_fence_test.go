package routes

import (
	"strings"
	"testing"
)

// Every scheduler write of a bundle state (state.scheduledUpdate) happens
// under the global fence: a user check, download or export holding it must
// never be overwritten by a read-modify-write of the scheduler.
func TestScheduleVerdictWaitsForFence(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): telegramV2(t)})
	env.runUpdate()
	if str(sub(env.state("telegram"), "scheduledUpdate"), "status") != "ready" {
		t.Fatal("not ready")
	}
	// the verdict is gone (as after a manual step); checked, downloaded and
	// verified already, so the next run would only record «ready» again
	s := env.state("telegram")
	delete(s, "scheduledUpdate")
	if err := writeObj(env.e.stateFile("telegram"), s); err != nil {
		t.Fatal(err)
	}
	rel, ok := env.d.Lock.TryAcquire("routes", "check", "telegram") // a user check is running
	if !ok {
		t.Fatal("lock")
	}
	res := env.runUpdate()
	if env.state("telegram")["scheduledUpdate"] != nil {
		rel()
		t.Fatal("scheduler wrote the bundle state outside the fence")
	}
	rel()
	if !strings.Contains(str(res, "message"), "Отложено") {
		t.Fatalf("busy run %v", res)
	}
	env.runUpdate()
	if str(sub(env.state("telegram"), "scheduledUpdate"), "status") != "ready" {
		t.Fatal("verdict not recorded after the fence was free")
	}
}

func TestScheduleInvalidSourceVerdictWaitsForFence(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	bad := append(telegramV2(t), []byte("route add 10.0.0.300 mask 255.0.0.0 0.0.0.0\r\n")...)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): bad})
	rel, ok := env.d.Lock.TryAcquire("routes", "download", "telegram")
	if !ok {
		t.Fatal("lock")
	}
	env.runUpdate()
	got := env.state("telegram")["scheduledUpdate"]
	rel()
	if got != nil {
		t.Fatalf("verdict written while the fence was held: %v", got)
	}
	env.runUpdate()
	if su := sub(env.state("telegram"), "scheduledUpdate"); str(su, "reason") != "invalid_source" {
		t.Fatalf("scheduledUpdate %v", su)
	}
}

// «Требует проверки» ends once the owner installed that version manually.
func TestScheduleReviewClearsAfterManualInstall(t *testing.T) {
	env, g := schedEnv(t)
	env.installBundle("telegram")
	env.setSchedule(6, false)
	g.addCommit(commit2, "2026-11-01T10:00:00Z", map[string][]byte{telegramPath(): []byte("route add 91.108.4.0 mask 255.255.252.0 0.0.0.0\r\n")})
	env.runUpdate()
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram")) // the owner looks at it
	env.runUpdate()                                                    // the downloaded version is guarded again
	if su := sub(env.state("telegram"), "scheduledUpdate"); str(su, "status") != "review_required" {
		t.Fatalf("scheduledUpdate %v", su)
	}
	env.exportBundle("telegram") // the owner installs it anyway
	env.runUpdate()
	b := sub(sub(env.e.readScheduleStatus(), "bundles"), "telegram")
	if str(b, "result") == "review_required" {
		t.Fatalf("still «Требует проверки» after the manual install: %v", b)
	}
	if scheduleReviewRequired(env.state("telegram")) {
		t.Fatal("card still needs review")
	}
}

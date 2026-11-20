package routes

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

var scheduleHeaders = map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"}

func TestScheduleSettingsCAS(t *testing.T) {
	env, _ := schedEnv(t)
	env.mustOK(env.callH("POST", "/api/routes/catalog-schedule.cgi", `{"intervalHours":24,"autoApply":false,"maxRemovalPercent":30}`, scheduleHeaders))
	v := env.mustOK(env.get("/api/routes/catalog-schedule.cgi"))
	rev, _ := v["revision"].(string)
	if len(rev) != 64 {
		t.Fatalf("missing revision: %v", v["revision"])
	}
	next := env.mustOK(env.callH("POST", "/api/routes/catalog-schedule.cgi", fmt.Sprintf(`{"autoApply":true,"expectedRevision":%q}`, rev), scheduleHeaders))
	if next["autoApply"] != true || next["intervalHours"] != float64(24) || next["maxRemovalPercent"] != float64(30) {
		t.Fatalf("unrelated fields changed: %v", next)
	}
	env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", fmt.Sprintf(`{"autoApply":false,"expectedRevision":%q}`, rev), scheduleHeaders), 409, "ROUTES_SCHEDULE_CHANGED")
	next = env.mustOK(env.callH("POST", "/api/routes/catalog-schedule.cgi", fmt.Sprintf(`{"intervalHours":6,"expectedRevision":%q}`, next["revision"]), scheduleHeaders))
	if next["autoApply"] != true || next["intervalHours"] != float64(6) || next["maxRemovalPercent"] != float64(30) {
		t.Fatal(next)
	}
}

func TestScheduleSettingsRejectsAmbiguousAndDamaged(t *testing.T) {
	env, _ := schedEnv(t)
	for _, body := range []string{
		`{"intervalHours":6,"autoApply":null}`, `{"intervalHours":6,"autoApply":true,"autoApply":false}`,
		`{"intervalHours":6,"AutoApply":true}`, `{"intervalHours":6,"autoApply":true,"maxRemovalPercent":null}`,
		`{"intervalHours":6,"autoApply":true,"expectedRevision":null}`,
	} {
		env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", body, scheduleHeaders), 400, "REQUEST_INVALID")
	}
	bad := []byte(`{"schemaVersion":1,"intervalHours":24,"autoApply":"bad","maxRemovalPercent":30}`)
	if err := os.WriteFile(env.e.scheduleFile(), bad, 0600); err != nil {
		t.Fatal(err)
	}
	v := env.mustOK(env.get("/api/routes/catalog-schedule.cgi"))
	if v["settingsValid"] != false || v["revision"] != "" {
		t.Fatalf("invalid file accepted: %v", v)
	}
	env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", `{"intervalHours":6,"autoApply":true}`, scheduleHeaders), 409, "ROUTES_SCHEDULE_UNCONFIRMED")
	after, _ := os.ReadFile(env.e.scheduleFile())
	if string(after) != string(bad) {
		t.Fatal("damaged file overwritten")
	}
}

func TestScheduleSettingsFenceAndConcurrentConflict(t *testing.T) {
	env, _ := schedEnv(t)
	rel, ok := env.d.Lock.TryAcquire("operations", "backup", "test")
	if !ok {
		t.Fatal("fence")
	}
	env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", `{"intervalHours":6,"autoApply":true}`, scheduleHeaders), 409, "ROUTES_OPERATION_BUSY")
	rel()
	rev := env.mustOK(env.get("/api/routes/catalog-schedule.cgi"))["revision"]
	// Two clients loaded the same snapshot; exactly the first change may commit.
	body := fmt.Sprintf(`{"maxRemovalPercent":40,"expectedRevision":%q}`, rev)
	env.mustOK(env.callH("POST", "/api/routes/catalog-schedule.cgi", body, scheduleHeaders))
	env.wantErr(env.callH("POST", "/api/routes/catalog-schedule.cgi", strings.Replace(body, "40", "60", 1), scheduleHeaders), 409, "ROUTES_SCHEDULE_CHANGED")
}

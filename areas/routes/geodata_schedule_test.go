package routes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

const geoScheduleURL = "/api/routes/geodata-schedule.cgi"

func geoScheduleView(env *testEnv) map[string]any { return env.mustOK(env.get(geoScheduleURL)) }
func geoToggle(env *testEnv, enabled bool) map[string]any {
	env.t.Helper()
	value := "false"
	if enabled {
		value = "true"
	}
	v := geoScheduleView(env)
	return env.mustOK(env.callH("POST", geoScheduleURL, `{"enabled":`+value+`,"expectedRevision":"`+v["revision"].(string)+`"}`, policyHeaders))
}

func TestGeodataScheduleDisableDuringFetchAndNoDuplicate(t *testing.T) {
	env, _ := geoAPIEnv(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	env.e.geoNow = func() time.Time { return now }
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	geoToggle(env, true)
	now = now.Add(24 * time.Hour)
	started, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(proceed) })
	env.e.geoResolve = func(ctx context.Context, _ geodata.Kind) (string, error) {
		close(started)
		select {
		case <-proceed:
			return strings.Repeat("a", 40), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	r := env.e.enqueueDueGeodata()
	if r == nil {
		t.Fatal("not scheduled")
	}
	select {
	case <-started:
	case <-time.After(15 * time.Second):
		t.Fatal("fetch not entered")
	}
	if other := env.e.enqueueDueGeodata(); other != nil {
		t.Fatal("duplicate queued")
	}
	geoToggle(env, false)
	once.Do(func() { close(proceed) })
	geoMustComplete(t, geoWait(env, r.RequestID))
	s := geoScheduleDisk(t, env)
	if s.Enabled || s.Attempts["geoip"].Status != "ok" {
		t.Fatalf("finish overwrote disabled setting: %+v", s)
	}
}

func TestGeodataScheduleUnconfirmedResultPreservesCommittedPolicy(t *testing.T) {
	env, _ := geoAPIEnv(t)
	env.d.Svc.XrayValidateConfig = func(context.Context, []byte) error {
		return os.WriteFile(filepath.Join(env.e.root, routepolicy.GeoScheduleFile), []byte(`null`), 0600)
	}
	r := geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`)
	if r.State != "failed" || r.ErrorCode != "GEODATA_SCHEDULE_UNCONFIRMED" {
		t.Fatalf("unconfirmed not visible: %+v", r)
	}
	if len(policyState(t, env).Rules) != 1 || len(geoLoad(t, env).Assets) != 1 {
		t.Fatal("committed working data rolled back")
	}
	if env.d.Lock.Busy() {
		t.Fatal("metadata failure left global fence")
	}
	pending, err := routepolicy.RecoveryPending(context.Background(), env.d.Root)
	if err != nil || pending {
		t.Fatalf("policy pending: %v %v", pending, err)
	}
}

func TestGeodataSchedulePendingAndCrashRetry(t *testing.T) {
	env, _ := geoAPIEnv(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	env.e.geoNow = func() time.Time { return now }
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	geoToggle(env, true)
	now = now.Add(24 * time.Hour)
	release, ok := env.d.Lock.TryAcquire("routes", "routes:policy", "policy")
	if !ok {
		t.Fatal("fence")
	}
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("busy scheduled")
	}
	release()
	a := routepolicy.GeoAttempt{OperationID: "crashed", Source: "SCHEDULER", StartedAt: now.Format(time.RFC3339Nano), RetryAt: now.Add(15 * time.Minute).Format(time.RFC3339Nano), Status: "running"}
	if _, err := routepolicy.WriteGeoAttempt(context.Background(), env.e.root, "geoip", "", a); err != nil {
		t.Fatal(err)
	}
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("crashed attempt retried early")
	}
	now = now.Add(15 * time.Minute)
	path := filepath.Join(env.e.root, routepolicy.JournalFile)
	if err := os.WriteFile(path, []byte(`pending`), 0600); err != nil {
		t.Fatal(err)
	}
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("pending policy scheduled")
	}
}
func geoScheduleDisk(t *testing.T, env *testEnv) routepolicy.GeoSchedule {
	t.Helper()
	s, err := routepolicy.ReadGeoSchedule(context.Background(), env.e.root)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGeodataScheduleAPIIndependentToggle(t *testing.T) {
	env := newEnv(t)
	env.d.Svc.XrayApplyConfig = func(context.Context, []byte) error { t.Fatal("toggle restarted core"); return nil }
	v := geoScheduleView(env)
	if v["enabled"] != false {
		t.Fatal("not default off")
	}
	rev := v["revision"].(string)
	release, ok := env.d.Lock.TryAcquire("routes", geodataAction, "policy")
	if !ok {
		t.Fatal("fence")
	}
	defer release()
	geoToggle(env, true)
	env.wantErr(env.callH("POST", geoScheduleURL, `{"enabled":false,"expectedRevision":"`+rev+`"}`, policyHeaders), 409, "GEODATA_SCHEDULE_CHANGED")
	geoToggle(env, false)
	for _, body := range []string{`{"enabled":null,"expectedRevision":"` + rev + `"}`, `{"enabled":true,"expectedRevision":"` + rev + `","extra":1}`} {
		env.wantErr(env.callH("POST", geoScheduleURL, body, policyHeaders), 400, "GEODATA_SCHEDULE_INVALID")
	}
	r := env.callH("POST", geoScheduleURL, `{"enabled":true,"expectedRevision":"`+rev+`"}`, nil)
	if r.status < 400 {
		t.Fatal("unguarded POST accepted")
	}
	if geoScheduleDisk(t, env).Enabled {
		t.Fatal("invalid request changed toggle")
	}
}

func TestGeodataScheduleChecksBothKindsAndRespectsPause(t *testing.T) {
	env, _ := geoAPIEnv(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	env.e.geoNow = func() time.Time { return now }
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("default off scheduled")
	}
	geoToggle(env, true)
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("no selection scheduled")
	}
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "yt", `,"name":"YouTube","kind":"geosite","tags":["youtube"]`))
	now = now.Add(24*time.Hour - time.Second)
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("scheduled before24h")
	}
	now = now.Add(time.Second)
	if err := env.d.Queue.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("paused scheduled")
	}
	if err := env.d.Queue.SetPaused(false); err != nil {
		t.Fatal(err)
	}
	r := env.e.enqueueDueGeodata()
	if r == nil {
		t.Fatal("due selected OFF data not scheduled")
	}
	geoMustComplete(t, geoWait(env, r.RequestID))
	s := geoScheduleDisk(t, env)
	for _, kind := range []string{"geoip", "geosite"} {
		if s.Attempts[kind].Source != "SCHEDULER" || s.Attempts[kind].Status != "ok" || s.Attempts[kind].OperationID != s.Attempts["geoip"].OperationID {
			t.Fatalf("missing durable batch result: %+v", s)
		}
	}
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("successful check immediately repeated")
	}
	if policyState(t, env).Rules[0].Enabled {
		t.Fatal("scheduled update enabled rule")
	}
}

func TestGeodataScheduleCheckLeavesChangedBatchPending(t *testing.T) {
	env, _ := geoAPIEnv(t)
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	env.e.geoNow = func() time.Time { return now }
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	before := geoLoad(t, env)
	env.d.Svc.XrayApplyConfig = func(context.Context, []byte) error {
		t.Fatal("scheduled check applied VPN configuration")
		return nil
	}
	geoToggle(env, true)
	now = now.Add(24 * time.Hour)
	env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) { return strings.Repeat("b", 40), nil }
	env.e.geoFetch = func(context.Context, geodata.Source) ([]byte, error) {
		return []byte{0x0a, 0x0e, 0x0a, 0x02, 'F', 'I', 0x12, 0x08, 0x0a, 0x04, 192, 0, 2, 0, 0x10, 24}, nil
	}
	r := env.e.enqueueDueGeodata()
	if r == nil {
		t.Fatal("due check absent")
	}
	geoMustComplete(t, geoWait(env, r.RequestID))
	if geoLoad(t, env).Revision != before.Revision || policyView(env)["geoPending"] == nil {
		t.Fatal("schedule changed active files instead of publishing a ready batch")
	}
	if more := env.e.enqueueDueGeodata(); more != nil {
		t.Fatal("scheduler repeated a just-completed pending check")
	}
}

func TestGeodataScheduleFailureBackoffSurvivesReload(t *testing.T) {
	env, _ := geoAPIEnv(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	env.e.geoNow = func() time.Time { return now }
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	before := policyState(t, env)
	geoToggle(env, true)
	now = now.Add(24 * time.Hour)
	env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) { return "", errors.New("network unavailable") }
	r := env.e.enqueueDueGeodata()
	if r == nil {
		t.Fatal("due job absent")
	}
	if geoWait(env, r.RequestID).State != "failed" {
		t.Fatal("network failure lost")
	}
	s := geoScheduleDisk(t, env)
	if s.Attempts["geoip"].Status != "failed" || s.Attempts["geoip"].RetryAt != "2026-11-18T00:15:00Z" {
		t.Fatalf("retry missing: %+v", s)
	}
	if policyState(t, env).GeoUpdatedAt["geoip"] != before.GeoUpdatedAt["geoip"] {
		t.Fatal("failed check moved success date")
	}
	reloaded := newEngine(env.d)
	reloaded.geoNow = env.e.geoNow
	now = now.Add(15*time.Minute - time.Second)
	if r := reloaded.enqueueDueGeodata(); r != nil {
		t.Fatal("retry lost after reload")
	}
	now = now.Add(time.Second)
	r = reloaded.enqueueDueGeodata()
	if r == nil {
		t.Fatal("retry did not become due")
	}
	geoWait(env, r.RequestID)
}

func TestGeodataScheduleRechecksAfterFence(t *testing.T) {
	for _, mode := range []string{"disable", "pause"} {
		t.Run(mode, func(t *testing.T) {
			env, _ := geoAPIEnv(t)
			geoToggle(env, true)
			geoMustComplete(t, geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
			rev, err := routepolicy.StateRevision(policyState(t, env))
			if err != nil {
				t.Fatal(err)
			}
			m, err := routepolicy.DecodeMutation([]byte(`{"expectedRevision":"` + rev + `","operation":"geo-update","id":"geoip","kind":"geoip"}`))
			if err != nil {
				t.Fatal(err)
			}
			release, ok := env.d.Lock.TryAcquire("routes", "routes:policy", "policy")
			if !ok {
				t.Fatal("fence")
			}
			env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) {
				t.Error("automatic fetch after stop")
				return strings.Repeat("a", 40), nil
			}
			op := env.d.Queue.StartOperation("", "route_policy", geodataAction, "SCHEDULER", "protected", "")
			done := make(chan error, 1)
			go func() { _, err := env.e.runPolicy(context.Background(), m, op); op.Finish(err); done <- err }()
			if mode == "disable" {
				geoToggle(env, false)
			} else {
				if err := env.d.Queue.SetPaused(true); err != nil {
					t.Fatal(err)
				}
			}
			release()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("disabled queued job ran")
				}
			case <-time.After(15 * time.Second):
				t.Fatal("waiting job stuck")
			}
		})
	}
}

func TestGeodataScheduleManualWhileOffAndPausedAndUnsafeStatus(t *testing.T) {
	env, _ := geoAPIEnv(t)
	if err := env.d.Queue.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	s := geoScheduleDisk(t, env)
	if s.Enabled || s.Attempts["geoip"].Status != "ok" || s.Attempts["geoip"].Source != "USER" {
		t.Fatalf("manual outcome: %+v", s)
	}
	if err := os.WriteFile(filepath.Join(env.e.root, routepolicy.GeoScheduleFile), []byte(`null`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := env.d.Queue.SetPaused(false); err != nil {
		t.Fatal(err)
	}
	if r := env.e.enqueueDueGeodata(); r != nil {
		t.Fatal("invalid status started job")
	}
}

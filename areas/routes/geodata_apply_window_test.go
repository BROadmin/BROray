package routes

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

func TestGeodataScheduleApplyOnlyAtConfirmedUTCMinute(t *testing.T) {
	for _, mode := range []string{"at minute", "missed minute", "settings changed", "expired after validation", "failure not replayed"} {
		t.Run(mode, func(t *testing.T) {
			env, _ := geoAPIEnv(t)
			at := time.Date(2026, 11, 24, 9, 45, 0, 0, time.UTC)
			now := at.Add(-time.Minute)
			env.e.geoNow = func() time.Time { return now }
			path := env.d.Root.XrayConfig()
			if err := os.WriteFile(path, []byte(policyTestBase), 0600); err != nil {
				t.Fatal(err)
			}
			env.d.Svc.ServersPreviewActiveConfig = func(ctx context.Context) ([]byte, error) {
				return env.e.configureRouting(ctx, []byte(policyTestBase))
			}
			applies := 0
			env.d.Svc.XrayApplyConfig = func(_ context.Context, cfg []byte) error {
				applies++
				return platform.WriteFileAtomicNoFollow(path, cfg, 0600)
			}
			geoMustComplete(t, geoSubmit(env, "geo-upsert", "own", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
			env.mustOK(policyPost(env, policyView(env)["revision"].(string), "enabled", `,"enabled":true`))
			before := geoLoad(t, env)
			beforeApplies := applies
			setupPrepared := func(revision string, address byte) {
				t.Helper()
				env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) { return strings.Repeat(revision, 40), nil }
				env.e.geoFetch = func(context.Context, geodata.Source) ([]byte, error) {
					return []byte{0x0a, 0x0e, 0x0a, 0x02, 'F', 'I', 0x12, 0x08, 0x0a, 0x04, 192, 0, address, 0, 0x10, 24}, nil
				}
				geoMustComplete(t, geoSubmit(env, "geo-check", "geodata", ""))
			}
			setupPrepared("b", 2)
			config := func(value string) map[string]any {
				t.Helper()
				current := geoScheduleView(env)
				rev, _ := current["applyRevision"].(string)
				return env.mustOK(env.callH("POST", geoScheduleURL, `{"applyAtUTC":"`+value+`","expectedRevision":"`+rev+`"}`, policyHeaders))
			}
			schedule := config("09:45")
			if schedule["enabled"] != false || schedule["applyAtUTC"] != "09:45" || applies != beforeApplies || env.e.enqueueDueGeodata() != nil {
				t.Fatal("time was not independent of daily checking or applied early")
			}
			now = at
			if mode == "missed minute" {
				now = at.Add(time.Minute)
				if env.e.enqueueDueGeodata() != nil || applies != beforeApplies || geoLoad(t, env).Revision != before.Revision {
					t.Fatal("automatic apply caught up outside the chosen minute")
				}
				return
			}
			if mode == "settings changed" {
				view := policyView(env)
				body := `{"operation":"geo-apply","id":"geodata","expectedRevision":"` + view["revision"].(string) +
					`","expectedPendingRevision":"` + view["geoPending"].(map[string]any)["revision"].(string) +
					`","expectedScheduleRevision":"` + schedule["applyRevision"].(string) + `","applyDayUTC":"2026-11-24"}`
				env.wantErr(env.callH("POST", "/api/routes/policy.cgi", body, policyHeaders), 400, "POLICY_REQUEST_INVALID")
				m, err := routepolicy.DecodeMutation([]byte(body))
				if err != nil {
					t.Fatal(err)
				}
				config("")
				config("09:45")
				op := env.d.Queue.StartOperation("", "route_policy", geodataAction, "SCHEDULER", "protected", "")
				_, err = env.e.runPolicy(context.Background(), m, op)
				op.Finish(err)
				if err == nil || applies != beforeApplies {
					t.Fatal("superseded settings gave old job authority")
				}
				return
			}
			if mode == "expired after validation" {
				env.d.Svc.XrayValidateConfig = func(context.Context, []byte) error {
					now = at.Add(time.Minute)
					return nil
				}
			}
			if mode == "failure not replayed" {
				env.d.Svc.XrayApplyConfig = func(_ context.Context, cfg []byte) error {
					applies++
					if err := platform.WriteFileAtomicNoFollow(path, cfg, 0600); err != nil {
						return err
					}
					if applies == beforeApplies+1 {
						return errors.New("apply failed")
					}
					return nil
				}
			}
			r := env.e.enqueueDueGeodata()
			if r == nil {
				t.Fatal("prepared batch was not scheduled")
			}
			result := geoWait(env, r.RequestID)
			if mode == "expired after validation" {
				if result.State != "failed" || applies != beforeApplies || geoLoad(t, env).Revision != before.Revision {
					t.Fatal("expired authority reached the live apply")
				}
				return
			}
			if mode == "failure not replayed" {
				if result.State != "failed" || applies != beforeApplies+2 || geoLoad(t, env).Revision != before.Revision || policyView(env)["geoPending"] == nil {
					t.Fatal("failed apply did not preserve working generation and pending receipt")
				}
			} else {
				geoMustComplete(t, result)
				if applies != beforeApplies+1 || geoLoad(t, env).Revision == before.Revision || policyView(env)["geoPending"] != nil {
					t.Fatal("scheduled batch did not apply once")
				}
				setupPrepared("c", 3)
			}
			reopened := newEngine(env.d)
			reopened.geoNow = env.e.geoNow
			if reopened.enqueueDueGeodata() != nil {
				t.Fatal("durable daily claim replayed after engine recreation")
			}
			now = at.Add(24 * time.Hour)
			if r := env.e.enqueueDueGeodata(); r == nil {
				t.Fatal("next configured day incorrectly excluded")
			} else {
				geoMustComplete(t, geoWait(env, r.RequestID))
			}
		})
	}
}

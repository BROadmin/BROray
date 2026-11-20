package routes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

func geoAPIEnv(t *testing.T) (*testEnv, *[]geodata.Kind) {
	env := newEnv(t)
	env.e.defineGeodataQueue()
	env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) { return strings.Repeat("a", 40), nil }
	calls := []geodata.Kind{}
	env.e.geoFetch = func(_ context.Context, source geodata.Source) ([]byte, error) {
		calls = append(calls, source.Kind)
		name := "dlc.dat"
		if source.Kind == geodata.GeoIP {
			name = "fi.dat"
		}
		return os.ReadFile(filepath.Join(filepath.Dir(os.Getenv("BRORAY_XRAY_BIN")), name))
	}
	env.d.Svc.XrayValidateConfig = func(ctx context.Context, cfg []byte) error {
		if !compat.FenceHeld(ctx) {
			t.Fatal("validation outside fence")
		}
		return nil
	}
	env.d.Svc.ServersPreviewActiveConfig = func(context.Context) ([]byte, error) { return nil, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); env.d.Queue.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("queue did not stop")
		}
	})
	return env, &calls
}

func geoSubmit(env *testEnv, operation, id, extra string) *compat.Request {
	env.t.Helper()
	revision := policyView(env)["revision"].(string)
	r := env.callH("POST", "/api/routes/policy.cgi", `{"expectedRevision":"`+revision+`","operation":"`+operation+`","id":"`+id+`"`+extra+`}`, policyHeaders)
	v := env.mustOK(r)
	requestID, ok := v["requestId"].(string)
	if !ok {
		env.t.Fatalf("not queued: %v", v)
	}
	return geoWait(env, requestID)
}
func geoWait(env *testEnv, id string) *compat.Request {
	env.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r, ok := env.d.Queue.Request(id)
		if !ok {
			env.t.Fatal("queue request missing")
		}
		if r.State == "completed" || r.State == "failed" || r.State == "cancelled" {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	env.t.Fatal("geodata operation timed out")
	return nil
}
func geoMustComplete(t *testing.T, r *compat.Request) {
	t.Helper()
	if r.State != "completed" {
		t.Fatalf("geodata failed: %s %s", r.ErrorCode, r.Message)
	}
}
func geoLoad(t *testing.T, env *testEnv) geodata.Snapshot {
	t.Helper()
	store, err := geodata.Open(filepath.Join(env.d.Root.Runtime(), geodata.AssetDirectory))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGeodataAPISelectionPersistsWhileOffAndUpdatesPerKind(t *testing.T) {
	env, calls := geoAPIEnv(t)
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "own", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	s := policyState(t, env)
	snapshot := geoLoad(t, env)
	if s.Rules[0].Enabled || s.Rules[0].Action != routepolicy.Deny || s.GeoRevision != snapshot.Revision || len(snapshot.Selection.CountryCodes) != 1 {
		t.Fatal("disabled selected country not persisted")
	}
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "youtube", `,"name":"YouTube","kind":"geosite","tags":["youtube"]`))
	if len(*calls) != 2 || (*calls)[0] != geodata.GeoIP || (*calls)[1] != geodata.GeoSite {
		t.Fatalf("unrelated data redownloaded: %v", *calls)
	}
	before := policyState(t, env)
	env.e.geoResolve = func(_ context.Context, kind geodata.Kind) (string, error) {
		if kind != geodata.GeoIP {
			t.Fatal("unrequested source resolved")
		}
		return strings.Repeat("b", 40), nil
	}
	geoMustComplete(t, geoSubmit(env, "geo-update", "geoip", `,"kind":"geoip"`))
	after := policyState(t, env)
	snapshot = geoLoad(t, env)
	if len(*calls) != 3 || (*calls)[2] != geodata.GeoIP || len(after.Rules) != len(before.Rules) || after.Rules[0].Enabled != before.Rules[0].Enabled {
		t.Fatal("update changed choices or downloaded wrong kind")
	}
	for _, asset := range snapshot.Assets {
		if asset.Kind == geodata.GeoSite && asset.Source.Revision != strings.Repeat("a", 40) {
			t.Fatal("GeoIP update changed GeoSite revision")
		}
	}
	v := policyView(env)
	state := v["state"].(map[string]any)
	dates, ok := state["geoUpdatedAt"].(map[string]any)
	if !ok || dates["geoip"] == nil || dates["geosite"] == nil {
		t.Fatal("successful update dates missing")
	}
}

func TestGeodataAPIFailuresPreserveWorkingState(t *testing.T) {
	for _, mode := range []string{"network", "core", "missing-core", "missing-preview", "apply", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			env, _ := geoAPIEnv(t)
			if err := os.WriteFile(env.d.Root.XrayConfig(), []byte(policyTestBase), 0600); err != nil {
				t.Fatal(err)
			}
			env.d.Svc.ServersPreviewActiveConfig = func(ctx context.Context) ([]byte, error) { return env.e.configureRouting(ctx, []byte(policyTestBase)) }
			env.d.Svc.XrayApplyConfig = func(ctx context.Context, cfg []byte) error {
				return platform.WriteFileAtomicNoFollow(env.d.Root.XrayConfig(), cfg, 0600)
			}
			geoMustComplete(t, geoSubmit(env, "geo-upsert", "own", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
			env.mustOK(policyPost(env, policyView(env)["revision"].(string), "enabled", `,"enabled":true`))
			old := geoLoad(t, env)
			stateRev := policyView(env)["revision"]
			oldConfig, err := os.ReadFile(env.d.Root.XrayConfig())
			if err != nil {
				t.Fatal(err)
			}
			env.e.geoResolve = func(context.Context, geodata.Kind) (string, error) { return strings.Repeat("b", 40), nil }
			if mode == "network" {
				env.e.geoFetch = func(context.Context, geodata.Source) ([]byte, error) { return nil, errors.New("offline") }
			}
			if mode == "core" {
				env.d.Svc.XrayValidateConfig = func(context.Context, []byte) error { return errors.New("core rejected") }
			}
			if mode == "missing-core" {
				env.d.Svc.XrayValidateConfig = nil
			}
			if mode == "missing-preview" {
				env.d.Svc.ServersPreviewActiveConfig = nil
			}
			if mode == "apply" || mode == "unknown" {
				calls := 0
				env.d.Svc.XrayApplyConfig = func(ctx context.Context, cfg []byte) error {
					calls++
					if calls > 1 && mode == "unknown" {
						return errors.New("rollback unknown")
					}
					if err := platform.WriteFileAtomicNoFollow(env.d.Root.XrayConfig(), cfg, 0600); err != nil {
						return err
					}
					if calls == 1 {
						return errors.New("apply failed")
					}
					return nil
				}
			}
			r := geoSubmit(env, "geo-update", "geoip", `,"kind":"geoip"`)
			if r.State != "failed" {
				t.Fatal("failed update reported success")
			}
			if geoLoad(t, env).Revision != old.Revision || policyView(env)["revision"] != stateRev {
				t.Fatal("failed update lost working state")
			}
			if mode == "unknown" {
				if !env.d.Lock.Busy() || policyView(env)["pending"] != true {
					t.Fatal("unknown result not fenced")
				}
			} else {
				cfg, _ := os.ReadFile(env.d.Root.XrayConfig())
				if string(cfg) != string(oldConfig) || env.d.Lock.Busy() {
					t.Fatal("confirmed rollback failed")
				}
			}
		})
	}
}

func TestGeodataAPIOfficialCoreValidatesDisabledSelection(t *testing.T) {
	env, _ := geoAPIEnv(t)
	calls := 0
	env.d.Svc.XrayValidateConfig = func(ctx context.Context, cfg []byte) error {
		calls++
		path := filepath.Join(t.TempDir(), "candidate.json")
		if err := os.WriteFile(path, cfg, 0600); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, os.Getenv("BRORAY_XRAY_BIN"), "run", "-test", "-c", path)
		cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+env.d.Root.Runtime())
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errors.New(string(out))
		}
		return nil
	}
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "own", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "ads", `,"name":"Ads","kind":"geosite","tags":["category-ads","category-telemetry"]`))
	if calls != 2 || policyState(t, env).Rules[0].Enabled {
		t.Fatal("disabled selection skipped core validation")
	}
}

func TestGeodataAPIUnchangedRevisionKeepsActiveGeneration(t *testing.T) {
	env, calls := geoAPIEnv(t)
	if err := os.WriteFile(env.d.Root.XrayConfig(), []byte(policyTestBase), 0600); err != nil {
		t.Fatal(err)
	}
	env.d.Svc.ServersPreviewActiveConfig = func(ctx context.Context) ([]byte, error) { return env.e.configureRouting(ctx, []byte(policyTestBase)) }
	applies := 0
	env.d.Svc.XrayApplyConfig = func(ctx context.Context, cfg []byte) error {
		applies++
		return platform.WriteFileAtomicNoFollow(env.d.Root.XrayConfig(), cfg, 0600)
	}
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "own", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	env.mustOK(policyPost(env, policyView(env)["revision"].(string), "enabled", `,"enabled":true`))
	before := geoLoad(t, env)
	beforeApply := applies
	checks := 0
	env.d.Svc.XrayValidateConfig = func(context.Context, []byte) error { checks++; return nil }
	geoMustComplete(t, geoSubmit(env, "geo-update", "geoip", `,"kind":"geoip"`))
	after := geoLoad(t, env)
	if after.Revision != before.Revision || applies != beforeApply || len(*calls) != 1 || checks != 1 {
		t.Fatalf("unchanged update caused mutation: generationChanged=%v applies=%d->%d fetches=%d checks=%d", after.Revision != before.Revision, beforeApply, applies, len(*calls), checks)
	}
}

func TestGeodataAPIQueueRechecksRevisionAndCoalescesNonce(t *testing.T) {
	for _, stale := range []bool{false, true} {
		env, calls := geoAPIEnv(t)
		revision := policyView(env)["revision"].(string)
		release, ok := env.d.Lock.TryAcquire("operations", "test:hold", "")
		if !ok {
			t.Fatal("test fence unavailable")
		}
		defer release()
		headers := map[string]string{}
		for k, v := range policyHeaders {
			headers[k] = v
		}
		headers["X-BROray-Nonce"] = strings.Repeat("c", 32)
		body := `{"expectedRevision":"` + revision + `","operation":"geo-upsert","id":"own","name":"Finland","kind":"geoip","tags":["fi"]}`
		first := env.mustOK(env.callH("POST", "/api/routes/policy.cgi", body, headers))
		second := env.mustOK(env.callH("POST", "/api/routes/policy.cgi", body, headers))
		if first["requestId"] != second["requestId"] || policyView(env)["busy"] != true {
			t.Fatal("queue retry or busy readback incorrect")
		}
		if stale {
			writeTestPolicy(t, env.e, `{"schemaVersion":1,"rules":[{"id":"newer","enabled":false,"action":"deny","domains":["newer.example"]}]}`)
		}
		release()
		r := geoWait(env, first["requestId"].(string))
		if stale {
			if r.State != "failed" || r.ErrorCode != "POLICY_REVISION_CHANGED" || len(*calls) != 0 {
				t.Fatalf("stale queued mutation executed: %s %s fetches%d", r.State, r.ErrorCode, len(*calls))
			}
		} else {
			geoMustComplete(t, r)
			if len(*calls) != 1 {
				t.Fatal("nonce retry fetched twice")
			}
		}
	}
}

func TestGeodataAPIRefusesForeignManagedRoot(t *testing.T) {
	env, _ := geoAPIEnv(t)
	foreign := t.TempDir()
	if err := os.Symlink(foreign, filepath.Join(env.d.Root.Runtime(), geodata.AssetDirectory)); err != nil {
		t.Fatal(err)
	}
	revision := policyView(env)["revision"]
	r := geoSubmit(env, "geo-upsert", "own", `,"name":"Finland","kind":"geoip","tags":["fi"]`)
	if r.State != "failed" {
		t.Fatal("foreign managed root accepted")
	}
	entries, err := os.ReadDir(foreign)
	if err != nil || len(entries) != 0 {
		t.Fatal("foreign directory modified")
	}
	if policyView(env)["revision"] != revision || env.d.Lock.Busy() {
		t.Fatal("failed preparation changed state or retained fence")
	}
}

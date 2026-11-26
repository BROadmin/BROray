package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

var policyHeaders = map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com", "Content-Type": "application/json"}

func policyPost(env *testEnv, revision, operation, extra string) resp {
	env.t.Helper()
	return env.callH("POST", "/api/routes/policy.cgi", `{"expectedRevision":"`+revision+`","operation":"`+operation+`","id":"own"`+extra+`}`, policyHeaders)
}
func policyView(env *testEnv) map[string]any { return env.mustOK(env.get("/api/routes/policy.cgi")) }
func policyState(t *testing.T, env *testEnv) routepolicy.State {
	t.Helper()
	state, err := routepolicy.ReadState(context.Background(), env.e.root)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestPolicyAPICRUDIndependentSettingsAndCAS(t *testing.T) {
	env := newEnv(t)
	env.d.Svc.ServersPreviewActiveConfig = func(context.Context) ([]byte, error) { return nil, nil }
	// Deactivation can retain the last config. Saving rules must not start VPN.
	if err := os.WriteFile(env.d.Root.XrayConfig(), []byte(policyTestBase), 0600); err != nil {
		t.Fatal(err)
	}
	env.d.Svc.XrayApplyConfig = func(context.Context, []byte) error { t.Fatal("inactive save started VPN"); return nil }
	v := policyView(env)
	revision := v["revision"].(string)
	if v["busy"] != false || v["pending"] != false {
		t.Fatal(v)
	}
	v = env.mustOK(policyPost(env, revision, "upsert", `,"name":"Own","domains":"Example.COM\nfull:one.example"`))
	s := policyState(t, env)
	if len(s.Rules) != 1 || s.Rules[0].Enabled || s.Rules[0].Action != routepolicy.Deny || len(s.Rules[0].Domains) != 2 {
		t.Fatalf("new default: %+v", s)
	}
	env.wantErr(policyPost(env, revision, "enabled", `,"enabled":true`), 409, "POLICY_REVISION_CHANGED")
	v = env.mustOK(policyPost(env, v["revision"].(string), "action", `,"action":"direct"`))
	v = env.mustOK(policyPost(env, v["revision"].(string), "enabled", `,"enabled":true`))
	v = env.mustOK(policyPost(env, v["revision"].(string), "upsert", `,"name":"Edited","domains":"new.example"`))
	s = policyState(t, env)
	if !s.Rules[0].Enabled || s.Rules[0].Action != routepolicy.Direct || s.Rules[0].Name != "Edited" {
		t.Fatalf("edit reset independent controls: %+v", s)
	}
	v = env.mustOK(policyPost(env, v["revision"].(string), "delete", ""))
	if len(policyState(t, env).Rules) != 0 || env.d.Lock.Busy() {
		t.Fatal("delete or fence failed")
	}
	got, err := os.ReadFile(env.d.Root.XrayConfig())
	if err != nil || string(got) != policyTestBase {
		t.Fatal("inactive configuration changed")
	}
}

func TestPolicyAPIStrictRequestAndGuards(t *testing.T) {
	env := newEnv(t)
	v := policyView(env)
	rev := v["revision"].(string)
	base := `{"expectedRevision":"` + rev + `","operation":"enabled","id":"own"`
	for _, raw := range []string{base + `}`, base + `,"enabled":null}`, base + `,"enabled":true,"enabled":false}`, base + `,"enabled":true,"Enabled":false}`, base + `,"enabled":true,"name":"dirty neighbor"}`, `null`, `{} `, base + `,"enabled":true} {}`} {
		env.wantErr(env.callH("POST", "/api/routes/policy.cgi", raw, policyHeaders), 400, "POLICY_REQUEST_INVALID")
	}
	valid := base + `,"enabled":true}`
	env.wantErr(env.callH("POST", "/api/routes/policy.cgi", valid, nil), 403, "CSRF_REJECTED")
	env.wantErr(env.callH("POST", "/api/routes/policy.cgi", valid, map[string]string{"X-BROray-Request": "operations", "Origin": "http://evil.example"}), 403, "ORIGIN_REJECTED")
	if policyView(env)["revision"] != rev || env.d.Lock.Busy() {
		t.Fatal("rejected input changed state")
	}
}

func TestPolicyAPIApplyRollbackAndUnknownOutcome(t *testing.T) {
	for _, mode := range []string{"success", "rollback", "unknown", "preview-failure", "foreign-config"} {
		t.Run(mode, func(t *testing.T) {
			env := newEnv(t)
			writeTestPolicy(t, env.e, policyTestDomain)
			if err := os.WriteFile(env.d.Root.XrayConfig(), []byte(policyTestBase), 0600); err != nil {
				t.Fatal(err)
			}
			env.d.Svc.ServersPreviewActiveConfig = func(ctx context.Context) ([]byte, error) {
				if !compat.FenceHeld(ctx) {
					t.Fatal("preview outside fence")
				}
				if mode == "preview-failure" {
					return nil, errors.New("core rejected")
				}
				if mode == "foreign-config" {
					if err := os.WriteFile(env.d.Root.XrayConfig(), []byte(`{"foreign":true}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return env.e.configureRouting(ctx, []byte(policyTestBase))
			}
			calls := 0
			env.d.Svc.XrayApplyConfig = func(ctx context.Context, cfg []byte) error {
				calls++
				if !compat.FenceHeld(ctx) {
					t.Fatal("apply outside fence")
				}
				if mode == "unknown" && calls > 1 {
					return errors.New("restore unconfirmed")
				}
				if err := platform.WriteFileAtomicNoFollow(env.d.Root.XrayConfig(), cfg, 0600); err != nil {
					return err
				}
				if (mode == "rollback" || mode == "unknown") && calls == 1 {
					return errors.New("apply unconfirmed")
				}
				return nil
			}
			rev := policyView(env)["revision"].(string)
			r := policyPost(env, rev, "action", `,"action":"direct"`)
			if mode == "rollback" || mode == "unknown" {
				rows, _ := env.d.Queue.Journal().ReportEvents()["failures"].([]map[string]any)
				expected := "unconfirmed"
				if mode == "rollback" {
					expected = "restored"
				}
				if len(rows) != 1 || rows[0]["action"] != "routes:policy" ||
					rows[0]["configurationRollback"] != expected || rows[0]["vpnRecovery"] != "unconfirmed" {
					t.Fatal("rollback outcome should distinguish config and traffic evidence", rows)
				}
			}
			if mode == "success" {
				env.mustOK(r)
				cfg, _ := os.ReadFile(env.d.Root.XrayConfig())
				if !bytes.Contains(cfg, []byte("broray-direct")) || bytes.Contains(cfg, []byte("broray-deny")) {
					t.Fatalf("wrong prospective policy: %s", cfg)
				}
				if policyState(t, env).Rules[0].Action != routepolicy.Direct || calls != 1 {
					t.Fatal("commit failed")
				}
			} else {
				if r.status < 400 {
					t.Fatalf("failure reported as success: %s", r.raw)
				}
				if policyState(t, env).Rules[0].Action != routepolicy.Deny {
					t.Fatal("failed save installed state")
				}
			}
			v := policyView(env)
			if mode == "unknown" {
				if !env.d.Lock.Busy() || v["pending"] != true {
					t.Fatal("uncertain result released fence")
				}
				env.wantErr(policyPost(env, rev, "delete", ""), 409, "POLICY_BUSY")
			} else if env.d.Lock.Busy() || v["pending"] != false {
				t.Fatal("confirmed operation retained fence")
			}
			if mode == "rollback" {
				cfg, _ := os.ReadFile(env.d.Root.XrayConfig())
				if string(cfg) != policyTestBase || calls != 2 {
					t.Fatal("exact previous config not restored")
				}
			}
			if (mode == "preview-failure" || mode == "foreign-config") && calls != 0 {
				t.Fatal("applied unvalidated/stale preview")
			}
		})
	}
}

func TestPolicyAPIRejectsUnavailableServiceAndInvalidDomains(t *testing.T) {
	env := newEnv(t)
	rev := policyView(env)["revision"].(string)
	for _, domains := range []string{"", "https://example.com", "1.2.3.4", strings.Repeat("a", (2<<20)+1)} {
		b, _ := json.Marshal(domains)
		r := policyPost(env, rev, "upsert", `,"name":"Own","domains":`+string(b))
		if r.status < 400 {
			t.Fatal("invalid domain accepted")
		}
	}
	r := policyPost(env, rev, "upsert", `,"name":"Own","domains":"example.com"`)
	if r.status < 400 {
		t.Fatal("missing service treated as inactive")
	}
	if len(policyState(t, env).Rules) != 0 || env.d.Lock.Busy() {
		t.Fatal("failed save changed state")
	}
}

func TestPolicyAPIExistingJournalRetainsFence(t *testing.T) {
	env := newEnv(t)
	rev := policyView(env)["revision"].(string)
	j, err := routepolicy.OpenJournal(env.d.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.Begin(context.Background(), routepolicy.State{SchemaVersion: 1, Rules: []routepolicy.Rule{}}, nil, ""); err != nil {
		t.Fatal(err)
	}
	r := policyPost(env, rev, "upsert", `,"name":"Own","domains":"example.com"`)
	env.wantErr(r, 503, "POLICY_RECOVERY_REQUIRED")
	if !env.d.Lock.Busy() {
		t.Fatal("pending policy released mutation fence")
	}
	if policyView(env)["pending"] != true {
		t.Fatal("journal missing from readback")
	}
}

func TestPolicyAPIRequestDisconnectAndBusyReadback(t *testing.T) {
	env := newEnv(t)
	rev := policyView(env)["revision"].(string)
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env.d.Svc.ServersPreviewActiveConfig = func(ctx context.Context) ([]byte, error) {
		cancel()
		if ctx.Err() != nil {
			t.Fatal("HTTP disconnect abandoned transaction")
		}
		if policyView(env)["busy"] != true {
			t.Fatal("preview is not visible as busy")
		}
		env.wantErr(policyPost(env, rev, "delete", ""), 409, "POLICY_BUSY")
		return nil, nil
	}
	req := httptest.NewRequest("POST", "/api/routes/policy.cgi", strings.NewReader(`{"expectedRevision":"`+rev+`","operation":"upsert","id":"own","name":"Own","domains":"example.com"}`)).WithContext(requestCtx)
	for key, value := range policyHeaders {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	if !cgi.Serve(w, req, "/api/routes/policy.cgi", nil) || w.Code != 200 {
		t.Fatalf("disconnected request not committed: %d %s", w.Code, w.Body.String())
	}
	if len(policyState(t, env).Rules) != 1 || env.d.Lock.Busy() {
		t.Fatal("transaction did not finish")
	}
}

func TestPolicyPreparedContextIsImmutableAndFenced(t *testing.T) {
	env := newEnv(t)
	state := routepolicy.State{SchemaVersion: 1, Rules: []routepolicy.Rule{{ID: "own", Enabled: true, Action: routepolicy.Deny, Domains: []string{"before.example"}}}}
	unfenced, err := withPreparedPolicy(context.Background(), state, geodata.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.e.configureRouting(unfenced, []byte(policyTestBase)); err == nil {
		t.Fatal("unfenced preview accepted")
	}
	ctx, err := withPreparedPolicy(compat.WithFence(context.Background()), state, geodata.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	state.Rules[0].Domains[0] = "after.example"
	got, err := env.e.configureRouting(ctx, []byte(policyTestBase))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("before.example")) || bytes.Contains(got, []byte("after.example")) {
		t.Fatal("preview aliased mutable input")
	}
	got, err = env.e.configureRouting(context.Background(), []byte(policyTestBase))
	if err != nil || string(got) != policyTestBase {
		t.Fatal("preview leaked into saved policy")
	}
}

func TestPolicyAPIRequiresStoredGeoAndActivePredecessor(t *testing.T) {
	for _, mode := range []string{"disabled-geo", "active-missing-config"} {
		t.Run(mode, func(t *testing.T) {
			env := newEnv(t)
			if mode == "disabled-geo" {
				writeTestPolicy(t, env.e, `{"schemaVersion":1,"rules":[{"id":"geo","enabled":false,"action":"deny","countryCodes":["fi"]}]}`)
			}
			env.d.Svc.ServersPreviewActiveConfig = func(ctx context.Context) ([]byte, error) { return env.e.configureRouting(ctx, []byte(policyTestBase)) }
			env.d.Svc.XrayApplyConfig = func(context.Context, []byte) error { t.Fatal("unsafe candidate applied"); return nil }
			rev := policyView(env)["revision"].(string)
			r := policyPost(env, rev, "upsert", `,"name":"Own","domains":"example.com"`)
			if r.status < 400 {
				t.Fatal("missing prerequisites accepted")
			}
			if policyView(env)["revision"] != rev || env.d.Lock.Busy() {
				t.Fatal("failure changed policy")
			}
		})
	}
}

func TestPolicyAPIDisabledEditDoesNotRestartActiveVPN(t *testing.T) {
	env := newEnv(t)
	if err := os.WriteFile(env.d.Root.XrayConfig(), []byte(policyTestBase), 0600); err != nil {
		t.Fatal(err)
	}
	previews, applies := 0, 0
	env.d.Svc.ServersPreviewActiveConfig = func(ctx context.Context) ([]byte, error) {
		previews++
		return env.e.configureRouting(ctx, []byte(policyTestBase))
	}
	env.d.Svc.XrayApplyConfig = func(context.Context, []byte) error { applies++; return nil }
	rev := policyView(env)["revision"].(string)
	v := env.mustOK(policyPost(env, rev, "upsert", `,"name":"Own","domains":"example.com"`))
	v = env.mustOK(policyPost(env, v["revision"].(string), "action", `,"action":"vpn"`))
	if previews != 2 || applies != 0 {
		t.Fatalf("inactive rule changes restarted active VPN: previews=%d applies=%d", previews, applies)
	}
	s := policyState(t, env)
	if len(s.Rules) != 1 || s.Rules[0].Enabled || s.Rules[0].Action != routepolicy.VPN || env.d.Lock.Busy() {
		t.Fatal("state-only change not confirmed")
	}
}

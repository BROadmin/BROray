package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/modules/geodata"
)

func TestNativeDomainOwnPreviewUsesSavedMatchers(t *testing.T) {
	env := newEnv(t)
	env.d.Svc.ServersPreviewActiveConfig = func(context.Context) ([]byte, error) { return nil, nil }
	env.mustOK(policyPost(env, policyView(env)["revision"].(string), "upsert",
		`,"name":"Saved DNS list","domains":"Example.COM\nfull:one.example"`))
	result := env.callH("POST", "/api/routes/native-domain-preview.cgi", `{"id":"own"}`, policyHeaders)
	if result.status != http.StatusOK {
		t.Fatalf("own native preview failed: %s", result.raw)
	}
	var reply struct {
		Data nativeDomainPreview `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.raw), &reply); err != nil {
		t.Fatal(err)
	}
	p := reply.Data
	if len(p.Domains) != 1 || p.Domains[0] != "example.com" || len(p.Skipped) != 1 ||
		p.Skipped[0].Value != "one.example" || p.Skipped[0].Reason != "keenetic-includes-subdomains" {
		t.Fatalf("matcher scope changed: %+v", p)
	}
}

func TestNativeDomainApplyJournalAndRollback(t *testing.T) {
	source := os.Getenv("BRORAY_NATIVE_DOMAIN_TEST_SOURCE")
	if source == "" {
		if core := os.Getenv("BRORAY_XRAY_BIN"); core != "" {
			source = filepath.Join(filepath.Dir(core), "dlc.dat")
		} else {
			t.Skip("BRORAY_XRAY_BIN and adjacent pinned data fixtures required")
		}
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	env := newEnv(t)
	const id = "telegram"
	const index = "38dab1834f6f9435395776ea9d6b02a1"
	target := compat.OwnedInterfaceTarget{Name: env.e.managedInterface()}
	if !keenetic.ValidProxyName(target.Name) {
		t.Fatal("fixture has no managed interface", target)
	}
	env.d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, _ string) (compat.OwnedInterfaceTarget, error) {
		if !compat.FenceHeld(ctx) {
			t.Error("native interface proof outside fence")
		}
		return target, nil
	}
	trackedID := id
	env.e.geoFetch = func(_ context.Context, _ geodata.Source) ([]byte, error) { return data, nil }
	path := nativeDomainRecordPath(env.e.root, id)
	lists := map[string]any{}
	var routes []keenetic.DomainRouteBinding
	var reject bool
	rci := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requests []map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&requests); err != nil || len(requests) == 0 {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if requests[0]["show"] != nil {
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"show": map[string]any{"sc": map[string]any{"object-group": map[string]any{"fqdn": lists}}}},
				map[string]any{"show": map[string]any{"sc": map[string]any{"dns-proxy": map[string]any{"route": routes}}}},
			})
			return
		}
		if string(requests[0]["parse"]) == `"system configuration save"` {
			_, _ = w.Write([]byte(`[{"parse":{"status":[{"status":"message"}]}}]`))
			return
		}
		journal, err := readNativeDomainRecord(nativeDomainRecordPath(env.e.root, trackedID), trackedID)
		if err != nil || journal == nil || journal.Phase == "enabled" {
			t.Error("mutation reached router before durable intent", err)
			http.Error(w, "missing fixture journal", http.StatusBadRequest)
			return
		}
		if raw := requests[0]["parse"]; raw != nil {
			var command string
			_ = json.Unmarshal(raw, &command)
			switch command {
			case "dns-proxy route object-group " + journal.Group + " " + target.Name + " auto reject":
				routes = []keenetic.DomainRouteBinding{{Group: journal.Group, Interface: target.Name, Index: index, Auto: true, Reject: reject}}
			case "no dns-proxy route object-group " + journal.Group + " " + target.Name:
				routes = nil
			default:
				http.Error(w, "unknown fixture command", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`[{"parse":{"status":[{"status":"message"}]}}]`))
			return
		}
		replies := make([]any, len(requests))
		for i, request := range requests {
			replies[i] = map[string]any{}
			if raw := request["object-group"]; raw != nil {
				var op struct {
					FQDN json.RawMessage `json:"fqdn"`
				}
				_ = json.Unmarshal(raw, &op)
				var created map[string]any
				if json.Unmarshal(op.FQDN, &created) == nil {
					lists[journal.Group] = created[journal.Group]
				} else {
					delete(lists, journal.Group)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(replies)
	}))
	defer rci.Close()
	env.e.T = keenetic.NewRCI(rci.URL)
	const endpoint = "/api/routes/native-domain-apply.cgi"
	preview, err := env.e.nativePresetPreview(t.Context(), id, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	enable := `{"id":"telegram","action":"enable","sourceRevision":"` + preview.Revision + `","sourceSHA256":"` + preview.Hash + `"}`
	env.wantErr(env.callH("POST", endpoint, enable, nil), 403, "CSRF_REJECTED")
	if out := env.callH("POST", endpoint, enable, policyHeaders); out.status < 400 {
		t.Fatal("binding without exclusive routing was accepted", out.raw)
	}
	if len(lists) != 0 || len(routes) != 0 {
		t.Fatal("unconfirmed apply was not rolled back")
	}
	if record, err := readNativeDomainRecord(path, id); err != nil || record != nil {
		t.Fatal("verified rollback retained journal", record, err)
	}
	reject = true
	out := env.mustOK(env.callH("POST", endpoint, enable, policyHeaders))
	if out["phase"] != "enabled" || len(lists) != 1 || len(routes) != 1 {
		t.Fatal("list was not applied", out)
	}
	status := env.mustOK(env.callH("GET", endpoint, "", nil))
	if status["states"].(map[string]any)[id].(map[string]any)["phase"] != "enabled" {
		t.Fatal("router readback did not confirm enabled", status)
	}
	disabled := env.mustOK(env.callH("POST", endpoint, `{"id":"telegram","action":"disable"}`, policyHeaders))
	if disabled["phase"] != "disabled" || len(lists) != 0 || len(routes) != 0 {
		t.Fatal("owned route was not removed", disabled)
	}

	trackedID = "own"
	env.d.Svc.ServersPreviewActiveConfig = func(context.Context) ([]byte, error) { return nil, nil }
	view := env.mustOK(policyPost(env, policyView(env)["revision"].(string), "upsert",
		`,"name":"Личный список","domains":"example.com\nfull:one.example"`))
	ownPreview, _, err := env.e.nativeOwnPreview(t.Context(), trackedID)
	if err != nil {
		t.Fatal(err)
	}
	ownEnable := `{"id":"own","action":"enable","sourceRevision":"own","sourceSHA256":"` + ownPreview.Hash + `"}`
	env.wantErr(env.callH("POST", endpoint, strings.Replace(ownEnable, ownPreview.Hash, strings.Repeat("0", 64), 1), policyHeaders),
		409, "NATIVE_DOMAIN_PREVIEW_CHANGED")
	view = env.mustOK(policyPost(env, view["revision"].(string), "enabled", `,"enabled":true`))
	env.wantErr(env.callH("POST", endpoint, ownEnable, policyHeaders), 409, "NATIVE_DOMAIN_LEGACY_ACTIVE")
	view = env.mustOK(policyPost(env, view["revision"].(string), "enabled", `,"enabled":false`))
	env.mustOK(env.callH("POST", endpoint, ownEnable, policyHeaders))
	record, err := readNativeDomainRecord(nativeDomainRecordPath(env.e.root, trackedID), trackedID)
	if err != nil || record == nil || record.Description != "BROray - Личный список" || len(record.Preview.Domains) != 1 ||
		record.Preview.Domains[0] != "example.com" || len(record.Preview.Skipped) != 1 || len(lists) != 1 {
		t.Fatalf("unexpected own route: %+v, %v", record, err)
	}
	env.wantErr(policyPost(env, view["revision"].(string), "delete", ""), 409, "NATIVE_DOMAIN_IN_USE")
	applied := env.mustOK(env.callH("GET", endpoint, "", nil))
	if applied["states"].(map[string]any)["own"].(map[string]any)["phase"] != "enabled" {
		t.Fatal("own route was not confirmed by readback")
	}
	record.Phase, record.Binding = "creating", nil
	routes = nil
	if err := saveNativeDomainRecord(nativeDomainRecordPath(env.e.root, trackedID), record); err != nil {
		t.Fatal(err)
	}
	recovered := env.mustOK(env.callH("POST", endpoint, `{"id":"own","action":"recover"}`, policyHeaders))
	if recovered["phase"] != "enabled" || len(lists) != 1 || len(routes) != 1 || routes[0].Group != record.Group {
		t.Fatal("partial apply must complete the original owned list", recovered)
	}
	env.mustOK(env.callH("POST", endpoint, `{"id":"own","action":"disable"}`, policyHeaders))
	if len(lists) != 0 || len(routes) != 0 || policyState(t, env).Rules[0].Enabled {
		t.Fatal("own rollback modified the saved source or retained native state")
	}
	env.mustOK(policyPost(env, view["revision"].(string), "delete", ""))
}

package servers

import (
	"encoding/json"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestAutoSwitchEnabledIsolatedRevision(t *testing.T) {
	e := newEnv(t)
	base := map[string]any{"enabled": false, "failureThreshold": 7, "cooldownMinutes": 20,
		"minimumRating": "good", "selectionRule": "best-quality", "preferredServerId": nil,
		"qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 180, "futureSetting": "keep"}
	if err := compat.WriteJSON(autoSwitchFile(e.d), base); err != nil {
		t.Fatal(err)
	}
	status := call(t, "GET", "/api/servers/auto-switch-status.cgi", "", nil)
	rev, ok := get(status.Body, "data", "revision").(string)
	if !ok || rev == "" {
		t.Fatalf("missing revision: %s", status.Raw)
	}
	for _, enabled := range []bool{true, false} {
		body, _ := json.Marshal(map[string]any{"enabled": enabled, "expectedRevision": rev})
		r := call(t, "POST", "/api/servers/auto-switch-enabled.cgi", string(body), nil)
		if r.Code != 200 || get(r.Body, "data", "value") != enabled {
			t.Fatalf("save: %s", r.Raw)
		}
		cur, _ := readObjectFile(autoSwitchFile(e.d))
		for k, v := range base {
			if k != "enabled" && !jsonEq(cur[k], v) {
				t.Fatalf("changed %s: %v", k, cur[k])
			}
		}
		stale := call(t, "POST", "/api/servers/auto-switch-enabled.cgi", string(body), nil)
		if stale.Code != 409 || errCode(stale) != "SETTINGS_CHANGED" {
			t.Fatalf("stale: %s", stale.Raw)
		}
		rev = get(r.Body, "data", "revision").(string)
	}
	release, _ := e.d.Lock.TryAcquire("dot", "dot:apply", "")
	r := call(t, "POST", "/api/servers/auto-switch-enabled.cgi", `{"enabled":true,"expectedRevision":"x"}`, nil)
	release()
	if r.Code != 409 || errCode(r) != "OPERATION_BUSY" {
		t.Fatalf("busy: %s", r.Raw)
	}
}

func TestAutoSwitchEnabledRejectsBadInput(t *testing.T) {
	newEnv(t)
	for _, body := range []string{`{}`, `{"enabled":"true","expectedRevision":"x"}`, `{"enabled":true}`, `{"enabled":true,"expectedRevision":"x","failureThreshold":7}`, `{"enabled":true,"expectedRevision":"x"} {}`} {
		r := call(t, "POST", "/api/servers/auto-switch-enabled.cgi", body, nil)
		if r.Code != 400 {
			t.Errorf("%s: %s", body, r.Raw)
		}
	}
}

func TestQualityEnabledPreservesScheduleAndAutoSwitch(t *testing.T) {
	e := newEnv(t)
	base := map[string]any{"enabled": true, "failureThreshold": 7, "cooldownMinutes": 20,
		"minimumRating": "good", "selectionRule": "best-quality", "preferredServerId": nil,
		"qualityRefreshEnabled": false, "qualityRefreshIntervalMinutes": 180, "futureSetting": "keep"}
	if err := compat.WriteJSON(autoSwitchFile(e.d), base); err != nil {
		t.Fatal(err)
	}
	r := call(t, "GET", "/api/servers/quality-refresh-status.cgi", "", nil)
	rev, ok := get(r.Body, "data", "revision").(string)
	if !ok || rev == "" {
		t.Fatalf("missing revision: %s", r.Raw)
	}
	for _, enabled := range []bool{true, false} {
		body, _ := json.Marshal(map[string]any{"enabled": enabled, "expectedRevision": rev})
		r = call(t, "POST", "/api/servers/quality-refresh-enabled.cgi", string(body), nil)
		if r.Code != 200 || get(r.Body, "data", "value") != enabled {
			t.Fatalf("save: %s", r.Raw)
		}
		cur, _ := readObjectFile(autoSwitchFile(e.d))
		for k, v := range base {
			if k != "qualityRefreshEnabled" && !jsonEq(cur[k], v) {
				t.Fatalf("changed %s", k)
			}
		}
		rev = get(r.Body, "data", "revision").(string)
	}
}

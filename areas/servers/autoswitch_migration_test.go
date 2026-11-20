package servers

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestSettingsToggleLegacyMissingPreferred(t *testing.T) {
	for _, tc := range []struct {
		name, rule, field string
		enabled           bool
		status            int
	}{
		{"best-quality-disable", "best-quality", "enabled", false, 200},
		{"best-quality-enable", "best-quality", "enabled", true, 200},
		{"lowest-ping-enable", "lowest-ping", "enabled", true, 200},
		{"manual-disable", "manual", "enabled", false, 200},
		{"preferred-disable", "preferred", "enabled", false, 200},
		{"preferred-enable-refused", "preferred", "enabled", true, 400},
		{"quality-enable", "best-quality", "qualityRefreshEnabled", true, 200},
		{"quality-disable", "preferred", "qualityRefreshEnabled", false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			base := map[string]any{"schemaVersion": 3, "enabled": true, "failureThreshold": 3, "cooldownMinutes": 10, "minimumRating": "acceptable", "selectionRule": tc.rule, "preferredServerId": "subscription-legacy-missing", "qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 60, "futureSetting": "preserve"}
			base[tc.field] = !tc.enabled
			if err := compat.WriteJSON(autoSwitchFile(e.d), base); err != nil {
				t.Fatal(err)
			}
			current, _ := readObjectFile(autoSwitchFile(e.d))
			body, _ := json.Marshal(map[string]any{"enabled": tc.enabled, "expectedRevision": autoSwitchRevision(current)})
			path := "/api/servers/auto-switch-enabled.cgi"
			if tc.field == "qualityRefreshEnabled" {
				path = "/api/servers/quality-refresh-enabled.cgi"
			}
			response := call(t, "POST", path, string(body), nil)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d: %s", response.Code, tc.status, response.Raw)
			}
			after, _ := readObjectFile(autoSwitchFile(e.d))
			if tc.status != 200 {
				if errCode(response) != "SERVER_NOT_FOUND" || !reflect.DeepEqual(current, after) {
					t.Fatalf("unsafe preferred enable: %s", response.Raw)
				}
				return
			}
			if after[tc.field] != tc.enabled {
				t.Fatalf("toggle not persisted: %v", after)
			}
			for key, value := range current {
				if key != tc.field && key != "updatedAt" && !jsonEq(after[key], value) {
					t.Fatalf("unrelated field %s changed", key)
				}
			}
			stale := call(t, "POST", path, string(body), nil)
			if stale.Code != 409 || errCode(stale) != "SETTINGS_CHANGED" {
				t.Fatalf("stale write: %s", stale.Raw)
			}
		})
	}
}

func TestSettingsToggleRejectsMalformedDormantPreferred(t *testing.T) {
	e := newEnv(t)
	base := map[string]any{"enabled": true, "failureThreshold": 3, "cooldownMinutes": 10, "minimumRating": "acceptable", "selectionRule": "best-quality", "preferredServerId": "../foreign", "qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 60}
	if err := compat.WriteJSON(autoSwitchFile(e.d), base); err != nil {
		t.Fatal(err)
	}
	before, _ := readObjectFile(autoSwitchFile(e.d))
	body, _ := json.Marshal(map[string]any{"enabled": false, "expectedRevision": autoSwitchRevision(before)})
	response := call(t, "POST", "/api/servers/auto-switch-enabled.cgi", string(body), nil)
	after, _ := readObjectFile(autoSwitchFile(e.d))
	if response.Code != 500 || errCode(response) != "CONFIG_INVALID" || !reflect.DeepEqual(before, after) {
		t.Fatalf("malformed settings accepted: %s", response.Raw)
	}
}

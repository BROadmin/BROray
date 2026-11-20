package servers

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestBackupStageDormantPreferredMigration(t *testing.T) {
	for _, rule := range []string{"best-quality", "lowest-ping", "manual", "preferred"} {
		for _, enabled := range []bool{false, true} {
			name := rule + "-disabled"
			if enabled {
				name = rule + "-enabled"
			}
			t.Run(name, func(t *testing.T) {
				e := newEnv(t)
				live := map[string]any{"schemaVersion": 3, "enabled": false, "failureThreshold": 3, "cooldownMinutes": 10, "minimumRating": "acceptable", "selectionRule": "best-quality", "preferredServerId": nil, "qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 60}
				if err := compat.WriteJSON(autoSwitchFile(e.d), live); err != nil {
					t.Fatal(err)
				}
				liveBefore, err := os.ReadFile(autoSwitchFile(e.d))
				if err != nil {
					t.Fatal(err)
				}
				settings := map[string]any{"schemaVersion": 3, "enabled": enabled, "failureThreshold": 3, "cooldownMinutes": 10, "minimumRating": "acceptable", "selectionRule": rule, "preferredServerId": "subscription-legacy-missing", "qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 60}
				auto, _ := json.Marshal(settings)
				payload, _ := json.Marshal(serversBackup{Records: []json.RawMessage{}, AutoSwitch: auto})
				stage, err := compat.NewBackupStage(e.d, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				_, err = e.s.backupStage(e.d.Ctx, payload, stage)
				if rule == "preferred" {
					if err == nil {
						t.Fatal("selected missing preferred server accepted")
					}
				} else {
					if err != nil {
						t.Fatal("legacy dormant preferred reference blocked backup", err)
					}
					staged, ok := readObjectFile(autoSwitchFile(stage.St))
					if !ok || !sameSettings(settings, staged) {
						t.Fatal("backup lost dormant ID or neighboring settings")
					}
				}
				liveAfter, err := os.ReadFile(autoSwitchFile(e.d))
				if err != nil || !bytes.Equal(liveBefore, liveAfter) || len(e.applied) != 0 {
					t.Fatal("backup preview changed live settings or runtime")
				}
			})
		}
	}
}

func TestBackupDormantPreferredStillValidatesID(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{"../foreign", ".hidden", "bad/id"} {
		t.Run(id, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"enabled": true, "failureThreshold": 3, "cooldownMinutes": 10, "minimumRating": "acceptable", "selectionRule": "best-quality", "preferredServerId": id})
			if _, err := e.s.backupAutoSwitch(raw, func(string) bool { return false }); err == nil {
				t.Fatal("malformed dormant ID accepted")
			}
		})
	}
}

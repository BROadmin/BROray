package servers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestCoreMaintenancePausesAutoWithoutChangingSettings(t *testing.T) {
	e := newEnv(t)
	e.addServer(t, "trojan://synthetic@fixture.example:443#fixture")
	cfg := map[string]any{"schemaVersion": 3, "enabled": true, "qualityRefreshEnabled": true, "qualityRefreshIntervalMinutes": 30}
	if err := compat.WriteJSON(autoSwitchFile(e.d), cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(autoSwitchFile(e.d))
	if err := os.MkdirAll(e.d.Root.State(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.d.Root.State(), "xray-maintenance.json"), []byte(`{"schemaVersion":1,"status":"removed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.s.enqueueDue(); err != nil {
		t.Fatal(err)
	}
	if rows := e.d.Queue.QueueRows(); len(rows) != 0 {
		t.Fatalf("automation scheduled against deliberately removed core: %v", rows)
	}
	if after, _ := os.ReadFile(autoSwitchFile(e.d)); string(after) != string(before) {
		t.Fatal("user automation preferences changed")
	}
}

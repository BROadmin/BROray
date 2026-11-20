package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

func seedBackupGeodata(t *testing.T, w *world, enabled bool) routepolicy.State {
	t.Helper()
	dir := filepath.Join(w.d.Root.Runtime(), geodata.AssetDirectory)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := geodata.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	core := os.Getenv("BRORAY_XRAY_BIN")
	if core == "" {
		t.Fatal("pinned native core required")
	}
	fetch := func(context.Context, geodata.Source) ([]byte, error) {
		return os.ReadFile(filepath.Join(filepath.Dir(core), "fi.dat"))
	}
	candidate, err := store.Prepare(context.Background(), geodata.Selection{CountryCodes: []string{"fi"}}, geodata.Revisions{GeoIP: strings.Repeat("a", 40)}, fetch, func(context.Context, geodata.Snapshot) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Commit(context.Background(), candidate, "")
	if err != nil {
		t.Fatal(err)
	}
	state := routepolicy.State{SchemaVersion: 1, GeoRevision: snapshot.Revision, Rules: []routepolicy.Rule{{ID: "finland", Name: "Finland", Enabled: enabled, Action: routepolicy.Deny, CountryCodes: []string{"fi"}}}}
	writeBackupPolicy(t, w, state)
	return state
}
func writeBackupPolicy(t *testing.T, w *world, state routepolicy.State) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = routepolicy.DecodeState(data); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(w.d.Root.Routes(), routepolicy.StateFile), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func nativeBackupValidator(t *testing.T, w *world) {
	t.Helper()
	w.d.Svc.XrayValidateStagedConfig = func(ctx context.Context, cfg []byte, assets *os.File) error {
		if !compat.FenceHeld(ctx) || assets.Name() == w.d.Root.Runtime() {
			t.Fatal("live/unfenced backup validation")
		}
		core := os.Getenv("BRORAY_XRAY_BIN")
		if core == "" {
			t.Fatal("pinned native core required")
		}
		cmd := exec.CommandContext(ctx, core, "run", "-test", "-c", "stdin:", "-format", "json")
		cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+assets.Name())
		cmd.Stdin = bytes.NewReader(cfg)
		return cmd.Run()
	}
}
func exportGeodataCopy(t *testing.T) string {
	t.Helper()
	ex := do(t, "POST", "/api/broray/backup-export.cgi", map[string]any{"encrypt": false, "acknowledgeUnencrypted": true})
	if ex.code != 200 {
		t.Fatal(ex.code, ex.raw)
	}
	return ex.raw
}
func TestBackupGeodataAcrossAreasOfflineUndo(t *testing.T) {
	source := newWorld(t)
	seedBackupGeodata(t, source, false)
	schedule, err := routepolicy.ReadGeoSchedule(context.Background(), source.d.Root.Routes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = routepolicy.SaveGeoSchedule(context.Background(), source.d.Root.Routes(), routepolicy.GeoScheduleRevision(schedule), true); err != nil {
		t.Fatal(err)
	}
	file := exportGeodataCopy(t)
	target := newWorld(t)
	nativeBackupValidator(t, target)
	before, err := routepolicy.ReadState(context.Background(), target.d.Root.Routes())
	if err != nil {
		t.Fatal(err)
	}
	pv := preview(t, file, "")
	if pv.code != 200 {
		t.Fatal(pv.code, pv.raw)
	}
	current, err := routepolicy.ReadState(context.Background(), target.d.Root.Routes())
	if err != nil || len(current.Rules) != len(before.Rules) {
		t.Fatal("preview writes live policy", err)
	}
	r := importFile(t, file, "")
	id := importCopyID(t, r)
	current, err = routepolicy.ReadState(context.Background(), target.d.Root.Routes())
	if err != nil || len(current.Rules) != 1 || current.Rules[0].Enabled {
		t.Fatal("OFF rule not restored", err)
	}
	schedule, err = routepolicy.ReadGeoSchedule(context.Background(), target.d.Root.Routes())
	if err != nil || !schedule.Enabled || len(schedule.Attempts) != 0 {
		t.Fatal("schedule choice/history", err, schedule)
	}
	u := undo(t, id)
	if u.code != 200 {
		t.Fatal(u.code, u.raw)
	}
	current, err = routepolicy.ReadState(context.Background(), target.d.Root.Routes())
	if err != nil || len(current.Rules) != 0 {
		t.Fatal("policy undo", err)
	}
	schedule, err = routepolicy.ReadGeoSchedule(context.Background(), target.d.Root.Routes())
	if err != nil || schedule.Enabled {
		t.Fatal("absent schedule undo corrupted defaults", err, schedule)
	}
}
func TestBackupGeodataRefreshesUnchangedActiveServer(t *testing.T) {
	w := newWorld(t)
	state := seedBackupGeodata(t, w, true)
	nativeBackupValidator(t, w)
	id := w.manual(t, "trojan://test@a.example:443#A")
	r := do(t, "POST", "/api/servers/activate.cgi", map[string]any{"id": id})
	if r.code != 200 {
		t.Fatal(r.code, r.raw)
	}
	file := exportGeodataCopy(t)
	state.Rules[0].Enabled = false
	writeBackupPolicy(t, w, state)
	if err := w.d.Svc.ReapplyActiveServer(compat.WithFence(context.Background()), "test-before-import"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(w.d.Root.XrayConfig())
	r = importFile(t, file, "")
	copyID := importCopyID(t, r)
	after, err := os.ReadFile(w.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) || !bytes.Contains(after, []byte("geoip-fi.dat:fi")) {
		t.Fatal("saved policy restored but active same-server config unchanged")
	}
	u := undo(t, copyID)
	if u.code != 200 {
		t.Fatal(u.code, u.raw)
	}
	after, err = os.ReadFile(w.d.Root.XrayConfig())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("active policy undo", err)
	}
}

func TestBackupGeodataStoppedAndFailedRuntime(t *testing.T) {
	for _, kind := range []string{"stopped", "failure"} {
		t.Run(kind, func(t *testing.T) {
			w := newWorld(t)
			state := seedBackupGeodata(t, w, true)
			nativeBackupValidator(t, w)
			id := w.manual(t, "trojan://test@a.example:443#A")
			r := do(t, "POST", "/api/servers/activate.cgi", map[string]any{"id": id})
			if r.code != 200 {
				t.Fatal(r.code, r.raw)
			}
			file := exportGeodataCopy(t)
			state.Rules[0].Enabled = false
			writeBackupPolicy(t, w, state)
			if err := w.d.Svc.ReapplyActiveServer(compat.WithFence(context.Background()), "before-import"); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(w.d.Root.XrayConfig())
			calls := 0
			if kind == "stopped" {
				w.d.Svc.XrayRunning = func() bool { return false }
			}
			w.d.Svc.XrayApplyConfig = func(context.Context, []byte) error { calls++; return errors.New("injected owned apply failure") }
			r = importFile(t, file, "")
			copyID := importCopyID(t, r)
			if kind == "stopped" && calls != 0 || kind == "failure" && calls != 1 {
				t.Fatal("wrong runtime apply count", calls)
			}
			if kind == "failure" && len(r.data()["followUps"].([]any)) == 0 {
				t.Fatal("runtime failure not reported")
			}
			cfg, err := os.ReadFile(w.d.Root.XrayConfig())
			if err != nil || bytes.Equal(cfg, before) || !bytes.Contains(cfg, []byte("geoip-fi.dat:fi")) {
				t.Fatal("import left inconsistent reboot config", err)
			}
			u := undo(t, copyID)
			if u.code != 200 {
				t.Fatal(u.code, u.raw)
			}
			cfg, err = os.ReadFile(w.d.Root.XrayConfig())
			if err != nil || !bytes.Equal(cfg, before) {
				t.Fatal("undo did not restore reboot config", err)
			}
			if kind == "stopped" && calls != 0 || kind == "failure" && calls != 2 {
				t.Fatal("wrong undo runtime apply count", calls)
			}
		})
	}
}

func TestBackupGeodataEmptyPolicyClearsAndUndo(t *testing.T) {
	newWorld(t)
	empty := exportGeodataCopy(t)
	w := newWorld(t)
	old := seedBackupGeodata(t, w, true)
	nativeBackupValidator(t, w)
	id := w.manual(t, "trojan://test@a.example:443#A")
	r := do(t, "POST", "/api/servers/activate.cgi", map[string]any{"id": id})
	if r.code != 200 {
		t.Fatal(r.code, r.raw)
	}
	before, err := os.ReadFile(w.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	r = importFile(t, empty, "")
	copyID := importCopyID(t, r)
	state, err := routepolicy.ReadState(context.Background(), w.d.Root.Routes())
	if err != nil || len(state.Rules) != 0 {
		t.Fatal("empty policy not restored", err)
	}
	cfg, err := os.ReadFile(w.d.Root.XrayConfig())
	if err != nil || bytes.Contains(cfg, []byte("geoip-fi.dat:fi")) {
		t.Fatal("empty policy left geo rule active", err)
	}
	// Re-export must remain valid; the cleared policy and current pointer agree.
	exportGeodataCopy(t)
	store, err := geodata.Open(filepath.Join(w.d.Root.Runtime(), geodata.AssetDirectory))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load(context.Background())
	store.Close()
	if err != nil || snapshot.Revision != state.GeoRevision || snapshot.PreviousRevision != old.GeoRevision {
		t.Fatal("predecessor not retained", err, snapshot)
	}
	u := undo(t, copyID)
	if u.code != 200 {
		t.Fatal(u.code, u.raw)
	}
	cfg, err = os.ReadFile(w.d.Root.XrayConfig())
	if err != nil || !bytes.Equal(before, cfg) {
		t.Fatal("empty-policy undo changed original config", err)
	}
}

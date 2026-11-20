package servers

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

const reserveSettingsURL = "/api/servers/reserve-settings.cgi"

var reserveSettingsHeaders = map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com"}

func reserveGetRevision(t *testing.T) string {
	t.Helper()
	r := call(t, "GET", reserveSettingsURL, "", nil)
	if r.Code != 200 {
		t.Fatal(r.Raw)
	}
	rev, ok := get(r.Body, "data", "revision").(string)
	if !ok || len(rev) != 64 {
		t.Fatal("missing revision", r.Raw)
	}
	return rev
}

func reservePost(t *testing.T, change map[string]any) resp {
	t.Helper()
	b, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	return call(t, "POST", reserveSettingsURL, string(b), reserveSettingsHeaders)
}

func TestReservePreferencesDraftCASAndBusy(t *testing.T) {
	e := newEnv(t)
	primary := e.addServer(t, "trojan://pw@main.example:443#Main")
	backup := e.addServer(t, "trojan://pw@backup.example:443#Backup")
	oldAuto := []byte(`{"enabled":true,"qualityRefreshEnabled":true,"custom":"keep"}`)
	if err := os.WriteFile(autoSwitchFile(e.d), oldAuto, 0600); err != nil {
		t.Fatal(err)
	}
	r := call(t, "GET", reserveSettingsURL, "", nil)
	if r.Code != 200 || get(r.Body, "data", "enabled") != false || get(r.Body, "data", "configured") != false {
		t.Fatal("not default OFF", r.Raw)
	}
	if _, err := os.Lstat(reservePreferencesFile(e.d)); !os.IsNotExist(err) {
		t.Fatal("GET created settings")
	}
	rev := reserveGetRevision(t)
	change := map[string]any{"primaryServerId": primary, "expectedRevision": rev}
	r = reservePost(t, change)
	if r.Code != 200 || get(r.Body, "data", "primaryServerId") != primary || get(r.Body, "data", "configured") != false {
		t.Fatal("primary draft not saved", r.Raw)
	}
	if stale := reservePost(t, change); stale.Code != 409 || errCode(stale) != "SETTINGS_CHANGED" {
		t.Fatal("stale write accepted", stale.Raw)
	}
	rev = reserveGetRevision(t)
	release, ok := e.d.Lock.TryAcquire("backup", "backup:restore", "")
	if !ok {
		t.Fatal("fixture fence unavailable")
	}
	busy := reservePost(t, map[string]any{"backupServerIds": []string{backup}, "expectedRevision": rev})
	release()
	if busy.Code != 409 || errCode(busy) != "OPERATION_BUSY" {
		t.Fatal("global fence bypassed", busy.Raw)
	}
	r = reservePost(t, map[string]any{"backupServerIds": []string{backup}, "expectedRevision": rev})
	if r.Code != 200 || get(r.Body, "data", "enabled") != false || get(r.Body, "data", "configured") != true || get(r.Body, "data", "primaryServerId") != primary {
		t.Fatal("backup autosave changed primary", r.Raw)
	}
	stored, err := os.ReadFile(reservePreferencesFile(e.d))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stored, []byte(backup)) || !bytes.Contains(stored, []byte(primary)) {
		t.Fatal("roster not durable")
	}
	auto, err := os.ReadFile(autoSwitchFile(e.d))
	if err != nil || !bytes.Equal(auto, oldAuto) || len(e.applied) != 0 || e.ensured.Load() != 0 {
		t.Fatal("saving roster changed ordinary settings/runtime")
	}
	// A fresh service instance reads the durable selection, not a cached form.
	current.Store(newSvc(e.d))
	if got := reserveGetRevision(t); got != get(r.Body, "data", "revision") {
		t.Fatal("reload revision mismatch")
	}
}

func TestReservePreferencesRejectsAmbiguousInput(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@main.example:443#Main")
	rev := reserveGetRevision(t)
	for _, body := range []string{
		`{}`, `{"expectedRevision":"` + rev + `","enabled":true}`,
		`{"expectedRevision":"` + rev + `","primaryServerId":null}`,
		`{"expectedRevision":"` + rev + `","PrimaryServerId":"` + a + `"}`,
		`{"expectedRevision":"` + rev + `","primaryServerId":"","primaryServerId":"` + a + `"}`,
		`{"expectedRevision":"` + rev + `","backupServerIds":null}`,
		`{"expectedRevision":"` + rev + `","backupServerIds":["` + a + `","` + a + `"]}`,
		`{"expectedRevision":"` + rev + `","primaryServerId":"` + a + `","backupServerIds":["` + a + `"]}`,
		`{"expectedRevision":"` + rev + `","backupServerIds":["one","two","three"]}`,
		`{"expectedRevision":"` + rev + `","primaryServerId":"../outside"}`,
		`{"expectedRevision":"` + rev + `","primaryServerId":"missing-server"}`,
		`{"expectedRevision":"` + rev + `","primaryServerId":""} {}`,
	} {
		r := call(t, "POST", reserveSettingsURL, body, reserveSettingsHeaders)
		if r.Code != 400 {
			t.Errorf("input accepted %s: %s", body, r.Raw)
		}
	}
	if _, err := os.Lstat(reservePreferencesFile(e.d)); !os.IsNotExist(err) {
		t.Fatal("invalid input wrote file")
	}
	r := call(t, "POST", reserveSettingsURL, `{"expectedRevision":"`+rev+`","primaryServerId":""}`, nil)
	if r.Code != 403 {
		t.Fatal("unguarded mutation accepted", r.Raw)
	}
}

func TestReservePreferencesRejectsTwoVariantsOfOneServer(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{"uri", "json"} {
		if err := WriteRecord(e.d, presentationRecord(t, id, "")); err != nil {
			t.Fatal(err)
		}
	}
	other := e.addServer(t, "trojan://pw@other.test:443#Other")
	writeReserveAutomatic(t, e, "uri", other, false)
	rev := reserveGetRevision(t)
	r := reservePost(t, map[string]any{"expectedRevision": rev, "backupServerIds": []string{"json"}})
	if r.Code != 400 || errCode(r) != "RESERVE_DUPLICATE_SERVER" {
		t.Fatal("two variants of one server were accepted", r.Raw)
	}
	if got := reserveGetRevision(t); got != rev {
		t.Fatal("rejected selection changed saved roster")
	}
	r = reservePost(t, map[string]any{"expectedRevision": rev, "primaryServerId": "json", "backupServerIds": []string{other}})
	if r.Code != 200 || get(r.Body, "data", "primaryServerId") != "json" {
		t.Fatal("exact alternate profile was not preserved", r.Raw)
	}
}

func TestReservePreferencesPreservesInvalidStorage(t *testing.T) {
	for _, kind := range []string{"corrupt", "duplicate", "symlink", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			rev := reserveGetRevision(t)
			path := reservePreferencesFile(e.d)
			data := []byte(`{"schemaVersion":1,"enabled":false,"primaryServerId":"","backupServerIds":[],"enabled":true}`)
			if kind == "corrupt" {
				data = []byte("{broken")
			}
			if kind == "oversize" {
				data = []byte(strings.Repeat(" ", 4096) + `{"schemaVersion":1,"enabled":false,"primaryServerId":"","backupServerIds":[]}`)
			}
			if kind == "symlink" {
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			r := reservePost(t, map[string]any{"primaryServerId": "", "expectedRevision": rev})
			if r.Code != 500 || errCode(r) != "CONFIG_INVALID" {
				t.Fatal("invalid storage was reset", r.Raw)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, data) {
				t.Fatal("invalid bytes changed")
			}
			if kind == "symlink" {
				st, err := os.Lstat(path)
				if err != nil || st.Mode()&os.ModeSymlink == 0 {
					t.Fatal("symlink replaced")
				}
			}
		})
	}
}

func TestReservePreferencesBackupInactiveRoundtrip(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@main.example:443#Main")
	b := e.addServer(t, "trojan://pw@backup.example:443#Backup")
	response := reservePost(t, map[string]any{"primaryServerId": a, "backupServerIds": []string{b}, "expectedRevision": reserveGetRevision(t)})
	if response.Code != 200 {
		t.Fatal(response.Raw)
	}
	exported, err := e.s.backupExport()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"reserve"`)) {
		t.Fatal("backup lost reserve roster")
	}
	before, err := os.ReadFile(reservePreferencesFile(e.d))
	if err != nil {
		t.Fatal(err)
	}
	// Clear the live draft first; stage must restore only into the swap area.
	response = reservePost(t, map[string]any{"primaryServerId": "", "backupServerIds": []string{}, "expectedRevision": reserveGetRevision(t)})
	if response.Code != 200 {
		t.Fatal(response.Raw)
	}
	live, err := os.ReadFile(reservePreferencesFile(e.d))
	if err != nil {
		t.Fatal(err)
	}
	stage, err := compat.NewBackupStage(e.d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.backupStage(e.d.Ctx, payload, stage); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(reservePreferencesFile(stage.St))
	if err != nil || !bytes.Equal(restored, before) {
		t.Fatal("staged roster not restored", err)
	}
	after, err := os.ReadFile(reservePreferencesFile(e.d))
	if err != nil || !bytes.Equal(after, live) || len(e.applied) != 0 {
		t.Fatal("staging mutated live config")
	}
	// An older backup without the reserve field leaves the live selection alone.
	old := []byte(`{"records":[],"activeServerId":null,"autoSwitch":null}`)
	legacy, err := compat.NewBackupStage(e.d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.backupStage(e.d.Ctx, old, legacy); err != nil {
		t.Fatal("legacy backup rejected", err)
	}
}

func TestReservePreferencesLegacyNullBackup(t *testing.T) {
	e := newEnv(t)
	e.addServer(t, "trojan://pw@main.example:443#Main")
	for _, raw := range [][]byte{nil, []byte("null"), []byte(" \nnull\t "), []byte("{}")} {
		stage, err := compat.NewBackupStage(e.d, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.backupStage(e.d.Ctx, raw, stage); err != nil {
			t.Fatalf("legacy empty section %q rejected: %v", raw, err)
		}
	}
	for _, raw := range [][]byte{[]byte(" \n\t "), []byte("null {}")} {
		stage, err := compat.NewBackupStage(e.d, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.backupStage(e.d.Ctx, raw, stage); err == nil {
			t.Fatalf("invalid section %q accepted", raw)
		}
	}
}

func TestReservePreferencesBackupRefusesInvalidOrActive(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@main.example:443#Main")
	b := e.addServer(t, "trojan://pw@backup.example:443#Backup")
	for _, kind := range []string{"missing", "active", "duplicate-container", "alias-container", "duplicate-field"} {
		t.Run(kind, func(t *testing.T) {
			prefs := reservePreferences{1, kind == "active", a, []string{b}, nil}
			if kind == "missing" {
				prefs.BackupServerIDs = []string{"missing-server"}
			}
			value, err := json.Marshal(prefs)
			if err != nil {
				t.Fatal(err)
			}
			body := `{"records":[],"reserve":` + string(value) + `}`
			if kind == "duplicate-container" {
				body = `{"records":[],"reserve":` + string(value) + `,"reserve":` + string(value) + `}`
			}
			if kind == "alias-container" {
				body = `{"records":[],"Reserve":` + string(value) + `}`
			}
			if kind == "duplicate-field" {
				body = strings.Replace(body, `"enabled":false`, `"enabled":false,"enabled":true`, 1)
			}
			stage, err := compat.NewBackupStage(e.d, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.s.backupStage(e.d.Ctx, []byte(body), stage); err == nil {
				t.Fatal("unsafe reserve restore accepted", kind)
			}
			if _, err := os.Lstat(reservePreferencesFile(e.d)); !os.IsNotExist(err) {
				t.Fatal("restore stage wrote live settings")
			}
		})
	}
}

func TestReservePreferencesActiveRosterRequiresQueue(t *testing.T) {
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@main.example:443#Main")
	b := e.addServer(t, "trojan://pw@backup.example:443#Backup")
	cfg := map[string]any{"schemaVersion": 1, "enabled": true, "primaryServerId": a, "backupServerIds": []string{b}}
	if err := compat.WriteJSON(reservePreferencesFile(e.d), cfg); err != nil {
		t.Fatal(err)
	}
	rev := reserveGetRevision(t)
	r := reservePost(t, map[string]any{"backupServerIds": []string{}, "expectedRevision": rev})
	if r.Code != 409 || errCode(r) != "RESERVE_MODE_ACTIVE" {
		t.Fatal("active roster changed outside queue", r.Raw)
	}
	data, err := os.ReadFile(reservePreferencesFile(e.d))
	if err != nil || !strings.Contains(string(data), b) {
		t.Fatal("active roster lost")
	}
}

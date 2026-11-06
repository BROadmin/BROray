package ops_test

// Settings backup across the real areas (servers, subscriptions, routes,
// dns, ops) on a temporary root: export → wipe → preview → import.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"

	_ "github.com/BROadmin/broray/areas/dns"
	_ "github.com/BROadmin/broray/areas/ops"
	_ "github.com/BROadmin/broray/areas/routes"
	_ "github.com/BROadmin/broray/areas/subscriptions"
)

type world struct {
	d       *compat.Deps
	mu      sync.Mutex
	applied int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// servers run `xray run -test`; a stand-in binary accepts every config
	if err := os.WriteFile(root.XrayBinary(), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d := &compat.Deps{Root: root, Log: log.New(io.Discard, "", 0), Ctx: ctx, Svc: &compat.Services{}, Maintenance: true}
	d.Lock = compat.NewGlobalLock(d.RunPath("global-operation.json"))
	d.Queue = compat.NewCoordinator(d)
	w := &world{d: d}
	compat.StartAreas(d)
	d.Svc.XrayApplyConfig = func(ctx context.Context, cfg []byte) error {
		w.mu.Lock()
		w.applied++
		w.mu.Unlock()
		return platform.WriteFileAtomic(root.XrayConfig(), cfg, 0o600)
	}
	d.Svc.XrayRunning = func() bool { return true }
	d.Svc.KeeneticEnsureInterface = func(ctx context.Context) error { return nil }
	return w
}

type res struct {
	code int
	raw  string
	body map[string]any
}

func (r res) data() map[string]any { m, _ := r.body["data"].(map[string]any); return m }
func (r res) errCode() string {
	e, _ := r.body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func do(t *testing.T, method, path string, body any) res {
	t.Helper()
	var s string
	if b, ok := body.(string); ok {
		s = b
	} else if body != nil {
		b, _ := json.Marshal(body)
		s = string(b)
	}
	var rd io.Reader
	if s != "" {
		rd = strings.NewReader(s)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-BROray-Request", "1")
	r.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	p := path
	if i := strings.Index(p, "?"); i >= 0 {
		p = p[:i]
	}
	if !cgi.Serve(w, r, p, nil) {
		t.Fatalf("route %s not registered", p)
	}
	out := res{code: w.Code, raw: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &out.body)
	return out
}

func (w *world) manual(t *testing.T, link string) string {
	t.Helper()
	id, err := servers.NewManualID(w.d)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := servers.ParseLink(link, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := servers.WriteRecord(w.d, rec); err != nil {
		t.Fatal(err)
	}
	return id
}

func subServer(t *testing.T, subID, sid, link string, idx int) servers.Record {
	t.Helper()
	rec, err := servers.ParseLink(link, sid, map[string]any{"type": "subscription", "subscriptionId": subID, "nodeIndex": idx})
	if err != nil {
		t.Fatal(err)
	}
	key, err := servers.CanonicalKey(rec)
	if err != nil {
		t.Fatal(err)
	}
	rec["source"].(map[string]any)["importKey"] = key
	return rec
}

func (w *world) subscription(t *testing.T, id string, enabled bool, links ...string) {
	t.Helper()
	rec := map[string]any{"schemaVersion": 1, "id": id, "name": "Провайдер " + id, "url": "https://sub.example.com/" + id + "?token=SECRET",
		"httpUserAgent": "Happ/2.0", "sendDeviceInfo": false, "enabled": enabled, "autoUpdateEnabled": true, "updateIntervalMinutes": 60,
		"lastUpdateStatus": "success", "serversReceived": len(links), "createdAt": compat.Now(), "updatedAt": compat.Now()}
	if err := os.MkdirAll(w.d.Path("config", "subscriptions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(w.d.Path("config", "subscriptions", id+".json"), rec); err != nil {
		t.Fatal(err)
	}
	for i, l := range links {
		srv := subServer(t, id, id+"-n"+string(rune('0'+i)), l, i)
		if enabled {
			if err := servers.WriteRecord(w.d, srv); err != nil {
				t.Fatal(err)
			}
		} else {
			dir := w.d.Path("config", "disabled-subscription-servers", id)
			_ = os.MkdirAll(dir, 0o700)
			if err := compat.WriteJSON(filepath.Join(dir, srv["id"].(string)+".json"), srv); err != nil {
				t.Fatal(err)
			}
		}
	}
}

const batA = "route add 10.10.0.0 mask 255.255.0.0 0.0.0.0\r\nroute add 1.2.3.4 mask 255.255.255.255 0.0.0.0\r\n"

func (w *world) customRoutes(t *testing.T, name string) string {
	t.Helper()
	pv := do(t, "POST", "/api/routes/custom-preview.cgi", map[string]any{"name": name,
		"files": []any{map[string]any{"name": "мои сети.bat", "contentBase64": base64.StdEncoding.EncodeToString([]byte(batA))}}})
	if pv.code != 200 {
		t.Fatalf("custom preview %s", pv.raw)
	}
	cm := do(t, "POST", "/api/routes/custom-commit.cgi", map[string]any{"token": pv.data()["token"], "name": name})
	if cm.code != 200 {
		t.Fatalf("custom commit %s", cm.raw)
	}
	return cm.data()["bundleId"].(string)
}

func (w *world) dot(t *testing.T, ids []string, auto bool) {
	t.Helper()
	dir := w.d.Path("routes", "dot")
	_ = os.MkdirAll(dir, 0o700)
	if err := compat.WriteJSON(filepath.Join(dir, "config.json"), map[string]any{"schemaVersion": 3, "requestedIds": ids, "selectedIds": ids,
		"effectiveIds": []any{}, "migrationState": "none", "managed": []any{}, "quarantinedReceipts": []any{}, "updatedAt": nil}); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(filepath.Join(dir, "auto-check.json"), map[string]any{"schemaVersion": 1, "enabled": auto, "updatedAt": compat.Now()}); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := compat.ReadJSON(p, &m); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return m
}

func canon(t *testing.T, v any) string {
	b, err := compat.Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func preview(t *testing.T, file, password string) res {
	t.Helper()
	return do(t, "POST", "/api/broray/backup-preview.cgi", map[string]any{"file": file, "password": password})
}

func importFile(t *testing.T, file, password string) res {
	t.Helper()
	pv := preview(t, file, password)
	if pv.code != 200 {
		t.Fatalf("preview %d %s", pv.code, pv.raw)
	}
	return do(t, "POST", "/api/broray/backup-import.cgi", map[string]any{"file": file, "password": password, "confirmToken": pv.data()["confirmToken"]})
}

func count(r res, kind, action string) float64 {
	s, _ := r.data()["summary"].(map[string]any)[kind].(map[string]any)
	f, _ := s[action].(float64)
	return f
}

func TestBackupAcrossAreas(t *testing.T) {
	for _, password := range []string{"Пароль владельца 1", ""} {
		w := newWorld(t)
		a := w.manual(t, "trojan://pw-a@a.example:443#A")
		b := w.manual(t, "vless://0f6c1b2a-1111-2222-3333-444455556666@nl.example.net:443?type=tcp&security=reality&pbk=PBKPBKPBKPBKPBK&sni=a.com&flow=xtls-rprx-vision#NL")
		w.subscription(t, "sub-aaaa", true, "trojan://s1@s1.example:443#S1", "trojan://s2@s2.example:443#S2")
		w.subscription(t, "sub-bbbb", false, "trojan://s3@s3.example:443#S3")
		if r := do(t, "POST", "/api/servers/activate.cgi", map[string]any{"id": b}); r.code != 200 {
			t.Fatalf("activate %s", r.raw)
		}
		if r := do(t, "POST", "/api/servers/auto-switch-save.cgi", map[string]any{"enabled": true, "failureThreshold": 4, "cooldownMinutes": 15,
			"minimumRating": "good", "selectionRule": "preferred", "preferredServerId": a}); r.code != 200 {
			t.Fatalf("auto-switch %s", r.raw)
		}
		bundle := w.customRoutes(t, "Мои сети")
		w.dot(t, []string{"quad9", "google-primary"}, true)
		markInstalled := func(id string, v any) {
			for _, p := range []string{w.d.Path("routes", "state", id+".json"), w.d.Path("routes", "installed", "bundles", id+".json")} {
				m := readJSON(t, p)
				m["installedVersion"] = v
				if err := compat.WriteJSON(p, m); err != nil {
					t.Fatal(err)
				}
			}
		}
		markInstalled("telegram", map[string]any{"contentSha256": "x"}) // installed on Keenetic when the backup is made
		snapshot := func() map[string]string {
			out := map[string]string{}
			for _, id := range []string{a, b, "sub-aaaa-n0", "sub-aaaa-n1"} {
				out[id] = canon(t, readJSON(t, servers.RecordPath(w.d, id)))
			}
			out["sub-aaaa"] = canon(t, readJSON(t, w.d.Path("config", "subscriptions", "sub-aaaa.json")))
			out["sub-bbbb"] = canon(t, readJSON(t, w.d.Path("config", "subscriptions", "sub-bbbb.json")))
			out["disabled"] = canon(t, readJSON(t, w.d.Path("config", "disabled-subscription-servers", "sub-bbbb", "sub-bbbb-n0.json")))
			out["active"] = servers.ActiveID(w.d)
			as := readJSON(t, w.d.Path("config", "system", "server-auto-switch.json"))
			delete(as, "updatedAt")
			out["autoSwitch"] = canon(t, as)
			out["dot"] = canon(t, readJSON(t, w.d.Path("routes", "dot", "config.json"))["requestedIds"])
			sc := readJSON(t, w.d.Path("routes", "catalog-schedule.json"))
			out["schedule"] = canon(t, map[string]any{"h": sc["intervalHours"], "auto": sc["autoApply"]})
			return out
		}
		writeSched := func(h int, auto bool) {
			b, _ := json.Marshal(map[string]any{"schemaVersion": 1, "intervalHours": h, "autoApply": auto, "maxRemovalPercent": 50})
			if err := os.WriteFile(w.d.Path("routes", "catalog-schedule.json"), b, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		writeSched(24, true) // scheduled catalog updates are part of the backup
		before := snapshot()

		ex := do(t, "POST", "/api/broray/backup-export.cgi", map[string]any{"encrypt": password != "", "password": password, "acknowledgeUnencrypted": password == ""})
		if ex.code != 200 {
			t.Fatalf("export %d %s", ex.code, ex.raw)
		}
		file := ex.raw
		markInstalled("telegram", nil) // a router where the set is not installed
		if password != "" && (strings.Contains(file, "SECRET") || strings.Contains(file, "pw-a")) {
			t.Fatal("encrypted export leaks secrets")
		}
		if password == "" && !strings.Contains(file, "token=SECRET") {
			t.Fatal("plain export must carry the subscription URL")
		}
		for _, banned := range []string{"interface.json", "receipt", "managedRouteKeys", "operations.jsonl", "global-operation"} {
			if password == "" && strings.Contains(file, banned) {
				t.Errorf("export contains %s", banned)
			}
		}

		// identical state → nothing to change
		pv := preview(t, file, password)
		if pv.code != 200 || pv.data()["hasChanges"] != false {
			t.Fatalf("preview of the current state must be empty: %s", pv.raw)
		}

		// wipe: other active server, delete servers/subscriptions/custom set, change DoT
		c := w.manual(t, "trojan://pw-c@c.example:443#C")
		if r := do(t, "POST", "/api/servers/activate.cgi", map[string]any{"id": c}); r.code != 200 {
			t.Fatalf("activate c %s", r.raw)
		}
		for _, id := range []string{a, b} {
			if r := do(t, "POST", "/api/servers/delete.cgi", map[string]any{"id": id}); r.code != 200 {
				t.Fatalf("delete %s %s", id, r.raw)
			}
		}
		for _, id := range []string{"sub-aaaa", "sub-bbbb"} {
			if r := do(t, "POST", "/api/subscriptions/delete.cgi?id="+id, nil); r.code != 200 {
				t.Fatalf("delete sub %s %s", id, r.raw)
			}
		}
		if r := do(t, "POST", "/api/routes/custom-remove.cgi?bundleId="+bundle, map[string]any{}); r.code != 200 {
			t.Fatalf("custom remove %s", r.raw)
		}
		w.dot(t, []string{"cloudflare-primary"}, false)
		writeSched(0, false)
		w.applied = 0

		pv = preview(t, file, password)
		if pv.code != 200 {
			t.Fatalf("preview %s", pv.raw)
		}
		if count(pv, "servers", "add") != 5 || count(pv, "servers", "keep") != 1 || count(pv, "subscriptions", "add") != 2 ||
			count(pv, "customRoutes", "add") != 1 || count(pv, "settings", "replace") != 4 { // + routes schedule
			t.Fatalf("preview summary %v", pv.data()["summary"])
		}
		follow := strings.Join(func() []string {
			var s []string
			for _, x := range pv.data()["followUps"].([]any) {
				s = append(s, x.(string))
			}
			return s
		}(), "\n")
		if !strings.Contains(follow, "«Маршруты»: Telegram.") || !strings.Contains(follow, "DNS-over-TLS") {
			t.Fatalf("follow-ups %s", follow)
		}
		if _, err := os.Stat(servers.RecordPath(w.d, a)); !os.IsNotExist(err) {
			t.Fatal("preview wrote live data")
		}

		r := importFile(t, file, password)
		if r.code != 200 {
			t.Fatalf("import %d %s", r.code, r.raw)
		}
		after := snapshot()
		for k, v := range before {
			if after[k] != v {
				t.Errorf("%s differs after import:\n%s\n%s", k, v, after[k])
			}
		}
		if !servers.RecordExists(w.d, c) {
			t.Fatal("servers missing from the backup must stay")
		}
		if w.applied != 1 {
			t.Fatalf("active server must be re-applied once, got %d", w.applied)
		}
		if v := do(t, "POST", "/api/routes/custom-validate.cgi?bundleId="+bundle, map[string]any{}); v.code != 200 {
			t.Fatalf("restored custom set does not validate: %s", v.raw)
		}
		copyID := r.data()["preImportCopy"].(map[string]any)["id"].(string)
		if _, err := os.Stat(w.d.Path("backup", copyID, "root", "data", "servers", c+".json")); err != nil {
			t.Fatalf("pre-import copy lacks the replaced servers dir: %v", err)
		}
		// importing the same file again changes nothing
		if pv := preview(t, file, password); pv.data()["hasChanges"] != false {
			t.Fatalf("second preview %v", pv.data()["summary"])
		}
	}
}

func TestBackupAreasRejectsTraversalAndConflicts(t *testing.T) {
	w := newWorld(t)
	a := w.manual(t, "trojan://pw-a@a.example:443#A")
	ex := do(t, "POST", "/api/broray/backup-export.cgi", map[string]any{"encrypt": false, "acknowledgeUnencrypted": true})
	var f map[string]any
	if err := json.Unmarshal([]byte(ex.raw), &f); err != nil {
		t.Fatal(err)
	}
	rec := readJSON(t, servers.RecordPath(w.d, a))
	encode := func(parts map[string]any) string {
		p := map[string]any{"format": f["format"], "schemaVersion": 1, "brorayVersion": f["brorayVersion"], "createdAt": f["createdAt"], "parts": parts}
		b, _ := compat.Marshal(p)
		b = []byte(strings.TrimSpace(string(b)))
		file := map[string]any{"format": f["format"], "schemaVersion": 1, "brorayVersion": f["brorayVersion"], "createdAt": f["createdAt"],
			"encrypted": false, "payload": json.RawMessage(b), "payloadSha256": compat.SHA256Hex(b)}
		out, _ := json.Marshal(file)
		return string(out)
	}
	withID := func(id string) map[string]any {
		m := map[string]any{}
		for k, v := range rec {
			m[k] = v
		}
		m["id"] = id
		return m
	}
	sub := func(id string) map[string]any {
		return map[string]any{"schemaVersion": 1, "id": id, "name": "x", "url": "https://sub.example.com/x", "enabled": true,
			"autoUpdateEnabled": false, "updateIntervalMinutes": 60, "lastUpdateStatus": "never", "serversReceived": 0,
			"createdAt": compat.Now(), "updatedAt": compat.Now()}
	}
	bat := base64.StdEncoding.EncodeToString([]byte(batA))
	cases := []struct {
		name  string
		parts map[string]any
		code  string
	}{
		{"server id ..", map[string]any{"servers": map[string]any{"records": []any{withID("..")}}}, "BACKUP_INVALID"},
		{"server id traversal", map[string]any{"servers": map[string]any{"records": []any{withID("../../evil")}}}, "BACKUP_INVALID"},
		{"server hidden id", map[string]any{"servers": map[string]any{"records": []any{withID(".hidden")}}}, "BACKUP_INVALID"},
		{"active missing", map[string]any{"servers": map[string]any{"records": []any{}, "activeServerId": "manual-x"}}, "BACKUP_INVALID"},
		{"active traversal", map[string]any{"servers": map[string]any{"activeServerId": "../x"}}, "BACKUP_INVALID"},
		{"subscription traversal", map[string]any{"subscriptions": map[string]any{"records": []any{sub("../../x")}}}, "BACKUP_INVALID"},
		{"subscription catalog key", map[string]any{"subscriptions": map[string]any{"records": []any{sub("sub-1")},
			"catalogs": map[string]any{"../x": map[string]any{"live": []any{}}}}}, "BACKUP_INVALID"},
		{"subscription bad url", map[string]any{"subscriptions": map[string]any{"records": []any{func() map[string]any {
			m := sub("sub-1")
			m["url"] = "file:///etc/passwd"
			return m
		}()}}}, "BACKUP_INVALID"},
		{"manual server under subscription", map[string]any{"subscriptions": map[string]any{"records": []any{sub("sub-1")},
			"catalogs": map[string]any{"sub-1": map[string]any{"live": []any{withID("x1")}}}}}, "BACKUP_INVALID"},
		{"custom id traversal", map[string]any{"routes": map[string]any{"custom": []any{map[string]any{"id": "user-../../x", "name": "x",
			"files": []any{map[string]any{"name": "a.bat", "contentBase64": bat}}}}}}, "BACKUP_INVALID"},
		{"custom file path", map[string]any{"routes": map[string]any{"custom": []any{map[string]any{"id": "user-abc", "name": "x",
			"files": []any{map[string]any{"name": "../a.bat", "contentBase64": bat}}}}}}, "BACKUP_INVALID"},
		{"custom default route", map[string]any{"routes": map[string]any{"custom": []any{map[string]any{"id": "user-abc", "name": "x",
			"files": []any{map[string]any{"name": "a.bat", "contentBase64": base64.StdEncoding.EncodeToString([]byte("route add 0.0.0.0 mask 0.0.0.0 0.0.0.0\n"))}}}}}}, "BACKUP_INVALID"},
		{"builtin unknown", map[string]any{"routes": map[string]any{"builtinInstalled": []any{"../x"}}}, "BACKUP_INVALID"},
		{"dot unknown", map[string]any{"dns": map[string]any{"requestedIds": []any{"evil"}}}, "BACKUP_INVALID"},
	}
	for _, tc := range cases {
		r := preview(t, encode(tc.parts), "")
		if r.code != 400 || r.errCode() != tc.code {
			t.Errorf("%s: %d %s", tc.name, r.code, r.raw)
		}
		e, _ := r.body["error"].(map[string]any)
		t.Logf("%s → %v", tc.name, e["message"])
	}
	for _, p := range []string{filepath.Join(w.d.Root.Dir, "evil"), filepath.Join(w.d.Root.Dir, "x"), filepath.Join(filepath.Dir(w.d.Root.Dir), "evil")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s written", p)
		}
	}
	// a restored subscription that drops the server active now is refused
	w.subscription(t, "sub-aaaa", true, "trojan://s1@s1.example:443#S1")
	if r := do(t, "POST", "/api/servers/activate.cgi", map[string]any{"id": "sub-aaaa-n0"}); r.code != 200 {
		t.Fatalf("activate %s", r.raw)
	}
	r := preview(t, encode(map[string]any{"subscriptions": map[string]any{"records": []any{readJSON(t, w.d.Path("config", "subscriptions", "sub-aaaa.json"))},
		"catalogs": map[string]any{"sub-aaaa": map[string]any{"live": []any{}}}}}), "")
	if r.code != 409 || r.errCode() != "ACTIVE_SERVER_CONFLICT" {
		t.Fatalf("active conflict: %d %s", r.code, r.raw)
	}
}

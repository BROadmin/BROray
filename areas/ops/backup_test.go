package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

func init() { backupIterations = backupMinIter } // keep -race fast; the format records the count

func testPayload() backupPayload {
	return backupPayload{Format: backupFormat, SchemaVersion: 1, BROrayVersion: "3.5.0-test", CreatedAt: "2026-11-02T10:00:00Z",
		Parts: map[string]json.RawMessage{"fake": json.RawMessage(`{"v":"секрет <&>"}`)}}
}

func codeOfErr(err error) string {
	var be *backupError
	if errors.As(err, &be) {
		return be.Code
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestBackupFileEncryptedRoundTrip(t *testing.T) {
	file, err := encodeBackupFile(testPayload(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(file), "секрет") {
		t.Fatal("plaintext leaked into the encrypted file")
	}
	p, enc, err := decodeBackupFile(file, "correct horse")
	if err != nil || !enc || string(p.Parts["fake"]) != `{"v":"секрет <&>"}` || p.BROrayVersion != "3.5.0-test" {
		t.Fatalf("%v %v %s", err, enc, p.Parts["fake"])
	}
	for _, tc := range []struct{ pw, code string }{{"wrong password", "BACKUP_DECRYPT_FAILED"}, {"", "BACKUP_PASSWORD_REQUIRED"}} {
		if _, _, err := decodeBackupFile(file, tc.pw); codeOfErr(err) != tc.code {
			t.Errorf("password %q: %v", tc.pw, err)
		}
	}
	var f map[string]any
	_ = json.Unmarshal(file, &f)
	mutate := func(fn func(m map[string]any)) []byte {
		var m map[string]any
		_ = json.Unmarshal(file, &m)
		fn(m)
		b, _ := json.Marshal(m)
		return b
	}
	ct, _ := base64.StdEncoding.DecodeString(f["ciphertext"].(string))
	ct[len(ct)/2] ^= 1
	cases := []struct {
		name string
		b    []byte
		code string
	}{
		{"ciphertext bit flip", mutate(func(m map[string]any) { m["ciphertext"] = base64.StdEncoding.EncodeToString(ct) }), "BACKUP_DECRYPT_FAILED"},
		{"header version (AAD)", mutate(func(m map[string]any) { m["brorayVersion"] = "9.9.9" }), "BACKUP_DECRYPT_FAILED"},
		{"header date (AAD)", mutate(func(m map[string]any) { m["createdAt"] = "2020-01-01T00:00:00Z" }), "BACKUP_DECRYPT_FAILED"},
		{"iterations below minimum", mutate(func(m map[string]any) { m["encryption"].(map[string]any)["iterations"] = 1 }), "BACKUP_DAMAGED"},
		{"iterations above maximum", mutate(func(m map[string]any) { m["encryption"].(map[string]any)["iterations"] = 1e9 }), "BACKUP_DAMAGED"},
		{"unknown cipher", mutate(func(m map[string]any) { m["encryption"].(map[string]any)["cipher"] = "none" }), "BACKUP_DAMAGED"},
		{"short nonce", mutate(func(m map[string]any) { m["encryption"].(map[string]any)["nonce"] = "AAAA" }), "BACKUP_DAMAGED"},
		{"not base64", mutate(func(m map[string]any) { m["ciphertext"] = "!!" }), "BACKUP_DAMAGED"},
		{"format", mutate(func(m map[string]any) { m["format"] = "other" }), "BACKUP_FORMAT_INVALID"},
		{"schema", mutate(func(m map[string]any) { m["schemaVersion"] = 2 }), "BACKUP_SCHEMA_UNSUPPORTED"},
		{"not json", []byte("garbage"), "BACKUP_FORMAT_INVALID"},
		{"two documents", append(append([]byte{}, file...), file...), "BACKUP_FORMAT_INVALID"},
		{"oversized", make([]byte, backupMaxFileBytes+1), "BACKUP_TOO_LARGE"},
	}
	for _, tc := range cases {
		if _, _, err := decodeBackupFile(tc.b, "correct horse"); codeOfErr(err) != tc.code {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func TestBackupPBKDF2IterationLimit(t *testing.T) {
	file, err := encodeBackupFile(testPayload(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		iterations int
		want       string
	}{{2_000_000, "BACKUP_PASSWORD_REQUIRED"}, {2_000_001, "BACKUP_DAMAGED"}} {
		var m map[string]any
		if err := json.Unmarshal(file, &m); err != nil {
			t.Fatal(err)
		}
		m["encryption"].(map[string]any)["iterations"] = tc.iterations
		b, _ := json.Marshal(m)
		_, _, err := decodeBackupFile(b, "") // validation occurs before key derivation
		if got := codeOfErr(err); got != tc.want {
			t.Fatalf("iterations=%d: got %s want %s", tc.iterations, got, tc.want)
		}
	}
}
func TestBackupFilePlain(t *testing.T) {
	file, err := encodeBackupFile(testPayload(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(file), "секрет <&>") || !strings.Contains(string(file), `"encrypted": false`) {
		t.Fatalf("plain file must be readable JSON: %s", file)
	}
	p, enc, err := decodeBackupFile(file, "ignored")
	if err != nil || enc || string(p.Parts["fake"]) != `{"v":"секрет <&>"}` {
		t.Fatalf("%v %s", err, p.Parts["fake"])
	}
	edited := strings.Replace(string(file), "секрет", "secret", 1)
	if _, _, err := decodeBackupFile([]byte(edited), ""); codeOfErr(err) != "BACKUP_DAMAGED" {
		t.Fatalf("edited payload: %v", err)
	}
}

// ---------- handlers with a fake part ----------

// fakePart keeps one JSON value in data/fake/value.json.
func fakePart(a *area) compat.BackupPart {
	file := func(d *compat.Deps) string { return d.Path("fake", "value.json") }
	return compat.BackupPart{
		Name: "fake", Order: 1,
		Export: func(ctx context.Context) (any, error) {
			var v any
			if err := compat.ReadJSON(file(a.d), &v); err != nil {
				return nil, nil
			}
			return map[string]any{"value": v}, nil
		},
		Stage: func(ctx context.Context, raw json.RawMessage, st *compat.BackupStage) (compat.BackupPlan, error) {
			var plan compat.BackupPlan
			var in struct {
				Value string `json:"value"`
			}
			if len(raw) == 0 {
				return plan, nil
			}
			if json.Unmarshal(raw, &in) != nil || strings.Contains(in.Value, "/") {
				return plan, errors.New("BACKUP_INVALID: fake value invalid")
			}
			var cur string
			_ = compat.ReadJSON(file(a.d), &cur)
			if cur == in.Value {
				plan.Add("setting", "fake", "Fake", "unchanged", "")
				return plan, nil
			}
			plan.Add("setting", "fake", "Fake", "replace", in.Value)
			plan.FollowUps = append(plan.FollowUps, "fake follow-up")
			if err := st.Take(compat.DataRel("fake")); err != nil {
				return plan, err
			}
			if err := os.MkdirAll(st.St.Path("fake"), 0o700); err != nil {
				return plan, err
			}
			return plan, compat.WriteJSON(file(st.St), in.Value)
		},
	}
}

func backupArea(t *testing.T) *area {
	a := testArea(t)
	a.parts = func() []compat.BackupPart { return []compat.BackupPart{fakePart(a), a.settingsPart()} }
	return a
}

func TestBackupClearStagesPreservesUnverifiedEntry(t *testing.T) {
	a := backupArea(t)
	dir := a.backupDir(".stage-aaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "owner-sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	owned := a.backupDir(".stage-bbbbbbbbbbbbbbbb")
	if err := os.MkdirAll(owned, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, backupStageOwnerFile), []byte(backupStageOwnerText), 0o600); err != nil {
		t.Fatal(err)
	}
	a.clearStages()
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "keep" {
		t.Fatalf("unverified stage removed or changed: %q, %v", b, err)
	}
	if _, err := os.Lstat(owned); !os.IsNotExist(err) {
		t.Fatalf("verified interrupted stage not removed: %v", err)
	}
}

func TestBackupPruneCopiesPreservesUnverifiedEntry(t *testing.T) {
	a := backupArea(t)
	unknown := a.backupDir("settings-before-import-20260101-000000-aaaaaaaa")
	if err := os.MkdirAll(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(unknown, "owner-sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= backupKeep+1; i++ {
		id := fmt.Sprintf("settings-before-import-20261102-1010%02d-%08x", i, i)
		if err := os.MkdirAll(a.backupDir(id), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := compat.WriteJSON(a.backupDir(id, "manifest.json"), copyManifest{SchemaVersion: 1, Kind: "pre-import", Complete: true}); err != nil {
			t.Fatal(err)
		}
	}
	a.pruneCopies("settings-before-import")
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "keep" {
		t.Fatalf("unverified copy removed or changed: %q, %v", b, err)
	}
	oldestOwned := a.backupDir("settings-before-import-20261102-101001-00000001")
	if _, err := os.Stat(oldestOwned); !os.IsNotExist(err) {
		t.Fatalf("oldest verified copy not pruned: %v", err)
	}
}

func setFake(t *testing.T, a *area, v string) {
	t.Helper()
	if err := os.MkdirAll(a.d.Path("fake"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(a.d.Path("fake", "value.json"), v); err != nil {
		t.Fatal(err)
	}
}

func getFake(a *area) string {
	var v string
	_ = compat.ReadJSON(a.d.Path("fake", "value.json"), &v)
	return v
}

func body(v any) string { b, _ := json.Marshal(v); return string(b) }

func backupHdr() map[string]string {
	return map[string]string{"X-BROray-Request": "1", "Content-Type": "application/json", "Origin": "http://example.com"}
}

func exportFile(t *testing.T, password string) string {
	t.Helper()
	req := map[string]any{"encrypt": password != "", "password": password, "acknowledgeUnencrypted": password == ""}
	r := call(t, "POST", "/api/broray/backup-export.cgi", body(req), backupHdr())
	if r.Code != 200 || !strings.Contains(r.Header.Get("Content-Disposition"), `attachment; filename="broray-settings-`) {
		t.Fatalf("export %d %v %s", r.Code, r.Header, r.Raw)
	}
	return r.Raw
}

func TestBackupHandlersRoundTripAndUndo(t *testing.T) {
	for _, password := range []string{"owner-secret-1", ""} {
		a := backupArea(t)
		setFake(t, a, "original")
		file := exportFile(t, password)
		if password != "" && strings.Contains(file, "original") {
			t.Fatal("encrypted export leaks the value")
		}
		setFake(t, a, "changed")
		pv := call(t, "POST", "/api/broray/backup-preview.cgi", body(map[string]any{"file": file, "password": password}), backupHdr())
		if pv.Code != 200 {
			t.Fatalf("preview %d %s", pv.Code, pv.Raw)
		}
		d := data(pv)
		if d["hasChanges"] != true || d["file"].(map[string]any)["encrypted"] != (password != "") {
			t.Fatalf("preview %v", d)
		}
		if s := d["summary"].(map[string]any)["settings"].(map[string]any); s["replace"] != float64(1) {
			t.Fatalf("summary %v", s)
		}
		if getFake(a) != "changed" {
			t.Fatal("preview must not change live data")
		}
		token := d["confirmToken"].(string)
		// no / wrong token → refused, nothing changed
		r := call(t, "POST", "/api/broray/backup-import.cgi", body(map[string]any{"file": file, "password": password}), backupHdr())
		if r.Code != 400 || errCode(r) != "BACKUP_CONFIRM_REQUIRED" || getFake(a) != "changed" {
			t.Fatalf("no token %d %s", r.Code, r.Raw)
		}
		r = call(t, "POST", "/api/broray/backup-import.cgi", body(map[string]any{"file": file, "password": password, "confirmToken": strings.Repeat("0", 64)}), backupHdr())
		if r.Code != 409 || errCode(r) != "BACKUP_PLAN_CHANGED" || getFake(a) != "changed" {
			t.Fatalf("stale token %d %s", r.Code, r.Raw)
		}
		r = call(t, "POST", "/api/broray/backup-import.cgi", body(map[string]any{"file": file, "password": password, "confirmToken": token}), backupHdr())
		if r.Code != 200 || data(r)["applied"] != true {
			t.Fatalf("import %d %s", r.Code, r.Raw)
		}
		if getFake(a) != "original" {
			t.Fatalf("not restored: %q", getFake(a))
		}
		if fu := data(r)["followUps"].([]any); len(fu) != 1 || fu[0] != "fake follow-up" {
			t.Fatalf("followUps %v", fu)
		}
		copyID := data(r)["preImportCopy"].(map[string]any)["id"].(string)
		var pre string
		if err := compat.ReadJSON(a.backupDir(copyID, backupCopyRoot, "data", "fake", "value.json"), &pre); err != nil || pre != "changed" {
			t.Fatalf("pre-import copy: %q %v", pre, err)
		}
		st := call(t, "GET", "/api/broray/backup-status.cgi", "", nil)
		copies := data(st)["preImportCopies"].([]any)
		if len(copies) != 1 || copies[0].(map[string]any)["id"] != copyID {
			t.Fatalf("status %s", st.Raw)
		}
		if m, _ := filepath.Glob(a.backupDir(".stage-*")); len(m) != 0 {
			t.Fatalf("stage dirs left: %v", m)
		}
		// undo brings back the state before the import
		u := call(t, "POST", "/api/broray/backup-undo.cgi", body(map[string]any{"copyId": copyID}), backupHdr())
		if u.Code != 200 || getFake(a) != "changed" {
			t.Fatalf("undo %d %s → %q", u.Code, u.Raw, getFake(a))
		}
		if u := call(t, "POST", "/api/broray/backup-undo.cgi", body(map[string]any{"copyId": copyID}), backupHdr()); u.Code != 404 {
			t.Fatalf("second undo %d", u.Code)
		}
		for _, bad := range []string{"../../etc", "settings-before-import-x", ""} {
			if u := call(t, "POST", "/api/broray/backup-undo.cgi", body(map[string]any{"copyId": bad}), backupHdr()); u.Code != 400 {
				t.Fatalf("undo %q: %d", bad, u.Code)
			}
		}
	}
}

func TestBackupNewPathRemovedByUndo(t *testing.T) {
	a := backupArea(t)
	setFake(t, a, "original")
	file := exportFile(t, "owner-secret-1")
	if err := os.RemoveAll(a.d.Path("fake")); err != nil {
		t.Fatal(err)
	}
	pv := call(t, "POST", "/api/broray/backup-preview.cgi", body(map[string]any{"file": file, "password": "owner-secret-1"}), backupHdr())
	r := call(t, "POST", "/api/broray/backup-import.cgi", body(map[string]any{"file": file, "password": "owner-secret-1",
		"confirmToken": data(pv)["confirmToken"]}), backupHdr())
	if r.Code != 200 || getFake(a) != "original" {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	id := data(r)["preImportCopy"].(map[string]any)["id"].(string)
	if u := call(t, "POST", "/api/broray/backup-undo.cgi", body(map[string]any{"copyId": id}), backupHdr()); u.Code != 200 {
		t.Fatalf("undo %d %s", u.Code, u.Raw)
	}
	if _, err := os.Stat(a.d.Path("fake")); !os.IsNotExist(err) {
		t.Fatal("undo must remove what the import created")
	}
}

func TestBackupGuardsAndValidation(t *testing.T) {
	a := backupArea(t)
	setFake(t, a, "v")
	file := exportFile(t, "owner-secret-1")
	paths := []string{"/api/broray/backup-export.cgi", "/api/broray/backup-preview.cgi", "/api/broray/backup-import.cgi", "/api/broray/backup-undo.cgi"}
	for _, p := range paths {
		if r := call(t, "GET", p, "", nil); r.Code != 405 {
			t.Errorf("%s GET %d", p, r.Code)
		}
		if r := call(t, "POST", p, "{}", map[string]string{"Content-Type": "application/json", "Origin": "http://example.com"}); r.Code != 403 || errCode(r) != "INVALID_REQUEST" {
			t.Errorf("%s no CSRF header %d %s", p, r.Code, r.Raw)
		}
		if r := call(t, "POST", p, "{}", map[string]string{"X-BROray-Request": "1"}); r.Code != 403 || errCode(r) != "ORIGIN_REJECTED" {
			t.Errorf("%s no Origin %d %s", p, r.Code, r.Raw)
		}
		if r := call(t, "POST", p, "{}", map[string]string{"X-BROray-Request": "1", "Origin": "http://evil.example"}); r.Code != 403 || errCode(r) != "ORIGIN_REJECTED" {
			t.Errorf("%s foreign Origin %d %s", p, r.Code, r.Raw)
		}
	}
	cases := []struct {
		path string
		req  any
		code string
		st   int
	}{
		{"/api/broray/backup-export.cgi", map[string]any{"encrypt": true, "password": "short"}, "BACKUP_PASSWORD_INVALID", 400},
		{"/api/broray/backup-export.cgi", map[string]any{"encrypt": false}, "BACKUP_UNENCRYPTED_NOT_CONFIRMED", 400},
		{"/api/broray/backup-export.cgi", map[string]any{}, "BACKUP_OPTIONS_INVALID", 400},
		{"/api/broray/backup-preview.cgi", map[string]any{"password": "x"}, "BACKUP_FILE_REQUIRED", 400},
		{"/api/broray/backup-preview.cgi", map[string]any{"file": "{\"format\":\"zip\"}"}, "BACKUP_FORMAT_INVALID", 400},
		{"/api/broray/backup-preview.cgi", map[string]any{"file": file, "password": "wrong-password"}, "BACKUP_DECRYPT_FAILED", 400},
		{"/api/broray/backup-preview.cgi", map[string]any{"file": file}, "BACKUP_PASSWORD_REQUIRED", 400},
		{"/api/broray/backup-preview.cgi", map[string]any{"file": strings.Repeat("x", backupMaxFileBytes+1)}, "BACKUP_TOO_LARGE", 413},
	}
	for _, tc := range cases {
		r := call(t, "POST", tc.path, body(tc.req), backupHdr())
		if r.Code != tc.st || errCode(r) != tc.code {
			t.Errorf("%s %.80v: %d %.300s", tc.path, tc.req, r.Code, r.Raw)
		}
	}
	// body above the read limit
	if r := call(t, "POST", "/api/broray/backup-preview.cgi", strings.Repeat("x", backupBodyMax+1), backupHdr()); r.Code != 413 || errCode(r) != "BACKUP_TOO_LARGE" {
		t.Fatalf("huge body %d %s", r.Code, r.Raw)
	}
	// unknown part and invalid part values are refused before anything is written
	plain := func(parts map[string]json.RawMessage) string {
		p := testPayload()
		p.Parts = parts
		b, _ := encodeBackupFile(p, "")
		return string(b)
	}
	for _, tc := range []struct {
		parts map[string]json.RawMessage
		code  string
	}{
		{map[string]json.RawMessage{"evil": json.RawMessage(`{}`)}, "BACKUP_PART_UNSUPPORTED"},
		{map[string]json.RawMessage{"fake": json.RawMessage(`{"value":"../../etc/passwd"}`)}, "BACKUP_INVALID"},
		{map[string]json.RawMessage{"settings": json.RawMessage(`{"logLevel":"verbose"}`)}, "BACKUP_INVALID"},
		{map[string]json.RawMessage{"settings": json.RawMessage(`{"health":{"probeUrls":["file:///etc/passwd"],"intervalSec":15,"failureThreshold":3,"cooldownMinutes":10,"selectionRule":"best-quality"}}`)}, "BACKUP_INVALID"},
	} {
		r := call(t, "POST", "/api/broray/backup-preview.cgi", body(map[string]any{"file": plain(tc.parts)}), backupHdr())
		if r.Code != 400 || errCode(r) != tc.code {
			t.Errorf("%v: %d %s", tc.parts, r.Code, r.Raw)
		}
	}
	if getFake(a) != "v" {
		t.Fatal("refused previews must not change data")
	}
}

func TestBackupLockBusy(t *testing.T) {
	a := backupArea(t)
	setFake(t, a, "v")
	file := exportFile(t, "")
	pv := call(t, "POST", "/api/broray/backup-preview.cgi", body(map[string]any{"file": file}), backupHdr())
	token := data(pv)["confirmToken"]
	rel, ok := a.d.Lock.TryAcquire("routes", "export", "telegram")
	if !ok {
		t.Fatal("lock")
	}
	defer rel()
	for _, tc := range []struct{ path, body string }{
		{"/api/broray/backup-export.cgi", body(map[string]any{"encrypt": false, "acknowledgeUnencrypted": true})},
		{"/api/broray/backup-preview.cgi", body(map[string]any{"file": file})},
		{"/api/broray/backup-import.cgi", body(map[string]any{"file": file, "confirmToken": token})},
		{"/api/broray/backup-undo.cgi", body(map[string]any{"copyId": "settings-before-import-20261102-101010-abcdef01"})},
	} {
		r := call(t, "POST", tc.path, tc.body, backupHdr())
		if r.Code != 409 || errCode(r) != "BACKUP_OPERATION_BUSY" {
			t.Errorf("%s: %d %s", tc.path, r.Code, r.Raw)
		}
	}
	if m, _ := filepath.Glob(a.backupDir("settings-before-import-*")); len(m) != 0 {
		t.Fatal("a refused import must not create a pre-import copy")
	}
}

func TestBackupDecryptBeforeGlobalFence(t *testing.T) {
	a := backupArea(t)
	setFake(t, a, "v")
	file := exportFile(t, "correct horse")
	rel, ok := a.d.Lock.TryAcquire("routes", "export", "telegram")
	if !ok {
		t.Fatal("lock")
	}
	defer rel()
	for _, tc := range []struct {
		path string
		body map[string]any
	}{
		{"/api/broray/backup-preview.cgi", map[string]any{"file": file, "password": "wrong password"}},
		{"/api/broray/backup-import.cgi", map[string]any{"file": file, "password": "wrong password", "confirmToken": strings.Repeat("0", 64)}},
	} {
		r := call(t, "POST", tc.path, body(tc.body), backupHdr())
		if r.Code != 400 || errCode(r) != "BACKUP_DECRYPT_FAILED" {
			t.Errorf("%s: expected decryption before busy fence, got %d %s", tc.path, r.Code, r.Raw)
		}
	}
	if getFake(a) != "v" {
		t.Fatal("failed decryption changed live settings")
	}
}
func TestBackupSettingsPart(t *testing.T) {
	a := backupArea(t)
	cfg := platform.DefaultConfig()
	cfg.ListenAddress, cfg.WebPort, cfg.LogLevel = "192.168.1.1", 8080, "warning"
	if err := platform.SaveConfig(a.d.Root, cfg); err != nil {
		t.Fatal(err)
	}
	file := exportFile(t, "")
	if !strings.Contains(file, `\"logLevel\":\"warning\"`) && !strings.Contains(file, `"logLevel": "warning"`) {
		t.Fatalf("settings not exported: %s", file)
	}
	if strings.Contains(file, "192.168.1.1") || strings.Contains(file, "rciUrl") {
		t.Fatal("machine-specific config must not be exported")
	}
	// another router: other LAN address, debug log level
	cfg.ListenAddress, cfg.LogLevel = "10.0.0.1", "debug"
	if err := platform.SaveConfig(a.d.Root, cfg); err != nil {
		t.Fatal(err)
	}
	pv := call(t, "POST", "/api/broray/backup-preview.cgi", body(map[string]any{"file": file}), backupHdr())
	r := call(t, "POST", "/api/broray/backup-import.cgi", body(map[string]any{"file": file, "confirmToken": data(pv)["confirmToken"]}), backupHdr())
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	got, err := platform.LoadConfig(a.d.Root)
	if err != nil || got.LogLevel != "warning" || got.ListenAddress != "10.0.0.1" {
		t.Fatalf("%+v %v", got, err)
	}
	if fmt.Sprint(data(r)["followUps"]) == "[]" {
		t.Fatal("restart follow-up expected")
	}
}

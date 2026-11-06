package ops_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
)

// plainBackup builds an unencrypted backup file with the given parts.
func plainBackup(t *testing.T, w *world, parts map[string]any) string {
	t.Helper()
	ex := do(t, "POST", "/api/broray/backup-export.cgi", map[string]any{"encrypt": false, "acknowledgeUnencrypted": true})
	var f map[string]any
	if err := json.Unmarshal([]byte(ex.raw), &f); err != nil {
		t.Fatal(ex.raw)
	}
	p := map[string]any{"format": f["format"], "schemaVersion": 1, "brorayVersion": f["brorayVersion"], "createdAt": f["createdAt"], "parts": parts}
	b, _ := compat.Marshal(p)
	b = []byte(strings.TrimSpace(string(b)))
	file := map[string]any{"format": f["format"], "schemaVersion": 1, "brorayVersion": f["brorayVersion"], "createdAt": f["createdAt"],
		"encrypted": false, "payload": json.RawMessage(b), "payloadSha256": compat.SHA256Hex(b)}
	out, _ := json.Marshal(file)
	return string(out)
}

// A manual server of the backup must not overwrite a subscription server
// the same import stages (the live tree does not have it yet).
func TestBackupManualIDCollidesWithStagedSubscriptionServer(t *testing.T) {
	w := newWorld(t)
	srv := subServer(t, "sub-cccc", "sub-cccc-n0", "trojan://s1@s1.example:443#S1", 0)
	manual, err := servers.ParseLink("trojan://pw-m@m.example:443#M", "sub-cccc-n0", nil)
	if err != nil {
		t.Fatal(err)
	}
	sub := map[string]any{"schemaVersion": 1, "id": "sub-cccc", "name": "Провайдер", "url": "https://sub.example.com/c", "enabled": true,
		"autoUpdateEnabled": false, "updateIntervalMinutes": 60, "lastUpdateStatus": "never", "serversReceived": 1,
		"createdAt": compat.Now(), "updatedAt": compat.Now()}
	file := plainBackup(t, w, map[string]any{
		"subscriptions": map[string]any{"records": []any{sub}, "catalogs": map[string]any{"sub-cccc": map[string]any{"live": []any{srv}}}},
		"servers":       map[string]any{"records": []any{manual}},
	})
	r := preview(t, file, "")
	if r.code != 409 || r.errCode() != "BACKUP_CONFLICT" {
		t.Fatalf("preview %d %s", r.code, r.raw)
	}
}

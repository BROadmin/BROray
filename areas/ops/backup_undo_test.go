package ops_test

// Undo of an import must never bring back stale Keenetic ownership state:
// registries of custom route sets are not part of the swap, the DNS config
// is merged field by field, and an undo is refused once a swapped path was
// changed after the import.

import (
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func jsonOf(t *testing.T, v any) string { return strings.TrimSpace(canon(t, v)) }

func undo(t *testing.T, id string) res {
	t.Helper()
	return do(t, "POST", "/api/broray/backup-undo.cgi", map[string]any{"copyId": id})
}

func importCopyID(t *testing.T, r res) string {
	t.Helper()
	if r.code != 200 {
		t.Fatalf("import %d %s", r.code, r.raw)
	}
	return r.data()["preImportCopy"].(map[string]any)["id"].(string)
}

func manifestPaths(t *testing.T, w *world, id string) []string {
	t.Helper()
	m := readJSON(t, w.d.Path("backup", id, "manifest.json"))
	var out []string
	for _, e := range m["paths"].([]any) {
		out = append(out, e.(map[string]any)["path"].(string))
	}
	return out
}

func TestBackupRestoredRouteSetRegistryNotSwapped(t *testing.T) {
	w := newWorld(t)
	bundle := w.customRoutes(t, "Мои сети")
	ex := do(t, "POST", "/api/broray/backup-export.cgi", map[string]any{"encrypt": false, "acknowledgeUnencrypted": true})
	if ex.code != 200 {
		t.Fatal(ex.raw)
	}
	if r := do(t, "POST", "/api/routes/custom-remove.cgi?bundleId="+bundle, map[string]any{}); r.code != 200 {
		t.Fatal(r.raw)
	}
	regPath := w.d.Path("routes", "installed", "bundles", bundle+".json")
	if _, err := os.Stat(regPath); !os.IsNotExist(err) {
		t.Fatalf("registry after remove: %v", err)
	}
	copyID := importCopyID(t, importFile(t, ex.raw, ""))
	for _, p := range manifestPaths(t, w, copyID) {
		if strings.Contains(p, "installed") {
			t.Fatalf("ownership registry %s is part of the swap", p)
		}
	}
	// the registry of the restored set is seeded live (empty), so install works
	reg := readJSON(t, regPath)
	if reg["bundleId"] != bundle || reg["installedVersion"] != nil || len(reg["managedRouteKeys"].([]any)) != 0 {
		t.Fatalf("seeded registry %v", reg)
	}
	// plain undo still works and leaves no empty registry behind
	if u := undo(t, copyID); u.code != 200 {
		t.Fatalf("undo %d %s", u.code, u.raw)
	}
	if _, err := os.Stat(w.d.Path("routes", "catalog", bundle)); !os.IsNotExist(err) {
		t.Fatal("undo must remove the restored set")
	}
	if _, err := os.Stat(regPath); !os.IsNotExist(err) {
		t.Fatalf("empty seeded registry left after undo: %v", err)
	}
}

func TestBackupUndoRefusedAfterInstallKeepsReceipts(t *testing.T) {
	w := newWorld(t)
	bundle := w.customRoutes(t, "Мои сети")
	ex := do(t, "POST", "/api/broray/backup-export.cgi", map[string]any{"encrypt": false, "acknowledgeUnencrypted": true})
	if r := do(t, "POST", "/api/routes/custom-remove.cgi?bundleId="+bundle, map[string]any{}); r.code != 200 {
		t.Fatal(r.raw)
	}
	copyID := importCopyID(t, importFile(t, ex.raw, ""))
	// the owner installs the restored set after the import (receipts + state)
	regPath := w.d.Path("routes", "installed", "bundles", bundle+".json")
	reg := readJSON(t, regPath)
	reg["managedRouteKeys"] = []any{"ipv4|1.2.3.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified"}
	reg["installedVersion"] = map[string]any{"contentSha256": "x"}
	if err := compat.WriteJSON(regPath, reg); err != nil {
		t.Fatal(err)
	}
	statePath := w.d.Path("routes", "state", bundle+".json")
	st := readJSON(t, statePath)
	st["installedVersion"], st["status"] = map[string]any{"contentSha256": "x"}, "installed"
	if err := compat.WriteJSON(statePath, st); err != nil {
		t.Fatal(err)
	}
	u := undo(t, copyID)
	if u.code != 409 || u.errCode() != "BACKUP_UNDO_STATE_CHANGED" || !strings.Contains(u.raw, "отмена невозможна") {
		t.Fatalf("undo after install: %d %s", u.code, u.raw)
	}
	if r := readJSON(t, regPath); len(r["managedRouteKeys"].([]any)) != 1 {
		t.Fatalf("receipts lost: %v", r)
	}
	if r := readJSON(t, statePath); r["installedVersion"] == nil {
		t.Fatal("state replaced by a refused undo")
	}
	if _, err := os.Stat(w.d.Path("backup", copyID, "manifest.json")); err != nil {
		t.Fatalf("refused undo must keep the copy: %v", err)
	}
}

func TestBackupDotReceiptsSurviveImportAndUndo(t *testing.T) {
	w := newWorld(t)
	w.dot(t, []string{"quad9"}, false)
	ex := do(t, "POST", "/api/broray/backup-export.cgi", map[string]any{"encrypt": false, "acknowledgeUnencrypted": true})
	w.dot(t, []string{"cloudflare-primary"}, false)
	dotPath := w.d.Path("routes", "dot", "config.json")
	setManaged := func(id string) {
		dc := readJSON(t, dotPath)
		dc["managed"] = []any{dotReceipt(id)}
		dc["effectiveIds"] = []any{id}
		if err := compat.WriteJSON(dotPath, dc); err != nil {
			t.Fatal(err)
		}
	}
	managedID := func(m map[string]any) string {
		l, _ := m["managed"].([]any)
		if len(l) != 1 {
			return ""
		}
		return l[0].(map[string]any)["id"].(string)
	}
	setManaged("cloudflare-primary") // applied before the import
	pv := preview(t, ex.raw, "")
	if pv.code != 200 {
		t.Fatal(pv.raw)
	}
	// a DoT apply between the preview and the import is kept (the import re-stages)
	setManaged("cloudflare-secondary")
	r := do(t, "POST", "/api/broray/backup-import.cgi", map[string]any{"file": ex.raw, "password": "", "confirmToken": pv.data()["confirmToken"]})
	copyID := importCopyID(t, r)
	m := readJSON(t, dotPath)
	if jsonOf(t, m["requestedIds"]) != `["quad9"]` || jsonOf(t, m["selectedIds"]) != `["quad9"]` {
		t.Fatalf("selection not restored: %v", m)
	}
	if managedID(m) != "cloudflare-secondary" || jsonOf(t, m["effectiveIds"]) != `["cloudflare-secondary"]` {
		t.Fatalf("receipts replaced by the import: %v", m)
	}
	// DoT apply after the import (receipts only): undo restores the selection, keeps receipts
	setManaged("quad9")
	if u := undo(t, copyID); u.code != 200 {
		t.Fatalf("undo %d %s", u.code, u.raw)
	}
	m = readJSON(t, dotPath)
	if jsonOf(t, m["requestedIds"]) != `["cloudflare-primary"]` {
		t.Fatalf("selection not undone: %v", m)
	}
	if managedID(m) != "quad9" || jsonOf(t, m["effectiveIds"]) != `["quad9"]` {
		t.Fatalf("receipts lost by undo: %v", m)
	}
}

// dotReceipt is a complete config.managed[] entry for a catalog preset.
func dotReceipt(id string) map[string]any {
	addr := map[string]string{"cloudflare-primary": "1.1.1.1", "cloudflare-secondary": "1.0.0.1", "quad9": "9.9.9.9"}[id]
	sni := map[string]string{"cloudflare-primary": "cloudflare-dns.com", "cloudflare-secondary": "cloudflare-dns.com", "quad9": "dns.quad9.net"}[id]
	return map[string]any{"receiptSchemaVersion": 1, "id": id, "provider": "p", "address": addr, "portRaw": "853", "sni": sni,
		"spki": "", "interface": "", "domain": "", "on": "", "observedAt": compat.Now(), "effectivePort": 853, "portState": "explicit",
		"writeProtocolSha256": strings.Repeat("a", 64),
		"catalogIdentity":     map[string]any{"address": addr, "effectivePort": 853, "sni": sni},
		"selector":            map[string]any{"address": addr, "effectivePort": 853}}
}

func TestBackupUndoRefusedWhenSelectionChangedAfterImport(t *testing.T) {
	w := newWorld(t)
	w.dot(t, []string{"quad9"}, false)
	ex := do(t, "POST", "/api/broray/backup-export.cgi", map[string]any{"encrypt": false, "acknowledgeUnencrypted": true})
	w.dot(t, []string{"cloudflare-primary"}, false)
	copyID := importCopyID(t, importFile(t, ex.raw, ""))
	dotPath := w.d.Path("routes", "dot", "config.json")
	dc := readJSON(t, dotPath)
	dc["requestedIds"], dc["selectedIds"] = []any{"google-primary"}, []any{"google-primary"}
	if err := compat.WriteJSON(dotPath, dc); err != nil {
		t.Fatal(err)
	}
	if u := undo(t, copyID); u.code != 409 || u.errCode() != "BACKUP_UNDO_STATE_CHANGED" {
		t.Fatalf("undo %d %s", u.code, u.raw)
	}
	if m := readJSON(t, dotPath); jsonOf(t, m["requestedIds"]) != `["google-primary"]` {
		t.Fatalf("refused undo changed the selection: %v", m)
	}
}

package dns

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// Constants of lib/routes-dot.sh.
const (
	maxServers          = 8
	testTTL             = 600
	writeCandidate      = "3.0.0-r15c16"
	writeProtocolSHA256 = "dc8501856b27260738828e9959e10512496410ff44adfceaa8564056bed1ddfa"
	autoInterval        = 300
	statusCacheSeconds  = 30
)

// catEntry is one immutable catalog preset (RD:117-131). Order is significant.
type catEntry struct {
	ID       string
	Provider string
	Name     string
	Address  string
	SNI      string
}

var catalog = []catEntry{
	{"google-primary", "Google", "Google 8.8.8.8", "8.8.8.8", "dns.google"},
	{"google-secondary", "Google", "Google 8.8.4.4", "8.8.4.4", "dns.google"},
	{"yandex-primary", "Yandex DNS", "Yandex DNS 77.88.8.8", "77.88.8.8", "common.dot.dns.yandex.net"},
	{"cloudflare-primary", "Cloudflare", "Cloudflare 1.1.1.1", "1.1.1.1", "cloudflare-dns.com"},
	{"cloudflare-secondary", "Cloudflare", "Cloudflare 1.0.0.1", "1.0.0.1", "cloudflare-dns.com"},
	{"quad9", "Quad9", "Quad9 9.9.9.9", "9.9.9.9", "dns.quad9.net"},
	{"adguard-primary", "AdGuard DNS", "AdGuard 94.140.14.14", "94.140.14.14", "dns.adguard-dns.com"},
	{"adguard-secondary", "AdGuard DNS", "AdGuard 94.140.15.15", "94.140.15.15", "dns.adguard-dns.com"},
}

func findCatalog(id string) (catEntry, bool) {
	for _, c := range catalog {
		if c.ID == id {
			return c, true
		}
	}
	return catEntry{}, false
}

// endpoint is the catalog entry as a record (spki/interface/domain empty, port 853).
func (c catEntry) endpoint() liveRec {
	return liveRec{Address: c.Address, EffectivePort: 853, PortRaw: "853", PortState: "explicit", SNI: c.SNI}
}

func (c catEntry) object() map[string]any {
	return map[string]any{"id": c.ID, "provider": c.Provider, "name": c.Name, "address": c.Address, "port": 853,
		"effectivePort": 853, "sni": c.SNI, "spki": "", "on": "", "interface": "", "domain": ""}
}

func (c catEntry) entryJSON() map[string]any {
	return map[string]any{"id": c.ID, "address": c.Address, "effectivePort": 853, "sni": c.SNI, "spki": "", "interface": "", "domain": ""}
}

// ---------- paths ----------

func (s *service) dotDir() string     { return s.d.Path("routes", "dot") }
func (s *service) configPath() string { return filepath.Join(s.dotDir(), "config.json") }
func (s *service) statePath() string  { return filepath.Join(s.dotDir(), "state.json") }
func (s *service) autoPath() string   { return filepath.Join(s.dotDir(), "auto-check.json") }
func (s *service) markerPath() string {
	return filepath.Join(s.dotDir(), "transaction-recovery-required.json")
}
func (s *service) cachePath() string { return s.d.RunPath("dot-status-cache.json") }
func (s *service) automationPath() string {
	return s.d.Path("state", "background-automation.json")
}

// ---------- errors ----------

// dotErr is a CLI error BRORAY_ERROR:<code>:<message> + details mapped to HTTP (DC:81-128).
func dotErr(code, message, details string) error {
	return &dotError{code, message, details}
}

type dotError struct{ Code, Message, Details string }

func (e *dotError) Error() string { return e.Code + ": " + e.Message }

func dotStatus(code string) int {
	switch code {
	case "ROUTES_OPERATION_BUSY", "DOT_TEST_REQUIRED", "DOT_LIMIT_EXCEEDED", "DOT_RECOVERY_REQUIRED", "DOT_PHYSICAL_WRITE_PROTOCOL_REQUIRED",
		"DOT_OBSERVATION_UNDERDETERMINED", "DOT_DELETE_AUTHORITY_REFUSED", "DOT_DELETE_CONFIRMATION_STALE", "DOT_SELECTOR_CONFLICT":
		return 409
	case "NDMC_UNAVAILABLE", "DEPENDENCY_MISSING", "MODULE_UNAVAILABLE", "STORAGE_UNAVAILABLE", "CONFIG_INVALID", "CONFIG_MIGRATION_FAILED",
		"STATE_INVALID", "DOT_CATALOG_INVALID", "DOT_TEST_FAILED":
		return 500
	case "KEENETIC_UNAVAILABLE", "KEENETIC_RUNTIME_UNAVAILABLE", "DOT_APPLY_FAILED", "DOT_DELETE_FAILED":
		return 502
	}
	return 400
}

// ---------- JSON helpers ----------

func readMap(path string) (map[string]any, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, errors.New("not a JSON object")
	}
	return m, nil
}

func nonEmptyFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func numIs(v any, want float64) bool { f, ok := num(v); return ok && f == want }

func str(v any) (string, bool) { s, ok := v.(string); return s, ok }

func strOr(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

func stringList(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func anyList(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

func strSlice(ids []string) []any {
	out := make([]any, 0, len(ids))
	for _, s := range ids {
		out = append(out, s)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func uniqueSorted(ids []string) []string {
	m := map[string]bool{}
	var out []string
	for _, s := range ids {
		if !m[s] {
			m[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

func writeMap(path string, m map[string]any) error { return compat.WriteJSON(path, m) }

func stamp(t time.Time) string { return compat.Stamp(t) }

// ---------- receipts ----------

// receipt is a complete config.managed[] entry (B.R6).
type receipt struct {
	ID        string
	Provider  string
	Address   string
	PortRaw   string
	Port      int
	PortState string
	SNI       string
	SPKI      string
	Interface string
	Domain    string
	ProtoSHA  string
}

func (r receipt) rec() liveRec {
	return liveRec{Address: r.Address, PortRaw: r.PortRaw, EffectivePort: r.Port, PortState: r.PortState, SNI: r.SNI,
		SPKI: r.SPKI, On: r.Interface, Interface: r.Interface, Domain: r.Domain}
}

// parseReceipt applies complete_receipt (RD:300-319).
func parseReceipt(v any) (receipt, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return receipt{}, false
	}
	var r receipt
	if !numIs(m["receiptSchemaVersion"], 1) {
		return r, false
	}
	strs := map[string]*string{"id": &r.ID, "provider": &r.Provider, "address": &r.Address, "portRaw": &r.PortRaw, "sni": &r.SNI,
		"spki": &r.SPKI, "interface": &r.Interface, "domain": &r.Domain, "writeProtocolSha256": &r.ProtoSHA}
	for k, p := range strs {
		s, ok := m[k].(string)
		if !ok {
			return r, false
		}
		*p = s
	}
	on, ok1 := m["on"].(string)
	obsAt, ok2 := m["observedAt"].(string)
	_ = obsAt
	if !ok1 || !ok2 || on != r.Interface || r.ID == "" || r.Address == "" {
		return r, false
	}
	if !numIs(m["effectivePort"], 853) {
		return r, false
	}
	r.Port = 853
	ps, _ := m["portState"].(string)
	if ps != "explicit" && ps != "omitted" {
		return r, false
	}
	r.PortState = ps
	if r.SPKI != "" || r.Interface != "" || r.Domain != "" || len(r.ProtoSHA) != 64 {
		return r, false
	}
	ci, ok := m["catalogIdentity"].(map[string]any)
	if !ok || len(ci) != 3 || ci["address"] != r.Address || !numIs(ci["effectivePort"], 853) || ci["sni"] != r.SNI {
		return r, false
	}
	sel, ok := m["selector"].(map[string]any)
	if !ok || len(sel) != 2 || sel["address"] != r.Address || !numIs(sel["effectivePort"], 853) {
		return r, false
	}
	c, ok := findCatalog(r.ID)
	if !ok || c.Address != r.Address || c.SNI != r.SNI {
		return r, false
	}
	return r, true
}

func receiptObject(id, provider string, live liveRec, at string) map[string]any {
	return map[string]any{"receiptSchemaVersion": 1, "id": id, "provider": provider, "address": live.Address,
		"portRaw": live.PortRaw, "effectivePort": live.EffectivePort, "portState": live.PortState, "sni": live.SNI,
		"spki": live.SPKI, "on": live.Interface, "interface": live.Interface, "domain": live.Domain,
		"catalogIdentity": map[string]any{"address": live.Address, "effectivePort": live.EffectivePort, "sni": live.SNI},
		"selector":        map[string]any{"address": live.Address, "effectivePort": live.EffectivePort},
		"observedAt":      at, "writeProtocolSha256": writeProtocolSHA256}
}

// ---------- config / state ----------

type files struct {
	config   map[string]any
	state    map[string]any
	receipts []receipt
	managed  []any // raw receipts as stored
}

func (f files) requested() []string {
	if ids, ok := stringList(f.config["requestedIds"]); ok {
		return ids
	}
	if ids, ok := stringList(f.config["selectedIds"]); ok {
		return ids
	}
	return []string{}
}

// ensureFiles is broray_dot_ensure_files (RD:261-338): create defaults,
// migrate schema 1/2, validate schema 3 and receipts, validate state.
func (s *service) ensureFiles() (files, error) {
	if err := os.MkdirAll(s.dotDir(), 0o700); err != nil {
		return files{}, dotErr("STORAGE_UNAVAILABLE", "Не удалось подготовить хранилище DNS-over-TLS.", "")
	}
	_ = os.MkdirAll(s.d.Root.Tmp(), 0o700)
	_ = os.MkdirAll(s.d.Root.Run(), 0o700)
	if !nonEmptyFile(s.configPath()) {
		def := []any{"google-primary", "cloudflare-primary", "quad9"}
		if err := writeMap(s.configPath(), map[string]any{"schemaVersion": 3, "requestedIds": def, "selectedIds": append([]any(nil), def...),
			"effectiveIds": []any{}, "migrationState": "none", "managed": []any{}, "quarantinedReceipts": []any{}, "updatedAt": nil}); err != nil {
			return files{}, errors.New("config write failed")
		}
	}
	if !nonEmptyFile(s.statePath()) {
		if err := writeMap(s.statePath(), map[string]any{"schemaVersion": 1, "tests": []any{}, "lastTestedAt": nil, "lastAppliedAt": nil,
			"lastDeletedAt": nil, "lastOperation": nil, "lastError": nil, "updatedAt": nil}); err != nil {
			return files{}, errors.New("state write failed")
		}
	}
	cfg, err := readMap(s.configPath())
	schema := 0.0
	if err == nil {
		schema, _ = num(cfg["schemaVersion"])
	}
	switch {
	case err == nil && (schema == 1 || schema == 2):
		if err := s.migrateConfig(cfg, int(schema)); err != nil {
			return files{}, dotErr("CONFIG_MIGRATION_FAILED", "Не удалось безопасно обновить схему конфигурации DNS-over-TLS.", "")
		}
		cfg, err = readMap(s.configPath())
	case err == nil && schema == 3:
	default:
		return files{}, dotErr("CONFIG_INVALID", "Конфигурация DNS-over-TLS повреждена.", "")
	}
	invalid := dotErr("CONFIG_INVALID", "Конфигурация DNS-over-TLS повреждена.", "")
	if err != nil || !numIs(cfg["schemaVersion"], 3) {
		return files{}, invalid
	}
	req, ok1 := cfg["requestedIds"].([]any)
	sel, ok2 := cfg["selectedIds"].([]any)
	_, ok3 := cfg["effectiveIds"].([]any)
	managed, ok4 := cfg["managed"].([]any)
	_, ok5 := cfg["quarantinedReceipts"].([]any)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !jsonEqual(req, sel) {
		return files{}, invalid
	}
	if ms, present := cfg["migrationState"]; present && ms != nil {
		if _, ok := ms.(string); !ok {
			return files{}, invalid
		}
	}
	var receipts []receipt
	for _, m := range managed {
		r, ok := parseReceipt(m)
		if !ok {
			return files{}, invalid
		}
		receipts = append(receipts, r)
	}
	st, err := readMap(s.statePath())
	if err != nil || !numIs(st["schemaVersion"], 1) {
		return files{}, dotErr("STATE_INVALID", "Состояние DNS-over-TLS повреждено.", "")
	}
	if _, ok := st["tests"].([]any); !ok {
		return files{}, dotErr("STATE_INVALID", "Состояние DNS-over-TLS повреждено.", "")
	}
	return files{config: cfg, state: st, receipts: receipts, managed: managed}, nil
}

func jsonEqual(a, b any) bool {
	x, err1 := compat.Canonical(a)
	y, err2 := compat.Canonical(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// migrateConfig is broray_dot_migrate_config (RD:206-259).
func (s *service) migrateConfig(old map[string]any, oldSchema int) error {
	var requested any = []any{}
	if v, ok := old["requestedIds"]; ok && v != nil && v != false {
		requested = v
	} else if v, ok := old["selectedIds"]; ok && v != nil && v != false {
		requested = v
	}
	reqList, ok := requested.([]any)
	if !ok {
		return errors.New("requestedIds not an array")
	}
	at := stamp(s.now())
	quarantined := []any{}
	if q, ok := old["quarantinedReceipts"].([]any); ok {
		quarantined = append(quarantined, q...)
	} else if old["quarantinedReceipts"] != nil {
		return errors.New("quarantinedReceipts not an array")
	}
	if m, ok := old["managed"].([]any); ok {
		for _, r := range m {
			quarantined = append(quarantined, map[string]any{"reason": "legacy-incomplete-receipt", "sourceSchemaVersion": oldSchema,
				"quarantinedAt": at, "receipt": r})
		}
	}
	state := "none"
	if len(reqList) > maxServers {
		state = "selection-over-limit"
	} else if len(quarantined) > 0 {
		state = "legacy-receipts-quarantined"
	}
	out := map[string]any{}
	for k, v := range old {
		out[k] = v
	}
	out["schemaVersion"] = 3
	out["requestedIds"] = reqList
	out["selectedIds"] = append([]any(nil), reqList...)
	out["effectiveIds"] = []any{}
	out["migrationState"] = state
	out["managed"] = []any{}
	out["quarantinedReceipts"] = quarantined
	if _, ok := out["updatedAt"]; !ok {
		out["updatedAt"] = nil
	}
	return writeMap(s.configPath(), out)
}

// ---------- tests stored in state ----------

type testRow struct {
	raw map[string]any
	rec liveRec // address, effectivePort, sni, spki, interface, domain
	id  string
	ok  bool
	ep  float64 // testedEpoch; 0 when absent or not a number
	num bool
}

func testRows(state map[string]any) []testRow {
	var out []testRow
	for _, t := range anyList(state["tests"]) {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		r := testRow{raw: m}
		r.id, _ = str(m["id"])
		r.rec.Address, _ = str(m["address"])
		if p, ok := num(m["effectivePort"]); ok {
			r.rec.EffectivePort = int(p)
		} else {
			r.rec.EffectivePort = -1
		}
		r.rec.SNI, _ = str(m["sni"])
		r.rec.SPKI = strOr(m["spki"], "\x00")
		r.rec.Interface = strOr(m["interface"], "\x00")
		r.rec.Domain = strOr(m["domain"], "\x00")
		r.ok = m["ok"] == true
		r.ep, r.num = num(m["testedEpoch"])
		out = append(out, r)
	}
	return out
}

func (t testRow) matches(c catEntry) bool {
	return t.id == c.ID && t.rec.Address == c.Address && t.rec.EffectivePort == 853 && t.rec.SNI == c.SNI &&
		t.rec.SPKI == "" && t.rec.Interface == "" && t.rec.Domain == ""
}

func (t testRow) fresh(now int64, ttl int64) bool {
	if !t.ok || !t.num || t.ep <= 0 {
		return false
	}
	age := float64(now) - t.ep
	return age >= 0 && age <= float64(ttl)
}

// testsFreshAndOK is broray_dot_tests_fresh_and_ok (RD:691-705).
func testsFreshAndOK(state map[string]any, ids []string, now int64) bool {
	rows := testRows(state)
	for _, id := range ids {
		c, _ := findCatalog(id)
		found := false
		for _, t := range rows {
			if t.matches(c) && t.fresh(now, testTTL) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ---------- request validation ----------

// validateRequest is broray_dot_validate_request (RD:642-663).
func validateRequest(m map[string]any) ([]string, error) {
	bad := dotErr("REQUEST_INVALID", "Выбран некорректный список DNS-over-TLS серверов.", "")
	ids, ok := stringList(m["serverIds"])
	if !ok {
		// jq: non-string ids can never match a preset id
		return nil, bad
	}
	if len(ids) < 1 || len(ids) > maxServers || len(uniqueSorted(ids)) != len(ids) {
		return nil, bad
	}
	for _, id := range ids {
		if _, ok := findCatalog(id); !ok {
			return nil, bad
		}
	}
	if v, present := m["allowUntested"]; present && v != nil && v != false {
		return nil, bad
	}
	return ids, nil
}

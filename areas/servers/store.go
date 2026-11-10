package servers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

// ---------- paths (3.x /opt/broray/... → Deps.Path / RunPath) ----------

// ServersDir is /opt/broray/servers.
func ServersDir(d *compat.Deps) string { return d.Path("servers") }

// RecordPath is /opt/broray/servers/<id>.json.
func RecordPath(d *compat.Deps, id string) string { return filepath.Join(ServersDir(d), id+".json") }

// QualityPath is /opt/broray/run/server-quality/<id>.json.
func QualityPath(d *compat.Deps, id string) string { return d.RunPath("server-quality", id+".json") }

// ActiveFile is /opt/broray/config/active-server.
func ActiveFile(d *compat.Deps) string { return d.Path("config", "active-server") }

func autoSwitchFile(d *compat.Deps) string {
	return d.Path("config", "system", "server-auto-switch.json")
}
func settingsFile(d *compat.Deps) string { return d.Path("config", "system", "settings.json") }
func stateFile(d *compat.Deps) string    { return d.RunPath("server-auto-switch-state.json") }

// ---------- records ----------

// ErrNotFound is returned by ReadRecord for a missing record.
var ErrNotFound = errors.New("server record not found")

// ReadRecord reads servers/<id>.json (a symlink or non-regular file is refused).
func ReadRecord(d *compat.Deps, id string) (Record, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid server id")
	}
	return readRecordFile(RecordPath(d, id))
}

func readRecordFile(path string) (Record, error) {
	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("server record is not a regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, ok := decodeObject(b)
	if !ok {
		return nil, errors.New("server record is not a JSON object")
	}
	return m, nil
}

// RecordExists is broray_server_exists ([ -f file ]).
func RecordExists(d *compat.Deps, id string) bool {
	if !ValidID(id) {
		return false
	}
	st, err := os.Stat(RecordPath(d, id))
	return err == nil && st.Mode().IsRegular()
}

// WriteRecord atomically writes servers/<rec.id>.json (0600). The record
// must be valid (Validate) and its id a valid file stem.
func WriteRecord(d *compat.Deps, rec Record) error {
	id, _ := rec["id"].(string)
	if !ValidID(id) {
		return errors.New("invalid server id")
	}
	if err := Validate(rec, RecordPath(d, id)); err != nil {
		return err
	}
	if err := os.MkdirAll(ServersDir(d), 0o700); err != nil {
		return err
	}
	return compat.WriteJSON(RecordPath(d, id), normalize(rec))
}

// DeleteRecord removes servers/<id>.json and its quality record
// (rm -f semantics: missing files are not an error).
func DeleteRecord(d *compat.Deps, id string) error {
	if !ValidID(id) {
		return errors.New("invalid server id")
	}
	if err := os.Remove(RecordPath(d, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(QualityPath(d, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RecordIDs lists the file stems of servers/*.json in glob (byte) order;
// files starting with "." are skipped like the shell glob.
func RecordIDs(d *compat.Deps) ([]string, error) {
	entries, err := os.ReadDir(ServersDir(d))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") || !strings.HasSuffix(n, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(n, ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}

// ListRecords reads every servers/*.json in glob order.
func ListRecords(d *compat.Deps) ([]Record, error) {
	ids, err := RecordIDs(d)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(ids))
	for _, id := range ids {
		st, err := os.Stat(RecordPath(d, id))
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		rec, err := readRecordFile(RecordPath(d, id))
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// ActiveID is the first line of config/active-server ("" if none).
func ActiveID(d *compat.Deps) string {
	b, err := os.ReadFile(ActiveFile(d))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return line
}

func setActiveID(d *compat.Deps, id string) error {
	if err := os.MkdirAll(filepath.Dir(ActiveFile(d)), 0o700); err != nil {
		return err
	}
	return platform.WriteFileAtomic(ActiveFile(d), []byte(id+"\n"), 0o600)
}

// NewManualID is broray_server_generate_id manual:
// manual-<YYYYmmddHHMMSS>-<8 lowercase hex>, 32 attempts until unused.
func NewManualID(d *compat.Deps) (string, error) {
	ts := time.Now().Format("20060102150405")
	for i := 0; i < 32; i++ {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return "", die("не удалось получить случайный идентификатор сервера")
		}
		id := "manual-" + ts + "-" + hex.EncodeToString(b)
		if _, err := os.Lstat(RecordPath(d, id)); os.IsNotExist(err) {
			return id, nil
		}
	}
	return "", die("не удалось выделить уникальный идентификатор сервера")
}

// MaskRecord is broray_server_mask_json: secrets removed, REALITY public key
// shortened.
func MaskRecord(rec Record) Record {
	out := normalize(rec)
	// Refresh presentation of old metadata without migrating saved profiles.
	if warnings, ok := out["importWarnings"].([]any); ok {
		for i, warning := range warnings {
			if text, ok := warning.(string); ok && text == legacyMLKEMMetadataWarning {
				warnings[i] = mlkemMetadataWarning
			}
		}
	}
	for _, k := range []string{"uri", "uuid", "password", "auth", "privateKey", "amneziawg", "wireguard", "streamOptions", "xdrive", "masque"} {
		delete(out, k)
	}
	if c, ok := out["connection"].(map[string]any); ok {
		for _, k := range []string{"uuid", "password", "auth", "privateKey"} {
			delete(c, k)
		}
	}
	if h, ok := out["hysteria"].(map[string]any); ok {
		delete(h, "obfsPassword")
		delete(h, "finalMask")
	}
	if transport, ok := out["transport"].(map[string]any); ok {
		delete(transport, "finalMask")
		delete(transport, "extra")
		delete(transport, "header")
	}
	if xhttp, ok := out["xhttp"].(map[string]any); ok {
		delete(xhttp, "extra")
	}
	if r, ok := out["reality"].(map[string]any); ok && truthy(r["publicKey"]) {
		pk := r["publicKey"]
		if l, ok := jqLength(pk); ok && l > 12 {
			if s, ok := pk.(string); ok {
				rs := []rune(s)
				r["publicKey"] = string(rs[:6]) + "…" + string(rs[len(rs)-4:])
			}
		} else {
			r["publicKey"] = "••••••"
		}
	}
	return out
}

// ---------- quality records ----------

// readQualityRaw returns the quality file bytes and mtime (ok=false if no regular file).
func readQualityRaw(d *compat.Deps, id string) ([]byte, time.Time, bool) {
	p := QualityPath(d, id)
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() {
		return nil, time.Time{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, time.Time{}, false
	}
	return b, st.ModTime(), true
}

func writeQuality(d *compat.Deps, id string, q map[string]any) error {
	if err := os.MkdirAll(d.RunPath("server-quality"), 0o700); err != nil {
		return err
	}
	return compat.WriteJSON(QualityPath(d, id), q)
}

// fileSHA256 is `sha256sum file | cut -d' ' -f1`.
func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return compat.SHA256Hex(b), nil
}

func readObjectFile(path string) (map[string]any, bool) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return decodeObject(bytes.TrimSpace(b))
}

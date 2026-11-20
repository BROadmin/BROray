package servers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"golang.org/x/sys/unix"
)

type reservePreferences struct {
	SchemaVersion   int               `json:"schemaVersion"`
	Enabled         bool              `json:"enabled"`
	PrimaryServerID string            `json:"primaryServerId"`
	BackupServerIDs []string          `json:"backupServerIds"`
	Emergency       *reserveEmergency `json:"emergency,omitempty"`
}

type reserveEmergency struct {
	ServerID    string `json:"serverId"`
	Fingerprint string `json:"fingerprint"`
}

type reservePreferencesSnapshot struct {
	reservePreferences
	Revision   string `json:"revision"`
	Configured bool   `json:"configured"`
	Exists     bool   `json:"-"`
}

var errReservePreferences = errors.New("RESERVE_SETTINGS_INVALID")

func reservePreferencesRevision(data []byte, exists bool) string {
	prefix := "reserve-absent\x00"
	if exists {
		prefix = "reserve-file\x00"
	}
	sum := sha256.Sum256(append([]byte(prefix), data...))
	return hex.EncodeToString(sum[:])
}

func reserveRoster(m map[string]any) (string, []string, error) {
	primary, ok := m["primaryServerId"].(string)
	if !ok || (primary != "" && !BackupIDValid(primary)) {
		return "", nil, errReservePreferences
	}
	raw, ok := m["backupServerIds"].([]any)
	if !ok || len(raw) > 2 {
		return "", nil, errReservePreferences
	}
	backups := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, value := range raw {
		id, ok := value.(string)
		if !ok || !BackupIDValid(id) || id == primary || seen[id] {
			return "", nil, errReservePreferences
		}
		seen[id] = true
		backups = append(backups, id)
	}
	return primary, backups, nil
}

func decodeReservePreferences(data []byte) (reservePreferences, error) {
	v, err := strictjson.Decode(data)
	if err != nil {
		return reservePreferences{}, errReservePreferences
	}
	m, ok := v.(map[string]any)
	if !ok || len(m) < 4 || len(m) > 5 {
		return reservePreferences{}, errReservePreferences
	}
	version, ok := m["schemaVersion"].(json.Number)
	if !ok || version != "1" {
		return reservePreferences{}, errReservePreferences
	}
	enabled, ok := m["enabled"].(bool)
	if !ok {
		return reservePreferences{}, errReservePreferences
	}
	primary, backups, err := reserveRoster(m)
	if err != nil {
		return reservePreferences{}, err
	}
	if enabled && (primary == "" || len(backups) == 0) {
		return reservePreferences{}, errReservePreferences
	}
	prefs := reservePreferences{SchemaVersion: 1, Enabled: enabled, PrimaryServerID: primary, BackupServerIDs: backups}
	if len(m) == 5 {
		obj, ok := m["emergency"].(map[string]any)
		id, idOK := obj["serverId"].(string)
		hash, hashOK := obj["fingerprint"].(string)
		if !enabled || !ok || len(obj) != 2 || !idOK || !BackupIDValid(id) || !hashOK || len(hash) != 64 ||
			id == primary {
			return reservePreferences{}, errReservePreferences
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return reservePreferences{}, errReservePreferences
		}
		for _, backup := range backups {
			if id == backup {
				return reservePreferences{}, errReservePreferences
			}
		}
		prefs.Emergency = &reserveEmergency{ServerID: id, Fingerprint: hash}
	}
	return prefs, nil
}

func readReservePreferences(d *compat.Deps) (reservePreferencesSnapshot, error) {
	data, err := readReservePreferencesBytes(reservePreferencesFile(d))
	if errors.Is(err, os.ErrNotExist) {
		return reservePreferencesSnapshot{reservePreferences: reservePreferences{SchemaVersion: 1, BackupServerIDs: []string{}}, Revision: reservePreferencesRevision(nil, false)}, nil
	}
	if err != nil {
		return reservePreferencesSnapshot{}, errReservePreferences
	}
	prefs, err := decodeReservePreferences(data)
	if err != nil {
		return reservePreferencesSnapshot{}, err
	}
	return reservePreferencesSnapshot{prefs, reservePreferencesRevision(data, true), prefs.PrimaryServerID != "" && len(prefs.BackupServerIDs) > 0, true}, nil
}

func readReservePreferencesBytes(path string) ([]byte, error) {
	parent, err := platform.OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 4096 {
		return nil, errReservePreferences
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 {
		return nil, errReservePreferences
	}
	return data, nil
}

func reservePreferencesFile(d *compat.Deps) string {
	return d.Path("config", "system", "server-reserve.json")
}

func (s *svc) handleReservePreferences(c *cgi.Req) (any, error) {
	invalid := cgi.E(400, "REQUEST_INVALID", "Некорректный состав резервирования.")
	storageInvalid := cgi.E(500, "CONFIG_INVALID", "Настройки резервирования недоступны; сохранённые данные не изменены.")
	if c.R.Method == "GET" {
		snapshot, err := readReservePreferences(s.d)
		if err != nil {
			return nil, storageInvalid
		}
		return snapshot, nil
	}
	if c.Header("X-BROray-Request") != "operations" {
		return nil, cgi.E(403, "CSRF_REJECTED", "Обновите страницу и повторите действие.")
	}
	if !compat.OriginAllowed(c.R) {
		return nil, cgi.E(403, "ORIGIN_REJECTED", "Источник запроса не подтверждён.")
	}
	if err := bodyCheck(c); err != nil {
		return nil, err
	}
	v, err := strictjson.Decode(c.Body())
	if err != nil {
		return nil, invalid
	}
	change, ok := v.(map[string]any)
	if !ok {
		return nil, invalid
	}
	for key := range change {
		if key != "expectedRevision" && key != "primaryServerId" && key != "backupServerIds" {
			return nil, invalid
		}
	}
	revision, ok := change["expectedRevision"].(string)
	if !ok || len(revision) != 64 {
		return nil, invalid
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return nil, invalid
	}
	if len(change) < 2 {
		return nil, invalid
	}
	release, ok := s.d.Lock.TryAcquire("servers", "servers:reserve-settings", "")
	if !ok {
		return nil, errBusy
	}
	defer release()
	current, err := readReservePreferences(s.d)
	if err != nil {
		return nil, storageInvalid
	}
	if revision != current.Revision {
		return nil, cgi.E(409, "SETTINGS_CHANGED", "Настройки изменились. Обновите состояние.")
	}
	if current.Enabled {
		return nil, cgi.E(409, "RESERVE_MODE_ACTIVE", "Изменение состава работающего резервирования требует операции переключения.")
	}
	backups := make([]any, len(current.BackupServerIDs))
	for i, id := range current.BackupServerIDs {
		backups[i] = id
	}
	merged := map[string]any{"primaryServerId": current.PrimaryServerID, "backupServerIds": backups}
	for key, value := range change {
		if key != "expectedRevision" {
			merged[key] = value
		}
	}
	primary, ids, err := reserveRoster(merged)
	if err != nil {
		return nil, invalid
	}
	for _, id := range append([]string{primary}, ids...) {
		if id == "" {
			continue
		}
		raw, err := platform.ReadRegularNoFollow(RecordPath(s.d, id))
		if err != nil {
			return nil, cgi.E(400, "SERVER_NOT_FOUND", "Выбранный сервер недоступен.")
		}
		record, err := DecodeBackupRecord(raw, "reserve")
		if err != nil || record["id"] != id || requireConnectionSupport(record) != nil {
			return nil, cgi.E(400, "SERVER_UNSUPPORTED", "Выбранный сервер не поддерживает подключение.")
		}
	}
	next := reservePreferences{SchemaVersion: 1, PrimaryServerID: primary, BackupServerIDs: ids}
	if reflect.DeepEqual(next, current.reservePreferences) {
		return current, nil
	}
	if primary != "" && len(ids) > 0 || len(ids) > 1 {
		catalog, err := ListRecords(s.d)
		if err != nil {
			return nil, storageInvalid
		}
		groups := newPresentationGroups()
		for _, rec := range catalog {
			groups.add(asString(rec["id"]), rec)
		}
		chosen := map[string]bool{}
		for _, id := range append([]string{primary}, ids...) {
			if id == "" {
				continue
			}
			group := groups.root(id)
			if chosen[group] {
				return nil, cgi.E(400, "RESERVE_DUPLICATE_SERVER", "Выберите разные серверы для основного подключения и резерва.")
			}
			chosen[group] = true
		}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return nil, storageInvalid
	}
	if err := platform.WriteFileAtomicNoFollow(reservePreferencesFile(s.d), data, 0600); err != nil {
		return nil, cgi.E(500, "CONFIG_SAVE_UNCONFIRMED", "Результат сохранения не подтверждён. Обновите состояние.")
	}
	confirmed, err := readReservePreferences(s.d)
	if err != nil || !confirmed.Exists || !reflect.DeepEqual(confirmed.reservePreferences, next) {
		return nil, cgi.E(500, "CONFIG_SAVE_UNCONFIRMED", "Результат сохранения не подтверждён. Обновите состояние.")
	}
	return confirmed, nil
}

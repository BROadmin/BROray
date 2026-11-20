package servers

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

// Inspect only the new field's aliases/duplicates here. Legacy backup parts
// retain their existing size/schema contract; do not impose the small
// preference-document limit on an archive containing thousands of records.
func (s *svc) reserveBackupInput(raw []byte) (*reservePreferences, error) {
	current, err := readReservePreferences(s.d)
	if err != nil {
		return nil, backupInvalid("текущие настройки резервирования повреждены")
	}
	if current.Enabled {
		return nil, backupInvalid("восстановление при работающем резервировании требует проверки состояния VPN")
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, backupInvalid("раздел серверов повреждён")
	}
	var found *reservePreferences
	seen := false
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, backupInvalid("раздел серверов повреждён")
		}
		name, ok := key.(string)
		if !ok {
			return nil, backupInvalid("раздел серверов повреждён")
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, backupInvalid("раздел серверов повреждён")
		}
		if !strings.EqualFold(name, "reserve") {
			continue
		}
		if seen || name != "reserve" {
			return nil, backupInvalid("неоднозначные настройки резервирования")
		}
		seen = true
		if len(value) > 4096 {
			return nil, backupInvalid("настройки резервирования слишком велики")
		}
		prefs, err := decodeReservePreferences(value)
		if err != nil {
			return nil, backupInvalid("настройки резервирования повреждены")
		}
		if prefs.Enabled {
			return nil, backupInvalid("включение резервирования при восстановлении требует проверки состояния VPN")
		}
		found = &prefs
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') {
		return nil, backupInvalid("раздел серверов повреждён")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, backupInvalid("лишние данные в разделе серверов")
	}
	return found, nil
}

func (s *svc) stageReservePreferences(prefs *reservePreferences, st *compat.BackupStage, plan *compat.BackupPlan) error {
	if prefs == nil {
		return nil
	}
	for _, id := range append([]string{prefs.PrimaryServerID}, prefs.BackupServerIDs...) {
		if id == "" {
			continue
		}
		rec, err := ReadRecord(st.St, id)
		if err != nil || requireConnectionSupport(rec) != nil {
			return backupInvalid("сервер резервирования %q недоступен в итоговом составе", id)
		}
	}
	current, err := readReservePreferences(s.d)
	if err != nil {
		return backupInvalid("текущие настройки резервирования повреждены")
	}
	if reflect.DeepEqual(current.reservePreferences, *prefs) {
		return nil
	}
	plan.Add("setting", "reserve", "Состав резервирования VPN", "replace", "")
	if err := st.Take(compat.DataRel("config", "system", "server-reserve.json")); err != nil {
		return err
	}
	path := reservePreferencesFile(st.St)
	if err := platform.MkdirAllDurable(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomicNoFollow(path, data, 0600)
}

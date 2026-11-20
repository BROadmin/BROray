package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

func decodeScheduleObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	bad := errors.New("invalid schedule object")
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return nil, bad
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		t, err := d.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return nil, bad
		}
		if _, duplicate := m[key]; duplicate {
			return nil, bad
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, bad
		}
		m[key] = value
	}
	t, err = d.Token()
	if err != nil || t != json.Delim('}') || d.Decode(new(any)) != io.EOF {
		return nil, bad
	}
	return m, nil
}

func (e *engine) scheduleSettingsSnapshot() (schedSettings, bool) {
	defaults := schedSettings{MaxRemovalPercent: defaultRemovalPct}
	raw, err := platform.ReadRegularNoFollow(e.scheduleFile())
	if os.IsNotExist(err) {
		return defaults, true
	}
	if err != nil || len(raw) > 16384 {
		return defaults, false
	}
	m, err := decodeScheduleObject(raw)
	if err != nil {
		return defaults, false
	}
	var schema, interval, removal *int
	var auto *bool
	if json.Unmarshal(m["schemaVersion"], &schema) != nil || schema == nil || *schema != 1 ||
		json.Unmarshal(m["intervalHours"], &interval) != nil || interval == nil || !validInterval(*interval) ||
		json.Unmarshal(m["autoApply"], &auto) != nil || auto == nil {
		return defaults, false
	}
	s := schedSettings{IntervalHours: *interval, AutoApply: *auto, MaxRemovalPercent: defaultRemovalPct}
	if value, exists := m["maxRemovalPercent"]; exists {
		if json.Unmarshal(value, &removal) != nil || removal == nil || !validRemovalPct(*removal) {
			return defaults, false
		}
		s.MaxRemovalPercent = *removal
	}
	return s, true
}

func scheduleSettingsRevision(s schedSettings) string {
	revision, _ := compat.CanonicalSHA256(obj{"intervalHours": s.IntervalHours, "autoApply": s.AutoApply, "maxRemovalPercent": s.MaxRemovalPercent})
	return revision
}

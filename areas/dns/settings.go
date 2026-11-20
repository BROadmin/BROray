package dns

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Preserve enabled-only callers while allowing the new UI to reject stale
// writes. Do not accept duplicate/case-aliased keys or null as false.
func decodeAutoSettingsChange(body []byte) (enabled bool, revision string, err error) {
	bad := errors.New("invalid auto-settings request")
	d := json.NewDecoder(bytes.NewReader(body))
	opening, err := d.Token()
	if err != nil || opening != json.Delim('{') {
		return false, "", bad
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || (key != "enabled" && key != "expectedRevision") {
			return false, "", bad
		}
		seen[key] = true
		var value any
		if err := d.Decode(&value); err != nil {
			return false, "", bad
		}
		if key == "enabled" {
			enabled, ok = value.(bool)
		} else {
			revision, ok = value.(string)
			ok = ok && len(revision) == 64
		}
		if !ok {
			return false, "", bad
		}
	}
	closing, err := d.Token()
	if err != nil || closing != json.Delim('}') || !seen["enabled"] || d.Decode(new(any)) != io.EOF {
		return false, "", bad
	}
	return enabled, revision, nil
}

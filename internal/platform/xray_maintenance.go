package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// XrayMaintenanceBlocked is a fail-closed cross-area view of the persistent
// maintenance marker. Missing markers retain the behaviour of older releases.
func XrayMaintenanceBlocked(root Root) bool {
	path := filepath.Join(root.State(), "xray-maintenance.json")
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil || !st.Mode().IsRegular() || st.Size() > 16384 {
		return true
	}
	b, err := os.ReadFile(path)
	var state struct {
		SchemaVersion int    `json:"schemaVersion"`
		Status        string `json:"status"`
	}
	return err != nil || json.Unmarshal(b, &state) != nil || state.SchemaVersion != 1 || state.Status != "ready"
}

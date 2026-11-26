package servers

import (
	"bytes"
	"os"
	"testing"
)

func TestUnifiedReserveStatusReadsOrdinaryPolicy(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		name, raw string
		reserve   bool
		auto      any
	}{
		{"manual", `{"enabled":false}`, false, false},
		{"automatic", `{"enabled":true}`, false, true},
		{"reserve", `{"enabled":false}`, true, false},
		{"reserve-first", `{"enabled":true}`, true, true},
		{"legacy", `{"enabled":"true"}`, true, true},
		{"invalid", `{"enabled":"unexpected"}`, true, nil},
		{"malformed", `{"enabled":true,"enabled":false}`, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeReserveAutomatic(t, e, "a", "b", tc.reserve)
			path := autoSwitchFile(e.d)
			if err := os.WriteFile(path, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			r := call(t, "GET", "/api/servers/reserve-status.cgi", "", nil)
			data, ok := r.Body["data"].(map[string]any)
			if r.Code != 200 || !ok {
				t.Fatal("reserve status", r.Code, r.Raw)
			}
			auto, present := data["autoSwitchEnabled"]
			if !present || auto != tc.auto || get(data, "settings", "enabled") != tc.reserve {
				t.Fatalf("status mismatch: auto=%v present=%v expected=%v", auto, present, tc.auto)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, []byte(tc.raw)) {
				t.Fatal("status rewrote independent setting", err)
			}
		})
	}
}

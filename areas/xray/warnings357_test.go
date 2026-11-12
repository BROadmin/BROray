package xray

import (
	"encoding/json"
	"os"
	"testing"
)

func TestWarnings357TrialEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
		want                bool
	}{{"missing-hash", "", "", false}, {"stale-first", "wrong", "current", true}, {"both-stale", "wrong", "wrong", false}, {"current", "current", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, true)
			sum := fileSHA256(h.d.Root.XrayBinary())
			for i, name := range []string{"xray-reinstall-test.json", "xray-lifecycle-test.json"} {
				hash := tc.first
				if i == 1 {
					hash = tc.second
					if hash == "" {
						continue
					}
				}
				if hash == "current" {
					hash = sum
				}
				b, _ := json.Marshal(map[string]any{"success": true, "sha256": hash})
				if err := os.WriteFile(h.d.RunPath(name), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			d := h.call("GET", "/api/xray/diagnostics.cgi", "")
			if !d.Success {
				t.Fatal(d)
			}
			found := false
			for _, v := range d.Data["checks"].([]any) {
				m := v.(map[string]any)
				if m["id"] == "real-device-test" {
					found = true
					if (m["status"] == "ok") != tc.want {
						t.Fatal("trial does not describe current binary", m)
					}
				}
			}
			if !found {
				t.Fatal("test fixture missing check", d.Data["checks"])
			}
		})
	}
}

package xray

import (
	"os"
	"testing"
)

func TestUpdateCheckDev10Reservation(t *testing.T) {
	h := newHarness(t, true)
	fakeGitHub(t, []fakeRel{{tag: "v26.10.31", published: "2026-10-31T00:00:00Z", archive: zipWith(t, "26.10.31")}})
	for _, missing := range []bool{false, true} {
		if missing {
			if err := os.Remove(h.d.Root.XrayBinary()); err != nil {
				t.Fatal(err)
			}
		}
		var size int64
		if st, err := os.Stat(h.d.Root.XrayBinary()); err == nil {
			size = st.Size()
		}
		r := h.call("POST", "/api/xray/update-check.cgi", "{}")
		if r.Status != 200 {
			t.Fatal(r.Data)
		}
		need := int64(r.Data["storage"].(map[string]any)["requiredBytes"].(float64))
		if minimum := size + 8<<20; need < minimum {
			t.Errorf("missing=%v advertised budget %d is below actual install minimum %d", missing, need, minimum)
		}
	}
}

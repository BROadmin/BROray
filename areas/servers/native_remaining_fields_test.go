package servers

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestNativeRemainingClientBounds(t *testing.T) {
	check := func(t *testing.T, out map[string]any, valid bool) {
		t.Helper()
		if valid {
			assertNativeUnchanged(t, out)
			return
		}
		if _, err := ParseNativeOutbound(out, "bounds", "bounds", nil); err == nil {
			t.Fatal("invalid client bound accepted")
		}
	}
	for _, field := range []string{"concurrency", "xudpConcurrency"} {
		for _, tc := range []struct {
			name  string
			value any
			valid bool
		}{
			{"zero", 0, true}, {"max", 32767, true},
			{"negative", -1, false}, {"overflow", 32768, false},
			{"fraction", json.Number("1.5"), false}, {"null", nil, false},
			{"string", "1", false}, {"bool", true, false},
		} {
			t.Run("mux/"+field+"/"+tc.name, func(t *testing.T) {
				out := nativeTestOutbound("xdrive")
				out["mux"].(map[string]any)[field] = tc.value
				check(t, out, tc.valid)
			})
		}
	}
	for _, tc := range []struct {
		value any
		valid bool
	}{
		{"", true}, {"allow", true}, {"reject", true}, {"skip", false},
		{"unknown", false}, {"ALLOW", false}, {nil, false}, {false, false}, {1, false},
	} {
		t.Run(fmt.Sprintf("mux/udp443/%v", tc.value), func(t *testing.T) {
			out := nativeTestOutbound("xdrive")
			out["mux"].(map[string]any)["xudpProxyUDP443"] = tc.value
			check(t, out, tc.valid)
		})
	}
	for _, count := range []int{0, 64, 65} {
		t.Run(fmt.Sprintf("masque/remoteDNS/%d", count), func(t *testing.T) {
			out := nativeTestOutbound("masque")
			list := make([]any, count)
			for i := range list {
				list[i] = fmt.Sprintf("192.0.2.%d", i+1)
			}
			out["settings"].(map[string]any)["remoteDNS"] = list
			check(t, out, count <= 64)
		})
	}
}

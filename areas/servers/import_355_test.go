package servers

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestYour355Range(t *testing.T) {
	for _, v := range []any{"", "0", json.Number("0")} {
		if r, ok := rangePair(v); !ok || r != [2]float64{} {
			t.Errorf("zero range rejected: %v", v)
		}
	}
	for _, v := range []any{"-1", "2147483648", "1-0", " ", true, nil, -1, 0.5} {
		if _, ok := rangePair(v); ok {
			t.Errorf("invalid range accepted: %v", v)
		}
	}
}

func TestYour355NativeLevel(t *testing.T) {
	for _, level := range []any{0, 8, 255, 256, 4294967295, -1, 4294967296, 0.5, "8", true, nil} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			out := nativeTestOutbound("xdrive")
			user := get(out, "settings", "vnext").([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
			user["level"] = level
			before, _ := json.Marshal(out)
			r, err := ParseNativeOutbound(out, "test", "level-test", nil)
			valid := level == 0 || level == 8 || level == 255 || level == 256 || level == 4294967295
			if (err == nil) != valid {
				t.Fatalf("valid=%v err=%v", valid, err)
			}
			after, _ := json.Marshal(out)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("mutated caller")
			}
			if valid {
				if r["uuid"] != user["id"] || !reflect.DeepEqual(r["xdrive"], normalize(out)["streamSettings"].(map[string]any)["xdriveSettings"]) {
					t.Fatal("endpoint changed")
				}
				if !strings.Contains(fmt.Sprint(r["importWarnings"]), "политик") {
					t.Fatal("missing policy warning")
				}
				if _, ok := r["level"]; ok {
					t.Fatal("foreign level retained")
				}
				coverageRoundTrip(t, r, true)
			}
		})
	}
}

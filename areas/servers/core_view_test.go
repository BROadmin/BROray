package servers

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCoreCapabilityPublicView(t *testing.T) {
	for _, version := range []string{"26.10.31", "26.11.9"} {
		t.Run(version, func(t *testing.T) {
			e := newEnv(t)
			if err := os.WriteFile(e.d.Root.XrayBinary(), []byte("#!/bin/sh\necho 'Xray "+version+" (test)'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			r := parseNativeFixture(t, nativeTestOutbound("xdrive"))
			if err := WriteRecord(e.d, r); err != nil {
				t.Fatal(err)
			}
			summary, err := e.s.summary(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			details, err := e.s.details("native-test")
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(nativeTestOutbound("xdrive"))
			preview, err := e.s.preview(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			view := []map[string]any{details, preview}
			rows, ok := summary["servers"].([]any)
			if !ok {
				t.Fatal("summary has no servers")
			}
			view = append(view, rows[0].(map[string]any))
			for _, row := range view {
				support, _ := row["connectionSupport"].(map[string]any)
				if support["minimumCore"] != "26.11.9" {
					t.Error("minimum core missing")
				}
				if support["supported"] != (version == "26.11.9") {
					t.Error("public support disagrees with runtime")
				}
				b, _ := json.Marshal(row)
				if strings.Contains(string(b), "synthetic-private") {
					t.Error("private data in view")
				}
			}
		})
	}
}

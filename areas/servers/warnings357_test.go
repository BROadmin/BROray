package servers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWarnings357CoreEvidence(t *testing.T) {
	e := newEnv(t)
	r := parseNativeFixture(t, nativeTestOutbound("xdrive"))
	view := e.s.supportViewChecker(context.Background())(r)
	if view["supported"] != false || view["mode"] != "core-unverified" || strings.Contains(asString(view["message"]), "обновите") {
		t.Fatal("unknown version presented as old core", view)
	}
}

func TestSummaryImportWarningProjection(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		value      any
		present    bool
	}{
		{name: "missing", want: "null"},
		{name: "null", want: "null", present: true},
		{name: "empty array", want: "[]", value: []any{}, present: true},
		{name: "old warning", value: []any{legacyMLKEMMetadataWarning, "fixture", nil}, present: true},
		{name: "legacy scalar", want: `"fixture"`, value: "fixture", present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			id := e.addServer(t, "trojan://synthetic-private@example.test:443")
			r, err := ReadRecord(e.d, id)
			if err != nil {
				t.Fatal(err)
			}
			delete(r, "importWarnings")
			if tc.present {
				r["importWarnings"] = tc.value
			}
			if err := WriteRecord(e.d, r); err != nil {
				t.Fatal(err)
			}
			summary, err := e.s.summary(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			row := summary["servers"].([]any)[0].(map[string]any)
			warnings, err := json.Marshal(row["importWarnings"])
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want
			if tc.name == "old warning" {
				value, _ := json.Marshal([]any{mlkemMetadataWarning, "fixture", nil})
				want = string(value)
			}
			if string(warnings) != want {
				t.Fatalf("warning response got %s want %s", warnings, want)
			}
			stored, err := ReadRecord(e.d, id)
			if err != nil {
				t.Fatal(err)
			}
			masked, err := json.Marshal(MaskRecord(stored))
			if err != nil || strings.Contains(string(masked), "synthetic-private") {
				t.Fatal("full card exposed credentials or failed to marshal")
			}
			if tc.name == "old warning" && stored["importWarnings"].([]any)[0] != legacyMLKEMMetadataWarning {
				t.Fatal("summary changed the stored warning")
			}
		})
	}
}
func TestWarnings357SummaryRetainsCaveats(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "trojan://synthetic@example.test:443?fp=unsafe")
	r, err := ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	r["importWarnings"] = []any{"Synthetic safe warning"}
	if err = WriteRecord(e.d, r); err != nil {
		t.Fatal(err)
	}
	summary, err := e.s.summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rows := summary["servers"].([]any)
	if rows[0].(map[string]any)["importWarnings"] == nil {
		t.Fatal("saved warnings missing from summary")
	}
}

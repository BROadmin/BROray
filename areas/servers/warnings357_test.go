package servers

import (
	"context"
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

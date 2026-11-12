package clashschema

import "testing"

func TestKnownProjectionPreservesUnmappedFields(t *testing.T) {
	n, e := Parse(map[string]any{"type": "vless", "name": "a", "server": "example.invalid", "port": 443, "uuid": "test", "smux": map[string]any{"enabled": true}, "ws-opts": map[string]any{"path": "/", "private-extension": "secret"}, "other-private-extension": "secret"})
	if e != nil {
		t.Fatal(e)
	}
	if n.KnownFields == nil {
		t.Fatal("no independent declared-field projection")
	}
	if _, ok := n.KnownFields["other-private-extension"]; ok {
		t.Fatal("unknown root projected")
	}
	if _, ok := n.KnownFields["smux"]; !ok {
		t.Fatal("known unmapped field dropped")
	}
	w := n.KnownFields["ws-opts"].(map[string]any)
	if _, ok := w["private-extension"]; ok {
		t.Fatal("unknown nested projected")
	}
	w["path"] = "changed"
	if n.Fields["ws-opts"].(map[string]any)["path"] != "/" {
		t.Fatal("projection aliases private original")
	}
}

package xrayschema

import "testing"

func TestKnownProjectionDynamicObjects(t *testing.T) {
	n, e := Parse(object(t, `{"protocol":"vless","streamSettings":{"rawSettings":{"header":{"type":"none","private":"token"}},"finalmask":{"udp":[{"type":"realm","settings":{"portMapping":{"enabled":false,"private":"token"}}}]}}}`))
	if e != nil {
		t.Fatal(e)
	}
	h := n.KnownFields["streamSettings"].(map[string]any)["rawSettings"].(map[string]any)["header"].(map[string]any)
	if _, ok := h["private"]; ok {
		t.Fatal("unknown no-header field survived projection")
	}
	items := n.KnownFields["streamSettings"].(map[string]any)["finalmask"].(map[string]any)["udp"].([]any)
	pm := items[0].(map[string]any)["settings"].(map[string]any)["portMapping"].(map[string]any)
	if _, ok := pm["private"]; ok {
		t.Fatal("unknown external type field survived projection")
	}
}

func TestKnownProjectionPreservesUnmappedFields(t *testing.T) {
	n, e := Parse(object(t, `{"protocol":"http","settings":{"address":"example.invalid","port":443,"private-extension":"secret"},"streamSettings":{"sockopt":{"mark":42,"unknown-private":1}}}`))
	if e != nil {
		t.Fatal(e)
	}
	if n.KnownFields == nil {
		t.Fatal("no independent declared-field projection")
	}
	if _, ok := n.KnownFields["settings"].(map[string]any)["private-extension"]; ok {
		t.Fatal("unknown projected")
	}
	s := n.KnownFields["streamSettings"].(map[string]any)["sockopt"].(map[string]any)
	if _, ok := s["mark"]; !ok {
		t.Fatal("known local control dropped")
	}
	if _, ok := s["unknown-private"]; ok {
		t.Fatal("nested unknown projected")
	}
	s["mark"] = 0
	if n.Fields["streamSettings"].(map[string]any)["sockopt"].(map[string]any)["mark"] == 0 {
		t.Fatal("projection aliases private original")
	}
}

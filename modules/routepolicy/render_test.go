package routepolicy

import (
	"bytes"
	"encoding/json"
	"github.com/BROadmin/broray/modules/geodata"
	"testing"
)

var baseConfig = []byte(`{"inbounds":[{"tag":"socks","protocol":"socks","settings":{"auth":"noauth","udp":true}}],"outbounds":[{"tag":"proxy","protocol":"blackhole","settings":{"untouched":9007199254740993}}]}`)

func TestRenderPreservesDefaultsAndAddsScopedPolicy(t *testing.T) {
	got, err := Render(baseConfig, []Rule{NewRule("off", "Off")}, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, "lo")
	if err != nil || !bytes.Equal(got, baseConfig) {
		t.Fatal("disabled policy changed base bytes")
	}
	rules := []Rule{{ID: "ad", Enabled: true, Action: Deny, Domains: []string{"ads.example"}}, {ID: "direct", Enabled: true, Action: Direct, Domains: []string{"direct.example"}}}
	got, err = Render(baseConfig, rules, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, "lo")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatal(err)
	}
	routing, ok := cfg["routing"].(map[string]any)
	if !ok || routing["domainStrategy"] != DomainStrategy {
		t.Fatal("missing routing strategy")
	}
	if len(cfg["outbounds"].([]any)) != 3 || !bytes.Contains(got, []byte("9007199254740993")) {
		t.Fatal("proxy data changed")
	}
	inbound := cfg["inbounds"].([]any)[0].(map[string]any)
	if inbound["sniffing"].(map[string]any)["routeOnly"] != true || inbound["settings"].(map[string]any)["auth"] != "noauth" {
		t.Fatal("inbound behavior changed")
	}
	if _, err := Render(baseConfig, rules, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"foreign"}, "lo"); err == nil {
		t.Fatal("missing inbound scope accepted")
	}
	var occupied map[string]any
	_ = json.Unmarshal(baseConfig, &occupied)
	occupied["routing"] = map[string]any{"rules": []any{}}
	data, _ := json.Marshal(occupied)
	if _, err := Render(data, rules, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, "lo"); err == nil {
		t.Fatal("existing routing overwritten")
	}
}

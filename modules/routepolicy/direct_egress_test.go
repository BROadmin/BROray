package routepolicy

import (
	"encoding/json"
	"github.com/BROadmin/broray/modules/geodata"
	"testing"
)

func TestDirectEgressRequiresInterface(t *testing.T) {
	got, err := Render(baseConfig, []Rule{{ID: "direct", Enabled: true, Action: Direct, Domains: []string{"example.com"}}}, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, "lo")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err = json.Unmarshal(got, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, v := range cfg["outbounds"].([]any) {
		out := v.(map[string]any)
		if out["tag"] == testTags.Direct {
			opts := out["streamSettings"].(map[string]any)["sockopt"].(map[string]any)
			if opts["interface"] != "lo" {
				t.Fatal("managed DIRECT has no explicit egress binding; static ProxyN routes can recurse")
			}
		}
	}
}

func TestDirectEgressRejectsMissingOrUnsafeBinding(t *testing.T) {
	rules := []Rule{{ID: "direct", Enabled: true, Action: Direct, Domains: []string{"example.com"}}}
	for _, name := range []string{"", "eth0;bad", "../eth0", "eth0\x00", "interface-name-too-long"} {
		if _, err := Render(baseConfig, rules, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, name); err == nil {
			t.Fatalf("accepted unsafe interface %q", name)
		}
	}
	rules[0].Action = Deny
	if _, err := Render(baseConfig, rules, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, ""); err != nil {
		t.Fatal("deny unexpectedly requires WAN", err)
	}
	rules[0].Action = Direct
	rules[0].Enabled = false
	if got, err := Render(baseConfig, rules, geodata.Snapshot{}, geodata.AssetDirectory, testTags, []string{"socks"}, ""); err != nil || string(got) != string(baseConfig) {
		t.Fatal("disabled DIRECT changed base", err)
	}
}

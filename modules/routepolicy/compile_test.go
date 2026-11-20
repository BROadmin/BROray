package routepolicy

import (
	"encoding/json"
	"github.com/BROadmin/broray/modules/geodata"
	"reflect"
	"strings"
	"testing"
)

var testTags = OutboundTags{VPN: "proxy", Direct: "direct", Deny: "deny"}

func TestDomainImport(t *testing.T) {
	got, err := ParseDomainList("# comment\r\nEXAMPLE.COM.\n*.example.com\nfull:Only.Example.com\n// comment\n")
	if err != nil || !reflect.DeepEqual(got, []string{"domain:example.com", "full:only.example.com"}) {
		t.Fatalf("%v %v", got, err)
	}
	for _, input := range []string{"https://example.com/x", "example.com:443", "regexp:.*", "a..com", "bad_name.com", "127.0.0.1", "*.com;reboot", "example.com other.com"} {
		if _, err := ParseDomainList(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
func TestCompilePriorityScopeAndDefaults(t *testing.T) {
	rules := []Rule{{ID: "vpn", Enabled: true, Action: VPN, Domains: []string{"example.com"}}, {ID: "direct", Enabled: true, Action: Direct, Domains: []string{"full:example.com"}}, {ID: "deny", Enabled: true, Action: Deny, Domains: []string{"example.com"}}, NewRule("off", "New")}
	compiled, err := Compile(rules, geodata.Snapshot{}, testTags, []string{"socks"})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 3 {
		t.Fatalf("%d rules", len(compiled))
	}
	for i, want := range []string{"deny", "direct", "proxy"} {
		var m map[string]any
		if err := json.Unmarshal(compiled[i], &m); err != nil {
			t.Fatal(err)
		}
		if m["outboundTag"] != want || !reflect.DeepEqual(m["inboundTag"], []any{"socks"}) {
			t.Fatalf("rule %s", compiled[i])
		}
	}
	reversed := []Rule{rules[3], rules[2], rules[1], rules[0]}
	again, err := Compile(reversed, geodata.Snapshot{}, testTags, []string{"socks"})
	if err != nil || !reflect.DeepEqual(compiled, again) {
		t.Fatal("card order changes routing")
	}
	if NewRule("new", "New").Enabled || NewRule("new", "New").Action != Deny {
		t.Fatal("unsafe new-rule defaults")
	}
	if _, err := Compile(rules, geodata.Snapshot{}, testTags, nil); err == nil {
		t.Fatal("unscoped policy accepted")
	}
}
func TestCompileExternalTagsAndMatcherOR(t *testing.T) {
	rev := strings.Repeat("a", 40)
	generation := "gen-" + strings.Repeat("b", 32)
	plan, err := geodata.SourcePlan(geodata.Selection{CountryCodes: []string{"fi"}, GeoSiteTags: []string{"category-ads"}}, geodata.Revisions{GeoIP: rev, GeoSite: rev})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := geodata.Snapshot{Generation: generation}
	for _, s := range plan {
		snapshot.Assets = append(snapshot.Assets, geodata.Asset{Source: s, Path: generation + "/" + s.Name, SHA256: strings.Repeat("c", 64), Bytes: 1})
	}
	rules := []Rule{{ID: "blocked", Enabled: true, Action: Deny, CountryCodes: []string{"fi"}, GeoSiteTags: []string{"category-ads"}, Domains: []string{"example.com"}}}
	compiled, err := Compile(rules, snapshot, testTags, []string{"socks"})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 2 {
		t.Fatal("IP and domain predicates must be separate OR alternatives")
	}
	for _, data := range compiled {
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m["ip"] != nil && m["domain"] != nil {
			t.Fatal("accidental AND semantics")
		}
	}
	if _, err := Compile(rules, geodata.Snapshot{}, testTags, []string{"socks"}); err == nil {
		t.Fatal("missing geodata accepted")
	}
	snapshot.Assets[0].Path = "../../foreign.dat"
	if _, err := Compile(rules, snapshot, testTags, []string{"socks"}); err == nil {
		t.Fatal("foreign data path accepted")
	}
}

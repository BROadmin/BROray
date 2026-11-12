package subscriptions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/clashimport"
)

func clashPolicy357Unavailable(t *testing.T, p map[string]any) {
	t.Helper()
	input, err := clashimport.ConvertNode(p, importChecks())
	if err != nil || input.Unavailable == nil || input.Capability == nil {
		t.Fatalf("valid source constraint must be retained with explicit unavailable support: %v", err)
	}
	if input.Link != "" || len(input.ClientFields) != 0 {
		t.Fatal("local source constraint produced an empty or stripped runnable input")
	}
	if !jsonEqual(input.Unavailable.Fields, p) {
		t.Fatal("local source constraint disappeared during Clash-to-Xray adaptation")
	}
	if input.Capability.Kind == "" || input.Capability.Kind == "core-unsupported" || input.Capability.Kind == "unknown-extension" {
		t.Fatal("local policy must not be mislabeled as missing core support or an unknown field")
	}
	if len(input.Capability.Fields) == 0 {
		t.Fatal("local constraint needs an explicit affected field")
	}
	capability, _ := json.Marshal(input.Capability)
	if strings.Contains(string(capability), "synthetic-bound-interface-357") {
		t.Fatal("capability diagnostic disclosed a source-local interface value")
	}
	clashCapability357Persist(t, p)
}

func TestClashPolicy357LocalControlsRetained(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
	}{
		{"interface-name", "synthetic-bound-interface-357"},
		{"routing-mark", 357},
	} {
		t.Run(tc.field, func(t *testing.T) {
			p := clashMapped357Base("vless")
			p[tc.field] = tc.value
			// A shared Xray UnavailableSource is a classification result, not
			// a successful empty Link and not permission to lose Clash fields.
			clashPolicy357Unavailable(t, p)
		})
	}
}

func TestClashPolicy357UDPFalseRetained(t *testing.T) {
	for _, kind := range []string{"vless", "vmess", "trojan", "ss", "socks5", "wireguard"} {
		t.Run(kind, func(t *testing.T) {
			p := clashMapped357Base(kind)
			if kind == "wireguard" {
				p = clashFull357WG()
			}
			p["udp"] = false
			// A warning followed by a UDP-capable outbound broadens the source
			// policy. Until an actual runtime constraint is mapped, retain the
			// complete server privately and refuse execution explicitly.
			clashPolicy357Unavailable(t, p)
		})
	}
}

func TestClashPolicy357EmptyControlsRemainConvertible(t *testing.T) {
	p := clashMapped357Base("vless")
	p["interface-name"], p["routing-mark"], p["udp"] = "", 0, true
	input, err := clashimport.ConvertNode(p, importChecks())
	if err != nil || input.Unavailable != nil || input.Link == "" {
		t.Fatalf("inactive local controls must not disable an otherwise convertible client: %v", err)
	}
	clashMapped357Roundtrip(t, p, map[string]any{"protocol": "vless"})
}

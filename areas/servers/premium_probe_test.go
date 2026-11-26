package servers

import (
	"encoding/json"
	"net/netip"
	"testing"
)

func TestPremiumConfigPinsOnlyApprovedAddresses(t *testing.T) {
	base := []byte(`{"inbounds":[{"tag":"socks","protocol":"socks"}],"outbounds":[{"tag":"proxy","protocol":"trojan"}]}`)
	ip := netip.MustParseAddr("93.184.216.34")
	config, err := premiumRestrictedConfig(base, []netip.Addr{ip})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Routing struct {
			Rules []struct {
				InboundTag  []string `json:"inboundTag"`
				OutboundTag string   `json:"outboundTag"`
				IP          []string `json:"ip"`
			}
		}
	}
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	rules := decoded.Routing.Rules
	if len(rules) != 2 || rules[0].OutboundTag != "proxy" || len(rules[0].IP) != 1 ||
		rules[0].IP[0] != "93.184.216.34/32" || len(rules[0].InboundTag) != 1 ||
		rules[0].InboundTag[0] != "broray-reserve-probe" || rules[1].OutboundTag != "broray-premium-deny" {
		t.Fatalf("unexpected isolated path: %+v", rules)
	}
	if _, err := premiumRestrictedConfig(base, []netip.Addr{netip.MustParseAddr("127.0.0.1")}); err == nil {
		t.Fatal("loopback accepted as a VPN probe destination")
	}
	config, err = premiumRestrictedConfig(base, []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")})
	if err != nil || json.Unmarshal(config, &decoded) != nil || decoded.Routing.Rules[0].IP[0] != "2606:4700:4700::1111/128" {
		t.Fatalf("IPv6 not limited to a single host: %s, %v", config, err)
	}
}

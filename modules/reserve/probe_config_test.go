package reserve

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/strictjson"
)

const healthBase = `{"log":{"loglevel":"none"},"inbounds":[{"tag":"socks","listen":"0.0.0.0","port":2080,"protocol":"socks","settings":{"auth":"noauth","udp":true}}],"outbounds":[{"tag":"proxy","protocol":"trojan","settings":{"servers":[{"address":"vpn.example","port":443,"password":"private"}]},"streamSettings":{"network":"tcp","security":"tls","finalmask":{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200","interval":"1-10"}}]},"sockopt":{"tcpUserTimeout":9007199254740993}}},{"tag":"direct","protocol":"freedom"}],"routing":{"domainStrategy":"IPOnDemand","rules":[{"type":"field","inboundTag":["socks"],"domain":["full:cp.cloudflare.com"],"outboundTag":"direct"}]}}`

func configObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	v, err := strictjson.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatal("not an object")
	}
	return m
}

func TestReserveHealthRoutePreservesUserPolicyAndMasks(t *testing.T) {
	input := []byte(healthBase)
	before := append([]byte(nil), input...)
	got, err := AttachHealthInbound(input, netip.MustParseAddrPort("127.0.0.1:32080"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, before) || !bytes.Contains(got, []byte("9007199254740993")) {
		t.Fatal("input mutated or number rounded")
	}
	old, cfg := configObject(t, input), configObject(t, got)
	obs := cfg["outbounds"].([]any)
	if len(obs) != 3 || !reflect.DeepEqual(obs[:2], old["outbounds"]) {
		t.Fatal("profile/custom mask or existing outbound changed")
	}
	ins := cfg["inbounds"].([]any)
	if len(ins) != 2 || !reflect.DeepEqual(ins[0], old["inbounds"].([]any)[0]) {
		t.Fatal("normal inbound changed")
	}
	diag := ins[1].(map[string]any)
	if diag["tag"] != HealthInboundTag || diag["listen"] != "127.0.0.1" || diag["port"] != json.Number("32080") || diag["protocol"] != "socks" {
		t.Fatal("wrong diagnostic endpoint", diag)
	}
	routing := cfg["routing"].(map[string]any)
	rules := routing["rules"].([]any)
	if len(rules) != 3 || !reflect.DeepEqual(rules[2:], old["routing"].(map[string]any)["rules"]) {
		t.Fatal("user rule changed/reordered")
	}
	allow, deny := rules[0].(map[string]any), rules[1].(map[string]any)
	if allow["outboundTag"] != "proxy" || allow["port"] != "443" || allow["network"] != "tcp" || !reflect.DeepEqual(allow["inboundTag"], []any{HealthInboundTag}) || !reflect.DeepEqual(allow["domain"], []any{"full:cp.cloudflare.com", "full:www.gstatic.com"}) {
		t.Fatal("diagnostic traffic can escape proxy/control scope", allow)
	}
	if deny["outboundTag"] != healthDenyTag || !reflect.DeepEqual(deny["inboundTag"], []any{HealthInboundTag}) || obs[2].(map[string]any)["protocol"] != "blackhole" {
		t.Fatal("missing deny fallback")
	}
}

func TestReserveHealthConfigRejectsAmbiguityAndForeignTags(t *testing.T) {
	for name, raw := range map[string]string{
		"duplicate":         strings.Replace(healthBase, `"log":`, `"log":{},"log":`, 1),
		"trailing":          healthBase + `{}`,
		"direct-proxy":      strings.Replace(healthBase, `"protocol":"trojan"`, `"protocol":"freedom"`, 1),
		"reserved-inbound":  strings.Replace(healthBase, `"tag":"socks"`, `"tag":"broray-reserve-health"`, 1),
		"reserved-outbound": strings.Replace(healthBase, `"tag":"direct"`, `"tag":"broray-reserve-health-deny"`, 1),
		"port-collision":    strings.Replace(healthBase, `"port":2080`, `"port":32080`, 1),
		"bad-routing":       strings.Replace(healthBase, `"rules":[`, `"rules":null,"unused":[`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := AttachHealthInbound([]byte(raw), netip.MustParseAddrPort("127.0.0.1:32080")); err == nil || got != nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
	for _, endpoint := range []netip.AddrPort{{}, netip.MustParseAddrPort("0.0.0.0:32080"), netip.MustParseAddrPort("192.168.1.1:32080"), netip.MustParseAddrPort("127.0.0.1:0")} {
		if _, err := AttachHealthInbound([]byte(healthBase), endpoint); err == nil {
			t.Fatal("non-private endpoint accepted", endpoint)
		}
	}
}

func TestReserveHealthConfigIPv6WithoutExistingRouting(t *testing.T) {
	cfg := configObject(t, []byte(healthBase))
	delete(cfg, "routing")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := AttachHealthInbound(raw, netip.MustParseAddrPort("[::1]:32080"))
	if err != nil {
		t.Fatal(err)
	}
	next := configObject(t, got)
	if next["inbounds"].([]any)[1].(map[string]any)["listen"] != "::1" {
		t.Fatal("IPv6 loopback lost")
	}
	if len(next["routing"].(map[string]any)["rules"].([]any)) != 2 {
		t.Fatal("wrong default rules")
	}
	if _, err := AttachHealthInbound(got, netip.MustParseAddrPort("[::1]:32081")); err == nil {
		t.Fatal("existing diagnostic objects silently overwritten")
	}
}

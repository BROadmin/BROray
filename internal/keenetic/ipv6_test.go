package keenetic

import (
	"reflect"
	"strings"
	"testing"
)

// The fixtures below are ASSUMED shapes of KeeneticOS 4.x/5.x output: no
// IPv6-enabled router was available when they were written. The parser is
// deliberately generic (any address/prefix value, interface taken from the
// record or its enclosing interface), so small differences in key names or
// nesting do not matter; it must be re-checked against a real router.

// RCI GET /rci/show/ipv6/addresses (assumed).
const rciAddressesGlobal = `{"address":[
 {"address":"fe80::52ff:20ff:fe00:1","interface":"GigabitEthernet1","valid-lifetime":"infinite"},
 {"address":"2a02:2698:a002:1d::5","interface":"GigabitEthernet1","valid-lifetime":"86400"},
 {"address":"2a02:2698:a400:7e01::1","interface":"Bridge0","valid-lifetime":"86400"},
 {"address":"fd8f:1d3a:2b4c::1","interface":"Bridge0","valid-lifetime":"infinite"}
]}`

// Only link-local and ULA (fc00::/7) addresses: no global connectivity.
const rciAddressesLocalOnly = `{"address":[
 {"address":"fe80::52ff:20ff:fe00:1","interface":"GigabitEthernet1","valid-lifetime":"infinite"},
 {"address":"fe80::1","interface":"Bridge0","valid-lifetime":"infinite"},
 {"address":"fd8f:1d3a:2b4c::1","interface":"Bridge0","valid-lifetime":"infinite"},
 {"address":"fc00:1::2","interface":"Wireguard0","valid-lifetime":"infinite"}
]}`

// RCI GET /rci/show/ipv6/prefixes (assumed): a DHCPv6-PD prefix received on
// the WAN while the WAN itself has only a link-local address (common).
const rciPrefixesPD = `{"prefix":[{"prefix":"2a02:2698:a400:7e00::/56","interface":"PPPoE0","valid-lifetime":"86400","preferred-lifetime":"43200"}]}`

// RCI GET /rci/show/interface (assumed): object keyed by interface id, the
// IPv6 section nested under "ipv6".
const rciInterfaces = `{
 "GigabitEthernet1":{"id":"GigabitEthernet1","interface-name":"ISP","type":"GigabitEthernet","global":true,"address":"203.0.113.2","mask":"255.255.255.0",
   "ipv6":{"addresses":[{"address":"fe80::52ff:20ff:fe00:1","prefix-length":64},{"address":"2a02:2698:a002:1d::5","prefix-length":64,"proto":"dhcp"}],"dns":["2a02:2698::53"]}},
 "Bridge0":{"id":"Bridge0","interface-name":"Home","type":"Bridge","address":"192.168.1.1",
   "ipv6":{"addresses":[{"address":"2a02:2698:a400:7e01::1","prefix-length":64}]}},
 "Wireguard0":{"id":"Wireguard0","type":"Wireguard","address":"10.8.0.2","wireguard":{"peer":[{"remote-endpoint-address":"2a01:4f8::1"}]}},
 "Proxy0":{"id":"Proxy0","type":"Proxy","address":"0.0.0.0"}
}`

// ndmc -c "show ipv6 addresses" (assumed): keys right-aligned at the colon,
// one "address:" header per entry.
var ndmcAddressesGlobal = []string{
	"",
	"          address: ",
	"               address: fe80::52ff:20ff:fe00:1",
	"             interface: GigabitEthernet1",
	"        valid-lifetime: infinite",
	"",
	"          address: ",
	"               address: 2a02:2698:a002:1d::5",
	"             interface: GigabitEthernet1",
	"        valid-lifetime: 86400",
	"",
	"          address: ",
	"               address: 2a02:2698:a400:7e01::1",
	"             interface: Bridge0",
	"        valid-lifetime: 86400",
}

var ndmcAddressesLinkLocal = []string{
	"          address: ",
	"               address: fe80::52ff:20ff:fe00:1",
	"             interface: GigabitEthernet1",
	"        valid-lifetime: infinite",
	"          address: ",
	"               address: fe80::1",
	"             interface: Bridge0",
	"        valid-lifetime: infinite",
}

// ndmc -c "show interface" (assumed): one block per interface; the IPv6
// section carries no interface name of its own.
var ndmcInterfaces = []string{
	`Interface, name = "GigabitEthernet1"`,
	"               id: GigabitEthernet1",
	"            index: 2",
	"   interface-name: ISP",
	"             type: GigabitEthernet",
	"          address: 203.0.113.2",
	"             ipv6: ",
	"                addresses: ",
	"                       address: 2a02:2698:a002:1d::5",
	"                 prefix-length: 64",
	"",
	`Interface, name = "Bridge0"`,
	"               id: Bridge0",
	"   interface-name: Home",
	"          address: 192.168.1.1",
	"             ipv6: ",
	"                addresses: ",
	"                       address: fe80::1",
}

func TestParseIPv6JSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want IPv6Facts
	}{
		{"global wan and lan", rciAddressesGlobal, IPv6Facts{State: IPv6Global, WAN: []string{"GigabitEthernet1"}, LANPrefix: true}},
		{"link-local and ula only", rciAddressesLocalOnly, IPv6Facts{State: IPv6None, WAN: []string{}}},
		{"delegated prefix only", rciPrefixesPD, IPv6Facts{State: IPv6Global, WAN: []string{"PPPoE0"}}},
		{"show interface", rciInterfaces, IPv6Facts{State: IPv6Global, WAN: []string{"GigabitEthernet1"}, LANPrefix: true}},
		{"empty object", `{}`, IPv6Facts{State: IPv6None, WAN: []string{}}},
		{"no ipv6 anywhere", `{"Bridge0":{"id":"Bridge0","address":"192.168.1.1","mac":"50:ff:20:00:00:01"}}`, IPv6Facts{State: IPv6None, WAN: []string{}}},
		{"ipv4-mapped is not ipv6", `{"address":[{"address":"::ffff:203.0.113.2","interface":"GigabitEthernet1"}]}`, IPv6Facts{State: IPv6None, WAN: []string{}}},
		{"global prefix on lan", `{"prefix":[{"prefix":"2a02:2698:a400:7e01::/64","interface":"Bridge0"}]}`, IPv6Facts{State: IPv6Global, WAN: []string{}, LANPrefix: true}},
		{"global without interface", `{"address":[{"address":"2001:470:1f0b::2"}]}`, IPv6Facts{State: IPv6Global, WAN: []string{}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseIPv6JSON([]byte(c.raw))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestParseIPv6JSONMalformed(t *testing.T) {
	for _, raw := range []string{
		``, `{`, `not json`, `[]`, `"text"`, `null`, `{"address":[}`,
		// RCI answers an unknown or failing command with a status error.
		`{"status":[{"status":"error","code":"7405602","ident":"Command::Base","message":"no such command: ipv6"}]}`,
	} {
		if f, err := ParseIPv6JSON([]byte(raw)); err == nil {
			t.Fatalf("%q: no error, got %+v", raw, f)
		}
	}
}

func TestParseIPv6Text(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  IPv6Facts
	}{
		{"ndmc addresses global", ndmcAddressesGlobal, IPv6Facts{State: IPv6Global, WAN: []string{"GigabitEthernet1"}, LANPrefix: true}},
		{"ndmc addresses link-local", ndmcAddressesLinkLocal, IPv6Facts{State: IPv6None, WAN: []string{}}},
		{"ndmc show interface", ndmcInterfaces, IPv6Facts{State: IPv6Global, WAN: []string{"GigabitEthernet1"}}},
		{"empty output", nil, IPv6Facts{State: IPv6None, WAN: []string{}}},
		{"ula on wan", []string{"address: fd00::5", "interface: GigabitEthernet1"}, IPv6Facts{State: IPv6None, WAN: []string{}}},
		{"value with prefix length", []string{"prefix: 2a02:2698:a400:7e00::/56", "interface: GigabitEthernet1"}, IPv6Facts{State: IPv6Global, WAN: []string{"GigabitEthernet1"}}},
		{"dns server is not an address of the router", []string{"id: GigabitEthernet1", "ipv6:", "    dns-server: 2a02:2698::53", "    gateway-address: 2a02:2698::1"}, IPv6Facts{State: IPv6None, WAN: []string{}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseIPv6Text(c.lines)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// The RCI JSON rendered into the ndmc layout (areas/keenetic renderRCI)
// must give the same answer as the JSON itself.
func TestParseIPv6TextOfRenderedJSON(t *testing.T) {
	rendered := []string{
		"address:",
		"    address: fe80::52ff:20ff:fe00:1",
		"    interface: GigabitEthernet1",
		"address:",
		"    address: 2a02:2698:a002:1d::5",
		"    interface: GigabitEthernet1",
	}
	got, err := ParseIPv6Text(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if want := (IPv6Facts{State: IPv6Global, WAN: []string{"GigabitEthernet1"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseIPv6TextMalformed(t *testing.T) {
	for _, lines := range [][]string{
		{"Command::Base error[7405602]: no such command: ipv6."},
		{"garbage without any key value"},
		{"(config)> show ipv6 addresses", "% unknown command"},
		{"status:", "    status: error", "    code: 7405602", "    message: no such command: ipv6"},
	} {
		if f, err := ParseIPv6Text(lines); err == nil {
			t.Fatalf("%q: no error, got %+v", lines, f)
		}
	}
}

func TestIPv6TunnelInterfacesAreNotWAN(t *testing.T) {
	raw := []byte(`{"address":[{"address":"2001:db8::1","interface":"Wireguard0"},{"address":"2001:db8::2","interface":"OpenVPN1"},{"address":"2001:db8::3","interface":"Proxy0"},{"address":"2001:db8::4","interface":"GigabitEthernet1"}]}`)
	f, err := ParseIPv6JSON(raw)
	if err != nil || f.State != IPv6Global || !reflect.DeepEqual(f.WAN, []string{"GigabitEthernet1"}) {
		t.Fatalf("tunnel listed as WAN: %+v %v", f, err)
	}
	if !strings.HasPrefix(IPv6WarningText, "На роутере включён IPv6:") {
		t.Fatalf("warning attributes IPv6 to the ISP: %q", IPv6WarningText)
	}
}

func TestIPv6MergeAndObject(t *testing.T) {
	var f IPv6Facts
	if f.Object("")["state"] != IPv6Unknown {
		t.Fatalf("zero value must be unknown: %v", f.Object(""))
	}
	f = f.Merge(IPv6Facts{State: IPv6None, WAN: []string{}})
	if f.State != IPv6None {
		t.Fatalf("%+v", f)
	}
	f = f.Merge(IPv6Facts{State: IPv6Global, WAN: []string{"PPPoE0"}})
	f = f.Merge(IPv6Facts{State: IPv6Global, WAN: []string{"GigabitEthernet1", "PPPoE0"}, LANPrefix: true})
	f = f.Merge(IPv6Facts{State: IPv6None, WAN: []string{}})
	if want := (IPv6Facts{State: IPv6Global, WAN: []string{"GigabitEthernet1", "PPPoE0"}, LANPrefix: true}); !reflect.DeepEqual(f, want) {
		t.Fatalf("got %+v", f)
	}
	o := f.Object("2026-11-02T10:00:00+0300")
	if o["state"] != "global" || o["lanPrefix"] != true || o["checkedAt"] != "2026-11-02T10:00:00+0300" {
		t.Fatalf("%v", o)
	}
	if w, _ := o["warning"].(string); !strings.Contains(w, "IPv6") || w != IPv6WarningText {
		t.Fatalf("warning %v", o["warning"])
	}
	if !reflect.DeepEqual(o["wan"], []string{"GigabitEthernet1", "PPPoE0"}) {
		t.Fatalf("wan %v", o["wan"])
	}
	for _, st := range []string{IPv6None, IPv6Unknown} {
		o := IPv6Facts{State: st}.Object("")
		if o["warning"] != nil || o["checkedAt"] != nil {
			t.Fatalf("%s: %v", st, o)
		}
		if wan, ok := o["wan"].([]string); !ok || wan == nil || len(wan) != 0 {
			t.Fatalf("%s: wan must be an empty list, got %#v", st, o["wan"])
		}
	}
}

func TestIPv6Classify(t *testing.T) {
	for s, want := range map[string]bool{
		"2a02:2698:a002:1d::5": true, "2001:470:1f0b::2": true, "3fff::1": true, "2a02::/32": true,
		"fe80::1": false, "fe80::1%br0": false, "fd00::1": false, "fc00::1": false, "::1": false, "::": false,
		"ff02::1": false, "::ffff:1.2.3.4": false, "192.168.1.1": false, "50:ff:20:00:00:01": false, "": false, "4000::1": false,
	} {
		if got := isGlobalIPv6(s); got != want {
			t.Errorf("%q: got %v", s, got)
		}
	}
}

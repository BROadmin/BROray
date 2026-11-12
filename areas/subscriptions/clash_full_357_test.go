package subscriptions

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/amnezia"
	"github.com/BROadmin/broray/internal/cgi"
)

const (
	clashFull357Private = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	clashFull357Public  = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	clashFull357Other   = "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="
)

func clashFull357WG() map[string]any {
	return map[string]any{
		"name": "synthetic-full-wg", "type": "wireguard",
		"server": "peer.example.invalid", "port": 51820,
		"ip": "10.7.0.2", "private-key": clashFull357Private, "public-key": clashFull357Public,
	}
}

func clashFull357Peers() map[string]any {
	p := clashFull357WG()
	delete(p, "server")
	delete(p, "port")
	delete(p, "public-key")
	p["peers"] = []any{
		map[string]any{"server": "first.example.invalid", "port": 51820, "public-key": clashFull357Public, "allowed-ips": []any{"10.80.0.0/16"}},
		map[string]any{"server": "2001:db8::2", "port": 51821, "public-key": clashFull357Other, "allowed-ips": []any{"2001:db8:80::/48"}},
	}
	return p
}

func clashFull357Record(t *testing.T, p map[string]any) (servers.Record, *staged) {
	t.Helper()
	st := clashStage(t, clashBody(p))
	if st.Received != 1 || st.Accepted != 1 || st.Rejected != 0 || len(st.Records) != 1 {
		t.Fatalf("valid Clash client must be retained: received=%d accepted=%d rejected=%d warnings=%v", st.Received, st.Accepted, st.Rejected, st.Warnings)
	}
	r := st.Records[0]
	if err := servers.Validate(r, r["id"].(string)+".json"); err != nil {
		t.Fatalf("retained record must pass storage validation: %v", err)
	}
	return r, st
}

// Exercise the public config boundary, not just the intermediate record.
func clashFull357Settings(t *testing.T, r servers.Record) map[string]any {
	t.Helper()
	b, err := servers.XrayConfig(r, servers.GenSettings{Listen: "127.0.0.1", SocksPort: 2080}, "synthetic-full-import")
	if err != nil {
		t.Fatalf("convertible client must produce config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	obs, ok := cfg["outbounds"].([]any)
	if !ok || len(obs) != 1 {
		t.Fatal("expected one WireGuard outbound")
	}
	ob := obs[0].(map[string]any)
	if ob["protocol"] != "wireguard" {
		t.Fatalf("protocol changed to %v", ob["protocol"])
	}
	if _, exists := ob["streamSettings"]; exists {
		t.Fatal("WireGuard must not acquire an unrelated stream transport")
	}
	return ob["settings"].(map[string]any)
}

func clashFull357Equal(t *testing.T, got, want any, field string) {
	t.Helper()
	if !jsonEqual(got, want) {
		t.Fatalf("%s changed: got %v, want %v", field, got, want)
	}
}

func TestClashFull357WireGuardIPv6Only(t *testing.T) {
	p := clashFull357WG()
	delete(p, "ip")
	p["ipv6"] = "fd00:7::2"
	r, _ := clashFull357Record(t, p)
	settings := clashFull357Settings(t, r)
	clashFull357Equal(t, settings["address"], []any{"fd00:7::2/128"}, "local address")
	peers := settings["peers"].([]any)
	clashFull357Equal(t, peers[0].(map[string]any)["allowedIPs"], []any{"::/0"}, "IPv6-only default routes")
}

func TestClashFull357WireGuardSourceDefaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		mtu  any
		want int
	}{
		{"absent", nil, 1408},
		{"explicit-default", 0, 1408},
		{"explicit-value", 1340, 1340},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := clashFull357WG()
			if tc.mtu != nil {
				p["mtu"] = tc.mtu
			}
			r, _ := clashFull357Record(t, p)
			settings := clashFull357Settings(t, r)
			clashFull357Equal(t, settings["mtu"], tc.want, "source MTU default")
			peers := settings["peers"].([]any)
			clashFull357Equal(t, peers[0].(map[string]any)["allowedIPs"], []any{"0.0.0.0/0"}, "IPv4-only default routes")
		})
	}
}

func TestClashFull357WireGuardMultiplePeers(t *testing.T) {
	p := clashFull357Peers()
	p["ipv6"] = "fd00:7::2/128"
	p["persistent-keepalive"] = 25
	p["peers"].([]any)[1].(map[string]any)["pre-shared-key"] = clashFull357Private
	r, _ := clashFull357Record(t, p)
	settings := clashFull357Settings(t, r)
	want := []any{
		map[string]any{"publicKey": clashFull357Public, "endpoint": "first.example.invalid:51820", "allowedIPs": []any{"10.80.0.0/16"}, "keepAlive": 25},
		map[string]any{"publicKey": clashFull357Other, "preSharedKey": clashFull357Private, "endpoint": "[2001:db8::2]:51821", "allowedIPs": []any{"2001:db8:80::/48"}, "keepAlive": 25},
	}
	clashFull357Equal(t, settings["peers"], want, "all peer endpoints, credentials, routes and keepalive")
	clashFull357Equal(t, settings["address"], []any{"10.7.0.2/32", "fd00:7::2/128"}, "dual-stack addresses")
}

func TestClashFull357WireGuardFullPeerPrecedence(t *testing.T) {
	p := clashFull357Peers()
	p["server"], p["port"], p["public-key"] = "ignored.example.invalid", 443, clashFull357Private
	p["allowed-ips"] = []any{"0.0.0.0/0", "::/0"}
	r, _ := clashFull357Record(t, p)
	settings := clashFull357Settings(t, r)
	peers := settings["peers"].([]any)
	if len(peers) != 2 {
		t.Fatal("top-level shortcut must not replace explicit peers")
	}
	clashFull357Equal(t, peers[0].(map[string]any)["endpoint"], "first.example.invalid:51820", "full-form endpoint precedence")
	clashFull357Equal(t, peers[0].(map[string]any)["allowedIPs"], []any{"10.80.0.0/16"}, "per-peer route precedence")
}

func TestClashFull357WireGuardAllowedIPs(t *testing.T) {
	p := clashFull357WG()
	p["allowed-ips"] = []any{"10.80.0.0/16", "192.0.2.0/24"}
	r, _ := clashFull357Record(t, p)
	settings := clashFull357Settings(t, r)
	peers := settings["peers"].([]any)
	// Follow the explicit documented client restriction; never silently widen it.
	clashFull357Equal(t, peers[0].(map[string]any)["allowedIPs"], []any{"10.80.0.0/16", "192.0.2.0/24"}, "explicit allowed-ips")
}

func TestClashFull357WireGuardReserved(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"bytes", []any{209, 98, 59}},
		{"base64", "0WI7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := clashFull357WG()
			p["reserved"] = tc.value
			r, _ := clashFull357Record(t, p)
			settings := clashFull357Settings(t, r)
			clashFull357Equal(t, settings["reserved"], []any{209, 98, 59}, "reserved bytes")
		})
	}
	t.Run("matching-per-peer-values", func(t *testing.T) {
		p := clashFull357Peers()
		p["peers"].([]any)[0].(map[string]any)["reserved"] = []any{209, 98, 59}
		p["peers"].([]any)[1].(map[string]any)["reserved"] = "0WI7"
		r, _ := clashFull357Record(t, p)
		settings := clashFull357Settings(t, r)
		clashFull357Equal(t, settings["reserved"], []any{209, 98, 59}, "equivalent peer reserved values")
		for _, peer := range settings["peers"].([]any) {
			if _, exists := peer.(map[string]any)["reserved"]; exists {
				t.Fatal("do not emit a core-unknown per-peer reserved field")
			}
		}
	})
}

// The exact private-source container is deliberately not fixed by this test:
// known peer semantics must remain recoverable as structured data, not merely
// as a substring of a link or an opaque JSON blob.
func clashFull357ContainsSource(v any, want map[string]any) bool {
	switch x := v.(type) {
	case map[string]any:
		if jsonEqual(x, want) {
			return true
		}
		for _, child := range x {
			if clashFull357ContainsSource(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if clashFull357ContainsSource(child, want) {
				return true
			}
		}
	}
	return false
}

func TestClashFull357WireGuardDifferentPeerReserved(t *testing.T) {
	p := clashFull357Peers()
	p["peers"].([]any)[0].(map[string]any)["reserved"] = []any{1, 2, 3}
	p["peers"].([]any)[1].(map[string]any)["reserved"] = []any{4, 5, 6}
	r, st := clashFull357Record(t, p)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var persisted servers.Record
	if err := json.Unmarshal(b, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := servers.Validate(persisted, persisted["id"].(string)+".json"); err != nil {
		t.Fatalf("unsupported client must remain persistable: %v", err)
	}
	if !clashFull357ContainsSource(persisted, p) {
		t.Fatal("different peer reserved values must survive in a private structured source")
	}
	if !strings.Contains(strings.Join(st.Warnings, " "), "reserved") {
		t.Fatal("import must explain which retained setting prevents runtime conversion")
	}
	cfg, err := servers.XrayConfig(persisted, servers.GenSettings{Listen: "127.0.0.1"}, "synthetic-unavailable")
	var api *cgi.APIError
	if !errors.As(err, &api) || api.Code != "PROTOCOL_NOT_SUPPORTED" || len(cfg) != 0 {
		t.Fatalf("must refuse lossy reserved conversion with a typed capability error: %v", err)
	}
	outbound, err := servers.Outbound(persisted)
	if !errors.As(err, &api) || api.Code != "PROTOCOL_NOT_SUPPORTED" || outbound != nil {
		t.Fatalf("direct outbound generation must not bypass the capability refusal: %v", err)
	}
	public, err := json.Marshal(servers.MaskRecord(persisted))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), clashFull357Private) || strings.Contains(string(public), "private-key") {
		t.Fatal("private retained source leaked through public record")
	}
}

func TestClashFull357WireGuardRemoteDNS(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setFlag bool
		flag    bool
		wantDNS bool
	}{
		{"absent-is-disabled", false, false, false},
		{"explicit-false", true, false, false},
		{"explicit-true", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := clashFull357WG()
			p["dns"] = []any{"1.1.1.1", "2606:4700:4700::1111"}
			if tc.setFlag {
				p["remote-dns-resolve"] = tc.flag
			}
			r, _ := clashFull357Record(t, p)
			settings := clashFull357Settings(t, r)
			if tc.wantDNS {
				clashFull357Equal(t, settings["remoteDNS"], []any{"1.1.1.1", "2606:4700:4700::1111"}, "enabled remote DNS")
			} else if _, exists := settings["remoteDNS"]; exists {
				t.Fatal("inactive Mihomo dns list must not activate Xray remote DNS")
			}
		})
	}
}

func TestClashFull357WireGuardExtensionIntegrity(t *testing.T) {
	for _, mutation := range []string{"source", "normalized", "projection"} {
		t.Run(mutation, func(t *testing.T) {
			p := clashFull357Peers()
			p["peers"].([]any)[0].(map[string]any)["reserved"] = []any{1, 2, 3}
			p["peers"].([]any)[1].(map[string]any)["reserved"] = []any{4, 5, 6}
			r, _ := clashFull357Record(t, p)
			ext := r["protocolExtensions"].(map[string]any)["wireguard"].(map[string]any)
			switch mutation {
			case "source":
				ext["source"].(map[string]any)["peers"].([]any)[1].(map[string]any)["reserved"] = []any{1, 2, 3}
			case "normalized":
				ext["settings"].(map[string]any)["peerReserved"] = []any{[]any{1, 2, 3}, []any{1, 2, 3}}
			case "projection":
				r["wireguard"].(map[string]any)["peers"].([]any)[0].(map[string]any)["allowedIPs"] = []any{"0.0.0.0/0"}
			}
			if err := servers.Validate(r, r["id"].(string)+".json"); err == nil {
				t.Fatal("stored source, normalized extensions and core projection must agree")
			}
			if cfg, err := servers.XrayConfig(r, servers.GenSettings{Listen: "127.0.0.1"}, "synthetic-tamper"); err == nil || len(cfg) != 0 {
				t.Fatal("a forged extension must not enable a stripped runtime config")
			}
		})
	}
}

func clashFull357AWG(t *testing.T, p map[string]any) (servers.Record, amnezia.Profile) {
	t.Helper()
	r, st := clashFull357Record(t, p)
	if r["protocol"] != "amneziawg" {
		t.Fatalf("AWG must not fall back to plain WireGuard: %v", r["protocol"])
	}
	b, err := json.Marshal(r["amneziawg"])
	if err != nil {
		t.Fatal(err)
	}
	var profile amnezia.Profile
	if err := json.Unmarshal(b, &profile); err != nil {
		t.Fatal(err)
	}
	if _, _, err := profile.Validate(); err != nil {
		t.Fatalf("normalized AWG profile must validate: %v", err)
	}
	source := profile.Parameters["awg"].(map[string]any)["clashOptions"]
	clashFull357Equal(t, source, p, "original AWG structured options")
	if !strings.Contains(strings.Join(st.Warnings, " "), "Подключение пока не поддерживается") {
		t.Fatal("AWG must declare its unavailable runtime")
	}
	cfg, err := servers.XrayConfig(r, servers.GenSettings{Listen: "127.0.0.1"}, "synthetic-awg")
	var api *cgi.APIError
	if !errors.As(err, &api) || api.Code != "PROTOCOL_NOT_SUPPORTED" || len(cfg) != 0 {
		t.Fatalf("AWG runtime must remain explicitly unavailable: %v", err)
	}
	return r, profile
}

func TestClashFull357AWGVersionSelector(t *testing.T) {
	for _, version := range []any{nil, 3} {
		name := "absent"
		if version != nil {
			name = "explicit-three"
		}
		t.Run(name, func(t *testing.T) {
			p := clashFull357WG()
			opts := map[string]any{"jc": 4}
			if version != nil {
				opts["version"] = version
			}
			p["amnezia-wg-option"] = opts
			r, profile := clashFull357AWG(t, p)
			model := r["amneziawg"].(map[string]any)
			if version == nil {
				if _, exists := model["protocolVersion"]; exists {
					t.Fatal("do not infer an explicit AWG implementation selector when absent")
				}
			} else {
				clashFull357Equal(t, model["protocolVersion"], 3, "AWG implementation selector")
			}
			if strings.Contains(profile.NativeConfig, "Version =") {
				t.Fatal("Mihomo version selector is not an invented native INI command")
			}
		})
	}
}

func TestClashFull357AWGLegacyJunkFields(t *testing.T) {
	p := clashFull357WG()
	p["amnezia-wg-option"] = map[string]any{
		"jc": 4, "j1": "<b 0xffffffff><c>", "j2": "<t><wt 1000>", "j3": "<c><r 10>", "itime": 60,
	}
	_, profile := clashFull357AWG(t, p)
	for field, want := range map[string]string{"J1": "<b 0xffffffff><c>", "J2": "<t><wt 1000>", "J3": "<c><r 10>", "Itime": "60"} {
		if !reflect.DeepEqual(profile.Interface[field], []string{want}) {
			t.Fatalf("%s missing from normalized AWG fields", field)
		}
	}
}

func TestClashFull357AWGEmptyOptionalInitiators(t *testing.T) {
	p := clashFull357WG()
	opts := map[string]any{"jc": 4}
	for _, field := range []string{"i1", "i2", "i3", "i4", "i5"} {
		opts[field] = ""
	}
	p["amnezia-wg-option"] = opts
	_, profile := clashFull357AWG(t, p)
	for _, field := range []string{"I1", "I2", "I3", "I4", "I5"} {
		if values, present := profile.Interface[field]; present && !reflect.DeepEqual(values, []string{""}) {
			t.Fatalf("%s must stay inactive when explicitly empty", field)
		}
	}
}

func TestClashFull357AWGMultiplePeers(t *testing.T) {
	p := clashFull357Peers()
	delete(p, "ip")
	p["ipv6"] = "fd00:7::2"
	p["amnezia-wg-option"] = map[string]any{"version": 3, "jc": 4, "random-trailers": false, "disable-cookies": true}
	_, profile := clashFull357AWG(t, p)
	if len(profile.Peers) != 2 {
		t.Fatal("AWG import lost a peer")
	}
	for i, want := range []struct {
		endpoint string
		allowed  string
	}{
		{"first.example.invalid:51820", "10.80.0.0/16"},
		{"[2001:db8::2]:51821", "2001:db8:80::/48"},
	} {
		if !reflect.DeepEqual(profile.Peers[i]["Endpoint"], []string{want.endpoint}) ||
			!reflect.DeepEqual(profile.Peers[i]["AllowedIPs"], []string{want.allowed}) {
			t.Fatalf("AWG peer %d endpoint/routes changed", i)
		}
	}
	if !reflect.DeepEqual(profile.Interface["Address"], []string{"fd00:7::2/128"}) ||
		!reflect.DeepEqual(profile.Interface["RandomTrailers"], []string{"false"}) ||
		!reflect.DeepEqual(profile.Interface["DisableCookies"], []string{"true"}) {
		t.Fatal("AWG IPv6-only address or explicit booleans changed")
	}
}

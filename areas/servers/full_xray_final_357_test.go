package servers

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestFullXray357LastSourceFields(t *testing.T) {
	t.Run("filter-private", func(t *testing.T) {
		for _, lane := range []string{"tcp", "udp"} {
			for _, empty := range []bool{false, true} {
				t.Run(lane+"-"+strconv.FormatBool(empty), func(t *testing.T) {
					o := fullXray357Base("vless")
					settings := map[string]any{"blockedSourceIPs": []any{"192.0.2.0/24"}, "allowedSourceIPs": []any{"2001:db8::/32", "198.51.100.4"}}
					if empty {
						settings = map[string]any{"blockedSourceIPs": []any{}, "allowedSourceIPs": []any{}}
					}
					o["streamSettings"].(map[string]any)["finalmask"] = map[string]any{lane: []any{map[string]any{"type": "filter", "settings": settings}}}
					fullXray357Unavailable(t, o)
				})
			}
		}
		for _, value := range []any{true, nil, []any{1}, []any{"synthetic-private-invalid"}, []any{"192.0.2.1/999"}, []any{"2001:db8::/129"}} {
			o := fullXray357Base("vless")
			o["streamSettings"].(map[string]any)["finalmask"] = map[string]any{"tcp": []any{map[string]any{"type": "filter", "settings": map[string]any{"blockedSourceIPs": value}}}}
			fullXray357Reject(t, o)
		}
	})
	t.Run("reality-show-retained", func(t *testing.T) {
		base := func(show any) map[string]any {
			o := fullXray357Base("vless")
			s := o["streamSettings"].(map[string]any)
			delete(s, "tlsSettings")
			s["security"] = "reality"
			s["realitySettings"] = map[string]any{"serverName": "front.example.test", "publicKey": "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc", "fingerprint": "chrome", "show": show}
			return o
		}
		for _, show := range []bool{false, true} {
			t.Run(strconv.FormatBool(show), func(t *testing.T) {
				r := fullXray357Parse(t, base(show))
				if has(r, "importProfile") || connectionSupport(r)["supported"] == false {
					t.Fatal("valid diagnostic bool disabled ordinary client")
				}
				if value, ok := fullXray357At(r, "reality.show"); !ok || value != show {
					t.Fatal("explicit REALITY show lost from stored record")
				}
				if show && !strings.Contains(strings.ToLower(fullXray357JSON(t, r["importWarnings"])), "show") {
					t.Fatal("enabled REALITY diagnostics lack explicit warning")
				}
				fullXray357Roundtrip(t, r, map[string]any{"streamSettings.realitySettings.show": show})
			})
		}
		for _, value := range []any{nil, "false", 0} {
			fullXray357Reject(t, base(value))
		}
	})
	t.Run("wireguard-peer-metadata-retained", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("K", 32)))
		base := func(email, level any) map[string]any {
			return map[string]any{"protocol": "wireguard", "settings": map[string]any{"secretKey": key, "peers": []any{map[string]any{"publicKey": key, "endpoint": "peer.example.test:51820", "email": email, "level": level}}}}
		}
		for _, tc := range []struct {
			email string
			level uint64
		}{{"", 0}, {"synthetic-private-peer@example.test", 4294967295}} {
			t.Run(strconv.FormatUint(tc.level, 10), func(t *testing.T) {
				r := fullXray357Parse(t, base(tc.email, tc.level))
				if has(r, "importProfile") || connectionSupport(r)["supported"] == false {
					t.Fatal("valid server metadata disabled WireGuard client")
				}
				for path, want := range map[string]any{"wireguard.peers.0.email": tc.email, "wireguard.peers.0.level": tc.level} {
					if value, ok := fullXray357At(r, path); !ok || !jsonValueEqual(value, want) {
						t.Fatal("WireGuard peer metadata not retained")
					}
				}
				masked := fullXray357JSON(t, MaskRecord(r))
				if strings.Contains(masked, "synthetic-private") {
					t.Fatal("private WireGuard email disclosed")
				}
				if fullXray357JSON(t, r["importWarnings"]) == "null" {
					t.Fatal("ignored client-side metadata needs warning")
				}
				fullXray357Roundtrip(t, r, map[string]any{"settings.peers.0.email": tc.email, "settings.peers.0.level": tc.level})
			})
		}
		for _, pair := range [][2]any{{nil, 0}, {true, 0}, {"", nil}, {"", -1}, {"", uint64(4294967296)}, {"", "0"}} {
			fullXray357Reject(t, base(pair[0], pair[1]))
		}
	})
}

func TestFullXray357HysteriaTimeoutBounds(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"0", true}, {"2", true}, {"600", true}, {"1", false}, {"601", false}, {"-1", false}, {"9223372036854775808", false},
	} {
		for _, form := range []string{"json", "uri", "stored"} {
			t.Run(form+"-"+tc.value, func(t *testing.T) {
				o := map[string]any{"protocol": "hysteria", "settings": map[string]any{"version": 2, "address": "edge.example.test", "port": 443},
					"streamSettings": map[string]any{"network": "hysteria", "security": "tls", "tlsSettings": map[string]any{"serverName": "edge.example.test"},
						"hysteriaSettings": map[string]any{"version": 2, "auth": "synthetic-private-auth", "udpIdleTimeout": json.Number(tc.value)}}}
				var rec Record
				var err error
				switch form {
				case "json":
					rec, err = ParseLink(fullXray357JSON(t, o), "hy-timeout", nil)
				case "uri":
					rec, err = ParseLink("hysteria2://synthetic-private-auth@edge.example.test:443?udpIdleTimeout="+tc.value, "hy-timeout", nil)
				case "stored":
					rec, err = ParseLink("hysteria2://synthetic-private-auth@edge.example.test:443?udpIdleTimeout=2", "hy-timeout", nil)
					if err != nil {
						t.Fatal("base Hysteria fixture invalid")
					}
					rec["hysteria"].(map[string]any)["udpIdleTimeout"] = json.Number(tc.value)
					err = Validate(rec, "hy-timeout")
				}
				if (err == nil) != tc.valid {
					t.Fatal("Hysteria timeout disagrees with pinned core 0 or 2..600 bound")
				}
				if err != nil && strings.Contains(err.Error(), "synthetic-private") {
					t.Fatal("timeout error disclosed authentication")
				}
				if tc.valid {
					fullXray357Roundtrip(t, rec, map[string]any{"streamSettings.hysteriaSettings.udpIdleTimeout": json.Number(tc.value)})
				}
			})
		}
	}
}

// Final finite gaps of the 3.5.7 source disposition handoff. No core, storage
// service, local path, command or network is executed by these tests.
func TestFullXray357FinalDisposition(t *testing.T) {
	t.Run("vless-source-only", func(t *testing.T) {
		for _, tc := range []struct {
			field  string
			value  any
			legacy bool
		}{
			{"seed", "synthetic-private-seed", false}, {"seed", "", false},
			{"testseed", []any{0, 1, uint64(4294967295)}, false}, {"testseed", []any{}, false},
			{"testseed", []any{0, 1, uint64(4294967295)}, true}, {"testseed", []any{}, true},
		} {
			t.Run(tc.field+"-"+strconv.FormatBool(tc.legacy)+"-"+strconv.Itoa(len(fullXray357JSON(t, tc.value))), func(t *testing.T) {
				o := fullXray357Base("vless")
				s := o["settings"].(map[string]any)
				if tc.legacy {
					u := map[string]any{"id": s["id"], "encryption": s["encryption"], tc.field: tc.value}
					o["settings"] = map[string]any{"vnext": []any{map[string]any{"address": s["address"], "port": s["port"], "users": []any{u}}}}
				} else {
					s[tc.field] = tc.value
				}
				fullXray357Unavailable(t, o)
			})
		}
		for _, tc := range []struct {
			field string
			value any
		}{
			{"seed", nil}, {"seed", 1}, {"testseed", true}, {"testseed", []any{-1}}, {"testseed", []any{uint64(4294967296)}}, {"testseed", []any{"synthetic-private-value"}},
		} {
			o := fullXray357Base("vless")
			o["settings"].(map[string]any)[tc.field] = tc.value
			fullXray357Reject(t, o)
		}
	})
	t.Run("inactive-source", func(t *testing.T) {
		for _, kind := range []string{"ws-on-raw", "raw-alias", "xhttp-alias", "method-network"} {
			t.Run(kind, func(t *testing.T) {
				o := fullXray357Base("vless")
				s := o["streamSettings"].(map[string]any)
				switch kind {
				case "ws-on-raw":
					s["wsSettings"] = map[string]any{"path": "/synthetic-private-inactive", "heartbeatPeriod": 3}
				case "raw-alias":
					s["rawSettings"] = map[string]any{"header": map[string]any{"type": "none"}}
					s["tcpSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": map[string]any{"path": []any{"/synthetic-private-inactive"}}}}
				case "xhttp-alias":
					s["network"] = "xhttp"
					s["xhttpSettings"] = map[string]any{"path": "/active", "mode": "packet-up"}
					s["splithttpSettings"] = map[string]any{"path": "/synthetic-private-inactive", "mode": "stream-up"}
				case "method-network":
					s["method"], s["network"] = "raw", "ws"
				}
				fullXray357Unavailable(t, o)
			})
		}
		// Declared inactive fields do not bypass their type checks, and a
		// retained inactive block must not hide a malformed active endpoint.
		for _, bad := range []string{"inactive-type", "active-port", "active-tls"} {
			o := fullXray357Base("vless")
			s := o["streamSettings"].(map[string]any)
			s["wsSettings"] = map[string]any{"path": "/inactive", "heartbeatPeriod": 3}
			switch bad {
			case "inactive-type":
				s["wsSettings"].(map[string]any)["heartbeatPeriod"] = "synthetic-private-value"
			case "active-port":
				o["settings"].(map[string]any)["port"] = "synthetic-private-value"
			case "active-tls":
				s["tlsSettings"].(map[string]any)["minVersion"], s["tlsSettings"].(map[string]any)["maxVersion"] = "1.3", "1.2"
			}
			fullXray357Reject(t, o)
		}
	})
	t.Run("equivalent-alias-controls", func(t *testing.T) {
		o := fullXray357Base("vless")
		s := o["streamSettings"].(map[string]any)
		s["method"], s["network"] = "raw", "tcp"
		s["rawSettings"] = map[string]any{"header": map[string]any{"type": "none"}}
		s["tcpSettings"] = map[string]any{"header": map[string]any{"type": "none"}}
		r := fullXray357Parse(t, o)
		if has(r, "importProfile") {
			t.Fatal("equivalent aliases need no unavailable source")
		}
		fullXray357Roundtrip(t, r, map[string]any{"streamSettings.network": "raw"})
	})
	t.Run("native-warning-context", func(t *testing.T) {
		for _, tc := range []struct {
			network, alpn         string
			fingerprint           string
			utls, ignoredEndpoint bool
		}{
			{"masque", "h2", "chrome", true, true}, {"masque", "h2", "unsafe", false, true},
			{"masque", "h3", "chrome", false, true}, {"masque", "h2,h3", "chrome", false, true},
			{"xdrive", "h2", "chrome", true, false}, {"xdrive", "h2", "unsafe", false, false},
			{"xdrive", "h3", "chrome", false, false}, {"xdrive", "h2,h3", "chrome", true, false},
		} {
			t.Run(tc.network+"-"+tc.alpn+"-"+tc.fingerprint, func(t *testing.T) {
				o := nativeTestOutbound(tc.network)
				s := o["streamSettings"].(map[string]any)
				s["address"], s["port"] = "fronting.example.test", 8443
				tls := s["tlsSettings"].(map[string]any)
				var alpn []any
				for _, value := range strings.Split(tc.alpn, ",") {
					alpn = append(alpn, value)
				}
				tls["alpn"], tls["fingerprint"], tls["minVersion"] = alpn, tc.fingerprint, "1.2"
				r := fullXray357Parse(t, o)
				warnings := strings.ToLower(fullXray357JSON(t, r["importWarnings"]))
				if strings.Contains(warnings, "utls") != tc.utls {
					t.Fatal("native TLS warning contradicts pinned HTTP/QUIC path")
				}
				if strings.Contains(warnings, "streamsettings.address") != tc.ignoredEndpoint {
					t.Fatal("native endpoint warning contradicts pinned destination consumer")
				}
				fullXray357Roundtrip(t, r, map[string]any{"streamSettings.address": "fronting.example.test", "streamSettings.port": 8443, "streamSettings.tlsSettings.minVersion": "1.2"})
			})
		}
	})
}

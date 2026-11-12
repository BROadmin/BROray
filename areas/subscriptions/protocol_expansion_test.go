package subscriptions

import (
	"encoding/json"
	"github.com/BROadmin/broray/areas/servers"
	"strings"
	"testing"
)

func TestUnknownJSONFieldIsNamedWithoutValue(t *testing.T) {
	body := `{"outbounds":[{"protocol":"http","settings":{"address":"example.com","port":80,"secretExtension":"never-display-this"}}]}`
	ex, err := extractNodes([]byte(body), newMetadata())
	if err != nil {
		t.Fatal(err)
	}
	st, err := stageNodes("test", ex, true, nil)
	if err != nil || st == nil || st.Accepted != 1 || st.Rejected != 0 {
		t.Fatal("unknown server intent was not retained privately")
	}
	rec := st.Records[0]
	profile, ok := rec["importProfile"].(map[string]any)
	if !ok || profile["node"].(map[string]any)["settings"].(map[string]any)["secretExtension"] != "never-display-this" {
		t.Fatal("unknown field lost")
	}
	if _, e := servers.Outbound(rec); e == nil {
		t.Fatal("unknown extension executed as a stripped connection")
	}
	public, _ := json.Marshal(servers.MaskRecord(rec))
	if len(st.Warnings) == 0 || strings.Contains(strings.Join(st.Warnings, " ")+string(public), "never-display-this") {
		t.Fatal("missing private-source diagnostic or disclosed value")
	}
}

func TestWireGuardINIFraming(t *testing.T) {
	body := "; WireGuard client\n[Interface]\nPrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress = 10.10.0.2/32\n[Peer]\nPublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nEndpoint = example.com:51820\nAllowedIPs = 0.0.0.0/0\n"
	ex, err := extractNodes([]byte(body), newMetadata())
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Lines) != 1 {
		t.Fatal("INI split into nodes")
	}
	if _, err := servers.ParseLink(ex.Lines[0], "test", nil); err != nil {
		t.Fatal(err)
	}
}

func TestExtendedStreamJSON(t *testing.T) {
	for _, stream := range []string{
		`{"network":"ws","wsSettings":{"host":"front.example","headers":{"Host":"front.example","Authorization":"synthetic-secret"},"heartbeatPeriod":0},"sockopt":{"tcpFastOpen":false}}`,
		`{"network":"httpupgrade","httpupgradeSettings":{"headers":{"X-Test":"value"}}}`,
		`{"network":"grpc","grpcSettings":{"idle_timeout":0,"health_check_timeout":5,"permit_without_stream":false,"initial_windows_size":65536,"user_agent":"test"}}`,
	} {
		body := `{"outbounds":[{"protocol":"vless","settings":{"address":"example.com","port":443,"id":"test","encryption":"none"},"streamSettings":` + stream + `}]}`
		lines, ok := xrayJSONToLinks([]byte(body), 10)
		if !ok || len(lines) != 1 || lines[0] == unsupportedMarker {
			t.Fatal("extended stream JSON rejected")
		}
		r, err := servers.ParseLink(lines[0], "test", nil)
		if err != nil {
			t.Fatal(err)
		}
		out, err := servers.Outbound(r)
		if err != nil {
			t.Fatal(err)
		}
		var expected map[string]any
		_ = json.Unmarshal([]byte(stream), &expected)
		got := out["streamSettings"].(map[string]any)
		for block, v := range expected {
			if block == "network" {
				continue
			}
			for field, want := range v.(map[string]any) {
				if field == "headers" {
					delete(want.(map[string]any), "Host")
				}
				a, _ := json.Marshal(got[block].(map[string]any)[field])
				b, _ := json.Marshal(want)
				if string(a) != string(b) {
					t.Fatalf("lost field %s.%s", block, field)
				}
			}
		}
	}
}

func TestProxyJSONImport(t *testing.T) {
	for _, body := range []string{
		`{"outbounds":[{"protocol":"socks","settings":{"address":"proxy.example","port":1080,"user":"alice","pass":"secret"}}]}`,
		`{"outbounds":[{"protocol":"http","settings":{"servers":[{"address":"proxy.example","port":443,"users":[{"user":"alice","pass":"secret"}]}]},"streamSettings":{"network":"raw","security":"tls","tlsSettings":{"serverName":"front.example"}}}]}`,
	} {
		lines, ok := xrayJSONToLinks([]byte(body), 10)
		if !ok || len(lines) != 1 || lines[0] == unsupportedMarker {
			t.Fatal("supported proxy JSON rejected")
		}
		r, err := servers.ParseLink(lines[0], "proxy", nil)
		if err != nil {
			t.Fatal(err)
		}
		if r["username"] != "alice" || r["password"] != "secret" {
			t.Fatal("credentials changed")
		}
		if _, err := servers.Outbound(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWireGuardAndHysteriaJSON(t *testing.T) {
	for _, body := range []string{
		`{"outbounds":[{"protocol":"wireguard","settings":{"secretKey":"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=","address":["10.10.0.2/32"],"peers":[{"publicKey":"AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=","endpoint":"wg.example:51820","allowedIPs":["0.0.0.0/0"],"keepAlive":25}],"reserved":[1,2,3],"mtu":1420,"noKernelTun":true}}]}`,
		`{"outbounds":[{"protocol":"hysteria","settings":{"version":2,"address":"hy.example","port":443},"streamSettings":{"network":"hysteria","security":"tls","tlsSettings":{"serverName":"hy.example","alpn":["h3"]},"hysteriaSettings":{"version":2,"auth":"secret","udpIdleTimeout":41}}}]}`,
	} {
		// Native WG is intentionally carried as typed input, not a synthetic URI.
		ex, e := extractNodes([]byte(body), newMetadata())
		if e != nil {
			t.Fatal(e)
		}
		st, e := stageNodes("native-protocol", ex, true, nil)
		if e != nil || st.Accepted != 1 || st.Rejected != 0 {
			t.Fatal("supported WG/HY2 typed input rejected")
		}
		r := st.Records[0]
		if _, err := servers.Outbound(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWireGuardJSONSubscriptionStaging(t *testing.T) {
	body := `{"outbounds":[{"protocol":"wireguard","settings":{"secretKey":"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=","address":["10.10.0.2/32"],"peers":[{"publicKey":"AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=","endpoint":"wg.example:51820"}]}}]}`
	ex, e := extractNodes([]byte(body), newMetadata())
	if e != nil {
		t.Fatal(e)
	}
	st, e := stageNodes("wg", ex, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	if st.Accepted != 1 || st.Records[0]["protocol"] != "wireguard" {
		t.Fatal("WG JSON node lost")
	}
}

func TestSockoptForHysteriaAndShadowsocksJSON(t *testing.T) {
	for _, body := range []string{
		`{"outbounds":[{"protocol":"hysteria","settings":{"version":2,"address":"hy.example","port":443},"streamSettings":{"network":"hysteria","security":"tls","hysteriaSettings":{"version":2,"auth":"secret"},"sockopt":{"tcpKeepAliveIdle":0}}}]}`,
		`{"outbounds":[{"protocol":"shadowsocks","settings":{"address":"ss.example","port":443,"method":"chacha20-ietf-poly1305","password":"secret"},"streamSettings":{"sockopt":{"tcpKeepAliveIdle":0}}}]}`,
	} {
		lines, ok := xrayJSONToLinks([]byte(body), 10)
		if !ok || len(lines) != 1 || strings.HasPrefix(lines[0], unsupportedMarker) {
			t.Fatal("supported sockopt rejected")
		}
		r, e := servers.ParseLink(lines[0], "test", nil)
		if e != nil {
			t.Fatal(e)
		}
		out, e := servers.Outbound(r)
		if e != nil {
			t.Fatal(e)
		}
		st := out["streamSettings"].(map[string]any)
		sock, ok := st["sockopt"].(map[string]any)
		if !ok || !isInt(sock["tcpKeepAliveIdle"], 0, 0) {
			t.Fatal("sockopt lost")
		}
	}
}

func TestHysteriaJSONFinalMaskIsPrivate(t *testing.T) {
	body := `{"outbounds":[{"protocol":"hysteria","settings":{"version":2,"address":"hy.example","port":443},"streamSettings":{"network":"hysteria","security":"tls","hysteriaSettings":{"version":2,"auth":"synthetic-auth"},"finalmask":{"udp":[{"type":"salamander","settings":{"password":"private-mask-secret"}}]}}}]}`
	ex, e := extractNodes([]byte(body), newMetadata())
	if e != nil {
		t.Fatal(e)
	}
	st, e := stageNodes("hy", ex, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	masked, _ := json.Marshal(servers.MaskRecord(st.Records[0]))
	if strings.Contains(string(masked), "private-mask-secret") || strings.Contains(string(masked), "synthetic-auth") {
		t.Fatal("FinalMask credential exposed")
	}
	out, outErr := servers.Outbound(st.Records[0])
	if outErr != nil {
		t.Fatal(outErr)
	}
	private, _ := json.Marshal(out)
	if !strings.Contains(string(private), "private-mask-secret") {
		t.Fatal("private runtime credential discarded")
	}
}

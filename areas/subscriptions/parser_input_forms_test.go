package subscriptions

import (
	"encoding/base64"
	"encoding/json"
	"github.com/BROadmin/broray/areas/servers"
	"strings"
	"testing"
)

// Single-request manual API versus subscription envelopes. The WebUI splits
// URI lists before calling that API; list splitting has separate Node tests.
// Unsupported envelopes are negative results, not supported formats.
func TestParserInputEnvelopeMatrix(t *testing.T) {
	raw, _ := json.Marshal(parserCoverageBase("vless"))
	ordinary := string(raw)
	profile := func(out string) string { return `{"outbounds":[` + out + `]}` }
	nativeProfile := profile(nativeMasqueJSON)
	wg := "[Interface]\nPrivateKey = " + parserCoverageWGKey + "\nAddress = 192.0.2.2/32\n[Peer]\nPublicKey = " + parserCoverageWGPeerKey + "\nEndpoint = edge.example.test:51820\nAllowedIPs = 0.0.0.0/0\n"
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	uri := "vless://" + parserCoverageID + "@edge.example.test:443?encryption=none&security=none#envelope"
	cases := []struct {
		name, body   string
		manual       bool
		subscription int
	}{
		{"single-uri", uri, true, 1},
		{"base64-uri", enc(uri), false, 1},
		{"base64-uri-url-raw", base64.RawURLEncoding.EncodeToString([]byte(uri)), false, 1},
		{"ordinary-bare", ordinary, false, 0},
		{"ordinary-full", profile(ordinary), false, 1},
		{"ordinary-profile-array", "[" + profile(ordinary) + "]", false, 1},
		{"ordinary-outbound-array", "[" + ordinary + "]", false, 0},
		{"ordinary-base64-profile", enc(profile(ordinary)), false, 1},
		{"ordinary-base64-bare", enc(ordinary), false, 0},
		{"native-bare", nativeMasqueJSON, true, 0},
		{"native-full", nativeProfile, true, 1},
		{"native-profile-array", "[" + nativeProfile + "]", false, 1},
		{"native-outbound-array", "[" + nativeMasqueJSON + "]", false, 0},
		{"native-base64-profile", enc(nativeProfile), false, 1},
		{"native-base64-profile-array", enc("[" + nativeProfile + "]"), false, 1},
		{"native-base64-bare", enc(nativeMasqueJSON), false, 0},
		{"native-full-with-freedom", profile(nativeMasqueJSON + `,{"protocol":"freedom"}`), true, 1},
		{"native-full-with-blackhole", profile(nativeMasqueJSON + `,{"protocol":"blackhole"}`), true, 1},
		{"native-two-endpoints", profile(nativeMasqueJSON + "," + ordinary), false, 2},
		{"native-bom-bare", "\ufeff" + nativeMasqueJSON, true, 0},
		{"native-bom-full", "\ufeff" + nativeProfile, true, 1},
		{"wireguard-ini", wg, true, 1},
		{"wireguard-ini-comments", "# synthetic\n; comment\n" + wg, true, 1},
		{"wireguard-base64-ini", enc(wg), false, 0},
		{"json-duplicate-key", strings.Replace(nativeProfile, `"port":443`, `"port":443,"port":8443`, 1), false, 0},
		{"json-trailing-data", nativeProfile + "x", false, 0},
		{"json-two-documents", nativeProfile + nativeProfile, false, 0},
		{"json-comment", "/* comment */" + nativeProfile, false, 0},
		{"json-trailing-comma", strings.TrimSuffix(nativeProfile, "}") + ",}", false, 0},
		{"html", "<!DOCTYPE html><html>no subscription</html>", false, 0},
		{"unknown-protocol", "tuic://synthetic@example.test:443", false, 0},
		{"mtproto", "mtproto://synthetic@example.test:443", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manual, err := servers.ParseLink(tc.body, "envelope", nil)
			if (err == nil) != tc.manual {
				t.Fatalf("manual API acceptance mismatch: want %v got error %v", tc.manual, err)
			}
			ex, se := extractNodes([]byte(tc.body), newMetadata())
			var records []servers.Record
			if se == nil {
				st, e := stageNodes("envelope", ex, true, nil)
				records = st.Records
				se = e
			}
			if len(records) != tc.subscription {
				t.Fatalf("subscription count=%d want=%d error=%v", len(records), tc.subscription, se)
			}
			if tc.subscription > 0 && se != nil {
				t.Fatal("accepted nodes accompanied by fatal error", se)
			}
			if tc.manual && tc.subscription == 1 {
				a, e := servers.Outbound(manual)
				if e != nil {
					t.Fatal(e)
				}
				b, e := servers.Outbound(records[0])
				if e != nil {
					t.Fatal(e)
				}
				ab, _ := json.Marshal(a)
				bb, _ := json.Marshal(b)
				if string(ab) != string(bb) {
					t.Fatal("manual and subscription envelopes changed outbound")
				}
			}
		})
	}
}

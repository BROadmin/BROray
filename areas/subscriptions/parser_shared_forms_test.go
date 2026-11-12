package subscriptions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func sharedFormsJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal("cannot encode synthetic fixture")
	}
	return string(b)
}

func sharedFormsStage(t *testing.T, body string) *staged {
	t.Helper()
	ex, err := extractNodes([]byte(body), newMetadata())
	if err != nil {
		t.Fatalf("supported envelope rejected: %v", err)
	}
	st, err := stageNodes("shared-forms", ex, true, nil)
	if err != nil || st == nil || st.Rejected != 0 {
		t.Fatalf("supported endpoint rejected: %v", err)
	}
	return st
}

func sharedFormsSameConnection(t *testing.T, got, want servers.Record) {
	t.Helper()
	gotOut, err := servers.Outbound(got)
	if err != nil {
		t.Fatal("imported record cannot render")
	}
	wantOut, err := servers.Outbound(want)
	if err != nil {
		t.Fatal("control record cannot render")
	}
	if sharedFormsJSON(t, gotOut) != sharedFormsJSON(t, wantOut) {
		t.Fatal("envelope changed the generated outbound")
	}
	gotKey, err := servers.CanonicalKey(got)
	if err != nil {
		t.Fatal("imported record has no canonical identity")
	}
	wantKey, err := servers.CanonicalKey(want)
	if err != nil || gotKey != wantKey {
		t.Fatal("envelope changed connection identity")
	}
}

func sharedFormsNative(kind string) map[string]any {
	if kind == "masque" {
		return map[string]any{
			"protocol": "masque", "tag": "shared-native",
			"settings": map[string]any{"address": "proxy.example.test", "port": 443},
			"streamSettings": map[string]any{"network": "masque", "security": "tls",
				"tlsSettings":    map[string]any{"serverName": "proxy.example.test", "alpn": []any{"h2"}},
				"masqueSettings": map[string]any{"user": "synthetic-private-user", "pass": "synthetic-private-pass"}},
		}
	}
	return map[string]any{
		"protocol": "vless", "tag": "shared-native",
		"settings": map[string]any{"address": "drive.example.test", "port": 443, "id": parserCoverageID,
			"encryption": "mlkem768x25519plus.native.0rtt." + strings.Repeat("A", 43)},
		"streamSettings": map[string]any{"network": "xdrive", "security": "none",
			"xdriveSettings": map[string]any{"service": "google-drive", "remoteFolder": "synthetic-folder",
				"secrets": []any{"synthetic-private-client", "synthetic-private-secret", "synthetic-private-refresh"}}},
	}
}

func TestSharedImportSingletonParity(t *testing.T) {
	for _, kind := range []string{"vless", "vmess", "trojan", "shadowsocks", "http", "socks", "hysteria", "wireguard", "xdrive", "masque"} {
		t.Run(kind, func(t *testing.T) {
			var out map[string]any
			if kind == "xdrive" || kind == "masque" {
				out = sharedFormsNative(kind)
			} else {
				out = parserCoverageBase(kind)
			}
			bare := sharedFormsJSON(t, out)
			profile := sharedFormsJSON(t, map[string]any{"outbounds": []any{out}})
			control := sharedFormsStage(t, profile)
			if control.Accepted != 1 {
				t.Fatal("control did not contain one endpoint")
			}
			for _, shape := range []struct{ name, body string }{
				{"bare", bare},
				{"profile", profile},
				{"outbound-array", "[" + bare + "]"},
				{"profile-array", "[" + profile + "]"},
				{"bom-crlf", "\ufeff\r\n " + bare + "\r\n"},
				{"base64-bare", base64.StdEncoding.EncodeToString([]byte(bare))},
				{"base64-profile", base64.RawURLEncoding.EncodeToString([]byte(profile))},
				{"base64-array", base64.RawStdEncoding.EncodeToString([]byte("[" + bare + "]"))},
			} {
				t.Run(shape.name, func(t *testing.T) {
					manual, err := servers.ParseLink(shape.body, "shared-forms", nil)
					if err != nil {
						t.Fatalf("manual singleton envelope rejected: %v", err)
					}
					st := sharedFormsStage(t, shape.body)
					if st.Received != 1 || st.Parsed != 1 || st.Accepted != 1 || len(st.Records) != 1 {
						t.Fatal("singleton envelope changed item counts")
					}
					sharedFormsSameConnection(t, manual, control.Records[0])
					sharedFormsSameConnection(t, st.Records[0], control.Records[0])
				})
			}
		})
	}
}

func TestSharedImportURIAndWireGuardFraming(t *testing.T) {
	uri := "trojan://synthetic%2Bpass%40%3A%25@edge.example.test:443#Name%20%2B%20%E6%B5%8B%E8%AF%95"
	wg := "# synthetic\r\n; comment\r\n[Interface]\r\nPrivateKey = " + parserCoverageWGKey + "\r\nAddress = 192.0.2.2/32\r\n[Peer]\r\nPublicKey = " + parserCoverageWGPeerKey + "\r\nEndpoint = wg.example.test:51820\r\nAllowedIPs = 0.0.0.0/0, ::/0\r\n"
	for _, input := range []struct{ name, body string }{{"uri", uri}, {"wireguard", wg}} {
		control, err := servers.ParseLink(input.body, "shared-control", nil)
		if err != nil {
			t.Fatal("existing framing control rejected")
		}
		for _, encoding := range []struct {
			name string
			enc  *base64.Encoding
		}{{"standard", base64.StdEncoding}, {"standard-raw", base64.RawStdEncoding}, {"url", base64.URLEncoding}, {"url-raw", base64.RawURLEncoding}} {
			t.Run(input.name+"/"+encoding.name, func(t *testing.T) {
				body := encoding.enc.EncodeToString([]byte(input.body))
				body = body[:12] + "\r\n\t " + body[12:]
				manual, err := servers.ParseLink(body, "shared-forms", nil)
				if err != nil {
					t.Fatalf("encoded singleton rejected: %v", err)
				}
				st := sharedFormsStage(t, body)
				if st.Accepted != 1 {
					t.Fatal("encoded singleton split into multiple items")
				}
				sharedFormsSameConnection(t, manual, control)
				sharedFormsSameConnection(t, st.Records[0], control)
			})
		}
	}
	for _, body := range []string{sharedFormsJSON(t, []any{uri}), base64.StdEncoding.EncodeToString([]byte(sharedFormsJSON(t, []any{uri})))} {
		if _, err := servers.ParseLink(body, "shared-forms", nil); err != nil {
			t.Fatalf("singleton URI array rejected: %v", err)
		}
		if st := sharedFormsStage(t, body); st.Accepted != 1 {
			t.Fatal("singleton URI array changed count")
		}
	}
}

func TestSharedImportMixedArrayOrderAndDedup(t *testing.T) {
	uri := "trojan://synthetic@uri.example.test:443#URI"
	ordinary := parserCoverageBase("vless")
	native := sharedFormsNative("masque")
	profile := map[string]any{"remarks": "Native profile", "outbounds": []any{map[string]any{"protocol": "freedom"}, native, map[string]any{"protocol": "blackhole"}}}
	body := sharedFormsJSON(t, []any{uri, ordinary, profile, uri, ordinary})
	for _, encoded := range []bool{false, true} {
		t.Run(fmt.Sprint(encoded), func(t *testing.T) {
			input := body
			if encoded {
				input = base64.RawURLEncoding.EncodeToString([]byte(input))
			}
			ex, err := extractNodes([]byte(input), newMetadata())
			if err != nil || ex == nil || !ex.JSONImport {
				t.Fatalf("mixed JSON provenance lost: %v", err)
			}
			st, err := stageNodes("shared-forms", ex, true, nil)
			if err != nil || st.Received != 5 || st.Parsed != 5 || st.Accepted != 3 || st.Rejected != 0 {
				t.Fatalf("mixed-array accounting changed: %v", err)
			}
			for i, proto := range []string{"trojan", "vless", "masque"} {
				if st.Records[i]["protocol"] != proto {
					t.Fatal("mixed-array order changed")
				}
				parserCoverageAssert(t, map[string]any(st.Records[i]), "source.nodeIndex", i+1, false)
			}
			if strings.Contains(strings.Join(st.Warnings, " "), "synthetic-private") {
				t.Fatal("private native values escaped in warnings")
			}
			if _, err := servers.ParseLink(input, "shared-forms", nil); err == nil {
				t.Fatal("manual singleton request silently selected a mixed-array node")
			}
		})
	}
}

func TestSharedImportManualRejectsExpandedLists(t *testing.T) {
	ordinary := parserCoverageBase("vless")
	uri := "trojan://synthetic@uri.example.test:443#URI"
	multi := parserCoverageBase("vless")
	multi["settings"] = map[string]any{"vnext": []any{map[string]any{
		"address": "edge.example.test", "port": 443,
		"users": []any{map[string]any{"id": parserCoverageID, "encryption": "none"}, map[string]any{"id": "00000000-0000-4000-8000-000000000002", "encryption": "none"}},
	}}}
	for _, tc := range []struct{ name, body string }{
		{"uri-lines", uri + "\n" + uri},
		{"uri-array", sharedFormsJSON(t, []any{uri, uri})},
		{"outbound-array", sharedFormsJSON(t, []any{ordinary, ordinary})},
		{"profile", sharedFormsJSON(t, map[string]any{"outbounds": []any{ordinary, ordinary}})},
		{"expanded-bare", sharedFormsJSON(t, multi)},
		{"expanded-profile", sharedFormsJSON(t, map[string]any{"outbounds": []any{multi}})},
	} {
		for _, encoded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/base64=%v", tc.name, encoded), func(t *testing.T) {
				body := tc.body
				if encoded {
					body = base64.StdEncoding.EncodeToString([]byte(body))
				}
				if _, err := servers.ParseLink(body, "shared-forms", nil); err == nil {
					t.Fatal("manual singleton request accepted multiple expanded inputs")
				}
			})
		}
	}
	st := sharedFormsStage(t, sharedFormsJSON(t, multi))
	if st.Received != 2 || st.Accepted != 2 {
		t.Fatal("bare legacy outbound did not expand both users")
	}
}

func TestSharedImportStrictDocumentBoundary(t *testing.T) {
	uri := "trojan://synthetic@uri.example.test:443#URI"
	bare := sharedFormsJSON(t, parserCoverageBase("trojan"))
	profile := `{"outbounds":[` + bare + `]}`
	for _, tc := range []struct{ name, body string }{
		{"duplicate-key", strings.Replace(bare, `"port":443`, `"port":443,"port":8443`, 1)},
		{"trailing-document", profile + profile},
		{"trailing-uri", profile + "\n" + uri},
		{"unknown-wrapper-with-uri", "{\n\"unknown\": [\n" + uri + "\n]}"},
		{"mixed-scalar", sharedFormsJSON(t, []any{uri, 42})},
		{"mixed-empty-object", sharedFormsJSON(t, []any{uri, map[string]any{}})},
		{"nested-array", sharedFormsJSON(t, []any{[]any{uri}})},
		{"multiline-uri-string", sharedFormsJSON(t, []any{uri + "\n" + uri})},
		{"ambiguous-root", `{"protocol":"vless","outbounds":[` + bare + `]}`},
		{"unknown-wrapper", sharedFormsJSON(t, map[string]any{"links": []any{uri}})},
		{"invalid-utf8", strings.Replace(bare, "field-coverage", string([]byte{0xff}), 1)},
		{"unescaped-cr", strings.Replace(bare, "field-coverage", "field\rcoverage", 1)},
		{"unescaped-lf", strings.Replace(bare, "field-coverage", "field\ncoverage", 1)},
		{"leading-space-bom", " \ufeff" + bare},
		{"two-boms", "\ufeff\ufeff" + bare},
		{"double-base64", base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString([]byte(uri))))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := servers.ParseLink(tc.body, "shared-forms", nil); err == nil {
				t.Fatal("ambiguous/invalid manual document accepted")
			}
			if _, err := extractNodes([]byte(tc.body), newMetadata()); err == nil {
				t.Fatal("ambiguous/invalid subscription document accepted")
			}
		})
	}
}

func TestSharedImportStrictDocumentBounds(t *testing.T) {
	bare := sharedFormsJSON(t, parserCoverageBase("vless"))
	for _, tc := range []struct{ name, body string }{
		{"byte-limit", strings.Repeat(" ", 2*1024*1024) + bare},
		{"depth-limit", `{"outbounds":[` + bare + `],"metadata":` + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := servers.ParseLink(tc.body, "shared-forms", nil); err == nil {
				t.Fatal("manual document bound bypassed")
			}
			if _, err := extractNodes([]byte(tc.body), newMetadata()); err == nil {
				t.Fatal("subscription document bound bypassed")
			}
		})
	}
	uri := "trojan://synthetic@uri.example.test:443#URI"
	for _, count := range []int{maxNodes, maxNodes + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			items := make([]any, count)
			for i := range items {
				if i%2 == 0 {
					items[i] = uri
				} else {
					items[i] = parserCoverageBase("vless")
				}
			}
			ex, err := extractNodes([]byte(sharedFormsJSON(t, items)), newMetadata())
			if count > maxNodes {
				if err == nil || err.Code != "CONTENT_TOO_LARGE" {
					t.Fatal("mixed-array node budget was not enforced before dedup")
				}
				return
			}
			if err != nil {
				t.Fatalf("exact node limit rejected: %v", err)
			}
			st, err := stageNodes("shared-forms", ex, true, nil)
			if err != nil || st.Received != maxNodes || st.Accepted != 2 {
				t.Fatal("node limit or exact-key dedup changed")
			}
		})
	}
}

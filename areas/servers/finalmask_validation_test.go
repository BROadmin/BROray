package servers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Literal fixtures describe Xray v26.11.9 schemas. Only BROray fresh import,
// generation and backup are exercised; no core, script, socket or endpoint runs.
// A missing nested validator must fail rejection cases. A narrowed previously
// supported schema or a rewritten false/zero/range must fail preservation cases.
type finalMaskSchemaFixture struct {
	name, lane, kind, settings string
}

var finalMaskSchemaFixtures = []finalMaskSchemaFixture{
	{"tcp-header", "tcp", "header-custom", `{"clients":[[{"delay":0,"capture":"saved","type":"str","packet":"A"},{"delay":"0-1","rand":1,"randRange":"0-255"},{"reuse":"saved"},{"transform":{"op":"concat","args":[{"type":"str","bytes":"B"},{"reuse":"saved"},{"transform":{"op":"be16","args":[{"u64":1}]}}]}}]],"servers":[[{"type":"hex","packet":"41"}]],"errors":[[{"type":"base64","packet":"QQ=="}]]}`},
	{"tcp-fragment", "tcp", "fragment", `{"packets":"tlshello","length":"1-2","delay":0,"lengths":[1,"2-3"],"delays":[0,"1-2"],"maxSplit":2}`},
	{"tcp-sudoku", "tcp", "sudoku", `{"password":"synthetic-private-mask","ascii":"","customTable":"","custom_table":"","customTables":[],"custom_tables":[],"paddingMin":0,"padding_min":0,"paddingMax":0,"padding_max":0}`},
	{"tcp-xmc", "tcp", "xmc", `{"hostname":"game.example.test","password":"synthetic-private-mask","profiles":[{"username":"TestUser","uuid":"00000000-0000-4000-8000-000000000001","texturesValue":"e30=","texturesSignature":"AQ=="}]}`},
	{"udp-header", "udp", "header-custom", `{"mode":"prefix","client":[{"capture":"saved","type":"array","packet":[0,255]},{"rand":1,"randRange":"0-255"},{"reuse":"saved"},{"transform":{"op":"be16","args":[{"metadata":"remote.port"}]}}],"server":[{"type":"str","packet":"B"}]}`},
	{"udp-mkcp", "udp", "mkcp-legacy", `{"header":"dns","value":"mask.example.test"}`},
	{"udp-noise", "udp", "noise", `{"reset":"0-1","noise":[{"rand":0,"randRange":"0-255","type":"str","packet":"A","delay":0}],"script":""}`},
	{"udp-salamander", "udp", "salamander", `{"password":"synthetic-private-mask","packetSize":"512-1200"}`},
	{"udp-sudoku", "udp", "sudoku", `{"password":"synthetic-private-mask","ascii":"","customTable":"","custom_table":"","customTables":[],"custom_tables":[],"paddingMin":0,"padding_min":0,"paddingMax":0,"padding_max":0}`},
	{"udp-xdns", "udp", "xdns", `{"domains":[{"names":["mask.example.test"],"lenLimit":255,"labelLimit":63,"types":[1,16],"edns0":0}],"resolvers":[{"addrs":["udp://resolver.example.test:53"],"worker":1,"tlsConfig":{"serverName":"resolver.example.test","fingerprint":"chrome","alpn":["h2"]},"quicConfig":{"congestion":"reno","maxIdleTimeout":4,"keepAlivePeriod":2}}],"extraPoll":0}`},
	{"udp-xicmp", "udp", "xicmp", `{"dgram":false,"ips":["192.0.2.1","2001:db8::1"]}`},
	{"udp-realm", "udp", "realm", `{"url":"realm://synthetic-token@relay.example.test/session","stunServers":["stun.example.test:3478"],"tlsConfig":{"serverName":"relay.example.test","fingerprint":"chrome","alpn":["h2"],"allowInsecure":false,"enableSessionResumption":false,"disableSystemRoot":false,"minVersion":"1.2","maxVersion":"1.3","cipherSuites":"","curvePreferences":["X25519"],"pinnedPeerCertSha256":"","verifyPeerCertByName":"","echConfigList":"","echSockopt":{"tcpFastOpen":false,"domainStrategy":"UseIPv4"},"useSystemCA":false},"ipMode":"","portMapping":{"enabled":false,"timeout":0,"lifetime":0}}`},
	{"udp-hop", "udp", "udphop", `{"mode":"intervalremote","interval":5,"remoteIPs":["192.0.2.1","2001:db8::/64"],"remotePorts":"443,8443-8444"}`},
}

const finalMaskQuicFixture = `{"congestion":"reno","debug":false,"bbrProfile":"standard","brutalUp":"","brutalDown":"","brutalDisableLossCompensation":false,"initStreamReceiveWindow":16384,"maxStreamReceiveWindow":16384,"initConnectionReceiveWindow":16384,"maxConnectionReceiveWindow":16384,"maxIdleTimeout":4,"keepAlivePeriod":2,"disablePathMTUDiscovery":false,"disableChromeParrot":false,"disableGSO":false,"maxIncomingStreams":8,"disableStatelessReset":false}`

func finalMaskTestDecode(t *testing.T, raw string) any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		t.Fatalf("bad test fixture: %v", err)
	}
	return value
}

func finalMaskTestJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("bad test value: %v", err)
	}
	return string(b)
}

func finalMaskFixtureValue(t *testing.T, f finalMaskSchemaFixture) map[string]any {
	t.Helper()
	return map[string]any{f.lane: []any{map[string]any{
		"type": f.kind, "settings": finalMaskTestDecode(t, f.settings),
	}}}
}

func finalMaskImport(t *testing.T, mask any, network string) (Record, error) {
	t.Helper()
	return ParseLink("vless://test@example.test:443?type="+network+"&fm="+
		strings.ReplaceAll(url.QueryEscape(finalMaskTestJSON(t, mask)), "+", "%20"), "mask-schema", nil)
}

func finalMaskAssertPreserved(t *testing.T, mask any, network string) {
	t.Helper()
	want := finalMaskTestJSON(t, mask)
	r, err := finalMaskImport(t, mask, network)
	if err != nil {
		t.Fatalf("supported schema rejected: %v", err)
	}
	out, err := Outbound(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := finalMaskTestJSON(t, get(out, "streamSettings", "finalmask")); got != want {
		t.Fatal("import/generation changed a supported FinalMask value")
	}
	b, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeBackupRecord(b, "mask-schema")
	if err != nil {
		t.Fatalf("valid stored/backup mask rejected: %v", err)
	}
	out, err = Outbound(restored)
	if err != nil {
		t.Fatal(err)
	}
	if got := finalMaskTestJSON(t, get(out, "streamSettings", "finalmask")); got != want {
		t.Fatal("backup changed a supported FinalMask value")
	}
}

func finalMaskAssertRejected(t *testing.T, mask any, network string) {
	t.Helper()
	_, err := finalMaskImport(t, mask, network)
	if err == nil {
		t.Fatal("invalid nested FinalMask was accepted before save")
	}
	if strings.Contains(err.Error(), "synthetic-private") {
		t.Fatal("validation error exposed a private field value")
	}
}

func TestFinalMaskSupportedSchemaRoundtrip(t *testing.T) {
	for _, f := range finalMaskSchemaFixtures {
		t.Run(f.name, func(t *testing.T) {
			network := "raw"
			if f.lane == "udp" {
				network = "kcp"
			}
			finalMaskAssertPreserved(t, finalMaskFixtureValue(t, f), network)
		})
	}
	t.Run("quic-all-fields", func(t *testing.T) {
		finalMaskAssertPreserved(t, map[string]any{
			"quicParams": finalMaskTestDecode(t, finalMaskQuicFixture),
		}, "raw")
	})
}

// The literal fixtures, not production code, define these official typed
// members. Check array elements as well as containers. Explicit null at the
// fresh boundary differs from an absent optional field or a legacy stored null.
type finalMaskFieldProbe struct {
	path []string
	bad  any
}

func finalMaskFieldProbes(value any, path []string) []finalMaskFieldProbe {
	var probes []finalMaskFieldProbe
	switch object := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			next := append(append([]string(nil), path...), key)
			bad := any("synthetic-private-wrong-type")
			switch object[key].(type) {
			case string, []any:
				bad = true
			}
			probes = append(probes, finalMaskFieldProbe{next, bad})
			probes = append(probes, finalMaskFieldProbes(object[key], next)...)
		}
	case []any:
		for i, item := range object {
			next := append(append([]string(nil), path...), strconv.Itoa(i))
			bad := any("synthetic-private-wrong-type")
			if _, ok := item.(string); ok {
				bad = false
			}
			probes = append(probes, finalMaskFieldProbe{next, bad})
			probes = append(probes, finalMaskFieldProbes(item, next)...)
		}
	}
	return probes
}

func finalMaskSetTestPath(t *testing.T, value any, path []string, replacement any) {
	t.Helper()
	for i, part := range path {
		last := i == len(path)-1
		switch object := value.(type) {
		case map[string]any:
			if last {
				object[part] = replacement
				return
			}
			value = object[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(object) {
				t.Fatal("bad test path")
			}
			if last {
				object[index] = replacement
				return
			}
			value = object[index]
		default:
			t.Fatal("test path crosses a scalar")
		}
	}
	t.Fatal("empty test path")
}

func TestFinalMaskKnownFieldTypesAndNull(t *testing.T) {
	for _, f := range finalMaskSchemaFixtures {
		for _, probe := range finalMaskFieldProbes(finalMaskFixtureValue(t, f), nil) {
			for _, mutation := range []struct {
				name  string
				value any
			}{{"wrong-type", probe.bad}, {"null", nil}} {
				t.Run(f.name+"/"+strings.Join(probe.path, ".")+"/"+mutation.name, func(t *testing.T) {
					changed := finalMaskFixtureValue(t, f)
					finalMaskSetTestPath(t, changed, probe.path, mutation.value)
					network := "raw"
					if f.lane == "udp" {
						network = "kcp"
					}
					finalMaskAssertRejected(t, changed, network)
				})
			}
		}
	}
	for _, probe := range finalMaskFieldProbes(finalMaskTestDecode(t, finalMaskQuicFixture), nil) {
		for _, bad := range []any{probe.bad, nil} {
			t.Run("quic/"+strings.Join(probe.path, ".")+"/"+fmt.Sprint(bad == nil), func(t *testing.T) {
				quic := finalMaskTestDecode(t, finalMaskQuicFixture)
				finalMaskSetTestPath(t, quic, probe.path, bad)
				finalMaskAssertRejected(t, map[string]any{"quicParams": quic}, "raw")
			})
		}
	}
}

func TestFinalMaskUnknownFieldsAndUnsupportedTypes(t *testing.T) {
	for _, f := range finalMaskSchemaFixtures {
		t.Run(f.name+"/unknown-settings", func(t *testing.T) {
			mask := finalMaskFixtureValue(t, f)
			settings := mask[f.lane].([]any)[0].(map[string]any)["settings"].(map[string]any)
			settings["notAField"] = "synthetic-private-unknown"
			finalMaskAssertRejected(t, mask, "raw")
		})
	}
	for i, raw := range []string{
		`null`, `[]`, `{"notAField":"synthetic-private-unknown"}`,
		`{"quicParams":false}`, `{"quicParams":[]}`, `{"quicParams":null}`,
		`{"quicParams":{"notAField":"synthetic-private-unknown"}}`,
		`{"tcp":[{"type":"fragment","settings":{"length":1},"notAField":"synthetic-private-unknown"}]}`,
		// filter remains an explicit BROray exclusion; official Xray has it.
		// Recognized types in the wrong lane must also be refused.
		`{"tcp":[{"type":"filter","settings":{}}]}`,
		`{"udp":[{"type":"filter","settings":{}}]}`,
		`{"tcp":[{"type":"noise","settings":{}}]}`,
		`{"udp":[{"type":"fragment","settings":{"length":1}}]}`,
	} {
		t.Run(fmt.Sprintf("wrapper-%d", i), func(t *testing.T) {
			finalMaskAssertRejected(t, finalMaskTestDecode(t, raw), "raw")
		})
	}
}

func TestFinalMaskUpstreamBoundsAndRelations(t *testing.T) {
	// Pinned Build constraints, not guessed limits on unrestricted signed fields.
	for _, tc := range []struct {
		name, lane, kind, settings string
	}{
		{"custom-byte-range", "tcp", "header-custom", `{"clients":[[{"rand":1,"randRange":"0-256"}]]}`},
		{"custom-kind-conflict", "udp", "header-custom", `{"client":[{"rand":1,"type":"str","packet":"A"}]}`},
		{"custom-capture-name", "udp", "header-custom", `{"client":[{"capture":"bad name","type":"str","packet":"A"}]}`},
		{"custom-transform-empty-op", "udp", "header-custom", `{"client":[{"transform":{"op":"","args":[{"u64":1}]}}]}`},
		{"custom-transform-empty-args", "udp", "header-custom", `{"client":[{"transform":{"op":"concat","args":[]}}]}`},
		{"custom-transform-two-sources", "udp", "header-custom", `{"client":[{"transform":{"op":"be16","args":[{"u64":1,"metadata":"remote.port"}]}}]}`},
		{"custom-unknown-transform-member", "udp", "header-custom", `{"client":[{"transform":{"op":"be16","args":[{"u64":1,"notAField":"synthetic-private-value"}]}}]}`},
		{"custom-invalid-byte-encoding", "udp", "header-custom", `{"client":[{"type":"hex","packet":"not-hex"}]}`},
		{"custom-byte-array-overflow", "udp", "header-custom", `{"client":[{"type":"array","packet":[256]}]}`},
		{"fragment-zero-length", "tcp", "fragment", `{"length":0}`},
		{"fragment-final-array-zero", "tcp", "fragment", `{"length":1,"lengths":[1,0]}`},
		{"fragment-zero-packet-start", "tcp", "fragment", `{"packets":"0-1","length":1}`},
		{"noise-byte-range", "udp", "noise", `{"noise":[{"rand":1,"randRange":"0-256"}]}`},
		{"noise-packet-rand-conflict", "udp", "noise", `{"noise":[{"rand":1,"type":"str","packet":"A"}]}`},
		{"noise-script-conflict", "udp", "noise", `{"script":"return {}","noise":[{"type":"str","packet":"A"}]}`},
		{"mkcp-header-enum", "udp", "mkcp-legacy", `{"header":"unsupported"}`},
		{"salamander-minimum", "udp", "salamander", `{"packetSize":"0-1"}`},
		{"salamander-maximum", "udp", "salamander", `{"packetSize":"1-2049"}`},
		{"sudoku-unsigned-negative", "udp", "sudoku", `{"paddingMin":-1}`},
		{"sudoku-unsigned-overflow", "udp", "sudoku", `{"paddingMax":4294967296}`},
		{"sudoku-unsigned-fraction", "udp", "sudoku", `{"padding_min":1.5}`},

		{"xdns-extra-poll-negative", "udp", "xdns", `{"extraPoll":-1}`},
		{"xdns-extra-poll-maximum", "udp", "xdns", `{"extraPoll":4}`},
		{"xdns-domain-int32-overflow", "udp", "xdns", `{"domains":[{"names":["mask.example.test"],"lenLimit":2147483648}]}`},
		{"xdns-resolver-scheme", "udp", "xdns", `{"resolvers":[{"addrs":["ftp://resolver.example.test"]}]}`},
		{"xmc-empty-profile", "tcp", "xmc", `{"password":"synthetic-private-mask","profiles":[]}`},
		{"xmc-invalid-username", "tcp", "xmc", `{"password":"synthetic-private-mask","profiles":[{"username":"x","uuid":"00000000-0000-4000-8000-000000000001","texturesValue":"e30=","texturesSignature":"AQ=="}]}`},
		{"xicmp-invalid-ip", "udp", "xicmp", `{"ips":["not-an-ip"]}`},
		{"realm-wrong-scheme", "udp", "realm", `{"url":"https://relay.example.test/session","stunServers":["stun.example.test:3478"]}`},
		{"realm-missing-token", "udp", "realm", `{"url":"realm://relay.example.test/session","stunServers":["stun.example.test:3478"]}`},
		{"realm-invalid-stun", "udp", "realm", `{"url":"realm://token@relay.example.test/session","stunServers":["no-port"]}`},
		{"realm-portmapping-int64-overflow", "udp", "realm", `{"url":"realm://token@relay.example.test/session","stunServers":["stun.example.test:3478"],"portMapping":{"timeout":9223372036854775808}}`},
		{"hop-mode", "udp", "udphop", `{"mode":"unsupported"}`},
		{"hop-minimum-interval", "udp", "udphop", `{"mode":"intervalremote","interval":"4-5"}`},
		{"hop-invalid-ip", "udp", "udphop", `{"mode":"intervalremote","remoteIPs":["not-an-ip"]}`},
		{"hop-port-string-overflow", "udp", "udphop", `{"mode":"intervalremote","remotePorts":"65536"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := finalMaskSchemaFixture{tc.name, tc.lane, tc.kind, tc.settings}
			finalMaskAssertRejected(t, finalMaskFixtureValue(t, f), "raw")
		})
	}
	for _, tc := range []struct{ name, raw string }{
		{"congestion", `{"congestion":"unsupported"}`},
		{"bbr-profile", `{"bbrProfile":"unsupported"}`},
		{"force-brutal-needs-up", `{"congestion":"force-brutal"}`},
		{"bandwidth-minimum", `{"brutalUp":"8 bps"}`},
		{"window-minimum", `{"initStreamReceiveWindow":16383}`},
		{"window-negative", `{"maxStreamReceiveWindow":-1}`},
		{"window-fraction", `{"initConnectionReceiveWindow":16384.5}`},
		{"window-overflow", `{"maxConnectionReceiveWindow":18446744073709551616}`},
		{"idle-minimum", `{"maxIdleTimeout":3}`},
		{"idle-maximum", `{"maxIdleTimeout":121}`},
		{"keepalive-minimum", `{"keepAlivePeriod":1}`},
		{"keepalive-maximum", `{"keepAlivePeriod":61}`},
		{"streams-minimum", `{"maxIncomingStreams":7}`},
		{"streams-int64-overflow", `{"maxIncomingStreams":9223372036854775808}`},
	} {
		t.Run("quic/"+tc.name, func(t *testing.T) {
			finalMaskAssertRejected(t, map[string]any{"quicParams": finalMaskTestDecode(t, tc.raw)}, "raw")
		})
	}
}

func TestFinalMaskPreservesUpstreamNeutralAndRangeVariants(t *testing.T) {
	for i, raw := range []string{
		`{"tcp":[],"udp":[],"quicParams":{}}`,
		`{"udp":[{"type":"noise"}]}`,
		`{"tcp":[{"type":"fragment","settings":{"length":"2-1","delay":-1,"maxSplit":-1}}]}`,
		`{"tcp":[{"type":"fragment","settings":{"length":0,"lengths":[1]}}]}`,
		`{"udp":[{"type":"header-custom","settings":{"client":[{"rand":1,"randRange":"255-0"}]}}]}`,
		`{"udp":[{"type":"salamander","settings":{"password":"","packetSize":2048}}]}`,
		`{"udp":[{"type":"udphop","settings":{"mode":"intervalremote,perconnremote","interval":0}}]}`,
		`{"quicParams":{"congestion":"BBR","bbrProfile":"STANDARD","maxIdleTimeout":0,"keepAlivePeriod":0,"maxIncomingStreams":0}}`,
		`{"quicParams":{"maxIdleTimeout":120,"keepAlivePeriod":60,"maxIncomingStreams":8}}`,
	} {
		t.Run(fmt.Sprintf("variant-%d", i), func(t *testing.T) {
			finalMaskAssertPreserved(t, finalMaskTestDecode(t, raw), "raw")
		})
	}
}

func TestFinalMaskNestedDownloadUsesSameContract(t *testing.T) {
	for _, tc := range []struct {
		name, mask string
		valid      bool
	}{
		{"positive", `{"tcp":[{"type":"fragment","settings":{"length":1,"delay":0}}]}`, true},
		{"unknown-root", `{"notAField":"synthetic-private-value"}`, false},
		{"unknown-settings", `{"tcp":[{"type":"sudoku","settings":{"notAField":"synthetic-private-value"}}]}`, false},
		{"wrong-quic", `{"quicParams":"synthetic-private-value"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mask := finalMaskTestDecode(t, tc.mask)
			extra := map[string]any{"downloadSettings": map[string]any{
				"address": "download.example.test", "port": 443,
				"network": "xhttp", "security": "tls",
				"tlsSettings": map[string]any{"serverName": "download.example.test"},
				"finalmask":   mask,
			}}
			r, err := ParseLink("vless://test@example.test:443?type=xhttp&extra="+
				url.QueryEscape(finalMaskTestJSON(t, extra)), "mask-download", nil)
			if !tc.valid {
				if err == nil {
					t.Fatal("nested download FinalMask bypassed fresh-import validation")
				}
				if strings.Contains(err.Error(), "synthetic-private") {
					t.Fatal("nested validation exposed a private value")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			out, err := Outbound(r)
			if err != nil {
				t.Fatal(err)
			}
			got := get(out, "streamSettings", "xhttpSettings", "extra", "downloadSettings", "finalmask")
			if finalMaskTestJSON(t, got) != finalMaskTestJSON(t, mask) {
				t.Fatal("nested download mask changed")
			}
		})
	}
}

func TestFinalMaskXDriveH3CongestionCase(t *testing.T) {
	for _, congestion := range []string{"brutal", "BRUTAL", "BrUtAl", "force-brutal", "FORCE-BRUTAL"} {
		t.Run(congestion, func(t *testing.T) {
			out := nativeTestOutbound("xdrive")
			out["streamSettings"].(map[string]any)["finalmask"] = map[string]any{
				"quicParams": map[string]any{"congestion": congestion, "brutalUp": "1 mbps"},
			}
			_, err := ParseLink(finalMaskTestJSON(t, out), "mask-h3", nil)
			if err == nil {
				t.Fatal("XDRIVE H3 admitted a case variant of forbidden brutal congestion")
			}
		})
	}
}

// Stricter fresh import must not reject or rewrite a legacy optional null
// which Xray interprets as absent. This tests the stored/backup reader separately.
func TestFinalMaskStoredOptionalNullRemainsUnchanged(t *testing.T) {
	r, err := ParseLink("vless://test@example.test:443?type=kcp", "stored-mask", nil)
	if err != nil {
		t.Fatal(err)
	}
	mask := finalMaskTestDecode(t, `{"udp":[{"type":"noise","settings":null}],"quicParams":null}`)
	r["transport"].(map[string]any)["finalMask"] = mask
	original, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeBackupRecord(original, "stored-mask")
	if err != nil {
		t.Fatalf("legacy optional null rejected by stored reader: %v", err)
	}
	out, err := Outbound(restored)
	if err != nil {
		t.Fatal(err)
	}
	if finalMaskTestJSON(t, get(out, "streamSettings", "finalmask")) != finalMaskTestJSON(t, mask) {
		t.Fatal("stored nullable FinalMask was silently rewritten")
	}
}

func TestFinalMaskNestedTLSRejectsUnknownMembers(t *testing.T) {
	for _, f := range finalMaskSchemaFixtures {
		var paths [][]string
		switch f.kind {
		case "xdns":
			paths = [][]string{{"udp", "0", "settings", "resolvers", "0", "tlsConfig", "notAField"},
				{"udp", "0", "settings", "resolvers", "0", "quicConfig", "notAField"}}
		case "realm":
			paths = [][]string{{"udp", "0", "settings", "tlsConfig", "notAField"},
				{"udp", "0", "settings", "tlsConfig", "echSockopt", "notAField"},
				{"udp", "0", "settings", "portMapping", "notAField"}}
		default:
			continue
		}
		for _, path := range paths {
			t.Run(f.name+"/"+strings.Join(path, "."), func(t *testing.T) {
				mask := finalMaskFixtureValue(t, f)
				finalMaskSetTestPath(t, mask, path, "synthetic-private-value")
				finalMaskAssertRejected(t, mask, "kcp")
			})
		}
	}
}

func TestFinalMaskFreshLocalControlsHaveSafeFieldPaths(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
	}{
		{"masterKeyLog", "synthetic-private-log"},
		{"certificates", []any{map[string]any{"certificateFile": "synthetic-private-cert"}}},
		{"certificates", []any{map[string]any{"keyFile": "synthetic-private-key"}}},
		{"echSockopt", map[string]any{"interface": "synthetic-private-interface"}},
		{"echSockopt", map[string]any{"dialerProxy": "synthetic-private-route"}},
		{"echSockopt", map[string]any{"mark": 1}},
		{"echSockopt", map[string]any{"tproxy": "tproxy"}},
		{"echSockopt", map[string]any{"customSockopt": []any{map[string]any{"value": "synthetic-private-option"}}}},
		{"echSockopt", map[string]any{"trustedXForwardedFor": []any{"192.0.2.1"}}},
	} {
		for _, f := range finalMaskSchemaFixtures {
			if f.kind != "realm" && f.kind != "xdns" {
				continue
			}
			t.Run(f.name+"/"+tc.field+"/"+finalMaskTestJSON(t, tc.value), func(t *testing.T) {
				mask := finalMaskFixtureValue(t, f)
				settings := mask["udp"].([]any)[0].(map[string]any)["settings"].(map[string]any)
				if f.kind == "xdns" {
					settings = settings["resolvers"].([]any)[0].(map[string]any)
				}
				tls := settings["tlsConfig"].(map[string]any)
				tls[tc.field] = tc.value
				_, err := finalMaskImport(t, mask, "kcp")
				if err == nil {
					t.Fatal("fresh import accepted an ownership-sensitive nested control")
				}
				if !strings.Contains(err.Error(), tc.field) || strings.Contains(err.Error(), "synthetic-private") {
					t.Fatalf("expected safe field path for %s", tc.field)
				}
			})
		}
	}
	mask := finalMaskTestDecode(t, `{"udp":[{"type":"noise","settings":{"script":"synthetic-private-script"}}]}`)
	_, err := finalMaskImport(t, mask, "kcp")
	if err == nil || !strings.Contains(err.Error(), "script") || strings.Contains(err.Error(), "synthetic-private") {
		t.Fatal("fresh script rejection must name the field without its contents")
	}
}

func TestFinalMaskNeutralLocalControlsAndEncodingVariants(t *testing.T) {
	for _, f := range finalMaskSchemaFixtures {
		if f.kind != "realm" {
			continue
		}
		mask := finalMaskFixtureValue(t, f)
		tls := mask["udp"].([]any)[0].(map[string]any)["settings"].(map[string]any)["tlsConfig"].(map[string]any)
		tls["masterKeyLog"] = ""
		tls["rejectUnknownSni"] = false
		tls["echServerKeys"] = ""
		tls["alpn"] = "h2,http/1.1"
		tls["certificates"] = []any{map[string]any{"certificateFile": "", "keyFile": "", "certificate": []any{"inline-certificate"}, "key": []any{"inline-key"}, "usage": "verify", "ocspStapling": 0, "oneTimeLoading": false, "buildChain": false}}
		tls["echSockopt"] = map[string]any{"mark": 0, "tproxy": "off", "interface": "", "dialerProxy": "", "customSockopt": []any{}, "trustedXForwardedFor": []any{}, "acceptProxyProtocol": false, "penetrate": false, "v6only": false, "tcpFastOpen": false, "happyEyeballs": map[string]any{"prioritizeIPv6": false, "tryDelayMs": 0, "interleave": 0, "maxConcurrentTry": 0}}
		finalMaskAssertPreserved(t, mask, "kcp")
	}
	for i, raw := range []string{
		`{"udp":[{"type":"udphop","settings":{"mode":"intervalremote","remotePorts":" , "}}]}`,
		`{"udp":[{"type":"header-custom","settings":{"client":[{"type":"array","packet":"QQ=="}]}}]}`,
		`{"udp":[{"type":"noise","settings":{"noise":[{"type":"exp","packet":"<b 0X41><r 0-1><rc 1><rd 2><t><c><n>"}]}}]}`,
		`{"tcp":[{"type":"fragment","settings":{"packets":"2-0","length":1}}]}`,
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) { finalMaskAssertPreserved(t, finalMaskTestDecode(t, raw), "kcp") })
	}
	for i, raw := range []string{
		`{"udp":[{"type":"noise","settings":{"noise":[{"type":"exp","packet":"<r 2-1>"}]}}]}`,
		`{"udp":[{"type":"header-custom","settings":{"mode":"PREFIX"}}]}`,
	} {
		t.Run("rejected-"+strconv.Itoa(i), func(t *testing.T) { finalMaskAssertRejected(t, finalMaskTestDecode(t, raw), "kcp") })
	}
}

// Independent declaration fixture for nested TLSConfig (19), TLSCertConfig (8),
// SocketConfig (20), and HappyEyeballsConfig (4), pinned to Xray v26.11.9.
// These are parser preservation/type checks. Inline certificate/key strings are
// intentionally placeholders: PEM, cipher/curve/fingerprint semantics and actual
// socket effects remain core/runtime concerns. Keep this separate from the 13
// finalMaskSchemaFixtures that are also consumed by the official-core tests.
const finalMaskNestedSecurityFixture = `{
	"allowInsecure":false,
	"certificates":[{
		"certificateFile":"","certificate":["inline-certificate"],
		"keyFile":"","key":["inline-key"],"usage":"verify",
		"ocspStapling":0,"oneTimeLoading":false,"buildChain":false
	}],
	"serverName":"nested.example.test","alpn":["h2"],
	"enableSessionResumption":false,"disableSystemRoot":false,
	"minVersion":"1.2","maxVersion":"1.3","cipherSuites":"",
	"fingerprint":"chrome","rejectUnknownSni":false,
	"curvePreferences":["X25519"],"masterKeyLog":"",
	"pinnedPeerCertSha256":"","verifyPeerCertByName":"",
	"echServerKeys":"","echConfigList":"",
	"echSockopt":{
		"mark":0,"tcpFastOpen":false,"tproxy":"off",
		"acceptProxyProtocol":false,"domainStrategy":"UseIPv4","dialerProxy":"",
		"tcpKeepAliveInterval":30,"tcpKeepAliveIdle":0,"tcpCongestion":"cubic",
		"tcpWindowClamp":65536,"tcpMaxSeg":1200,"penetrate":false,
		"tcpUserTimeout":1000,"v6only":false,"interface":"","tcpMptcp":true,
		"customSockopt":[],"addressPortStrategy":"srvportonly",
		"happyEyeballs":{"prioritizeIPv6":false,"tryDelayMs":250,"interleave":1,"maxConcurrentTry":4},
		"trustedXForwardedFor":[]
	},
	"useSystemCA":false
}`

func TestFinalMaskNestedSecuritySchemaTypes(t *testing.T) {
	for _, f := range finalMaskSchemaFixtures {
		if f.kind != "realm" && f.kind != "xdns" {
			continue
		}
		prefix := []string{"udp", "0", "settings", "tlsConfig"}
		if f.kind == "xdns" {
			prefix = []string{"udp", "0", "settings", "resolvers", "0", "tlsConfig"}
		}
		t.Run(f.name+"/preserved", func(t *testing.T) {
			mask := finalMaskFixtureValue(t, f)
			finalMaskSetTestPath(t, mask, prefix, finalMaskTestDecode(t, finalMaskNestedSecurityFixture))
			finalMaskAssertPreserved(t, mask, "kcp")
		})
		for _, probe := range finalMaskFieldProbes(finalMaskTestDecode(t, finalMaskNestedSecurityFixture), nil) {
			for _, mutation := range []struct {
				name  string
				value any
			}{{"wrong-type", probe.bad}, {"null", nil}} {
				t.Run(f.name+"/"+strings.Join(probe.path, ".")+"/"+mutation.name, func(t *testing.T) {
					mask := finalMaskFixtureValue(t, f)
					finalMaskSetTestPath(t, mask, prefix, finalMaskTestDecode(t, finalMaskNestedSecurityFixture))
					path := append(append([]string(nil), prefix...), probe.path...)
					finalMaskSetTestPath(t, mask, path, mutation.value)
					finalMaskAssertRejected(t, mask, "kcp")
				})
			}
		}
	}
}

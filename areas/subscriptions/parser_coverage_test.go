package subscriptions

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

// Finite client-import contracts, not traffic or full-core acceptance evidence.
// Sources: pinned v26.11.9 infra/conf protocol/transport definitions and
// artifacts/parser-audit-20261110/{FORMATS-AUDIT.md,TRANSPORT-INVENTORY.json}.
// Audit STATIC rows are not PASS. Native MASQUE/XDRIVE, recursive FinalMask /
// downloadSettings and URI cross-products have separate tests. Expectations
// below are literal contracts; product validators are never the test oracle.
const parserCoverageID = "00000000-0000-4000-8000-000000000001"
const parserCoverageWGKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
const parserCoverageWGPeerKey = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="

type parserCoverageValue struct {
	class string
	value any
	want  any
	omit  bool
}

func pcValue(class string, value any) parserCoverageValue {
	return parserCoverageValue{class: class, value: value, want: value}
}

func pcWant(class string, value, want any) parserCoverageValue {
	return parserCoverageValue{class: class, value: value, want: want}
}

func pcOmit(class string, value any) parserCoverageValue {
	return parserCoverageValue{class: class, value: value, omit: true}
}

func pcBool() []parserCoverageValue {
	return []parserCoverageValue{pcValue("false", false), pcValue("true", true)}
}

func pcBounds(min, max int64) []parserCoverageValue {
	return []parserCoverageValue{pcValue("min", min), pcValue("max", max)}
}

func pcBadNumber(min, max int64) []parserCoverageValue {
	return []parserCoverageValue{pcValue("null", nil), pcValue("type", "1"), pcValue("fraction", 1.5), pcValue("below-min", min-1), pcValue("above-max", max+1)}
}

func pcBadString() []parserCoverageValue {
	return []parserCoverageValue{pcValue("null", nil), pcValue("type", 1), pcValue("control", "bad\ntext")}
}

func pcBadBool() []parserCoverageValue {
	return []parserCoverageValue{pcValue("null", nil), pcValue("type", "false"), pcValue("numeric", 0)}
}

// Each call returns fresh synthetic input. Keys are nonzero test bytes.
func parserCoverageBase(kind string) map[string]any {
	out := map[string]any{"tag": "field-coverage", "protocol": "vless",
		"settings":       map[string]any{"address": "edge.example.test", "port": 443, "id": parserCoverageID, "encryption": "none"},
		"streamSettings": map[string]any{"network": "raw", "security": "none"}}
	s := out["streamSettings"].(map[string]any)
	switch kind {
	case "vless", "raw":
	case "tls":
		s["security"] = "tls"
		s["tlsSettings"] = map[string]any{"serverName": "front.example.test", "fingerprint": "chrome"}
	case "reality":
		s["security"] = "reality"
		s["realitySettings"] = map[string]any{"serverName": "front.example.test", "fingerprint": "chrome", "publicKey": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), "shortId": "0123456789abcdef", "spiderX": "/"}
	case "ws", "httpupgrade", "grpc", "kcp", "xhttp":
		s["network"] = kind
		s[kind+"Settings"] = map[string]any{}
		if kind == "xhttp" {
			s["xhttpSettings"] = map[string]any{"path": "/upload", "mode": "packet-up", "sessionIDLength": 8}
		}
	case "raw-header":
		s["rawSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": map[string]any{"version": "1.1", "method": "GET", "path": []any{"/one", "/two"}, "headers": map[string]any{"X-Probe": []any{"one", "two"}}}, "response": map[string]any{"version": "1.1", "status": "200", "reason": "OK", "headers": map[string]any{"X-Probe": []any{"one", "two"}}}}}
	case "vmess":
		out["protocol"] = kind
		out["settings"] = map[string]any{"address": "edge.example.test", "port": 443, "id": parserCoverageID, "security": "auto"}
	case "trojan":
		out = parserCoverageBase("tls")
		out["protocol"] = kind
		out["settings"] = map[string]any{"address": "edge.example.test", "port": 443, "password": "synthetic-pass:@%/"}
	case "shadowsocks":
		out["protocol"] = kind
		out["settings"] = map[string]any{"address": "edge.example.test", "port": 443, "method": "chacha20-ietf-poly1305", "password": "synthetic-pass:@%/"}
		delete(out, "streamSettings")
	case "http", "socks":
		out["protocol"] = kind
		out["settings"] = map[string]any{"address": "edge.example.test", "port": 443, "user": "user:@%/", "pass": "synthetic-pass:@%/"}
	case "wireguard":
		out["protocol"] = kind
		out["settings"] = map[string]any{"secretKey": parserCoverageWGKey, "address": []any{"192.0.2.2/32"}, "peers": []any{map[string]any{"publicKey": parserCoverageWGPeerKey, "endpoint": "wg.example.test:51820", "allowedIPs": []any{"0.0.0.0/0", "::/0"}}}}
		delete(out, "streamSettings")
	case "hysteria":
		out["protocol"] = kind
		out["settings"] = map[string]any{"version": 2, "address": "edge.example.test", "port": 443}
		out["streamSettings"] = map[string]any{"network": "hysteria", "security": "tls", "tlsSettings": map[string]any{"serverName": "front.example.test", "alpn": []any{"h3"}}, "hysteriaSettings": map[string]any{"version": 2, "auth": "synthetic-auth:@%/"}}
	default:
		panic("unknown synthetic coverage fixture")
	}
	return out
}

func parserCoverageGet(root any, path string) (any, bool) {
	v := root
	for _, key := range strings.Split(path, ".") {
		switch m := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = m[key]
			if !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(m) {
				return nil, false
			}
			v = m[i]
		default:
			return nil, false
		}
	}
	return v, true
}

func parserCoverageSet(root map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	var current any = root
	for _, part := range parts[:len(parts)-1] {
		switch m := current.(type) {
		case map[string]any:
			if _, exists := m[part]; !exists {
				m[part] = map[string]any{}
			}
			current = m[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(m) {
				panic("bad coverage array path")
			}
			current = m[i]
		default:
			panic("bad coverage object path")
		}
	}
	current.(map[string]any)[parts[len(parts)-1]] = value
}

func parserCoverageStage(t *testing.T, out map[string]any, accept bool) (*staged, map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"outbounds": []any{out}})
	if err != nil {
		t.Fatal(err)
	}
	ex, parseErr := extractNodes(body, newMetadata())
	if parseErr != nil {
		t.Fatalf("valid document framing failed: %v", parseErr)
	}
	st, stageErr := stageNodes("field-coverage", ex, true, nil)
	if st == nil {
		t.Fatal("missing staged result")
	}
	if !accept {
		if stageErr == nil || stageErr.Code != "NO_VALID_NODES" || st.Accepted != 0 || st.Rejected != 1 || len(st.Records) != 0 {
			t.Fatalf("expected one explicit rejection; accepted=%d rejected=%d error=%v", st.Accepted, st.Rejected, stageErr)
		}
		return st, nil
	}
	if stageErr != nil || st.Accepted != 1 || st.Rejected != 0 || len(st.Records) != 1 {
		t.Fatalf("supported input rejected: accepted=%d rejected=%d error=%v warnings=%v", st.Accepted, st.Rejected, stageErr, st.Warnings)
	}
	rendered, err := servers.Outbound(st.Records[0])
	if err != nil {
		t.Fatalf("accepted input cannot render: %v", err)
	}
	return st, rendered
}

func parserCoverageAssert(t *testing.T, out map[string]any, path string, want any, omit bool) {
	t.Helper()
	got, found := parserCoverageGet(out, path)
	if omit {
		if found {
			t.Fatalf("metadata/default field unexpectedly rendered: %s", path)
		}
		return
	}
	a, errA := json.Marshal(got)
	b, errB := json.Marshal(want)
	if !found || errA != nil || errB != nil || !bytes.Equal(a, b) {
		// Never print connection values, even for this synthetic corpus.
		t.Fatalf("field lost or changed: %s (present=%t)", path, found)
	}
}

func parserCoverageField(t *testing.T, schema, base, path, output string, good, bad []parserCoverageValue) {
	t.Helper()
	if output == "" {
		output = path
	}
	for _, tc := range good {
		t.Run(schema+"/"+strings.TrimPrefix(path, "streamSettings.")+"/"+tc.class, func(t *testing.T) {
			input := parserCoverageBase(base)
			parserCoverageSet(input, path, tc.value)
			_, out := parserCoverageStage(t, input, true)
			parserCoverageAssert(t, out, output, tc.want, tc.omit)
		})
	}
	for _, tc := range bad {
		t.Run(schema+"/"+strings.TrimPrefix(path, "streamSettings.")+"/reject-"+tc.class, func(t *testing.T) {
			input := parserCoverageBase(base)
			parserCoverageSet(input, path, tc.value)
			parserCoverageStage(t, input, false)
		})
	}
}

func TestParserFieldCoverageTransport(t *testing.T) {
	field := func(schema, base, path, output string, good, bad []parserCoverageValue) {
		parserCoverageField(t, schema, base, "streamSettings."+path, output, good, bad)
	}
	for _, tc := range []struct{ key, value string }{
		{"serverName", "other.example.test"}, {"fingerprint", "firefox"},
		{"echConfigList", "AA=="}, {"verifyPeerCertByName", "one.example.test,two.example.test"},
	} {
		// ECH is opaque at Config.Build: preservation is not negotiation proof.
		field("TLS", "tls", "tlsSettings."+tc.key, "", []parserCoverageValue{pcValue("value", tc.value)}, pcBadString())
	}
	field("TLS", "tls", "tlsSettings.allowInsecure", "", []parserCoverageValue{pcOmit("false-equivalent-default", false)}, append(pcBadBool(), pcValue("removed-true", true)))
	field("TLS", "tls", "tlsSettings.alpn", "", []parserCoverageValue{pcValue("ordered-list", []any{"h2", "http/1.1"}), pcOmit("empty-list", []any{})}, []parserCoverageValue{pcValue("null", nil), pcValue("string-instead-of-array", "h2"), pcValue("element-type", []any{3}), pcValue("empty-element", []any{""}), pcValue("element-over-255", []any{strings.Repeat("a", 256)}), pcValue("comma-element", []any{"h2,http/1.1"})})
	pin := strings.Repeat("ab", 32)
	field("TLS", "tls", "tlsSettings.pinnedPeerCertSha256", "", []parserCoverageValue{pcValue("hex32", pin), pcWant("openssl-uppercase", strings.TrimSuffix(strings.Repeat("AB:", 32), ":"), pin), pcValue("two-pins", pin+","+strings.Repeat("cd", 32))}, append(pcBadString(), pcValue("short", strings.Repeat("a", 63)), pcValue("not-hex", strings.Repeat("z", 64))))
	for _, tc := range []struct{ key, value string }{
		{"serverName", "other.example.test"}, {"fingerprint", "firefox"},
		{"publicKey", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))},
		{"shortId", "ab12"}, {"spiderX", "/spider?x=1"},
		{"mldsa65Verify", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 1952))},
	} {
		field("REALITY", "reality", "realitySettings."+tc.key, "", []parserCoverageValue{pcValue("value", tc.value)}, pcBadString())
	}
	field("REALITY", "reality", "realitySettings.show", "", []parserCoverageValue{pcOmit("false-metadata", false), pcOmit("true-metadata", true)}, pcBadBool())
	for _, key := range []string{"tcpKeepAliveIdle", "tcpKeepAliveInterval"} {
		field("Socket", "raw", "sockopt."+key, "", pcBounds(0, 2147483647), pcBadNumber(0, 2147483647))
	}
	field("Socket", "raw", "sockopt.tcpFastOpen", "", append(pcBool(), pcBounds(0, 2147483647)...), pcBadNumber(0, 2147483647))
	var strategies []parserCoverageValue
	for _, strategy := range []string{"AsIs", "UseIP", "UseIPv4", "UseIPv6", "UseIPv4v6", "UseIPv6v4", "ForceIP", "ForceIPv4", "ForceIPv6", "ForceIPv4v6", "ForceIPv6v4"} {
		strategies = append(strategies, pcValue(strategy, strategy))
	}
	field("Socket", "raw", "sockopt.domainStrategy", "", strategies, append(pcBadString(), pcValue("unknown-enum", "Other")))
	for _, network := range []string{"ws", "httpupgrade"} {
		for _, tc := range []struct{ key, value string }{{"host", "front.example.test"}, {"path", "/socket?ed=2048"}} {
			field(network, network, network+"Settings."+tc.key, "", []parserCoverageValue{pcValue("value", tc.value)}, pcBadString())
		}
		field(network, network, network+"Settings.headers", "", []parserCoverageValue{pcValue("two-headers", map[string]any{"Authorization": "synthetic-token", "X-Probe": "one"})}, []parserCoverageValue{pcValue("null", nil), pcValue("type", []any{}), pcValue("value-type", map[string]any{"X-Probe": 1}), pcValue("invalid-token", map[string]any{"Bad Name": "one"}), pcValue("case-duplicate", map[string]any{"X-Probe": "one", "x-probe": "two"}), pcValue("control", map[string]any{"X-Probe": "one\ntwo"})})
	}
	field("WS", "ws", "wsSettings.heartbeatPeriod", "", pcBounds(0, 2147483647), pcBadNumber(0, 2147483647))
	for _, tc := range []struct{ key, value string }{{"authority", "front.example.test"}, {"serviceName", "rpc.Service"}, {"user_agent", "synthetic-agent/1"}} {
		field("GRPC", "grpc", "grpcSettings."+tc.key, "", []parserCoverageValue{pcValue("value", tc.value)}, pcBadString())
	}
	for _, key := range []string{"multiMode", "permit_without_stream"} {
		field("GRPC", "grpc", "grpcSettings."+key, "", pcBool(), pcBadBool())
	}
	for _, key := range []string{"idle_timeout", "health_check_timeout", "initial_windows_size"} {
		field("GRPC", "grpc", "grpcSettings."+key, "", pcBounds(0, 2147483647), pcBadNumber(0, 2147483647))
	}
	for _, tc := range []struct {
		key      string
		min, max int64
	}{{"mtu", 21, 65507}, {"tti", 10, 1000}, {"uplinkCapacity", 0, 4294967295}, {"downlinkCapacity", 0, 4294967295}, {"cwndMultiplier", 1, 4294967295}, {"maxSendingWindow", 1350, 4294967295}} {
		field("KCP", "kcp", "kcpSettings."+tc.key, "", pcBounds(tc.min, tc.max), pcBadNumber(tc.min, tc.max))
	}
	for _, part := range []string{"request", "response"} {
		keys := []string{"version"}
		if part == "request" {
			keys = append(keys, "method")
		} else {
			keys = append(keys, "status", "reason")
		}
		for _, key := range keys {
			value := map[string]string{"version": "1.0", "method": "POST", "status": "201", "reason": "Created"}[key]
			field("RAW", "raw-header", "rawSettings.header."+part+"."+key, "", []parserCoverageValue{pcValue("value", value)}, pcBadString())
		}
		field("RAW", "raw-header", "rawSettings.header."+part+".headers", "", []parserCoverageValue{pcValue("string-list", map[string]any{"X-Probe": []any{"one", "two"}}), pcValue("scalar-string", map[string]any{"X-Probe": "one"})}, []parserCoverageValue{pcValue("null", nil), pcValue("type", []any{}), pcValue("element-type", map[string]any{"X-Probe": []any{1}})})
	}
	field("RAW", "raw-header", "rawSettings.header.request.path", "", []parserCoverageValue{pcValue("string-list", []any{"/one", "/two"}), pcValue("scalar-string", "/one")}, []parserCoverageValue{pcValue("null", nil), pcValue("type", 7), pcValue("element-type", []any{false})})
}

func TestParserFieldCoverageXHTTP(t *testing.T) {
	field := func(schema, key string, good, bad []parserCoverageValue) {
		output := "streamSettings.xhttpSettings.extra." + key
		if key == "host" || key == "path" || key == "mode" {
			output = "streamSettings.xhttpSettings." + key
		}
		parserCoverageField(t, schema, "xhttp", "streamSettings.xhttpSettings."+key, output, good, bad)
	}
	for _, tc := range []struct{ key, value string }{{"host", "front.example.test"}, {"path", "/upload?probe=1"}, {"mode", "packet-up"}, {"xPaddingKey", "pad"}, {"xPaddingHeader", "X-Padding"}, {"sessionIDKey", "sid"}, {"seqKey", "seq"}, {"uplinkDataKey", "payload"}} {
		field("XHTTP", tc.key, []parserCoverageValue{pcValue("value", tc.value)}, pcBadString())
	}
	for _, tc := range []struct {
		key    string
		values []string
	}{
		{"xPaddingPlacement", []string{"", "cookie", "header", "query", "queryInHeader"}},
		{"xPaddingMethod", []string{"", "repeat-x", "tokenish"}},
		{"sessionIDPlacement", []string{"", "path", "cookie", "header", "query"}},
		{"seqPlacement", []string{"", "path", "cookie", "header", "query"}},
		{"uplinkDataPlacement", []string{"", "auto", "body", "cookie", "header"}},
	} {
		var values []parserCoverageValue
		for i, value := range tc.values {
			values = append(values, pcValue(fmt.Sprintf("enum-%d", i), value))
		}
		field("XHTTP", tc.key, values, append(pcBadString(), pcValue("unknown-enum", "invalid")))
	}
	field("XHTTP", "uplinkHTTPMethod", []parserCoverageValue{pcValue("POST", "POST"), pcValue("GET-in-packet-up", "GET")}, pcBadString())
	field("XHTTP", "sessionIDTable", []parserCoverageValue{pcValue("predefined", "Base62"), pcValue("custom", "0123456789abcdef")}, append(pcBadString(), pcValue("one-symbol", "a"), pcValue("non-ascii", "абвг"), pcValue("insufficient-space", "ab")))
	for _, key := range []string{"xPaddingObfsMode", "noGRPCHeader", "noSSEHeader"} {
		field("XHTTP", key, pcBool(), pcBadBool())
	}
	for _, key := range []string{"xPaddingBytes", "scMaxEachPostBytes", "scMinPostsIntervalMs", "scStreamUpServerSecs", "sessionIDLength", "uplinkChunkSize"} {
		field("XHTTP", key, []parserCoverageValue{pcValue("zero", 0), pcValue("max", 2147483647), pcValue("numeric-string", "16"), pcValue("ascending-range", "16-32")}, []parserCoverageValue{pcValue("null", nil), pcValue("type", false), pcValue("fraction", 1.5), pcValue("negative", -1), pcValue("above-max", int64(2147483648)), pcValue("reversed-range", "32-16"), pcValue("malformed-range", "1-2-3")})
	}
	field("XHTTP", "xPaddingBytes", nil, []parserCoverageValue{pcValue("zero-to-positive", "0-8")})
	for _, key := range []string{"scMaxBufferedPosts", "serverMaxHeaderBytes"} {
		field("XHTTP", key, pcBounds(0, 2147483647), pcBadNumber(0, 2147483647))
	}
	field("XHTTP", "headers", []parserCoverageValue{pcValue("string-map", map[string]any{"X-Probe": "one"})}, []parserCoverageValue{pcValue("null", nil), pcValue("type", []any{}), pcValue("value-type", map[string]any{"X-Probe": 1}), pcValue("host-conflict", map[string]any{"Host": "front.example.test"}), pcValue("control", map[string]any{"X-Probe": "one\ntwo"})})
	for _, key := range []string{"maxConcurrency", "maxConnections", "cMaxReuseTimes", "hMaxRequestTimes", "hMaxReusableSecs"} {
		field("XMux", "xmux."+key, []parserCoverageValue{pcValue("zero", 0), pcValue("max", 2147483647), pcValue("numeric-string", "16"), pcValue("ascending-range", "16-32")}, []parserCoverageValue{pcValue("null", nil), pcValue("type", false), pcValue("fraction", 1.5), pcValue("negative", -1), pcValue("above-max", int64(2147483648)), pcValue("reversed-range", "32-16")})
	}
	field("XMux", "xmux.hKeepAlivePeriod", []parserCoverageValue{pcValue("zero", 0), pcValue("minus-one", -1), pcValue("int64-min", json.Number("-9223372036854775808")), pcValue("int64-max", json.Number("9223372036854775807"))}, []parserCoverageValue{pcValue("null", nil), pcValue("type", "1"), pcValue("fraction", 1.5), pcValue("below-int64", json.Number("-9223372036854775809")), pcValue("above-int64", json.Number("9223372036854775808"))})
	t.Run("XMux/maxConcurrency-maxConnections/reject-both-positive", func(t *testing.T) {
		in := parserCoverageBase("xhttp")
		parserCoverageSet(in, "streamSettings.xhttpSettings.xmux", map[string]any{"maxConcurrency": "1-4", "maxConnections": 1})
		parserCoverageStage(t, in, false)
	})
	t.Run("XHTTP/extra/equivalent-wrapper", func(t *testing.T) {
		in := parserCoverageBase("xhttp")
		x := in["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
		delete(x, "sessionIDLength")
		extra := map[string]any{"headers": map[string]any{"X-Probe": "one"}, "sessionIDLength": "8-12", "sessionIDTable": "Base62", "noGRPCHeader": false, "xmux": map[string]any{"maxConnections": 2, "hKeepAlivePeriod": -1}}
		x["extra"] = extra
		_, out := parserCoverageStage(t, in, true)
		parserCoverageAssert(t, out, "streamSettings.xhttpSettings.extra", extra, false)
	})
	t.Run("XHTTP/extra/reject-mixed-top-level", func(t *testing.T) {
		in := parserCoverageBase("xhttp")
		parserCoverageSet(in, "streamSettings.xhttpSettings.extra", map[string]any{"noSSEHeader": false})
		parserCoverageStage(t, in, false)
	})
}

func TestParserFieldCoverageProtocol(t *testing.T) {
	for _, proto := range []string{"vless", "vmess", "trojan", "shadowsocks", "http", "socks", "hysteria"} {
		endpoint := "settings.servers.0."
		if proto == "vless" || proto == "vmess" {
			endpoint = "settings.vnext.0."
		}
		if proto == "hysteria" {
			endpoint = "settings."
		}
		ipv6Output := "2001:db8::10"
		if proto == "vless" || proto == "vmess" {
			// Xray common/net.ParseAddress strips brackets before net.ParseIP;
			// ipv6Address.String itself includes brackets. Both retain IP bytes.
			ipv6Output = "[2001:db8::10]"
		}
		parserCoverageField(t, proto, proto, "settings.address", endpoint+"address", []parserCoverageValue{pcValue("dns", "other.example.test"), pcValue("ipv4", "192.0.2.10"), pcWant("ipv6", "2001:db8::10", ipv6Output), pcWant("bracketed-ipv6", "[2001:db8::10]", ipv6Output)}, append(pcBadString(), pcValue("empty", "")))
		parserCoverageField(t, proto, proto, "settings.port", endpoint+"port", pcBounds(1, 65535), pcBadNumber(1, 65535))
		for _, form := range []string{"flat", "legacy"} {
			if proto == "hysteria" && form == "legacy" {
				continue
			}
			t.Run(proto+"/settings/"+form+"-all-fields", func(t *testing.T) {
				in := parserCoverageBase(proto)
				s := in["settings"].(map[string]any)
				address, port := s["address"], s["port"]
				account := map[string]any{}
				for k, v := range s {
					if k != "address" && k != "port" {
						account[k] = v
					}
				}
				var expected map[string]any
				switch proto {
				case "vless", "vmess":
					if form == "legacy" {
						in["settings"] = map[string]any{"vnext": []any{map[string]any{"address": address, "port": port, "users": []any{account}}}}
					}
					user := map[string]any{"id": parserCoverageID, "encryption": "none"}
					if proto == "vmess" {
						user = map[string]any{"id": parserCoverageID, "security": "auto", "alterId": 0}
					}
					expected = map[string]any{"vnext": []any{map[string]any{"address": "edge.example.test", "port": 443, "users": []any{user}}}}
				case "http", "socks":
					if form == "legacy" {
						in["settings"] = map[string]any{"servers": []any{map[string]any{"address": address, "port": port, "users": []any{account}}}}
					}
					expected = map[string]any{"servers": []any{map[string]any{"address": "edge.example.test", "port": 443, "users": []any{map[string]any{"user": "user:@%/", "pass": "synthetic-pass:@%/"}}}}}
				case "trojan", "shadowsocks":
					if form == "legacy" {
						in["settings"] = map[string]any{"servers": []any{s}}
					}
					server := map[string]any{"address": "edge.example.test", "port": 443, "password": "synthetic-pass:@%/"}
					if proto == "shadowsocks" {
						server["method"] = "chacha20-ietf-poly1305"
					}
					expected = map[string]any{"servers": []any{server}}
				case "hysteria":
					expected = map[string]any{"address": "edge.example.test", "port": 443, "version": 2}
				}
				st, out := parserCoverageStage(t, in, true)
				parserCoverageAssert(t, out, "settings", expected, false)
				parserCoverageAssert(t, out, "protocol", proto, false)
				parserCoverageAssert(t, out, "tag", "proxy", false)
				if st.Records[0]["name"] != "field-coverage" {
					t.Fatal("outbound tag not retained as record name")
				}
			})
		}
	}
	for _, proto := range []string{"vless", "vmess"} {
		parserCoverageField(t, proto, proto, "settings.id", "settings.vnext.0.users.0.id", []parserCoverageValue{pcValue("uuid", "00000000-0000-4000-8000-000000000002")}, append(pcBadString(), pcValue("empty", "")))
	}
	for _, security := range []string{"auto", "aes-128-gcm", "chacha20-poly1305", "none"} {
		parserCoverageField(t, "VMess", "vmess", "settings.security", "settings.vnext.0.users.0.security", []parserCoverageValue{pcValue(security, security)}, nil)
	}
	parserCoverageField(t, "VMess", "vmess", "settings.security", "", nil, append(pcBadString(), pcValue("unknown-cipher", "invalid")))
	parserCoverageField(t, "VMess", "vmess", "settings.alterId", "settings.vnext.0.users.0.alterId", []parserCoverageValue{pcValue("compat-zero", 0)}, []parserCoverageValue{pcValue("null", nil), pcValue("type", "0"), pcValue("nonzero", 1)})
	parserCoverageField(t, "VMess", "vmess", "settings.experiments", "settings.vnext.0.users.0.experiments", []parserCoverageValue{pcOmit("empty-default", "")}, []parserCoverageValue{pcValue("null", nil), pcValue("type", false)})
	parserCoverageField(t, "VLESS", "vless", "settings.encryption", "settings.vnext.0.users.0.encryption", []parserCoverageValue{pcValue("none", "none")}, append(pcBadString(), pcValue("unknown", "invalid")))
	for _, proto := range []string{"trojan", "shadowsocks"} {
		parserCoverageField(t, proto, proto, "settings.password", "settings.servers.0.password", []parserCoverageValue{pcValue("percent-sensitive", "synthetic-@:/?#%")}, append(pcBadString(), pcValue("empty", "")))
	}
	parserCoverageField(t, "Trojan", "trojan", "settings.flow", "settings.servers.0.flow", []parserCoverageValue{pcOmit("empty-default", "")}, []parserCoverageValue{pcValue("null", nil), pcValue("type", 0)})
	for _, method := range []string{"aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "chacha20-ietf-poly1305", "xchacha20-poly1305"} {
		parserCoverageField(t, "Shadowsocks", "shadowsocks", "settings.method", "settings.servers.0.method", []parserCoverageValue{pcValue(method, method)}, nil)
	}
	for _, tc := range []struct {
		method string
		bytes  int
	}{{"2022-blake3-aes-128-gcm", 16}, {"2022-blake3-aes-256-gcm", 32}, {"2022-blake3-chacha20-poly1305", 32}} {
		t.Run("Shadowsocks/method/"+tc.method, func(t *testing.T) {
			in := parserCoverageBase("shadowsocks")
			key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, tc.bytes))
			parserCoverageSet(in, "settings.method", tc.method)
			parserCoverageSet(in, "settings.password", key)
			_, out := parserCoverageStage(t, in, true)
			parserCoverageAssert(t, out, "settings.servers.0.method", tc.method, false)
			parserCoverageAssert(t, out, "settings.servers.0.password", key, false)
		})
	}
}

func TestParserFieldCoverageMetadata(t *testing.T) {
	// Ordinary VLESS shares the explicit level=0/email warning policy used by
	// the other protocols and native VLESS. Metadata is accepted with a warning
	// and omitted from the generated connection settings.
	for _, proto := range []string{"vless", "vmess", "trojan", "shadowsocks", "http", "socks", "wireguard"} {
		path := "settings."
		if proto == "wireguard" {
			path = "settings.peers.0."
		}
		for _, tc := range []struct {
			key   string
			value any
		}{{"level", 0}, {"email", "coverage@example.test"}} {
			t.Run(proto+"/"+tc.key+"/accepted-with-warning", func(t *testing.T) {
				in := parserCoverageBase(proto)
				parserCoverageSet(in, path+tc.key, tc.value)
				st, out := parserCoverageStage(t, in, true)
				if !strings.Contains(strings.Join(st.Warnings, " "), tc.key) {
					t.Fatal("metadata loss lacks warning")
				}
				settings, _ := json.Marshal(out["settings"])
				if bytes.Contains(settings, []byte(`"`+tc.key+`":`)) {
					t.Fatal("metadata unexpectedly rendered")
				}
			})
		}
		parserCoverageField(t, proto, proto, path+"level", "", nil, []parserCoverageValue{pcValue("policy-nonzero", 1), pcValue("null", nil), pcValue("type", "0"), pcValue("negative", -1)})
		parserCoverageField(t, proto, proto, path+"email", "", nil, pcBadString())
	}
	for _, tc := range []struct {
		base, path, word string
		value            any
	}{{"reality", "streamSettings.realitySettings.show", "show", true}, {"grpc", "streamSettings.grpcSettings.mode", "mode", false}} {
		t.Run(tc.base+"/"+tc.word+"/explicit-warning", func(t *testing.T) {
			in := parserCoverageBase(tc.base)
			parserCoverageSet(in, tc.path, tc.value)
			st, out := parserCoverageStage(t, in, true)
			if !strings.Contains(strings.Join(st.Warnings, " "), tc.word) {
				t.Fatal("metadata warning missing")
			}
			parserCoverageAssert(t, out, tc.path, nil, true)
		})
	}
}

func TestParserFieldCoverageProxy(t *testing.T) {
	for _, proto := range []string{"http", "socks"} {
		for _, key := range []string{"user", "pass"} {
			parserCoverageField(t, proto, proto, "settings."+key, "settings.servers.0.users.0."+key, []parserCoverageValue{pcValue("percent-sensitive", "synthetic-@:/?#%")}, pcBadString())
		}
		t.Run(proto+"/users/absent-no-auth", func(t *testing.T) {
			in := parserCoverageBase(proto)
			s := in["settings"].(map[string]any)
			delete(s, "user")
			delete(s, "pass")
			_, out := parserCoverageStage(t, in, true)
			parserCoverageAssert(t, out, "settings.servers.0.users", nil, true)
		})
		t.Run(proto+"/pass/reject-without-user", func(t *testing.T) {
			in := parserCoverageBase(proto)
			delete(in["settings"].(map[string]any), "user")
			parserCoverageStage(t, in, false)
		})
		parserCoverageField(t, proto, proto, "settings.pass", "settings.servers.0.users.0.pass", []parserCoverageValue{pcValue("empty-password", "")}, nil)
		for _, count := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("%s/users/count-%d", proto, count), func(t *testing.T) {
				in := parserCoverageBase(proto)
				users := []any{}
				for i := 0; i < count; i++ {
					users = append(users, map[string]any{"user": "user", "pass": "synthetic"})
				}
				in["settings"] = map[string]any{"servers": []any{map[string]any{"address": "edge.example.test", "port": 443, "users": users}}}
				_, out := parserCoverageStage(t, in, count <= 1)
				if count == 0 {
					parserCoverageAssert(t, out, "settings.servers.0.users", nil, true)
				}
				if count == 1 {
					parserCoverageAssert(t, out, "settings.servers.0.users", users, false)
				}
			})
		}
	}
	for _, key := range []string{"user", "pass"} {
		parserCoverageField(t, "SOCKS", "socks", "settings."+key, "settings.servers.0.users.0."+key, []parserCoverageValue{pcValue("255-bytes", strings.Repeat("a", 255))}, []parserCoverageValue{pcValue("256-bytes", strings.Repeat("a", 256)), pcValue("utf8-over-byte-bound", strings.Repeat("я", 128))})
	}
}

func TestParserFieldCoverageWireGuard(t *testing.T) {
	field := func(path string, good, bad []parserCoverageValue) {
		parserCoverageField(t, "WireGuard", "wireguard", "settings."+path, "", good, bad)
	}
	for _, path := range []string{"secretKey", "peers.0.publicKey", "peers.0.preSharedKey"} {
		field(path, []parserCoverageValue{pcValue("nonzero-base64", parserCoverageWGKey), pcWant("nonzero-hex", strings.Repeat("01", 32), parserCoverageWGKey), pcWant("unpadded", strings.TrimSuffix(parserCoverageWGKey, "="), parserCoverageWGKey)}, append(pcBadString(), pcValue("all-zero", base64.StdEncoding.EncodeToString(make([]byte, 32))), pcValue("31-byte", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31)))))
	}
	field("noKernelTun", []parserCoverageValue{pcValue("userspace-true", true)}, append(pcBadBool(), pcValue("kernel-false", false)))
	field("mtu", pcBounds(576, 9000), pcBadNumber(576, 9000))
	field("reserved", []parserCoverageValue{pcValue("three-bytes", []any{0, 128, 255}), pcOmit("empty-default", []any{})}, []parserCoverageValue{pcValue("null", nil), pcValue("type", "AQID"), pcValue("two-bytes", []any{0, 1}), pcValue("four-bytes", []any{0, 1, 2, 3}), pcValue("byte-overflow", []any{0, 1, 256}), pcValue("byte-type", []any{0, 1, "2"})})
	field("address", []parserCoverageValue{pcValue("dual-stack-prefix", []any{"192.0.2.2/32", "2001:db8::2/128"}), pcWant("host-to-prefix", []any{"192.0.2.2", "2001:db8::2"}, []any{"192.0.2.2/32", "2001:db8::2/128"})}, []parserCoverageValue{pcValue("null", nil), pcValue("type", "192.0.2.2"), pcValue("empty", []any{}), pcValue("element-type", []any{1}), pcValue("bad-prefix", []any{"192.0.2.2/33"})})
	field("remoteDNS", []parserCoverageValue{pcValue("dual-stack", []any{"192.0.2.53", "2001:db8::53"}), pcOmit("empty-default", []any{})}, []parserCoverageValue{pcValue("null", nil), pcValue("type", "192.0.2.53"), pcValue("element-type", []any{false}), pcValue("non-ip", []any{"dns.example.test"})})
	field("peers.0.endpoint", []parserCoverageValue{pcValue("dns-port", "other.example.test:65535"), pcValue("ipv6-port", "[2001:db8::1]:1")}, append(pcBadString(), pcValue("missing-port", "wg.example.test"), pcValue("zero-port", "wg.example.test:0"), pcValue("port-overflow", "wg.example.test:65536")))
	field("peers.0.allowedIPs", []parserCoverageValue{pcValue("dual-stack", []any{"192.0.2.0/24", "2001:db8::/32"})}, []parserCoverageValue{pcValue("null", nil), pcValue("type", "0.0.0.0/0"), pcValue("empty", []any{}), pcValue("element-type", []any{true}), pcValue("invalid-prefix", []any{"192.0.2.0/33"})})
	field("peers.0.keepAlive", pcBounds(0, 65535), pcBadNumber(0, 65535))
	t.Run("WireGuard/defaults/absent-optional-fields", func(t *testing.T) {
		in := parserCoverageBase("wireguard")
		delete(in["settings"].(map[string]any)["peers"].([]any)[0].(map[string]any), "allowedIPs")
		_, out := parserCoverageStage(t, in, true)
		parserCoverageAssert(t, out, "settings.noKernelTun", true, false)
		parserCoverageAssert(t, out, "settings.mtu", 1420, false)
		parserCoverageAssert(t, out, "settings.peers.0.allowedIPs", []any{"0.0.0.0/0", "::/0"}, false)
	})
	for _, count := range []int{2, 32, 33} {
		t.Run(fmt.Sprintf("WireGuard/peers/count-%d", count), func(t *testing.T) {
			in := parserCoverageBase("wireguard")
			peers := []any{}
			for i := 0; i < count; i++ {
				peers = append(peers, map[string]any{"publicKey": parserCoverageWGPeerKey, "endpoint": fmt.Sprintf("wg%d.example.test:51820", i), "allowedIPs": []any{fmt.Sprintf("10.0.%d.0/24", i)}})
			}
			parserCoverageSet(in, "settings.peers", peers)
			_, out := parserCoverageStage(t, in, count <= 32)
			if count <= 32 {
				parserCoverageAssert(t, out, "settings.peers", peers, false)
			}
		})
	}
}

func TestParserFieldCoverageHysteria(t *testing.T) {
	for _, path := range []string{"settings.version", "streamSettings.hysteriaSettings.version"} {
		parserCoverageField(t, "Hysteria", "hysteria", path, "", []parserCoverageValue{pcValue("version2", 2)}, []parserCoverageValue{pcValue("null", nil), pcValue("type", "2"), pcValue("version1", 1), pcValue("version3", 3)})
	}
	parserCoverageField(t, "Hysteria", "hysteria", "streamSettings.hysteriaSettings.auth", "", []parserCoverageValue{pcValue("percent-sensitive", "synthetic-@:/?#%"), pcValue("empty", "")}, pcBadString())
	parserCoverageField(t, "Hysteria", "hysteria", "streamSettings.hysteriaSettings.udpIdleTimeout", "", []parserCoverageValue{pcValue("zero-default", 0), pcValue("min-nonzero", 2), pcValue("max", 600)}, []parserCoverageValue{pcValue("null", nil), pcValue("type", "2"), pcValue("negative", -1), pcValue("one", 1), pcValue("above-max", 601), pcValue("fraction", 2.5)})
}

func TestParserFieldCoverageDeclaredUnsupported(t *testing.T) {
	// These negatives document deliberate importer limits, not unsupported
	// upstream functionality. Parent-level rejection also covers nested schemas.
	for _, tc := range []struct {
		schema, base, path string
		value              any
	}{
		{"Outbound", "vless", "sendThrough", "192.0.2.1"}, {"Outbound", "vless", "targetStrategy", "UseIPv4"},
		{"Outbound", "vless", "mux", map[string]any{"enabled": true, "concurrency": 8, "xudpConcurrency": 4, "xudpProxyUDP443": "allow"}},
		{"Outbound", "vless", "proxySettings", map[string]any{"tag": "foreign"}},
		{"Stream", "raw", "streamSettings.method", "raw"}, {"Stream", "raw", "streamSettings.address", "other.example.test"}, {"Stream", "raw", "streamSettings.port", 8443},
		{"VLESS", "vless", "settings.seed", "synthetic"}, {"VLESS", "vless", "settings.reverse", map[string]any{"tag": "foreign"}}, {"VLESS", "vless", "settings.testpre", 1}, {"VLESS", "vless", "settings.testseed", []any{1}},
		{"VMess", "vmess", "settings.experiments", "unsupported"}, {"Trojan", "trojan", "settings.flow", "xtls-rprx-vision"},
		{"HTTP", "http", "settings.headers", map[string]any{"X-Probe": "value"}},
		{"RAW", "raw", "streamSettings.rawSettings.acceptProxyProtocol", true}, {"WS", "ws", "streamSettings.wsSettings.acceptProxyProtocol", true}, {"HTTPUpgrade", "httpupgrade", "streamSettings.httpupgradeSettings.acceptProxyProtocol", true},
		{"Hysteria", "hysteria", "streamSettings.hysteriaSettings.masquerade", map[string]any{"type": "string", "content": "synthetic"}},
		{"XHTTP", "xhttp", "streamSettings.xhttpSettings.h2Params", map[string]any{"maxConcurrentStreams": 1}},
	} {
		parserCoverageField(t, tc.schema, tc.base, tc.path, "", nil, []parserCoverageValue{pcValue("declared-unsupported", tc.value)})
	}
	for key, value := range map[string]any{
		"certificates": []any{map[string]any{"certificate": []any{"synthetic"}, "usage": "verify"}}, "enableSessionResumption": true, "disableSystemRoot": true, "minVersion": "1.2", "maxVersion": "1.3", "cipherSuites": "TLS_AES_128_GCM_SHA256", "curvePreferences": []any{"X25519"}, "masterKeyLog": "synthetic.log", "echSockopt": map[string]any{"tcpFastOpen": false}, "useSystemCA": true, "rejectUnknownSni": true, "echServerKeys": "synthetic",
	} {
		parserCoverageField(t, "TLS", "tls", "streamSettings.tlsSettings."+key, "", nil, []parserCoverageValue{pcValue("declared-unsupported", value)})
	}
	for key, value := range map[string]any{
		"mark": 1, "tproxy": "tproxy", "acceptProxyProtocol": true, "dialerProxy": "foreign", "tcpCongestion": "cubic", "tcpWindowClamp": 65536, "tcpMaxSeg": 1200, "penetrate": true, "tcpUserTimeout": 1000, "v6only": true, "interface": "synthetic0", "tcpMptcp": true, "addressPortStrategy": "srvportonly", "trustedXForwardedFor": []any{"192.0.2.1"},
		"happyEyeballs": map[string]any{"prioritizeIPv6": false, "tryDelayMs": 250, "interleave": 1, "maxConcurrentTry": 2},
		"customSockopt": []any{map[string]any{"system": "linux", "network": "tcp", "level": "1", "opt": "1", "value": "1", "type": "int"}},
	} {
		parserCoverageField(t, "Socket", "raw", "streamSettings.sockopt."+key, "", nil, []parserCoverageValue{pcValue("declared-unsupported", value)})
	}
	for key, value := range map[string]any{
		"masterKeyLog": "synthetic.log", "target": "front.example.test:443", "dest": "front.example.test:443", "type": "tcp", "xver": 1, "serverNames": []any{"front.example.test"}, "privateKey": "synthetic", "minClientVer": "1.0.0", "maxClientVer": "99.0.0", "maxTimeDiff": 1000, "shortIds": []any{"abcd"}, "mldsa65Seed": "synthetic", "limitFallbackUpload": map[string]any{"AfterBytes": 1, "BytesPerSec": 1, "BurstBytesPerSec": 1}, "limitFallbackDownload": map[string]any{"AfterBytes": 1, "BytesPerSec": 1, "BurstBytesPerSec": 1},
	} {
		parserCoverageField(t, "REALITY", "reality", "streamSettings.realitySettings."+key, "", nil, []parserCoverageValue{pcValue("declared-unsupported", value)})
	}
	for _, method := range []string{"aead_aes_128_gcm", "aead_aes_256_gcm", "aead_chacha20_poly1305", "aead_xchacha20_poly1305", "xchacha20-ietf-poly1305"} {
		parserCoverageField(t, "Shadowsocks", "shadowsocks", "settings.method", "", nil, []parserCoverageValue{pcValue("declared-unsupported-"+method, method)})
	}
}

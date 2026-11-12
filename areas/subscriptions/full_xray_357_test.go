package subscriptions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

// Keep the subscription and manual entry points on one semantic contract.
// Sources are pinned to Xray-core 676eaf66f8f05d4d558ee7a74b4736623d3feeb9.
func TestFullXray357SubscriptionClientFieldParity(t *testing.T) {
	cases := []struct {
		name, base, input, output string
		value                     any
	}{
		{"tls-resumption", "tls", "streamSettings.tlsSettings.enableSessionResumption", "", true},
		{"tls-system-roots", "tls", "streamSettings.tlsSettings.disableSystemRoot", "", false},
		{"tls-version-min", "tls", "streamSettings.tlsSettings.minVersion", "", "1.2"},
		{"tls-version-max", "tls", "streamSettings.tlsSettings.maxVersion", "", "1.3"},
		{"tls-curves", "tls", "streamSettings.tlsSettings.curvePreferences", "", []any{"X25519"}},
		{"tls-ciphers", "tls", "streamSettings.tlsSettings.cipherSuites", "", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
		{"tls-ech-socket", "tls", "streamSettings.tlsSettings.echSockopt", "", map[string]any{"tcpFastOpen": false, "domainStrategy": "UseIPv4"}},
		{"tls-system-ca", "tls", "streamSettings.tlsSettings.useSystemCA", "", true},
		{"http-headers", "http", "settings.headers", "", map[string]any{"Authorization": "Bearer synthetic-private-value", "X-Profile": "357"}},
		{"vmess-experiments", "vmess", "settings.experiments", "settings.vnext.0.users.0.experiments", "AuthenticatedLength|NoTerminationSignal"},
		{"mux", "vmess", "mux", "", map[string]any{"enabled": false, "concurrency": -1, "xudpConcurrency": 0, "xudpProxyUDP443": "skip"}},
		{"target", "tls", "targetStrategy", "", "UseIPv6"},
		{"source", "tls", "sendThrough", "", "192.0.2.10"},
		{"stream-address", "xhttp", "streamSettings.address", "", "stream.example.test"},
		{"stream-port", "xhttp", "streamSettings.port", "", 8443},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := parserCoverageBase(tc.base)
			parserCoverageSet(o, tc.input, tc.value)
			st, rendered := parserCoverageStage(t, o, true)
			path := tc.output
			if path == "" {
				path = tc.input
			}
			// Both flat and legacy VMess output forms are valid.
			if tc.name == "vmess-experiments" {
				if _, found := parserCoverageGet(rendered, "settings.address"); found {
					path = "settings.experiments"
				}
			}
			parserCoverageAssert(t, rendered, path, tc.value, false)
			body, err := json.Marshal(o)
			if err != nil {
				t.Fatal("cannot encode synthetic outbound")
			}
			manual, err := servers.ParseLink(string(body), "manual-full-357", nil)
			if err != nil {
				t.Fatal("manual JSON rejected subscription-supported client semantics")
			}
			manualOut, err := servers.Outbound(manual)
			if err != nil {
				t.Fatal("manual record cannot generate outbound")
			}
			a, _ := json.Marshal(rendered)
			b, _ := json.Marshal(manualOut)
			if string(a) != string(b) {
				t.Fatal("manual and subscription JSON generate different outbound semantics")
			}
			masked, _ := json.Marshal(servers.MaskRecord(st.Records[0]))
			if strings.Contains(string(masked), "synthetic-private-value") || strings.Contains(strings.Join(st.Warnings, "\n"), "synthetic-private-value") {
				t.Fatal("subscription public output disclosed client credentials")
			}
		})
	}
}

func TestFullXray357SubscriptionMethodPrecedence(t *testing.T) {
	o := parserCoverageBase("tls")
	parserCoverageSet(o, "streamSettings.method", "ws")
	parserCoverageSet(o, "streamSettings.wsSettings", map[string]any{"path": "/socket", "host": "front.example.test"})
	fullXray357SubscriptionUnavailable(t, o)
	delete(o["streamSettings"].(map[string]any), "network")
	_, out := parserCoverageStage(t, o, true)
	network, _ := parserCoverageGet(out, "streamSettings.network")
	if method, present := parserCoverageGet(out, "streamSettings.method"); present {
		network = method
	}
	if network != "ws" && network != "websocket" {
		t.Fatal("method must override the raw network as defined by the exact core")
	}
	parserCoverageAssert(t, out, "streamSettings.wsSettings.path", "/socket", false)
}

func TestFullXray357SubscriptionRejectsInvalidOrUnresolvedFields(t *testing.T) {
	for _, tc := range []struct {
		name, base, path string
		value            any
	}{
		{"tls-null", "tls", "streamSettings.tlsSettings.enableSessionResumption", nil},
		{"tls-bool-type", "tls", "streamSettings.tlsSettings.useSystemCA", "true"},
		{"tls-unknown", "tls", "streamSettings.tlsSettings.notAField", "synthetic-private-value"},
		{"headers-null", "http", "settings.headers", nil},
		{"header-injection", "http", "settings.headers", map[string]any{"X-Test": "synthetic-private-value\r\nInjected: yes"}},
		{"experiments-type", "vmess", "settings.experiments", true},
		{"mux-null", "vmess", "mux", nil},
		{"mux-unknown", "vmess", "mux", map[string]any{"enabled": true, "notAField": "synthetic-private-value"}},
		{"mux-overflow", "vmess", "mux", map[string]any{"enabled": true, "xudpConcurrency": 32768}},
		{"target-type", "tls", "targetStrategy", 1},
		{"source-domain", "tls", "sendThrough", "synthetic-private-value.example.test"},
		{"stream-address-null", "xhttp", "streamSettings.address", nil},
		{"stream-port-null", "xhttp", "streamSettings.port", nil},
		{"stream-method-invalid", "tls", "streamSettings.method", "synthetic-private-value"},
		{"removed-allowInsecure", "tls", "streamSettings.tlsSettings.allowInsecure", true},
		{"removed-proxySettings", "tls", "proxySettings", map[string]any{"tag": "foreign-outbound"}},
		{"removed-trojan-flow", "trojan", "settings.flow", "xtls-rprx-vision"},
		{"unresolved-dialer", "tls", "streamSettings.sockopt", map[string]any{"dialerProxy": "foreign-outbound"}},
		{"unresolved-certificate", "tls", "streamSettings.tlsSettings.certificates", []any{map[string]any{"certificateFile": "/synthetic-private-value/ca.pem", "usage": "verify"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := parserCoverageBase(tc.base)
			parserCoverageSet(o, tc.path, tc.value)
			switch tc.name {
			case "tls-unknown", "mux-unknown", "removed-allowInsecure", "removed-proxySettings", "unresolved-dialer", "unresolved-certificate":
				fullXray357SubscriptionUnavailable(t, o)
				return
			}
			st, _ := parserCoverageStage(t, o, false)
			if strings.Contains(strings.Join(st.Warnings, "\n"), "synthetic-private-value") {
				t.Fatal("rejection disclosed private field value")
			}
		})
	}
}

func TestFullXray357SubscriptionNoSilentClientDeduplication(t *testing.T) {
	for _, tc := range []struct {
		name, base, path string
		value            any
	}{
		{"headers", "http", "settings.headers", map[string]any{"X-Variant": "357"}},
		{"experiments", "vmess", "settings.experiments", "AuthenticatedLength"},
		{"mux", "vmess", "mux", map[string]any{"enabled": true, "concurrency": 8}},
		{"target", "tls", "targetStrategy", "UseIPv4"},
		{"tls", "tls", "streamSettings.tlsSettings.enableSessionResumption", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, second := parserCoverageBase(tc.base), parserCoverageBase(tc.base)
			parserCoverageSet(second, tc.path, tc.value)
			body, err := json.Marshal(map[string]any{"outbounds": []any{first, second}})
			if err != nil {
				t.Fatal("cannot encode synthetic profile")
			}
			ex, e := extractNodes(body, newMetadata())
			if e != nil {
				t.Fatal("profile extraction failed")
			}
			st, e := stageNodes("full-xray-357", ex, true, nil)
			if e != nil || st == nil || st.Accepted != 2 || len(st.Records) != 2 || st.Rejected != 0 {
				t.Fatal("different client semantics were rejected or collapsed as duplicates")
			}
		})
	}
}

func TestFullXray357SubscriptionDoesNotApplyForeignProfile(t *testing.T) {
	o := parserCoverageBase("tls")
	parserCoverageSet(o, "streamSettings.tlsSettings.enableSessionResumption", true)
	body, err := json.Marshal(map[string]any{
		"outbounds": []any{o},
		"dns":       map[string]any{"servers": []any{"foreign-dns.example.test"}},
		"routing":   map[string]any{"rules": []any{map[string]any{"outboundTag": "foreign-outbound"}}},
		"inbounds":  []any{map[string]any{"listen": "0.0.0.0", "port": 12345, "protocol": "socks"}},
		"log":       map[string]any{"access": "/foreign-profile/access.log"},
	})
	if err != nil {
		t.Fatal("cannot encode synthetic profile")
	}
	ex, e := extractNodes(body, newMetadata())
	if e != nil {
		t.Fatal("client endpoint extraction failed")
	}
	st, e := stageNodes("full-xray-357", ex, true, nil)
	if e != nil || st == nil || st.Accepted != 1 || len(st.Records) != 1 {
		t.Fatal("supported client endpoint rejected")
	}
	if len(st.Warnings) == 0 {
		t.Fatal("excluded profile controls require an explicit import warning")
	}
	r := st.Records[0]
	for _, key := range []string{"dns", "routing", "inbounds", "log"} {
		if _, present := r[key]; present {
			t.Fatalf("foreign profile control copied to server record: %s", key)
		}
	}
	out, err := servers.Outbound(r)
	if err != nil {
		t.Fatal("cannot generate client outbound")
	}
	parserCoverageAssert(t, out, "streamSettings.tlsSettings.enableSessionResumption", true, false)
	for _, key := range []string{"dns", "routing", "inbounds", "log"} {
		if _, present := out[key]; present {
			t.Fatalf("foreign profile control copied to outbound: %s", key)
		}
	}
}

func fullXray357SubscriptionDownload() (map[string]any, map[string]any) {
	o := parserCoverageBase("xhttp")
	d := map[string]any{"address": "download.example.test", "port": 8443, "network": "xhttp", "security": "tls",
		"tlsSettings":   map[string]any{"serverName": "download.example.test", "fingerprint": "chrome"},
		"xhttpSettings": map[string]any{"path": "/download", "mode": "stream-one"}}
	x := o["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
	delete(x, "sessionIDLength")
	x["extra"] = map[string]any{"downloadSettings": d}
	return o, d
}

func TestFullXray357SubscriptionDownloadClientParity(t *testing.T) {
	const prefix = "streamSettings.xhttpSettings.extra.downloadSettings."
	for _, tc := range []struct {
		name, path string
		value      any
	}{
		{"tls-resumption", "tlsSettings.enableSessionResumption", true},
		{"tls-version", "tlsSettings.minVersion", "1.2"},
		{"tls-ech-socket", "tlsSettings.echSockopt", map[string]any{"tcpFastOpen": false, "domainStrategy": "UseIPv4"}},
		{"socket", "sockopt", map[string]any{"tcpFastOpen": false, "domainStrategy": "UseIPv6"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, d := fullXray357SubscriptionDownload()
			parserCoverageSet(d, tc.path, tc.value)
			_, out := parserCoverageStage(t, o, true)
			parserCoverageAssert(t, out, prefix+tc.path, tc.value, false)
		})
	}
	t.Run("method-precedence", func(t *testing.T) {
		o, d := fullXray357SubscriptionDownload()
		d["network"], d["method"] = "raw", "xhttp"
		fullXray357SubscriptionUnavailable(t, o)
		d["network"] = "splithttp" // Equivalent spelling is runnable.
		_, out := parserCoverageStage(t, o, true)
		network, _ := parserCoverageGet(out, prefix+"network")
		if method, found := parserCoverageGet(out, prefix+"method"); found {
			network = method
		}
		if network != "xhttp" && network != "splithttp" {
			t.Fatal("download method precedence changed")
		}
	})
	for _, tc := range []struct {
		name, path string
		value      any
	}{
		{"tls-null", "tlsSettings", nil},
		{"tls-type", "tlsSettings.enableSessionResumption", "synthetic-private-value"},
		{"socket-null", "sockopt", nil},
		{"dialer-reference", "sockopt", map[string]any{"dialerProxy": "synthetic-private-value"}},
		{"method-null", "method", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, d := fullXray357SubscriptionDownload()
			parserCoverageSet(d, tc.path, tc.value)
			if tc.name == "dialer-reference" {
				fullXray357SubscriptionUnavailable(t, o)
				return
			}
			st, _ := parserCoverageStage(t, o, false)
			if strings.Contains(strings.Join(st.Warnings, "\n"), "synthetic-private-value") {
				t.Fatal("download rejection disclosed private data")
			}
		})
	}
}

// A valid but unavailable source is accepted into private storage; neither
// normal generation nor backup restoration may activate its projection.
func fullXray357SubscriptionUnavailable(t *testing.T, source map[string]any) *staged {
	t.Helper()
	body, err := json.Marshal(map[string]any{"outbounds": []any{source}})
	if err != nil {
		t.Fatal("synthetic source encoding failed")
	}
	ex, parseErr := extractNodes(body, newMetadata())
	if parseErr != nil {
		t.Fatal("unavailable source framing rejected")
	}
	st, stageErr := stageNodes("full-xray-357", ex, true, nil)
	if stageErr != nil || st == nil || st.Accepted != 1 || st.Rejected != 0 || len(st.Records) != 1 {
		t.Fatal("valid unavailable source was not retained")
	}
	fullXray357SubscriptionUnavailableRecord(t, st.Records[0], source)
	if strings.Contains(strings.Join(st.Warnings, " "), "synthetic-private") {
		t.Fatal("unavailable warning disclosed private source")
	}
	return st
}

func fullXray357SubscriptionUnavailableRecord(t *testing.T, record servers.Record, source map[string]any) {
	t.Helper()
	parserCoverageAssert(t, record, "importProfile.node", source, false)
	if err := servers.Validate(record, "private-source"); err != nil {
		t.Fatal("private source record invalid")
	}
	if _, err := servers.Outbound(record); err == nil {
		t.Fatal("unavailable source emitted a reduced outbound")
	}
	if _, err := servers.XrayConfig(record, servers.GenSettings{}, "private-source"); err == nil {
		t.Fatal("unavailable source emitted executable configuration")
	}
	masked, _ := json.Marshal(servers.MaskRecord(record))
	if strings.Contains(string(masked), "synthetic-private") || strings.Contains(string(masked), "importProfile") {
		t.Fatal("private source escaped masking")
	}
	encoded, err := servers.Marshal(record)
	if err != nil {
		t.Fatal("private source backup failed")
	}
	restored, err := servers.DecodeBackupRecord(encoded, "private-source")
	if err != nil {
		t.Fatal("private source backup rejected")
	}
	parserCoverageAssert(t, restored, "importProfile.node", source, false)
	if _, err := servers.Outbound(restored); err == nil {
		t.Fatal("restored private source became runnable")
	}
}

func TestFullXray357SubscriptionVLESSPreconnect(t *testing.T) {
	o := parserCoverageBase("vless")
	parserCoverageSet(o, "settings.testpre", 2)
	_, out := parserCoverageStage(t, o, true)
	path := "settings.testpre"
	if _, flat := parserCoverageGet(out, "settings.address"); !flat {
		path = "settings.vnext.0.users.0.testpre"
	}
	parserCoverageAssert(t, out, path, 2, false)
}

func TestFullXray357SubscriptionWireGuardSourceDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, path  string
		value, want any
	}{
		{"zero-mtu", "settings.mtu", 0, 1420},
		{"empty-allowedIPs", "settings.peers.0.allowedIPs", []any{}, []any{}},
		{"kernel-preference", "settings.noKernelTun", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := parserCoverageBase("wireguard")
			parserCoverageSet(o, tc.path, tc.value)
			_, out := parserCoverageStage(t, o, true)
			parserCoverageAssert(t, out, tc.path, tc.want, false)
		})
	}
}

func TestFullXray357SubscriptionXHTTPPrecedenceAndInt64(t *testing.T) {
	o := parserCoverageBase("xhttp")
	x := o["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)
	x["noSSEHeader"] = true
	x["extra"] = map[string]any{"path": "/ignored", "mode": "stream-one", "noSSEHeader": false, "scMaxBufferedPosts": json.Number("2147483648")}
	_, out := parserCoverageStage(t, o, true)
	parserCoverageAssert(t, out, "streamSettings.xhttpSettings.path", "/upload", false)
	parserCoverageAssert(t, out, "streamSettings.xhttpSettings.mode", "packet-up", false)
	parserCoverageAssert(t, out, "streamSettings.xhttpSettings.extra.noSSEHeader", false, false)
	parserCoverageAssert(t, out, "streamSettings.xhttpSettings.extra.scMaxBufferedPosts", json.Number("2147483648"), false)
}

func TestFullXray357SubscriptionProtocolStreamParity(t *testing.T) {
	for _, tc := range []struct{ protocol, network string }{
		{"http", "ws"}, {"http", "xhttp"}, {"socks", "grpc"}, {"socks", "xhttp"},
		{"shadowsocks", "raw"}, {"shadowsocks", "ws"}, {"shadowsocks", "httpupgrade"},
	} {
		t.Run(tc.protocol+"-"+tc.network, func(t *testing.T) {
			o := parserCoverageBase(tc.protocol)
			s := map[string]any{"network": tc.network, "security": "tls", "tlsSettings": map[string]any{"serverName": "front.example.test", "fingerprint": "chrome"}}
			o["streamSettings"] = s
			switch tc.network {
			case "ws", "httpupgrade":
				s[tc.network+"Settings"] = map[string]any{"path": "/transport", "headers": map[string]any{"X-Stream": "357"}}
			case "grpc":
				s["grpcSettings"] = map[string]any{"serviceName": "transport"}
			case "xhttp":
				s["xhttpSettings"] = map[string]any{"path": "/transport", "mode": "packet-up"}
			}
			_, out := parserCoverageStage(t, o, true)
			parserCoverageAssert(t, out, "streamSettings.network", tc.network, false)
			parserCoverageAssert(t, out, "streamSettings.security", "tls", false)
			parserCoverageAssert(t, out, "streamSettings.tlsSettings.serverName", "front.example.test", false)
		})
	}
}

func TestFullXray357SubscriptionAccountEmail(t *testing.T) {
	const email = "synthetic-private-value@example.test"
	for _, protocol := range []string{"vless", "vmess", "trojan", "shadowsocks", "http", "socks"} {
		t.Run(protocol, func(t *testing.T) {
			o := parserCoverageBase(protocol)
			parserCoverageSet(o, "settings.email", email)
			st, out := parserCoverageStage(t, o, true)
			path := "settings.servers.0.email"
			if protocol == "vless" || protocol == "vmess" {
				path = "settings.vnext.0.users.0.email"
			}
			if protocol == "http" || protocol == "socks" {
				path = "settings.servers.0.users.0.email"
			}
			if _, flat := parserCoverageGet(out, "settings.address"); flat {
				path = "settings.email"
			}
			parserCoverageAssert(t, out, path, email, false)
			public, _ := json.Marshal(servers.MaskRecord(st.Records[0]))
			if strings.Contains(string(public), email) {
				t.Fatal("subscription account email exposed")
			}
		})
	}
}

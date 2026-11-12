package xrayschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyUnexecutableClientIntent(t *testing.T) {
	cases := []struct{ name, raw, path, kind string }{
		{"mark", `{"protocol":"http","settings":{"address":"proxy.invalid","port":443},"streamSettings":{"sockopt":{"mark":42,"tcpFastOpen":true}}}`, "Xray.streamSettings.sockopt.mark", "local-control"},
		{"interface", `{"protocol":"http","streamSettings":{"sockopt":{"interface":"private-interface"}}}`, "Xray.streamSettings.sockopt.interface", "local-control"},
		{"dialer", `{"protocol":"vless","streamSettings":{"sockopt":{"dialerProxy":"private-outbound-tag"}}}`, "Xray.streamSettings.sockopt.dialerProxy", "profile-dependency"},
		{"script", `{"protocol":"vless","streamSettings":{"finalmask":{"udp":[{"type":"noise","settings":{"script":"private code"}}]}}}`, "Xray.streamSettings.finalmask.udp[].settings.script", "local-execution"},
		{"file", `{"protocol":"http","streamSettings":{"tlsSettings":{"certificates":[{"certificateFile":"/private/cert","keyFile":"/private/key"}]}}}`, "Xray.streamSettings.tlsSettings.certificates[]", "local-resource"},
		{"download", `{"protocol":"vless","streamSettings":{"network":"xhttp","xhttpSettings":{"extra":{"downloadSettings":{"address":"download.invalid","port":443,"sockopt":{"interface":"private-interface"}}}}}}`, "Xray.streamSettings.xhttpSettings.extra.downloadSettings.sockopt.interface", "local-control"},
		{"reverse", `{"protocol":"vless","settings":{"reverse":{"tag":"private-tag","sniffing":{"enabled":true}}}}`, "Xray.settings.reverse", "profile-dependency"},
		{"unknown", `{"protocol":"http","settings":{"private-extra":"secret"}}`, "Xray.settings.<extension>", "unknown-extension"},
		{"legacy-reverse", `{"protocol":"vless","settings":{"vnext":[{"address":"proxy.invalid","port":443,"users":[{"id":"fixture","reverse":{"tag":"private-tag"}}]}]}}`, "Xray.settings.vnext[].users[].reverse", "profile-dependency"},
		{"h2-listener", `{"protocol":"vless","streamSettings":{"network":"xhttp","xhttpSettings":{"h2Params":{"MaxConcurrentStreams":42}}}}`, "Xray.streamSettings.xhttpSettings.h2Params", "listener-only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, e := Classify(object(t, tc.raw))
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, r := range a.Reasons {
				if r.Path == tc.path && r.Kind == tc.kind {
					found = true
				}
			}
			if !found {
				t.Fatal("missing explicit unsupported intent", a.Reasons)
			}
			original, _ := json.Marshal(a.Source.Fields)
			projection, _ := json.Marshal(a.Projection)
			if string(original) == string(projection) {
				t.Fatal("unsupported fields not detached for validation")
			}
			for _, r := range a.Reasons {
				if strings.Contains(r.Path, "private") || strings.Contains(r.Kind, "secret") {
					t.Fatal("private diagnostics")
				}
			}
		})
	}
}

func TestClassifyVMessObsoleteZeroCompatibility(t *testing.T) {
	for _, settings := range []string{
		`{"address":"proxy.invalid","port":443,"id":"fixture","alterId":0}`,
		`{"vnext":[{"address":"proxy.invalid","port":443,"users":[{"id":"fixture","alterId":0}]}]}`,
	} {
		a, err := Classify(object(t, `{"protocol":"vmess","settings":`+settings+`}`))
		if err != nil || len(a.Reasons) != 0 {
			t.Fatal("obsolete zero must not disable existing AEAD VMess exports")
		}
		for _, bad := range []string{"1", "\"0\"", "null", "true"} {
			if _, err := Classify(object(t, `{"protocol":"vmess","settings":`+strings.Replace(settings, `"alterId":0`, `"alterId":`+bad, 1)+`}`)); err == nil {
				t.Fatal("nonzero or malformed obsolete field accepted")
			}
		}
	}
}

func TestClassifyNeverHidesMalformedKnownFields(t *testing.T) {
	for _, s := range []string{
		`{"protocol":"http","settings":{"port":"bad","private-extra":true}}`,
		`{"protocol":"http","streamSettings":{"sockopt":{"mark":2147483648}}}`,
		`{"protocol":"http","streamSettings":{"tlsSettings":{"certificates":[{"keyFile":"/private/key"}]}}}`,
		`{"protocol":"vless","settings":{"reverse":{"tag":null}}}`,
	} {
		if _, e := Classify(object(t, s)); e == nil {
			t.Fatal("invalid known value hidden as capability")
		}
	}
}

func TestClassifyNeutralDoesNotDisableOrdinaryClient(t *testing.T) {
	a, e := Classify(object(t, `{"protocol":"http","settings":{"address":"example.invalid","port":443},"streamSettings":{"sockopt":{"mark":0,"interface":"","penetrate":false,"customSockopt":[],"tcpFastOpen":false}}}`))
	if e != nil || len(a.Reasons) != 0 {
		t.Fatal("neutral typed control disabled", e)
	}
	if a.Projection["streamSettings"].(map[string]any)["sockopt"].(map[string]any)["tcpFastOpen"] != false {
		t.Fatal("false lost")
	}
}

func TestClassifyDetachedCertificateStillValidatesInlineMaterial(t *testing.T) {
	for _, certificate := range []string{
		`{"usage":"issue","certificate":["not PEM"],"key":["not a key"]}`,
		`{"certificateFile":"/unread/provider/cert","certificate":["not PEM"],"keyFile":"/unread/provider/key"}`,
		`{"certificateFile":"/unread/provider/cert","key":["not a key"]}`,
		`{"certificateFile":"/unread/provider/cert","key":[""]}`,
		`{"usage":"issue","certificateFile":"/unread/provider/cert"}`,
	} {
		input := object(t, `{"protocol":"http","streamSettings":{"security":"tls","tlsSettings":{"certificates":[`+certificate+`]}}}`)
		if _, err := Classify(input); err == nil {
			t.Fatal("malformed inline TLS material hidden by unavailable source")
		}
	}
	input := object(t, `{"protocol":"http","streamSettings":{"security":"tls","tlsSettings":{"certificates":[{"certificateFile":"/unread/provider/cert","keyFile":"/unread/provider/key"}]}}}`)
	if a, err := Classify(input); err != nil || len(a.Reasons) != 1 {
		t.Fatal("file-only source must remain unresolved without accessing files")
	}
	for _, empty := range []string{"[]", "null"} {
		input := object(t, `{"protocol":"http","streamSettings":{"security":"tls","tlsSettings":{"certificates":[{"certificateFile":"/unread/provider/cert","keyFile":"/unread/provider/key","certificate":`+empty+`,"key":`+empty+`}]}}}`)
		if a, err := Classify(input); err != nil || len(a.Reasons) != 1 {
			t.Fatal("empty inline arrays must fall back to unresolved file source")
		}
	}
}

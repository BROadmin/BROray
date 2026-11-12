package subscriptions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

// Synthetic provider shape only; no live provider material belongs in fixtures.
func hysteriaMasquerade356() map[string]any {
	out := parserCoverageBase("hysteria")
	parserCoverageSet(out, "streamSettings.hysteriaSettings.udpIdleTimeout", 60)
	parserCoverageSet(out, "streamSettings.tlsSettings.echConfigList", "AA==")
	parserCoverageSet(out, "streamSettings.tlsSettings.fingerprint", "chrome")
	parserCoverageSet(out, "streamSettings.hysteriaSettings.masquerade", map[string]any{
		"type": "proxy", "url": "https://masquerade.example.test/synthetic-private-target",
		"dir": "", "content": "", "headers": map[string]any{},
		"insecure": false, "rewriteHost": true, "statusCode": 0, "xForwarded": true,
	})
	return out
}

func hysteriaDocument356(t *testing.T, outbounds ...any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"outbounds": outbounds})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func assertMasqueradeWarning356(t *testing.T, warnings []string) {
	t.Helper()
	text := strings.Join(warnings, " ")
	if !strings.Contains(text, "masquerade") || !strings.Contains(text, "сервер") {
		t.Fatal("missing explicit server-only masquerade warning")
	}
	for _, private := range []string{"synthetic-", "example.test", "AA==", "https://"} {
		if strings.Contains(text, private) {
			t.Fatal("warning contains a connection or masquerade value")
		}
	}
}

func TestHysteriaMasquerade356ProviderShape(t *testing.T) {
	body := hysteriaDocument356(t, hysteriaMasquerade356())
	for _, tc := range []struct{ name, body string }{
		{"json", string(body)},
		{"profile-array", "[" + string(body) + "]"},
		{"base64-json", base64.StdEncoding.EncodeToString(body)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex, err := extractNodes([]byte(tc.body), newMetadata())
			if err != nil {
				t.Fatal("provider-shaped document extraction failed", err)
			}
			st, err := stageNodes(subX, ex, true, nil)
			if err != nil || st.Received != 1 || st.Parsed != 1 || st.Accepted != 1 || st.Rejected != 0 {
				t.Fatalf("server-only masquerade rejected: received=%d parsed=%d accepted=%d rejected=%d error=%v", st.Received, st.Parsed, st.Accepted, st.Rejected, err)
			}
			out, renderErr := servers.Outbound(st.Records[0])
			if renderErr != nil {
				t.Fatal("accepted Hysteria node cannot render", renderErr)
			}
			for path, want := range map[string]any{
				"settings.address": "edge.example.test", "settings.port": 443,
				"settings.version": 2, "streamSettings.network": "hysteria", "streamSettings.security": "tls",
				"streamSettings.hysteriaSettings.version": 2, "streamSettings.hysteriaSettings.auth": "synthetic-auth:@%/",
				"streamSettings.hysteriaSettings.udpIdleTimeout": 60, "streamSettings.tlsSettings.serverName": "front.example.test",
				"streamSettings.tlsSettings.alpn": []any{"h3"}, "streamSettings.tlsSettings.echConfigList": "AA==",
				"streamSettings.tlsSettings.fingerprint": "chrome",
			} {
				parserCoverageAssert(t, out, path, want, false)
			}
			parserCoverageAssert(t, out, "streamSettings.hysteriaSettings.masquerade", nil, true)
			stored, _ := json.Marshal(st.Records[0])
			if strings.Contains(string(stored), "masquerade") || strings.Contains(ex.Lines[0], "masquerade") {
				t.Fatal("server masquerade leaked into client record or share link")
			}
			assertMasqueradeWarning356(t, st.Warnings)
		})
	}
}

func TestHysteriaMasquerade356Schema(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		accept bool
	}{
		{"default", map[string]any{}, true},
		{"empty-type", map[string]any{"type": ""}, true},
		{"string", map[string]any{"type": "string", "content": "synthetic-server-content", "headers": map[string]any{"Content-Type": "text/plain"}, "statusCode": 200}, true},
		{"multiline-content", map[string]any{"type": "string", "content": "synthetic-server-content\nsecond line"}, true},
		{"file", map[string]any{"type": "file", "dir": "/synthetic-private-directory"}, true},
		{"statusCode-int32-min", map[string]any{"statusCode": -2147483648}, true},
		{"statusCode-int32-max", map[string]any{"statusCode": 2147483647}, true},
		{"null", nil, false}, {"wrong-object-type", "proxy", false},
		{"unknown-field", map[string]any{"type": "proxy", "secretExtension": "synthetic-private-value"}, false},
		{"unknown-type", map[string]any{"type": "future-mode"}, false},
		{"type-number", map[string]any{"type": 1}, false},
		{"url-number", map[string]any{"type": "proxy", "url": 1}, false},
		{"dir-object", map[string]any{"type": "file", "dir": map[string]any{}}, false},
		{"content-null", map[string]any{"type": "string", "content": nil}, false},
		{"headers-array", map[string]any{"type": "string", "headers": []any{}}, false},
		{"header-value-number", map[string]any{"type": "string", "headers": map[string]any{"X-Test": 1}}, false},
		{"insecure-string", map[string]any{"type": "proxy", "insecure": "false"}, false},
		{"rewriteHost-number", map[string]any{"type": "proxy", "rewriteHost": 1}, false},
		{"xForwarded-null", map[string]any{"type": "proxy", "xForwarded": nil}, false},
		{"statusCode-string", map[string]any{"type": "string", "statusCode": "200"}, false},
		{"statusCode-fraction", map[string]any{"type": "string", "statusCode": 200.5}, false},
		{"statusCode-below-int32", map[string]any{"type": "string", "statusCode": int64(-2147483649)}, false},
		{"statusCode-above-int32", map[string]any{"type": "string", "statusCode": int64(2147483648)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := hysteriaMasquerade356()
			parserCoverageSet(out, "streamSettings.hysteriaSettings.masquerade", tc.value)
			st, rendered := parserCoverageStage(t, out, tc.accept)
			if tc.accept {
				parserCoverageAssert(t, rendered, "streamSettings.hysteriaSettings.masquerade", nil, true)
				assertMasqueradeWarning356(t, st.Warnings)
			}
		})
	}
	t.Run("absent", func(t *testing.T) {
		st, _ := parserCoverageStage(t, parserCoverageBase("hysteria"), true)
		if strings.Contains(strings.Join(st.Warnings, " "), "masquerade") {
			t.Fatal("absent masquerade produced an omission warning")
		}
	})
}

func TestHysteriaMasquerade356StageDocuments(t *testing.T) {
	var links []string
	var outbounds []any
	for port := 443; port <= 447; port++ {
		outbounds = append(outbounds, map[string]any{"protocol": "trojan",
			"settings":       map[string]any{"address": "edge.example.test", "port": port, "password": "synthetic-pass"},
			"streamSettings": map[string]any{"security": "tls"}})
		if port < 447 {
			links = append(links, fmt.Sprintf("trojan://synthetic-pass@edge.example.test:%d?security=tls", port))
		}
	}
	outbounds = append(outbounds, hysteriaMasquerade356())
	docs := []sourceDocument{{body: []byte(strings.Join(links, "\n"))}, {body: hysteriaDocument356(t, outbounds...), format: "json"}}
	st, err := stageDocuments(subX, docs, newMetadata(), true, nil)
	if err != nil || st.Received != 10 || st.Parsed != 10 || st.Accepted != 6 || st.Rejected != 0 {
		t.Fatalf("complete union failed: received=%d parsed=%d accepted=%d rejected=%d error=%v", st.Received, st.Parsed, st.Accepted, st.Rejected, err)
	}
	assertMasqueradeWarning356(t, st.Warnings)
	// A genuine unsupported client field still aborts the whole advertised export.
	parserCoverageSet(outbounds[5].(map[string]any), "streamSettings.hysteriaSettings.futureClientOption", "synthetic-private-value")
	docs[1].body = hysteriaDocument356(t, outbounds...)
	st, err = stageDocuments(subX, docs, newMetadata(), true, nil)
	if err == nil || err.Code != "ALTERNATE_EXPORT_ERROR" || st.Received != 10 || st.Parsed != 9 || st.Rejected != 1 || st.Accepted != 4 {
		t.Fatal("genuine JSON failure no longer aborts the union")
	}
}

func TestHysteriaMasquerade356AtomicRefresh(t *testing.T) {
	e := newEnv(t)
	p := newAutoFixture355(t)
	body := hysteriaDocument356(t, hysteriaMasquerade356())
	p.change(func() { p.jsonBody = string(body) })
	id := e.create(t, p.URL+"/sub/invented", "")
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal("initial complete refresh failed", err)
	}
	before := snapshotAutoCatalog355(t, e)
	for _, malformed := range []string{
		strings.Replace(string(body), `"statusCode":0`, `"statusCode":"bad"`, 1),
		strings.Replace(string(body), `"statusCode":0`, `"statusCode":0,"statusCode":200`, 1),
		string(body) + `{}`,
		strings.Replace(string(body), `"udpIdleTimeout":60`, `"udpIdleTimeout":60,"futureClientOption":true`, 1),
	} {
		p.change(func() { p.jsonBody = malformed })
		if err := e.refreshSync(t, id); err == nil {
			t.Fatal("malformed advertised export was committed")
		}
		if !reflect.DeepEqual(before, snapshotAutoCatalog355(t, e)) {
			t.Fatal("failed export changed the catalog")
		}
	}
}

package servers

import (
	"encoding/json"
	"strings"
	"testing"
)

func assertNativeUnchanged(t *testing.T, o map[string]any) {
	t.Helper()
	r := parseNativeFixture(t, o)
	b, e := Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	restored, e := DecodeBackupRecord(b, "native-test")
	if e != nil {
		t.Fatal(e)
	}
	result, e := Outbound(restored)
	if e != nil {
		t.Fatal(e)
	}
	o["tag"] = "proxy"
	if !jsonValueEqual(o, result) {
		t.Fatal("field matrix changed private outbound")
	}
	public, _ := json.Marshal(MaskRecord(restored))
	if strings.Contains(string(public), "synthetic-private") {
		t.Fatal("field matrix leaked private values")
	}
}

func TestNativeOptionalFieldMatrix(t *testing.T) {
	for _, kind := range []string{"google-drive", "s3", "template", "masque"} {
		t.Run(kind, func(t *testing.T) {
			out := nativeBackendFixture(t, kind)
			st := out["streamSettings"].(map[string]any)
			st["sockopt"] = map[string]any{"tcpFastOpen": false, "tcpKeepAliveIdle": 0, "tcpKeepAliveInterval": 30, "domainStrategy": "UseIPv4"}
			block := "xdriveSettings"
			if kind == "masque" {
				block = "masqueSettings"
			}
			get(out, "streamSettings", block).(map[string]any)["xmux"] = map[string]any{"maxConcurrency": 0, "maxConnections": "1-2", "cMaxReuseTimes": 3, "hMaxRequestTimes": "4-5", "hMaxReusableSecs": "6", "hKeepAlivePeriod": -1}
			if kind == "template" {
				x := get(out, "streamSettings", block, "template").(map[string]any)
				x["flatten"] = true
				x["concurrency"] = 17
				for _, op := range []string{"put", "get", "delete", "list"} {
					m := x[op].(map[string]any)
					m["headers"] = map[string]any{"X-Token": "synthetic-private-header"}
					m["hrefPath"] = "response.href"
					m["hrefMethod"] = "POST"
				}
				x["put"].(map[string]any)["body"] = `{"data":"{data}","secret":"{secret0}"}`
				x["retry"] = map[string]any{"status": []any{429, 503}, "rateReason": "rateLimited"}
			}
			assertNativeUnchanged(t, out)
		})
	}
	for _, auth := range []map[string]any{
		{"type": "none"}, {"type": "basic", "username": "synthetic-private-user", "password": "synthetic-private-pass"},
		{"type": "oauth2", "tokenUrl": "https://storage.example.test/token", "form": map[string]any{"refresh_token": "synthetic-private-refresh"}, "header": map[string]any{"Authorization": "Bearer {token}"}, "tokenPath": "response.token", "expiryPath": "response.expires"},
	} {
		out := nativeBackendFixture(t, "template")
		get(out, "streamSettings", "xdriveSettings", "template").(map[string]any)["auth"] = auth
		assertNativeUnchanged(t, out)
	}
	for _, value := range []any{0, json.Number("4294967295")} {
		out := nativeBackendFixture(t, "google-drive")
		x := get(out, "streamSettings", "xdriveSettings").(map[string]any)
		for _, field := range []string{"segmentBytes", "flushIntervalMs", "pollIntervalMs", "maxPollIntervalMs", "sessionTtlSeconds", "concurrency", "eagerWindowMs", "holeTimeoutMs"} {
			x[field] = value
		}
		assertNativeUnchanged(t, out)
	}
}

func TestTemplateRejectsUnusedFields(t *testing.T) {
	for _, tc := range []struct {
		block, field string
		value        any
	}{
		{"get", "body", "synthetic-private-unused"}, {"delete", "body", "synthetic-private-unused"}, {"list", "body", "synthetic-private-unused"},
		{"put", "namesRegex", "(ignored)"}, {"get", "namesRegex", "(ignored)"}, {"delete", "namesRegex", "(ignored)"},
		{"get", "hrefMethod", "POST"}, {"get", "url", "https://"},
		{"auth", "username", "synthetic-private-unused"}, {"auth", "password", "synthetic-private-unused"}, {"auth", "tokenUrl", "https://ignored.example.test"}, {"auth", "form", map[string]any{"unused": "synthetic-private"}},
	} {
		t.Run(tc.block+"-"+tc.field, func(t *testing.T) {
			out := nativeBackendFixture(t, "template")
			get(out, "streamSettings", "xdriveSettings", "template", tc.block).(map[string]any)[tc.field] = tc.value
			b, _ := json.Marshal(out)
			_, err := ParseLink(string(b), "test", nil)
			if err == nil {
				t.Error("unused or malformed template field accepted")
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("private value exposed")
			}
		})
	}
}

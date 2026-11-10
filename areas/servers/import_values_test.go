package servers

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestImportValueSemantics(t *testing.T) {
	t.Run("nested-private-fields-roundtrip", func(t *testing.T) {
		extra := `{"headers":{"Authorization":"synthetic-private-upload"},"noGRPCHeader":false,"scMaxBufferedPosts":0,"downloadSettings":{"address":"download.example.test","port":443,"network":"xhttp","security":"tls","tlsSettings":{"serverName":"download.example.test","fingerprint":"chrome"},"xhttpSettings":{"path":"/下载","extra":{"headers":{"X-Token":"synthetic-private-download"},"noSSEHeader":false}}}}`
		r, err := ParseLink("vless://test@[2001:db8::1]:443?type=xhttp&security=tls&sni=example.test&path=%2F%D1%82%D0%B5%D1%81%D1%82%2Ba&extra="+url.QueryEscape(extra)+"#%D0%A2%D0%B5%D1%81%D1%82", "value-test", nil)
		if err != nil {
			t.Fatal(err)
		}
		out, err := Outbound(r)
		if err != nil {
			t.Fatal(err)
		}
		if get(out, "streamSettings", "xhttpSettings", "path") != "/тест+a" || get(out, "streamSettings", "xhttpSettings", "extra", "noGRPCHeader") != false || !jsonEq(get(out, "streamSettings", "xhttpSettings", "extra", "scMaxBufferedPosts"), 0) {
			t.Fatal("explicit values lost")
		}
		e := newEnv(t)
		if err := WriteRecord(e.d, r); err != nil {
			t.Fatal(err)
		}
		stored, err := ReadRecord(e.d, "value-test")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := Marshal(stored)
		restored, err := DecodeBackupRecord(b, "value-test")
		if err != nil {
			t.Fatal(err)
		}
		after, err := Outbound(restored)
		if err != nil || !jsonValueEqual(out, after) {
			t.Fatal("storage/backup changed outbound", err)
		}
		masked, _ := json.Marshal(MaskRecord(restored))
		if strings.Contains(string(masked), "synthetic-private-") {
			t.Error("private extension headers leaked in public record")
		}
		private, _ := json.Marshal(after)
		if !strings.Contains(string(private), "synthetic-private-upload") || !strings.Contains(string(private), "synthetic-private-download") {
			t.Fatal("private outbound lost headers")
		}
	})
	t.Run("duplicate-json", func(t *testing.T) {
		legacy := `{"v":"2","add":"first.example.test","add":"second.example.test","port":"443","id":"test","net":"tcp"}`
		links := []string{
			"vmess://" + base64.StdEncoding.EncodeToString([]byte(legacy)),
			"vless://test@example.test:443?type=xhttp&extra=" + url.QueryEscape(`{"noGRPCHeader":true,"noGRPCHeader":false}`),
			"vless://test@example.test:443?type=ws&streamOptions=" + url.QueryEscape(`{"wsSettings":{"heartbeatPeriod":0,"heartbeatPeriod":5}}`),
			"vless://test@example.test:443?type=xhttp&extra=" + url.QueryEscape(`{}trailing`),
		}
		for i, link := range links {
			if _, err := ParseLink(link, "test", nil); err == nil {
				t.Errorf("ambiguous/invalid JSON accepted: case %d", i)
			}
		}
	})
}

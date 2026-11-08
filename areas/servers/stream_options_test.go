package servers

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestNoSilentTransportLoss(t *testing.T) {
	legacy := `{"v":"2","add":"example.com","port":"443","id":"test","net":"ws","mystery":"secret-value"}`
	if _, e := ParseLink("vmess://"+base64.StdEncoding.EncodeToString([]byte(legacy)), "test", nil); e == nil || !strings.Contains(e.Error(), "mystery") || strings.Contains(e.Error(), "secret-value") {
		t.Fatal("unknown VMess JSON field silently discarded")
	}
	if _, e := ParseLink("vless://test@example.com:443?type=ws&extra=%7B%22xPaddingBytes%22%3A%22100-1000%22%7D", "test", nil); e == nil {
		t.Fatal("XHTTP extra silently ignored on WS")
	}
	r, e := ParseLink("vless://test@example.com:443?type=xhttp&x_padding_bytes=100-1000", "test", nil)
	if e != nil {
		t.Fatal(e)
	}
	o, e := Outbound(r)
	if e != nil {
		t.Fatal(e)
	}
	if get(o, "streamSettings", "xhttpSettings", "extra", "xPaddingBytes") != "100-1000" {
		t.Fatal("padding alias lost")
	}
	if _, e := ParseLink("hy2://secret@example.com?sni=one.example&peer=two.example", "test", nil); e == nil {
		t.Fatal("conflicting SNI alias ignored")
	}
}

func TestStreamOptionsRoundtrip(t *testing.T) {
	for _, tc := range []struct {
		network, options, block, field string
		value                          any
	}{
		{"ws", `{"wsSettings":{"headers":{"Authorization":"synthetic-secret"},"heartbeatPeriod":0},"sockopt":{"tcpFastOpen":false,"tcpKeepAliveIdle":0,"tcpKeepAliveInterval":30,"domainStrategy":"UseIPv4"}}`, "wsSettings", "heartbeatPeriod", 0},
		{"httpupgrade", `{"httpupgradeSettings":{"headers":{"X-Test":"synthetic-secret"}}}`, "httpupgradeSettings", "headers", map[string]any{"X-Test": "synthetic-secret"}},
		{"grpc", `{"grpcSettings":{"idle_timeout":0,"health_check_timeout":5,"permit_without_stream":false,"initial_windows_size":65536,"user_agent":"test-agent"}}`, "grpcSettings", "permit_without_stream", false},
	} {
		t.Run(tc.network, func(t *testing.T) {
			uri := "vless://test@example.com:443?type=" + tc.network + "&security=tls&sni=example.com&streamOptions=" + strings.ReplaceAll(url.QueryEscape(tc.options), "+", "%20")
			rec, err := ParseLink(uri, "test", nil)
			if err != nil {
				t.Fatal(err)
			}
			out, err := Outbound(rec)
			if err != nil {
				t.Fatal(err)
			}
			if !jsonValueEqual(get(out, "streamSettings", tc.block, tc.field), tc.value) {
				t.Fatal("stream field lost")
			}
			b, _ := Marshal(rec)
			restored, err := DecodeBackupRecord(b, "test")
			if err != nil {
				t.Fatal(err)
			}
			out2, _ := Outbound(restored)
			if !jsonValueEqual(out, out2) {
				t.Fatal("backup changes outbound")
			}
			masked, _ := json.Marshal(MaskRecord(rec))
			if strings.Contains(string(masked), "synthetic-secret") {
				t.Fatal("secret exposed")
			}
			plain, err := ParseLink(strings.Split(uri, "&streamOptions=")[0], "test", nil)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := CanonicalKey(rec)
			bkey, _ := CanonicalKey(plain)
			if a == bkey {
				t.Fatal("options omitted from identity")
			}
		})
	}
}

func TestStreamOptionsRejectLoss(t *testing.T) {
	for _, opts := range []string{
		`{"wsSettings":{"headers":{"Host":"other.example"}}}`,
		`{"wsSettings":{"headers":{"X-Test":"bad\r\nInjected: value"}}}`,
		`{"wsSettings":{"headers":{"Bad Name":"value"}}}`,
		`{"grpcSettings":{"idle_timeout":1}}`,
		`{"sockopt":{"mark":123}}`,
		`{"sockopt":{"tcpFastOpen":"false"}}`,
		`{"wsSettings":{"heartbeatPeriod":-1}}`,
	} {
		if _, err := ParseLink("vless://test@example.com:443?type=ws&streamOptions="+strings.ReplaceAll(url.QueryEscape(opts), "+", "%20"), "test", nil); err == nil {
			t.Fatal("unsupported or invalid options silently accepted")
		}
	}
	_, err := ParseLink("vless://test@example.com:443?mystery=secret-value", "test", nil)
	if err == nil || !strings.Contains(err.Error(), "mystery") || strings.Contains(err.Error(), "secret-value") {
		t.Fatal("unknown field must produce a safe named error")
	}
}

func TestShadowsocksSocketOptionsReachXray(t *testing.T) {
	r, e := ParseLink("ss://aes-128-gcm:synthetic@example.com:443?streamOptions="+url.QueryEscape(`{"sockopt":{"domainStrategy":"UseIPv4"}}`), "test", nil)
	if e != nil {
		t.Fatal(e)
	}
	o, e := Outbound(r)
	if e != nil {
		t.Fatal(e)
	}
	if get(o, "streamSettings", "sockopt", "domainStrategy") != "UseIPv4" {
		t.Fatal("Shadowsocks sockopt silently lost")
	}
}

func TestPaddingAliasValidatedBeforeCommit(t *testing.T) {
	if _, e := ParseLink("vless://test@example.com:443?type=xhttp&x_padding_bytes=0-1", "test", nil); e == nil {
		t.Fatal("invalid final record accepted")
	}
}

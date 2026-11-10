package servers

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestXHTTPXMuxKeepAlive(t *testing.T) {
	for _, value := range []string{"-1", "0", "1", "9223372036854775807"} {
		r, err := ParseLink("vless://test@example.test:443?type=xhttp&security=tls&extra="+url.QueryEscape(`{"xmux":{"hKeepAlivePeriod":`+value+`}}`), "test", nil)
		if err != nil {
			t.Errorf("official signed keepalive %s rejected: %v", value, err)
			continue
		}
		out, _ := Outbound(r)
		b, _ := json.Marshal(get(out, "streamSettings", "xhttpSettings", "extra", "xmux", "hKeepAlivePeriod"))
		if string(b) != value {
			t.Fatalf("XHTTP keepalive changed: want %s, got %s", value, b)
		}
	}
}

func TestNativeXMuxKeepAlive(t *testing.T) {
	for _, kind := range []string{"xdrive", "masque"} {
		for _, value := range []json.Number{"-1", "0", "1", "-9223372036854775808", "9223372036854775807"} {
			t.Run(kind+"/"+string(value), func(t *testing.T) {
				out := nativeTestOutbound(kind)
				get(out, "streamSettings", kind+"Settings", "xmux").(map[string]any)["hKeepAlivePeriod"] = value
				r := parseNativeFixture(t, out)
				after, _ := Outbound(r)
				if !jsonValueEqual(get(after, "streamSettings", kind+"Settings", "xmux", "hKeepAlivePeriod"), value) {
					t.Fatal("signed int64 changed")
				}
			})
		}
		for _, value := range []any{json.Number("1.5"), json.Number("9223372036854775808"), "-1", false} {
			out := nativeTestOutbound(kind)
			get(out, "streamSettings", kind+"Settings", "xmux").(map[string]any)["hKeepAlivePeriod"] = value
			if _, err := ParseNativeOutbound(out, "test", "native-test", nil); err == nil {
				t.Fatal("invalid keepalive accepted")
			}
		}
	}
}

func TestNativeUnsignedBounds(t *testing.T) {
	for _, field := range []string{"segmentBytes", "flushIntervalMs", "pollIntervalMs", "maxPollIntervalMs", "sessionTtlSeconds", "concurrency", "eagerWindowMs", "holeTimeoutMs"} {
		for _, value := range []any{json.Number("-1"), json.Number("4294967296"), json.Number("1.5"), false, "1", nil} {
			out := nativeTestOutbound("xdrive")
			get(out, "streamSettings", "xdriveSettings").(map[string]any)[field] = value
			if _, err := ParseNativeOutbound(out, "test", "native-test", nil); err == nil {
				t.Errorf("invalid uint32 accepted: %s", field)
			}
		}
	}
}

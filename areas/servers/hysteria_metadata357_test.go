package servers

import "testing"

func TestHysteria357MasqueradeMetadata(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"protocol": "hysteria", "settings": map[string]any{"address": "edge.example.test", "port": 443, "version": 2}, "streamSettings": map[string]any{"network": "hysteria", "security": "tls", "tlsSettings": map[string]any{"serverName": "front.example.test"}, "hysteriaSettings": map[string]any{"version": 2, "auth": "synthetic-only"}}}
	}
	plain := fullXray357Parse(t, base())
	pk, _ := presentationKey(plain)
	for _, kind := range []string{"file", "proxy", "string", ""} {
		t.Run(kind, func(t *testing.T) {
			o := base()
			m := map[string]any{"type": kind, "dir": "/nonexistent-private-fixture", "url": "https://unused.example.test/", "content": "private-body", "rewriteHost": false, "statusCode": 0}
			o["streamSettings"].(map[string]any)["hysteriaSettings"].(map[string]any)["masquerade"] = m
			r := fullXray357Parse(t, o)
			if has(r, "importProfile") || connectionSupport(r)["supported"] == false {
				t.Fatal("listener metadata disables valid client")
			}
			if !jsonValueEqual(r["hysteriaMasquerade"], m) {
				t.Fatal("metadata lost")
			}
			k, _ := presentationKey(r)
			if k != pk {
				t.Fatal("ignored metadata creates duplicate card")
			}
			if has(MaskRecord(r), "hysteriaMasquerade") {
				t.Fatal("private metadata exposed")
			}
		})
	}
}

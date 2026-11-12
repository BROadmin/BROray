package servers

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPresentation357DiagnosticsMetadata(t *testing.T) {
	reality := presentationRecord(t, "metadata-reality", "")
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("K", 32)))
	wg := fullXray357Parse(t, map[string]any{"protocol": "wireguard", "settings": map[string]any{"secretKey": key, "peers": []any{map[string]any{"publicKey": key, "endpoint": "peer.example.test:51820"}}}})
	for _, tc := range []struct {
		name   string
		base   Record
		change func(Record)
	}{
		{"reality-false", reality, func(r Record) { r["reality"].(map[string]any)["show"] = false }},
		{"reality-true", reality, func(r Record) { r["reality"].(map[string]any)["show"] = true }},
		{"wireguard-neutral", wg, func(r Record) {
			p := r["wireguard"].(map[string]any)["peers"].([]any)[0].(map[string]any)
			p["email"] = ""
			p["level"] = 0
		}},
		{"wireguard-labelled", wg, func(r Record) {
			p := r["wireguard"].(map[string]any)["peers"].([]any)[0].(map[string]any)
			p["email"] = "private@example.test"
			p["level"] = 123
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := normalize(tc.base)
			tc.change(changed)
			before := fullXray357JSON(t, changed)
			want, err := presentationKey(tc.base)
			if err != nil {
				t.Fatal(err)
			}
			got, err := presentationKey(changed)
			if err != nil || got != want {
				t.Fatal("diagnostic/server-side metadata split the connection card")
			}
			if before != fullXray357JSON(t, changed) {
				t.Fatal("display grouping modified private metadata")
			}
		})
	}
}

// Account email identifies a local log/statistics bucket, not a different
// network connection. Keep source records complete while grouping their UI.
func TestPresentation357AccountEmailMetadata(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "trojan", "shadowsocks", "http", "socks"} {
		t.Run(protocol, func(t *testing.T) {
			baseInput, _ := fullXray357EmailFixture(protocol, false, "")
			delete(baseInput["settings"].(map[string]any), "email")
			base := fullXray357Parse(t, baseInput)
			want, err := presentationKey(base)
			if err != nil {
				t.Fatal(err)
			}
			for _, email := range []string{"", "one@example.test", "two@example.test"} {
				input, _ := fullXray357EmailFixture(protocol, false, email)
				rec := fullXray357Parse(t, input)
				got, err := presentationKey(rec)
				if err != nil || got != want {
					t.Fatal("account metadata created a duplicate connection card")
				}
				if rec["accountEmail"] != email {
					t.Fatal("display grouping mutated the private account metadata")
				}
				fullXray357Roundtrip(t, rec, nil)
			}
		})
	}
}

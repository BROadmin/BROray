package servers

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestFullIdentity357WireGuardByteEncoding(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("K", 32)))
	o := map[string]any{"protocol": "wireguard", "settings": map[string]any{"secretKey": key, "noKernelTun": true, "reserved": "AQID", "peers": []any{map[string]any{"publicKey": key, "endpoint": "peer.example.test:51820"}}}}
	r := fullXray357Parse(t, o)
	fullXray357Roundtrip(t, r, map[string]any{"settings.reserved": []any{1, 2, 3}})
	want, _ := CanonicalKey(r)
	o["settings"].(map[string]any)["reserved"] = []any{json.Number("1"), json.Number("2"), json.Number("3")}
	other := fullXray357Parse(t, o)
	got, _ := CanonicalKey(other)
	if want != got {
		t.Fatal("same decoded reserved bytes split identity")
	}
	o["settings"].(map[string]any)["reserved"] = "!!invalid"
	fullXray357Reject(t, o)
}

func TestFullIdentity357CoreIDForms(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess"} {
		t.Run(protocol, func(t *testing.T) {
			var previous string
			for _, id := range []string{"клиент:357", "1faa3d77-e5ff-58a7-b6cd-9ca0a7199b6d", "1FAA3D77E5FF58A7B6CD9CA0A7199B6D"} {
				o := fullXray357Base(protocol)
				o["settings"].(map[string]any)["id"] = id
				r := fullXray357Parse(t, o)
				k, e := CanonicalKey(r)
				if e != nil {
					t.Fatal(e)
				}
				if previous != "" && previous != k {
					t.Fatal("same effective core credential split into duplicate identity")
				}
				previous = k
				fullXray357Roundtrip(t, r, map[string]any{})
				coverageRoundTrip(t, r, true)
			}
		})
	}
}
func TestFullIdentity357RejectInvalidIDs(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess"} {
		for _, id := range []string{strings.Repeat("a", 31), strings.Repeat("z", 32), strings.Repeat("a", 37)} {
			o := fullXray357Base(protocol)
			o["settings"].(map[string]any)["id"] = id
			fullXray357Reject(t, o)
		}
	}
	for _, id := range []string{strings.Repeat("a", 31), strings.Repeat("z", 32)} {
		if _, e := ParseLink("vless://"+url.PathEscape(id)+"@edge.example.test:443?security=tls", "test-id357", nil); e == nil {
			t.Fatal("invalid URI credential accepted")
		}
	}
	valid := fullXray357Parse(t, fullXray357Base("vless"))
	valid["uuid"] = strings.Repeat("z", 32)
	if e := Validate(valid, "restored"); e == nil {
		t.Fatal("stored invalid credential accepted")
	}
}

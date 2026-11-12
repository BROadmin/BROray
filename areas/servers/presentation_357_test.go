package servers

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestPresentation357AWGExportMetadata(t *testing.T) {
	ini := "[Interface]\nPrivateKey = " + awgPrivate + "\nAddress = 10.0.0.2/32\nJc = 4\nRandomTrailers = true\n[Peer]\nPublicKey = " + awgPublic + "\nEndpoint = node.example:443\nAllowedIPs = 0.0.0.0/0, ::/0\n"
	a, err := ParseLink("vpn://"+base64.StdEncoding.EncodeToString([]byte(ini)), "uri", nil)
	if err != nil {
		t.Fatal(err)
	}
	last, _ := json.Marshal(map[string]any{"config": ini})
	opts := map[string]any{"type": "wireguard", "name": "synthetic", "server": "node.example", "port": 443, "private-key": awgPrivate, "public-key": awgPublic, "ip": "10.0.0.2", "amnezia-wg-option": map[string]any{"jc": 4, "random-trailers": true}}
	profile := map[string]any{"description": "synthetic", "containers": []any{map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": string(last), "sourceFormat": "clash-yaml", "clashOptions": opts}}}}
	body, _ := json.Marshal(profile)
	b, err := ParseLink("vpn://"+base64.StdEncoding.EncodeToString(body), "clash", nil)
	if err != nil {
		t.Fatal(err)
	}
	ka, _ := presentationKey(a)
	kb, _ := presentationKey(b)
	if ka != kb {
		t.Error("equivalent normalized profile split by sourceFormat/raw serialization")
	}
	for _, value := range []string{"on", "ON", "1"} {
		c, err := ParseLink("vpn://"+base64.StdEncoding.EncodeToString([]byte(strings.Replace(ini, "RandomTrailers = true", "RandomTrailers = "+value, 1))), "boolean-alias", nil)
		if err != nil {
			t.Fatal(err)
		}
		kc, _ := presentationKey(c)
		if kc != ka {
			t.Errorf("equivalent boolean %s splits presentation", value)
		}
	}
	unknown, err := ParseLink("vpn://"+base64.StdEncoding.EncodeToString([]byte(strings.Replace(ini, "RandomTrailers = true", "RandomTrailers = yes", 1))), "unknown-bool", nil)
	if err != nil {
		t.Fatal(err)
	}
	ku, _ := presentationKey(unknown)
	if ku == ka {
		t.Fatal("unknown boolean spelling discarded")
	}
	c := normalize(b)
	c["amneziawg"].(map[string]any)["parameters"].(map[string]any)["awg"].(map[string]any)["clashOptions"].(map[string]any)["amnezia-wg-option"].(map[string]any)["future-extension"] = "preserved"
	kc, _ := presentationKey(c)
	if kc == kb {
		t.Fatal("unknown extension discarded by grouping")
	}
	legacy := normalize(b)
	legacy["amneziawg"].(map[string]any)["parameters"].(map[string]any)["awg"].(map[string]any)["clashOptions"].(map[string]any)["amnezia-wg-option"].(map[string]any)["header-protection-key"] = "legacy-unmapped-secret"
	kl, _ := presentationKey(legacy)
	if kl == kb {
		t.Error("legacy raw-only extension discarded without normalized equivalent")
	}
	changed := strings.Replace(ini, "RandomTrailers = true", "RandomTrailers = false", 1)
	d, err := ParseLink("vpn://"+base64.StdEncoding.EncodeToString([]byte(changed)), "other", nil)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := presentationKey(d)
	if ka == kd {
		t.Fatal("different normalized extension grouped")
	}
}

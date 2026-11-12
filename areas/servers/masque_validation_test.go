package servers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestMasqueWarpFields(t *testing.T) {
	for _, field := range []string{"privateKey", "privateCurve", "publicKey", "address-type", "address-invalid", "address-family", "address-zone", "address-empty"} {
		t.Run(field, func(t *testing.T) {
			out := nativeBackendFixture(t, "masque-warp")
			w := get(out, "streamSettings", "masqueSettings", "warp").(map[string]any)
			switch field {
			case "privateKey", "publicKey":
				w[field] = "synthetic-private-invalid"
			case "privateCurve":
				k, e := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
				if e != nil {
					t.Fatal(e)
				}
				b, e := x509.MarshalPKCS8PrivateKey(k)
				if e != nil {
					t.Fatal(e)
				}
				w["privateKey"] = base64.StdEncoding.EncodeToString(b)
			case "address-type":
				w["address"] = "192.0.2.2"
			case "address-invalid":
				w["address"] = []any{"invalid"}
			case "address-family":
				w["address"] = []any{"192.0.2.2", "192.0.2.3"}
			case "address-zone":
				w["address"] = []any{"fe80::1%eth0"}
			case "address-empty":
				w["address"] = []any{}
			}
			b, _ := json.Marshal(out)
			_, err := ParseLink(string(b), "test", nil)
			want := "address"
			if strings.HasPrefix(field, "private") {
				want = "privateKey"
			}
			if field == "publicKey" {
				want = "publicKey"
			}
			want = "warp." + want
			if field == "address-type" {
				// Declared shape is rejected by the shared structural boundary.
				want = "поле JSON: outbound"
			}
			if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatalf("wrong field outcome: %v", err)
			}
		})
	}
}

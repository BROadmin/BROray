package amnezia

import (
	"encoding/base64"
	"strings"
	"testing"
)

const nativeINI = "[Interface]\nPrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress = 10.0.0.2/32\nJc = 4\nJmin = 40\nJmax = 70\nS3 = 0\nH1 = 12345\nI1 = <b 0123><r 20>\nHeaderProtectionKey = synthetic-extension\nRandomTrailers = 1\n[Peer]\nPublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nAllowedIPs = 0.0.0.0/0\nEndpoint = node.example:443\n"

func TestBase64INIProfile(t *testing.T) {
	for _, prefix := range []string{"", "; exported client profile\r\n# comment\r\n\r\n"} {
		for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
			text := prefix + nativeINI
			got, err := ParseLink("vpn://" + encoding.EncodeToString([]byte(text)))
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != "AmneziaWG" || got.Address != "node.example" || got.Port != 443 || got.Profile.NativeConfig != text {
				t.Fatal("native profile or endpoint changed")
			}
			if got.Profile.Interface["HeaderProtectionKey"][0] != "synthetic-extension" || got.Profile.Interface["I1"][0] != "<b 0123><r 20>" {
				t.Fatal("extension lost")
			}
			if _, _, err := got.Profile.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestBase64INIRejectsInvalidAndPlainWireGuard(t *testing.T) {
	wg := "[Interface]\nPrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress = 10.0.0.2/32\n[Peer]\nPublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nAllowedIPs = 0.0.0.0/0\nEndpoint = node.example:443\n"
	for _, text := range []string{
		wg,
		strings.Replace(nativeINI, "Jc = 4", "Jc = 4\nJc = 5", 1),
		strings.Replace(nativeINI, "Endpoint = node.example:443", "Endpoint = broken", 1),
		strings.Replace(nativeINI, "PrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "PrivateKey = invalid-private-value", 1),
	} {
		_, err := ParseLink("vpn://" + base64.RawURLEncoding.EncodeToString([]byte(text)))
		if err == nil || strings.Contains(err.Error(), "invalid-private-value") {
			t.Fatal("invalid profile accepted or private field leaked")
		}
	}
}

package amnezia

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestFull357NativeAWGVersionSpecificDetection(t *testing.T) {
	base := "[Interface]\nPrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress = 10.7.0.2/32\n%s\n[Peer]\nPublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nAllowedIPs = 0.0.0.0/0\nEndpoint = peer.example.invalid:51820\n"
	for _, option := range []string{
		"J1 = <b 0xffffffff><c>", "Itime = 60",
		"HeaderProtectionKey = MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"RandomTrailers = false", "DisableCookies = true",
	} {
		t.Run(strings.Split(option, " = ")[0], func(t *testing.T) {
			ini := "\ufeff" + strings.Replace(base, "%s", option, 1)
			imported, err := ParseLink("vpn://" + base64.RawURLEncoding.EncodeToString([]byte(ini)))
			if err != nil {
				t.Fatalf("version-specific AWG option must identify the native profile: %v", err)
			}
			if imported.Profile.NativeConfig != ini || imported.Profile.ProtocolVersion != nil {
				t.Fatal("native source must be retained without an invented implementation selector")
			}
			if _, _, err := imported.Profile.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFull357NativeAWGEmptyOptionalFields(t *testing.T) {
	ini := strings.Replace(nativeINI, "Jc = 4", "Jc = 4\nI2 =\nI3 =\nI4 =\nI5 =\nJ1 =\nJ2 =\nJ3 =", 1)
	imported, err := ParseLink("vpn://" + base64.StdEncoding.EncodeToString([]byte(ini)))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"I2", "I3", "I4", "I5", "J1", "J2", "J3"} {
		values := imported.Profile.Interface[field]
		if len(values) != 1 || values[0] != "" {
			t.Fatalf("explicit inactive option %s was lost", field)
		}
	}
	if _, _, err := imported.Profile.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		strings.Replace(ini, "Jc = 4", "Jc =", 1),
		strings.Replace(ini, "Jc = 4", "Jc = 4\nUnknownOption =", 1),
		strings.Replace(ini, "Endpoint = node.example:443", "I2 =\nEndpoint = node.example:443", 1),
	} {
		if _, err := ParseLink("vpn://" + base64.StdEncoding.EncodeToString([]byte(invalid))); err == nil {
			t.Fatal("empty-value exception escaped known AWG interface fields")
		}
	}
}

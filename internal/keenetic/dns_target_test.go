package keenetic

import "testing"

func TestDNSRouteTargetNormalization(t *testing.T) {
	for input, want := range map[string]string{
		"WWW.Example.com.": "www.example.com", "1.1.1.1": "1.1.1.1",
		"2606:4700:4700:0:0:0:0:1111": "2606:4700:4700::1111",
		"1.1.1.1/32":                  "1.1.1.1", "2606:4700::1/128": "2606:4700::1",
	} {
		if got, ok := DNSRouteTarget(input); !ok || got != want {
			t.Errorf("%q gave %q %t, want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"1.1.1.0/24", "fe80::1%eth0", "127.0.0.1:5", "https://1.1.1.1", "a.example x"} {
		if _, ok := DNSRouteTarget(input); ok {
			t.Errorf("unexpected target %q", input)
		}
	}
}

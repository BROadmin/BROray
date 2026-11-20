package keenetic

import "testing"

func TestAllowDoTIPv6(t *testing.T) {
	for _, address := range []string{"2606:4700:4700::1111", "2001:4860:4860:0:0:0:0:8888", "2620:fe::fe"} {
		for _, cmd := range []string{"dns-proxy tls upstream " + address + " 853 sni dns.example", "no dns-proxy tls upstream " + address + " 853"} {
			if err := Allow(AreaDoT, cmd); err != nil {
				t.Errorf("IPv6 command refused: %v", err)
			}
		}
	}
	for _, address := range []string{"[::1]", "fe80::1%eth0", "::ffff:1.1.1.1", "1::2::3", "2001:db8::/32", "2001:db8::1;reboot", "hostname"} {
		if Allow(AreaDoT, "dns-proxy tls upstream "+address+" 853 sni dns.example") == nil || Allow(AreaDoT, "no dns-proxy tls upstream "+address+" 853") == nil {
			t.Errorf("invalid address accepted: %s", address)
		}
	}
}

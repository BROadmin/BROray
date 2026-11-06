package keenetic

import (
	"strings"
	"testing"
)

func TestAllowRoutes(t *testing.T) {
	ok := []string{"ip route 13.32.0.0 255.240.0.0 Proxy0 1200", "no ip route 13.32.0.0 255.240.0.0 Proxy0"}
	bad := []string{"ip route 13.32.0.0 255.240.0.0 Proxy0", "ip route 13.32.0.0 255.240.0.0 Proxy0 auto", "ip route 0.0.0.0 0.0.0.0 Proxy0 1200; reboot", "ip route 13.32.0.0 255.255.0.1 Proxy0 1200", "ip route 13.32.0.0 255.240.0.0 Wireguard0 1200", "no ip route 13.32.0.0 255.240.0.0 Proxy256"}
	for _, c := range ok {
		if err := Allow(AreaRoutes, c); err != nil {
			t.Errorf("%q refused: %v", c, err)
		}
	}
	for _, c := range bad {
		if err := Allow(AreaRoutes, c); err == nil {
			t.Errorf("%q accepted", c)
		}
	}
}

func TestAllowInterfaceDoTWeb(t *testing.T) {
	ok := []string{
		"interface Proxy0", "interface Proxy0 proxy protocol socks5", "interface Proxy0 proxy upstream 192.168.1.1 2080",
		"interface Proxy0 proxy connect", `interface Proxy0 description "BROray - NL 🇳🇱"`, "interface Proxy0 description BROray",
		"interface Proxy0 security-level public", "interface Proxy0 up", "no interface Proxy0",
		"interface Proxy0 no description", "interface Proxy0 description null",
		"dns-proxy tls upstream 1.1.1.1 853 sni cloudflare-dns.com", "no dns-proxy tls upstream 1.1.1.1 853",
		"ip http proxy broray", "ip http proxy broray upstream http 192.168.1.1 8080", "ip http proxy broray domain ndns",
		"ip http proxy broray ssl redirect", "ip http proxy broray security-level public", "no ip http proxy broray",
		"system configuration save",
	}
	for _, c := range ok {
		area := AreaInterface
		switch {
		case strings.Contains(c, "dns-proxy"):
			area = AreaDoT
		case strings.Contains(c, "http proxy"):
			area = AreaWeb
		case strings.HasPrefix(c, "system"):
			area = AreaSystem
		}
		if err := Allow(area, c); err != nil {
			t.Errorf("%q refused: %v", c, err)
		}
	}
	bad := map[Area][]string{
		AreaInterface: {"interface Wireguard0 up", `interface Proxy0 description "a" ; system reboot`, "interface Proxy0 proxy upstream evil.example 2080", "system reboot"},
		AreaDoT:       {"dns-proxy tls upstream 1.1.1.1 853", "no dns-proxy tls upstream 94.140.14.14", "dns-proxy tls upstream 1.1.1.1 853 sni bad host"},
		AreaWeb:       {"ip http proxy Broray", "ip http proxy broray upstream http 192.168.1.1", "ip http proxy broray upstream https 192.168.1.1 8080"},
		AreaSystem:    {"system reboot", "system configuration save now"},
	}
	for area, list := range bad {
		for _, c := range list {
			if err := Allow(area, c); err == nil {
				t.Errorf("%s %q accepted", area, c)
			}
		}
	}
}

func TestParseBlocksAndRoutes(t *testing.T) {
	lines := []string{
		"! $$$ Model: Keenetic Ultra", "interface Proxy0", `    description "BROray - NL"`, "    security-level public",
		"    proxy protocol socks5", "    proxy upstream 192.168.1.1 2080", "    up", "!",
		"ip route 2.16.0.0 255.248.0.0 Proxy0 1200", "ip route 1.2.3.0 255.255.255.0 Proxy0 1200", "ip route 9.9.9.0 255.255.255.0 Wireguard0 1200",
		"ip route 8.8.8.0 255.255.255.0 Proxy0", "dns-proxy", "    rebind-protect auto", "    tls upstream 94.140.14.14 sni dns.adguard-dns.com", "    tls upstream 1.1.1.1 853 sni cloudflare-dns.com", "!",
	}
	blocks := ParseBlocks(lines)
	b, ok := FindBlock(blocks, "interface Proxy0")
	if !ok {
		t.Fatal("block missing")
	}
	st := parseInterfaceBlock(b)
	if st.Description != "BROray - NL" || st.Upstream != "192.168.1.1 2080" || !st.Up || st.Protocol != "socks5" {
		t.Fatalf("bad parse: %+v", st)
	}
	routes := routesFromLines(lines, "Proxy0", 1200)
	if len(routes) != 2 {
		t.Fatalf("want 2 managed routes, got %d", len(routes))
	}
	d, _ := FindBlock(blocks, "dns-proxy")
	if len(d.Body) != 3 {
		t.Fatalf("dns-proxy body: %v", d.Body)
	}
}

func TestEqualConfigIgnoresHeaders(t *testing.T) {
	a := []string{"! $$$ Model: X", "! $$$ Version: 5.2", "interface Proxy0", "!"}
	b := []string{"! saved 2026", "interface Proxy0"}
	if !equalConfig(a, b) {
		t.Fatal("configs should be equal ignoring comments")
	}
	if equalConfig(a, []string{"interface Proxy1"}) {
		t.Fatal("configs should differ")
	}
}

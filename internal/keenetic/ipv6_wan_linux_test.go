package keenetic

import (
	"io"
	"strings"
	"testing"
)

func TestIPv6WANRouteEvidence(t *testing.T) {
	const zero = "00000000000000000000000000000000"
	const local = "fd001234000000000000000000000000"
	const public = "20010db8000000000000000000000000"
	const gateway = "fe800000000000000000000000000001"
	row := func(dst, bits, src, sourceBits, via, flags, device string) string {
		return strings.Join([]string{dst, bits, src, sourceBits, via, "00000100", "00000000", "00000000", flags, device}, " ") + "\n"
	}
	basedecor := row(zero, "00", zero, "00", zero, "00000001", "t2s1") +
		row(local, "40", zero, "00", zero, "00000001", "br0") +
		row(public, "40", zero, "00", zero, "00000001", "ezcfg0")
	for _, tc := range []struct {
		name, table string
		absent      bool
	}{
		{"Basedecor internal addresses and tunnel", basedecor, true},
		{"IPv4-only with kernel reject default", row(zero, "00", zero, "00", zero, "00200201", "lo"), true},
		{"IPv6 enabled with no routes", "", true},
		{"IPv6 uplink alongside ProxyN", basedecor + row(zero, "00", zero, "00", gateway, "00010003", "eth3"), false},
		{"bridge configured as alternate uplink", basedecor + row(zero, "00", zero, "00", gateway, "00000003", "br1"), false},
		{"IPv6 source-specific default", basedecor + row(zero, "00", public, "38", gateway, "00000003", "ppp0"), false},
		{"another tunnel is still a possible exit", basedecor + row(zero, "00", zero, "00", zero, "00000001", "wg1"), false},
		{"split public aggregate", row(zero, "01", zero, "00", zero, "00000001", "ppp0"), false},
		{"specific public route via gateway", row(public, "40", zero, "00", gateway, "00000003", "eth3"), false},
		{"down external route", basedecor + row(zero, "00", zero, "00", gateway, "00000002", "eth3"), true},
		{"malformed tail", basedecor + "00000000\n", false},
		{"oversized next hop", row(zero, "00", zero, "00", gateway+"00", "00000003", "eth3"), false},
		{"invalid flags", basedecor + row(zero, "00", zero, "00", zero, "badflags", "t2s1"), false},
		{"oversized input", strings.Repeat(basedecor, (maxIPv6RouteTable/len(basedecor))+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ipv6WANAbsent(strings.NewReader(tc.table)); got != tc.absent {
				t.Fatalf("absence=%t, want %t", got, tc.absent)
			}
		})
	}
	if ipv6WANAbsent(io.MultiReader(strings.NewReader(basedecor), wanRouteReadFailure{})) {
		t.Fatal("partial route table confirmed an outage")
	}
}

type wanRouteReadFailure struct{}

func (wanRouteReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

package routes

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseBatRules(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		routes     []string
		errors     []string
		normalized int
	}{
		{"basic", "route add 91.108.4.0 mask 255.255.252.0 0.0.0.0\n", []string{"91.108.4.0/22"}, nil, 0},
		{"crlf and trailing spaces", "route add 1.2.3.0 mask 255.255.255.0 0.0.0.0  \r\n", []string{"1.2.3.0/24"}, nil, 0},
		{"no final newline", "route add 1.2.3.0 mask 255.255.255.0 0.0.0.0", []string{"1.2.3.0/24"}, nil, 0},
		{"case insensitive keywords", "ROUTE ADD 1.2.3.0 MASK 255.255.255.0 0.0.0.0", []string{"1.2.3.0/24"}, nil, 0},
		{"tabs as separators", "\troute\tadd\t1.2.3.0\tmask\t255.255.255.0\t0.0.0.0", []string{"1.2.3.0/24"}, nil, 0},
		{"comments skipped", "# c\n:: c\nrem x\nREM\n@echo off\necho hi\nchcp 65001\ntitle t\npause\ncls\nsetlocal\nendlocal\nexit /b 0\nEXIT  /B\n\n", nil, nil, 0},
		{"remark is not rem", "remark x", nil, []string{"Строка 1: " + msgOnlyRouteAdd}, 0},
		{"exit without /b is rejected", "exit", nil, []string{"Строка 1: " + msgOnlyRouteAdd}, 0},
		{"& rem comment", "route add 1.2.3.0 mask 255.255.255.0 0.0.0.0 & rem Telegram", []string{"1.2.3.0/24"}, nil, 0},
		{"& REM comment ci", "route add 1.2.3.0 mask 255.255.255.0 0.0.0.0 & REM x y z", []string{"1.2.3.0/24"}, nil, 0},
		{"extra field", "route add 1.2.3.0 mask 255.255.255.0 0.0.0.0 metric 5", nil, []string{"Строка 1: " + msgOnlyRem}, 0},
		{"7 fields", "route add 1.2.3.0 mask 255.255.255.0 0.0.0.0 &", nil, []string{"Строка 1: " + msgOnlyRem}, 0},
		{"too few fields", "route add 1.2.3.0 mask 255.255.255.0", nil, []string{"Строка 1: " + msgOnlyRouteAdd}, 0},
		{"route -p", "route -p add 1.2.3.0 mask 255.255.255.0 0.0.0.0", nil, []string{"Строка 1: " + msgOnlyRouteAdd}, 0},
		{"bad ip", "route add 1.2.3.256 mask 255.255.255.0 0.0.0.0", nil, []string{"Строка 1: " + msgBadIPv4}, 0},
		{"bad ip letters", "route add 1.2.x.0 mask 255.255.255.0 0.0.0.0", nil, []string{"Строка 1: " + msgBadIPv4}, 0},
		{"ip three octets", "route add 1.2.3 mask 255.255.255.0 0.0.0.0", nil, []string{"Строка 1: " + msgBadIPv4}, 0},
		{"non contiguous mask", "route add 1.2.3.0 mask 255.0.255.0 0.0.0.0", nil, []string{"Строка 1: " + msgBadMask}, 0},
		{"invalid mask octet", "route add 1.2.3.0 mask 255.255.253.0 0.0.0.0", nil, []string{"Строка 1: " + msgBadMask}, 0},
		{"gateway", "route add 1.2.3.0 mask 255.255.255.0 10.0.0.1", nil, []string{"Строка 1: " + msgOnlyGateway}, 0},
		{"gateway leading zero is not 0.0.0.0", "route add 1.2.3.0 mask 255.255.255.0 0.0.0.00", nil, []string{"Строка 1: " + msgOnlyGateway}, 0},
		{"leading zeros normalized", "route add 010.001.002.000 mask 255.255.255.0 0.0.0.0", []string{"10.1.2.0/24"}, nil, 1},
		{"leading zero mask", "route add 10.1.2.0 mask 0255.255.255.000 0.0.0.0", []string{"10.1.2.0/24"}, nil, 0},
		{"host route", "route add 1.2.3.4 mask 255.255.255.255 0.0.0.0", []string{"1.2.3.4/32"}, nil, 0},
		{"network aligned", "route add 1.2.3.77 mask 255.255.255.192 0.0.0.0", []string{"1.2.3.64/26"}, nil, 1},
		{"default route", "route add 0.0.0.0 mask 0.0.0.0 0.0.0.0", nil, []string{"Строка 1: " + msgDefaultCustom}, 0},
		{"default via host bits", "route add 9.9.9.9 mask 0.0.0.0 0.0.0.0", nil, []string{"Строка 1: " + msgDefaultCustom}, 0},
		{"slash 1 allowed", "route add 128.0.0.0 mask 128.0.0.0 0.0.0.0", []string{"128.0.0.0/1"}, nil, 0},
		{"line numbers count every line", "# x\n\nroute add 1.2.3.0 mask 255.255.255.0 1.1.1.1\nfoo", nil,
			[]string{"Строка 3: " + msgOnlyGateway, "Строка 4: " + msgOnlyRouteAdd}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := parseBat([]byte(tc.in), false, 20)
			if !reflect.DeepEqual(r.Routes, tc.routes) || !reflect.DeepEqual(r.Errors, tc.errors) || r.NormalizedLines != tc.normalized {
				t.Fatalf("got routes=%v errors=%v normalized=%d", r.Routes, r.Errors, r.NormalizedLines)
			}
		})
	}
}

func TestParseBatCatalogDefaultMessageAndErrorLimit(t *testing.T) {
	r := parseBat([]byte("route add 0.0.0.0 mask 0.0.0.0 0.0.0.0"), true, 0)
	if len(r.Errors) != 1 || r.Errors[0] != "Строка 1: "+msgDefaultCatalog {
		t.Fatal(r.Errors)
	}
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString("bad line\n")
	}
	r = parseBat([]byte(b.String()), false, 20)
	if len(r.Errors) != 20 || r.ErrorCount != 30 {
		t.Fatalf("errors kept=%d count=%d", len(r.Errors), r.ErrorCount)
	}
}

func TestCollapseCIDR(t *testing.T) {
	cases := []struct {
		in, out []string
	}{
		{[]string{"10.0.0.0/24", "10.0.1.0/24"}, []string{"10.0.0.0/23"}},
		{[]string{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/23"}, []string{"10.0.0.0/22"}},
		{[]string{"10.0.0.0/8", "10.1.2.0/24", "10.200.0.0/16"}, []string{"10.0.0.0/8"}},
		{[]string{"10.0.1.0/24", "10.0.2.0/24"}, []string{"10.0.1.0/24", "10.0.2.0/24"}}, // not siblings
		{[]string{"1.2.3.4/32", "1.2.3.5/32", "1.2.3.6/32", "1.2.3.7/32"}, []string{"1.2.3.4/30"}},
		{[]string{"0.0.0.0/1", "128.0.0.0/1"}, []string{"0.0.0.0/1", "128.0.0.0/1"}}, // never /0
		{[]string{"0.0.0.0/2", "64.0.0.0/2"}, []string{"0.0.0.0/1"}},
		{[]string{"9.0.0.0/8", "1.0.0.0/8", "1.0.0.0/16"}, []string{"1.0.0.0/8", "9.0.0.0/8"}}, // numeric sort
		{[]string{"100.0.0.0/24", "20.0.0.0/24", "3.0.0.0/24"}, []string{"3.0.0.0/24", "20.0.0.0/24", "100.0.0.0/24"}},
	}
	for i, tc := range cases {
		if got := collapseCIDR(tc.in); !reflect.DeepEqual(got, tc.out) {
			t.Errorf("case %d: got %v want %v", i, got, tc.out)
		}
	}
	// a full /16 of /24s collapses to one /16
	var in []string
	for i := 0; i < 256; i++ {
		in = append(in, fmt.Sprintf("10.20.%d.0/24", i))
	}
	if got := collapseCIDR(sortedUnique(in)); !reflect.DeepEqual(got, []string{"10.20.0.0/16"}) {
		t.Fatal(got)
	}
}

func TestRunningConfigParser(t *testing.T) {
	s := parseRunningConfig([]string{
		"interface Proxy0",
		"ip route 1.2.3.0 255.255.255.0 Proxy0 1200",
		"ip route 1.2.3.4 Proxy0 1200",
		"ip route 5.5.5.0 255.255.255.0 10.0.0.1 ISP auto",
		"ip route 6.6.6.0 255.255.255.0 Proxy0",
		"ip route 7.7.7.0 255.255.255.0 Proxy0 1 2",
		"ip route 8.8.8.0 255.255.255.0 Proxy0 weird",
		"ip route foo",
		"  ip route 9.9.9.0 255.255.255.0 Proxy0 1200 exclusive",
	})
	if s.Unknown != 3 || len(s.Routes) != 8 {
		t.Fatalf("unknown=%d routes=%d", s.Unknown, len(s.Routes))
	}
	if s.identityCount("1.2.3.4/32", "Proxy0") != 1 || s.attributeCount("1.2.3.0/24", "Proxy0", 1200) != 1 {
		t.Fatal("identity/attribute")
	}
	if s.attributeCount("6.6.6.0/24", "Proxy0", 1200) != 0 || s.identityCount("6.6.6.0/24", "Proxy0") != 1 {
		t.Fatal("implicit metric")
	}
	if s.identityCount("5.5.5.0/24", "ISP") != 0 || s.targetCount("5.5.5.0/24", "ISP") != 1 {
		t.Fatal("gateway route")
	}
	if !s.Routes[7].Exclusive || s.Routes[7].metricOr(0) != 1200 {
		t.Fatal("exclusive")
	}
}

func TestPinnedCatalogSourcesParse(t *testing.T) {
	for _, b := range builtins {
		for _, f := range b.Files {
			data, err := readCloneFile(b.Directory, f.Name)
			if err != nil {
				t.Skipf("clone unavailable: %v", err)
			}
			if sha256Hex(data) != f.SHA256 || len(data) != f.Size {
				t.Fatalf("%s/%s does not match the pinned digest", b.Directory, f.Name)
			}
			r := parseBat(data, true, 0)
			if r.ErrorCount != 0 || len(r.Routes) == 0 || len(sortedUnique(r.Routes)) > b.MaxRoutes {
				t.Fatalf("%s: errors=%v routes=%d", f.Name, r.Errors, len(r.Routes))
			}
			_, bogons := filterBogons(sortedUnique(r.Routes))
			want := ""
			if b.ID == "netflix" {
				want = "100.64.0.0/10"
			}
			if strings.Join(bogons, ",") != want {
				t.Fatalf("%s: bogons %v, want %q", b.ID, bogons, want)
			}
		}
	}
	if len(builtins) != 12 {
		t.Fatalf("%d built-in bundles", len(builtins))
	}
	// one alphabetical list
	for i := 1; i < len(builtins); i++ {
		if strings.ToLower(builtins[i-1].Name) > strings.ToLower(builtins[i].Name) {
			t.Fatalf("builtins not sorted: %s > %s", builtins[i-1].Name, builtins[i].Name)
		}
	}
}

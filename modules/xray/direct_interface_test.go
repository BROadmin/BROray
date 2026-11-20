package xray

import (
	"io"
	"strings"
	"testing"
)

func TestDirectInterfaceDefaultRoute(t *testing.T) {
	row := func(name, destination, flags, metric string) string {
		return name + "\t" + destination + "\t01807B64\t" + flags + "\t0\t0\t" + metric + "\t00000000\t0\t0\t0\n"
	}
	wan := row("eth3", "00000000", "0003", "1000")
	for _, tt := range []struct{ name, table, want string }{
		{"default", wan, "eth3"}, {"duplicates", wan + wan, "eth3"},
		{"proxy-more-specific", row("t2s1", "00341002", "0001", "1") + wan, "eth3"},
		{"lowest-metric", row("ppp0", "00000000", "0001", "2000") + wan, "eth3"},
		{"lower-resolves-tie", row("ppp0", "00000000", "0001", "2000") + row("eth2", "00000000", "0001", "2000") + wan, "eth3"},
		{"equal-distinct", wan + row("ppp0", "00000000", "0001", "1000"), ""},
		{"no-default", "Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n", ""},
		{"down", row("eth3", "00000000", "0002", "1"), ""},
		{"reject", row("eth3", "00000000", "0201", "1"), ""},
		{"proxy-default", row("t2s1", "00000000", "0001", "1"), ""},
		{"bridge-default", row("br0", "00000000", "0001", "1"), ""},
		{"loopback", row("lo", "00000000", "0001", "1"), ""},
		{"metric-malformed", row("eth3", "00000000", "0001", "bad"), ""},
		{"flags-malformed", row("eth3", "00000000", "badflag", "1"), ""},
		{"truncated", "eth3 00000000\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDirectInterface(strings.NewReader(tt.table))
			if tt.want == "" {
				if err == nil {
					t.Fatal("unsafe default accepted", got)
				}
			} else if err != nil || got != tt.want {
				t.Fatal(got, err)
			}
		})
	}
	if _, err := parseDirectInterface(io.MultiReader(strings.NewReader(wan), failingRouteReader{})); err == nil {
		t.Fatal("partial route read accepted")
	}
}

type failingRouteReader struct{}

func (failingRouteReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

package keenetic

import (
	"strings"
	"testing"
	"time"
)

func TestInternetStatusConfirmsOnlyFreshGatewayOutage(t *testing.T) {
	now := time.Date(2026, 11, 25, 18, 57, 40, 0, time.UTC)
	offline := "checked: Wed Nov 25 18:57:20 2026\nreliable: yes\ngateway-accessible: no\ndns-accessible: no\nhost-accessible: no\ninternet: no\n"
	for _, tc := range []struct {
		name, status string
		want         bool
	}{
		{"fresh", offline, true},
		{"Basedecor field layout", strings.Replace(offline, "host-accessible: no\n", "", 1), true},
		{"indented", "   " + strings.ReplaceAll(offline, "\n", "\r\n   "), true},
		{"default gateway available", strings.Replace(offline, "gateway-accessible: no", "gateway-accessible: yes", 1), false},
		{"dns still available", strings.Replace(offline, "dns-accessible: no", "dns-accessible: yes", 1), false},
		{"host still available", strings.Replace(offline, "host-accessible: no", "host-accessible: yes", 1), false},
		{"internet available", strings.Replace(offline, "internet: no", "internet: yes", 1), false},
		{"unreliable", strings.Replace(offline, "reliable: yes", "reliable: no", 1), false},
		{"stale", strings.Replace(offline, "18:57:20", "18:48:20", 1), false},
		{"future", strings.Replace(offline, "18:57:20", "18:58:20", 1), false},
		{"missing", strings.Replace(offline, "gateway-accessible: no\n", "", 1), false},
		{"duplicate", offline + "internet: yes\n", false},
		{"oversized", offline + strings.Repeat(" ", 40<<10), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checked, negative := negativeInternetCheck(tc.status)
			if got := negative && recentInternetCheck(checked, now); got != tc.want {
				t.Fatalf("outage %t, want %t", got, tc.want)
			}
		})
	}
}

func TestInternetStatusKeeneticClock(t *testing.T) {
	const native = "weekday: 3\n day: 25\n month: 11\n year: 2026\n hour: 16\n min: 37\n sec: 14\n msec: 83\n dst: inactive\n tz:\n  locality: Europe/Moscow\n  stdoffset: 10800\n"
	now, ok := parseKeeneticClock(native)
	if !ok || !recentInternetCheck("Wed Nov 25 16:36:00 2026", now) ||
		recentInternetCheck("Wed Nov 25 13:37:10 2026", now) ||
		recentInternetCheck("Wed Nov 25 16:37:15 2026", now) {
		t.Fatal("wrong freshness against the router clock", now, ok)
	}
	for _, invalid := range []string{
		strings.Replace(native, "sec: 14", "sec: 60", 1),
		strings.Replace(native, "day: 25", "day: 31", 1),
		strings.Replace(native, "month: 11", "month: 13", 1),
		strings.Replace(native, "hour: 16", "hour:", 1),
		strings.Replace(native, " min: 37\n", "", 1),
		native + "year: 2026\n",
		strings.Repeat(" ", 4097) + native,
	} {
		if _, ok := parseKeeneticClock(invalid); ok {
			t.Fatal("accepted incomplete or invalid router clock")
		}
	}
}

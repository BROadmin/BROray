package subscriptions

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"
)

// Only previously missing bounded classes. The evidence note maps existing
// ML-KEM, KCP legacy and SS2022 chain controls without duplicating them here.
// Admission and exact generated values are asserted; no network/core process
// is started and product validation functions are not expectation oracles.
func TestParserRemainingWireGuardListBounds(t *testing.T) {
	for _, tc := range []struct {
		path string
		max  int
	}{{"address", 32}, {"remoteDNS", 16}, {"peers.0.allowedIPs", 128}} {
		for _, count := range []int{tc.max, tc.max + 1} {
			t.Run(fmt.Sprintf("WireGuard/%s/count-%d", tc.path, count), func(t *testing.T) {
				in := parserCoverageBase("wireguard")
				values := []any{}
				for i := 0; i < count; i++ {
					switch tc.path {
					case "address":
						values = append(values, fmt.Sprintf("192.0.2.%d/32", i+1))
					case "remoteDNS":
						values = append(values, fmt.Sprintf("192.0.2.%d", i+1))
					case "peers.0.allowedIPs":
						values = append(values, fmt.Sprintf("10.0.%d.0/24", i))
					}
				}
				parserCoverageSet(in, "settings."+tc.path, values)
				_, out := parserCoverageStage(t, in, count == tc.max)
				if count == tc.max {
					parserCoverageAssert(t, out, "settings."+tc.path, values, false)
				}
			})
		}
	}
	t.Run("WireGuard/peers/count-0", func(t *testing.T) {
		in := parserCoverageBase("wireguard")
		parserCoverageSet(in, "settings.peers", []any{})
		parserCoverageStage(t, in, false)
	})
}

func TestParserRemainingWireGuardIPv6MTU(t *testing.T) {
	for _, mtu := range []int{1279, 1280} {
		t.Run(fmt.Sprintf("WireGuard/mtu/ipv6-%d", mtu), func(t *testing.T) {
			in := parserCoverageBase("wireguard")
			addresses := []any{"192.0.2.2/32", "2001:db8::2/128"}
			parserCoverageSet(in, "settings.address", addresses)
			parserCoverageSet(in, "settings.mtu", mtu)
			_, out := parserCoverageStage(t, in, mtu == 1280)
			if mtu == 1280 {
				parserCoverageAssert(t, out, "settings.address", addresses, false)
				parserCoverageAssert(t, out, "settings.mtu", 1280, false)
			}
		})
	}
}

func TestParserRemainingShadowsocks2022Keys(t *testing.T) {
	for _, tc := range []struct {
		method string
		bytes  int
	}{{"2022-blake3-aes-128-gcm", 16}, {"2022-blake3-aes-256-gcm", 32}, {"2022-blake3-chacha20-poly1305", 32}} {
		for _, size := range []int{tc.bytes - 1, tc.bytes + 1} {
			t.Run(fmt.Sprintf("Shadowsocks/%s/key-bytes-%d", tc.method, size), func(t *testing.T) {
				in := parserCoverageBase("shadowsocks")
				parserCoverageSet(in, "settings.method", tc.method)
				parserCoverageSet(in, "settings.password", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, size)))
				parserCoverageStage(t, in, false)
			})
		}
		t.Run("Shadowsocks/"+tc.method+"/malformed-key-base64", func(t *testing.T) {
			in := parserCoverageBase("shadowsocks")
			parserCoverageSet(in, "settings.method", tc.method)
			parserCoverageSet(in, "settings.password", "not!base64")
			parserCoverageStage(t, in, false)
		})
		if tc.method == "2022-blake3-chacha20-poly1305" {
			continue // Existing TestParseLinkReference rejects its key chain.
		}
		key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, tc.bytes))
		short := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, tc.bytes-1))
		for _, bad := range []struct{ name, password string }{{"empty-middle-component", key + "::" + key}, {"short-last-component", key + ":" + short}} {
			t.Run("Shadowsocks/"+tc.method+"/"+bad.name, func(t *testing.T) {
				in := parserCoverageBase("shadowsocks")
				parserCoverageSet(in, "settings.method", tc.method)
				parserCoverageSet(in, "settings.password", bad.password)
				parserCoverageStage(t, in, false)
			})
		}
	}
	t.Run("Shadowsocks/2022-blake3-aes-128-gcm/two-key-chain-preserved", func(t *testing.T) {
		in := parserCoverageBase("shadowsocks")
		method := "2022-blake3-aes-128-gcm"
		password := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16)) + ":" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 16))
		parserCoverageSet(in, "settings.method", method)
		parserCoverageSet(in, "settings.password", password)
		_, out := parserCoverageStage(t, in, true)
		parserCoverageAssert(t, out, "settings.servers.0.method", method, false)
		parserCoverageAssert(t, out, "settings.servers.0.password", password, false)
	})
}

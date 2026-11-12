package clashimport

import (
	"net/netip"
	"regexp"
	"strings"

	"github.com/BROadmin/broray/internal/xrayimport"
)

var canonicalUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validateTUIC(p map[string]any, checks xrayimport.Checks) {
	need(textValue(p["name"]) && textValue(p["server"]) && p["server"] != "" && isInt(p["port"], 1, 65535))
	need(!strings.ContainsAny(p["server"].(string), " /?#@"))
	// v4 token and v5 UUID/password are alternative authentication modes.
	// A malformed UUID must not silently become uuid.Nil during import.
	if stringField(p, "token", "") == "" {
		need(canonicalUUID.MatchString(stringField(p, "uuid", "")))
		stringField(p, "password", "")
	} else {
		if has(p, "uuid") {
			need(canonicalUUID.MatchString(stringField(p, "uuid", "")))
		}
		stringField(p, "password", "")
	}
	if ip := stringField(p, "ip", ""); ip != "" {
		_, err := netip.ParseAddr(ip)
		need(err == nil)
	}
	if has(p, "udp-over-stream-version") {
		need(isInt(p["udp-over-stream-version"], 0, 2))
	}
	need(inList(stringField(p, "udp-relay-mode", ""), "", "native", "quic"))
	need(inList(stringField(p, "congestion-controller", ""), "", "cubic", "new_reno", "bbr"))
	if has(p, "ip-version") {
		need(inList(stringField(p, "ip-version", ""), "dual", "ipv4", "ipv6", "ipv4-prefer", "ipv6-prefer"))
	}
	// TLS pins and inline certificate/key material are validated even though
	// this entire backend is unavailable. No unavailable path bypasses them.
	stream := map[string]any{}
	configureTLS(without(p, "skip-cert-verify"), "trojan", stream)
	need(checks.ValidateClientFields != nil && checks.ValidateClientFields("vless", "raw", "tls", map[string]any{"tls": stream["tlsSettings"]}) == nil)
}

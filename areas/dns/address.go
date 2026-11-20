package dns

import "net/netip"

// Canonicalize identity only; keep the original observed spelling in receipts,
// confirmation fingerprints and rollback records. All other attributes remain
// part of their existing exact comparison.
func addressKey(value string) string {
	a, err := netip.ParseAddr(value)
	if err == nil && a.Zone() == "" && !a.Is4In6() {
		return a.String()
	}
	return value
}

func sameAddress(a, b string) bool { return addressKey(a) == addressKey(b) }

func addressFamily(value string) string {
	a, err := netip.ParseAddr(value)
	if err != nil || a.Zone() != "" || a.Is4In6() {
		return "unknown"
	}
	if a.Is6() {
		return "IPv6"
	}
	return "IPv4"
}

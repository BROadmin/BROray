package routes

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

func routeFamily(network string) string {
	if strings.Contains(network, ":") {
		return "ipv6"
	}
	return "ipv4"
}

func ipv6Prefix(text string) (netip.Prefix, bool) {
	p, err := netip.ParsePrefix(text)
	return p, err == nil && p.Addr().Is6() && !p.Addr().Is4In6() && p.Bits() > 0
}

func validRouteNetwork(family, network string, prefix int) bool {
	return family == routeFamily(network) && networkAligned(network, prefix)
}

func routeGateway(network string) string {
	if routeFamily(network) == "ipv6" {
		return "::"
	}
	return "0.0.0.0"
}

func routeAttributes(network string, prefix int) (string, int) {
	if routeFamily(network) == "ipv6" {
		return "", 0
	}
	return maskString(prefix), managedMetric
}

func managedRouteAttributes(m obj, legacyDefault int) bool {
	if routeFamily(str(m, "network")) != "ipv6" {
		return intOr(m["metric"], legacyDefault) == managedMetric
	}
	metric, ok := intOf(m["metric"])
	network, prefix, iface := str(m, "network"), intOr(m["prefix"], 0), str(m, "interface")
	return ok && metric == 0 && str(m, "family") == "ipv6" && str(m, "mask") == "" && str(m, "gatewayToken") == "::" &&
		networkAligned(network, prefix) && str(m, "key") == routeKey(network, prefix, iface)
}

// Stack aggregation is lossless and never merges two /1 routes into /0.
func collapseIPv6(input []string) []string {
	var list []netip.Prefix
	for _, s := range input {
		if p, ok := ipv6Prefix(s); ok {
			list = append(list, p.Masked())
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Addr() != list[j].Addr() {
			return list[i].Addr().Less(list[j].Addr())
		}
		return list[i].Bits() < list[j].Bits()
	})
	stack := make([]netip.Prefix, 0, len(list))
	for _, p := range list {
		if len(stack) > 0 && stack[len(stack)-1].Contains(p.Addr()) && stack[len(stack)-1].Bits() <= p.Bits() {
			continue
		}
		stack = append(stack, p)
		for len(stack) > 1 {
			a, b := stack[len(stack)-2], stack[len(stack)-1]
			if a.Bits() != b.Bits() || a.Bits() <= 1 {
				break
			}
			parent := netip.PrefixFrom(a.Addr(), a.Bits()-1).Masked()
			if !parent.Contains(b.Addr()) {
				break
			}
			stack = append(stack[:len(stack)-2], parent)
		}
	}
	out := make([]string, len(stack))
	for i, p := range stack {
		out[i] = p.String()
	}
	return out
}

// IPv6 running-config has no IPv4 ownership metric. Registry provenance is
// still required before a route can be deleted; an existing route is external.
func parseIPv6Route(f []string, line int) rcRoute {
	r := rcRoute{Line: line, Gateway: "::"}
	bad := func() rcRoute { r.Known = false; r.Reason = "unsupported-ipv6-route"; return r }
	if len(f) < 4 {
		return bad()
	}
	prefix := f[2]
	if prefix == "default" {
		prefix = "::/0"
	}
	p, err := netip.ParsePrefix(prefix)
	if err != nil || !p.Addr().Is6() || p.Addr().Is4In6() {
		return bad()
	}
	p = p.Masked()
	r.Network = p.Addr().String()
	r.Prefix = p.Bits()
	r.Destination = p.String()
	idx := 4
	if a, e := netip.ParseAddr(f[3]); e == nil && a.Is6() {
		r.Gateway = a.String()
	} else {
		r.Interface = f[3]
	}
	if len(f) > 4 {
		if a, e := netip.ParseAddr(f[4]); e == nil && a.Is6() {
			r.Gateway = a.String()
			idx = 5
		}
	}
	for _, opt := range f[idx:] {
		switch opt {
		case "auto":
			r.Automatic = true
		case "reject":
			r.Exclusive = true
		default:
			if !allDigits(opt) || r.MetricExplicit {
				return bad()
			}
			n, e := strconv.Atoi(opt)
			if e != nil {
				return bad()
			}
			r.Metric = &n
			r.MetricExplicit = true
		}
	}
	r.Known = true
	return r
}

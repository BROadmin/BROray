package dns

import (
	"context"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

var componentName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Only the observed ndw.components CSV schema is authoritative. Neither a
// firmware number, an unreadable inventory nor ndns proves DoT availability.
func installedComponents(version map[string]any) (map[string]bool, bool) {
	ndw, ok := version["ndw"].(map[string]any)
	if !ok {
		return nil, false
	}
	csv, ok := ndw["components"].(string)
	if !ok || len(csv) == 0 || len(csv) > 16384 {
		return nil, false
	}
	parts := strings.Split(csv, ",")
	if len(parts) > 512 {
		return nil, false
	}
	found := make(map[string]bool, len(parts))
	for _, part := range parts {
		if !componentName.MatchString(part) || found[part] {
			return nil, false
		}
		found[part] = true
	}
	return found, found["base"]
}

func (s *service) capabilities(ctx context.Context, obs observed) map[string]any {
	version := map[string]any{}
	if s.d.App != nil && s.d.App.T != nil {
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := s.d.App.T.Show(bounded, "show/version", &version); err != nil {
			version = nil
		}
	}
	firmware, _ := version["release"].(string)
	if len(firmware) > 64 || strings.ContainsAny(firmware, "\r\n\x00") {
		firmware = ""
	}
	inventory, valid := installedComponents(version)
	component, source, code := "unknown", "unavailable", "DOT_CAPABILITY_UNKNOWN"
	if valid {
		component, source, code = "missing", "show-version.ndw.components", "DOT_COMPONENT_MISSING"
		if inventory["dns-tls"] {
			component, code = "present", ""
		}
	}
	runtime, ok := obs.Runtime.(runtimeObs)
	runtimeProof := ok && obs.Determinate && obs.RuntimeReconciled && runtime.Determinate && runtime.SystemProxyTLSSections == 1
	if runtimeProof {
		if component == "missing" {
			component, source, code = "unknown", "conflicting-observations", "DOT_CAPABILITY_CONFLICT"
		} else if component == "unknown" {
			component, source, code = "present", "reconciled-system-tls", ""
		}
	}
	ipv6 := "unknown"
	if component == "missing" {
		ipv6 = "component-missing"
	}
	if component == "present" && runtimeProof {
		for _, row := range runtime.Dot {
			ip, err := netip.ParseAddr(row.Address)
			if err == nil && ip.Is6() && !ip.Is4In6() && row.clean() {
				ipv6 = "confirmed"
				break
			}
		}
	}
	evidence := ""
	if ipv6 == "confirmed" {
		evidence = "reconciled-router-runtime"
	}
	return map[string]any{"dotComponent": component, "source": source, "code": code, "firmware": firmware,
		"ipv6DoT": ipv6, "ipv6Evidence": evidence, "minimumFirmware": nil}
}

func addCapabilities(status, capabilities map[string]any) {
	status["capabilities"] = capabilities
	switch capabilities["dotComponent"] {
	case "present":
		status["supported"] = true
	case "missing":
		status["supported"] = false
		status["mutationAvailable"] = false
		status["mutationBlockedReason"] = "dot-component-missing"
		status["deleteEligible"] = false
		status["deleteBlockedReason"] = "dot-component-missing"
	default:
		status["supported"] = nil
	}
}

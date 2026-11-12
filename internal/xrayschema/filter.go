package xrayschema

import (
	"net"
	"strconv"
	"strings"
)

// Pinned Filter wraps server listeners only; both client wrappers are no-ops.
// Validate the declaration before detaching it. Geodata references are checked
// as syntax and retained unavailable, without opening provider-supplied paths.
func validateFilterRules(settings map[string]any, path string) (bool, error) {
	resource := false
	for _, field := range []string{"blockedSourceIPs", "allowedSourceIPs"} {
		value, exists := settings[field]
		if !exists {
			continue
		}
		list, ok := value.([]any)
		if !ok || list == nil {
			return false, invalid(path + "." + field)
		}
		for _, item := range list {
			rule, ok := item.(string)
			if !ok {
				return false, invalid(path + "." + field)
			}
			rule = strings.TrimLeft(rule, "!")
			if strings.HasPrefix(rule, "geoip:") {
				rule = "ext:geoip.dat:" + strings.TrimPrefix(rule, "geoip:")
			}
			if strings.HasPrefix(rule, "ext:") || strings.HasPrefix(rule, "ext-ip:") {
				_, reference, _ := strings.Cut(rule, ":")
				file, code, found := strings.Cut(reference, ":")
				if !found || file == "" || strings.TrimLeft(code, "!") == "" {
					return false, invalid(path + "." + field)
				}
				resource = true
				continue
			}
			address, prefix, _ := strings.Cut(rule, "/")
			if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
				address = address[1 : len(address)-1]
			}
			ip := net.ParseIP(strings.TrimSpace(address))
			if ip == nil {
				return false, invalid(path + "." + field)
			}
			max := uint64(128)
			if ip.To4() != nil {
				max = 32
			}
			if prefix != "" {
				bits, err := strconv.ParseUint(prefix, 10, 32)
				if err != nil || bits > max {
					return false, invalid(path + "." + field)
				}
			}
		}
	}
	return resource, nil
}

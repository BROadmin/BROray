package subscriptions

import (
	"github.com/BROadmin/broray/areas/servers"
	"strings"
)

// Extract supported extensions, then let the existing transport converter
// validate every remaining field. Nothing unrecognized is removed.
func transport(sv any) []string {
	original := obj(sv)
	s := without(original, "sockopt")
	extra := map[string]any{}
	if has(original, "sockopt") {
		need(keysOnly(original["sockopt"], "tcpFastOpen", "tcpKeepAliveIdle", "tcpKeepAliveInterval", "domainStrategy"))
		extra["sockopt"] = obj(original["sockopt"])
	}
	for _, block := range []string{"wsSettings", "httpupgradeSettings", "grpcSettings"} {
		if !has(s, block) {
			continue
		}
		current := obj(s[block])
		clean := without(current)
		options := map[string]any{}
		fields := []string{"heartbeatPeriod"}
		if block == "grpcSettings" {
			fields = []string{"idle_timeout", "health_check_timeout", "permit_without_stream", "initial_windows_size", "user_agent"}
		}
		for _, k := range fields {
			if has(clean, k) {
				options[k] = clean[k]
				delete(clean, k)
			}
		}
		if block != "grpcSettings" && has(clean, "headers") {
			headers := obj(clean["headers"])
			other := map[string]any{}
			hostHeaders := map[string]any{}
			for k, v := range headers {
				if strings.EqualFold(k, "Host") {
					hostHeaders[k] = v
				} else {
					other[k] = v
				}
			}
			if len(other) > 0 {
				options["headers"] = other
			}
			clean["headers"] = hostHeaders
			// Both transports use the same explicit host conflict checks.
			host := websocketHost(clean)
			clean["host"] = host
			delete(clean, "headers")
		}
		if len(options) > 0 {
			extra[block] = options
		}
		s[block] = clean
	}
	net := network(original)
	if net == "tcp" {
		net = "raw"
	}
	need(servers.ValidateStreamOptions(net, extra) == nil)
	out := basicTransport(s)
	if len(extra) > 0 {
		out = append(out, pair("streamOptions", toJSON(extra)))
	}
	return out
}

func safeJSONField(s string) string {
	if len(s) == 0 || len(s) > 64 {
		return "unknown"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return "unknown"
		}
	}
	return s
}

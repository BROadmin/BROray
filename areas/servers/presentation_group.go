package servers

import (
	"net/netip"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
)

// presentationKey is private to the summary view. It must never be used for
// persistence, import reconciliation, activation, or automatic server selection.
func presentationKey(rec Record) (string, error) {
	s := normalize(rec) // Detach all nested maps before removing display-only differences.
	if s["network"] == "xdrive" || s["protocol"] == "masque" {
		return CanonicalKey(s)
	}
	if s["protocol"] == "amneziawg" {
		profile, ok := s["amneziawg"].(map[string]any)
		if !ok || validateAmnezia(s) != nil {
			return CanonicalKey(s)
		}
		// The parsed interface/peers retain all parameters, including extensions.
		// Remove only a redundant serialization whose content we can establish.
		if awg, ok := get(profile, "parameters", "awg").(map[string]any); ok {
			if raw, ok := awg["last_config"].(string); ok {
				value, err := strictjson.Decode([]byte(raw))
				client, ok := value.(map[string]any)
				if err != nil || !ok || client["config"] != profile["nativeConfig"] {
					return CanonicalKey(s)
				}
				delete(client, "config")
				awg["last_config"] = client
			}
		}
		delete(profile, "nativeConfig")
		return compat.CanonicalSHA256(map[string]any{"protocol": s["protocol"], "profile": profile})
	}
	if s["protocol"] == "wireguard" {
		// The imported settings already contain the complete parsed profile,
		// without INI comments or serialization metadata. Retain every setting.
		return CanonicalKey(s)
	}
	key := importKeyObject(s)
	address := strings.TrimSuffix(strings.ToLower(asString(s["address"])), ".")
	if ip, err := netip.ParseAddr(strings.Trim(address, "[]")); err == nil {
		address = ip.String()
	}
	key["address"] = address
	key["username"] = alt(s["username"], "")
	key["remoteDNS"] = s["remoteDNS"]
	for _, field := range []string{"serverName", "shortId", "fingerprint", "spiderX"} {
		delete(key, field)
	}
	if tls, ok := key["tls"].(map[string]any); ok {
		delete(tls, "serverName")
		delete(tls, "fingerprint")
	}
	if extra, ok := key["extra"].(map[string]any); ok {
		// Keep headers, paths, downloadSettings and protocol selectors. Only
		// explicitly approved padding / client performance knobs are omitted.
		for _, field := range []string{"xmux", "xPaddingBytes", "xPaddingObfsMode", "xPaddingKey", "xPaddingHeader", "xPaddingPlacement", "xPaddingMethod", "noGRPCHeader", "noSSEHeader", "scMaxEachPostBytes", "scMinPostsIntervalMs", "scMaxBufferedPosts", "scStreamUpServerSecs"} {
			delete(extra, field)
		}
		if mode, ok := extra["mode"].(string); ok && orStr(mode, "auto") == orStr(asString(key["mode"]), "auto") {
			delete(extra, "mode") // A validated duplicate of the top-level mode.
		}
		if headers, ok := extra["headers"].(map[string]any); ok && len(headers) == 0 {
			delete(extra, "headers")
		}
	}
	if key["network"] == "xhttp" {
		key["mode"] = orStr(asString(key["mode"]), "auto")
	}
	if options, ok := s["streamOptions"].(map[string]any); ok {
		// Client timing and window sizes do not identify a remote service.
		// Headers, user agent and address resolution policy can do so.
		for block, raw := range options {
			if fields, ok := raw.(map[string]any); ok {
				for field := range fields {
					if in(field, "tcpFastOpen", "tcpKeepAliveIdle", "tcpKeepAliveInterval", "heartbeatPeriod", "idle_timeout", "health_check_timeout", "initial_windows_size", "permit_without_stream") {
						delete(fields, field)
					}
				}
				if len(fields) == 0 {
					delete(options, block)
				}
			}
		}
		if len(options) > 0 {
			key["streamOptions"] = options
		}
	}
	return compat.CanonicalSHA256(key)
}

func (s *svc) presentationSubscriptionName(rec Record, names map[string]string) any {
	id := asString(get(rec, "source", "subscriptionId"))
	if id == "" {
		return nil
	}
	if name, ok := names[id]; ok {
		return name
	}
	name := id
	if ValidID(id) {
		if sub, ok := readObjectFile(s.d.Path("config", "subscriptions", id+".json")); ok && sub["id"] == id {
			if value := asString(sub["name"]); value != "" {
				name = value
			}
		}
	}
	names[id] = name
	return name
}

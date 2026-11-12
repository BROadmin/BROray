package servers

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/BROadmin/broray/internal/amnezia"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
)

// presentationKey is private to the summary view. It must never be used for
// persistence, import reconciliation, activation, or automatic server selection.
func presentationKey(rec Record) (string, error) {
	s := normalize(rec) // Detach all nested maps before removing display-only differences.
	// A core log/statistics label is not a different remote connection. Keep it
	// in the private record and reconciliation key, but not in the UI grouping.
	delete(s, "accountEmail")
	delete(s, "hysteriaMasquerade")
	// Xray's no-policy DefaultManager gives only level 1 a different idle
	// timeout. HTTP/SOCKS without an account always use level 0. Keep exact
	// imported levels in persistence; this is display grouping only.
	level, _ := freshUnsigned(s["accountLevel"], 32)
	if level != 1 || clientSS2022MetadataIgnored(s) || in(asString(s["protocol"]), "http", "socks") && asString(s["username"]) == "" && s["proxyAccount"] != true {
		delete(s, "accountLevel")
	}
	if s["proxyAccount"] != true {
		delete(s, "proxyAccount")
	}
	if reality, ok := s["reality"].(map[string]any); ok {
		delete(reality, "show")
	}
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
				if len(client) == 0 {
					delete(awg, "last_config")
				}
			}
			if awg["sourceFormat"] == "clash-yaml" {
				delete(awg, "sourceFormat")
				if opts, ok := awg["clashOptions"].(map[string]any); ok {
					for _, k := range []string{"name", "type", "server", "port", "private-key", "public-key", "pre-shared-key", "ip", "ipv6", "mtu", "dns", "persistent-keepalive", "udp"} {
						delete(opts, k)
					}
					if opts["remote-dns-resolve"] == false {
						delete(opts, "remote-dns-resolve")
					}
					if options, ok := opts["amnezia-wg-option"].(map[string]any); ok {
						for k := range options {
							if field, known := amnezia.ClashInterfaceField(k); known {
								// Older records kept these only in raw Clash. Do not
								// erase a value absent/different in the parsed model.
								if vals, ok := get(profile, "interface", field).([]any); ok && len(vals) == 1 && fmt.Sprint(vals[0]) == fmt.Sprint(options[k]) {
									delete(options, k)
								}
							}
						}
						if len(options) == 0 {
							delete(opts, "amnezia-wg-option")
						}
					}
					if len(opts) == 0 {
						delete(awg, "clashOptions")
					}
				}
			}
			if len(awg) == 0 {
				delete(profile["parameters"].(map[string]any), "awg")
			}
		}
		if params, ok := profile["parameters"].(map[string]any); ok && params["sourceFormat"] == "base64-ini" {
			delete(params, "sourceFormat")
		}
		if iface, ok := profile["interface"].(map[string]any); ok {
			normalizeProfileLists(iface, "Address", "DNS")
			// AWG INI uses on/off or 0/1; Clash emits JSON booleans.
			// Normalize only these documented flags for display grouping.
			for _, field := range []string{"RandomTrailers", "DisableCookies"} {
				if vals, ok := iface[field].([]any); ok && len(vals) == 1 {
					switch strings.ToLower(asString(vals[0])) {
					case "on", "1", "true":
						vals[0] = "true"
					case "off", "0", "false":
						vals[0] = "false"
					}
				}
			}
		}
		if peers, ok := profile["peers"].([]any); ok {
			for _, p := range peers {
				if m, ok := p.(map[string]any); ok {
					normalizeProfileLists(m, "AllowedIPs")
				}
			}
		}
		delete(profile, "nativeConfig")
		return compat.CanonicalSHA256(map[string]any{"protocol": s["protocol"], "profile": profile})
	}
	if s["protocol"] == "wireguard" {
		if peers, ok := get(s, "wireguard", "peers").([]any); ok {
			for _, raw := range peers {
				if peer, ok := raw.(map[string]any); ok {
					delete(peer, "email")
					delete(peer, "level")
				}
			}
		}
		// The imported settings already contain the complete parsed profile,
		// without comments or server-only metadata. Retain connection settings.
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

func normalizeProfileLists(m map[string]any, keys ...string) {
	for _, k := range keys {
		values, ok := m[k].([]any)
		if !ok {
			continue
		}
		out := []any{}
		for _, v := range values {
			for _, item := range strings.Split(asString(v), ",") {
				out = append(out, strings.TrimSpace(item))
			}
		}
		m[k] = out
	}
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

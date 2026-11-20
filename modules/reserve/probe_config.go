package reserve

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strings"

	"github.com/BROadmin/broray/internal/strictjson"
)

const HealthInboundTag = "broray-reserve-health"
const healthDenyTag = "broray-reserve-health-deny"

// AttachHealthInbound renders a local, control-only diagnostic path in an
// existing Xray configuration. It does not apply or persist the result.
func AttachHealthInbound(config []byte, endpoint netip.AddrPort) ([]byte, error) {
	fail := errors.New("RESERVE_HEALTH_CONFIG_UNCONFIRMED")
	if !endpoint.IsValid() || !endpoint.Addr().IsLoopback() || endpoint.Addr().Zone() != "" || endpoint.Addr().Is4In6() || endpoint.Port() == 0 {
		return nil, fail
	}
	value, err := strictjson.Decode(config)
	if err != nil {
		return nil, fail
	}
	cfg, ok := value.(map[string]any)
	if !ok || cfg == nil {
		return nil, fail
	}
	for key := range cfg {
		for _, owned := range []string{"inbounds", "outbounds", "routing"} {
			if strings.EqualFold(key, owned) && key != owned {
				return nil, fail
			}
		}
	}
	inbounds, ok := cfg["inbounds"].([]any)
	if !ok || len(inbounds) == 0 {
		return nil, fail
	}
	seen := map[string]bool{}
	for _, raw := range inbounds {
		in, ok := raw.(map[string]any)
		if !ok {
			return nil, fail
		}
		tag, ok := in["tag"].(string)
		if !ok || tag == "" || tag == HealthInboundTag || seen[tag] {
			return nil, fail
		}
		seen[tag] = true
		port, ok := in["port"].(json.Number)
		if !ok {
			return nil, fail
		}
		n, err := port.Int64()
		if err != nil || n < 1 || n > 65535 || n == int64(endpoint.Port()) {
			return nil, fail
		}
	}
	outbounds, ok := cfg["outbounds"].([]any)
	if !ok || len(outbounds) == 0 {
		return nil, fail
	}
	seen = map[string]bool{}
	for i, raw := range outbounds {
		out, ok := raw.(map[string]any)
		if !ok {
			return nil, fail
		}
		tag, ok := out["tag"].(string)
		if !ok || tag == "" || tag == healthDenyTag || seen[tag] {
			return nil, fail
		}
		seen[tag] = true
		if i == 0 && tag != "proxy" {
			return nil, fail
		}
		if tag == "proxy" {
			switch out["protocol"] {
			case "vless", "vmess", "trojan", "shadowsocks", "socks", "http", "wireguard", "hysteria", "hysteria2", "masque":
			default:
				return nil, fail
			}
			if _, present := out["proxySettings"]; present {
				return nil, fail
			}
			if stream, ok := out["streamSettings"].(map[string]any); ok {
				if opts, ok := stream["sockopt"].(map[string]any); ok {
					if _, present := opts["dialerProxy"]; present {
						return nil, fail
					}
				}
			}
		}
	}
	routing := map[string]any{"domainStrategy": "AsIs"}
	rules := []any{}
	if raw, present := cfg["routing"]; present {
		routing, ok = raw.(map[string]any)
		if !ok || routing == nil {
			return nil, fail
		}
		rules, ok = routing["rules"].([]any)
		if !ok {
			return nil, fail
		}
		for _, raw := range rules {
			rule, ok := raw.(map[string]any)
			if !ok {
				return nil, fail
			}
			if rule["outboundTag"] == healthDenyTag {
				return nil, fail
			}
			if scope, ok := rule["inboundTag"].([]any); ok {
				for _, tag := range scope {
					if tag == HealthInboundTag {
						return nil, fail
					}
				}
			}
		}
	}
	inbound := map[string]any{"tag": HealthInboundTag, "listen": endpoint.Addr().String(), "port": endpoint.Port(), "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}
	allow := map[string]any{"type": "field", "inboundTag": []string{HealthInboundTag}, "network": "tcp", "port": "443", "domain": []string{"full:cp.cloudflare.com", "full:www.gstatic.com"}, "outboundTag": "proxy"}
	deny := map[string]any{"type": "field", "inboundTag": []string{HealthInboundTag}, "outboundTag": healthDenyTag}
	cfg["inbounds"] = append(inbounds, inbound)
	cfg["outbounds"] = append(outbounds, map[string]any{"tag": healthDenyTag, "protocol": "blackhole"})
	routing["rules"] = append([]any{allow, deny}, rules...)
	cfg["routing"] = routing
	return json.Marshal(cfg)
}

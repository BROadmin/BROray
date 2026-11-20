package routepolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/BROadmin/broray/modules/geodata"
)

var directInterfacePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$`)

func Render(config []byte, rules []Rule, snapshot geodata.Snapshot, assetPrefix string, tags OutboundTags, inboundTags []string, directInterface string) ([]byte, error) {
	compiled, err := Compile(rules, snapshot, tags, inboundTags)
	if err != nil {
		return nil, err
	}
	if len(compiled) == 0 {
		return config, nil
	}
	if assetPrefix != geodata.AssetDirectory {
		return nil, fmt.Errorf("invalid managed asset prefix")
	}
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()
	var cfg map[string]any
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("configuration must be object")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("trailing configuration data")
	}
	if cfg["routing"] != nil {
		return nil, fmt.Errorf("existing routing cannot be replaced")
	}
	outbounds, ok := cfg["outbounds"].([]any)
	if !ok || len(outbounds) == 0 {
		return nil, fmt.Errorf("VPN outbound required")
	}
	first, _ := outbounds[0].(map[string]any)
	if first["tag"] != tags.VPN {
		return nil, fmt.Errorf("default outbound must remain VPN")
	}
	existing := map[string]bool{}
	for _, out := range outbounds {
		m, ok := out.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid outbound")
		}
		tag, _ := m["tag"].(string)
		existing[tag] = true
	}
	used := map[string]bool{}
	for i, raw := range compiled {
		var rule map[string]any
		if err := json.Unmarshal(raw, &rule); err != nil {
			return nil, err
		}
		outbound, _ := rule["outboundTag"].(string)
		used[outbound] = true
		for _, field := range []string{"ip", "domain"} {
			values, _ := rule[field].([]any)
			for j, value := range values {
				reference, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("invalid compiled matcher")
				}
				if strings.HasPrefix(reference, "ext:") {
					values[j] = "ext:" + assetPrefix + "/" + strings.TrimPrefix(reference, "ext:")
				}
			}
		}
		compiled[i], err = json.Marshal(rule)
		if err != nil {
			return nil, err
		}
	}
	for _, out := range []struct{ tag, protocol string }{{tags.Direct, "freedom"}, {tags.Deny, "blackhole"}} {
		if !used[out.tag] {
			continue
		}
		if existing[out.tag] {
			return nil, fmt.Errorf("policy outbound tag already occupied")
		}
		entry := map[string]any{"tag": out.tag, "protocol": out.protocol}
		if out.tag == tags.Direct {
			// Kernel ProxyN routes must not capture this outbound again.
			if !directInterfacePattern.MatchString(directInterface) {
				return nil, fmt.Errorf("DIRECT_EGRESS_INTERFACE_REQUIRED")
			}
			// A single unreachable A/AAAA address must not hide a working peer.
			entry["streamSettings"] = map[string]any{"sockopt": map[string]any{
				"domainStrategy": "UseIP",
				"interface":      directInterface,
				"happyEyeballs":  map[string]any{"tryDelayMs": 250, "prioritizeIPv6": false, "interleave": 1, "maxConcurrentTry": 4},
			}}
		}
		outbounds = append(outbounds, entry)
	}
	scope := map[string]bool{}
	for _, tag := range inboundTags {
		scope[tag] = false
	}
	domainRules := false
	for _, rule := range rules {
		if rule.Enabled && (len(rule.Domains) > 0 || len(rule.GeoSiteTags) > 0) {
			domainRules = true
		}
	}
	inbounds, ok := cfg["inbounds"].([]any)
	if !ok {
		return nil, fmt.Errorf("inbound scope unavailable")
	}
	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid inbound")
		}
		tag, _ := inbound["tag"].(string)
		if _, owned := scope[tag]; !owned {
			continue
		}
		if scope[tag] || inbound["protocol"] != "socks" {
			return nil, fmt.Errorf("policy scope must be a unique SOCKS inbound")
		}
		scope[tag] = true
		if domainRules && inbound["sniffing"] == nil {
			inbound["sniffing"] = map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}
		}
	}
	for _, found := range scope {
		if !found {
			return nil, fmt.Errorf("policy inbound does not exist")
		}
	}
	cfg["outbounds"] = outbounds
	cfg["routing"] = map[string]any{"domainStrategy": DomainStrategy, "rules": compiled}
	return json.Marshal(cfg)
}

package reserve

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/strictjson"
)

const (
	hotControlTag = "broray-reserve-control"
	hotSelector   = "broray-reserve"
	HotBlockedTag = "broray-reserve-blocked"
)

var errHotLayout = errors.New("RESERVE_HOT_LAYOUT_UNSUPPORTED")

func HotSlot(index int) string {
	return "broray-reserve-slot-" + strconv.Itoa(index+1)
}

// HotLayoutSocket detects the marker. Callers must still verify the complete
// layout and match the socket to the local supervisor's reserved path.
func HotLayoutSocket(data []byte) (string, bool) {
	cfg := decodeHotConfig(data)
	api, _ := cfg["api"].(map[string]any)
	if api["tag"] != hotControlTag {
		return "", false
	}
	listen, _ := api["listen"].(string)
	path, _ := strings.CutSuffix(listen, ",0600")
	return path, true
}

func decodeHotConfig(data []byte) map[string]any {
	decoded, err := strictjson.Decode(data)
	if err != nil {
		return nil
	}
	cfg, _ := decoded.(map[string]any)
	return cfg
}

func hotSocketPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		len(path) <= 107 && !strings.ContainsAny(path, "\x00\t\n\r ,")
}

// BuildHotLayout keeps the managed routes in order and sends each VPN decision
// through the selected slot. A new Xray generation starts with a blackhole.
func BuildHotLayout(profiles [][]byte, endpoint netip.AddrPort, socket string) ([]byte, error) {
	if len(profiles) < 2 || len(profiles) > 3 || !hotSocketPath(socket) {
		return nil, errHotLayout
	}
	outbounds := []any{map[string]any{"tag": HotBlockedTag, "protocol": "blackhole"}}
	var shared map[string]any
	var common []byte
	for index, profile := range profiles {
		cfg := decodeHotConfig(profile)
		if !validSingleProbeConfig(decodeHotConfig(profile), endpoint) {
			return nil, errHotLayout
		}
		slot, ok := detachStockSlot(cfg)
		if !ok {
			return nil, errHotLayout
		}
		slot["tag"] = HotSlot(index)
		outbounds = append(outbounds, slot)
		data, err := json.Marshal(cfg)
		if err != nil {
			return nil, errHotLayout
		}
		if index == 0 {
			shared, common = cfg, data
		} else if !bytes.Equal(common, data) {
			return nil, errHotLayout
		}
	}
	return finishHotLayout(shared, outbounds, socket)
}

func detachStockSlot(cfg map[string]any) (map[string]any, bool) {
	for key := range cfg {
		switch key {
		case "log", "inbounds", "outbounds", "routing":
		default:
			return nil, false
		}
	}
	inbounds, _ := cfg["inbounds"].([]any)
	outbounds, _ := cfg["outbounds"].([]any)
	if len(inbounds) != 2 || len(outbounds) < 2 {
		return nil, false
	}
	in, _ := inbounds[0].(map[string]any)
	slot, _ := outbounds[0].(map[string]any)
	if in["tag"] != "socks" || in["protocol"] != "socks" || slot["tag"] != "proxy" || slot["protocol"] != "vless" {
		return nil, false
	}
	destinations := map[string]bool{"proxy": true, healthDenyTag: true}
	for _, entry := range outbounds[1 : len(outbounds)-1] {
		out, _ := entry.(map[string]any)
		tag, _ := out["tag"].(string)
		switch {
		case tag == "broray-direct" && out["protocol"] == "freedom",
			tag == "broray-deny" && out["protocol"] == "blackhole":
			destinations[tag] = true
		default:
			return nil, false
		}
	}
	routing, _ := cfg["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	if len(routing) != 2 || len(rules) < 2 ||
		(routing["domainStrategy"] != "AsIs" && routing["domainStrategy"] != "IPOnDemand") {
		return nil, false
	}
	for index, rule := range rules {
		r, _ := rule.(map[string]any)
		dest, _ := r["outboundTag"].(string)
		if !destinations[dest] {
			return nil, false
		}
		for key := range r {
			if strings.EqualFold(key, "balancerTag") || (strings.EqualFold(key, "outboundTag") && key != "outboundTag") {
				return nil, false
			}
		}
		if index < 2 {
			continue
		}
		scope, _ := r["inboundTag"].([]any)
		if len(scope) != 1 || scope[0] != "socks" || dest == healthDenyTag {
			return nil, false
		}
	}
	cfg["outbounds"] = outbounds[1:]
	return slot, true
}

func finishHotLayout(cfg map[string]any, slots []any, socket string) ([]byte, error) {
	routing := cfg["routing"].(map[string]any)
	rules := routing["rules"].([]any)
	for _, entry := range rules {
		rule := entry.(map[string]any)
		if rule["outboundTag"] == "proxy" {
			delete(rule, "outboundTag")
			rule["balancerTag"] = hotSelector
		}
	}
	cfg["outbounds"] = append(slots, cfg["outbounds"].([]any)...)
	cfg["api"] = map[string]any{"tag": hotControlTag, "listen": socket + ",0600", "services": []string{"RoutingService"}}
	routing["balancers"] = []any{map[string]any{"tag": hotSelector, "selector": []string{HotBlockedTag}}}
	routing["rules"] = append(rules, map[string]any{"type": "field", "inboundTag": []string{"socks"}, "balancerTag": hotSelector})
	return json.Marshal(cfg)
}

// Restore each stock profile and rebuild the combined image byte for byte.
// A marker or matching port alone cannot establish diagnostic authority.
func validHotProbeConfig(cfg map[string]any, endpoint netip.AddrPort) bool {
	api, _ := cfg["api"].(map[string]any)
	listen, _ := api["listen"].(string)
	socket, ok := strings.CutSuffix(listen, ",0600")
	if !ok {
		return false
	}
	original, err := json.Marshal(cfg)
	if err != nil {
		return false
	}
	outbounds, _ := cfg["outbounds"].([]any)
	routing, _ := cfg["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	if len(outbounds) < 4 || len(rules) < 3 {
		return false
	}
	count := 0
	for count < 3 {
		out, _ := outbounds[count+1].(map[string]any)
		if out["tag"] != HotSlot(count) {
			break
		}
		count++
	}
	if count < 2 || len(outbounds) <= count+1 {
		return false
	}
	delete(cfg, "api")
	delete(routing, "balancers")
	routing["rules"] = rules[:len(rules)-1]
	for _, entry := range rules[:len(rules)-1] {
		rule, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		if rule["balancerTag"] == hotSelector {
			delete(rule, "balancerTag")
			rule["outboundTag"] = "proxy"
		}
	}
	profiles := make([][]byte, count)
	for i := range profiles {
		slot := outbounds[i+1].(map[string]any)
		slot["tag"] = "proxy"
		cfg["outbounds"] = append([]any{slot}, outbounds[count+1:]...)
		profiles[i], err = json.Marshal(cfg)
		if err != nil {
			return false
		}
	}
	rebuilt, err := BuildHotLayout(profiles, endpoint, socket)
	return err == nil && bytes.Equal(original, rebuilt)
}

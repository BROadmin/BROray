package reserve

import (
	"encoding/json"
	"net/netip"

	"github.com/BROadmin/broray/internal/strictjson"
)

// DiagnosticEndpoint recognizes only the exact generated diagnostic addition.
// Configuration shape alone is not proof of runtime ownership or traffic.
func DiagnosticEndpoint(config []byte) (netip.AddrPort, bool) {
	v, err := strictjson.Decode(config)
	cfg, ok := v.(map[string]any)
	if err != nil || !ok {
		return netip.AddrPort{}, false
	}
	ins, ok := cfg["inbounds"].([]any)
	if !ok || len(ins) < 2 {
		return netip.AddrPort{}, false
	}
	in, ok := ins[len(ins)-1].(map[string]any)
	if !ok || in["tag"] != HealthInboundTag {
		return netip.AddrPort{}, false
	}
	listen, ok := in["listen"].(string)
	if !ok {
		return netip.AddrPort{}, false
	}
	addr, err := netip.ParseAddr(listen)
	if err != nil {
		return netip.AddrPort{}, false
	}
	port, ok := in["port"].(json.Number)
	if !ok {
		return netip.AddrPort{}, false
	}
	n, err := port.Int64()
	if err != nil || n < 1 || n > 65535 {
		return netip.AddrPort{}, false
	}
	endpoint := netip.AddrPortFrom(addr, uint16(n))
	if !validProbeConfig(config, endpoint) {
		return netip.AddrPort{}, false
	}
	return endpoint, true
}

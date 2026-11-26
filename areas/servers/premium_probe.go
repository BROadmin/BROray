package servers

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

func (s *svc) premiumVPNIdentity() string {
	id, err := s.policyActiveID()
	if err != nil || !ValidID(id) {
		return ""
	}
	return s.runtimeContext(id)
}

// Probe the active profile's outbound with the existing isolated Xray. The
// ordinary SOCKS listener can apply local direct and deny routing rules.
func (s *svc) premiumVPNProbe(ctx context.Context, identity string, addresses []netip.Addr, probe func(context.Context, string) bool) (bool, error) {
	id, err := s.policyActiveID()
	valid := func() bool { return identity != "" && s.premiumVPNIdentity() == identity }
	if err != nil || !ValidID(id) || !valid() || !s.reserveRuntimeFence(ctx) || probe == nil {
		return false, errReserveRuntime
	}
	record, err := ReadRecord(s.d, id)
	if err != nil {
		return false, err
	}
	if err := s.requireRuntimeSupport(ctx, record); err != nil {
		return false, err
	}
	base, err := XrayConfig(record, s.genSettings(ctx), RecordPath(s.d, id))
	if err != nil {
		return false, err
	}
	base, err = premiumRestrictedConfig(base, addresses)
	if err != nil {
		return false, err
	}
	started := time.Now()
	sample, err := s.probeIsolatedOwnedEndpoint(ctx, id, compat.SHA256Hex(base), base, started, valid, true,
		func(ctx context.Context, target reserve.ProbeTarget, endpoint netip.AddrPort) reserve.ProbeOutcome {
			out := reserve.ProbeOutcome{StartedAt: time.Now(), Verdict: "unhealthy"}
			out.Identity, _ = reserve.VerifyProbeTarget(target)
			if out.Identity != "" && probe(ctx, endpoint.String()) {
				out.Verdict = "healthy"
			}
			out.FinishedAt = time.Now()
			return out
		})
	if err != nil {
		return false, err
	}
	return sample.Verified, nil
}

func premiumRestrictedConfig(base []byte, addresses []netip.Addr) ([]byte, error) {
	const deny = "broray-premium-deny"
	if len(addresses) == 0 || len(addresses) > 4 {
		return nil, errReserveRuntime
	}
	var ips []string
	for _, address := range addresses {
		if !address.IsGlobalUnicast() || address.IsPrivate() || address.Zone() != "" || address.Is4In6() {
			return nil, errReserveRuntime
		}
		ips = append(ips, netip.PrefixFrom(address, address.BitLen()).String())
	}
	raw, err := strictjson.Decode(base)
	cfg, ok := raw.(map[string]any)
	if err != nil || !ok {
		return nil, errReserveRuntime
	}
	outbounds, ok := cfg["outbounds"].([]any)
	if !ok {
		return nil, errReserveRuntime
	}
	for _, item := range outbounds {
		outbound, ok := item.(map[string]any)
		if !ok || outbound["tag"] == deny {
			return nil, errReserveRuntime
		}
	}
	routing := map[string]any{"domainStrategy": "AsIs"}
	var rules []any
	if current, ok := cfg["routing"].(map[string]any); ok {
		routing = current
		rules, _ = routing["rules"].([]any)
	}
	inbound := []string{"broray-reserve-probe"}
	routing["rules"] = append([]any{
		map[string]any{"type": "field", "inboundTag": inbound, "network": "tcp", "port": "443", "ip": ips, "outboundTag": "proxy"},
		map[string]any{"type": "field", "inboundTag": inbound, "outboundTag": deny},
	}, rules...)
	cfg["routing"] = routing
	cfg["outbounds"] = append(outbounds, map[string]string{"tag": deny, "protocol": "blackhole"})
	return json.Marshal(cfg)
}

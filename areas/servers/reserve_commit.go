package servers

import (
	"context"
	"encoding/json"
	"net/netip"
	"strconv"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

type reserveCommitProof struct {
	Interface compat.OwnedInterfaceTarget
	Listener  reserve.ListenerProof
}

// verifyReserveCommit is the read-only composition check for a protected
// transaction. It neither applies configuration nor proves Internet health.
// Never replace the exact child snapshot with the policy identity: an automatic
// restart may retain that identity while invalidating an in-flight decision.
func (s *svc) verifyReserveCommit(ctx context.Context, binding reserveActiveBinding, description string) (reserveCommitProof, error) {
	failed := func() (reserveCommitProof, error) { return reserveCommitProof{}, errReserveRuntime }
	valid := func() bool {
		return description != "" && len(binding.identity) == 64 && binding.process != nil && s.d.App != nil &&
			s.d.App.Xray == binding.process && binding.snapshot.Running && binding.snapshot.Wanted &&
			binding.process.RuntimeSnapshot() == binding.snapshot && s.reservePlanInputs(ctx, binding.plan, false, true) &&
			binding.target.ConfigPath == s.d.Root.XrayConfig() && binding.target.ConfigSHA256 == binding.plan.configSHA &&
			binding.target.Endpoint == binding.plan.endpoint && compat.SHA256Hex(binding.plan.config) == binding.plan.configSHA &&
			s.reserveCoreMatches(binding.target.Process)
	}
	proveRuntime := func() bool {
		if !valid() {
			return false
		}
		identity, err := reserve.VerifyProbeTarget(binding.target)
		return err == nil && identity == binding.proofIdentity && valid()
	}
	if !proveRuntime() {
		return failed()
	}
	target, err := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, description)
	if err != nil || target.Name == "" || !valid() || !reservePrimaryMatches(binding.plan.config, target.Upstream) {
		return failed()
	}
	listener, err := reserve.VerifyListener(binding.target.Process, target.Upstream)
	if err != nil {
		return failed()
	}
	after, err := s.d.Svc.VerifyOwnedInterfaceTarget(ctx, description)
	if err != nil || after != target || !proveRuntime() {
		return failed()
	}
	final, err := reserve.VerifyListener(binding.target.Process, target.Upstream)
	if err != nil || final != listener || !valid() {
		return failed()
	}
	return reserveCommitProof{Interface: target, Listener: listener}, nil
}

// Match the first normal SOCKS inbound, as used by the Keenetic adapter. A
// diagnostic socket owned by the same Xray child is not the router upstream.
func reservePrimaryMatches(config []byte, endpoint netip.AddrPort) bool {
	if !endpoint.IsValid() || endpoint.Port() == 0 || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsMulticast() || endpoint.Addr().Zone() != "" {
		return false
	}
	v, err := strictjson.Decode(config)
	m, ok := v.(map[string]any)
	if err != nil || !ok {
		return false
	}
	inbounds, ok := m["inbounds"].([]any)
	if !ok {
		return false
	}
	for _, value := range inbounds {
		in, ok := value.(map[string]any)
		if !ok {
			return false
		}
		if in["protocol"] != "socks" {
			continue
		}
		if in["tag"] == "broray-reserve-health" {
			return false
		}
		port, ok := in["port"].(json.Number)
		if !ok {
			return false
		}
		n, err := strconv.ParseUint(string(port), 10, 16)
		if err != nil || n == 0 || uint16(n) != endpoint.Port() {
			return false
		}
		listen, ok := in["listen"].(string)
		if !ok {
			return false
		}
		addr, err := netip.ParseAddr(listen)
		if err != nil || addr.Zone() != "" {
			return false
		}
		return addr == endpoint.Addr() || (addr.IsUnspecified() && addr.Is4() == endpoint.Addr().Is4())
	}
	return false
}

package servers

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
)

// preparePolicyVPN leases a private check port for an ordinary running profile.
// The caller already owns an authorized Geo apply and will release the lease
// before handing the rendered bytes to the existing Xray lifecycle.
func (s *svc) preparePolicyVPN(ctx context.Context, config []byte) ([]byte, io.Closer) {
	if !compat.FenceHeld(ctx) || ctx.Err() != nil || s.d.App == nil || s.d.App.Xray == nil {
		return config, nil
	}
	if _, ok := reserve.DiagnosticEndpoint(config); ok {
		return config, nil
	}
	runtime := s.d.App.Xray.RuntimeSnapshot()
	if !runtime.Wanted || !runtime.Running || runtime.ExitConfirmed {
		return config, nil
	}
	lease, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return config, nil
	}
	rendered, err := reserve.AttachHealthInbound(config, lease.Addr().(*net.TCPAddr).AddrPort())
	if err != nil {
		_ = lease.Close()
		return config, nil
	}
	return rendered, lease
}

// probeAppliedPolicyVPN checks only the exact health-only path in the applied
// configuration. The ordinary SOCKS input can route a probe directly.
func (s *svc) probeAppliedPolicyVPN(ctx context.Context, config []byte) compat.VPNCheck {
	return s.probeAppliedPolicyVPNWith(ctx, config, reserve.Probe)
}

func (s *svc) probeAppliedPolicyVPNWith(ctx context.Context, config []byte, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) compat.VPNCheck {
	if !compat.FenceHeld(ctx) || s.d.App == nil || s.d.App.Xray == nil {
		return compat.VPNUnconfirmed
	}
	endpoint, ok := reserve.DiagnosticEndpoint(config)
	if !ok {
		return compat.VPNUnconfirmed
	}
	id, err := s.policyActiveID()
	if err != nil || id == "" {
		return compat.VPNUnconfirmed
	}
	process := s.d.App.Xray
	runtime := process.RuntimeSnapshot()
	if !runtime.Wanted || !runtime.Running || runtime.ExitConfirmed || runtime.PID <= 0 ||
		runtime.Generation == 0 || runtime.Intent == 0 {
		return compat.VPNUnconfirmed
	}
	identity, err := reserve.CaptureProcess(runtime.PID, s.d.Root.XrayBinary())
	if err != nil {
		return compat.VPNUnconfirmed
	}
	target := reserve.ProbeTarget{Process: identity, ConfigPath: s.d.Root.XrayConfig(), ConfigSHA256: compat.SHA256Hex(config), Endpoint: endpoint}
	before, err := reserve.VerifyProbeTarget(target)
	if err != nil || process.RuntimeSnapshot() != runtime {
		return compat.VPNUnconfirmed
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	started := time.Now()
	sample := probe(probeCtx, target)
	after, err := reserve.VerifyProbeTarget(target)
	selected, idErr := s.policyActiveID()
	if err != nil || idErr != nil || selected != id || before != after || sample.Identity != after ||
		probeCtx.Err() != nil || sample.StartedAt.Before(started) || sample.FinishedAt.Before(sample.StartedAt) ||
		sample.FinishedAt.After(time.Now()) ||
		s.d.App.Xray != process || process.RuntimeSnapshot() != runtime {
		return compat.VPNUnconfirmed
	}
	switch sample.Verdict {
	case "healthy":
		return compat.VPNVerified
	case "unhealthy":
		return compat.VPNFailed
	}
	return compat.VPNUnconfirmed
}

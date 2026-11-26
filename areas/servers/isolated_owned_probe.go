package servers

import (
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
	"golang.org/x/sys/unix"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"time"
)

// probeIsolatedOwned shares only isolation mechanics. Each caller supplies its
// own repeatedly checked input authority; a DPI proposal never borrows reserve
// roster membership, main-process intent, or a saved profile's configuration.
func (s *svc) probeIsolatedOwned(ctx context.Context, id, fingerprint string, base []byte, started time.Time, valid func() bool, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (sample reserve.Sample, resultErr error) {
	var run func(context.Context, reserve.ProbeTarget, netip.AddrPort) reserve.ProbeOutcome
	if probe != nil {
		run = func(ctx context.Context, target reserve.ProbeTarget, _ netip.AddrPort) reserve.ProbeOutcome {
			return probe(ctx, target)
		}
	}
	return s.probeIsolatedOwnedEndpoint(ctx, id, fingerprint, base, started, valid, false, run)
}

func (s *svc) probeIsolatedOwnedEndpoint(ctx context.Context, id, fingerprint string, base []byte, started time.Time, valid func() bool,
	useOrdinary bool, probe func(context.Context, reserve.ProbeTarget, netip.AddrPort) reserve.ProbeOutcome) (sample reserve.Sample, resultErr error) {
	unknown := func() reserve.Sample {
		return reserve.Sample{ServerID: id, Fingerprint: fingerprint, Verdict: "unknown", StartedAt: started, FinishedAt: time.Now()}
	}
	sample = unknown()
	if valid == nil || probe == nil || !ValidID(id) || fingerprint != compat.SHA256Hex(base) || !s.reserveRuntimeFence(ctx) || !valid() {
		return sample, errReserveRuntime
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	parent, err := platform.OpenDirNoFollow(s.d.Root.Tmp())
	if err != nil {
		return unknown(), errReserveRuntime
	}
	defer parent.Close()
	name, err := platform.MkdirTempAtNoFollow(parent, "reserve-probe-", 0700)
	if err != nil {
		return unknown(), errReserveProbeCleanup
	}
	held, err := platform.OpenDirAtNoFollow(parent, name)
	if err != nil {
		return unknown(), errReserveProbeCleanup
	}
	defer held.Close()
	root := platform.Root{Dir: filepath.Join(s.d.Root.Tmp(), name)}
	dirs := map[string]*os.File{root.Dir: held}
	var child *xray.Process
	var target reserve.ProbeTarget
	defer func() {
		if child != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
			err := child.Stop(stopCtx)
			stopCancel()
			if err != nil {
				sample = unknown()
				resultErr = errors.Join(resultErr, errReserveProbeCleanup)
				return
			}
		}
		if err := platform.RemoveOwnedDirTreeAtNoFollow(parent, name, held); err != nil {
			sample = unknown()
			resultErr = errors.Join(resultErr, errReserveProbeCleanup)
		}
		if sample.Verified && (!valid() || !s.reserveCoreMatches(target.Process)) {
			sample = unknown()
		}
	}()
	for _, dir := range []string{"config", "run", "logs"} {
		if err := unix.Mkdirat(int(held.Fd()), dir, 0700); err != nil {
			return unknown(), errReserveRuntime
		}
		f, err := platform.OpenDirAtNoFollow(held, dir)
		if err != nil {
			return unknown(), errReserveRuntime
		}
		defer f.Close()
		dirs[filepath.Join(root.Dir, dir)] = f
	}
	workValid := func() bool {
		if !reserveProbeDirectoryMatches(s.d.Root.Tmp(), parent) {
			return false
		}
		for path, f := range dirs {
			if !reserveProbeDirectoryMatches(path, f) {
				return false
			}
		}
		return valid()
	}
	// Bind both ports until immediately before starting the private core. A
	// later port race still cannot pass the owned socket proof.
	ordinary, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return unknown(), errReserveRuntime
	}
	defer ordinary.Close()
	diagnostic, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return unknown(), errReserveRuntime
	}
	defer diagnostic.Close()
	mainEndpoint := ordinary.Addr().(*net.TCPAddr).AddrPort()
	endpoint := diagnostic.Addr().(*net.TCPAddr).AddrPort()
	data, err := reserveIsolatedConfig(base, mainEndpoint, endpoint)
	if err != nil || !workValid() {
		return unknown(), errReserveRuntime
	}
	if err := s.prepareNativeConfig(data); err != nil {
		return unknown(), errReserveRuntime
	}
	if err := platform.WriteNewRegularAtNoFollow(dirs[root.Config()], "config.json", data, 0600); err != nil {
		return unknown(), errReserveRuntime
	}
	if s.xrayTest == nil || s.xrayTest(ctx, root.XrayConfig()) != nil || !workValid() {
		return unknown(), errReserveRuntime
	}
	child = xray.NewProbeProcess(root, s.d.Root.XrayBinary(), s.d.Root.Runtime())
	ordinary.Close()
	diagnostic.Close()
	if err := child.Start(ctx); err != nil {
		return unknown(), errReserveRuntime
	}
	var snapshot xray.RuntimeSnapshot
	var proof string
	var ordinaryProof reserve.ListenerProof
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for proof == "" {
		if !workValid() {
			return unknown(), nil
		}
		snapshot = child.RuntimeSnapshot()
		if snapshot.ExitConfirmed || !snapshot.Wanted || !snapshot.Running {
			return unknown(), nil
		}
		identity, err := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
		if err == nil {
			target = reserve.ProbeTarget{Process: identity, ConfigPath: root.XrayConfig(), ConfigSHA256: compat.SHA256Hex(data), Endpoint: endpoint}
			proof, _ = reserve.VerifyProbeTarget(target)
			if proof != "" && useOrdinary {
				ordinaryProof, err = reserve.VerifyListener(identity, mainEndpoint)
				if err != nil {
					proof = ""
				}
			}
			if child.RuntimeSnapshot() != snapshot {
				return unknown(), nil
			}
		}
		if proof != "" {
			break
		}
		select {
		case <-ctx.Done():
			return unknown(), nil
		case <-deadline.C:
			return unknown(), nil
		case <-tick.C:
		}
	}
	if !workValid() || !s.reserveCoreMatches(target.Process) {
		return unknown(), nil
	}
	out := probe(ctx, target, mainEndpoint)
	afterProof, err := reserve.VerifyProbeTarget(target)
	if useOrdinary {
		after, err := reserve.VerifyListener(target.Process, mainEndpoint)
		if err != nil || ordinaryProof != after {
			return unknown(), nil
		}
	}
	finished := time.Now()
	if err != nil || afterProof != proof || out.Identity != proof || (out.Verdict != "healthy" && out.Verdict != "unhealthy") ||
		out.StartedAt.Before(started) || out.FinishedAt.Before(out.StartedAt) || out.FinishedAt.After(finished) ||
		!workValid() || !s.reserveCoreMatches(target.Process) || child.RuntimeSnapshot() != snapshot {
		return unknown(), nil
	}
	return reserve.Sample{ServerID: id, Fingerprint: fingerprint, Identity: proof, Verdict: out.Verdict, Verified: true, StartedAt: out.StartedAt, FinishedAt: out.FinishedAt}, nil
}

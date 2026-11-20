package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

var errDPIConnectionRejected = errors.New("DPI connection rejected")
var errDPIApplyRolledBack = errors.New("DPI apply failed; previous profile restored")

func (s *svc) applyDPI(ctx context.Context, id, revision string, enabled bool) error {
	return s.applyDPIWith(ctx, id, revision, enabled, reserve.Probe)
}

// A proposal is proved before any durable write. From Begin onward, uncertainty
// retains the transaction owner; only a proved unhealthy connection may roll back.
func (s *svc) applyDPIWith(ctx context.Context, id, revision string, enabled bool, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (retErr error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if !ValidID(id) || !s.reserveRuntimeFence(ctx) || probe == nil || s.d.App == nil || s.d.App.Xray == nil || !s.d.DaemonGuard.Owns(s.d.Root.DaemonLock()) {
		return errReserveRuntime
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return errReserveTransactionPending
	}
	if err := reserve.CheckProbeWorkspaces(s.d.Root); err != nil {
		return err
	}
	image, oldRecord, err := readInactiveImage(RecordPath(s.d, id), false)
	if err != nil || image.hash != revision {
		return errReserveRuntime
	}
	value, err := strictjson.Decode(oldRecord)
	old, ok := value.(map[string]any)
	if err != nil || !ok || old["id"] != id || Validate(old, "DPI apply") != nil {
		return errReserveRuntime
	}
	wasEnabled, err := ManagedDPI(old)
	if err != nil {
		return err
	}
	if wasEnabled == enabled {
		return errReserveRuntime
	}
	next, err := prepareDPIRecord(old, enabled)
	if err != nil {
		return err
	}
	nextRecord, err := Marshal(next)
	if err != nil {
		return err
	}
	p := s.d.App.Xray
	snapshot := p.RuntimeSnapshot()
	core, err := reserveRuntimeOpen(s.d.Root.XrayBinary())
	if err != nil {
		return err
	}
	defer core.Close()
	coreInfo, err := core.Stat()
	if err != nil || coreInfo.Mode()&0111 == 0 {
		return errReserveRuntime
	}
	coreMatches := func() bool {
		f, e := reserveRuntimeOpen(s.d.Root.XrayBinary())
		if e != nil {
			return false
		}
		defer f.Close()
		info, e := f.Stat()
		return e == nil && os.SameFile(coreInfo, info) && info.Size() == coreInfo.Size() && info.Mode() == coreInfo.Mode() && info.ModTime().Equal(coreInfo.ModTime())
	}
	settings, err := s.reserveRuntimeSettings()
	if err != nil {
		return err
	}
	type input struct {
		path  string
		limit int64
		image dpiProbeImage
	}
	inputs := []input{{path: ActiveFile(s.d), limit: 4096}, {path: reservePreferencesFile(s.d), limit: 4096}, {path: settingsFile(s.d), limit: 1 << 20}, {path: s.d.Path("config", "interface.json"), limit: 64 << 10}}
	for i := range inputs {
		inputs[i].image, err = dpiReadProbeImage(inputs[i].path, inputs[i].limit)
		if err != nil {
			return err
		}
	}
	activeBytes, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	active := strings.TrimSpace(string(activeBytes)) == id
	oldConfig, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	wantRecord, wantConfig := oldRecord, oldConfig
	same := func(path string, want []byte, limit int64) bool {
		got, e := reserveRuntimeFile(path, limit)
		if want == nil {
			return errors.Is(e, os.ErrNotExist)
		}
		return e == nil && bytes.Equal(got, want)
	}
	var cold *reserve.StoppedProfileLease
	// This closure is also called under the supervisor mutex: never call Process here.
	fixed := func() bool {
		st, e := s.reserveRuntimeSettings()
		if e != nil || st != settings || !s.reserveRuntimeFence(ctx) || s.d.App.Xray != p || !s.d.DaemonGuard.Owns(s.d.Root.DaemonLock()) || !coreMatches() {
			return false
		}
		if cold != nil && cold.Check(ctx, s.d.Root) != nil {
			return false
		}
		for _, in := range inputs {
			got, e := dpiReadProbeImage(in.path, in.limit)
			if e != nil || got != in.image {
				return false
			}
		}
		return same(RecordPath(s.d, id), wantRecord, 1<<20) && same(s.d.Root.XrayConfig(), wantConfig, 2<<20)
	}
	current := func() bool { return fixed() && p.RuntimeSnapshot() == snapshot }
	confirmedExit := func() bool {
		after := p.RuntimeSnapshot()
		exited := after.ConfirmsUnexpectedExit(snapshot) || (after == snapshot && snapshot.Wanted && !snapshot.Running && snapshot.ExitConfirmed && snapshot.LastPID > 0)
		return exited && fixed() && p.RuntimeSnapshot() == after
	}
	if !current() {
		return errReserveRuntime
	}
	sample, err := s.probeDPIProposalWith(ctx, RecordPath(s.d, id), revision, next, probe)
	if err != nil {
		return err
	}
	if !current() || !image.unchanged() || !sample.Verified {
		return errReserveRuntime
	}
	if sample.Verdict != "healthy" {
		return errDPIConnectionRejected
	}
	nextConfig := oldConfig
	var target compat.OwnedInterfaceTarget
	description := ""
	var diagnostic netip.AddrPort
	var port net.Listener
	defer func() {
		if port != nil {
			_ = port.Close()
		}
	}()
	if active {
		if snapshot.ColdStopped() {
			cold, err = reserve.OpenStoppedProfile(ctx, s.d.Root, s.d.DaemonGuard)
			if err != nil {
				return err
			}
			defer cold.Close()
		} else if snapshot.Generation == 0 || snapshot.Intent == 0 || (!snapshot.Running && (!snapshot.ExitConfirmed || snapshot.LastPID <= 0)) {
			return errReserveRuntime
		}
		description, err = s.reserveInterfaceDescription()
		if err != nil {
			return err
		}
		if description == "" {
			description, err = s.d.Svc.OrdinaryInterfaceDescription()
		}
		if err != nil || description == "" {
			return errReserveRuntime
		}
		target, err = s.d.Svc.VerifyOwnedInterfaceConfiguration(ctx, description)
		if err != nil || target.Name == "" || !reservePrimaryMatches(oldConfig, target.Upstream) || !current() {
			return errReserveRuntime
		}
		if snapshot.Running {
			identity, e := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
			if e != nil {
				return e
			}
			if _, e = reserve.VerifyListener(identity, target.Upstream); e != nil || !current() {
				return errReserveRuntime
			}
		}
		previousOutbound, e := Outbound(normalize(old))
		if e != nil || !dpiRecoveredProxyMatches(oldConfig, previousOutbound) {
			return errReserveRuntime
		}
		outbound, e := Outbound(normalize(next))
		if e != nil {
			return e
		}
		v, e := strictjson.Decode(oldConfig)
		cfg, ok := v.(map[string]any)
		if e != nil || !ok {
			return errReserveRuntime
		}
		outs, ok := cfg["outbounds"].([]any)
		if !ok || len(outs) == 0 {
			return errReserveRuntime
		}
		outs[0] = outbound
		nextConfig, err = json.Marshal(cfg)
		if err != nil {
			return err
		}
		if snapshot.Wanted {
			var present bool
			diagnostic, present = reserve.DiagnosticEndpoint(oldConfig)
			if !present {
				prefs, e := readReservePreferences(s.d)
				if e != nil || prefs.Enabled {
					return errReserveRuntime
				}
				port, err = net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					return err
				}
				diagnostic, err = netip.ParseAddrPort(port.Addr().String())
				if err != nil {
					return err
				}
				nextConfig, err = reserve.AttachHealthInbound(nextConfig, diagnostic)
				if err != nil {
					return err
				}
			}
		}
	}
	if !current() {
		return errReserveRuntime
	}
	if active {
		if err := s.testDPIRecoveryConfigs(ctx, [][]byte{nextConfig}, current); err != nil {
			return err
		}
	}
	j, err := reserve.OpenProfileJournal(s.d.Root)
	if err != nil {
		return err
	}
	defer j.Close()
	owned, settled := false, false
	// Check pending even on a partially failed Begin; never lose an uncertain writer.
	defer func() {
		if retErr != nil {
			c, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer stop()
			pending, e := reserve.TransactionPending(c, s.d.Root)
			if e != nil || pending || (owned && !settled) {
				retErr = errors.Join(errReserveTransactionPending, retErr, e)
			}
		}
	}()
	r, err := j.Begin(ctx, id, revision, nextRecord, nextConfig, active && snapshot.Wanted, compat.SHA256Hex([]byte(id+revision+settings+compat.SHA256Hex(nextConfig))))
	if err != nil {
		return err
	}
	owned = true
	if !bytes.Equal(r.Previous.Record.Data, oldRecord) || r.Previous.Config.Exists != (oldConfig != nil) || !bytes.Equal(r.Previous.Config.Data, oldConfig) || !current() {
		return errReserveRuntime
	}
	install := func() error {
		if !fixed() {
			return errReserveRuntime
		}
		if e := j.Install(ctx, r); e != nil {
			return e
		}
		wantRecord, wantConfig = nextRecord, nextConfig
		if port != nil {
			e := port.Close()
			port = nil
			if e != nil {
				return e
			}
		}
		if !fixed() {
			return errReserveRuntime
		}
		return nil
	}
	wanted := active && snapshot.Wanted
	if !active {
		err = install()
	} else if cold != nil {
		err = p.InstallIfCold(ctx, snapshot, cold, install)
	} else if wanted {
		snapshot, err = p.ReplaceIfCurrent(ctx, snapshot, install)
	} else {
		snapshot, err = p.StopIfCurrent(ctx, snapshot, install)
	}
	if err != nil {
		return err
	}
	if !current() && !(wanted && confirmedExit()) {
		return errReserveRuntime
	}
	// Verify exact restored/accepted native configuration and owned primary binding.
	verify := func(c context.Context, decision reserve.ProfileTransactionRecord) error {
		if !current() {
			return errReserveRuntime
		}
		if e := s.verifyRecoveredDPIProfile(c, decision, current); e != nil {
			return e
		}
		if !active {
			return nil
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if !current() {
				return errReserveRuntime
			}
			after, e := s.d.Svc.VerifyOwnedInterfaceConfiguration(c, description)
			if e != nil || after != target {
				return errReserveRuntime
			}
			if !wanted {
				if !current() {
					return errReserveRuntime
				}
				return nil
			}
			identity, e := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
			if e == nil {
				_, e = reserve.VerifyListener(identity, target.Upstream)
				if e == nil {
					last, le := s.d.Svc.VerifyOwnedInterfaceTarget(c, description)
					if le == nil && last == target && current() {
						return nil
					}
				}
			}
			if time.Now().After(deadline) {
				return errReserveRuntime
			}
			select {
			case <-c.Done():
				return c.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if wanted {
		verdict := s.probeDPILive(ctx, snapshot.PID, diagnostic, current, confirmedExit, probe)
		if verdict == "unknown" {
			return errReserveRuntime
		}
		if verdict == "unhealthy" {
			restore := func() error {
				if !fixed() {
					return errReserveRuntime
				}
				if e := j.Restore(ctx, r); e != nil {
					return e
				}
				wantRecord, wantConfig = oldRecord, oldConfig
				return nil
			}
			snapshot, err = p.ReplaceIfCurrent(ctx, snapshot, restore)
			if err != nil {
				return err
			}
			if err = j.RollbackPrepared(ctx, r, verify); err != nil {
				return err
			}
			settled = true
			return errDPIApplyRolledBack
		}
	}
	if active && !wanted {
		if cold != nil {
			return j.ConfirmStoppedLeased(ctx, r, cold, verify)
		}
		return j.ConfirmStopped(ctx, r, s.d.DaemonGuard, verify)
	}
	return j.Confirm(ctx, r, func(c context.Context, decision reserve.ProfileTransactionRecord) error {
		decision.Phase = "committed"
		return verify(c, decision)
	})
}

func (s *svc) probeDPILive(ctx context.Context, pid int, endpoint netip.AddrPort, valid, confirmedExit func() bool, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) string {
	started := time.Now()
	deadline := started.Add(10 * time.Second)
	for {
		if confirmedExit() {
			return "unhealthy"
		}
		if !valid() {
			return "unknown"
		}
		identity, e := reserve.CaptureProcess(pid, s.d.Root.XrayBinary())
		config, ce := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
		if e == nil && ce == nil {
			target := reserve.ProbeTarget{Process: identity, ConfigPath: s.d.Root.XrayConfig(), ConfigSHA256: compat.SHA256Hex(config), Endpoint: endpoint}
			proof, e := reserve.VerifyProbeTarget(target)
			if e == nil && valid() {
				out := probe(ctx, target)
				if confirmedExit() {
					return "unhealthy"
				}
				last, le := reserve.VerifyProbeTarget(target)
				if le != nil || last != proof || out.Identity != proof || out.StartedAt.Before(started) || out.FinishedAt.Before(out.StartedAt) || out.FinishedAt.After(time.Now()) || !valid() {
					return "unknown"
				}
				if out.Verdict == "healthy" || out.Verdict == "unhealthy" {
					return out.Verdict
				}
				return "unknown"
			}
		}
		if time.Now().After(deadline) {
			return "unknown"
		}
		select {
		case <-ctx.Done():
			return "unknown"
		case <-time.After(100 * time.Millisecond):
		}
	}
}

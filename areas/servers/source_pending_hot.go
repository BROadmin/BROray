package servers

import (
	"bytes"
	"context"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type pendingSourceHotPlan struct {
	nextConfig []byte
	slot       string
	current    func() bool
}

func (s *svc) preparePendingSourceHot(ctx context.Context, sourceID, id string, prefs reservePreferencesSnapshot,
	config, previousRecord []byte, control string) (*pendingSourceHotPlan, error) {
	reject := compat.ErrXrayCandidateRejected
	endpoint, ok := reserve.DiagnosticEndpoint(config)
	if !ok || control != xray.ReserveControlSocket(s.d.Root) || !prefs.Enabled || prefs.Emergency != nil || !reserveRosterContains(prefs, id) {
		return nil, reject
	}
	record, err := s.previousPendingRecord(sourceID, id, previousRecord)
	if err != nil {
		return nil, err
	}
	roster := append([]string{prefs.PrimaryServerID}, prefs.BackupServerIDs...)
	plans := make(map[string]reserveRuntimePlan, len(roster))
	for _, member := range roster {
		plan, err := s.prepareReserveStockRuntime(ctx, member, endpoint, false)
		if err != nil {
			return nil, reject
		}
		plans[member] = plan
	}
	selected := plans[id]
	next, err := s.buildReserveHotPlan(selected, roster, plans)
	if err != nil {
		return nil, reject
	}
	base, err := XrayConfig(record, s.genSettings(ctx), RecordPath(s.d, id))
	if err != nil {
		return nil, reject
	}
	routed, err := s.d.Svc.ConfigureRouting(ctx, base)
	if err != nil {
		return nil, err
	}
	previousConfig, err := reserve.AttachHealthInbound(routed, endpoint)
	if err != nil {
		return nil, reject
	}
	old := selected
	old.config = previousConfig
	old.node.Fingerprint = compat.SHA256Hex(base)
	plans[id] = old
	rebuilt, err := s.buildReserveHotPlan(old, roster, plans)
	plans[id] = selected
	if err != nil || rebuilt.hotSlot != next.hotSlot || !bytes.Equal(config, rebuilt.config) {
		return nil, reject
	}
	current := func() bool {
		settings, err := s.reserveRuntimeSettings()
		if err != nil || settings != selected.settingsSHA {
			return false
		}
		for member, plan := range plans {
			raw, err := reserveRuntimeFile(RecordPath(s.d, member), 1<<20)
			if err != nil || compat.SHA256Hex(raw) != plan.recordSHA {
				return false
			}
		}
		return true
	}
	if !current() {
		return nil, reject
	}
	return &pendingSourceHotPlan{nextConfig: next.config, slot: next.hotSlot, current: current}, nil
}

func (s *svc) selectPendingSourceHot(ctx context.Context, p *xray.Process, snapshot xray.RuntimeSnapshot,
	config []byte, plan *pendingSourceHotPlan, current func() bool) bool {
	endpoint, ok := reserve.DiagnosticEndpoint(config)
	if !ok || !waitPendingSourceReady(ctx, current, func() bool {
		identity, err := reserve.CaptureProcess(snapshot.PID, s.d.Root.XrayBinary())
		if err != nil {
			return false
		}
		_, err = reserve.VerifyProbeTarget(reserve.ProbeTarget{Process: identity, ConfigPath: s.d.Root.XrayConfig(),
			ConfigSHA256: compat.SHA256Hex(config), Endpoint: endpoint})
		return err == nil
	}) {
		return false
	}
	return p.SelectReserveOutboundIfCurrent(ctx, snapshot, plan.slot) == nil && current()
}

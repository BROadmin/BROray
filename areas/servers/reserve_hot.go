package servers

import (
	"bytes"
	"context"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

func (s *svc) buildReserveHotPlan(selected reserveRuntimePlan, roster []string, plans map[string]reserveRuntimePlan) (reserveRuntimePlan, error) {
	profiles := make([][]byte, 0, len(roster))
	slot := ""
	for i, id := range roster {
		plan := plans[id]
		if plan.owner != s || plan.revision != selected.revision || plan.endpoint != selected.endpoint ||
			plan.hotSlot != "" || plan.outsideRoster {
			return reserveRuntimePlan{}, errReserveRuntime
		}
		if id == selected.node.ID {
			slot = reserve.HotSlot(i)
		}
		profiles = append(profiles, plan.config)
	}
	if slot == "" {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	config, err := reserve.BuildHotLayout(profiles, selected.endpoint, xray.ReserveControlSocket(s.d.Root))
	if err != nil {
		return reserveRuntimePlan{}, err
	}
	selected.config = config
	selected.configSHA = compat.SHA256Hex(config)
	selected.hotSlot = slot
	return selected, nil
}

func (s *svc) initialReserveHotPlan(selected reserveRuntimePlan, roster []string, plans map[string]reserveRuntimePlan) reserveRuntimePlan {
	if !s.reserveHotExperiment {
		return selected
	}
	plan, err := s.buildReserveHotPlan(selected, roster, plans)
	if err != nil {
		return selected
	}
	return plan
}

func (s *svc) currentReserveHotPlan(ctx context.Context, plan reserveRuntimePlan, config []byte) (reserveRuntimePlan, error) {
	path, hot := reserve.HotLayoutSocket(config)
	if !hot {
		return plan, nil
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || !prefs.Enabled || path != xray.ReserveControlSocket(s.d.Root) || prefs.Revision != plan.revision {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	roster := append([]string{prefs.PrimaryServerID}, prefs.BackupServerIDs...)
	plans := map[string]reserveRuntimePlan{plan.node.ID: plan}
	for _, id := range roster {
		if id == plan.node.ID {
			continue
		}
		next, err := s.prepareReserveStockRuntime(ctx, id, plan.endpoint, false)
		if err != nil {
			return reserveRuntimePlan{}, err
		}
		plans[id] = next
	}
	rebuilt, err := s.buildReserveHotPlan(plan, roster, plans)
	if err != nil || !bytes.Equal(rebuilt.config, config) {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	return rebuilt, nil
}

func (s *svc) ordinaryReserveRuntime(ctx context.Context, plan reserveRuntimePlan, record Record) (reserveRuntimePlan, error) {
	id := plan.node.ID
	base, err := XrayConfig(record, s.genSettings(ctx), RecordPath(s.d, id))
	if err != nil || compat.SHA256Hex(base) != plan.node.Fingerprint {
		return reserveRuntimePlan{}, errReserveRuntime
	}
	plan.config, err = s.d.Svc.ConfigureRouting(ctx, base)
	if err != nil {
		return reserveRuntimePlan{}, err
	}
	plan.configSHA = compat.SHA256Hex(plan.config)
	plan.hotSlot = ""
	return plan, nil
}

func (s *svc) confirmReserveHotTraffic(ctx context.Context, binding reserveActiveBinding) bool {
	if binding.plan.hotSlot == "" {
		return true
	}
	sample := s.probeReserveRuntimeWith(ctx, binding, s.reserveHotProbe)
	if verifiedReserveSelection(sample, binding.plan) && sample.Identity == binding.identity {
		return true
	}
	// A negative proof cannot commit a new selection. Stop sending VPN traffic
	// to that slot before the caller tries restoring the original selection.
	if s.d.App != nil && s.d.App.Xray == binding.process && s.reservePlanCurrent(binding.plan, true, true) {
		block, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := binding.process.SelectReserveOutboundIfCurrent(block, binding.snapshot, reserve.HotBlockedTag); err != nil {
			s.logf("RESERVE_HOT_BLOCK=UNCONFIRMED")
		}
	}
	return false
}

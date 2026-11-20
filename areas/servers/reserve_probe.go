package servers

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

var errReserveProbeCleanup = errors.New("RESERVE_PROBE_CLEANUP_UNCONFIRMED")

func (s *svc) probeReserveBackup(ctx context.Context, id string) (reserve.Sample, error) {
	return s.probeReserveBackupWith(ctx, id, reserve.Probe)
}

func (s *svc) probeReserveBackupWith(ctx context.Context, id string, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (sample reserve.Sample, resultErr error) {
	return s.probeReserveCandidate(ctx, id, false, probe)
}

func (s *svc) probeReserveCandidate(ctx context.Context, id string, outsideRoster bool, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (sample reserve.Sample, resultErr error) {
	started := time.Now()
	plan := reserveRuntimePlan{node: reserve.Node{ID: id}}
	unknown := func() reserve.Sample {
		return reserve.Sample{ServerID: id, Fingerprint: plan.node.Fingerprint, Verdict: "unknown", StartedAt: started, FinishedAt: time.Now()}
	}
	sample = unknown()
	if !s.reserveRuntimeFence(ctx) || !ValidID(id) || probe == nil || s.d.App == nil || s.d.App.Xray == nil {
		return sample, errReserveRuntime
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	main := s.d.App.Xray
	mainSnapshot := main.RuntimeSnapshot()
	mainConfig, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
	if err != nil {
		return sample, errReserveRuntime
	}
	active, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
	if err != nil || strings.TrimSpace(string(active)) == id || !mainSnapshot.Wanted || mainSnapshot.Intent == 0 {
		return sample, errReserveRuntime
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || !prefs.Configured || outsideRoster == reserveRosterContains(prefs, id) {
		return sample, errReserveRuntime
	}
	record, err := reserveRuntimeFile(RecordPath(s.d, id), 1<<20)
	if err != nil {
		return sample, errReserveRuntime
	}
	v, err := strictjson.Decode(record)
	m, ok := v.(map[string]any)
	if err != nil || !ok || m["id"] != id {
		return sample, errReserveRuntime
	}
	settings, err := s.reserveRuntimeSettings()
	if err != nil {
		return sample, errReserveRuntime
	}
	base, err := XrayConfig(Record(m), s.genSettings(ctx), RecordPath(s.d, id))
	if err != nil {
		return sample, errReserveRuntime
	}
	plan = reserveRuntimePlan{owner: s, node: reserve.Node{ID: id, Fingerprint: compat.SHA256Hex(base)}, revision: prefs.Revision, recordSHA: compat.SHA256Hex(record), settingsSHA: settings, outsideRoster: outsideRoster}
	valid := func() bool {
		if !s.reservePlanInputs(ctx, plan, false, false) || s.d.App.Xray != main || main.RuntimeSnapshot() != mainSnapshot {
			return false
		}
		config, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
		if err != nil || compat.SHA256Hex(config) != compat.SHA256Hex(mainConfig) {
			return false
		}
		selection, err := reserveRuntimeFile(ActiveFile(s.d), 4096)
		return err == nil && string(selection) == string(active)
	}
	if !valid() {
		return unknown(), errReserveRuntime
	}
	return s.probeIsolatedOwned(ctx, id, plan.node.Fingerprint, base, started, valid, probe)
}

func reserveProbeDirectoryMatches(path string, held *os.File) bool {
	visible, err := platform.OpenDirNoFollow(path)
	if err != nil {
		return false
	}
	defer visible.Close()
	a, ea := held.Stat()
	b, eb := visible.Stat()
	return ea == nil && eb == nil && os.SameFile(a, b)
}

func reserveIsolatedConfig(base []byte, ordinary, diagnostic netip.AddrPort) ([]byte, error) {
	v, err := strictjson.Decode(base)
	cfg, ok := v.(map[string]any)
	if err != nil || !ok {
		return nil, errReserveRuntime
	}
	cfg["log"] = privateCoreLogSettings()
	cfg["inbounds"] = []any{map[string]any{"tag": "broray-reserve-probe", "listen": ordinary.Addr().String(), "port": ordinary.Port(), "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, errReserveRuntime
	}
	return reserve.AttachHealthInbound(raw, diagnostic)
}

package servers

import (
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
	"os"
	"path/filepath"
	"time"
)

func (s *svc) probeDPIProposal(ctx context.Context, path, expectedSHA string, candidate Record) (reserve.Sample, error) {
	return s.probeDPIProposalWith(ctx, path, expectedSHA, candidate, reserve.Probe)
}

// Only exact existing catalog members are readable; a caller cannot turn a
// proposal into a probe of another source or an arbitrary filesystem path.
func (s *svc) dpiProposalPath(path string, rec Record) bool {
	id, ok := rec["id"].(string)
	if !ok || !ValidID(id) {
		return false
	}
	if path == RecordPath(s.d, id) {
		return true
	}
	source, ok := rec["source"].(map[string]any)
	if !ok || source["type"] != "subscription" {
		return false
	}
	subID, ok := source["subscriptionId"].(string)
	if !ok || !ValidID(subID) {
		return false
	}
	return path == filepath.Join(s.d.Path("config", "disabled-subscription-servers"), subID, id+".json")
}

type dpiProbeImage struct {
	present bool
	hash    string
}

func dpiReadProbeImage(path string, limit int64) (dpiProbeImage, error) {
	raw, err := reserveRuntimeFile(path, limit)
	if errors.Is(err, os.ErrNotExist) {
		return dpiProbeImage{}, nil
	}
	if err != nil {
		return dpiProbeImage{}, err
	}
	return dpiProbeImage{true, compat.SHA256Hex(raw)}, nil
}

// Validates a detached proposal without saving it or changing the main process.
// A healthy result proves this exact generated connection while the old inputs
// remain unchanged. The transaction owner must still bind proposal bytes to its
// eventual commit and retain ownership if cleanup is unconfirmed.
func (s *svc) probeDPIProposalWith(ctx context.Context, path, expectedSHA string, candidate Record, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (reserve.Sample, error) {
	started := time.Now()
	unknown := reserve.Sample{Verdict: "unknown", StartedAt: started, FinishedAt: started}
	if !s.reserveRuntimeFence(ctx) || probe == nil || s.d.App == nil || s.d.App.Xray == nil || !s.dpiProposalPath(path, candidate) {
		return unknown, errReserveRuntime
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return unknown, errReserveTransactionPending
	}
	if err := reserve.CheckProbeWorkspaces(s.d.Root); err != nil {
		return unknown, err
	}
	raw, err := Marshal(candidate)
	if err != nil || len(raw) > 1<<20 {
		return unknown, errReserveRuntime
	}
	value, err := strictjson.Decode(raw)
	next, ok := value.(map[string]any)
	if err != nil || !ok {
		return unknown, errReserveRuntime
	}
	enabled, err := ManagedDPI(next)
	if err != nil {
		return unknown, err
	}
	image, previous, err := readInactiveImage(path, false)
	if err != nil || image.hash != expectedSHA {
		return unknown, errReserveRuntime
	}
	value, err = strictjson.Decode(previous)
	old, ok := value.(map[string]any)
	if err != nil || !ok || old["id"] != next["id"] || !s.dpiProposalPath(path, old) {
		return unknown, errReserveRuntime
	}
	previousEnabled, err := ManagedDPI(old)
	if err != nil || (!enabled && !previousEnabled) {
		return unknown, errReserveRuntime
	}
	oldSource, oldOK := old["source"].(map[string]any)
	newSource, newOK := next["source"].(map[string]any)
	if !oldOK || !newOK || oldSource["type"] != newSource["type"] || oldSource["subscriptionId"] != newSource["subscriptionId"] {
		return unknown, errReserveRuntime
	}
	core, err := reserveRuntimeOpen(s.d.Root.XrayBinary())
	if err != nil {
		return unknown, errReserveRuntime
	}
	defer core.Close()
	coreInfo, err := core.Stat()
	if err != nil || coreInfo.Mode()&0111 == 0 {
		return unknown, errReserveRuntime
	}
	coreUnchanged := func() bool {
		visible, err := reserveRuntimeOpen(s.d.Root.XrayBinary())
		if err != nil {
			return false
		}
		defer visible.Close()
		info, err := visible.Stat()
		return err == nil && os.SameFile(coreInfo, info) && coreInfo.Size() == info.Size() && coreInfo.ModTime().Equal(info.ModTime()) && coreInfo.Mode() == info.Mode()
	}
	main := s.d.App.Xray
	snapshot := main.RuntimeSnapshot()
	config, err := dpiReadProbeImage(s.d.Root.XrayConfig(), 2<<20)
	if err != nil {
		return unknown, errReserveRuntime
	}
	active, err := dpiReadProbeImage(ActiveFile(s.d), 4096)
	if err != nil {
		return unknown, errReserveRuntime
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return unknown, errReserveRuntime
	}
	settings, err := s.reserveRuntimeSettings()
	if err != nil {
		return unknown, errReserveRuntime
	}
	valid := func() bool {
		if !s.reserveRuntimeFence(ctx) || s.d.App == nil || s.d.App.Xray != main || main.RuntimeSnapshot() != snapshot || !image.unchanged() || !coreUnchanged() {
			return false
		}
		if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
			return false
		}
		p, err := readReservePreferences(s.d)
		if err != nil || p.Revision != prefs.Revision {
			return false
		}
		st, err := s.reserveRuntimeSettings()
		if err != nil || st != settings {
			return false
		}
		cfg, err := dpiReadProbeImage(s.d.Root.XrayConfig(), 2<<20)
		if err != nil || cfg != config {
			return false
		}
		sel, err := dpiReadProbeImage(ActiveFile(s.d), 4096)
		return err == nil && sel == active
	}
	if !valid() {
		return unknown, errReserveRuntime
	}
	if err := s.requireRuntimeSupport(ctx, next); err != nil {
		return unknown, err
	}
	base, err := XrayConfig(next, s.genSettings(ctx), path)
	if err != nil || !valid() {
		return unknown, errReserveRuntime
	}
	return s.probeIsolatedOwned(ctx, asString(next["id"]), compat.SHA256Hex(base), base, started, valid, probe)
}

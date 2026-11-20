package servers

import (
	"context"
	"time"

	"github.com/BROadmin/broray/modules/reserve"
)

func (s *svc) reserveAutoChoice(ctx context.Context, m *reserveMonitor, prefs reservePreferencesSnapshot, active string, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (reserve.Sample, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cfg, ok := s.readAutoConfig()
	if !ok {
		return reserve.Sample{}, "", errReserveRuntime
	}
	rule := asString(cfg.raw["selectionRule"])
	if !in(rule, "best-quality", "lowest-ping", "preferred") {
		rule = "best-quality"
	}
	preferred := asString(cfg.raw["preferredServerId"])
	minimum := rankOf(alt(cfg.raw["minimumRating"], "acceptable"))
	if minimum == 0 {
		minimum = 2
	}
	snapshot, err := s.qualitySnapshot("all")
	if err != nil {
		return reserve.Sample{}, "", err
	}
	metric := func(v any) float64 {
		if n, ok := isInt(v); ok && n >= 0 {
			return n
		}
		return 999999
	}
	stale, expired := s.qualityPolicy()
	var candidates []map[string]any
	for _, entry := range snapshot {
		if entry.ID == active || reserveRosterContains(prefs, entry.ID) {
			continue
		}
		q := s.qualityView(entry.ID, stale, expired)
		if q["serverFingerprint"] != entry.SHA256 || q["freshness"] == "expired" {
			q = nil
		}
		candidates = append(candidates, map[string]any{"id": entry.ID, "sha256": entry.SHA256,
			"ratingRank": rankOf(q["rating"]), "ping": metric(q["ping"]), "jitter": metric(q["jitter"])})
	}
	if len(candidates) == 0 {
		return reserve.Sample{}, "", nil
	}
	// One candidate per monitor operation. Rotate after unsuccessful attempts
	// so a preferred but unavailable profile cannot monopolize the queue.
	index := m.autoChoiceCursor % len(candidates)
	m.autoChoiceCursor = (index + 1) % len(candidates)
	selected := rankedCandidates(candidates, rule, preferred)[index]
	id := asString(selected["id"])
	result, err := s.checkServer(ctx, id, "auto-switch", asString(selected["sha256"]))
	if err != nil {
		return reserve.Sample{}, "", err
	}
	quality, _ := result.Object["quality"].(map[string]any)
	if !result.Success || rankOf(quality["rating"]) < minimum {
		return reserve.Sample{}, "", nil
	}
	if current, err := fileSHA256(autoSwitchFile(s.d)); err != nil || current != cfg.hash {
		return reserve.Sample{}, "", errReserveRuntime
	}
	sample, err := s.probeReserveCandidate(ctx, id, true, probe)
	if err == nil {
		if h, err := fileSHA256(RecordPath(s.d, id)); err != nil || h != selected["sha256"] {
			return reserve.Sample{}, "", errReserveRuntime
		}
	}
	return sample, cfg.hash, err
}

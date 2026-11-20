package servers

import (
	"context"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
	"time"
)

var errSourceCandidateRejected = errors.Join(errors.New("SOURCE_CANDIDATE_REJECTED"), compat.ErrXrayCandidateRejected)

// selectRemovedSource consumes an ordered, complete list of eligible IDs from
// the caller's strict catalog snapshot. The caller owns the protected fence,
// preserves that snapshot on failure and performs catalog removal only after
// success. This helper never removes catalog records or changes reserve policy.
func (s *svc) selectRemovedSource(ctx context.Context, sourceID, activeID string, candidates []string) (string, error) {
	return s.selectSourceCandidates(ctx, sourceID, activeID, candidates, false)
}

func (s *svc) selectSourceCandidates(ctx context.Context, sourceID, activeID string, candidates []string, refreshed bool) (string, error) {
	if !s.reserveRuntimeFence(ctx) {
		return "", errReserveRuntime
	}
	seen := make(map[string]bool, len(candidates))
	for _, id := range candidates {
		if !ValidID(id) || id == activeID || seen[id] {
			return "", errReserveRuntime
		}
		seen[id] = true
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, id := range candidates {
		err := s.replaceSourceCandidate(ctx, sourceID, activeID, id, refreshed)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, errSourceCandidateRejected) {
			return "", err
		}
	}
	if refreshed {
		return "", errSourceCandidateRejected
	}
	if err := s.withdrawRemovedSource(ctx, sourceID, activeID); err != nil {
		return "", err
	}
	return "", nil
}

package reserve

import (
	"crypto/sha256"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// ProbeScheduler serializes probe attempts; it does not own a process or apply a switch.
type ProbeScheduler struct {
	mu       sync.Mutex
	sequence uint64
	active   *ProbeLease
	boundary [32]byte
	attempts map[string]time.Time
	finished time.Time
}

type ProbeLease struct {
	Node      Node
	StartedAt time.Time
	owner     *ProbeScheduler
	sequence  uint64
	boundary  [32]byte
}

// Due uses an isolated scheduler snapshot, so querying status never consumes a
// lease, resets the real boundary, or changes the last-attempt timestamps.
func (q *ProbeScheduler) Due(state State, now time.Time, policy Policy) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.active != nil {
		return false
	}
	copy := ProbeScheduler{sequence: q.sequence, boundary: q.boundary, finished: q.finished, attempts: map[string]time.Time{}}
	for id, at := range q.attempts {
		copy.attempts[id] = at
	}
	_, ok := copy.Begin(state, now, policy)
	return ok
}

func (q *ProbeScheduler) Begin(state State, now time.Time, policy Policy) (ProbeLease, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.active != nil || q.sequence == ^uint64(0) || now.IsZero() || now.Before(q.finished) ||
		!validPolicy(policy) || !validSettings(state.Settings) || !state.Settings.Enabled || state.Pending != nil ||
		state.ActiveIdentity == "" || now.Before(state.EvidenceSince) || now.Before(state.LastSwitchAt) {
		return ProbeLease{}, false
	}
	active, ok := lookupActive(state, state.ActiveID)
	if !ok {
		return ProbeLease{}, false
	}
	boundary, ok := probeBoundary(state, policy)
	if !ok {
		return ProbeLease{}, false
	}
	if boundary != q.boundary {
		q.boundary = boundary
		q.attempts = map[string]time.Time{}
	}
	lastAttempt := func(id string) time.Time {
		last := q.attempts[id]
		if observed := state.Samples[id].StartedAt; observed.After(last) {
			last = observed
		}
		return last
	}
	nodes := []Node{active}
	for _, n := range append([]Node{state.Settings.Primary}, state.Settings.Backups...) {
		if n.ID != active.ID {
			nodes = append(nodes, n)
		}
	}
	// A slow first backup must not repeatedly displace the second one.
	sort.SliceStable(nodes[1:], func(i, j int) bool {
		return lastAttempt(nodes[i+1].ID).Before(lastAttempt(nodes[j+1].ID))
	})
	for _, n := range nodes {
		interval := policy.RecoveryInterval
		if n.ID == state.ActiveID {
			interval = policy.ActiveInterval
		}
		last := lastAttempt(n.ID)
		if !last.IsZero() && now.Sub(last) < interval {
			continue
		}
		q.sequence++
		lease := ProbeLease{Node: n, StartedAt: now, owner: q, sequence: q.sequence, boundary: boundary}
		q.active = &lease
		q.attempts[n.ID] = now
		return lease, true
	}
	return ProbeLease{}, false
}

// Complete may release a lease only after the adapter confirms owned cleanup.
func (q *ProbeScheduler) Complete(lease ProbeLease, state State, sample Sample, now time.Time, policy Policy, cleanupConfirmed bool) (State, []Intent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.active == nil || lease != *q.active || lease.owner != q || !cleanupConfirmed {
		return clone(state), nil
	}
	// A confirmed completion releases the slot even if its sample became stale.
	// No clock advance, disable or configuration change can release it instead.
	q.active = nil
	if now.After(q.finished) {
		q.finished = now
	}
	boundary, ok := probeBoundary(state, policy)
	if !ok || boundary != lease.boundary || !state.Settings.Enabled || state.Pending != nil ||
		now.Before(lease.StartedAt) || sample.ServerID != lease.Node.ID || sample.Fingerprint != lease.Node.Fingerprint ||
		sample.StartedAt.Before(lease.StartedAt) || sample.FinishedAt.After(now) {
		return clone(state), nil
	}
	return Advance(state, Event{Kind: "sample", Sample: sample}, now, policy)
}

// This boundary is policy identity only. The runtime adapter must separately
// retain/revalidate its exact child generation, listener and lifecycle fence.
func probeBoundary(state State, policy Policy) ([32]byte, bool) {
	data, err := json.Marshal(struct {
		Settings                    Settings
		Emergency                   Node
		ActiveID, ActiveIdentity    string
		EvidenceSince, LastSwitchAt time.Time
		Policy                      Policy
	}{state.Settings, state.Emergency, state.ActiveID, state.ActiveIdentity, state.EvidenceSince, state.LastSwitchAt, policy})
	return sha256.Sum256(data), err == nil
}

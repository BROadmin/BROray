package reserve

import (
	"encoding/hex"
	"time"
)

func clone(s State) State {
	s.Settings.Backups = append([]Node(nil), s.Settings.Backups...)
	samples := make(map[string]Sample, len(s.Samples))
	for k, v := range s.Samples {
		samples[k] = v
	}
	s.Samples = samples
	if s.Pending != nil {
		pending := *s.Pending
		s.Pending = &pending
	}
	return s
}
func lookup(s Settings, id string) (Node, bool) {
	if s.Primary.ID == id {
		return s.Primary, true
	}
	for _, n := range s.Backups {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}
func lookupActive(s State, id string) (Node, bool) {
	if id != "" && s.Emergency.ID == id && s.ActiveID == id {
		return s.Emergency, true
	}
	return lookup(s.Settings, id)
}
func validSettings(s Settings) bool {
	if len(s.Backups) < 1 || len(s.Backups) > 2 {
		return false
	}
	seen := map[string]bool{}
	for _, n := range append([]Node{s.Primary}, s.Backups...) {
		if n.ID == "" || len(n.ID) > 128 || seen[n.ID] || len(n.Fingerprint) != 64 {
			return false
		}
		if _, err := hex.DecodeString(n.Fingerprint); err != nil {
			return false
		}
		seen[n.ID] = true
	}
	return true
}
func validPolicy(p Policy) bool {
	return p.ActiveInterval > 0 && p.RecoveryInterval > 0 && p.URLTimeout > 0 && p.FailbackGuard > 0 && p.ReadinessTTL > 0 && p.SampleAge > 0 && p.Failures > 0 && p.Successes > 0
}
func clearProof(s *State, now time.Time) {
	s.Samples = map[string]Sample{}
	s.ActiveFailures = 0
	s.PrimarySuccesses = 0
	s.EvidenceSince = now
}
func ready(s State, id string, now time.Time, p Policy) bool {
	n, ok := lookup(s.Settings, id)
	v, seen := s.Samples[id]
	return ok && seen && v.Verified && v.Verdict == "healthy" && v.Fingerprint == n.Fingerprint && v.Identity != "" &&
		!v.FinishedAt.After(now) && now.Sub(v.FinishedAt) <= p.ReadinessTTL
}
func issue(s State, to, reason string, now time.Time) (State, []Intent) {
	if s.Sequence == ^uint64(0) {
		s.Reason = "sequence-exhausted"
		return s, nil
	}
	n, _ := lookup(s.Settings, to)
	if reason == "auto-choice" {
		n = s.EmergencyProposal
	}
	s.Sequence++
	intent := Intent{Token: s.Sequence, From: s.ActiveID, To: to, Fingerprint: n.Fingerprint, ActiveIdentity: s.ActiveIdentity, Reason: reason, IssuedAt: now}
	s.Pending = &intent
	s.Reason = reason
	return s, []Intent{intent}
}

// Advance returns an independent snapshot and at most one switch intention.
// A caller must revalidate the intention under the existing lifecycle fence;
// emitting it never means a server has actually become active.
func Advance(state State, event Event, now time.Time, policy Policy) (State, []Intent) {
	s := clone(state)
	if now.IsZero() || !validPolicy(policy) {
		s.Reason = "invalid-policy"
		return s, nil
	}
	switch event.Kind {
	case "configure":
		if s.Pending != nil {
			s.Reason = "switch-unconfirmed"
			return s, nil
		}
		if !event.Verified || !validSettings(event.Settings) || event.CurrentIdentity == "" {
			s.Reason = "configuration-unconfirmed"
			return s, nil
		}
		_, rosterActive := lookup(event.Settings, event.CurrentID)
		emergency := event.Emergency
		if rosterActive && emergency != (Node{}) || !rosterActive &&
			(!event.Settings.Enabled || emergency.ID != event.CurrentID || emergency.ID == "" ||
				len(emergency.ID) > 128 || len(emergency.Fingerprint) != 64) {
			s.Reason = "active-outside-roster"
			return s, nil
		}
		if !rosterActive {
			if _, err := hex.DecodeString(emergency.Fingerprint); err != nil {
				s.Reason = "configuration-unconfirmed"
				return s, nil
			}
		}
		if s.LastSwitchAt.IsZero() || (s.ActiveID != "" && s.ActiveID != event.CurrentID) {
			s.LastSwitchAt = now
		}
		s.Settings = event.Settings
		s.Emergency, s.EmergencyProposal = emergency, Node{}
		s.Settings.Backups = append([]Node(nil), event.Settings.Backups...)
		s.ActiveID, s.ActiveIdentity = event.CurrentID, event.CurrentIdentity
		clearProof(&s, now)
		s.Reason = "configured"
		return s, nil
	case "enabled":
		if event.Enabled && (!validSettings(s.Settings) || s.ActiveIdentity == "") {
			s.Reason = "configuration-unconfirmed"
			return s, nil
		}
		s.Settings.Enabled = event.Enabled
		if event.Enabled && s.LastSwitchAt.IsZero() {
			s.LastSwitchAt = now
		}
		clearProof(&s, now)
		s.Reason = "disabled"
		if event.Enabled {
			s.Reason = "awaiting-probes"
		}
		return s, nil
	case "switch-result":
		pending := s.Pending
		if pending == nil || event.Token != pending.Token || !event.Verified || event.CurrentIdentity == "" || now.Before(pending.IssuedAt) {
			return s, nil
		}
		target, ok := lookup(s.Settings, pending.To)
		if pending.Reason == "auto-choice" {
			target = s.EmergencyProposal
			ok = target.ID == pending.To
		}
		if !ok || target.Fingerprint != pending.Fingerprint {
			s.Reason = "switch-unconfirmed"
			return s, nil
		}
		if event.Success {
			if event.CurrentID != pending.To {
				return s, nil
			}
			s.ActiveID, s.ActiveIdentity = event.CurrentID, event.CurrentIdentity
			s.Emergency = Node{}
			if pending.Reason == "auto-choice" {
				s.Emergency = target
			}
			s.LastSwitchAt = now
			s.Pending = nil
			clearProof(&s, now)
			s.Reason = "switched"
		} else {
			if event.CurrentID != pending.From {
				return s, nil
			}
			s.ActiveIdentity = event.CurrentIdentity
			s.Pending = nil
			clearProof(&s, now)
			s.Reason = "switch-failed-confirmed"
		}
		s.EmergencyProposal = Node{}
		return s, nil
	}
	if !s.Settings.Enabled {
		s.Reason = "disabled"
		return s, nil
	}
	if !validSettings(s.Settings) || s.ActiveIdentity == "" {
		s.Reason = "configuration-unconfirmed"
		return s, nil
	}
	if _, ok := lookupActive(s, s.ActiveID); !ok {
		s.Reason = "active-outside-roster"
		return s, nil
	}
	if s.LastSwitchAt.After(now) {
		s.Reason = "clock-unconfirmed"
		return s, nil
	}
	if s.Pending != nil {
		s.Reason = "switch-unconfirmed"
		return s, nil
	}
	if event.Kind == "sample" {
		v := event.Sample
		n, ok := lookupActive(s, v.ServerID)
		if !ok || n.Fingerprint != v.Fingerprint || !v.Verified || v.Identity == "" || v.StartedAt.IsZero() || v.StartedAt.Before(s.EvidenceSince) || v.FinishedAt.Before(v.StartedAt) ||
			v.FinishedAt.After(now) || now.Sub(v.FinishedAt) > policy.SampleAge || v.FinishedAt.Sub(v.StartedAt) > policy.ReadinessTTL {
			return s, nil
		}
		if v.Verdict != "healthy" && v.Verdict != "unhealthy" && v.Verdict != "unknown" {
			return s, nil
		}
		active := v.ServerID == s.ActiveID
		if active && (v.Identity != s.ActiveIdentity || v.StartedAt.Before(s.LastSwitchAt)) {
			return s, nil
		}
		interval := policy.RecoveryInterval
		if active {
			interval = policy.ActiveInterval
		}
		previous, seen := s.Samples[v.ServerID]
		if seen && (v.StartedAt.Sub(previous.StartedAt) < interval || v.StartedAt.Before(previous.FinishedAt)) {
			return s, nil
		}
		continuous := seen && v.StartedAt.Sub(previous.StartedAt) <= 2*interval+2*policy.URLTimeout
		s.Samples[v.ServerID] = v
		if active {
			if v.Verdict == "unhealthy" {
				if !continuous {
					s.ActiveFailures = 0
				}
				if s.ActiveFailures < policy.Failures {
					s.ActiveFailures++
				}
			} else {
				s.ActiveFailures = 0
			}
		} else if v.ServerID == s.Settings.Primary.ID {
			if v.Verdict == "healthy" {
				if !continuous {
					s.PrimarySuccesses = 0
				}
				if s.PrimarySuccesses < policy.Successes {
					s.PrimarySuccesses++
				}
			} else {
				s.PrimarySuccesses = 0
			}
		}
	} else if event.Kind != "tick" && event.Kind != "auto-choice" {
		return s, nil
	}
	if s.Emergency.ID != "" {
		for _, n := range append([]Node{s.Settings.Primary}, s.Settings.Backups...) {
			if ready(s, n.ID, now, policy) {
				return issue(s, n.ID, "roster-recovered", now)
			}
		}
	}
	if s.ActiveFailures >= policy.Failures {
		failure, seen := s.Samples[s.ActiveID]
		if !seen || !failure.Verified || failure.Verdict != "unhealthy" || failure.Identity != s.ActiveIdentity ||
			failure.FinishedAt.After(now) || now.Sub(failure.FinishedAt) > policy.SampleAge {
			s.ActiveFailures = 0
			s.Reason = "awaiting-active-probe"
			return s, nil
		}
		for _, n := range append([]Node{s.Settings.Primary}, s.Settings.Backups...) {
			if n.ID != s.ActiveID && ready(s, n.ID, now, policy) {
				return issue(s, n.ID, "active-failed", now)
			}
		}
		s.Reason = "no-ready-backup"
		if event.Kind == "auto-choice" && s.Sequence != ^uint64(0) && validAutoChoice(s, event.Sample, now, policy) {
			s.EmergencyProposal = Node{ID: event.Sample.ServerID, Fingerprint: event.Sample.Fingerprint}
			s.Samples[event.Sample.ServerID] = event.Sample
			return issue(s, event.Sample.ServerID, "auto-choice", now)
		}
		return s, nil
	}
	if s.Emergency.ID != "" {
		s.Reason = "auto-choice-active"
		return s, nil
	}
	if s.ActiveID != s.Settings.Primary.ID && s.PrimarySuccesses >= policy.Successes &&
		now.Sub(s.LastSwitchAt) >= policy.FailbackGuard && ready(s, s.Settings.Primary.ID, now, policy) {
		return issue(s, s.Settings.Primary.ID, "primary-recovered", now)
	}
	s.Reason = "monitoring"
	return s, nil
}

// A candidate sample is supplied by the transaction owner only after applying
// the user's auto-selection criteria and verifying traffic through that profile.
func validAutoChoice(s State, v Sample, now time.Time, p Policy) bool {
	if v.ServerID == "" || len(v.ServerID) > 128 || v.ServerID == s.ActiveID ||
		!v.Verified || v.Verdict != "healthy" || v.Identity == "" ||
		v.StartedAt.IsZero() || v.StartedAt.Before(s.EvidenceSince) ||
		v.FinishedAt.Before(v.StartedAt) || v.FinishedAt.After(now) ||
		now.Sub(v.FinishedAt) > p.SampleAge || v.FinishedAt.Sub(v.StartedAt) > p.ReadinessTTL ||
		len(v.Fingerprint) != 64 {
		return false
	}
	if _, err := hex.DecodeString(v.Fingerprint); err != nil {
		return false
	}
	if _, inRoster := lookup(s.Settings, v.ServerID); inRoster {
		return false
	}
	return AllRosterUnavailable(s, now, p)
}

// AllRosterUnavailable requires recent negative evidence for each configured
// member. A missing or unknown sample is insufficient to leave the roster.
func AllRosterUnavailable(s State, now time.Time, p Policy) bool {
	for _, n := range append([]Node{s.Settings.Primary}, s.Settings.Backups...) {
		sample := s.Samples[n.ID]
		if !sample.Verified || sample.Verdict != "unhealthy" ||
			sample.Fingerprint != n.Fingerprint || sample.FinishedAt.After(now) ||
			now.Sub(sample.FinishedAt) > p.ReadinessTTL {
			return false
		}
	}
	return true
}

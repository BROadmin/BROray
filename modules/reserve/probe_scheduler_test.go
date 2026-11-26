package reserve

import (
	"sync"
	"testing"
	"time"
)

func TestProbeSchedulerDueDoesNotConsumeLease(t *testing.T) {
	q := &ProbeScheduler{}
	d, ok := any(q).(interface {
		Due(State, time.Time, Policy) bool
	})
	if !ok {
		t.Fatal("read-only due query missing")
	}
	s := scheduledState()
	if !d.Due(s, base, DefaultPolicy()) || !d.Due(s, base, DefaultPolicy()) {
		t.Fatal("due query consumed attempt")
	}
	l := beginProbe(t, q, s, base, "p")
	if d.Due(s, base.Add(time.Second), DefaultPolicy()) {
		t.Fatal("query permits overlapping probe")
	}
	s, _ = completeProbe(q, l, s, "healthy")
	for i, id := range []string{"b", "c"} {
		l = beginProbe(t, q, s, base.Add(time.Duration(i+1)*time.Second), id)
		s, _ = completeProbe(q, l, s, "healthy")
	}
	if d.Due(s, base.Add(14*time.Second), DefaultPolicy()) {
		t.Fatal("query ignores cooldown")
	}
	if !d.Due(s, base.Add(15*time.Second), DefaultPolicy()) {
		t.Fatal("due active probe suppressed")
	}
	beginProbe(t, q, s, base.Add(15*time.Second), "p")
}

func TestProbeSchedulerSlowBackupDoesNotStarveSecond(t *testing.T) {
	q := &ProbeScheduler{}
	s := scheduledState()
	l := beginProbe(t, q, s, base, "p")
	s, _ = completeProbe(q, l, s, "healthy")
	l = beginProbe(t, q, s, base.Add(time.Second), "b")
	v := sample(s, "b", "healthy", l.StartedAt).Sample
	v.FinishedAt = base.Add(61 * time.Second)
	s, _ = q.Complete(l, s, v, v.FinishedAt, DefaultPolicy(), true)
	l = beginProbe(t, q, s, v.FinishedAt, "p")
	s, _ = completeProbe(q, l, s, "healthy")
	beginProbe(t, q, s, base.Add(62*time.Second), "c")
}

func TestProbeSchedulerConcurrentAcquisitionAndExhaustion(t *testing.T) {
	q := &ProbeScheduler{}
	s := scheduledState()
	results := make(chan ProbeLease, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l, ok := q.Begin(s, base, DefaultPolicy()); ok {
				results <- l
			}
		}()
	}
	wg.Wait()
	close(results)
	var leases []ProbeLease
	for l := range results {
		leases = append(leases, l)
	}
	if len(leases) != 1 {
		t.Fatalf("simultaneous probes: %d", len(leases))
	}
	completeProbe(q, leases[0], s, "healthy")
	q.sequence = ^uint64(0)
	if _, ok := q.Begin(s, base.Add(time.Minute), DefaultPolicy()); ok {
		t.Fatal("exhausted sequence reused")
	}
}

func scheduledState() State {
	s := configured()
	s.EvidenceSince, s.LastSwitchAt = base, base
	return s
}

func beginProbe(t *testing.T, q *ProbeScheduler, s State, at time.Time, id string) ProbeLease {
	t.Helper()
	l, ok := q.Begin(s, at, DefaultPolicy())
	if !ok || l.Node.ID != id || !l.StartedAt.Equal(at) {
		t.Fatalf("probe got %+v %v, want %s", l, ok, id)
	}
	return l
}

func completeProbe(q *ProbeScheduler, l ProbeLease, s State, verdict string) (State, []Intent) {
	v := sample(s, l.Node.ID, verdict, l.StartedAt).Sample
	return q.Complete(l, s, v, v.FinishedAt, DefaultPolicy(), true)
}

func TestProbeSchedulerCadenceAndUnknownAttempts(t *testing.T) {
	q := &ProbeScheduler{}
	s := scheduledState()
	l := beginProbe(t, q, s, base, "p")
	s, _ = completeProbe(q, l, s, "healthy")
	for i, id := range []string{"b", "c"} {
		l = beginProbe(t, q, s, base.Add(time.Duration(i+1)*time.Second), id)
		v := Sample{ServerID: id, Fingerprint: l.Node.Fingerprint, Verdict: "unknown", StartedAt: l.StartedAt, FinishedAt: l.StartedAt.Add(time.Second)}
		s, _ = q.Complete(l, s, v, v.FinishedAt, DefaultPolicy(), true)
	}
	if _, ok := q.Begin(s, base.Add(14*time.Second), DefaultPolicy()); ok {
		t.Fatal("early attempt")
	}
	l = beginProbe(t, q, s, base.Add(15*time.Second), "p")
	s, _ = completeProbe(q, l, s, "healthy")
	if _, ok := q.Begin(s, base.Add(16*time.Second), DefaultPolicy()); ok {
		t.Fatal("unknown backup retried early")
	}
	l = beginProbe(t, q, s, base.Add(60*time.Second), "p")
	s, _ = completeProbe(q, l, s, "healthy")
	l = beginProbe(t, q, s, base.Add(61*time.Second), "b")
	s, _ = completeProbe(q, l, s, "healthy")
	beginProbe(t, q, s, base.Add(62*time.Second), "c")
}

func TestProbeSchedulerRetainsUnfinishedLease(t *testing.T) {
	q := &ProbeScheduler{}
	s := scheduledState()
	l := beginProbe(t, q, s, base, "p")
	v := sample(s, "p", "unhealthy", base).Sample
	after, intents := q.Complete(l, s, v, base.Add(time.Second), DefaultPolicy(), false)
	if after.ActiveFailures != 0 || len(intents) != 0 {
		t.Fatal("unconfirmed cleanup counted")
	}
	if _, ok := q.Begin(s, base.Add(time.Hour), DefaultPolicy()); ok {
		t.Fatal("age released in-flight probe")
	}
	s.Settings.Enabled = false
	if _, ok := q.Begin(s, base.Add(time.Hour), DefaultPolicy()); ok {
		t.Fatal("disable released in-flight probe")
	}
	s.Settings.Enabled = true
	s.ActiveIdentity = "renewed-lifecycle"
	if _, ok := q.Begin(s, base.Add(time.Hour), DefaultPolicy()); ok {
		t.Fatal("authority change overlapped cleanup")
	}
	after, intents = q.Complete(l, s, v, base.Add(time.Hour), DefaultPolicy(), true)
	if after.ActiveFailures != 0 || len(intents) != 0 {
		t.Fatal("old authority counted")
	}
	beginProbe(t, q, s, base.Add(time.Hour), "p")
}

func TestProbeSchedulerRejectsForeignCompletionAndInvalidState(t *testing.T) {
	q, other := &ProbeScheduler{}, &ProbeScheduler{}
	s := scheduledState()
	l := beginProbe(t, q, s, base, "p")
	foreign := beginProbe(t, other, s, base, "p")
	v := sample(s, "p", "unhealthy", base).Sample
	q.Complete(foreign, s, v, base.Add(time.Second), DefaultPolicy(), true)
	if _, ok := q.Begin(s, base.Add(time.Minute), DefaultPolicy()); ok {
		t.Fatal("foreign completion released lease")
	}
	forged := l
	forged.Node.ID = "b"
	q.Complete(forged, s, v, base.Add(time.Second), DefaultPolicy(), true)
	if _, ok := q.Begin(s, base.Add(time.Minute), DefaultPolicy()); ok {
		t.Fatal("mutated completion released lease")
	}
	v.StartedAt = base.Add(-time.Second)
	after, intents := q.Complete(l, s, v, base.Add(time.Second), DefaultPolicy(), true)
	if after.ActiveFailures != 0 || len(intents) != 0 {
		t.Fatal("sample predates lease")
	}
	for _, kind := range []string{"off", "pending", "missing-active", "future", "invalid-policy"} {
		t.Run(kind, func(t *testing.T) {
			state := scheduledState()
			p := DefaultPolicy()
			switch kind {
			case "off":
				state.Settings.Enabled = false
			case "pending":
				state.Pending = &Intent{}
			case "missing-active":
				state.ActiveID = "absent"
			case "future":
				state.EvidenceSince = base.Add(time.Second)
			case "invalid-policy":
				p.ActiveInterval = 0
			}
			if _, ok := (&ProbeScheduler{}).Begin(state, base, p); ok {
				t.Fatal("invalid state admitted")
			}
		})
	}
}

func TestProbeSchedulerRechecksBackupAfterConfirmedActiveFailure(t *testing.T) {
	q := &ProbeScheduler{}
	s := scheduledState()
	l := beginProbe(t, q, s, base, "p")
	s, _ = completeProbe(q, l, s, "unhealthy")
	l = beginProbe(t, q, s, base.Add(time.Second), "b")
	s, _ = completeProbe(q, l, s, "healthy")
	l = beginProbe(t, q, s, base.Add(2*time.Second), "c")
	s, _ = completeProbe(q, l, s, "unhealthy")
	l = beginProbe(t, q, s, base.Add(15*time.Second), "p")
	s, _ = completeProbe(q, l, s, "unhealthy")
	l = beginProbe(t, q, s, base.Add(30*time.Second), "p")
	failed := sample(s, "p", "unhealthy", l.StartedAt).Sample
	failed.FinishedAt = l.StartedAt.Add(16 * time.Second)
	s, intents := q.Complete(l, s, failed, failed.FinishedAt, DefaultPolicy(), true)
	if s.ActiveFailures != 3 || len(intents) != 0 || s.Pending != nil {
		t.Fatal("old backup proof used at the confirmed failure", s, intents)
	}
	l = beginProbe(t, q, s, failed.FinishedAt, "b")
	s, intents = completeProbe(q, l, s, "unhealthy")
	if len(intents) != 0 || AllRosterUnavailable(s, l.StartedAt.Add(time.Second), DefaultPolicy()) {
		t.Fatal("another backup still needs a fresh check", s, intents)
	}
	l = beginProbe(t, q, s, l.StartedAt.Add(time.Second), "c")
	s, intents = completeProbe(q, l, s, "healthy")
	if len(intents) != 1 || intents[0].To != "c" || s.ActiveID != "p" || s.Pending == nil {
		t.Fatal("missing intention or premature active change", s, intents)
	}
	if _, ok := q.Begin(s, base.Add(time.Hour), DefaultPolicy()); ok {
		t.Fatal("pending transaction admitted probe")
	}
}

func TestProbeSchedulerRefreshesActiveFailureDuringSlowBackupChecks(t *testing.T) {
	q := &ProbeScheduler{}
	s := scheduledState()
	l := beginProbe(t, q, s, base, "p")
	s, _ = completeProbe(q, l, s, "unhealthy")
	for i, id := range []string{"b", "c"} {
		l = beginProbe(t, q, s, base.Add(time.Duration(i+1)*time.Second), id)
		s, _ = completeProbe(q, l, s, "healthy")
	}
	for _, offset := range []time.Duration{15 * time.Second, 30 * time.Second} {
		l = beginProbe(t, q, s, base.Add(offset), "p")
		s, _ = completeProbe(q, l, s, "unhealthy")
	}
	l = beginProbe(t, q, s, base.Add(31*time.Second), "b")
	slow := sample(s, "b", "unhealthy", l.StartedAt).Sample
	slow.FinishedAt = l.StartedAt.Add(24 * time.Second)
	s, _ = q.Complete(l, s, slow, slow.FinishedAt, DefaultPolicy(), true)
	l = beginProbe(t, q, s, slow.FinishedAt, "p")
	s, _ = completeProbe(q, l, s, "unhealthy")
	if s.ActiveFailures != DefaultPolicy().Failures {
		t.Fatal("refresh lost the confirmed active failure")
	}
	l = beginProbe(t, q, s, l.StartedAt.Add(time.Second), "c")
	s, _ = completeProbe(q, l, s, "unhealthy")
	if !AllRosterUnavailable(s, l.StartedAt.Add(time.Second), DefaultPolicy()) {
		t.Fatal("slow checks prevented a fresh all-roster decision")
	}
}

func TestProbeSchedulerEmergencyRecoveryCadenceAndFairness(t *testing.T) {
	q := &ProbeScheduler{}
	s := scheduledState()
	s.Emergency = Node{ID: "auto", Fingerprint: s.Settings.Primary.Fingerprint}
	s.ActiveID, s.ActiveIdentity = "auto", "runtime-auto"
	complete := func(l ProbeLease, verdict string, duration time.Duration) []Intent {
		v := sample(s, l.Node.ID, verdict, l.StartedAt).Sample
		v.Fingerprint = l.Node.Fingerprint
		v.FinishedAt = l.StartedAt.Add(duration)
		var intents []Intent
		s, intents = q.Complete(l, s, v, v.FinishedAt, DefaultPolicy(), true)
		return intents
	}
	complete(beginProbe(t, q, s, base, "auto"), "healthy", time.Second)
	for i, id := range []string{"p", "b", "c"} {
		complete(beginProbe(t, q, s, base.Add(time.Duration(i+1)*time.Second), id), "unhealthy", time.Second)
	}
	complete(beginProbe(t, q, s, base.Add(15*time.Second), "auto"), "healthy", 18*time.Second)
	l := beginProbe(t, q, s, base.Add(33*time.Second), "p")
	intents := complete(l, "healthy", time.Second)
	if len(intents) != 1 || intents[0].To != "p" || intents[0].Reason != "roster-recovered" {
		t.Fatal("fresh recovery probe did not return to the roster", intents)
	}
}

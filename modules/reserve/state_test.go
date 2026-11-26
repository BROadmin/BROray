package reserve

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var base = time.Unix(1800000000, 0).UTC()

func configured() State {
	return State{Settings: Settings{Enabled: true, Primary: Node{"p", strings.Repeat("a", 64)}, Backups: []Node{{"b", strings.Repeat("b", 64)}, {"c", strings.Repeat("c", 64)}}}, ActiveID: "p", ActiveIdentity: "runtime-p", Samples: map[string]Sample{}}
}
func node(s State, id string) Node {
	if s.Settings.Primary.ID == id {
		return s.Settings.Primary
	}
	for _, n := range s.Settings.Backups {
		if n.ID == id {
			return n
		}
	}
	return Node{}
}
func sample(s State, id, verdict string, at time.Time) Event {
	identity := "probe-" + id
	if id == s.ActiveID {
		identity = s.ActiveIdentity
	}
	return Event{Kind: "sample", Sample: Sample{ServerID: id, Fingerprint: node(s, id).Fingerprint, Identity: identity, Verdict: verdict, StartedAt: at, FinishedAt: at.Add(time.Second), Verified: true}}
}
func observe(s State, id, verdict string, at time.Time) (State, []Intent) {
	return Advance(s, sample(s, id, verdict, at), at.Add(time.Second), DefaultPolicy())
}

func TestReserveExpiredFailureDoesNotTriggerLaterBackup(t *testing.T) {
	s := configured()
	for i := 0; i < 3; i++ {
		s, _ = observe(s, "p", "unhealthy", base.Add(time.Duration(i)*15*time.Second))
	}
	// No backup was ready during the failure; a later successful backup probe
	// is not proof that the active server is still failing.
	s, out := observe(s, "b", "healthy", base.Add(5*time.Minute))
	if len(out) != 0 || s.Pending != nil {
		t.Fatal("expired active failures authorized a new switch")
	}
}

func TestReserveOverlappingRoundsDoNotAccumulate(t *testing.T) {
	s := configured()
	s, _ = observe(s, "b", "healthy", base)
	for i := 0; i < 3; i++ {
		e := sample(s, "p", "unhealthy", base.Add(time.Duration(i)*15*time.Second))
		e.Sample.FinishedAt = e.Sample.StartedAt.Add(24 * time.Second)
		var out []Intent
		s, out = Advance(s, e, e.Sample.FinishedAt, DefaultPolicy())
		if len(out) != 0 {
			t.Fatal("overlapping rounds counted as three consecutive failures")
		}
	}
}

func TestReserveUnknownSwitchHistoryStartsReturnGuard(t *testing.T) {
	s := configured()
	s.ActiveID, s.ActiveIdentity = "b", "runtime-b"
	s, _ = Advance(s, Event{Kind: "configure", Settings: s.Settings, Verified: true, CurrentID: "b", CurrentIdentity: "runtime-b"}, base, DefaultPolicy())
	for i := 0; i < 3; i++ {
		var out []Intent
		s, out = observe(s, "p", "healthy", base.Add(time.Duration(i)*time.Minute))
		if len(out) != 0 {
			t.Fatal("unknown last switch treated as expired return guard")
		}
	}
}

func TestReserveDisabledPendingCompletionAndRollback(t *testing.T) {
	for _, success := range []bool{true, false} {
		s := configured()
		s, _ = observe(s, "b", "healthy", base)
		for i := 0; i < 3; i++ {
			s, _ = observe(s, "p", "unhealthy", base.Add(time.Duration(i)*15*time.Second))
		}
		s, _ = observe(s, "b", "healthy", base.Add(31*time.Second))
		if s.Pending == nil {
			t.Fatal("missing switch")
		}
		s, out := Advance(s, Event{Kind: "enabled", Enabled: false}, base.Add(32*time.Second), DefaultPolicy())
		if len(out) != 0 || s.Pending == nil || s.ActiveID != "p" || s.Settings.Enabled {
			t.Fatal("disable changed active or lost unresolved switch")
		}
		id := "p"
		if success {
			id = "b"
		}
		e := Event{Kind: "switch-result", Token: s.Pending.Token + 1, Verified: true, Success: success, CurrentID: id, CurrentIdentity: "new-runtime-" + id}
		wrong, _ := Advance(s, e, base.Add(35*time.Second), DefaultPolicy())
		if wrong.Pending == nil || wrong.ActiveID != "p" {
			t.Fatal("wrong token resolved switch")
		}
		e.Token = s.Pending.Token
		done, out := Advance(s, e, base.Add(36*time.Second), DefaultPolicy())
		if len(out) != 0 || done.Pending != nil || done.ActiveID != id || done.Settings.Enabled || len(done.Settings.Backups) != 2 {
			t.Fatal("disabled mode must reconcile verified completion without another switch", done)
		}
		if !success && !done.LastSwitchAt.Equal(s.LastSwitchAt) {
			t.Fatal("rollback reset last successful switch time")
		}
	}
}

func TestReserveInvalidConfigurationAndDefaultOff(t *testing.T) {
	for _, mutate := range []func(*Settings){
		func(c *Settings) { c.Primary.ID = "" },
		func(c *Settings) { c.Backups = nil },
		func(c *Settings) { c.Backups = append(c.Backups, Node{"d", strings.Repeat("d", 64)}) },
		func(c *Settings) { c.Backups[0].ID = c.Primary.ID },
		func(c *Settings) { c.Backups[0].Fingerprint = strings.Repeat("z", 64) },
	} {
		s := configured()
		cfg := s.Settings
		cfg.Backups = append([]Node(nil), cfg.Backups...)
		mutate(&cfg)
		next, out := Advance(s, Event{Kind: "configure", Settings: cfg, Verified: true, CurrentID: s.ActiveID, CurrentIdentity: s.ActiveIdentity}, base, DefaultPolicy())
		if len(out) != 0 || !reflect.DeepEqual(next.Settings, s.Settings) {
			t.Fatal("invalid roster accepted")
		}
	}
	zero, out := Advance(State{}, Event{Kind: "tick"}, base, DefaultPolicy())
	if zero.Settings.Enabled || len(out) != 0 || zero.Pending != nil {
		t.Fatal("reserve not default OFF")
	}
}

func TestReserveCompletionCannotPredateIntent(t *testing.T) {
	s := configured()
	s, _ = observe(s, "b", "healthy", base)
	for i := 0; i < 3; i++ {
		s, _ = observe(s, "p", "unhealthy", base.Add(time.Duration(i)*15*time.Second))
	}
	s, _ = observe(s, "b", "healthy", base.Add(31*time.Second))
	if s.Pending == nil {
		t.Fatal("missing pending switch")
	}
	e := Event{Kind: "switch-result", Token: s.Pending.Token, Verified: true, Success: true, CurrentID: "b", CurrentIdentity: "runtime-b"}
	next, out := Advance(s, e, s.Pending.IssuedAt.Add(-time.Second), DefaultPolicy())
	if len(out) != 0 || next.Pending == nil || next.ActiveID != "p" {
		t.Fatal("completion accepted before intent time")
	}
}

func TestReserveReconfigurationRejectsInflightOldSamples(t *testing.T) {
	for _, kind := range []string{"configure", "enabled"} {
		t.Run(kind, func(t *testing.T) {
			s := configured()
			old := sample(s, "p", "unhealthy", base)
			e := Event{Kind: kind, Settings: s.Settings, Enabled: true, Verified: true, CurrentID: s.ActiveID, CurrentIdentity: s.ActiveIdentity}
			s, _ = Advance(s, e, base.Add(2*time.Second), DefaultPolicy())
			s, out := Advance(s, old, base.Add(3*time.Second), DefaultPolicy())
			if s.ActiveFailures != 0 || len(s.Samples) != 0 || len(out) != 0 {
				t.Fatal("pre-configuration sample reused", s)
			}
		})
	}
}

func TestReserveThreeFailuresAndSinglePendingIntent(t *testing.T) {
	s := configured()
	s, _ = observe(s, "b", "healthy", base)
	for i := 0; i < 3; i++ {
		var out []Intent
		s, out = observe(s, "p", "unhealthy", base.Add(time.Duration(i)*15*time.Second))
		if len(out) != 0 {
			t.Fatalf("round%d intents=%v state=%+v", i, out, s)
		}
	}
	s, out := observe(s, "b", "healthy", base.Add(31*time.Second))
	if len(out) != 1 {
		t.Fatal("fresh backup readiness did not authorize switch", out)
	}
	if s.ActiveID != "p" || s.Pending == nil || s.Pending.To != "b" {
		t.Fatal("intent is not confirmed activation", s)
	}
	next, out := Advance(s, Event{Kind: "tick"}, base.Add(time.Minute), DefaultPolicy())
	if len(out) != 0 || next.Pending == nil {
		t.Fatal("duplicate activation")
	}
	unknown, _ := Advance(s, Event{Kind: "switch-result", Token: s.Pending.Token, Success: true, CurrentIdentity: "runtime-b"}, base.Add(time.Minute), DefaultPolicy())
	if unknown.Pending == nil || unknown.ActiveID != "p" {
		t.Fatal("ambiguous result accepted")
	}
	done, _ := Advance(s, Event{Kind: "switch-result", Token: s.Pending.Token, Verified: true, Success: true, CurrentID: "b", CurrentIdentity: "runtime-b"}, base.Add(time.Minute), DefaultPolicy())
	if done.ActiveID != "b" || done.Pending != nil || !done.LastSwitchAt.Equal(base.Add(time.Minute)) {
		t.Fatal("confirmed activation not applied", done)
	}
}
func TestReserveFailureCounterReset(t *testing.T) {
	s := configured()
	s, _ = observe(s, "p", "unhealthy", base)
	s, _ = observe(s, "p", "unhealthy", base.Add(15*time.Second))
	if s.ActiveFailures != 2 {
		t.Fatal(s.ActiveFailures)
	}
	s, _ = observe(s, "p", "healthy", base.Add(30*time.Second))
	if s.ActiveFailures != 0 {
		t.Fatal("success did not reset")
	}
	s, _ = observe(s, "p", "unhealthy", base.Add(45*time.Second))
	s, _ = observe(s, "p", "unknown", base.Add(60*time.Second))
	if s.ActiveFailures != 0 {
		t.Fatal("unknown counted as continuous failure")
	}
}
func TestReserveStableFailbackAndGuard(t *testing.T) {
	s := configured()
	s.ActiveID = "b"
	s.ActiveIdentity = "runtime-b"
	s.LastSwitchAt = base
	for i := 0; i < 3; i++ {
		var out []Intent
		s, out = observe(s, "p", "healthy", base.Add(time.Duration(i)*time.Minute))
		if len(out) != 0 {
			t.Fatal("early failback")
		}
	}
	if s.PrimarySuccesses != 3 {
		t.Fatal("success counter", s.PrimarySuccesses)
	}
	// Old readiness cannot authorize a return even after the guard expires.
	s, out := Advance(s, Event{Kind: "tick"}, base.Add(10*time.Minute), DefaultPolicy())
	if len(out) != 0 {
		t.Fatal("stale primary readiness")
	}
	for i := 0; i < 3; i++ {
		s, out = observe(s, "p", "healthy", base.Add(time.Duration(10+i)*time.Minute))
		if (i == 2) != (len(out) == 1) {
			t.Fatalf("recovery round%d intents=%v", i, out)
		}
	}
	if s.Pending == nil || s.Pending.To != "p" || s.Pending.Reason != "primary-recovered" {
		t.Fatal(s.Pending)
	}
}
func TestReserveEmergencyIgnoresReturnGuard(t *testing.T) {
	s := configured()
	s.ActiveID = "b"
	s.ActiveIdentity = "runtime-b"
	s.LastSwitchAt = base
	s, _ = observe(s, "c", "healthy", base)
	var out []Intent
	for i := 0; i < 3; i++ {
		s, out = observe(s, "b", "unhealthy", base.Add(time.Duration(i)*15*time.Second))
		if len(out) != 0 {
			t.Fatal("used pre-failure readiness", out)
		}
	}
	s, out = observe(s, "c", "healthy", base.Add(31*time.Second))
	if len(out) != 1 || out[0].To != "c" || out[0].Reason != "active-failed" {
		t.Fatal("emergency delayed", out)
	}
}
func TestReserveRejectsUnboundStaleDuplicateEvidence(t *testing.T) {
	for _, kind := range []string{"fingerprint", "identity", "unverified", "future", "stale"} {
		t.Run(kind, func(t *testing.T) {
			s := configured()
			ev := sample(s, "p", "unhealthy", base)
			switch kind {
			case "fingerprint":
				ev.Sample.Fingerprint = strings.Repeat("f", 64)
			case "identity":
				ev.Sample.Identity = "foreign"
			case "unverified":
				ev.Sample.Verified = false
			case "future":
				ev.Sample.FinishedAt = base.Add(time.Hour)
			case "stale":
				ev.Sample.StartedAt = base.Add(-time.Hour)
				ev.Sample.FinishedAt = base.Add(-time.Minute)
			}
			s, out := Advance(s, ev, base.Add(time.Second), DefaultPolicy())
			if s.ActiveFailures != 0 || len(out) != 0 {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	s := configured()
	ev := sample(s, "p", "unhealthy", base)
	s, _ = Advance(s, ev, base.Add(time.Second), DefaultPolicy())
	s, _ = Advance(s, ev, base.Add(2*time.Second), DefaultPolicy())
	if s.ActiveFailures != 1 {
		t.Fatal("duplicate evidence counted", s.ActiveFailures)
	}
}
func TestReserveAllFailedAndDisableKeepVPN(t *testing.T) {
	s := configured()
	s, _ = observe(s, "b", "unhealthy", base)
	s, _ = observe(s, "c", "unhealthy", base)
	for i := 0; i < 3; i++ {
		var out []Intent
		s, out = observe(s, "p", "unhealthy", base.Add(time.Duration(i)*15*time.Second))
		if len(out) != 0 {
			t.Fatal("unsafe destination", out)
		}
	}
	if s.Reason != "no-ready-backup" || s.ActiveID != "p" {
		t.Fatal(s.Reason)
	}
	s.ActiveID = "b"
	s.ActiveIdentity = "runtime-b"
	roster := s.Settings
	s, out := Advance(s, Event{Kind: "enabled", Enabled: false}, base.Add(time.Minute), DefaultPolicy())
	if s.Settings.Enabled || s.ActiveID != "b" || s.ActiveIdentity != "runtime-b" || len(out) != 0 || !reflect.DeepEqual(s.Settings.Backups, roster.Backups) {
		t.Fatal("disable changed connection/roster")
	}
}

func TestReserveAutoChoiceAndReturn(t *testing.T) {
	s := configured()
	s, _ = observe(s, "b", "unhealthy", base)
	s, _ = observe(s, "c", "unhealthy", base)
	standby := Node{"auto", strings.Repeat("d", 64)}
	at := base.Add(34 * time.Second)
	ev := Event{Kind: "auto-choice", Sample: Sample{
		ServerID: standby.ID, Fingerprint: standby.Fingerprint, Identity: "probe-auto",
		Verdict: "healthy", StartedAt: at, FinishedAt: at.Add(time.Second), Verified: true,
	}}
	now := ev.Sample.FinishedAt
	if _, out := Advance(s, ev, now, DefaultPolicy()); len(out) != 0 {
		t.Fatal("auto-choice allowed before active failure")
	}
	for i := 0; i < 3; i++ {
		s, _ = observe(s, "p", "unhealthy", base.Add(time.Duration(i)*15*time.Second))
	}
	if _, out := Advance(s, ev, now, DefaultPolicy()); len(out) != 0 {
		t.Fatal("auto-choice used pre-failure unavailability")
	}
	s, _ = observe(s, "b", "unhealthy", base.Add(31*time.Second))
	s, _ = observe(s, "c", "unhealthy", base.Add(32*time.Second))
	bad := ev
	bad.Sample.Verified = false
	if _, out := Advance(s, bad, now, DefaultPolicy()); len(out) != 0 {
		t.Fatal("unverified auto-choice allowed")
	}
	missing := clone(s)
	delete(missing.Samples, "c")
	if _, out := Advance(missing, ev, now, DefaultPolicy()); len(out) != 0 {
		t.Fatal("auto-choice allowed with unconfirmed roster member")
	}
	s, out := Advance(s, ev, now, DefaultPolicy())
	if len(out) != 1 || out[0].To != standby.ID || s.ActiveID != "p" {
		t.Fatal("confirmed emergency switch not issued", out)
	}
	s, _ = Advance(s, Event{Kind: "switch-result", Token: out[0].Token, Verified: true, Success: true, CurrentID: standby.ID, CurrentIdentity: "runtime-auto"}, now, DefaultPolicy())
	if s.ActiveID != standby.ID || !reflect.DeepEqual(s.Settings, configured().Settings) {
		t.Fatal("emergency switch changed the configured roster", s)
	}
	var scheduler ProbeScheduler
	lease, ok := scheduler.Begin(s, now, DefaultPolicy())
	if !ok || lease.Node != standby {
		t.Fatal("temporary active server is not monitored", lease)
	}
	_, out = observe(s, "c", "healthy", now.Add(time.Second))
	if len(out) != 1 || out[0].To != "c" {
		t.Fatal("no prompt return to the recovered roster member", out)
	}
}
func TestReserveConfigurationInvalidatesProofWithoutMutatingInput(t *testing.T) {
	s := configured()
	before := configured()
	s, _ = observe(s, "b", "healthy", base)
	s, _ = observe(s, "p", "unhealthy", base)
	cfg := s.Settings
	cfg.Backups = append([]Node(nil), cfg.Backups...)
	cfg.Backups[0].Fingerprint = strings.Repeat("d", 64)
	next, out := Advance(s, Event{Kind: "configure", Settings: cfg, Verified: true, CurrentID: "p", CurrentIdentity: "runtime-p"}, base.Add(time.Minute), DefaultPolicy())
	if next.ActiveFailures != 0 || len(next.Samples) != 0 || len(out) != 0 {
		t.Fatal("old proof retained")
	}
	if len(s.Samples) != 2 || len(before.Samples) != 0 {
		t.Fatal("input aliased")
	}
	next.Settings.Backups[0].ID = "other"
	if s.Settings.Backups[0].ID != "b" {
		t.Fatal("roster aliased")
	}
}

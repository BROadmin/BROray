package reserve

import (
	"testing"
	"time"
)

func TestReserveWANOutageHoldsAndRevalidatesActive(t *testing.T) {
	s := configured()
	s, _ = observe(s, "p", "unhealthy", base)
	s, _ = observe(s, "p", "unhealthy", base.Add(15*time.Second))
	s, _ = observe(s, "b", "healthy", base.Add(20*time.Second))
	outage := sample(s, "p", "unknown", base.Add(30*time.Second))
	outage.Sample.WANUnavailable = true
	now := outage.Sample.FinishedAt
	next, intents := Advance(s, outage, now, DefaultPolicy())
	if next.ActiveID != "p" || next.ActiveFailures != 0 || next.PrimarySuccesses != 0 ||
		len(next.Samples) != 1 || next.Samples["p"] != outage.Sample || len(intents) != 0 {
		t.Fatalf("WAN hold retained old decisions: %+v %v", next, intents)
	}
	var q ProbeScheduler
	if q.Due(next, now, DefaultPolicy()) {
		t.Fatal("started a backup during confirmed WAN outage")
	}
	retry := base.Add(45 * time.Second)
	lease, ok := q.Begin(next, retry, DefaultPolicy())
	if !ok || lease.Node.ID != "p" {
		t.Fatal("next probe must revalidate the active profile")
	}
	recovered := sample(next, "p", "healthy", retry).Sample
	next, intents = q.Complete(lease, next, recovered, recovered.FinishedAt, DefaultPolicy(), true)
	if next.Samples["p"] != recovered || next.ActiveFailures != 0 || next.ActiveID != "p" || len(intents) != 0 {
		t.Fatalf("active service did not recover in place: %+v %v", next, intents)
	}
	if !q.Due(next, recovered.FinishedAt, DefaultPolicy()) {
		t.Fatal("standby probes did not resume")
	}
}

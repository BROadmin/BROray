// Package reserve is the pure decision policy for optional VPN reservation.
// It performs no I/O and trusts only evidence verified by the lifecycle adapter.
package reserve

import "time"

type Policy struct {
	ActiveInterval, RecoveryInterval, URLTimeout, FailbackGuard, ReadinessTTL, SampleAge time.Duration
	Failures, Successes                                                                  int
}

func DefaultPolicy() Policy {
	return Policy{15 * time.Second, 60 * time.Second, 12 * time.Second, 10 * time.Minute, 120 * time.Second, 30 * time.Second, 3, 3}
}

type Node struct{ ID, Fingerprint string }
type Settings struct {
	Enabled bool
	Primary Node
	Backups []Node
}
type Sample struct {
	ServerID, Fingerprint, Identity, Verdict string // healthy, unhealthy or unknown
	StartedAt, FinishedAt                    time.Time
	Verified                                 bool // owned proxy traffic or confirmed owned exit; lifecycle authority checked externally
}
type Intent struct {
	Token                                         uint64
	From, To, Fingerprint, ActiveIdentity, Reason string
	IssuedAt                                      time.Time
}
type State struct {
	Settings                         Settings
	ActiveID, ActiveIdentity         string
	Emergency                        Node
	EmergencyProposal                Node
	LastSwitchAt                     time.Time
	EvidenceSince                    time.Time // Reject probes started before the latest configuration/lifecycle boundary.
	ActiveFailures, PrimarySuccesses int
	Samples                          map[string]Sample
	Pending                          *Intent
	Sequence                         uint64
	Reason                           string
}
type Event struct {
	Kind                       string // configure, enabled, sample, tick, auto-choice, switch-result
	Settings                   Settings
	Enabled                    bool
	Sample                     Sample
	Token                      uint64
	Verified, Success          bool
	CurrentID, CurrentIdentity string
	Emergency                  Node // configure only, recovered from the committed runtime transaction
}

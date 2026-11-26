package servers

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BROadmin/broray/modules/health"
	"github.com/BROadmin/broray/modules/reserve"
)

type reserveMonitor struct {
	autoChoiceCursor int
	mu               sync.Mutex
	owner            *svc
	scheduler        reserve.ProbeScheduler
	state            reserve.State
	binding          *reserveActiveBinding
	activeEvidence   atomic.Pointer[reserveConnectionEvidence]
	lastFailure      *reserveActiveBinding
	blocked          bool
	authority        string
	retryRevision    string
	retryAfter       time.Time
}

type reserveConnectionEvidence struct {
	binding reserveActiveBinding
	sample  reserve.Sample
}

// activeReserveStatus reads the last verified probe without waiting for the
// monitor, which can hold its lock while checking another server.
func (s *svc) activeReserveStatus(id string) (string, bool) {
	m := s.reserveMon
	if m == nil || s.d.Lock.Ambiguous() {
		return "unknown", false
	}
	evidence := m.activeEvidence.Load()
	if evidence == nil || evidence.sample.ServerID != id {
		return "unknown", false
	}
	sample, binding := evidence.sample, evidence.binding
	age := s.now().Sub(sample.FinishedAt)
	if !sample.Verified || sample.Fingerprint != binding.plan.node.Fingerprint || sample.Identity != binding.identity ||
		age < 0 || age > reserve.DefaultPolicy().SampleAge ||
		(sample.Verdict != "healthy" && sample.Verdict != "unhealthy" && !sample.WANUnavailable) ||
		s.d.App == nil || s.d.App.Xray != binding.process ||
		!s.reservePlanCurrent(binding.plan, true, true) || !s.reserveCoreMatches(binding.target.Process) {
		return "unknown", false
	}
	snapshot := binding.process.RuntimeSnapshot()
	if m.activeEvidence.Load() != evidence ||
		(snapshot != binding.snapshot && (sample.Verdict != "unhealthy" || !snapshot.ConfirmsUnexpectedExit(binding.snapshot))) {
		return "unknown", false
	}
	return sample.Verdict, sample.WANUnavailable
}

type reserveMonitorResult struct {
	Probed        bool
	Sample        reserve.Sample
	Intent        *reserve.Intent
	Guard         reserveActiveBinding
	AutoConfigSHA string
}

func (s *svc) monitorReserve(ctx context.Context, m *reserveMonitor) (reserveMonitorResult, error) {
	return s.monitorReserveWith(ctx, m, reserve.Probe)
}

func (s *svc) monitorReserveWith(ctx context.Context, m *reserveMonitor, probe func(context.Context, reserve.ProbeTarget) reserve.ProbeOutcome) (reserveMonitorResult, error) {
	var result reserveMonitorResult
	if m == nil || probe == nil || !s.reserveRuntimeFence(ctx) || !m.mu.TryLock() {
		return result, errReserveRuntime
	}
	defer m.mu.Unlock()
	if m.owner != nil && m.owner != s {
		return result, errReserveRuntime
	}
	m.owner = s
	// The future queue owner must keep its fence on this error. Disabling or
	// waiting cannot establish ownership of an unresolved temporary workspace.
	if m.blocked {
		return result, errReserveProbeCleanup
	}
	if pending, err := reserve.TransactionPending(ctx, s.d.Root); err != nil || pending {
		return result, errors.Join(errReserveTransactionPending, err)
	}
	invalidate := func() {
		m.activeEvidence.Store(nil)
		m.state, _ = reserve.Advance(m.state, reserve.Event{Kind: "enabled", Enabled: false}, time.Now(), reserve.DefaultPolicy())
		m.authority = ""
		m.lastFailure = nil
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		invalidate()
		return result, err
	}
	if !prefs.Enabled {
		invalidate()
		return result, nil
	}
	if m.state.Pending != nil {
		return result, nil
	}
	config, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
	if err != nil {
		invalidate()
		return result, errReserveRuntime
	}
	endpoint, ok := reserve.DiagnosticEndpoint(config)
	if !ok {
		invalidate()
		return result, errReserveRuntime
	}
	active := ActiveID(s.d)
	inRoster := reserveRosterContains(prefs, active)
	if (prefs.Emergency == nil && !inRoster) || (prefs.Emergency != nil && prefs.Emergency.ServerID != active) {
		invalidate()
		return result, errReserveRuntime
	}
	plans := map[string]reserveRuntimePlan{}
	settings := reserve.Settings{Enabled: true}
	authority := ""
	for i, id := range append([]string{prefs.PrimaryServerID}, prefs.BackupServerIDs...) {
		plan, e := s.prepareReserveRuntime(ctx, id, endpoint)
		if e != nil || plan.revision != prefs.Revision {
			invalidate()
			return result, errReserveRuntime
		}
		plans[id] = plan
		authority += id + "\x00" + plan.revision + plan.recordSHA + plan.settingsSHA + plan.configSHA
		if i == 0 {
			settings.Primary = plan.node
		} else {
			settings.Backups = append(settings.Backups, plan.node)
		}
	}
	var emergency reserve.Node
	if !inRoster {
		plan, err := s.prepareReserveRuntime(ctx, active, endpoint)
		if err != nil || plan.revision != prefs.Revision {
			invalidate()
			return result, errReserveRuntime
		}
		plans[active] = plan
		emergency = plan.node
	}
	plan := plans[active]
	binding, err := s.bindReserveRuntime(ctx, plan, m.binding)
	if err != nil {
		// A confirmed unexpected exit can still be counted using the previously
		// verified binding. A manual stop or unproven child is never a failure.
		previous := m.binding
		if previous == nil || previous.process == nil || !sameReservePlan(previous.plan, plan) ||
			!previous.process.RuntimeSnapshot().ConfirmsUnexpectedExit(previous.snapshot) {
			invalidate()
			return result, errReserveRuntime
		}
		binding = *previous
		err = nil
	}
	if m.authority != authority || !reflect.DeepEqual(m.state.Settings, settings) || m.state.ActiveID != active || m.state.ActiveIdentity != binding.identity || m.state.Emergency != emergency {
		m.activeEvidence.Store(nil)
		m.state, _ = reserve.Advance(m.state, reserve.Event{Kind: "configure", Settings: settings, CurrentID: active, CurrentIdentity: binding.identity, Emergency: emergency, Verified: true}, time.Now(), reserve.DefaultPolicy())
		if m.state.Reason != "configured" {
			invalidate()
			return result, errReserveRuntime
		}
		m.authority = authority
		m.lastFailure = nil
	}
	m.binding = &binding
	lease, ok := m.scheduler.Begin(m.state, time.Now(), reserve.DefaultPolicy())
	if !ok {
		return result, nil
	}
	result.Probed = true
	var observed reserve.Sample
	var activeProbe reserve.ProbeOutcome
	if lease.Node.ID == active {
		observed = s.probeReserveRuntimeWith(ctx, binding, func(ctx context.Context, target reserve.ProbeTarget) reserve.ProbeOutcome {
			activeProbe = probe(ctx, target)
			return activeProbe
		})
		if observed.Verified && observed.Verdict == "unhealthy" && activeProbe.ErrorCode == "RESERVE_CONTROL_FAILED" &&
			len(activeProbe.Controls) == len(health.ActiveControls()) && binding.process.RuntimeSnapshot() == binding.snapshot {
			if s.readWANOutage(ctx) {
				observed.Verdict, observed.WANUnavailable = "unknown", true
			}
		}
	} else {
		observed, err = s.probeReserveBackupWith(ctx, lease.Node.ID, probe)
	}
	if errors.Is(err, errReserveProbeCleanup) {
		m.activeEvidence.Store(nil)
		m.blocked = true
		return result, errReserveProbeCleanup
	}
	valid := s.reserveRuntimeFence(ctx) && s.d.App != nil && s.d.App.Xray == binding.process && s.reserveCoreMatches(binding.target.Process)
	for id, p := range plans {
		valid = valid && s.reservePlanInputs(ctx, p, true, id == active)
	}
	snapshot := binding.process.RuntimeSnapshot()
	valid = valid && (snapshot == binding.snapshot || (lease.Node.ID == active && observed.Verdict == "unhealthy" && snapshot.ConfirmsUnexpectedExit(binding.snapshot)))
	if !valid {
		observed.Verified = false
		observed.Verdict = "unknown"
		observed.Identity = ""
		observed.WANUnavailable = false
	}
	var intents []reserve.Intent
	m.state, intents = m.scheduler.Complete(lease, m.state, observed, time.Now(), reserve.DefaultPolicy(), true)
	result.Sample = observed
	if !valid {
		invalidate()
		return result, errReserveRuntime
	}
	if err != nil {
		return result, err
	}
	if lease.Node.ID == active {
		if observed.Verdict != "healthy" && len(activeProbe.Controls) > 0 {
			s.logf("RESERVE_ACTIVE_CONTROLS=%+v WAN_UNAVAILABLE=%t", activeProbe.Controls, observed.WANUnavailable)
		}
		if observed.Verified && m.state.Samples[active] == observed {
			m.activeEvidence.Store(&reserveConnectionEvidence{binding: binding, sample: observed})
		} else {
			m.activeEvidence.Store(nil)
		}
	}
	if lease.Node.ID == active && observed.Verified {
		if observed.Verdict == "unhealthy" {
			failed := binding
			m.lastFailure = &failed
		} else if observed.Verdict == "healthy" || observed.WANUnavailable {
			m.lastFailure = nil
		}
	}
	if len(intents) == 0 && m.state.Reason == "no-ready-backup" && m.lastFailure != nil &&
		reserve.AllRosterUnavailable(m.state, time.Now(), reserve.DefaultPolicy()) {
		observed, hash, err := s.reserveAutoChoice(ctx, m, prefs, active, probe)
		if errors.Is(err, errReserveProbeCleanup) {
			m.activeEvidence.Store(nil)
			m.blocked = true
			return result, err
		}
		if err == nil && observed.Verified {
			m.state, intents = reserve.Advance(m.state, reserve.Event{Kind: "auto-choice", Sample: observed}, time.Now(), reserve.DefaultPolicy())
			result.AutoConfigSHA = hash
		}
	}
	if len(intents) > 0 {
		intent := intents[0]
		guard := binding
		if intent.Reason == "active-failed" || intent.Reason == "auto-choice" {
			if m.lastFailure == nil {
				return result, errReserveRuntime
			}
			guard = *m.lastFailure
		}
		if guard.identity != intent.ActiveIdentity || guard.plan.node.ID != intent.From {
			return result, errReserveRuntime
		}
		result.Intent = &intent
		result.Guard = guard
	}
	return result, nil
}

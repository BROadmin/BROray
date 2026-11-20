package xray

// RuntimeSnapshot is a process-local observation, not permission to signal.
// Its private owner identity is deliberately lost by serialization.
type RuntimeSnapshot struct {
	Wanted, Running    bool
	PID, LastPID       int
	Generation, Intent uint64
	ExitConfirmed      bool
	owner              *Process
}

// RuntimeSnapshot never invokes the core's version command or performs I/O.
func (p *Process) RuntimeSnapshot() RuntimeSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.runtimeSnapshotLocked()
}

func (p *Process) runtimeSnapshotLocked() RuntimeSnapshot {
	s := RuntimeSnapshot{Wanted: p.wanted, LastPID: p.lastChildPID, Generation: p.generation,
		Intent: p.lifecycleIntent, ExitConfirmed: p.exitConfirmed, owner: p}
	if p.cur != nil && !p.cur.exited {
		s.Running = true
		s.PID = p.cur.cmd.Process.Pid
	}
	return s
}

// Saturation invalidates observation proof instead of recycling a generation.
// It deliberately does not prevent ordinary VPN start/stop operations.
func advanceObservation(value *uint64) {
	if *value < ^uint64(0) {
		*value++
	}
}

// ConfirmsUnexpectedExit accepts only a previously running instance from this
// same supervisor and unchanged desired lifecycle. Callers still need their
// record/configuration and queue/maintenance fences; this is not traffic proof.
func (s RuntimeSnapshot) ConfirmsUnexpectedExit(previous RuntimeSnapshot) bool {
	return s.owner != nil && s.owner == previous.owner &&
		previous.Wanted && previous.Running && !previous.ExitConfirmed && previous.PID > 0 && previous.LastPID == previous.PID &&
		s.Wanted && !s.Running && s.PID == 0 && s.ExitConfirmed && s.LastPID == previous.PID &&
		s.Generation > 0 && s.Generation < ^uint64(0) && s.Generation == previous.Generation &&
		s.Intent > 0 && s.Intent < ^uint64(0) && s.Intent == previous.Intent
}

package xray

import (
	"context"
	"errors"
	"syscall"
	"time"
)

var ErrRuntimeChanged = errors.New("XRAY_RUNTIME_CHANGED")

// ReplaceIfCurrent claims the exact observed child before stopping it. The
// expected snapshot may describe that running child or its confirmed unexpected
// exit with Wanted still true. A manual stopped intent cannot restart. A later automatic
// restart never inherits this authority, even if its desired intent is equal.
//
// The caller retains the mutation fence and durable transaction throughout.
// install runs after reap, under the supervisor mutex: it must only perform
// bounded local file checks/writes and must not call Process methods or wait
// for other goroutines. On any error the caller must confirm rollback or retain
// its journal/fence. This method does not restore configuration or prove traffic.
func (p *Process) ReplaceIfCurrent(ctx context.Context, expected RuntimeSnapshot, install func() error) (RuntimeSnapshot, error) {
	return p.changeIfCurrent(ctx, expected, install, true)
}

// StopIfCurrent disconnects only the exact observed child (or its confirmed
// exit). It never starts a child. An already stopped observation is accepted
// only with confirmed reap of a real, previously owned generation.
//
// As with ReplaceIfCurrent, the caller retains the mutation fence and durable
// journal. disconnect runs after reap under the supervisor mutex and must only
// perform bounded local checks/writes, without calling Process methods or
// waiting for other goroutines. Errors require confirmed rollback or retained
// recovery ownership. The returned snapshot proves process state, not traffic.
func (p *Process) StopIfCurrent(ctx context.Context, expected RuntimeSnapshot, disconnect func() error) (RuntimeSnapshot, error) {
	return p.changeIfCurrent(ctx, expected, disconnect, false)
}

func (p *Process) changeIfCurrent(ctx context.Context, expected RuntimeSnapshot, install func() error, restart bool) (RuntimeSnapshot, error) {
	p.mu.Lock()
	current := p.runtimeSnapshotLocked()
	live := expected.Wanted && expected.Running && !expected.ExitConfirmed && expected.PID > 0 && expected.LastPID == expected.PID
	stopped := (!restart || expected.Wanted) && !expected.Running && expected.PID == 0 && expected.ExitConfirmed && expected.LastPID > 0
	if ctx.Err() != nil || install == nil || expected.owner != p || (!live && !stopped) ||
		expected.Generation == 0 || expected.Generation >= ^uint64(0)-1 ||
		expected.Intent == 0 || expected.Intent >= ^uint64(0)-1 ||
		(current != expected && !(live && current.ConfirmsUnexpectedExit(expected))) {
		p.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeChanged
	}
	advanceObservation(&p.lifecycleIntent)
	claim := p.lifecycleIntent
	p.wanted = false
	c := p.cur
	if c != nil {
		p.signalLocked(c, syscall.SIGTERM)
	}
	p.mu.Unlock()
	if c != nil {
		// Same unreaped-child signaling rule and bounded TERM/KILL waits as
		// Stop. Cancellation cannot authorize abandoning ownership confirmation.
		select {
		case <-c.done:
		case <-time.After(StopTimeouts[0]):
			p.mu.Lock()
			p.signalLocked(c, syscall.SIGKILL)
			p.mu.Unlock()
			select {
			case <-c.done:
			case <-time.After(StopTimeouts[1]):
				return RuntimeSnapshot{}, errors.New("XRAY_STOP_UNCONFIRMED: guarded change child not reaped")
			}
		}
	}
	// Reaping the leader does not prove that terminating descendants have
	// released the inherited ownership lock. Wait before changing any files;
	// the intent/generation checks below still reject intervening lifecycle work.
	if err := p.waitPreviousChild(ctx); err != nil {
		return RuntimeSnapshot{}, err
	}
	// wait closes done just before publishing cur=nil under mu. Do not race
	// that bookkeeping or change the established wait/signal ownership code.
	deadline := time.Now().Add(StopTimeouts[1])
	for {
		p.mu.Lock()
		if p.lifecycleIntent != claim || p.wanted || (p.cur != nil && p.cur != c) {
			p.mu.Unlock()
			return RuntimeSnapshot{}, ErrRuntimeChanged
		}
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return RuntimeSnapshot{}, err
		}
		if p.cur == nil {
			break // Keep mu held across the local install and optional launch.
		}
		p.mu.Unlock()
		if !time.Now().Before(deadline) {
			return RuntimeSnapshot{}, errors.New("XRAY_STOP_UNCONFIRMED: guarded change bookkeeping pending")
		}
		time.Sleep(time.Millisecond)
	}
	defer p.mu.Unlock()
	if !p.exitConfirmed || p.lastChildPID != expected.LastPID || p.generation != expected.Generation {
		return RuntimeSnapshot{}, ErrRuntimeChanged
	}
	if err := install(); err != nil {
		return RuntimeSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return RuntimeSnapshot{}, err
	}
	if !restart {
		return p.runtimeSnapshotLocked(), nil
	}
	p.wanted = true
	if err := p.launchLocked(); err != nil {
		p.wanted = false
		return RuntimeSnapshot{}, err
	}
	return p.runtimeSnapshotLocked(), nil
}

package xray

import (
	"context"
	"github.com/BROadmin/broray/modules/reserve"
)

// ColdStopped identifies a never-launched supervisor with stopped intent.
// It is only a classification; mutation additionally needs retained kernel
// exclusions and the durable stopped-profile decision.
func (s RuntimeSnapshot) ColdStopped() bool {
	return s.owner != nil && !s.Wanted && !s.Running && !s.ExitConfirmed && s.PID == 0 && s.LastPID == 0 && s.Generation == 0 && s.Intent < ^uint64(0)
}

// InstallIfCold performs only a bounded local install under the supervisor
// mutex. The caller retains the lease, daemon and transaction fence throughout
// its larger operation. install must not call Process methods. No lifecycle
// counters, desired intent, or fictional reap evidence are manufactured.
func (p *Process) InstallIfCold(ctx context.Context, expected RuntimeSnapshot, lease *reserve.StoppedProfileLease, install func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if install == nil || expected.owner != p || !expected.ColdStopped() || p.cur != nil || p.runtimeSnapshotLocked() != expected {
		return ErrRuntimeChanged
	}
	if err := lease.Check(ctx, p.root); err != nil {
		return err
	}
	if err := install(); err != nil {
		return err
	}
	return lease.Check(ctx, p.root)
}

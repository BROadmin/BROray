package servers

import (
	"context"

	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

type coldSourceKey struct{}
type coldSourceScope struct {
	process  *xray.Process
	snapshot xray.RuntimeSnapshot
	lease    *reserve.StoppedProfileLease
}

// A source owner keeps one exclusion through the nested transaction and the
// final public-state reconciliation. Nested operations may reuse only the
// exact Process observation and retained lease installed by this package.
func (s *svc) acquireColdSourceLease(ctx context.Context, process *xray.Process, snapshot xray.RuntimeSnapshot) (*reserve.StoppedProfileLease, func() error, error) {
	noop := func() error { return nil }
	if !snapshot.ColdStopped() || process == nil || process.RuntimeSnapshot() != snapshot {
		return nil, noop, errReserveRuntime
	}
	if scope, ok := ctx.Value(coldSourceKey{}).(*coldSourceScope); ok {
		if scope == nil || scope.process != process || scope.snapshot != snapshot || scope.lease.Check(ctx, s.d.Root) != nil {
			return nil, noop, errReserveRuntime
		}
		return scope.lease, noop, nil
	}
	lease, err := reserve.OpenStoppedProfile(ctx, s.d.Root, s.d.DaemonGuard)
	if err != nil {
		return nil, noop, err
	}
	return lease, lease.Close, nil
}

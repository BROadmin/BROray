package compat

import (
	"context"
	"errors"

	"github.com/BROadmin/broray/modules/reserve"
)

// ErrReserveTransactionPending also covers uncertain durability after a journal
// unlink. The absence of a visible journal alone cannot release that fence.
var ErrReserveTransactionPending = errors.New("RESERVE_TRANSACTION_RECOVERY_REQUIRED")

func reserveQueueAction(action string) bool {
	return action == ReserveModeAction || action == ReserveSwitchAction || action == SourceRemovalAction || action == SourceRefreshAction || action == DPIProfileAction
}

// Check both the in-process generation and the exact persisted owner. Never
// borrow an ordinary operation's fence or a replacement reserve operation.
func (g *GlobalLock) reserveQueueOwner(action string, token uint64) (uint64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reserveQueueOwnerLocked(action, token)
}

func (g *GlobalLock) reserveQueueOwnerLocked(action string, token uint64) (uint64, error) {
	if g.cur == nil || g.ambiguous || !g.daemonOwned || !g.cur.DaemonOwnedReserve ||
		!daemonReserveHolder(g.cur) || g.cur.Pending || g.cur.Action != action ||
		(token != 0 && g.cur.token != token) {
		return 0, errors.New("reserve queue ownership unavailable")
	}
	if err := g.ownerMatchesLocked(); err != nil {
		g.ambiguous = true
		return 0, err
	}
	return g.cur.token, nil
}

func (g *GlobalLock) releaseReserveQueueOwner(action string, token uint64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, err := g.reserveQueueOwnerLocked(action, token); err != nil {
		return err
	}
	if !g.releaseLocked() {
		return errors.New("reserve queue release unconfirmed")
	}
	return nil
}

func (c *Coordinator) reserveQueuePending(ctx context.Context) (bool, error) {
	// Cancellation must not skip the bounded local ownership check at the end
	// of a protected transaction. This performs no network or recovery work.
	if err := CheckSourceSyncWorkspaces(c.d.Root); err != nil {
		return false, err
	}
	if err := reserve.CheckProbeWorkspaces(c.d.Root); err != nil {
		return false, err
	}
	return reserve.TransactionPending(context.WithoutCancel(ctx), c.d.Root)
}

func (c *Coordinator) finishReserveQueue(ctx context.Context, action string, token uint64, stageErr error) error {
	pending, journalErr := c.reserveQueuePending(ctx)
	_, ownerErr := c.d.Lock.reserveQueueOwner(action, token)
	if pending || journalErr != nil || ownerErr != nil || errors.Is(stageErr, ErrReserveTransactionPending) {
		// MarkPending verifies the same persisted inode/hash before writing;
		// on failure the lock becomes ambiguous and Release cannot clear it.
		var retainErr error
		if ownerErr == nil {
			retainErr = c.d.Lock.MarkPending(false)
		}
		return errors.Join(ErrReserveTransactionPending, stageErr, journalErr, ownerErr, retainErr)
	}
	if err := c.d.Lock.releaseReserveQueueOwner(action, token); err != nil {
		return errors.Join(ErrReserveTransactionPending, stageErr, err)
	}
	return stageErr
}

// Refresh preparation stays cooperative. Only its final apply stage may own
// durable reserve transactions; the request target remains the source ID.
func validReserveQueueShape(r *Request, s ActionSpec) bool {
	if r.Action != SourceRefreshAction {
		return r.Target == ReserveOperationTarget && len(s.Stages) == 1 && s.Stages[0].Global && s.Stages[0].Protected
	}
	if !reQueueID.MatchString(r.Target) || len(s.Stages) != 3 {
		return false
	}
	for i, name := range []string{"fetch", "parse", "apply"} {
		st := s.Stages[i]
		final := i == 2
		if st.Name != name || st.Global != final || st.Protected != final {
			return false
		}
	}
	return true
}

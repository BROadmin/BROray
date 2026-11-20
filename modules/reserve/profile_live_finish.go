package reserve

import (
	"context"
	"strings"

	"github.com/BROadmin/broray/internal/platform"
)

func stoppedActiveProfile(r ProfileTransactionRecord) bool {
	return !r.WasRunning && r.Context.Active.Exists && strings.TrimSpace(string(r.Context.Active.Data)) == r.ServerID
}

// ConfirmStopped commits an installed stopped active profile while preserving
// durable stopped intent. The actual daemon and launch/child exclusions remain
// held through verification and cleanup. The verifier receives the requested
// decision (committed for apply, prepared for rollback), runs twice, and must
// neither start the main process nor reenter this journal.
func (j *ProfileJournal) ConfirmStopped(ctx context.Context, expected ProfileTransactionRecord, guard *platform.Lock, verify func(context.Context, ProfileTransactionRecord) error) error {
	return j.finishStopped(ctx, expected, guard, nil, verify, true)
}

func (j *ProfileJournal) RollbackStopped(ctx context.Context, expected ProfileTransactionRecord, guard *platform.Lock, verify func(context.Context, ProfileTransactionRecord) error) error {
	return j.finishStopped(ctx, expected, guard, nil, verify, false)
}

// Leased variants retain the cold caller's existing stopped ownership across
// install and finalization; they never close or replace the caller's lease.
func (j *ProfileJournal) ConfirmStoppedLeased(ctx context.Context, expected ProfileTransactionRecord, lease *StoppedProfileLease, verify func(context.Context, ProfileTransactionRecord) error) error {
	return j.finishStopped(ctx, expected, nil, lease, verify, true)
}
func (j *ProfileJournal) RollbackStoppedLeased(ctx context.Context, expected ProfileTransactionRecord, lease *StoppedProfileLease, verify func(context.Context, ProfileTransactionRecord) error) error {
	return j.finishStopped(ctx, expected, nil, lease, verify, false)
}

func (j *ProfileJournal) finishStopped(ctx context.Context, expected ProfileTransactionRecord, guard *platform.Lock, lease *StoppedProfileLease, verify func(context.Context, ProfileTransactionRecord) error, commit bool) error {
	expected = cloneProfileRecord(expected)
	action := func(proof *profileExclusion) error {
		r, owner, held, err := j.prepared(expected)
		if held != nil {
			defer held.Close()
		}
		if err != nil {
			return err
		}
		if verify == nil || !stoppedActiveProfile(r) {
			return ErrTransactionUnconfirmed
		}
		want := r.Previous
		decision := cloneProfileRecord(r)
		if commit {
			want = r.Next
			decision.Phase = "committed"
		}
		parent := j.base.dirs[j.base.root.Config()]
		bootImage, bootFile, err := openTransactionImage(parent, ProfileBootHoldFile, maxProfileBootHoldBytes)
		if bootFile != nil {
			defer bootFile.Close()
		}
		if err != nil {
			return err
		}
		if err = profileBootContext(r, bootImage, false); err != nil {
			return err
		}
		initialImage, initialFile := bootImage, bootFile
		proof.checkHold = func() error {
			if initialImage.Exists {
				if err := checkProfileBootHold(parent, initialImage, initialFile); err != nil {
					return err
				}
			}
			return checkProfileBootHold(parent, bootImage, bootFile)
		}
		if err = j.current(r, &want); err != nil {
			return err
		}
		if err = verify(ctx, cloneProfileRecord(decision)); err != nil {
			return err
		}
		if err = j.sameOwner(owner, held); err != nil {
			return err
		}
		if err = j.current(r, &want); err != nil {
			return err
		}
		if commit {
			// The durable accepted decision precedes its marker. Interrupted recovery
			// therefore preserves Next even if marker creation is interrupted.
			r.Phase = "committed"
			owner, held, err = j.persist(r, owner, held)
			if held != nil {
				defer held.Close()
			}
			if err != nil {
				return err
			}
		}
		bootImage, bootFile, err = j.preserveBootIntent(r, want, owner, held)
		if bootFile != nil {
			defer bootFile.Close()
		}
		if err != nil {
			return err
		}
		if err = j.sameOwner(owner, held); err != nil {
			return err
		}
		if err = j.currentWithBoot(r, &want, true); err != nil {
			return err
		}
		if err = verify(ctx, cloneProfileRecord(decision)); err != nil {
			return err
		}
		if err = j.sameOwner(owner, held); err != nil {
			return err
		}
		if err = j.currentWithBoot(r, &want, true); err != nil {
			return err
		}
		return j.clear(owner, held)
	}
	if lease != nil {
		return j.withProfileLease(ctx, lease, action)
	}
	return j.withProfileExclusion(ctx, guard, action)
}

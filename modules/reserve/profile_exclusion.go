package reserve

import (
	"context"
	"github.com/BROadmin/broray/internal/platform"
	"os"
	"path/filepath"
)

type profileExclusion struct{ checkHold func() error }

// Shared stopped-profile exclusion. All descriptors stay owned until action
// returns; the caller already retains the application mutation/recovery fence.
func (j *ProfileJournal) withProfileExclusion(ctx context.Context, guard *platform.Lock, action func(*profileExclusion) error) error {
	root := j.base.root
	if guard == nil || !guard.Owns(root.DaemonLock()) {
		return ErrTransactionUnconfirmed
	}
	return j.base.locked(ctx, func() error {
		bootParent := j.base.dirs[root.Config()]
		bootLock, err := reserveRecoveryGuard(bootParent, profileBootLockFile)
		if err != nil {
			return err
		}
		defer bootLock.Close()
		run, err := platform.OpenDirNoFollow(root.Run())
		if err != nil {
			return err
		}
		defer run.Close()
		exclusions := map[string]*os.File{}
		for _, name := range []string{"xray-child.lock", filepath.Base(root.InstallerLock()), filepath.Base(root.LauncherLock())} {
			file, err := reserveRecoveryGuard(run, name)
			if err != nil {
				return err
			}
			defer file.Close()
			exclusions[name] = file
		}
		proof := &profileExclusion{checkHold: func() error { return nil }}
		previousCheck := j.base.recoveryCheck
		j.base.recoveryCheck = func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := sameTransactionEntry(bootParent, profileBootLockFile, bootLock); err != nil {
				return err
			}
			if err := CheckProbeWorkspaces(root); err != nil {
				return err
			}
			if !guard.Owns(root.DaemonLock()) {
				return ErrTransactionUnconfirmed
			}
			visible, err := platform.OpenDirNoFollow(root.Run())
			if err != nil {
				return err
			}
			a, ea := run.Stat()
			b, eb := visible.Stat()
			visible.Close()
			if ea != nil || eb != nil || !os.SameFile(a, b) {
				return ErrTransactionUnconfirmed
			}
			for name, file := range exclusions {
				if err := sameTransactionEntry(run, name, file); err != nil {
					return err
				}
			}
			if previousCheck != nil {
				if err := previousCheck(); err != nil {
					return err
				}
			}
			return proof.checkHold()
		}
		defer func() { j.base.recoveryCheck = previousCheck }()
		if err := j.base.checkRoots(); err != nil {
			return err
		}
		return action(proof)
	})
}

// A cold transaction already owns these kernel exclusions. Borrow its exact
// retained lease rather than releasing/reacquiring it around finalization.
func (j *ProfileJournal) withProfileLease(ctx context.Context, lease *StoppedProfileLease, action func(*profileExclusion) error) error {
	if err := lease.Check(ctx, j.base.root); err != nil {
		return err
	}
	return j.base.locked(ctx, func() error {
		proof := &profileExclusion{checkHold: func() error { return nil }}
		previousCheck := j.base.recoveryCheck
		j.base.recoveryCheck = func() error {
			if err := lease.Check(ctx, j.base.root); err != nil {
				return err
			}
			if err := CheckProbeWorkspaces(j.base.root); err != nil {
				return err
			}
			if previousCheck != nil {
				if err := previousCheck(); err != nil {
					return err
				}
			}
			if err := proof.checkHold(); err != nil {
				return err
			}
			return lease.Check(ctx, j.base.root)
		}
		defer func() { j.base.recoveryCheck = previousCheck }()
		if err := j.base.checkRoots(); err != nil {
			return err
		}
		return action(proof)
	})
}

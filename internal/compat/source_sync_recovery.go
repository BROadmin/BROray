package compat

import (
	"errors"
	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strings"
)

var ErrSourceSyncRecoveryPending = errors.New("SOURCE_SYNC_RECOVERY_REQUIRED")

// CheckSourceSyncWorkspaces never reads marker contents or follows linked
// workspaces. A catalog rollback marker belongs to source recovery, not to the
// reserve journal replayer; an empty reserve journal cannot clear that owner.
func CheckSourceSyncWorkspaces(root platform.Root) (err error) {
	run, e := platform.OpenDirNoFollow(root.Run())
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return errors.Join(ErrSourceSyncRecoveryPending, e)
	}
	if e == nil {
		e = sourceSyncMarkerAt(run, "server-subscription.lock")
		ce := run.Close()
		if e != nil || ce != nil {
			return errors.Join(ErrSourceSyncRecoveryPending, e, ce)
		}
	}
	dir, e := platform.OpenDirNoFollow(root.Tmp())
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return errors.Join(ErrSourceSyncRecoveryPending, e)
	}
	defer func() {
		if e := dir.Close(); e != nil {
			err = errors.Join(ErrSourceSyncRecoveryPending, err, e)
		}
	}()
	for {
		entries, e := dir.ReadDir(128)
		if e != nil && !errors.Is(e, io.EOF) {
			return errors.Join(ErrSourceSyncRecoveryPending, e)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "server-subscription-sync.") {
				continue
			}
			work, we := platform.OpenDirAtNoFollow(dir, entry.Name())
			if we != nil {
				return errors.Join(ErrSourceSyncRecoveryPending, we)
			}
			me := sourceSyncMarkerAt(work, "rollback-required")
			ce := work.Close()
			if me != nil || ce != nil {
				return errors.Join(ErrSourceSyncRecoveryPending, me, ce)
			}
		}
		if errors.Is(e, io.EOF) {
			return nil
		}
	}
}
func sourceSyncMarkerAt(parent *os.File, name string) error {
	var st unix.Stat_t
	err := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return errors.Join(ErrSourceSyncRecoveryPending, err)
}

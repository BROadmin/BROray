package reserve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/BROadmin/broray/internal/platform"
)

// StoppedProfileLease excludes launch while a caller changes metadata for a
// cold supervisor. It retains an existing stopped decision, never creates or
// adopts one. The owner must retain the lease and daemon guard for the whole
// operation; Close must not run concurrently with that operation.
type StoppedProfileLease struct {
	mu       sync.Mutex
	root     platform.Root
	daemon   *platform.Lock
	dirs     map[string]*os.File
	locks    map[string]*os.File
	hold     TransactionImage
	holdFile *os.File
	closed   bool
}

func OpenStoppedProfile(ctx context.Context, root platform.Root, daemon *platform.Lock) (_ *StoppedProfileLease, retErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !daemon.Owns(root.DaemonLock()) {
		return nil, ErrTransactionUnconfirmed
	}
	l := &StoppedProfileLease{root: root, daemon: daemon, dirs: map[string]*os.File{}, locks: map[string]*os.File{}}
	defer func() {
		if retErr != nil {
			_ = l.Close()
		}
	}()
	for _, path := range []string{root.Config(), root.Run()} {
		f, err := platform.OpenDirNoFollow(path)
		if err != nil {
			return nil, err
		}
		l.dirs[path] = f
	}
	for _, path := range []string{filepath.Join(root.Config(), profileBootLockFile), filepath.Join(root.Run(), "xray-child.lock"), root.InstallerLock(), root.LauncherLock()} {
		f, err := reserveRecoveryGuard(l.dirs[filepath.Dir(path)], filepath.Base(path))
		if err != nil {
			return nil, err
		}
		l.locks[path] = f
	}
	var err error
	l.hold, l.holdFile, err = openTransactionImage(l.dirs[root.Config()], ProfileBootHoldFile, maxProfileBootHoldBytes)
	if err != nil {
		return nil, err
	}
	if !l.hold.Exists {
		return nil, ErrTransactionUnconfirmed
	}
	if _, err = decodeProfileBootHold(l.hold.Data); err != nil {
		return nil, err
	}
	if err = l.Check(ctx, root); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *StoppedProfileLease) Check(ctx context.Context, root platform.Root) error {
	if l == nil {
		return ErrTransactionUnconfirmed
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.closed || root.Dir != l.root.Dir || !l.daemon.Owns(root.DaemonLock()) {
		return ErrTransactionUnconfirmed
	}
	for path, held := range l.dirs {
		visible, err := platform.OpenDirNoFollow(path)
		if err != nil {
			return err
		}
		a, ea := held.Stat()
		b, eb := visible.Stat()
		visible.Close()
		if ea != nil || eb != nil || !os.SameFile(a, b) {
			return ErrTransactionUnconfirmed
		}
	}
	for path, held := range l.locks {
		if err := sameTransactionEntry(l.dirs[filepath.Dir(path)], filepath.Base(path), held); err != nil {
			return err
		}
	}
	return checkProfileBootHold(l.dirs[root.Config()], l.hold, l.holdFile)
}

// Close releases retained descriptors only. It never unlinks lock/hold files.
func (l *StoppedProfileLease) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	var result error
	if l.holdFile != nil {
		result = errors.Join(result, l.holdFile.Close())
	}
	for _, f := range l.locks {
		result = errors.Join(result, f.Close())
	}
	for _, f := range l.dirs {
		result = errors.Join(result, f.Close())
	}
	return result
}

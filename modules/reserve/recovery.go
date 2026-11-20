package reserve

import (
	"context"
	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// RestoreTransactionInterface proves and restores only the router description.
// Local receipt persistence belongs to RecoverTransaction's retained parents.
type RestoreTransactionInterface func(context.Context, TransactionRecord, []byte) error

func RecoverTransaction(ctx context.Context, root platform.Root, guard *platform.Lock, restore RestoreTransactionInterface) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !guard.Owns(root.DaemonLock()) {
		return ErrTransactionUnconfirmed
	}
	run, err := platform.OpenDirNoFollow(root.Run())
	if err != nil {
		return err
	}
	defer run.Close()
	exclusions := map[string]*os.File{}
	for _, name := range []string{"xray-child.lock", filepath.Base(root.InstallerLock()), filepath.Base(root.LauncherLock())} {
		f, err := reserveRecoveryGuard(run, name)
		if err != nil {
			return err
		}
		defer f.Close()
		exclusions[name] = f
	}
	check := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
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
		for name, f := range exclusions {
			if err := sameTransactionEntry(run, name, f); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	pending, err := TransactionPending(ctx, root)
	if err != nil {
		return err
	}
	if !pending {
		return check()
	}
	j, err := OpenTransactionJournal(root)
	if err != nil {
		return err
	}
	defer j.Close()
	j.recoveryCheck = check
	r, err := j.Read(ctx)
	if err != nil {
		return err
	}
	if r.Phase == "prepared" && restore == nil {
		return ErrTransactionUnconfirmed
	}
	return j.Rollback(ctx, r.ID, func(ctx context.Context, r TransactionRecord) error {
		if err := j.checkRoots(); err != nil {
			return err
		}
		parent := j.dirs[root.Config()]
		current, err := readTransactionImage(parent, "config.json", maxTransactionConfigBytes)
		if err != nil {
			return err
		}
		if !current.equal(r.Previous.Config) && !current.equal(r.Next.Config) {
			return ErrTransactionUnconfirmed
		}
		if err := replaceTransactionImage(parent, "config.json", r.Previous.Config, current, maxTransactionConfigBytes); err != nil {
			return err
		}
		if err := j.current(r.Previous); err != nil {
			return err
		}
		receiptParent := j.dirs[transactionMetadataDir(root)]
		receipt, err := readTransactionImage(receiptParent, "interface.json", maxTransactionReceiptBytes)
		if err != nil {
			return err
		}
		if !receipt.Exists {
			return ErrTransactionUnconfirmed
		}
		if err := restore(ctx, cloneTransactionRecord(r), append([]byte(nil), receipt.Data...)); err != nil {
			return err
		}
		if err := j.checkRoots(); err != nil {
			return err
		}
		if err := replaceTransactionImage(receiptParent, "interface.json", r.Receipt, receipt, maxTransactionReceiptBytes); err != nil {
			return err
		}
		after, err := readTransactionImage(receiptParent, "interface.json", maxTransactionReceiptBytes)
		if err != nil {
			return err
		}
		if !after.equal(r.Receipt) {
			return ErrTransactionUnconfirmed
		}
		return j.checkRoots()
	})
}

func reserveRecoveryGuard(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, ErrTransactionUnconfirmed
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	if err := sameTransactionEntry(parent, name, f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

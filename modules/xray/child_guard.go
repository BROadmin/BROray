package xray

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"time"
)

// The child inherits this open-file-description lock. Closing only the
// parent's descriptor keeps it held until the executable really exits.
func (p *Process) acquireChildGuard() (*os.File, error) {
	path := filepath.Join(p.root.Run(), "xray-child.lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("XRAY_CHILD_OWNER_INVALID")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (p *Process) waitPreviousChild(ctx context.Context) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		guard, err := p.acquireChildGuard()
		if err == nil {
			return guard.Close()
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return errors.New("XRAY_CHILD_OWNER_UNCONFIRMED")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("XRAY_PREVIOUS_CHILD_STILL_RUNNING")
		case <-tick.C:
		}
	}
}

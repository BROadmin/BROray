package geodata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

type Snapshot struct {
	Revision         string    `json:"revision"`
	PreviousRevision string    `json:"previousRevision"`
	Generation       string    `json:"generation"`
	Selection        Selection `json:"selection"`
	Assets           []Asset   `json:"assets"`
}

type Fetch func(context.Context, Source) ([]byte, error)

// CoreValidator tests the actual generated configuration against staged files.
// It must not call Store methods: Prepare retains the store lock.
type CoreValidator func(context.Context, Snapshot) error
type Store struct {
	root    string
	dir     *os.File
	mu      sync.Mutex
	pending map[*Candidate]bool
}
type Candidate struct {
	next  Snapshot
	base  string
	owner *Store
	dir   *os.File
	used  bool
}

var ErrStale = errors.New("geodata revision changed")
var ErrCommitUncertain = errors.New("geodata commit durability unknown; read back state")

func Open(root string) (*Store, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dir, err := platform.OpenDirNoFollow(root)
	if err != nil {
		return nil, err
	}
	return &Store{root: root, dir: dir, pending: map[*Candidate]bool{}}, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for candidate := range s.pending {
		if candidate.dir != nil {
			candidate.dir.Close()
			candidate.dir = nil
		}
	}
	s.pending = nil
	if s.dir == nil {
		return nil
	}
	err := s.dir.Close()
	s.dir = nil
	return err
}

func (s *Store) checkRoot() error {
	if s.dir == nil {
		return errors.New("geodata store closed")
	}
	actual, err := platform.OpenDirNoFollow(s.root)
	if err != nil {
		return err
	}
	defer actual.Close()
	a, err := s.dir.Stat()
	if err != nil {
		return err
	}
	b, err := actual.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(a, b) {
		return errors.New("geodata root changed")
	}
	return nil
}

// This kernel descriptor lock does not replace the application mutation fence.
// Process death releases it; PID and file age are not ownership evidence.
func (s *Store) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkRoot(); err != nil {
		return err
	}
	return unix.Flock(int(s.dir.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
func (s *Store) unlock() { _ = unix.Flock(int(s.dir.Fd()), unix.LOCK_UN) }

func (s *Store) Load(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return Snapshot{}, err
	}
	defer s.unlock()
	return s.load(ctx)
}

// RecoveryHead verifies pointer metadata and managed source identities only.
// It must not be used to approve a config or activate assets. Rollback verifies
// the complete predecessor before restoring it; Load verifies active payloads.
func (s *Store) RecoveryHead(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return Snapshot{}, err
	}
	defer s.unlock()
	return s.loadMetadata(ctx)
}

func (c *Candidate) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	return cloneSnapshot(c.next)
}

package geodata

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

const pendingUpdateFile = "pending-update.v1.json"

var ErrPendingChanged = errors.New("pending geodata update changed")

type PendingUpdate struct {
	Revision       string    `json:"revision"`
	Generation     string    `json:"generation"`
	PolicyRevision string    `json:"policyRevision"`
	BaseRevision   string    `json:"baseRevision"`
	CheckedAt      time.Time `json:"checkedAt"`
}

func (p PendingUpdate) valid() bool {
	return hashPattern.MatchString(p.Revision) && generationPattern.MatchString(p.Generation) &&
		hashPattern.MatchString(p.PolicyRevision) && (p.BaseRevision == "" || hashPattern.MatchString(p.BaseRevision)) &&
		p.BaseRevision != p.Revision && !p.CheckedAt.IsZero()
}

func (s *Store) readPending() (*PendingUpdate, error) {
	data, err := readBounded(s.dir, pendingUpdateFile, 4096)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pending PendingUpdate
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(pending)
	if err != nil || !pending.valid() || !bytes.Equal(data, canonical) {
		return nil, errors.New("invalid pending geodata metadata")
	}
	return &pending, nil
}

// Pending returns a display receipt for the supplied current policy. ReopenPending
// must verify the generation and its files before they can be applied.
func (s *Store) Pending(ctx context.Context, policy, base string) (*PendingUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	pending, err := s.readPending()
	if err != nil || pending == nil {
		return nil, err
	}
	if pending.PolicyRevision != policy || pending.BaseRevision != base {
		return nil, nil
	}
	return pending, s.checkRoot()
}

// StagePending publishes only the pending receipt. A possibly durable receipt
// must retain its files even if its caller receives an uncertain outcome.
func (s *Store) StagePending(ctx context.Context, candidate *Candidate, policy string, at time.Time) (*PendingUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	if candidate == nil || candidate.owner != s || candidate.used || !s.pending[candidate] || candidate.dir == nil {
		return nil, errors.New("pending candidate not owned")
	}
	current, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if current.Revision != candidate.base || current.Generation == candidate.next.Generation {
		return nil, ErrStale
	}
	if err := s.verifyCandidateDirectory(candidate); err != nil {
		return nil, err
	}
	pending := PendingUpdate{
		Revision: candidate.next.Revision, Generation: candidate.next.Generation,
		PolicyRevision: policy, BaseRevision: current.Revision, CheckedAt: at.UTC(),
	}
	if !pending.valid() {
		return nil, errors.New("invalid pending geodata identity")
	}
	if err := s.verify(ctx, candidate.next); err != nil {
		return nil, err
	}
	old, err := s.readPending()
	if err != nil {
		return nil, err
	}
	var retired *Candidate
	if old != nil && old.BaseRevision == current.Revision && old.Generation != pending.Generation && old.Generation != current.Generation {
		// Only a fully verified, unpublished predecessor of this receipt may
		// be retired. Leave damaged or ambiguous directories in place.
		retired, _ = s.reopenCandidate(ctx, *old, current)
	}
	if retired != nil {
		defer retired.dir.Close()
	}
	data, err := json.Marshal(pending)
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	temp := ".pending-update-" + hex.EncodeToString(nonce[:]) + ".json"
	if err := platform.WriteNewRegularAtNoFollow(s.dir, temp, data, 0600); err != nil {
		return nil, err
	}
	defer unix.Unlinkat(int(s.dir.Fd()), temp, 0)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.checkRoot(); err != nil {
		return nil, err
	}
	if err := unix.Renameat(int(s.dir.Fd()), temp, int(s.dir.Fd()), pendingUpdateFile); err != nil {
		return nil, err
	}
	if err := s.dir.Sync(); err != nil {
		return nil, fmt.Errorf("%w: pending update: %v", ErrCommitUncertain, err)
	}
	if retired != nil {
		if err := platform.RemoveOwnedDirTreeAtNoFollow(s.dir, retired.next.Generation, retired.dir); err != nil {
			return &pending, fmt.Errorf("pending update saved; previous generation cleanup: %w", err)
		}
	}
	return &pending, nil
}

func (s *Store) ReopenPending(ctx context.Context, policy, expected string) (*Candidate, PendingUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return nil, PendingUpdate{}, err
	}
	defer s.unlock()
	pending, err := s.readPending()
	if err != nil {
		return nil, PendingUpdate{}, err
	}
	if pending == nil || pending.PolicyRevision != policy || pending.Revision != expected {
		return nil, PendingUpdate{}, ErrPendingChanged
	}
	current, err := s.load(ctx)
	if err != nil {
		return nil, PendingUpdate{}, err
	}
	if pending.BaseRevision != current.Revision {
		return nil, PendingUpdate{}, ErrPendingChanged
	}
	candidate, err := s.reopenCandidate(ctx, *pending, current)
	if err != nil {
		return nil, PendingUpdate{}, err
	}
	s.pending[candidate] = true
	return candidate, *pending, nil
}

func (s *Store) reopenCandidate(ctx context.Context, pending PendingUpdate, current Snapshot) (*Candidate, error) {
	dir, err := platform.OpenDirAtNoFollow(s.dir, pending.Generation)
	if err != nil {
		return nil, err
	}
	retain := false
	defer func() {
		if !retain {
			dir.Close()
		}
	}()
	data, err := readBounded(dir, "snapshot.json", 1<<20)
	if err != nil {
		return nil, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Generation != pending.Generation || snapshot.Revision != pending.Revision || snapshot.PreviousRevision != current.Revision {
		return nil, ErrPendingChanged
	}
	prior, err := readBounded(dir, "previous.json", 1<<20)
	if err != nil {
		return nil, err
	}
	expected, err := json.Marshal(current)
	if err != nil || !bytes.Equal(prior, expected) {
		return nil, ErrPendingChanged
	}
	if err := s.verify(ctx, snapshot); err != nil {
		return nil, err
	}
	candidate := &Candidate{next: snapshot, base: current.Revision, owner: s, dir: dir}
	if err := s.verifyCandidateDirectory(candidate); err != nil {
		return nil, err
	}
	retain = true
	return candidate, nil
}

// ClearAppliedPending removes the exact receipt after the policy journal has
// confirmed the new working state. A later receipt is left alone.
func (s *Store) ClearAppliedPending(ctx context.Context, revision string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	pending, err := s.readPending()
	if err != nil || pending == nil || pending.Revision != revision {
		return err
	}
	current, err := s.loadMetadata(ctx)
	if err != nil {
		return err
	}
	if current.Revision != revision {
		return ErrPendingChanged
	}
	return s.removePending(ctx)
}

// InvalidatePending withdraws a receipt after a verified check found the
// working data still current. The caller holds the policy mutation fence.
func (s *Store) InvalidatePending(ctx context.Context, policy, base string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	pending, err := s.readPending()
	if err != nil || pending == nil || pending.PolicyRevision != policy || pending.BaseRevision != base {
		return err
	}
	current, err := s.load(ctx)
	if err != nil {
		return err
	}
	if current.Revision != base {
		return ErrPendingChanged
	}
	retired, _ := s.reopenCandidate(ctx, *pending, current)
	if retired != nil {
		defer retired.dir.Close()
	}
	if err := s.removePending(ctx); err != nil {
		return err
	}
	if retired != nil {
		return platform.RemoveOwnedDirTreeAtNoFollow(s.dir, retired.next.Generation, retired.dir)
	}
	return nil
}

func (s *Store) removePending(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkRoot(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(s.dir.Fd()), pendingUpdateFile, 0); err != nil {
		return err
	}
	return s.dir.Sync()
}

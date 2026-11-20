package geodata

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

func (s *Store) Prepare(ctx context.Context, selection Selection, revisions Revisions, fetch Fetch, validate CoreValidator) (candidate *Candidate, err error) {
	return s.prepare(ctx, selection, revisions, fetch, validate, false)
}

func (s *Store) prepare(ctx context.Context, selection Selection, revisions Revisions, fetch Fetch, validate CoreValidator, reuse bool) (candidate *Candidate, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	if fetch == nil || validate == nil {
		return nil, errors.New("fetch and actual-core validator required")
	}
	plan, err := SourcePlan(selection, revisions)
	if err != nil {
		return nil, err
	}
	previous, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	countries, _ := normalizeTags(selection.CountryCodes, true)
	tags, _ := normalizeTags(selection.GeoSiteTags, false)
	name, err := platform.MkdirTempAtNoFollow(s.dir, "gen-", 0700)
	if err != nil {
		return nil, err
	}
	dir, err := platform.OpenDirAtNoFollow(s.dir, name)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			cleanup := platform.RemoveOwnedDirTreeAtNoFollow(s.dir, name, dir)
			dir.Close()
			if cleanup != nil {
				err = errors.Join(err, cleanup)
			}
		}
	}()
	next := Snapshot{Generation: name, Selection: Selection{CountryCodes: countries, GeoSiteTags: tags}, Assets: []Asset{}}
	next.PreviousRevision = previous.Revision
	for _, source := range plan {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		var data []byte
		var reused bool
		var fetchErr error
		if reuse {
			data, reused, fetchErr = s.reuse(previous, source)
		}
		if fetchErr == nil && !reused {
			data, fetchErr = fetch(ctx, source)
		}
		if fetchErr != nil {
			return nil, fetchErr
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if err = Validate(source.Kind, data, source.Tags); err != nil {
			return nil, fmt.Errorf("%s: %w", source.Name, err)
		}
		if err = platform.WriteNewRegularAtNoFollow(dir, source.Name, data, 0600); err != nil {
			return nil, err
		}
		next.Assets = append(next.Assets, Asset{Source: source, Path: filepath.Join(name, source.Name), SHA256: digest(data), Bytes: int64(len(data))})
	}
	next.Revision = snapshotRevision(next)
	if err = s.verify(ctx, next); err != nil {
		return nil, err
	}
	if err = validate(ctx, cloneSnapshot(next)); err != nil {
		return nil, err
	}
	if err = s.verify(ctx, next); err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	if err = platform.WriteNewRegularAtNoFollow(dir, "snapshot.json", metadata, 0600); err != nil {
		return nil, err
	}
	prior, err := json.Marshal(previous)
	if err != nil {
		return nil, err
	}
	if err = platform.WriteNewRegularAtNoFollow(dir, "previous.json", prior, 0600); err != nil {
		return nil, err
	}
	candidate = &Candidate{next: next, base: previous.Revision, owner: s, dir: dir}
	s.pending[candidate] = true
	keep = true
	return candidate, nil
}

// Rollback restores a recorded, validated predecessor, including after restart.
// Application callers must separately restore the Xray configuration under the
// global mutation fence; this method changes only the geodata manifest.
func (s *Store) Rollback(ctx context.Context, expectedRevision string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return Snapshot{}, err
	}
	defer s.unlock()
	current, err := s.loadMetadata(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if current.Revision != expectedRevision {
		return Snapshot{}, ErrStale
	}
	if current.Revision == "" {
		return Snapshot{}, errors.New("no geodata activation to roll back")
	}
	dir, err := platform.OpenDirAtNoFollow(s.dir, current.Generation)
	if err != nil {
		return Snapshot{}, err
	}
	data, err := readBounded(dir, "previous.json", 1<<20)
	dir.Close()
	if err != nil {
		return Snapshot{}, err
	}
	var previous Snapshot
	if err := json.Unmarshal(data, &previous); err != nil {
		return Snapshot{}, err
	}
	if previous.Revision != current.PreviousRevision {
		return Snapshot{}, errors.New("predecessor revision mismatch")
	}
	if previous.Revision != "" {
		if err := s.verify(ctx, previous); err != nil {
			return Snapshot{}, err
		}
	} else if previous.Generation != "" || previous.PreviousRevision != "" || len(previous.Assets) != 0 || len(previous.Selection.CountryCodes) != 0 || len(previous.Selection.GeoSiteTags) != 0 {
		return Snapshot{}, errors.New("invalid empty predecessor")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := s.checkRoot(); err != nil {
		return Snapshot{}, err
	}
	if previous.Revision == "" {
		if err := unix.Unlinkat(int(s.dir.Fd()), "current.json", 0); err != nil {
			return Snapshot{}, err
		}
	} else {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return Snapshot{}, err
		}
		temp := ".rollback-" + hex.EncodeToString(nonce[:]) + ".json"
		if err := platform.WriteNewRegularAtNoFollow(s.dir, temp, data, 0600); err != nil {
			return Snapshot{}, err
		}
		defer unix.Unlinkat(int(s.dir.Fd()), temp, 0)
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		if err := s.checkRoot(); err != nil {
			return Snapshot{}, err
		}
		if err := unix.Renameat(int(s.dir.Fd()), temp, int(s.dir.Fd()), "current.json"); err != nil {
			return Snapshot{}, err
		}
	}
	if err := s.dir.Sync(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrCommitUncertain, err)
	}
	return previous, nil
}

func (s *Store) Commit(ctx context.Context, candidate *Candidate, expectedRevision string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return Snapshot{}, err
	}
	defer s.unlock()
	if candidate == nil || candidate.owner != s || candidate.used || !s.pending[candidate] || candidate.dir == nil {
		return Snapshot{}, errors.New("candidate not owned or already consumed")
	}
	current, err := s.load(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if current.Revision != expectedRevision || current.Revision != candidate.base {
		return Snapshot{}, ErrStale
	}
	dir, err := platform.OpenDirAtNoFollow(s.dir, candidate.next.Generation)
	if err != nil {
		return Snapshot{}, err
	}
	a, aErr := candidate.dir.Stat()
	b, bErr := dir.Stat()
	dir.Close()
	if aErr != nil || bErr != nil || !os.SameFile(a, b) {
		return Snapshot{}, errors.New("candidate directory replaced")
	}
	if err := s.verify(ctx, candidate.next); err != nil {
		return Snapshot{}, err
	}
	metadata, err := json.Marshal(candidate.next)
	if err != nil {
		return Snapshot{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Snapshot{}, err
	}
	temp := ".current-" + hex.EncodeToString(nonce[:]) + ".json"
	if err := platform.WriteNewRegularAtNoFollow(s.dir, temp, metadata, 0600); err != nil {
		return Snapshot{}, err
	}
	defer unix.Unlinkat(int(s.dir.Fd()), temp, 0)
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := s.checkRoot(); err != nil {
		return Snapshot{}, err
	}
	if err := unix.Renameat(int(s.dir.Fd()), temp, int(s.dir.Fd()), "current.json"); err != nil {
		return Snapshot{}, err
	}
	candidate.used = true
	candidate.dir.Close()
	candidate.dir = nil
	delete(s.pending, candidate)
	if err := s.dir.Sync(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrCommitUncertain, err)
	}
	return cloneSnapshot(candidate.next), nil
}

// Abort removes only an owned, never-committed candidate. Old working
// generations remain available for offline startup and application rollback.
func (s *Store) Abort(ctx context.Context, candidate *Candidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	if candidate == nil || candidate.owner != s || candidate.used || !s.pending[candidate] || candidate.dir == nil {
		return errors.New("candidate not owned")
	}
	current, err := s.load(ctx)
	if err != nil {
		return err
	}
	if current.Generation == candidate.next.Generation {
		return errors.New("candidate is active")
	}
	if err := platform.RemoveOwnedDirTreeAtNoFollow(s.dir, candidate.next.Generation, candidate.dir); err != nil {
		return err
	}
	candidate.dir.Close()
	candidate.dir = nil
	candidate.used = true
	delete(s.pending, candidate)
	return nil
}

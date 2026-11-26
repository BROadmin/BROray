package geodata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

// Duplicate identifies a planned replacement of a regular geodata file with
// an existing, byte-identical file. Path is relative to the geodata store.
type Duplicate struct {
	Path     string
	Identity string
	Bytes    int64
}

func (s *Store) history(ctx context.Context, head Snapshot) ([]Snapshot, error) {
	var history []Snapshot
	seen := make(map[string]bool)
	for head.Revision != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := snapshotPlan(head); err != nil {
			return nil, err
		}
		if seen[head.Generation] {
			return nil, errors.New("geodata predecessor cycle")
		}
		seen[head.Generation] = true
		history = append(history, head)
		dir, err := platform.OpenDirAtNoFollow(s.dir, head.Generation)
		if err != nil {
			return nil, err
		}
		data, err := readBounded(dir, "previous.json", 1<<20)
		dir.Close()
		if err != nil {
			return nil, err
		}
		var previous Snapshot
		if json.Unmarshal(data, &previous) != nil || previous.Revision != head.PreviousRevision ||
			(previous.Revision == "" && (previous.Generation != "" || previous.PreviousRevision != "" ||
				len(previous.Assets)+len(previous.Selection.CountryCodes)+len(previous.Selection.GeoSiteTags) != 0)) {
			return nil, errors.New("invalid geodata predecessor")
		}
		head = previous
	}
	return history, nil
}

func (s *Store) assetParent(asset Asset) (*os.File, error) {
	return platform.OpenDirAtNoFollow(s.dir, filepath.Dir(asset.Path))
}

func (s *Store) assetParentMatches(asset Asset, held *os.File) bool {
	current, err := s.assetParent(asset)
	if err != nil {
		return false
	}
	defer current.Close()
	old, oldErr := held.Stat()
	info, newErr := current.Stat()
	return oldErr == nil && newErr == nil && os.SameFile(old, info)
}

func assetStat(dir *os.File, asset Asset) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), asset.Name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return st, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size != asset.Bytes {
		return st, errors.New("geodata member is not the expected regular file")
	}
	return st, nil
}

func sameAssetFile(a, b unix.Stat_t) bool { return a.Dev == b.Dev && a.Ino == b.Ino }

func unchangedAsset(a, b unix.Stat_t) bool {
	return sameAssetFile(a, b) && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Size == b.Size && a.Mtim == b.Mtim
}

func assetIdentity(st unix.Stat_t) string {
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%v:%v:%d", st.Dev, st.Ino, st.Mode, st.Uid, st.Gid, st.Size, st.Mtim, st.Ctim, st.Nlink)
}

func checkedAsset(dir *os.File, asset Asset) (unix.Stat_t, error) {
	before, err := assetStat(dir, asset)
	if err != nil {
		return before, err
	}
	data, err := readBounded(dir, asset.Name, MaxAssetBytes)
	if err != nil {
		return before, err
	}
	after, err := assetStat(dir, asset)
	if err != nil || !unchangedAsset(before, after) || before.Ctim != after.Ctim || int64(len(data)) != asset.Bytes || digest(data) != asset.SHA256 {
		return after, errors.New("geodata checksum or identity changed")
	}
	return after, nil
}

type mergePair struct {
	Duplicate
	source, target         Asset
	sourceStat, targetStat unix.Stat_t
}

type storedAsset struct {
	asset Asset
	stat  unix.Stat_t
}

// Only a committed generation may share an inode. An independently staged
// candidate is still writable and must never expose the active file to writes.
func (s *Store) shareCommittedAssets(ctx context.Context, previous, next Snapshot) {
	history, err := s.history(ctx, previous)
	if err != nil {
		return
	}
	dir, err := platform.OpenDirAtNoFollow(s.dir, next.Generation)
	if err != nil {
		return
	}
	defer dir.Close()
	for _, asset := range next.Assets {
		if ctx.Err() != nil {
			return
		}
		target, err := assetStat(dir, asset)
		if err != nil {
			continue
		}
		if source, ok := s.findMatchingAsset(ctx, history, asset, target); ok && !sameAssetFile(source.stat, target) {
			// Failure leaves a valid independent copy for the cleanup button.
			_ = s.merge(mergePair{source: source.asset, target: asset, sourceStat: source.stat, targetStat: target})
		}
	}
}

func (s *Store) findMatchingAsset(ctx context.Context, history []Snapshot, asset Asset, target unix.Stat_t) (storedAsset, bool) {
	for _, snapshot := range history {
		if ctx.Err() != nil {
			break
		}
		for _, existing := range snapshot.Assets {
			if existing.Name != asset.Name || existing.SHA256 != asset.SHA256 || existing.Bytes != asset.Bytes {
				continue
			}
			dir, err := s.assetParent(existing)
			if err != nil {
				continue
			}
			st, err := checkedAsset(dir, existing)
			dir.Close()
			if err == nil && st.Dev == target.Dev && st.Mode == target.Mode && st.Uid == target.Uid && st.Gid == target.Gid {
				return storedAsset{existing, st}, true
			}
		}
	}
	return storedAsset{}, false
}

func (s *Store) duplicates(ctx context.Context) ([]mergePair, error) {
	current, err := s.loadMetadata(ctx)
	if err != nil {
		return nil, err
	}
	history, err := s.history(ctx, current)
	if err != nil {
		return nil, err
	}
	known := make(map[string]storedAsset)
	var pairs []mergePair
	for _, snapshot := range history {
		dir, err := platform.OpenDirAtNoFollow(s.dir, snapshot.Generation)
		if err != nil {
			return nil, err
		}
		for _, asset := range snapshot.Assets {
			if err := ctx.Err(); err != nil {
				dir.Close()
				return nil, err
			}
			st, err := checkedAsset(dir, asset)
			if err != nil {
				dir.Close()
				return nil, err
			}
			key := fmt.Sprint(asset.Name, ":", asset.SHA256, ":", st.Dev, ":", st.Mode, ":", st.Uid, ":", st.Gid)
			first, exists := known[key]
			if !exists {
				known[key] = storedAsset{asset, st}
				continue
			}
			if sameAssetFile(first.stat, st) {
				continue
			}
			// Multiple existing names may keep an inode allocated after replacement.
			size := int64(0)
			if st.Nlink == 1 {
				size = asset.Bytes
			}
			id := digest([]byte(fmt.Sprintf("%s:%s:%s:%s:%s", current.Revision, first.asset.Path, asset.SHA256,
				assetIdentity(first.stat), assetIdentity(st))))
			pairs = append(pairs, mergePair{Duplicate{asset.Path, id, size}, first.asset, asset, first.stat, st})
		}
		dir.Close()
	}
	return pairs, nil
}

// DuplicatePlan reads the committed predecessor chain; unreferenced directories
// and portable/settings backup files are outside this operation.
func (s *Store) DuplicatePlan(ctx context.Context) ([]Duplicate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	pairs, err := s.duplicates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Duplicate, len(pairs))
	for i, pair := range pairs {
		out[i] = pair.Duplicate
	}
	return out, nil
}

// Consolidate applies only the previously confirmed plan. The caller holds the
// application mutation fence; the store lock excludes concurrent geodata IO.
func (s *Store) Consolidate(ctx context.Context, expected map[string]string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return 0, err
	}
	defer s.unlock()
	pairs, err := s.duplicates(ctx)
	if err != nil {
		return 0, err
	}
	if len(pairs) != len(expected) {
		return 0, ErrStale
	}
	for _, pair := range pairs {
		if expected[pair.Path] != pair.Identity {
			return 0, ErrStale
		}
	}
	for i, pair := range pairs {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := s.merge(pair); err != nil {
			return i, err
		}
	}
	return len(pairs), nil
}

func (s *Store) merge(pair mergePair) (err error) {
	source, err := s.assetParent(pair.source)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := s.assetParent(pair.target)
	if err != nil {
		return err
	}
	defer target.Close()
	from, err := checkedAsset(source, pair.source)
	if err != nil || !unchangedAsset(from, pair.sourceStat) {
		return ErrStale
	}
	to, err := checkedAsset(target, pair.target)
	if err != nil || !unchangedAsset(to, pair.targetStat) {
		return ErrStale
	}
	name, err := platform.MkdirTempAtNoFollow(target, ".dedup-", 0700)
	if err != nil {
		return err
	}
	tmp, err := platform.OpenDirAtNoFollow(target, name)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, platform.RemoveOwnedDirTreeAtNoFollow(target, name, tmp), tmp.Close())
	}()
	if err := unix.Linkat(int(source.Fd()), pair.source.Name, int(tmp.Fd()), pair.target.Name, 0); err != nil {
		return err
	}
	if linked, err := checkedAsset(tmp, pair.target); err != nil || !unchangedAsset(linked, from) {
		return ErrStale
	}
	if now, err := assetStat(target, pair.target); err != nil || !unchangedAsset(now, to) {
		return ErrStale
	}
	if err := s.checkRoot(); err != nil {
		return err
	}
	if !s.assetParentMatches(pair.source, source) || !s.assetParentMatches(pair.target, target) {
		return ErrStale
	}
	if err := unix.Renameat(int(tmp.Fd()), pair.target.Name, int(target.Fd()), pair.target.Name); err != nil {
		return err
	}
	return target.Sync()
}

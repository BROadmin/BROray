package geodata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/BROadmin/broray/internal/platform"
)

type entryKey struct {
	kind Kind
	tag  string
}

// SameUsedData compares complete native entries for the given working rules.
// Bytes outside those entries, including other categories, cannot affect the
// result. Differences within an entry are treated conservatively as changes.
func (s *Store) SameUsedData(ctx context.Context, candidate *Candidate, used Selection) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return false, err
	}
	defer s.unlock()
	if candidate == nil || candidate.owner != s || candidate.used || !s.pending[candidate] || candidate.dir == nil {
		return false, errors.New("candidate not owned or already consumed")
	}
	current, err := s.load(ctx)
	if err != nil {
		return false, err
	}
	if current.Revision == "" || current.Revision != candidate.base {
		return false, ErrStale
	}
	if !reflect.DeepEqual(current.Selection, candidate.next.Selection) {
		return false, nil
	}
	if err := s.verifyCandidateDirectory(candidate); err != nil {
		return false, err
	}
	countries, err := normalizeTags(used.CountryCodes, true)
	if err != nil {
		return false, err
	}
	tags, err := normalizeTags(used.GeoSiteTags, false)
	if err != nil {
		return false, err
	}
	needed := make(map[entryKey]bool, len(countries)+len(tags))
	for _, tag := range countries {
		needed[entryKey{GeoIP, tag}] = true
	}
	for _, tag := range tags {
		needed[entryKey{GeoSite, tag}] = true
	}
	if len(needed) == 0 {
		return false, nil
	}
	previous, err := platform.OpenDirAtNoFollow(s.dir, current.Generation)
	if err != nil {
		return false, err
	}
	defer previous.Close()
	before, err := usedEntryHashes(ctx, previous, current.Assets, needed)
	if err != nil {
		return false, err
	}
	after, err := usedEntryHashes(ctx, candidate.dir, candidate.next.Assets, needed)
	if err != nil {
		return false, err
	}
	if err := s.checkRoot(); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return reflect.DeepEqual(before, after), nil
}

func usedEntryHashes(ctx context.Context, dir *os.File, assets []Asset, needed map[entryKey]bool) (map[entryKey]string, error) {
	hashes := make(map[entryKey]string, len(needed))
	for _, asset := range assets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tags := make(map[string]bool, len(asset.Tags))
		for _, tag := range asset.Tags {
			if needed[entryKey{asset.Kind, tag}] {
				tags[tag] = true
			}
		}
		if len(tags) == 0 {
			continue
		}
		data, err := readVerifiedAsset(dir, asset)
		if err != nil {
			return nil, err
		}
		if err := walk(data, func(entry wireField) error {
			if entry.number != 1 {
				return nil
			}
			if entry.wire != 2 {
				return errWire
			}
			// The file hash binds these bytes to the validated snapshot.
			// Scan fields for the tag without decoding each domain or CIDR.
			var tag string
			if err := walk(entry.data, func(field wireField) error {
				if field.number == 1 {
					tag = strings.ToLower(string(field.data))
				}
				return nil
			}); err != nil {
				return err
			}
			if tags[tag] {
				key := entryKey{asset.Kind, tag}
				if _, duplicate := hashes[key]; duplicate {
					return fmt.Errorf("duplicate working geodata entry %s", tag)
				}
				hashes[key] = digest(entry.data)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if len(hashes) != len(needed) {
		return nil, errors.New("working geodata entry missing")
	}
	return hashes, nil
}

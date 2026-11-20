package geodata

import (
	"context"
	"errors"

	"github.com/BROadmin/broray/internal/platform"
)

// PrepareReusing keeps unchanged pinned sources local during a per-kind update.
func (s *Store) PrepareReusing(ctx context.Context, selection Selection, revisions Revisions, fetch Fetch, validate CoreValidator) (*Candidate, error) {
	return s.prepare(ctx, selection, revisions, fetch, validate, true)
}

// Called only while prepare holds the store lock. Match the immutable payload
// identity, then validate the newly requested tags before constructing metadata.
func (s *Store) reuse(previous Snapshot, source Source) ([]byte, bool, error) {
	for _, asset := range previous.Assets {
		if asset.Kind != source.Kind || asset.Revision != source.Revision || asset.Name != source.Name || asset.URL != source.URL {
			continue
		}
		dir, err := platform.OpenDirAtNoFollow(s.dir, previous.Generation)
		if err != nil {
			return nil, false, err
		}
		data, err := readBounded(dir, source.Name, MaxAssetBytes)
		dir.Close()
		if err != nil {
			return nil, false, err
		}
		if int64(len(data)) != asset.Bytes || digest(data) != asset.SHA256 {
			return nil, false, errors.New("cached geodata changed")
		}
		if err := s.checkRoot(); err != nil {
			return nil, false, err
		}
		return data, true, nil
	}
	return nil, false, nil
}

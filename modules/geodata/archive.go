package geodata

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
)

const maxArchiveBytes = 2 << 20
const maxArchiveDecodedBytes = 64 << 20

// An archive records exact native payloads, never a host path to copy from.
// Imported generations are newly prepared and checked by the actual core.
type portableArchive struct {
	SchemaVersion int               `json:"schemaVersion"`
	Snapshot      Snapshot          `json:"snapshot"`
	Files         map[string][]byte `json:"files"`
}

var errArchive = errors.New("invalid, damaged or oversized geodata archive")

type archiveBuffer struct{ bytes.Buffer }

func (b *archiveBuffer) Write(p []byte) (int, error) {
	if len(p) > maxArchiveBytes-b.Len() {
		return 0, errArchive
	}
	return b.Buffer.Write(p)
}

// ExportArchive does not change the current pointer or prune any generation.
// The caller also holds the application's mutation fence for a settings backup.
func (s *Store) ExportArchive(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	current, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	archive := portableArchive{SchemaVersion: 1, Snapshot: current, Files: map[string][]byte{}}
	if current.Revision != "" {
		dir, err := platform.OpenDirAtNoFollow(s.dir, current.Generation)
		if err != nil {
			return nil, err
		}
		defer dir.Close()
		original, err := dir.Stat()
		if err != nil {
			return nil, err
		}
		var total int64
		compressedTotal := 0
		for _, asset := range current.Assets {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			total += asset.Bytes
			if total > maxArchiveDecodedBytes {
				return nil, errArchive
			}
			data, err := readBounded(dir, asset.Name, asset.Bytes)
			if err != nil {
				return nil, err
			}
			if int64(len(data)) != asset.Bytes || digest(data) != asset.SHA256 {
				return nil, errArchive
			}
			var compressed archiveBuffer
			writer, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
			if err != nil {
				return nil, err
			}
			if _, err = writer.Write(data); err != nil {
				_ = writer.Close()
				return nil, err
			}
			if err = writer.Close(); err != nil {
				return nil, err
			}
			compressedTotal += compressed.Len()
			if compressedTotal > maxArchiveBytes {
				return nil, errArchive
			}
			archive.Files[asset.Name] = compressed.Bytes()
		}
		visible, err := platform.OpenDirAtNoFollow(s.dir, current.Generation)
		if err != nil {
			return nil, err
		}
		info, err := visible.Stat()
		visible.Close()
		if err != nil || !os.SameFile(original, info) {
			return nil, errArchive
		}
	}
	after, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(current, after) {
		return nil, ErrStale
	}
	data, err := json.Marshal(archive)
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveBytes {
		return nil, errArchive
	}
	return data, ctx.Err()
}

func archiveKeys(v any, keys ...string) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			return nil, false
		}
	}
	return m, true
}
func decodeArchive(data []byte) (portableArchive, error) {
	var a portableArchive
	value, err := strictjson.Decode(data)
	if err != nil {
		return a, errArchive
	}
	root, ok := archiveKeys(value, "schemaVersion", "snapshot", "files")
	if !ok {
		return a, errArchive
	}
	snapshot, ok := archiveKeys(root["snapshot"], "revision", "previousRevision", "generation", "selection", "assets")
	if !ok {
		return a, errArchive
	}
	if _, ok := archiveKeys(snapshot["selection"], "countryCodes", "geoSiteTags"); !ok {
		return a, errArchive
	}
	if snapshot["assets"] != nil {
		assets, ok := snapshot["assets"].([]any)
		if !ok {
			return a, errArchive
		}
		for _, asset := range assets {
			if _, ok := archiveKeys(asset, "kind", "tags", "revision", "url", "name", "path", "sha256", "bytes"); !ok {
				return a, errArchive
			}
		}
	}
	if _, ok := root["files"].(map[string]any); !ok {
		return a, errArchive
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&a); err != nil || a.SchemaVersion != 1 {
		return a, errArchive
	}
	if a.Snapshot.Revision == "" {
		if a.Snapshot.Generation != "" || a.Snapshot.PreviousRevision != "" || len(a.Snapshot.Assets) != 0 || len(a.Snapshot.Selection.CountryCodes) != 0 || len(a.Snapshot.Selection.GeoSiteTags) != 0 || len(a.Files) != 0 {
			return a, errArchive
		}
		return a, nil
	}
	if _, err = snapshotPlan(a.Snapshot); err != nil {
		return a, err
	}
	if len(a.Files) != len(a.Snapshot.Assets) {
		return a, errArchive
	}
	var total int64
	for _, asset := range a.Snapshot.Assets {
		total += asset.Bytes
		if total > maxArchiveDecodedBytes || len(a.Files[asset.Name]) == 0 {
			return a, errArchive
		}
	}
	return a, nil
}

func archivePayload(compressed []byte, asset Asset) ([]byte, error) {
	input := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(input)
	if err != nil {
		return nil, errArchive
	}
	reader.Multistream(false)
	data, err := io.ReadAll(io.LimitReader(reader, asset.Bytes+1))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || input.Len() != 0 || int64(len(data)) != asset.Bytes || digest(data) != asset.SHA256 {
		return nil, errArchive
	}
	return data, nil
}

// PrepareArchive never activates data. The normal owned Candidate and expected
// revision are still required by Commit; failed validation removes only its own
// freshly created staging directory. No network fallback exists here.
func (s *Store) PrepareArchive(ctx context.Context, data []byte, validate CoreValidator) (*Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if validate == nil || len(data) > maxArchiveBytes {
		return nil, errArchive
	}
	archive, err := decodeArchive(data)
	if err != nil {
		return nil, err
	}
	revisions := Revisions{}
	for _, asset := range archive.Snapshot.Assets {
		if asset.Kind == GeoIP {
			revisions.GeoIP = asset.Revision
		} else {
			revisions.GeoSite = asset.Revision
		}
	}
	fetch := func(ctx context.Context, source Source) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, asset := range archive.Snapshot.Assets {
			if reflect.DeepEqual(asset.Source, source) {
				return archivePayload(archive.Files[asset.Name], asset)
			}
		}
		return nil, errArchive
	}
	return s.Prepare(ctx, archive.Snapshot.Selection, revisions, fetch, validate)
}

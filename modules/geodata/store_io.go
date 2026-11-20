package geodata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

var generationPattern = regexp.MustCompile(`^gen-[0-9a-f]{32}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func readBounded(parent *os.File, name string, limit int64) ([]byte, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return nil, errors.New("invalid managed name")
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("geodata file is not bounded regular data")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("geodata file grew beyond limit")
	}
	return data, nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func snapshotRevision(snapshot Snapshot) string {
	snapshot.Revision = ""
	b, _ := json.Marshal(snapshot)
	return digest(b)
}
func cloneSnapshot(snapshot Snapshot) Snapshot {
	b, _ := json.Marshal(snapshot)
	var out Snapshot
	_ = json.Unmarshal(b, &out)
	return out
}

func snapshotPlan(snapshot Snapshot) ([]Source, error) {
	if !generationPattern.MatchString(snapshot.Generation) || !hashPattern.MatchString(snapshot.Revision) || snapshotRevision(snapshot) != snapshot.Revision {
		return nil, errors.New("invalid geodata snapshot identity")
	}
	revs := Revisions{}
	for _, asset := range snapshot.Assets {
		switch asset.Kind {
		case GeoIP:
			if revs.GeoIP != "" && revs.GeoIP != asset.Revision {
				return nil, errors.New("mixed GeoIP revisions")
			}
			revs.GeoIP = asset.Revision
		case GeoSite:
			if revs.GeoSite != "" && revs.GeoSite != asset.Revision {
				return nil, errors.New("mixed GeoSite revisions")
			}
			revs.GeoSite = asset.Revision
		default:
			return nil, errors.New("unknown asset kind")
		}
	}
	plan, err := SourcePlan(snapshot.Selection, revs)
	if err != nil {
		return nil, err
	}
	if len(plan) != len(snapshot.Assets) {
		return nil, errors.New("incomplete geodata snapshot")
	}
	for i, source := range plan {
		asset := snapshot.Assets[i]
		if !reflect.DeepEqual(source, asset.Source) || asset.Path != filepath.Join(snapshot.Generation, source.Name) || !hashPattern.MatchString(asset.SHA256) || asset.Bytes <= 0 || asset.Bytes > MaxAssetBytes {
			return nil, errors.New("invalid asset metadata")
		}
	}
	return plan, nil
}

func (s *Store) verify(ctx context.Context, snapshot Snapshot) error {
	plan, err := snapshotPlan(snapshot)
	if err != nil {
		return err
	}
	dir, err := platform.OpenDirAtNoFollow(s.dir, snapshot.Generation)
	if err != nil {
		return err
	}
	defer dir.Close()
	for i, source := range plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		asset := snapshot.Assets[i]
		data, err := readBounded(dir, source.Name, MaxAssetBytes)
		if err != nil {
			return err
		}
		if int64(len(data)) != asset.Bytes || digest(data) != asset.SHA256 {
			return errors.New("asset checksum changed")
		}
		if err := Validate(source.Kind, data, source.Tags); err != nil {
			return err
		}
	}
	return s.checkRoot()
}

func (s *Store) load(ctx context.Context) (Snapshot, error) {
	snapshot, err := s.loadMetadata(ctx)
	if err != nil || snapshot.Revision == "" {
		return snapshot, err
	}
	if err := s.verify(ctx, snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// Recovery can inspect a valid pointer even when its payload is damaged.
// This is never enough for activation: load and rollback's predecessor still
// require every file's size, checksum and native contents to validate.
func (s *Store) loadMetadata(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	data, err := readBounded(s.dir, "current.json", 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if _, err := snapshotPlan(snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := s.checkRoot(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

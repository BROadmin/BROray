package routepolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
	"golang.org/x/sys/unix"
)

const JournalFile = "route-policy-pending.v1.json"

const maxJournalBytes = 24 << 20

// Journal records durable intent. Callers also hold the application mutation
// fence and validate the candidate with the actual core. Begin precedes
// ApplyConfig/geodata.Commit; InstallState follows confirmed application.
// Confirm is the only successful completion point.
type Journal struct {
	root        platform.Root
	dirs        map[string]*os.File
	mu          sync.Mutex
	exclusions  map[string]*os.File
	daemonGuard *platform.Lock
}
type RestoreConfig func(context.Context, []byte, bool) error

type fileImage struct {
	Exists bool   `json:"exists"`
	Data   []byte `json:"data"`
	SHA256 string `json:"sha256"`
}
type policyRecord struct {
	SchemaVersion  int       `json:"schemaVersion"`
	Phase          string    `json:"phase"`
	PreviousState  fileImage `json:"previousState"`
	NextState      fileImage `json:"nextState"`
	PreviousConfig fileImage `json:"previousConfig"`
	NextConfig     fileImage `json:"nextConfig"`
	PreviousGeo    string    `json:"previousGeo"`
	NextGeo        string    `json:"nextGeo"`
}

func OpenJournal(root platform.Root) (*Journal, error) {
	j := &Journal{root: root, dirs: map[string]*os.File{}}
	for _, path := range []string{root.Routes(), root.Config(), root.Runtime(), root.Run()} {
		dir, err := platform.OpenDirNoFollow(path)
		if err != nil {
			j.Close()
			return nil, err
		}
		j.dirs[path] = dir
	}
	return j, nil
}
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	var result error
	for _, dir := range j.dirs {
		result = errors.Join(result, dir.Close())
	}
	j.dirs = nil
	return result
}

func (j *Journal) checkRoots() error {
	if len(j.dirs) != 4 {
		return errors.New("policy journal closed")
	}
	for path, held := range j.dirs {
		visible, err := platform.OpenDirNoFollow(path)
		if err != nil {
			return err
		}
		a, errA := held.Stat()
		b, errB := visible.Stat()
		visible.Close()
		if errA != nil || errB != nil || !os.SameFile(a, b) {
			return errors.New("policy journal root changed")
		}
	}
	if j.daemonGuard != nil && !j.daemonGuard.Owns(j.root.DaemonLock()) {
		return errors.New("daemon ownership changed")
	}
	for name, held := range j.exclusions {
		if err := sameJournalEntry(j.dirs[j.root.Run()], name, held); err != nil {
			return err
		}
	}
	return nil
}

func (j *Journal) locked(ctx context.Context, action func() error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := j.checkRoots(); err != nil {
		return err
	}
	fd := int(j.dirs[j.root.Routes()].Fd())
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return action()
}

func (j *Journal) geo(ctx context.Context) (geodata.Snapshot, error) {
	return j.geoSnapshot(ctx, false)
}

func (j *Journal) geoSnapshot(ctx context.Context, recovery bool) (geodata.Snapshot, error) {
	store, err := geodata.Open(filepath.Join(j.root.Runtime(), geodata.AssetDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return geodata.Snapshot{}, j.checkRoots()
	}
	if err != nil {
		return geodata.Snapshot{}, err
	}
	defer store.Close()
	if recovery {
		return store.RecoveryHead(ctx)
	}
	return store.Load(ctx)
}

func (j *Journal) load() (policyRecord, fileImage, error) {
	image, err := readImage(j.dirs[j.root.Routes()], JournalFile, maxJournalBytes)
	if err != nil {
		return policyRecord{}, fileImage{}, err
	}
	if !image.Exists {
		return policyRecord{}, image, os.ErrNotExist
	}
	var record policyRecord
	if err := strictJournalJSON(image.Data, &record); err != nil {
		return record, image, err
	}
	if err := requireJournalFields(image.Data); err != nil {
		return record, image, err
	}
	if err := record.validate(); err != nil {
		return record, image, err
	}
	return record, image, nil
}

func strictJournalJSON(data []byte, target any) error {
	if err := uniqueJSONValue(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing policy journal JSON")
	}
	return nil
}

// Absence of a recorded predecessor is explicit. A missing JSON field must
// never be interpreted as permission to remove the current file on rollback.
func requireJournalFields(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"schemaVersion", "phase", "previousState", "nextState", "previousConfig", "nextConfig", "previousGeo", "nextGeo"} {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("missing required policy journal field")
		}
	}
	for _, name := range []string{"previousGeo", "nextGeo"} {
		var value string
		if err := json.Unmarshal(fields[name], &value); err != nil {
			return err
		}
	}
	for _, name := range []string{"previousState", "nextState", "previousConfig", "nextConfig"} {
		var image map[string]json.RawMessage
		if err := json.Unmarshal(fields[name], &image); err != nil {
			return err
		}
		for _, key := range []string{"exists", "data", "sha256"} {
			if _, ok := image[key]; !ok {
				return errors.New("incomplete policy journal file image")
			}
		}
		flag := string(bytes.TrimSpace(image["exists"]))
		if flag != "true" && flag != "false" {
			return errors.New("journal file existence must be explicit boolean")
		}
		if bytes.Equal(bytes.TrimSpace(image["sha256"]), []byte("null")) {
			return errors.New("journal file digest must be explicit string")
		}
	}
	return nil
}

func (r policyRecord) validate() error {
	if r.SchemaVersion != 1 || (r.Phase != "prepared" && r.Phase != "committed") || !r.NextState.Exists {
		return errors.New("invalid policy journal schema")
	}
	for _, value := range []fileImage{r.PreviousState, r.NextState, r.PreviousConfig, r.NextConfig} {
		if !value.valid(MaxStateBytes) {
			return errors.New("invalid policy journal file image")
		}
	}
	for _, rev := range []string{r.PreviousGeo, r.NextGeo} {
		if rev != "" && !hashPattern.MatchString(rev) {
			return errors.New("invalid journal geo revision")
		}
	}
	next, err := DecodeState(r.NextState.Data)
	if err != nil {
		return err
	}
	if next.GeoRevision != r.NextGeo {
		return errors.New("journal next policy/geo mismatch")
	}
	if r.PreviousState.Exists {
		old, err := DecodeState(r.PreviousState.Data)
		if err != nil {
			return err
		}
		if old.NeedsGeodata() && old.GeoRevision != r.PreviousGeo {
			return errors.New("journal previous policy/geo mismatch")
		}
	}
	if r.PreviousConfig.Exists && !r.NextConfig.Exists {
		return errors.New("policy transaction cannot discard active config")
	}
	for _, config := range []fileImage{r.PreviousConfig, r.NextConfig} {
		if config.Exists && !json.Valid(config.Data) {
			return errors.New("invalid journal config JSON")
		}
	}
	return nil
}

func (j *Journal) Begin(ctx context.Context, next State, config []byte, nextGeo string) error {
	return j.begin(ctx, next, config, nextGeo, nil)
}

func (j *Journal) begin(ctx context.Context, next State, config []byte, nextGeo string, baseline *Baseline) error {
	return j.locked(ctx, func() error {
		pending, err := readImage(j.dirs[j.root.Routes()], JournalFile, maxJournalBytes)
		if err != nil {
			return err
		}
		if pending.Exists {
			return errors.New("routing policy transaction pending")
		}
		data, err := json.Marshal(next)
		if err != nil {
			return err
		}
		previousState, err := readImage(j.dirs[j.root.Routes()], StateFile, MaxStateBytes)
		if err != nil {
			return err
		}
		previousConfig, err := readImage(j.dirs[j.root.Config()], filepath.Base(j.root.XrayConfig()), MaxStateBytes)
		if err != nil {
			return err
		}
		geo, err := j.geo(ctx)
		if err != nil {
			return err
		}
		if baseline != nil {
			_, actual, err := policyBaseline(previousState, previousConfig, geo.Revision)
			if err != nil {
				return err
			}
			if actual != *baseline {
				return errors.New("policy preview baseline changed")
			}
		}
		nextConfig := previousConfig
		if config != nil {
			nextConfig = makeImage(config)
		}
		record := policyRecord{SchemaVersion: 1, Phase: "prepared", PreviousState: previousState, NextState: makeImage(data), PreviousConfig: previousConfig, NextConfig: nextConfig, PreviousGeo: geo.Revision, NextGeo: nextGeo}
		if err := record.validate(); err != nil {
			return err
		}
		if err := j.checkRoots(); err != nil {
			return err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return replaceImage(j.dirs[j.root.Routes()], JournalFile, makeImage(raw), pending, maxJournalBytes)
	})
}

func (j *Journal) currentFiles(r policyRecord, onlyNext bool) error {
	if err := j.checkRoots(); err != nil {
		return err
	}
	state, err := readImage(j.dirs[j.root.Routes()], StateFile, MaxStateBytes)
	if err != nil {
		return err
	}
	config, err := readImage(j.dirs[j.root.Config()], filepath.Base(j.root.XrayConfig()), MaxStateBytes)
	if err != nil {
		return err
	}
	if !state.equal(r.NextState) && (onlyNext || !state.equal(r.PreviousState)) {
		return errors.New("policy state changed outside transaction")
	}
	if !config.equal(r.NextConfig) && (onlyNext || !config.equal(r.PreviousConfig)) {
		return errors.New("Xray config changed outside policy transaction")
	}
	return nil
}

func (j *Journal) current(ctx context.Context, r policyRecord, onlyNext bool) error {
	if err := j.currentFiles(r, onlyNext); err != nil {
		return err
	}
	geo, err := j.geo(ctx)
	if err != nil {
		return err
	}
	return journalGeoMatches(r, geo, onlyNext)
}

func journalGeoMatches(r policyRecord, geo geodata.Snapshot, onlyNext bool) error {
	if geo.Revision != r.NextGeo && (onlyNext || geo.Revision != r.PreviousGeo) {
		return errors.New("geodata changed outside policy transaction")
	}
	if r.NextGeo != r.PreviousGeo && geo.Revision == r.NextGeo && geo.PreviousRevision != r.PreviousGeo {
		return errors.New("geodata predecessor does not match policy journal")
	}
	return nil
}

func (j *Journal) InstallState(ctx context.Context) error {
	return j.locked(ctx, func() error {
		r, _, err := j.load()
		if err != nil {
			return err
		}
		if r.Phase != "prepared" {
			return errors.New("policy transaction already committed")
		}
		if err := j.current(ctx, r, false); err != nil {
			return err
		}
		config, err := readImage(j.dirs[j.root.Config()], filepath.Base(j.root.XrayConfig()), MaxStateBytes)
		if err != nil {
			return err
		}
		geo, err := j.geo(ctx)
		if err != nil {
			return err
		}
		if !config.equal(r.NextConfig) || geo.Revision != r.NextGeo {
			return errors.New("candidate config/geodata not applied")
		}
		state, err := readImage(j.dirs[j.root.Routes()], StateFile, MaxStateBytes)
		if err != nil {
			return err
		}
		if err := j.checkRoots(); err != nil {
			return err
		}
		return replaceImage(j.dirs[j.root.Routes()], StateFile, r.NextState, state, MaxStateBytes)
	})
}
func (j *Journal) Confirm(ctx context.Context) error {
	return j.locked(ctx, func() error {
		r, owner, err := j.load()
		if err != nil {
			return err
		}
		if err := j.current(ctx, r, true); err != nil {
			return err
		}
		r.Phase = "committed"
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err := j.checkRoots(); err != nil {
			return err
		}
		if err := replaceImage(j.dirs[j.root.Routes()], JournalFile, makeImage(data), owner, maxJournalBytes); err != nil {
			return err
		}
		return j.clear(makeImage(data))
	})
}

func (j *Journal) clear(owner fileImage) error {
	if err := j.checkRoots(); err != nil {
		return err
	}
	return replaceImage(j.dirs[j.root.Routes()], JournalFile, fileImage{}, owner, maxJournalBytes)
}

func (j *Journal) Rollback(ctx context.Context, restore RestoreConfig) error {
	return j.locked(ctx, func() error { return j.rollback(ctx, restore) })
}

func (j *Journal) rollback(ctx context.Context, restore RestoreConfig) error {
	r, owner, err := j.load()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Phase == "committed" {
		if err := j.current(ctx, r, true); err != nil {
			return err
		}
		return j.clear(owner)
	}
	if restore == nil {
		return errors.New("confirmed config restoration required")
	}
	if err := j.currentFiles(r, false); err != nil {
		return err
	}
	geo, err := j.geoSnapshot(ctx, true)
	if err != nil {
		return err
	}
	if err := journalGeoMatches(r, geo, false); err != nil {
		return err
	}
	if r.NextGeo != r.PreviousGeo && geo.Revision == r.NextGeo {
		store, err := geodata.Open(filepath.Join(j.root.Runtime(), geodata.AssetDirectory))
		if err != nil {
			return err
		}
		prior, err := store.Rollback(ctx, r.NextGeo)
		store.Close()
		if err != nil {
			return err
		}
		if prior.Revision != r.PreviousGeo {
			return errors.New("unexpected geodata rollback result")
		}
	} else if _, err := j.geo(ctx); err != nil {
		// The previous pointer is already installed: its payload must still
		// be fully usable before restoring a configuration that references it.
		return err
	}
	if err := restore(ctx, append([]byte(nil), r.PreviousConfig.Data...), r.PreviousConfig.Exists); err != nil {
		return err
	}
	if err := j.checkRoots(); err != nil {
		return err
	}
	config, err := readImage(j.dirs[j.root.Config()], filepath.Base(j.root.XrayConfig()), MaxStateBytes)
	if err != nil {
		return err
	}
	if !config.equal(r.PreviousConfig) {
		return errors.New("config rollback readback not confirmed")
	}
	state, err := readImage(j.dirs[j.root.Routes()], StateFile, MaxStateBytes)
	if err != nil {
		return err
	}
	if !state.equal(r.PreviousState) && !state.equal(r.NextState) {
		return errors.New("policy changed during rollback")
	}
	if err := replaceImage(j.dirs[j.root.Routes()], StateFile, r.PreviousState, state, MaxStateBytes); err != nil {
		return err
	}
	return j.clear(owner)
}

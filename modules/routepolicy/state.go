package routepolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

const StateFile = "route-policy.v1.json"
const MaxStateBytes = 4 << 20

// State binds saved rules to an immutable, verified geodata snapshot. Reading
// it does not create defaults or repair invalid data. Writers must coordinate
// state, geodata and live configuration under the application mutation fence.
type State struct {
	SchemaVersion int               `json:"schemaVersion"`
	GeoRevision   string            `json:"geoRevision,omitempty"`
	Rules         []Rule            `json:"rules"`
	GeoUpdatedAt  map[string]string `json:"geoUpdatedAt,omitempty"`
}

func (s State) NeedsGeodata() bool {
	for _, rule := range s.Rules {
		if rule.Enabled && (len(rule.CountryCodes) > 0 || len(rule.GeoSiteTags) > 0) {
			return true
		}
	}
	return false
}

// ReadState requires a real existing routes directory. Only an absent leaf is
// the legacy/default-off state; an inaccessible or replaced root is an error.
func ReadState(ctx context.Context, root string) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	dir, err := platform.OpenDirNoFollow(root)
	if err != nil {
		return State{}, err
	}
	defer dir.Close()
	data, err := readStateFile(dir)
	if err != nil {
		return State{}, err
	}
	actual, err := platform.OpenDirNoFollow(root)
	if err != nil {
		return State{}, err
	}
	defer actual.Close()
	before, err := dir.Stat()
	if err != nil {
		return State{}, err
	}
	after, err := actual.Stat()
	if err != nil {
		return State{}, err
	}
	if !os.SameFile(before, after) {
		return State{}, errors.New("routing policy directory changed")
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if data == nil {
		return State{SchemaVersion: 1, Rules: []Rule{}}, nil
	}
	return DecodeState(data)
}

// DecodeState uses the same strict schema for disk reads and staged changes.
func DecodeState(data []byte) (State, error) {
	if len(data) > MaxStateBytes {
		return State{}, errors.New("routing policy exceeds size limit")
	}
	if err := uniqueJSONValue(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return State{}, err
	}
	var document struct {
		SchemaVersion int               `json:"schemaVersion"`
		GeoRevision   string            `json:"geoRevision,omitempty"`
		Rules         []json.RawMessage `json:"rules"`
		GeoUpdatedAt  json.RawMessage   `json:"geoUpdatedAt,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return State{}, fmt.Errorf("routing policy JSON: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return State{}, errors.New("trailing routing policy data")
	}
	if document.SchemaVersion != 1 || document.Rules == nil || len(document.Rules) > 256 || (document.GeoRevision != "" && !hashPattern.MatchString(document.GeoRevision)) {
		return State{}, errors.New("invalid routing policy schema or revision")
	}
	state := State{SchemaVersion: 1, GeoRevision: document.GeoRevision, Rules: make([]Rule, 0, len(document.Rules))}
	if len(document.GeoUpdatedAt) > 0 {
		if err := json.Unmarshal(document.GeoUpdatedAt, &state.GeoUpdatedAt); err != nil {
			return State{}, err
		}
		if state.GeoUpdatedAt == nil || len(state.GeoUpdatedAt) > 2 {
			return State{}, errors.New("invalid geodata update dates")
		}
		for kind, date := range state.GeoUpdatedAt {
			if kind != "geoip" && kind != "geosite" {
				return State{}, errors.New("invalid geodata update kind")
			}
			if _, err := time.Parse(time.RFC3339Nano, date); err != nil {
				return State{}, errors.New("invalid geodata update date")
			}
		}
	}
	for _, raw := range document.Rules {
		var rule Rule
		stored := struct {
			*Rule
			Enabled *bool `json:"enabled"`
		}{Rule: &rule}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&stored); err != nil {
			return State{}, fmt.Errorf("routing rule JSON: %w", err)
		}
		if stored.Enabled == nil {
			return State{}, errors.New("routing rule requires explicit enabled boolean")
		}
		rule.Enabled = *stored.Enabled
		state.Rules = append(state.Rules, rule)
	}
	return state, nil
}

// encoding/json otherwise accepts repeated keys and silently uses the last
// value. That could turn an enabled deny into a disabled rule. Case folding
// also covers the field-name aliases accepted by its struct decoder.
func uniqueJSONValue(d *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("routing policy JSON nesting too deep")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[strings.ToLower(name)] {
				return errors.New("duplicate routing policy JSON key")
			}
			seen[strings.ToLower(name)] = true
			if err := uniqueJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid routing policy JSON delimiter")
	}
	_, err = d.Token()
	return err
}

func readStateFile(dir *os.File) ([]byte, error) {
	fd, err := unix.Openat(int(dir.Fd()), StateFile, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), StateFile)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxStateBytes {
		return nil, errors.New("routing policy must be bounded regular data")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxStateBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxStateBytes {
		return nil, errors.New("routing policy grew beyond size limit")
	}
	return data, nil
}

package routepolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
)

// Baseline binds the files used for preview to the later journal preparation.
type Baseline struct {
	StateRevision string
	ConfigSHA256  string
	GeoRevision   string
}

func (j *Journal) Baseline(ctx context.Context) (State, Baseline, error) {
	var state State
	var baseline Baseline
	err := j.locked(ctx, func() error {
		pending, err := readImage(j.dirs[j.root.Routes()], JournalFile, maxJournalBytes)
		if err != nil {
			return err
		}
		if pending.Exists {
			return errors.New("routing policy transaction pending")
		}
		previous, err := readImage(j.dirs[j.root.Routes()], StateFile, MaxStateBytes)
		if err != nil {
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
		state, baseline, err = policyBaseline(previous, config, geo.Revision)
		if err != nil {
			return err
		}
		return j.checkRoots()
	})
	return state, baseline, err
}

func (j *Journal) BeginChecked(ctx context.Context, next State, config []byte, nextGeo string, baseline Baseline) error {
	if !hashPattern.MatchString(baseline.StateRevision) {
		return errors.New("invalid policy baseline")
	}
	return j.begin(ctx, next, config, nextGeo, &baseline)
}

func StateRevision(state State) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	if _, err := DecodeState(data); err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func policyBaseline(previous, config fileImage, geo string) (State, Baseline, error) {
	state := State{SchemaVersion: 1, Rules: []Rule{}}
	var err error
	if previous.Exists {
		state, err = DecodeState(previous.Data)
		if err != nil {
			return State{}, Baseline{}, err
		}
	}
	revision, err := StateRevision(state)
	return state, Baseline{StateRevision: revision, ConfigSHA256: config.SHA256, GeoRevision: geo}, err
}

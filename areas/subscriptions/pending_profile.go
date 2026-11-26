package subscriptions

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
)

// The pending profile is private, outside the live server catalog. Its exact
// file bytes identify the revision that a subsequent apply may authorize.
type pendingActiveProfile struct {
	SchemaVersion   int    `json:"schemaVersion"`
	SourceID        string `json:"sourceId"`
	ServerID        string `json:"serverId"`
	SourceSHA256    string `json:"sourceSha256"`
	ActiveSHA256    string `json:"activeSha256"`
	CandidateSHA256 string `json:"candidateSha256"`
	Candidate       []byte `json:"candidate"`
}

type pendingProfileWrite struct {
	before, source deletionImage
	data           []byte
}

func (s *svc) readPendingActiveProfile(subID string) (deletionImage, *pendingActiveProfile, error) {
	image, err := s.pendingProfileImage(subID)
	if err != nil || image.info == nil {
		return image, nil, err
	}
	pending, err := decodePendingActiveProfile(subID, image.raw)
	return image, pending, err
}

func decodePendingActiveProfile(subID string, raw []byte) (*pendingActiveProfile, error) {
	if _, err := strictjson.Decode(raw); err != nil {
		return nil, errors.New("invalid pending profile")
	}
	var pending pendingActiveProfile
	if json.Unmarshal(raw, &pending) != nil || pending.SchemaVersion != 1 || pending.SourceID != subID ||
		!servers.ValidID(pending.ServerID) || !pendingProfileDigest(pending.SourceSHA256) || !pendingProfileDigest(pending.ActiveSHA256) ||
		compat.SHA256Hex(pending.Candidate) != pending.CandidateSHA256 {
		return nil, errors.New("invalid pending profile identity")
	}
	record, ok := decodeObject(pending.Candidate)
	if !ok || record["id"] != pending.ServerID || !belongsTo(record, subID) || servers.Validate(record, pending.ServerID+".json") != nil {
		return nil, errors.New("invalid pending profile record")
	}
	return &pending, nil
}

func (s *svc) pendingProfileImage(subID string) (deletionImage, error) {
	if validID(subID) != nil {
		return deletionImage{}, errors.New("invalid pending source identity")
	}
	path := filepath.Join(s.subDir(), ".pending-active-"+compat.SHA256Hex([]byte(subID))+".json")
	image, err := readDeletionImage(path, true)
	if err == nil && image.info != nil && image.info.Mode().Perm() != 0600 {
		return image, errors.New("untrusted pending profile permissions")
	}
	return image, err
}

func discardPendingProfile(image deletionImage) error {
	if !image.unchanged() {
		return errPersistence
	}
	if image.info == nil {
		return nil
	}
	if err := platform.RemoveRegularNoFollow(image.path); err != nil {
		return err
	}
	after, err := readDeletionImage(image.path, true)
	if err != nil || after.info != nil || image.parent == nil || after.parent == nil || !os.SameFile(image.parent, after.parent) {
		return errPersistence
	}
	return nil
}

func pendingProfileDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

const pendingSourceRevisionField = "pendingProfileSourceRevision"

type pendingSourceInputs struct {
	Fetch      fetchInput
	Enabled    bool
	FetchJSON  bool
	FetchClash bool
}

func pendingInputsOf(record map[string]any) pendingSourceInputs {
	return pendingSourceInputs{
		Fetch: inputOf(record), Enabled: record["enabled"] == true,
		FetchJSON: record["fetchJSON"] == true, FetchClash: record["fetchClash"] == true,
	}
}

// Bind authority to fetch inputs. Status, progress and derived provider
// metadata can legitimately change when the enclosing refresh completes.
func pendingSourceAuthority(source deletionImage, subID string) (string, error) {
	record, ok := decodeObject(source.raw)
	if !ok || !validRecord(record) || record["id"] != subID || record["enabled"] != true {
		return "", errors.New("unconfirmed subscription source for pending profile")
	}
	return compat.CanonicalSHA256(map[string]any{
		"sourceId": subID, "input": pendingInputsOf(record),
		"createdAt": record["createdAt"], "revision": record[pendingSourceRevisionField],
	})
}

type pendingActiveBinding struct {
	image, source, active, working deletionImage
	profile                        *pendingActiveProfile
}

func (b pendingActiveBinding) revision() string { return compat.SHA256Hex(b.image.raw) }

func (b pendingActiveBinding) unchanged() bool {
	return b.image.unchanged() && b.source.unchanged() && b.active.unchanged() && b.working.unchanged()
}

// Bind the private pending bytes to the current selected record and source.
func (s *svc) bindPendingActiveProfile(subID string) (pendingActiveBinding, error) {
	image, pending, err := s.readPendingActiveProfile(subID)
	if err != nil || pending == nil {
		return pendingActiveBinding{}, errPendingProfileChanged
	}
	source, err := readDeletionImage(s.recPath(subID), false)
	if err != nil {
		return pendingActiveBinding{}, errPendingProfileChanged
	}
	authority, err := pendingSourceAuthority(source, subID)
	if err != nil || authority != pending.SourceSHA256 {
		return pendingActiveBinding{}, errPendingProfileChanged
	}
	active, err := readDeletionImage(servers.ActiveFile(s.d), false)
	if err != nil || strings.TrimSpace(string(active.raw)) != pending.ServerID {
		return pendingActiveBinding{}, errPendingProfileChanged
	}
	working, err := readDeletionImage(servers.RecordPath(s.d, pending.ServerID), false)
	if err != nil || compat.SHA256Hex(working.raw) != pending.ActiveSHA256 {
		return pendingActiveBinding{}, errPendingProfileChanged
	}
	b := pendingActiveBinding{image: image, source: source, active: active, working: working, profile: pending}
	if !b.unchanged() || compat.CheckSourceSyncWorkspaces(s.d.Root) != nil {
		return pendingActiveBinding{}, errPendingProfileChanged
	}
	return b, nil
}

func (s *svc) pendingActiveProfileStatus(subID string) map[string]string {
	b, err := s.bindPendingActiveProfile(subID)
	if err != nil {
		return nil
	}
	return map[string]string{"serverId": b.profile.ServerID, "revision": b.revision()}
}

// Retain the working active record while refreshing the rest of the catalog.
// Only a complete response can replace or retire a pending candidate.
func (s *svc) planPendingActiveProfile(subID string, p *refreshCatalogPlan) (*pendingProfileWrite, error) {
	image, previous, err := s.readPendingActiveProfile(subID)
	if err != nil {
		return nil, err
	}
	complete := p.result.Policy == "replace"
	alreadyPending := previous != nil && previous.ServerID == p.activeID
	wanted := s.d.App != nil && s.d.App.Xray != nil && s.d.App.Xray.RuntimeSnapshot().Wanted
	holdSettings := p.result.Impact == "configuration-changed" && (wanted || alreadyPending)
	if !holdSettings && (complete || !alreadyPending) {
		if complete && previous != nil {
			return &pendingProfileWrite{before: image}, nil
		}
		return nil, nil
	}
	var old deletionImage
	for _, m := range p.members {
		if m.id == p.activeID {
			old = m.record
			break
		}
	}
	working, ok := decodeObject(old.raw)
	if !ok || working["id"] != p.activeID || !belongsTo(working, subID) {
		return nil, errors.New("unconfirmed working subscription profile")
	}
	var candidate servers.Record
	for i, r := range p.final {
		if r["id"] == p.activeID {
			candidate = r
			p.final[i] = working
			break
		}
	}
	if candidate == nil {
		return nil, errors.New("pending active candidate missing")
	}
	p.changed = slices.DeleteFunc(p.changed, func(id string) bool { return id == p.activeID })
	p.dpiChanged = slices.DeleteFunc(p.dpiChanged, func(r servers.Record) bool { return r["id"] == p.activeID })
	p.reapply = false
	if !complete {
		p.result.Impact = "configuration-retained"
		return nil, nil
	}
	source, err := readDeletionImage(s.recPath(subID), false)
	if err != nil {
		return nil, err
	}
	sourceSHA, err := pendingSourceAuthority(source, subID)
	if err != nil {
		return nil, err
	}
	raw, err := servers.Marshal(candidate)
	if err != nil {
		return nil, err
	}
	activeSHA := compat.SHA256Hex(old.raw)
	p.result.Impact = "configuration-pending"
	if alreadyPending && previous.SourceSHA256 == sourceSHA && previous.ActiveSHA256 == activeSHA {
		prior, _ := decodeObject(previous.Candidate)
		if sameIgnoringUpdatedAt(prior, candidate) {
			return &pendingProfileWrite{before: image, source: source, data: image.raw}, nil
		}
	}
	next := pendingActiveProfile{
		SchemaVersion: 1, SourceID: subID, ServerID: p.activeID, SourceSHA256: sourceSHA, ActiveSHA256: activeSHA,
		CandidateSHA256: compat.SHA256Hex(raw), Candidate: raw,
	}
	data, err := compat.Marshal(next)
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("pending subscription profile exceeds storage limit")
	}
	return &pendingProfileWrite{before: image, source: source, data: data}, nil
}

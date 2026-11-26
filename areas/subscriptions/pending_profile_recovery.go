package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

const profileRecoveryFile = "profile-recovery.json"
const sourceRecoveryMarker = "rollback-required"
const sourceRecoveryPrefix = "server-subscription-sync."

type profileCatalogRecovery struct {
	SchemaVersion     int    `json:"schemaVersion"`
	SourceID          string `json:"sourceId"`
	ServerID          string `json:"serverId"`
	SourceSHA256      string `json:"sourceSha256"`
	ActiveSHA256      string `json:"activeSha256"`
	ConfigSHA256      string `json:"configSha256"`
	PreferencesSHA256 string `json:"preferencesSha256"`
	PreviousRecord    []byte `json:"previousRecord"`
	Pending           []byte `json:"pending"`
}

func (s *svc) prepareProfileRecovery(subID string, apply *pendingApplyObservation) ([]byte, error) {
	prefs, err := readDeletionImageLimit(profileRecoveryPreferences(s.d), true, 4096)
	if err != nil || prefs.info != nil && len(prefs.raw) == 0 {
		return nil, errPersistence
	}
	b := apply.binding
	return json.Marshal(profileCatalogRecovery{
		SchemaVersion: 1, SourceID: subID, ServerID: b.profile.ServerID,
		SourceSHA256: compat.SHA256Hex(b.source.raw), ActiveSHA256: compat.SHA256Hex(b.active.raw),
		ConfigSHA256: compat.SHA256Hex(apply.config.raw), PreferencesSHA256: compat.SHA256Hex(prefs.raw),
		PreviousRecord: b.working.raw, Pending: b.image.raw,
	})
}

func profileRecoveryPreferences(d *compat.Deps) string {
	return d.Path("config", "system", "server-reserve.json")
}

func sourceProfileRecoveryRecord(root platform.Root) (deletionImage, deletionImage, error) {
	tmp, err := platform.OpenDirNoFollow(root.Tmp())
	if err != nil {
		return deletionImage{}, deletionImage{}, err
	}
	defer tmp.Close()
	legacy, err := readDeletionImageLimit(filepath.Join(root.Run(), "server-subscription.lock"), true, 128)
	if err != nil || legacy.info != nil {
		return deletionImage{}, deletionImage{}, compat.ErrSourceSyncRecoveryPending
	}
	var marker deletionImage
	for {
		entries, err := tmp.ReadDir(128)
		if err != nil && !errors.Is(err, io.EOF) {
			return marker, deletionImage{}, err
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), sourceRecoveryPrefix) {
				continue
			}
			path := filepath.Join(root.Tmp(), entry.Name(), sourceRecoveryMarker)
			image, e := readDeletionImageLimit(path, true, 128)
			if e != nil || image.parent == nil {
				return marker, deletionImage{}, compat.ErrSourceSyncRecoveryPending
			}
			if image.info == nil {
				continue
			}
			token := strings.TrimPrefix(entry.Name(), sourceRecoveryPrefix)
			if marker.info != nil || !exactApplyHex(token, 32) || image.info.Mode().Perm() != 0600 ||
				image.parent.Mode().Perm() != 0700 {
				return marker, deletionImage{}, compat.ErrSourceSyncRecoveryPending
			}
			marker = image
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if marker.info == nil {
		return marker, deletionImage{}, compat.ErrSourceSyncRecoveryPending
	}
	record, err := readDeletionImageLimit(filepath.Join(filepath.Dir(marker.path), profileRecoveryFile), false, 3<<20)
	if err != nil || record.info.Mode().Perm() != 0600 || !os.SameFile(marker.parent, record.parent) {
		return marker, deletionImage{}, compat.ErrSourceSyncRecoveryPending
	}
	return marker, record, nil
}

func decodeProfileRecovery(raw []byte) (profileCatalogRecovery, *pendingActiveProfile, error) {
	v, err := strictjson.Decode(raw)
	fields, _ := v.(map[string]any)
	names := []string{"schemaVersion", "sourceId", "serverId", "sourceSha256", "activeSha256", "configSha256", "preferencesSha256", "previousRecord", "pending"}
	if err != nil || len(fields) != len(names) {
		return profileCatalogRecovery{}, nil, compat.ErrSourceSyncRecoveryPending
	}
	for _, key := range names {
		if fields[key] == nil {
			return profileCatalogRecovery{}, nil, compat.ErrSourceSyncRecoveryPending
		}
	}
	var r profileCatalogRecovery
	if json.Unmarshal(raw, &r) != nil || r.SchemaVersion != 1 || !validSourceRemovalID(r.SourceID) ||
		!servers.BackupIDValid(r.ServerID) || !exactApplyHex(r.SourceSHA256, 64) || !exactApplyHex(r.ActiveSHA256, 64) ||
		!exactApplyHex(r.ConfigSHA256, 64) || !exactApplyHex(r.PreferencesSHA256, 64) ||
		len(r.PreviousRecord) == 0 || len(r.PreviousRecord) > 1<<20 || len(r.Pending) == 0 || len(r.Pending) > 1<<20 {
		return r, nil, compat.ErrSourceSyncRecoveryPending
	}
	previous, err := strictjson.Decode(r.PreviousRecord)
	record, ok := previous.(map[string]any)
	if err != nil || !ok || record["id"] != r.ServerID || !belongsTo(record, r.SourceID) ||
		servers.Validate(record, r.ServerID+".json") != nil {
		return r, nil, compat.ErrSourceSyncRecoveryPending
	}
	pending, err := decodePendingActiveProfile(r.SourceID, r.Pending)
	if err != nil || pending.ServerID != r.ServerID || pending.ActiveSHA256 != compat.SHA256Hex(r.PreviousRecord) ||
		pending.ActiveSHA256 == pending.CandidateSHA256 {
		return r, nil, compat.ErrSourceSyncRecoveryPending
	}
	return r, pending, nil
}

// RecoverPendingProfileCatalog resolves the exact catalog and native images
// before any area can start. Only the matching protected daemon owner calls it.
func RecoverPendingProfileCatalog(ctx context.Context, d *compat.Deps, guard *platform.Lock, restore reserve.RestoreTransactionInterface) error {
	return reserve.RecoverSourceProfileTransaction(ctx, d.Root, guard, restore,
		func(native *reserve.TransactionRecord, check, recoverNative func() error) error {
			return newSvc(d).recoverProfileCatalog(native, check, recoverNative)
		})
}

func (s *svc) recoverProfileCatalog(native *reserve.TransactionRecord, check, recoverNative func() error) error {
	fail := compat.ErrSourceSyncRecoveryPending
	marker, recipe, err := sourceProfileRecoveryRecord(s.d.Root)
	if err != nil {
		return err
	}
	r, pending, err := decodeProfileRecovery(recipe.raw)
	if err != nil || string(marker.raw) != r.SourceID+"\n" {
		return fail
	}
	source, err := readDeletionImage(s.recPath(r.SourceID), false)
	if err != nil || compat.SHA256Hex(source.raw) != r.SourceSHA256 {
		return fail
	}
	sourceDoc, ok := decodeObject(source.raw)
	if !ok || !validRecord(sourceDoc) || sourceDoc["id"] != r.SourceID || sourceDoc["enabled"] != true {
		return fail
	}
	active, err := readDeletionImageLimit(servers.ActiveFile(s.d), false, 4096)
	if err != nil || strings.TrimSpace(string(active.raw)) != r.ServerID || compat.SHA256Hex(active.raw) != r.ActiveSHA256 {
		return fail
	}
	record, err := readDeletionImage(servers.RecordPath(s.d, r.ServerID), false)
	if err != nil || record.info.Mode().Perm() != 0600 ||
		!bytes.Equal(record.raw, r.PreviousRecord) && !bytes.Equal(record.raw, pending.Candidate) {
		return fail
	}
	profile, err := s.pendingProfileImage(r.SourceID)
	if err != nil || profile.info != nil && !bytes.Equal(profile.raw, r.Pending) {
		return fail
	}
	owned := func() bool {
		return check() == nil && marker.unchanged() && recipe.unchanged() && source.unchanged() && active.unchanged()
	}
	if !owned() || !profile.unchanged() || !record.unchanged() {
		return fail
	}
	matchRuntime := func(configHash, prefsHash string) bool {
		config, err := readDeletionImageLimit(s.d.Root.XrayConfig(), false, 2<<20)
		if err != nil || compat.SHA256Hex(config.raw) != configHash {
			return false
		}
		prefs, err := readDeletionImageLimit(profileRecoveryPreferences(s.d), true, 4096)
		return err == nil && (prefs.info == nil || len(prefs.raw) != 0) && compat.SHA256Hex(prefs.raw) == prefsHash
	}
	if native != nil {
		if !native.IsSourceProfile() || native.Intent.Authority != compat.SHA256Hex(recipe.raw) ||
			!bytes.Equal(native.Previous.Active.Data, active.raw) || !bytes.Equal(native.Next.Active.Data, active.raw) ||
			native.Previous.Config.SHA256 != r.ConfigSHA256 || compat.SHA256Hex(native.Previous.Preferences.Data) != r.PreferencesSHA256 {
			return fail
		}
		if native.Phase == "committed" {
			if !bytes.Equal(record.raw, pending.Candidate) || profile.info != nil ||
				!matchRuntime(native.Next.Config.SHA256, compat.SHA256Hex(native.Next.Preferences.Data)) {
				return fail
			}
			if !owned() || !profile.unchanged() || !record.unchanged() {
				return fail
			}
			if err := discardPendingProfile(marker); err != nil {
				return err
			}
			if err := recoverNative(); err != nil {
				return err
			}
			s.cleanupRecoveredProfileArtifacts(recipe)
			return nil
		}
	}
	if err := recoverNative(); err != nil {
		return err
	}
	if !matchRuntime(r.ConfigSHA256, r.PreferencesSHA256) ||
		!owned() || !record.unchanged() || !profile.unchanged() {
		return fail
	}
	for _, target := range []struct {
		image deletionImage
		prior []byte
	}{{profile, r.Pending}, {record, r.PreviousRecord}} {
		if !owned() || !target.image.unchanged() {
			return fail
		}
		if !bytes.Equal(target.image.raw, target.prior) {
			if err := platform.WriteFileAtomicNoFollow(target.image.path, target.prior, 0600); err != nil {
				return err
			}
		}
	}
	for path, prior := range map[string][]byte{profile.path: r.Pending, record.path: r.PreviousRecord} {
		image, err := readDeletionImage(path, false)
		if err != nil || !bytes.Equal(image.raw, prior) {
			return fail
		}
	}
	if !owned() || !matchRuntime(r.ConfigSHA256, r.PreferencesSHA256) {
		return fail
	}
	if err := discardPendingProfile(marker); err != nil {
		return err
	}
	s.cleanupRecoveredProfileArtifacts(recipe)
	return nil
}

func (s *svc) cleanupRecoveredProfileArtifacts(recipe deletionImage) {
	// The marker is already retired. Never unlink an unknown file or
	// directory to make best-effort cleanup of a settled transaction succeed.
	if err := discardPendingProfile(recipe); err != nil {
		s.d.Log.Printf("subscriptions: settled profile recovery artifact retained: %v", err)
	}
}

// Package update is the BROray 3.5.0 release channel and launcher
// (docs/BRORAY-3.5.0-UPDATER-DESIGN.md §3-§4, docs/RELEASE-CHANNEL.md): it
// fetches a signed channel manifest, verifies it against the keys built
// into the binary, picks the installer for this architecture, downloads
// and verifies it, and runs `<installer> installer apply --abi 1 ...`.
// It knows nothing about data formats, versions of modules or migrations.
package update

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	SchemaVersion    = 1
	MaxManifestBytes = 256 << 10
	MaxArtifactBytes = 64 << 20  // download (tarball or binary)
	MaxUnpackedBytes = 128 << 20 // installer binary inside a tarball
	// ClockSkew tolerates a router clock slightly behind the signer's.
	ClockSkew = 10 * time.Minute
	// MinClockYear: an earlier year means the clock was never set (no NTP
	// yet after boot); expiry cannot be judged then, so nothing is trusted.
	MinClockYear = 2025
)

// Manifest is the channel manifest (design §4). The signature covers the
// exact bytes; unknown fields are ignored so later schema additions stay
// readable by published launchers.
type Manifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	Channel       string    `json:"channel"`
	IssuedAt      time.Time `json:"issuedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Releases      []Release `json:"releases"`
	Revoked       []string  `json:"revoked"`
	// Rollout is parsed and validated but not enforced yet (TODO: staged
	// rollout by hash of the installation ID, design §4).
	Rollout *Rollout `json:"rollout,omitempty"`
}

type Release struct {
	Version              string              `json:"version"`
	Installer            map[string]Artifact `json:"installer"` // key: Platform() ("arm64", "arm/7", "mipsle/softfloat")
	Notes                string              `json:"notes,omitempty"`
	MinimumSourceVersion string              `json:"minimumSourceVersion,omitempty"`
}

// Artifact is one downloadable file. Format "binary" (default) is the
// installer itself; "tar.gz" is a release tarball whose member Entry is
// the installer (UnpackedSize bytes). URL may be relative to the manifest.
type Artifact struct {
	URL          string `json:"url"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	Format       string `json:"format,omitempty"`
	Entry        string `json:"entry,omitempty"`
	UnpackedSize int64  `json:"unpackedSize,omitempty"`
}

type Rollout struct {
	Version string `json:"version,omitempty"`
	Percent int    `json:"percent"`
}

var (
	channelRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	shaRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ValidChannel reports whether name is a usable channel name.
func ValidChannel(name string) bool { return channelRe.MatchString(name) }

// ParseManifest decodes and validates a manifest whose signature has
// already been verified.
func ParseManifest(b []byte) (*Manifest, error) {
	if len(b) > MaxManifestBytes {
		return nil, errors.New("manifest too large")
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schemaVersion %d is not supported", m.SchemaVersion)
	}
	if !ValidChannel(m.Channel) {
		return fmt.Errorf("invalid channel %q", m.Channel)
	}
	if m.IssuedAt.IsZero() || m.ExpiresAt.IsZero() || !m.ExpiresAt.After(m.IssuedAt) {
		return errors.New("issuedAt/expiresAt missing or inverted")
	}
	seen := map[string]bool{}
	for _, r := range m.Releases {
		if !ValidVersion(r.Version) || seen[r.Version] {
			return fmt.Errorf("invalid or duplicate release version %q", r.Version)
		}
		seen[r.Version] = true
		if r.MinimumSourceVersion != "" && !ValidVersion(r.MinimumSourceVersion) {
			return fmt.Errorf("%s: invalid minimumSourceVersion", r.Version)
		}
		if len(r.Installer) == 0 {
			return fmt.Errorf("%s: no installer artifacts", r.Version)
		}
		for key, a := range r.Installer {
			if err := a.validate(); err != nil {
				return fmt.Errorf("%s/%s: %v", r.Version, key, err)
			}
		}
	}
	for _, v := range m.Revoked {
		if !ValidVersion(v) {
			return fmt.Errorf("invalid revoked version %q", v)
		}
	}
	if m.Rollout != nil && (m.Rollout.Percent < 0 || m.Rollout.Percent > 100 || (m.Rollout.Version != "" && !ValidVersion(m.Rollout.Version))) {
		return errors.New("invalid rollout")
	}
	return nil
}

func (a Artifact) validate() error {
	if a.URL == "" || strings.ContainsAny(a.URL, " \t\r\n") {
		return errors.New("url missing")
	}
	if a.Size <= 0 || a.Size > MaxArtifactBytes {
		return fmt.Errorf("size %d out of range", a.Size)
	}
	if !shaRe.MatchString(a.SHA256) {
		return errors.New("sha256 must be 64 lowercase hex digits")
	}
	switch a.Format {
	case "", "binary":
	case "tar.gz":
		if !safeEntry(a.Entry) {
			return fmt.Errorf("unsafe tarball entry %q", a.Entry)
		}
		if a.UnpackedSize <= 0 || a.UnpackedSize > MaxUnpackedBytes {
			return fmt.Errorf("unpackedSize %d out of range", a.UnpackedSize)
		}
	default:
		return fmt.Errorf("unknown format %q", a.Format)
	}
	return nil
}

// safeEntry: a clean relative path without "..".
func safeEntry(e string) bool {
	if e == "" || strings.HasPrefix(e, "/") || path.Clean(e) != e {
		return false
	}
	for _, p := range strings.Split(e, "/") {
		if p == ".." || p == "." {
			return false
		}
	}
	return true
}

// InstalledSize is the size of the installer binary once unpacked.
func (a Artifact) InstalledSize() int64 {
	if a.Format == "tar.gz" {
		return a.UnpackedSize
	}
	return a.Size
}

func (a Artifact) sha() []byte {
	b, _ := hex.DecodeString(a.SHA256)
	return b
}

// CheckTime refuses an unset clock, a manifest from the future and an
// expired manifest (protection against replay of an old manifest).
func (m *Manifest) CheckTime(now time.Time) error {
	if now.Year() < MinClockYear {
		return fail(ErrClockUnset, nil, "Часы роутера не установлены (%s): срок действия подписанного манифеста проверить нельзя. Дождитесь синхронизации времени (NTP) и повторите проверку.", now.UTC().Format("2006-01-02 15:04"))
	}
	if m.IssuedAt.After(now.Add(ClockSkew)) {
		return fail(ErrManifestFuture, nil, "Манифест канала выпущен позже текущего времени роутера (%s > %s). Проверьте часы роутера.", m.IssuedAt.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	if now.After(m.ExpiresAt) {
		return fail(ErrManifestExpired, nil, "Срок действия манифеста канала истёк %s. Канал не обновлялся; установка из него запрещена.", m.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return nil
}

// IsRevoked reports whether version v is withdrawn.
func (m *Manifest) IsRevoked(v string) bool {
	for _, r := range m.Revoked {
		if r == v {
			return true
		}
	}
	return false
}

// Find returns the release with version v.
func (m *Manifest) Find(v string) (Release, bool) {
	for _, r := range m.Releases {
		if r.Version == v {
			return r, true
		}
	}
	return Release{}, false
}

// Latest is the newest release that is not revoked.
func (m *Manifest) Latest() (Release, bool) {
	var best Release
	found := false
	for _, r := range m.Releases {
		if m.IsRevoked(r.Version) {
			continue
		}
		if !found || CompareVersions(r.Version, best.Version) > 0 {
			best, found = r, true
		}
	}
	return best, found
}

// ArtifactFor returns the installer for platform key p ("mipsle/softfloat"),
// accepting a bare-GOARCH entry as well.
func (r Release) ArtifactFor(p string) (Artifact, bool) {
	if a, ok := r.Installer[p]; ok {
		return a, true
	}
	if i := strings.IndexByte(p, '/'); i > 0 {
		if a, ok := r.Installer[p[:i]]; ok {
			return a, true
		}
	}
	return Artifact{}, false
}

// Target selects what to install. reinstall → the current version; else
// want (or the latest release when want is ""), which must be newer.
func (m *Manifest) Target(current, want, platformKey string, reinstall bool) (Release, Artifact, error) {
	var r Release
	var ok bool
	switch {
	case reinstall:
		if r, ok = m.Find(current); !ok {
			return r, Artifact{}, fail(ErrVersionNotListed, nil, "Источник установленной версии не подтверждён: версия %s не опубликована в канале «%s».", current, m.Channel)
		}
	case want != "":
		if r, ok = m.Find(want); !ok {
			return r, Artifact{}, fail(ErrNotAvailable, nil, "Версия %s не опубликована в канале «%s».", want, m.Channel)
		}
	default:
		if r, ok = m.Latest(); !ok {
			return r, Artifact{}, fail(ErrNotAvailable, nil, "Новая версия не обнаружена.")
		}
	}
	if m.IsRevoked(r.Version) {
		return r, Artifact{}, fail(ErrVersionRevoked, nil, "Версия %s отозвана и не устанавливается.", r.Version)
	}
	if !reinstall && CompareVersions(r.Version, current) <= 0 {
		return r, Artifact{}, fail(ErrNotAvailable, nil, "Новая версия не обнаружена.")
	}
	if !reinstall && r.MinimumSourceVersion != "" && CompareVersions(current, r.MinimumSourceVersion) < 0 {
		return r, Artifact{}, fail(ErrSourceTooOld, nil, "Версия %s устанавливается только поверх %s или новее; установлена %s.", r.Version, r.MinimumSourceVersion, current)
	}
	a, ok := r.ArtifactFor(platformKey)
	if !ok {
		return r, a, fail(ErrArchUnsupported, nil, "Версия %s опубликована без сборки для архитектуры %s.", r.Version, platformKey)
	}
	return r, a, nil
}

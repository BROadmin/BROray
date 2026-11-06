package xray

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/app"
	mxray "github.com/BROadmin/broray/modules/xray"
)

// The 3.x Xray version catalog (lib/xray-releases.sh, lib/xray-releases.jq):
// the owner may install ANY official XTLS release; BROray only tells how it
// relates to this build (compatible / partially compatible / not tested)
// and asks for consent for untested, partial, pre-release and downgrade.
// The archive is always an official GitHub asset whose SHA-256 comes from
// GitHub itself (API digest or the official .dgst file) and is re-checked
// at install time.

var (
	// GitHubAPI is the releases API base ("" disables the catalog; tests override).
	GitHubAPI = "https://api.github.com/repos/XTLS/Xray-core"
	// GitHubDownload is the canonical asset URL base (3.x requires exactly this).
	GitHubDownload = "https://github.com/XTLS/Xray-core/releases/download"
	// CatalogClient overrides the HTTP client (tests).
	CatalogClient *http.Client
)

const (
	catalogPages   = 10
	catalogPerPage = 20
	wantStable     = 2
	wantPre        = 5
)

// Compatibility is one record of the BROray compatibility registry
// (3.x share/xray-compatibility.json): which official archive was tested with
// which BROray build. status: compatible | incompatible ("частично").
type Compatibility struct {
	CandidateID   string   `json:"candidateId"`
	Architecture  string   `json:"architecture"`
	XrayTag       string   `json:"xrayTag"`
	ArchiveSHA256 string   `json:"archiveSha256"`
	TestedAt      string   `json:"testedAt"`
	Status        string   `json:"status"`
	Evidence      string   `json:"evidence"`
	Limitations   []string `json:"limitations,omitempty"`
}

// Registry is compiled into the build. Since dev6 it is derived from the
// evidence registry (evidence.go): only an archive checked at every level on
// that architecture is "compatible"; explicit "incompatible" records with
// limitations may be appended.
var Registry = registryFromEvidence()

func archFromZip(zip string) string {
	return strings.TrimSuffix(strings.TrimPrefix(zip, "Xray-linux-"), ".zip")
}

// compatibilityFor is xray-releases.jq:55-65: matching records sorted by
// [status=="incompatible", candidateId==current, testedAt]; the last wins.
func compatibilityFor(tag, sum string) map[string]any {
	return compatibilityForArch(tag, sum, archName())
}

func compatibilityForArch(tag, sum, arch string) map[string]any {
	var hits []Compatibility
	for _, r := range Registry {
		if r.XrayTag == tag && r.ArchiveSHA256 == sum && r.Architecture == arch {
			hits = append(hits, r)
		}
	}
	checks, passed := checksFor(tag, arch, sum)
	if len(hits) == 0 {
		m := map[string]any{"status": "untested", "label": "Совместимость не проверялась",
			"architecture": arch, "archiveSha256": sum, "checks": checks}
		if passed > 0 || hasEvidence(tag, arch, sum) {
			m["label"] = "Проверка совместимости не завершена"
		}
		return m
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		ai, bi := a.Status == "incompatible", b.Status == "incompatible"
		if ai != bi {
			return !ai
		}
		ac, bc := a.CandidateID == app.Version, b.CandidateID == app.Version
		if ac != bc {
			return !ac
		}
		return a.TestedAt < b.TestedAt
	})
	r := hits[len(hits)-1]
	label := "Совместима с BROray"
	if r.Status == "incompatible" {
		label = "Частично совместима с BROray"
	}
	m := map[string]any{"candidateId": r.CandidateID, "architecture": r.Architecture, "xrayTag": r.XrayTag,
		"archiveSha256": r.ArchiveSHA256, "testedAt": r.TestedAt, "status": r.Status, "evidence": r.Evidence, "label": label,
		"checks": checks}
	if len(r.Limitations) > 0 {
		m["limitations"] = r.Limitations
	}
	return m
}

type ghAsset struct {
	ID                 json.Number `json:"id"`
	Name               string      `json:"name"`
	Size               int64       `json:"size"`
	State              string      `json:"state"`
	BrowserDownloadURL string      `json:"browser_download_url"`
	Digest             string      `json:"digest"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt string    `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

// release is one normalised catalog row (xray-releases.jq:9-21).
type release struct {
	Tag        string
	Prerelease bool
	Published  string
	Zip        ghAsset
	Dgst       ghAsset
	SHA256     string
}

var reSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *service) httpClient() *http.Client {
	if CatalogClient != nil {
		return CatalogClient
	}
	if s.d.App != nil && s.d.App.HTTP != nil {
		return s.d.App.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (s *service) getJSON(ctx context.Context, url string, v any) error {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BROray-Xray/"+app.Version)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v)
}

// normalise keeps an official, complete release for this architecture.
func normalise(r ghRelease) (release, bool) {
	asset, err := mxray.CurrentAsset()
	if err != nil || r.Draft || !validTag(r.TagName) || r.PublishedAt == "" {
		return release{}, false
	}
	var zip, dgst []ghAsset
	for _, a := range r.Assets {
		want := GitHubDownload + "/" + r.TagName + "/" + a.Name
		if a.BrowserDownloadURL != want || (a.State != "" && a.State != "uploaded") {
			continue
		}
		switch a.Name {
		case asset.Zip:
			if a.Size > 0 && a.ID.String() != "" {
				zip = append(zip, a)
			}
		case asset.Zip + ".dgst":
			dgst = append(dgst, a)
		}
	}
	if len(zip) != 1 || len(dgst) != 1 {
		return release{}, false
	}
	out := release{Tag: r.TagName, Prerelease: r.Prerelease, Published: r.PublishedAt, Zip: zip[0], Dgst: dgst[0]}
	if d := strings.TrimPrefix(zip[0].Digest, "sha256:"); zip[0].Digest != d && reSHA.MatchString(d) {
		out.SHA256 = d
	}
	return out, true
}

// dgstSHA256 reads the SHA2-256 line of the official .dgst file (xray-update.sh:417-430).
func (s *service) dgstSHA256(ctx context.Context, url string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "BROray-Xray/"+app.Version)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 16384))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		for _, p := range []string{"SHA2-256=", "sha2-256 ", "SHA256=", "sha256 "} {
			if strings.HasPrefix(line, p) {
				v := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, p)))
				if reSHA.MatchString(v) {
					return v, nil
				}
			}
		}
	}
	return "", errors.New("no SHA2-256 line")
}

// fillSHA completes a release's archive digest from the .dgst when the API had none.
func (s *service) fillSHA(ctx context.Context, r *release) bool {
	if r.SHA256 != "" {
		return true
	}
	sum, err := s.dgstSHA256(ctx, r.Dgst.BrowserDownloadURL)
	if err != nil {
		return false
	}
	r.SHA256 = sum
	return true
}

// releaseByTag resolves releases/tags/<tag> (re-confirmation before install).
func (s *service) releaseByTag(ctx context.Context, tag string) (release, error) {
	if GitHubAPI == "" || !validTag(tag) {
		return release{}, errors.New("catalog disabled")
	}
	var r ghRelease
	if err := s.getJSON(ctx, GitHubAPI+"/releases/tags/"+tag, &r); err != nil {
		return release{}, err
	}
	rel, ok := normalise(r)
	if !ok || !s.fillSHA(ctx, &rel) {
		return release{}, errors.New("release not usable")
	}
	return rel, nil
}

// fetchCatalog is xray-releases.sh:148-194: newest 2 stable + 5 pre-releases,
// plus the installed tag and the newest compatible tag if missing.
func (s *service) fetchCatalog(ctx context.Context, current string) ([]release, bool, error) {
	if GitHubAPI == "" {
		return nil, false, errors.New("catalog disabled")
	}
	var all []release
	complete := false
	stable, pre := 0, 0
	seen := map[string]bool{}
	for page := 1; page <= catalogPages; page++ {
		var list []ghRelease
		if err := s.getJSON(ctx, fmt.Sprintf("%s/releases?per_page=%d&page=%d", GitHubAPI, catalogPerPage, page), &list); err != nil {
			if page == 1 {
				return nil, false, err
			}
			break
		}
		for _, r := range list {
			rel, ok := normalise(r)
			if !ok || seen[rel.Tag] {
				continue
			}
			seen[rel.Tag] = true
			all = append(all, rel)
			if rel.Prerelease {
				pre++
			} else {
				stable++
			}
		}
		if len(list) < catalogPerPage {
			complete = true
			break
		}
		if stable >= wantStable && pre >= wantPre {
			complete = true
			break
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Published > all[j].Published })
	var picked []release
	ns, np := 0, 0
	for _, r := range all {
		if r.Prerelease && np < wantPre {
			picked, np = append(picked, r), np+1
		} else if !r.Prerelease && ns < wantStable {
			picked, ns = append(picked, r), ns+1
		}
	}
	has := func(tag string) bool {
		for _, r := range picked {
			if r.Tag == tag {
				return true
			}
		}
		return false
	}
	extra := []string{}
	if current != "" {
		extra = append(extra, "v"+current)
	}
	if tags := mxray.PinnedTags(); len(tags) > 0 {
		extra = append(extra, tags[0]) // newest version tested with this build
	}
	for _, tag := range extra {
		if has(tag) {
			continue
		}
		found := false
		for _, r := range all {
			if r.Tag == tag {
				picked, found = append(picked, r), true
				break
			}
		}
		if !found {
			if r, err := s.releaseByTag(ctx, tag); err == nil {
				picked = append(picked, r)
			}
		}
	}
	var out []release
	for _, r := range picked {
		r := r
		if s.fillSHA(ctx, &r) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Published > out[j].Published })
	return out, complete, nil
}

func (r release) row(current string) map[string]any {
	version := strings.TrimPrefix(r.Tag, "v")
	return map[string]any{"tagName": r.Tag, "version": version, "prerelease": r.Prerelease, "publishedAt": r.Published,
		"installed": version == current, "available": true, "archiveSha256": r.SHA256,
		"asset":         map[string]any{"id": r.Zip.ID, "name": r.Zip.Name, "size": r.Zip.Size, "url": r.Zip.BrowserDownloadURL},
		"digest":        map[string]any{"name": r.Dgst.Name, "url": r.Dgst.BrowserDownloadURL},
		"compatibility": compatibilityFor(r.Tag, r.SHA256)}
}

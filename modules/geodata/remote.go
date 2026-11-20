package geodata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"time"
)

type Remote struct{ client *http.Client }

func NewRemote() *Remote {
	return &Remote{client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" || len(via) == 0 || req.URL.Host != via[0].URL.Host {
			return errors.New("geodata redirect refused")
		}
		return nil
	}}}
}
func (r *Remote) Resolve(ctx context.Context, kind Kind) (string, error) {
	repo := ""
	switch kind {
	case GeoIP:
		repo = "geoip"
	case GeoSite:
		repo = "domain-list-community"
	default:
		return "", errors.New("unknown data kind")
	}
	data, err := r.get(ctx, "https://api.github.com/repos/v2fly/"+repo+"/commits/release", 2<<20)
	if err != nil {
		return "", err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(data, &commit); err != nil {
		return "", err
	}
	if !revisionPattern.MatchString(commit.SHA) {
		return "", errors.New("publisher returned invalid immutable revision")
	}
	return commit.SHA, nil
}
func (r *Remote) Fetch(ctx context.Context, source Source) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selection, revisions := Selection{}, Revisions{}
	switch source.Kind {
	case GeoIP:
		selection.CountryCodes = source.Tags
		revisions.GeoIP = source.Revision
	case GeoSite:
		selection.GeoSiteTags = source.Tags
		revisions.GeoSite = source.Revision
	default:
		return nil, errors.New("unknown data kind")
	}
	plan, err := SourcePlan(selection, revisions)
	if err != nil {
		return nil, err
	}
	if len(plan) != 1 || !reflect.DeepEqual(plan[0], source) {
		return nil, errors.New("source differs from canonical pinned plan")
	}
	return r.get(ctx, source.URL, MaxAssetBytes)
}

func (r *Remote) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BROray-geodata")
	response, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geodata HTTP status %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("geodata download exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("geodata download exceeds size limit")
	}
	if response.ContentLength > 0 && int64(len(data)) != response.ContentLength {
		return nil, io.ErrUnexpectedEOF
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

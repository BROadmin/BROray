package routes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
)

func hNativeDomainPreview(e *engine, c *cgi.Req) (any, error) {
	if c.R.Method == http.MethodGet {
		return map[string]any{"presets": nativeDomainPresets()}, nil
	}
	if err := requestGuard(c); err != nil {
		return nil, err
	}
	var request struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.Body(), &request); err != nil {
		return nil, cgi.E(400, "DOMAIN_PRESET_INVALID", "Выберите готовый доменный набор.")
	}
	if _, ok := nativeDomainPresetByID(request.ID); !ok {
		preview, _, err := e.nativeOwnPreview(c.R.Context(), request.ID)
		return preview, err
	}
	revision, err := e.geoResolve(c.R.Context(), geodata.GeoSite)
	if err != nil {
		return nil, err
	}
	return e.nativePresetPreview(c.R.Context(), request.ID, revision)
}

type nativeDomainPreview struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Revision string `json:"sourceRevision"`
	Hash     string `json:"sourceSHA256"`
	nativeDomainContent
}

var nativeOwnID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}$`)

func (e *engine) nativeOwnPreview(ctx context.Context, id string) (nativeDomainPreview, bool, error) {
	var result nativeDomainPreview
	if !nativeOwnID.MatchString(id) {
		return result, false, cgi.E(400, "DOMAIN_PRESET_INVALID", "Выберите сохранённый доменный список.")
	}
	state, err := routepolicy.ReadState(ctx, e.root)
	if err != nil {
		return result, false, err
	}
	for _, rule := range state.Rules {
		if rule.ID != id || len(rule.Domains) == 0 || len(rule.CountryCodes) != 0 || len(rule.GeoSiteTags) != 0 {
			continue
		}
		source, err := json.Marshal(rule)
		if err != nil {
			return result, false, err
		}
		matchers := make([]geodata.DomainMatcher, 0, len(rule.Domains))
		for _, entry := range rule.Domains {
			prefix, name, _ := strings.Cut(entry, ":")
			switch prefix {
			case "domain":
				matchers = append(matchers, geodata.DomainMatcher{Kind: geodata.MatchSuffix, Value: name})
			case "full":
				matchers = append(matchers, geodata.DomainMatcher{Kind: geodata.MatchExact, Value: name})
			default:
				return result, false, fmt.Errorf("invalid saved domain matcher")
			}
		}
		return nativeDomainPreview{ID: id, Name: rule.Name, Revision: "own", Hash: fmt.Sprintf("%x", sha256.Sum256(source)),
			nativeDomainContent: nativeDomainSubset(matchers)}, rule.Enabled, nil
	}
	return result, false, cgi.E(400, "DOMAIN_PRESET_INVALID", "Выберите готовый набор или сохранённый доменный список.")
}

func (e *engine) nativePresetPreview(ctx context.Context, id, revision string) (nativeDomainPreview, error) {
	preset, ok := nativeDomainPresetByID(id)
	if !ok {
		return nativeDomainPreview{}, fmt.Errorf("unknown native domain preset")
	}
	sources, err := geodata.SourcePlan(geodata.Selection{GeoSiteTags: []string{preset.Tag}}, geodata.Revisions{GeoSite: revision})
	if err != nil {
		return nativeDomainPreview{}, err
	}
	data, err := e.geoFetch(ctx, sources[0])
	if err != nil {
		return nativeDomainPreview{}, err
	}
	content, err := nativePresetContent(data, id)
	if err != nil {
		return nativeDomainPreview{}, err
	}
	return nativeDomainPreview{ID: id, Revision: revision, Hash: fmt.Sprintf("%x", sha256.Sum256(data)), nativeDomainContent: content}, nil
}

type nativeDomainPreset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tag  string `json:"-"`
}

func nativeDomainPresets() []nativeDomainPreset {
	tags := map[string]string{"chatgpt": "openai", "claude": "anthropic", "wikipedia": "wikimedia"}
	result := make([]nativeDomainPreset, 0, len(builtins))
	for _, b := range builtins {
		tag := tags[b.ID]
		if tag == "" {
			tag = b.ID
		}
		result = append(result, nativeDomainPreset{ID: b.ID, Name: b.Name, Tag: tag})
	}
	return result
}

func nativeDomainPresetByID(id string) (nativeDomainPreset, bool) {
	for _, p := range nativeDomainPresets() {
		if p.ID == id {
			return p, true
		}
	}
	return nativeDomainPreset{}, false
}

type skippedDomainMatcher struct {
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

type nativeDomainContent struct {
	Domains []string               `json:"domains"`
	Skipped []skippedDomainMatcher `json:"skipped"`
}

func nativeDomainSubset(matchers []geodata.DomainMatcher) nativeDomainContent {
	result := nativeDomainContent{Domains: []string{}, Skipped: []skippedDomainMatcher{}}
	for _, m := range matchers {
		var kind, reason string
		switch m.Kind {
		case geodata.MatchSuffix:
			name := strings.ToLower(m.Value)
			if keenetic.ValidNativeDomainName(name) {
				result.Domains = append(result.Domains, name)
				continue
			}
			kind, reason = "suffix", "invalid-native-name"
		case geodata.MatchExact:
			kind, reason = "exact", "keenetic-includes-subdomains"
		case geodata.MatchRegexp:
			kind, reason = "regexp", "unsupported-regexp"
		case geodata.MatchKeyword:
			kind, reason = "keyword", "unsupported-keyword"
		default:
			kind, reason = "unknown", "unsupported-type"
		}
		result.Skipped = append(result.Skipped, skippedDomainMatcher{Kind: kind, Value: m.Value, Reason: reason})
	}
	slices.Sort(result.Domains)
	result.Domains = slices.Compact(result.Domains)
	return result
}

func nativePresetContent(data []byte, id string) (nativeDomainContent, error) {
	for _, preset := range nativeDomainPresets() {
		if preset.ID == id {
			matchers, err := geodata.GeoSiteMatchers(data, preset.Tag)
			if err != nil {
				return nativeDomainContent{}, err
			}
			return nativeDomainSubset(matchers), nil
		}
	}
	return nativeDomainContent{}, fmt.Errorf("unknown native domain preset")
}

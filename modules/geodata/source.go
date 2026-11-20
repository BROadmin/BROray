package geodata

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_!-]{0,95}$`)
var countryPattern = regexp.MustCompile(`^[a-z]{2}$`)

func normalizeTags(input []string, countries bool) ([]string, error) {
	if len(input) > 4096 {
		return nil, fmt.Errorf("too many selections")
	}
	seen := map[string]bool{}
	for _, value := range input {
		tag := strings.ToLower(value)
		if !tagPattern.MatchString(tag) || (countries && !countryPattern.MatchString(tag)) {
			return nil, fmt.Errorf("invalid geodata tag %q", value)
		}
		seen[tag] = true
	}
	result := make([]string, 0, len(seen))
	for tag := range seen {
		result = append(result, tag)
	}
	sort.Strings(result)
	return result, nil
}

// SourcePlan includes disabled but added rules: callers pass the complete
// selection, not only the active subset. No global GeoIP fallback exists.
func SourcePlan(selection Selection, revisions Revisions) ([]Source, error) {
	countries, err := normalizeTags(selection.CountryCodes, true)
	if err != nil {
		return nil, err
	}
	tags, err := normalizeTags(selection.GeoSiteTags, false)
	if err != nil {
		return nil, err
	}
	if len(countries) > 0 && !revisionPattern.MatchString(revisions.GeoIP) {
		return nil, fmt.Errorf("GeoIP needs an immutable Git commit")
	}
	if len(tags) > 0 && !revisionPattern.MatchString(revisions.GeoSite) {
		return nil, fmt.Errorf("GeoSite needs an immutable Git commit")
	}
	result := make([]Source, 0, len(countries)+1)
	for _, country := range countries {
		result = append(result, Source{Kind: GeoIP, Tags: []string{country}, Revision: revisions.GeoIP,
			URL: "https://raw.githubusercontent.com/v2fly/geoip/" + revisions.GeoIP + "/dat/" + country + ".dat", Name: "broray-geoip-" + country + ".dat"})
	}
	if len(tags) > 0 {
		result = append(result, Source{Kind: GeoSite, Tags: tags, Revision: revisions.GeoSite,
			URL: "https://raw.githubusercontent.com/v2fly/domain-list-community/" + revisions.GeoSite + "/dlc.dat", Name: "broray-geosite.dat"})
	}
	return result, nil
}

package geodata

import (
	"fmt"
	"strings"
)

// DomainMatchKind is the match type encoded by the GeoSite source.
type DomainMatchKind uint8

const (
	MatchKeyword DomainMatchKind = iota
	MatchRegexp
	MatchSuffix
	MatchExact
)

type DomainMatcher struct {
	Kind  DomainMatchKind
	Value string
}

// GeoSiteMatchers extracts a whole source tag with its original matching
// semantics. Consumers must decide which kinds their router supports.
func GeoSiteMatchers(data []byte, tag string) ([]DomainMatcher, error) {
	if !tagPattern.MatchString(tag) {
		return nil, fmt.Errorf("invalid GeoSite tag")
	}
	if err := Validate(GeoSite, data, []string{tag}); err != nil {
		return nil, err
	}
	var matchers []DomainMatcher
	err := walk(data, func(entry wireField) error {
		if entry.number != 1 {
			return nil
		}
		selected := false
		if err := walk(entry.data, func(field wireField) error {
			if field.number == 1 {
				selected = strings.EqualFold(string(field.data), tag)
			}
			return nil
		}); err != nil || !selected {
			return err
		}
		return walk(entry.data, func(field wireField) error {
			if field.number != 2 {
				return nil
			}
			if len(matchers) >= 50000 {
				return fmt.Errorf("GeoSite tag exceeds the domain limit")
			}
			rule, err := decodeDomain(field.data)
			if err == nil {
				matchers = append(matchers, rule)
			}
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	return matchers, nil
}

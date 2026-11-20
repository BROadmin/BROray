package routepolicy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/BROadmin/broray/modules/geodata"
)

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}$`)
var generationPattern = regexp.MustCompile(`^gen-[0-9a-f]{32}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func priority(action Action) int {
	switch action {
	case Deny:
		return 0
	case Direct:
		return 1
	case VPN:
		return 2
	}
	return -1
}

func references(snapshot geodata.Snapshot, kind geodata.Kind, tags []string) ([]string, error) {
	refs := map[string]bool{}
	for _, tag := range tags {
		var found *geodata.Asset
		for i := range snapshot.Assets {
			asset := &snapshot.Assets[i]
			if asset.Kind != kind {
				continue
			}
			for _, candidate := range asset.Tags {
				if strings.ToLower(candidate) == tag {
					if found != nil {
						return nil, fmt.Errorf("ambiguous geodata tag %s", tag)
					}
					found = asset
				}
			}
		}
		if found == nil {
			return nil, fmt.Errorf("geodata tag %s is not installed", tag)
		}
		selection, revs := geodata.Selection{}, geodata.Revisions{}
		if kind == geodata.GeoIP {
			selection.CountryCodes = found.Tags
			revs.GeoIP = found.Source.Revision
		} else {
			selection.GeoSiteTags = found.Tags
			revs.GeoSite = found.Source.Revision
		}
		plan, err := geodata.SourcePlan(selection, revs)
		if err != nil {
			return nil, err
		}
		if len(plan) != 1 || !reflect.DeepEqual(plan[0], found.Source) || !generationPattern.MatchString(snapshot.Generation) || found.Path != snapshot.Generation+"/"+found.Name || !hashPattern.MatchString(found.SHA256) || found.Bytes <= 0 || found.Bytes > geodata.MaxAssetBytes {
			return nil, fmt.Errorf("invalid managed geodata asset")
		}
		refs["ext:"+found.Path+":"+tag] = true
	}
	return sortedKeys(refs), nil
}

// Compile produces OR alternatives for different matcher classes, in a fixed
// deny/direct/VPN order. It never emits an unscoped or implicit DIRECT rule.
func Compile(rules []Rule, snapshot geodata.Snapshot, tags OutboundTags, inboundTags []string) ([]json.RawMessage, error) {
	if len(rules) > 256 {
		return nil, fmt.Errorf("too many policy rules")
	}
	seenTags := map[string]bool{}
	for _, tag := range []string{tags.VPN, tags.Direct, tags.Deny} {
		if !namePattern.MatchString(tag) || seenTags[tag] {
			return nil, fmt.Errorf("outbound tags must be valid and distinct")
		}
		seenTags[tag] = true
	}
	inbounds := map[string]bool{}
	for _, tag := range inboundTags {
		if !namePattern.MatchString(tag) {
			return nil, fmt.Errorf("invalid inbound scope")
		}
		inbounds[tag] = true
	}
	if len(inbounds) == 0 {
		return nil, fmt.Errorf("explicit inbound scope required")
	}
	scope := sortedKeys(inbounds)
	ordered := append([]Rule(nil), rules...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := priority(ordered[i].Action), priority(ordered[j].Action)
		if a != b {
			return a < b
		}
		return ordered[i].ID < ordered[j].ID
	})
	seenIDs := map[string]bool{}
	result := []json.RawMessage{}
	for _, rule := range ordered {
		if !namePattern.MatchString(rule.ID) || seenIDs[rule.ID] || priority(rule.Action) < 0 {
			return nil, fmt.Errorf("invalid/duplicate policy rule")
		}
		seenIDs[rule.ID] = true
		domains, err := ParseDomainList(strings.Join(rule.Domains, "\n"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rule.ID, err)
		}
		// Reuse the source module's strict normalized tag grammar even while off.
		selection := geodata.Selection{CountryCodes: rule.CountryCodes, GeoSiteTags: rule.GeoSiteTags}
		validationPlan, err := geodata.SourcePlan(selection, geodata.Revisions{GeoIP: strings.Repeat("0", 40), GeoSite: strings.Repeat("0", 40)})
		if err != nil {
			return nil, err
		}
		if !rule.Enabled {
			continue
		}
		ipTags, siteTags := []string{}, []string{}
		for _, source := range validationPlan {
			if source.Kind == geodata.GeoIP {
				ipTags = append(ipTags, source.Tags...)
			} else {
				siteTags = append(siteTags, source.Tags...)
			}
		}
		ips, err := references(snapshot, geodata.GeoIP, ipTags)
		if err != nil {
			return nil, err
		}
		sites, err := references(snapshot, geodata.GeoSite, siteTags)
		if err != nil {
			return nil, err
		}
		domains = append(domains, sites...)
		sort.Strings(domains)
		if len(ips)+len(domains) == 0 {
			return nil, fmt.Errorf("enabled rule %s has no matchers", rule.ID)
		}
		outbound := tags.VPN
		if rule.Action == Deny {
			outbound = tags.Deny
		} else if rule.Action == Direct {
			outbound = tags.Direct
		}
		for _, part := range []struct {
			field  string
			values []string
		}{{"ip", ips}, {"domain", domains}} {
			if len(part.values) == 0 {
				continue
			}
			data, err := json.Marshal(map[string]any{"type": "field", "ruleTag": "broray-" + rule.ID + "-" + part.field, "inboundTag": scope, "outboundTag": outbound, part.field: part.values})
			if err != nil {
				return nil, err
			}
			result = append(result, data)
		}
	}
	return result, nil
}

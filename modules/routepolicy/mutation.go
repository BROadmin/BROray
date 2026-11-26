package routepolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/BROadmin/broray/modules/geodata"
)

// Mutation edits one concern only. Upsert cannot carry enabled/action values
// from a dirty form; these controls have their own compare-and-swap commands.
type Mutation struct {
	ExpectedRevision         string
	ExpectedPendingRevision  string
	ExpectedScheduleRevision string
	ApplyDayUTC              string
	Operation                string
	ID                       string
	Name                     string
	Domains                  []string
	Enabled                  bool
	Action                   Action
	Kind                     geodata.Kind
	Tags                     []string
}

func DecodeMutation(data []byte) (Mutation, error) {
	var m Mutation
	invalid := errors.New("invalid policy mutation")
	if len(data) > MaxStateBytes {
		return m, invalid
	}
	var raw map[string]json.RawMessage
	if err := strictJournalJSON(data, &raw); err != nil {
		return m, err
	}
	if raw == nil {
		return m, invalid
	}
	for _, value := range raw {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return m, invalid
		}
	}
	if json.Unmarshal(raw["expectedRevision"], &m.ExpectedRevision) != nil || !hashPattern.MatchString(m.ExpectedRevision) ||
		json.Unmarshal(raw["operation"], &m.Operation) != nil || json.Unmarshal(raw["id"], &m.ID) != nil || !namePattern.MatchString(m.ID) {
		return m, invalid
	}
	allowed := map[string]bool{"expectedRevision": true, "operation": true, "id": true}
	switch m.Operation {
	case "upsert":
		allowed["name"], allowed["domains"] = true, true
		var domains string
		if json.Unmarshal(raw["name"], &m.Name) != nil || json.Unmarshal(raw["domains"], &domains) != nil {
			return m, invalid
		}
		m.Name = strings.TrimSpace(m.Name)
		if m.Name == "" || len([]rune(m.Name)) > 128 || strings.IndexFunc(m.Name, unicode.IsControl) >= 0 {
			return m, invalid
		}
		var err error
		m.Domains, err = ParseDomainList(domains)
		if err != nil || len(m.Domains) == 0 {
			return m, invalid
		}
	case "enabled":
		allowed["enabled"] = true
		if json.Unmarshal(raw["enabled"], &m.Enabled) != nil {
			return m, invalid
		}
	case "action":
		allowed["action"] = true
		if json.Unmarshal(raw["action"], &m.Action) != nil || priority(m.Action) < 0 {
			return m, invalid
		}
	case "delete":
	case "geo-check", "geo-apply":
		if m.ID != "geodata" {
			return m, invalid
		}
		if m.Operation == "geo-apply" {
			allowed["expectedPendingRevision"] = true
			if json.Unmarshal(raw["expectedPendingRevision"], &m.ExpectedPendingRevision) != nil || !hashPattern.MatchString(m.ExpectedPendingRevision) {
				return m, invalid
			}
			if raw["expectedScheduleRevision"] != nil || raw["applyDayUTC"] != nil {
				allowed["expectedScheduleRevision"], allowed["applyDayUTC"] = true, true
				if json.Unmarshal(raw["expectedScheduleRevision"], &m.ExpectedScheduleRevision) != nil ||
					json.Unmarshal(raw["applyDayUTC"], &m.ApplyDayUTC) != nil ||
					!hashPattern.MatchString(m.ExpectedScheduleRevision) || !validGeoApplyDay(m.ApplyDayUTC) {
					return m, invalid
				}
			}
		}
	case "geo-upsert", "geo-update":
		if len(data) > 64<<10 {
			return m, invalid
		}
		allowed["kind"] = true
		if json.Unmarshal(raw["kind"], &m.Kind) != nil || (m.Kind != geodata.GeoIP && m.Kind != geodata.GeoSite) {
			return m, invalid
		}
		if m.Operation == "geo-update" {
			if m.ID != string(m.Kind) {
				return m, invalid
			}
			break
		}
		allowed["name"], allowed["tags"] = true, true
		if json.Unmarshal(raw["name"], &m.Name) != nil || json.Unmarshal(raw["tags"], &m.Tags) != nil || len(m.Tags) == 0 {
			return m, invalid
		}
		m.Name = strings.TrimSpace(m.Name)
		if m.Name == "" || len([]rune(m.Name)) > 128 || strings.IndexFunc(m.Name, unicode.IsControl) >= 0 {
			return m, invalid
		}
		selection := geodata.Selection{}
		if m.Kind == geodata.GeoIP {
			selection.CountryCodes = m.Tags
		} else {
			selection.GeoSiteTags = m.Tags
		}
		plan, err := geodata.SourcePlan(selection, geodata.Revisions{GeoIP: strings.Repeat("0", 40), GeoSite: strings.Repeat("0", 40)})
		if err != nil {
			return m, invalid
		}
		m.Tags = nil
		for _, source := range plan {
			m.Tags = append(m.Tags, source.Tags...)
		}
	default:
		return m, invalid
	}
	if len(raw) != len(allowed) {
		return m, invalid
	}
	for key := range raw {
		if !allowed[key] {
			return m, invalid
		}
	}
	return m, nil
}

func (m Mutation) Apply(state State) (State, error) {
	state.Rules = append([]Rule{}, state.Rules...)
	if m.Operation == "geo-update" || m.Operation == "geo-check" || m.Operation == "geo-apply" {
		return state, nil
	}
	index := -1
	for i, rule := range state.Rules {
		if rule.ID == m.ID {
			index = i
			break
		}
	}
	if index < 0 {
		if m.Operation != "upsert" && m.Operation != "geo-upsert" {
			return State{}, errors.New("policy rule does not exist")
		}
		if len(state.Rules) >= 256 {
			return State{}, errors.New("too many policy rules")
		}
		state.Rules = append(state.Rules, NewRule(m.ID, m.Name))
		index = len(state.Rules) - 1
	}
	switch m.Operation {
	case "geo-upsert":
		rule := &state.Rules[index]
		if len(rule.Domains) > 0 || (m.Kind == geodata.GeoIP && len(rule.GeoSiteTags) > 0) || (m.Kind == geodata.GeoSite && len(rule.CountryCodes) > 0) {
			return State{}, errors.New("cannot change rule matcher kind")
		}
		rule.Name = m.Name
		if m.Kind == geodata.GeoIP {
			rule.CountryCodes = append([]string{}, m.Tags...)
		} else {
			rule.GeoSiteTags = append([]string{}, m.Tags...)
		}
	case "upsert":
		if len(state.Rules[index].GeoSiteTags)+len(state.Rules[index].CountryCodes) > 0 {
			return State{}, errors.New("managed geodata rule is not a custom domain list")
		}
		state.Rules[index].Name = m.Name
		state.Rules[index].Domains = append([]string{}, m.Domains...)
	case "enabled":
		state.Rules[index].Enabled = m.Enabled
	case "action":
		state.Rules[index].Action = m.Action
	case "delete":
		state.Rules = append(state.Rules[:index], state.Rules[index+1:]...)
	default:
		return State{}, errors.New("invalid policy operation")
	}
	return state, nil
}

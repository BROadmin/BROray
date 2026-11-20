package routepolicy

import (
	"strings"
	"testing"
)

func TestGeoMutationDefaultOffAndIndependentSettings(t *testing.T) {
	rev := strings.Repeat("a", 64)
	raw := `{"expectedRevision":"` + rev + `","operation":"geo-upsert","id":"fi","name":"Finland","kind":"geoip","tags":["FI","fi"]}`
	m, err := DecodeMutation([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.Apply(State{SchemaVersion: 1, Rules: []Rule{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Rules) != 1 || s.Rules[0].Enabled || s.Rules[0].Action != Deny || len(s.Rules[0].CountryCodes) != 1 || s.Rules[0].CountryCodes[0] != "fi" {
		t.Fatalf("bad geo rule: %+v", s)
	}
	s.Rules[0].Enabled = true
	s.Rules[0].Action = Direct
	s, err = m.Apply(s)
	if err != nil || !s.Rules[0].Enabled || s.Rules[0].Action != Direct {
		t.Fatal("edit reset controls")
	}
	update, err := DecodeMutation([]byte(`{"expectedRevision":"` + rev + `","operation":"geo-update","id":"geoip","kind":"geoip"}`))
	if err != nil {
		t.Fatal(err)
	}
	after, err := update.Apply(s)
	if err != nil || after.Rules[0].Action != Direct {
		t.Fatal("update changed rules")
	}
}

func TestGeoMutationRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	base := `{"expectedRevision":"` + strings.Repeat("a", 64) + `","operation":"geo-upsert","id":"geo","name":"Geo","kind":"geoip"`
	for _, tail := range []string{`}`, `,"tags":null}`, `,"tags":[]}`, `,"tags":["fi"],"enabled":true}`, `,"tags":["fi"],"Tags":["ru"]}`, `,"tags":["../fi"]}`, `,"tags":["fi"],"domains":"example.com"}`} {
		if _, err := DecodeMutation([]byte(base + tail)); err == nil {
			t.Fatal("ambiguous geo request accepted")
		}
	}
}

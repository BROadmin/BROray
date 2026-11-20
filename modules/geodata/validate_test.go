package geodata

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func TestGeoSiteMatchersPreserveSourceSemantics(t *testing.T) {
	want := []DomainMatcher{
		{MatchExact, "exact.example"},
		{MatchSuffix, "suffix.example"},
		{MatchRegexp, "^cdn[0-9]+[.]example$"},
		{MatchKeyword, "keyword"},
	}
	entry := fieldBytes(1, []byte("EXAMPLE"))
	for _, m := range want {
		domain := append(fieldInt(1, uint64(m.Kind)), fieldBytes(2, []byte(m.Value))...)
		entry = append(entry, fieldBytes(2, domain)...)
	}
	data := append(geoSiteFixture("OTHER", "other.example", 2), fieldBytes(1, entry)...)
	got, err := GeoSiteMatchers(data, "example")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("selected matchers = %#v, %v", got, err)
	}
	for _, tc := range []struct {
		tag  string
		data []byte
	}{
		{"missing", data},
		{"example", data[:len(data)-1]},
		{"example", append(data, fieldBytes(1, entry)...)},
	} {
		if rules, err := GeoSiteMatchers(tc.data, tc.tag); err == nil || len(rules) != 0 {
			t.Fatalf("extraction yielded partial or invalid rules for %q: %#v, %v", tc.tag, rules, err)
		}
	}
}

func fieldBytes(n uint64, data []byte) []byte {
	b := binary.AppendUvarint(nil, n<<3|2)
	b = binary.AppendUvarint(b, uint64(len(data)))
	return append(b, data...)
}

func fieldInt(n, value uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(nil, n<<3), value)
}

func geoIPFixture(code string, ip []byte, prefix uint64) []byte {
	cidr := append(fieldBytes(1, ip), fieldInt(2, prefix)...)
	entry := append(fieldBytes(1, []byte(code)), fieldBytes(2, cidr)...)
	return fieldBytes(1, entry)
}

func geoSiteFixture(code, domain string, typ uint64) []byte {
	d := append(fieldInt(1, typ), fieldBytes(2, []byte(domain))...)
	entry := append(fieldBytes(1, []byte(code)), fieldBytes(2, d)...)
	return fieldBytes(1, entry)
}

func TestNativeDataValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind Kind
		data []byte
		tags []string
	}{
		{"ipv4", GeoIP, geoIPFixture("FI", []byte{1, 2, 3, 0}, 24), []string{"fi"}},
		{"ipv6", GeoIP, geoIPFixture("FI", make([]byte, 16), 128), []string{"FI"}},
		{"site", GeoSite, geoSiteFixture("YOUTUBE", "youtube.com", 2), []string{"youtube"}},
		{"regex", GeoSite, geoSiteFixture("TEST", "^example[.]com$", 1), []string{"test"}},
		{"published-non-cn", GeoSite, geoSiteFixture("CATEGORY-AI-!CN", "example.com", 2), []string{"category-ai-!cn"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.kind, tc.data, tc.tags); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeDataRejectsCorruptionAndWrongTags(t *testing.T) {
	valid := geoIPFixture("FI", []byte{1, 2, 3, 0}, 24)
	for _, data := range [][]byte{nil, []byte("<html>error</html>"), valid[:len(valid)-1], append(valid, 0), append(valid, valid...), geoIPFixture("FI", []byte{1, 2, 3}, 24), geoIPFixture("FI", []byte{1, 2, 3, 4}, 33), geoIPFixture("FI", make([]byte, 16), 129)} {
		if Validate(GeoIP, data, []string{"fi"}) == nil {
			t.Fatalf("accepted corrupt data %x", data)
		}
	}
	if Validate(GeoIP, valid, []string{"de"}) == nil {
		t.Fatal("missing country accepted")
	}
	if Validate(GeoSite, geoSiteFixture("TEST", "[", 1), []string{"test"}) == nil {
		t.Fatal("invalid regex accepted")
	}
	if Validate(GeoSite, geoSiteFixture("TEST", "", 2), []string{"test"}) == nil {
		t.Fatal("empty domain accepted")
	}
	if Validate(GeoSite, geoSiteFixture("TEST", "example.com", 4), []string{"test"}) == nil {
		t.Fatal("unknown domain type accepted")
	}
	if Validate(Kind("other"), valid, []string{"fi"}) == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestSourcePlanSelectedCountriesOnly(t *testing.T) {
	rev := strings.Repeat("a", 40)
	plan, err := SourcePlan(Selection{CountryCodes: []string{"FI", "de", "fi"}, GeoSiteTags: []string{"youtube", "category-ads"}}, Revisions{GeoIP: rev, GeoSite: strings.Repeat("b", 40)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 {
		t.Fatalf("plan count %d", len(plan))
	}
	if plan[0].URL != "https://raw.githubusercontent.com/v2fly/geoip/"+rev+"/dat/de.dat" || plan[1].Tags[0] != "fi" || plan[2].Kind != GeoSite {
		t.Fatalf("unexpected plan %+v", plan)
	}
	for _, s := range plan {
		if strings.Contains(s.URL, "/release/") || strings.HasSuffix(s.URL, "/geoip.dat") {
			t.Fatal("mutable/full source")
		}
	}
	if _, err := SourcePlan(Selection{CountryCodes: []string{"fi"}}, Revisions{GeoIP: "release"}); err == nil {
		t.Fatal("mutable revision accepted")
	}
	if _, err := SourcePlan(Selection{CountryCodes: []string{"../fi"}}, Revisions{GeoIP: rev}); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, err := SourcePlan(Selection{GeoSiteTags: []string{"x@other"}}, Revisions{GeoSite: rev}); err == nil {
		t.Fatal("attribute injection accepted")
	}
	if plan, err := SourcePlan(Selection{}, Revisions{}); err != nil || len(plan) != 0 {
		t.Fatal("empty selection should not download")
	}
}

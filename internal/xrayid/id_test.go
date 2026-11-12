package xrayid

import (
	"strings"
	"testing"
)

func TestPinnedIDForms(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"клиент:357", "1faa3d77-e5ff-58a7-b6cd-9ca0a7199b6d"},
		{"synthetic", "e3b07f18-b6cf-500d-bd77-77c9c9e843d7"},
		{"00000000AABB4CCD8000000000000001", "00000000-aabb-4ccd-8000-000000000001"},
		{"00000000-aabb4ccd8000000000000001", "00000000-aabb-4ccd-8000-000000000001"},
		{"00000000-AABB-4CCD-8000-000000000001", "00000000-aabb-4ccd-8000-000000000001"},
	} {
		got, ok := Canonical(tc.input)
		if !ok || got != tc.want {
			t.Fatal("accepted core ID changed")
		}
	}
	if !Valid(strings.Repeat("ж", 15)) || Valid(strings.Repeat("ж", 16)) {
		t.Fatal("ID limit is bytes, not runes")
	}
}
func TestRejectMalformedID(t *testing.T) {
	for _, s := range []string{"", strings.Repeat("a", 31), strings.Repeat("z", 32), strings.Repeat("a", 37), "00000000aabb4ccd8000000000000001junk", "urn:uuid:00000000-aabb-4ccd-8000-000000000001"} {
		if Valid(s) {
			t.Fatal("malformed/ambiguous ID accepted")
		}
	}
}

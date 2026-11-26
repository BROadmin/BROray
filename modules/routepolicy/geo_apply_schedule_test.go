package routepolicy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGeoApplyScheduleStrictClockAndLegacyIsolation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	checks, err := ReadGeoSchedule(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveGeoSchedule(ctx, root, GeoScheduleRevision(checks), true); err != nil {
		t.Fatal(err)
	}
	dailyPath := filepath.Join(root, GeoScheduleFile)
	before, err := os.ReadFile(dailyPath)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := ReadGeoApplySchedule(ctx, root)
	if err != nil || initial.AtUTC != "" {
		t.Fatal("invalid default apply setting", initial, err)
	}
	for _, input := range []string{`"9:45"`, `"24:00"`, `"09:60"`, `"09:45Z"`, `null`, `false`} {
		if _, _, err := DecodeGeoApplyScheduleChange([]byte(`{"applyAtUTC":` + input + `,"expectedRevision":"` + initial.Revision + `"}`)); err == nil {
			t.Fatalf("accepted malformed time %s", input)
		}
	}
	if _, _, err := DecodeGeoApplyScheduleChange([]byte(`{"ApplyAtUTC":"09:45","expectedRevision":"` + initial.Revision + `"}`)); err == nil {
		t.Fatal("accepted case alias")
	}
	set, err := SaveGeoApplySchedule(ctx, root, initial.Revision, "09:45")
	if err != nil || set.Revision == initial.Revision {
		t.Fatal("configuration revision did not advance", err)
	}
	now := time.Date(2026, 11, 24, 12, 45, 20, 0, time.FixedZone("UTC+3", 3*60*60))
	if actual := set.DueDay(now); actual != "2026-11-24" {
		t.Fatal("ambiguous clock basis", actual)
	}
	if err := ClaimGeoApply(ctx, root, set.Revision, "2026-11-24", now); err != nil {
		t.Fatal(err)
	}
	for _, when := range []time.Time{now, now.Add(-24 * time.Hour)} {
		if err := ClaimGeoApply(ctx, root, set.Revision, when.UTC().Format(time.DateOnly), when); !errors.Is(err, ErrGeoApplyWindow) {
			t.Fatal("scheduled tick was replayed", when, err)
		}
	}
	changed, err := SaveGeoApplySchedule(ctx, root, set.Revision, "")
	if err != nil || changed.LastAttemptDay != "2026-11-24" || changed.DueDay(now.Add(24*time.Hour)) != "" {
		t.Fatal("disable discarded claim or kept scheduled authority", changed, err)
	}
	if _, err := SaveGeoApplySchedule(ctx, root, set.Revision, "09:45"); !errors.Is(err, ErrGeoScheduleChanged) {
		t.Fatal("stale settings read could be reused", err)
	}
	after, err := os.ReadFile(dailyPath)
	if err != nil || string(after) != string(before) {
		t.Fatal("new schedule changed the legacy checks file", err)
	}
	applyPath := filepath.Join(root, GeoApplyScheduleFile)
	if err := os.WriteFile(applyPath, []byte(`{"schemaVersion":1,"atUTC":"09:45","revision":"`+strings.Repeat("a", 64)+`","LastAttemptDay":"2026-11-24"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadGeoApplySchedule(ctx, root); err == nil {
		t.Fatal("persisted replay ledger accepts alias")
	}
}

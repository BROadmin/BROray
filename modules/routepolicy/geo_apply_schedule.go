package routepolicy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const GeoApplyScheduleFile = "geodata-apply-schedule.v1.json"

var ErrGeoApplyWindow = errors.New("geodata apply time is no longer available")

type GeoApplySchedule struct {
	SchemaVersion  int    `json:"schemaVersion"`
	AtUTC          string `json:"atUTC"`
	Revision       string `json:"revision"`
	LastAttemptDay string `json:"lastAttemptDay,omitempty"`
}

func ValidGeoApplyTime(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := time.Parse("15:04", value)
	return err == nil && value == parsed.Format("15:04")
}

func validGeoApplyDay(value string) bool {
	parsed, err := time.Parse(time.DateOnly, value)
	return err == nil && value == parsed.Format(time.DateOnly)
}

// DueDay authorizes only the configured UTC minute. Missed windows are not
// caught up after the router starts, and a clock reversal cannot replay a day.
func (s GeoApplySchedule) DueDay(now time.Time) string {
	now = now.UTC()
	day := now.Format(time.DateOnly)
	if now.Format("15:04") == s.AtUTC && s.LastAttemptDay < day {
		return day
	}
	return ""
}

func ReadGeoApplySchedule(ctx context.Context, root string) (GeoApplySchedule, error) {
	return editGeoApplySchedule(ctx, root, nil)
}

func SaveGeoApplySchedule(ctx context.Context, root, expected, at string) (GeoApplySchedule, error) {
	if !ValidGeoApplyTime(at) {
		return GeoApplySchedule{}, errors.New("invalid geodata apply time")
	}
	return editGeoApplySchedule(ctx, root, func(s *GeoApplySchedule) error {
		if s.Revision != expected {
			return ErrGeoScheduleChanged
		}
		if s.AtUTC == at {
			return nil
		}
		var revision [32]byte
		if _, err := rand.Read(revision[:]); err != nil {
			return err
		}
		s.AtUTC = at
		s.Revision = hex.EncodeToString(revision[:])
		return nil
	})
}

// ClaimGeoApply records that this day was used before the policy writer may
// change live files. It prevents replay even after a crash or failed apply.
func ClaimGeoApply(ctx context.Context, root, expected, day string, now time.Time) error {
	_, err := editGeoApplySchedule(ctx, root, func(s *GeoApplySchedule) error {
		if s.Revision != expected {
			return ErrGeoScheduleChanged
		}
		if day == "" || s.DueDay(now) != day {
			return ErrGeoApplyWindow
		}
		s.LastAttemptDay = day
		return nil
	})
	return err
}

func DecodeGeoApplyScheduleChange(data []byte) (string, string, error) {
	var v struct {
		AtUTC    string `json:"applyAtUTC"`
		Revision string `json:"expectedRevision"`
	}
	if len(data) > 4096 {
		return "", "", errors.New("schedule request too large")
	}
	if err := geoJSONFields(data, []string{"applyAtUTC", "expectedRevision"}, nil); err != nil {
		return "", "", err
	}
	if err := strictJournalJSON(data, &v); err != nil {
		return "", "", err
	}
	if !hashPattern.MatchString(v.Revision) || !ValidGeoApplyTime(v.AtUTC) {
		return "", "", errors.New("invalid apply schedule change")
	}
	return v.Revision, v.AtUTC, nil
}

func decodeGeoApplySchedule(data []byte) (GeoApplySchedule, error) {
	var s GeoApplySchedule
	if err := geoJSONFields(data, []string{"schemaVersion", "atUTC", "revision"}, []string{"lastAttemptDay"}); err != nil {
		return s, err
	}
	if err := strictJournalJSON(data, &s); err != nil {
		return s, err
	}
	if s.SchemaVersion != 1 || !hashPattern.MatchString(s.Revision) || !ValidGeoApplyTime(s.AtUTC) ||
		(s.LastAttemptDay != "" && !validGeoApplyDay(s.LastAttemptDay)) {
		return GeoApplySchedule{}, errors.New("invalid geodata apply schedule")
	}
	return s, nil
}

func editGeoApplySchedule(ctx context.Context, root string, edit func(*GeoApplySchedule) error) (GeoApplySchedule, error) {
	initial := GeoApplySchedule{SchemaVersion: 1, Revision: strings.Repeat("0", 64)}
	return editGeoMetadata(ctx, root, GeoApplyScheduleFile, initial, decodeGeoApplySchedule, edit)
}

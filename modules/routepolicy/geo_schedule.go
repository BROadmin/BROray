package routepolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

const GeoScheduleFile = "geodata-schedule.v1.json"
const maxGeoScheduleBytes = 8 << 10

var ErrGeoScheduleChanged = errors.New("geodata schedule changed")

// Attempts do not participate in the independent settings revision.
type GeoSchedule struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Enabled       bool                  `json:"enabled"`
	Attempts      map[string]GeoAttempt `json:"attempts"`
}

type GeoAttempt struct {
	OperationID string `json:"operationId"`
	Source      string `json:"source"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt,omitempty"`
	RetryAt     string `json:"retryAt"`
	Status      string `json:"status"`
	ErrorCode   string `json:"errorCode,omitempty"`
}

func ReadGeoSchedule(ctx context.Context, root string) (GeoSchedule, error) {
	return editGeoSchedule(ctx, root, nil)
}
func GeoScheduleRevision(s GeoSchedule) string {
	data, _ := json.Marshal(struct {
		SchemaVersion int
		Enabled       bool
	}{s.SchemaVersion, s.Enabled})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func SaveGeoSchedule(ctx context.Context, root, revision string, enabled bool) (GeoSchedule, error) {
	return editGeoSchedule(ctx, root, func(s *GeoSchedule) error {
		if revision != GeoScheduleRevision(*s) {
			return ErrGeoScheduleChanged
		}
		s.Enabled = enabled
		return nil
	})
}

// Caller holds the policy mutation fence. Finish also compares operation ID.
func WriteGeoAttempt(ctx context.Context, root, kind, previous string, attempt GeoAttempt) (GeoSchedule, error) {
	if (kind != "geoip" && kind != "geosite") || attempt.validate() != nil {
		return GeoSchedule{}, errors.New("invalid geodata attempt")
	}
	if previous == "" && attempt.Status != "running" || previous != "" && (attempt.OperationID != previous || attempt.Status == "running") {
		return GeoSchedule{}, errors.New("invalid geodata attempt transition")
	}
	return editGeoSchedule(ctx, root, func(s *GeoSchedule) error {
		if previous != "" && (s.Attempts[kind].OperationID != previous || s.Attempts[kind].Status != "running") {
			return ErrGeoScheduleChanged
		}
		s.Attempts[kind] = attempt
		return nil
	})
}
func DecodeGeoScheduleChange(data []byte) (string, bool, error) {
	var v struct {
		Enabled          *bool  `json:"enabled"`
		ExpectedRevision string `json:"expectedRevision"`
	}
	if len(data) > 4096 {
		return "", false, errors.New("schedule request too large")
	}
	if err := geoJSONFields(data, []string{"enabled", "expectedRevision"}, nil); err != nil {
		return "", false, err
	}
	if err := strictJournalJSON(data, &v); err != nil {
		return "", false, err
	}
	if v.Enabled == nil || !hashPattern.MatchString(v.ExpectedRevision) {
		return "", false, errors.New("invalid schedule change")
	}
	return v.ExpectedRevision, *v.Enabled, nil
}

func (a GeoAttempt) validate() error {
	validToken := func(s string, limit int) bool {
		if len(s) == 0 || len(s) > limit {
			return false
		}
		for _, c := range s {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
		return true
	}
	if !validToken(a.OperationID, 128) || (a.Source != "USER" && a.Source != "SCHEDULER") {
		return errors.New("invalid geodata attempt identity")
	}
	start, err := time.Parse(time.RFC3339Nano, a.StartedAt)
	if err != nil {
		return err
	}
	retry, err := time.Parse(time.RFC3339Nano, a.RetryAt)
	if err != nil {
		return err
	}
	if retry.Before(start) {
		return errors.New("geodata retry precedes attempt")
	}
	if a.Status == "running" {
		if a.CompletedAt != "" || a.ErrorCode != "" {
			return errors.New("running attempt contains result")
		}
		return nil
	}
	if a.Status != "ok" && a.Status != "failed" {
		return errors.New("invalid geodata attempt status")
	}
	if _, err := time.Parse(time.RFC3339Nano, a.CompletedAt); err != nil {
		return err
	}
	if a.Status == "ok" && a.ErrorCode != "" || a.Status == "failed" && !validToken(a.ErrorCode, 96) {
		return errors.New("invalid geodata result code")
	}
	return nil
}

func decodeGeoSchedule(data []byte) (GeoSchedule, error) {
	if err := geoJSONFields(data, []string{"schemaVersion", "enabled", "attempts"}, nil); err != nil {
		return GeoSchedule{}, err
	}
	var v struct {
		SchemaVersion int                        `json:"schemaVersion"`
		Enabled       *bool                      `json:"enabled"`
		Attempts      map[string]json.RawMessage `json:"attempts"`
	}
	if err := strictJournalJSON(data, &v); err != nil {
		return GeoSchedule{}, err
	}
	if v.SchemaVersion != 1 || v.Enabled == nil || v.Attempts == nil || len(v.Attempts) > 2 {
		return GeoSchedule{}, errors.New("invalid geodata schedule schema")
	}
	s := GeoSchedule{SchemaVersion: 1, Enabled: *v.Enabled, Attempts: map[string]GeoAttempt{}}
	for kind, raw := range v.Attempts {
		if kind != "geoip" && kind != "geosite" {
			return GeoSchedule{}, errors.New("invalid geodata kind")
		}
		var a GeoAttempt
		if err := geoJSONFields(raw, []string{"operationId", "source", "startedAt", "retryAt", "status"}, []string{"completedAt", "errorCode"}); err != nil {
			return GeoSchedule{}, err
		}
		if err := strictJournalJSON(raw, &a); err != nil {
			return GeoSchedule{}, err
		}
		if err := a.validate(); err != nil {
			return GeoSchedule{}, err
		}
		s.Attempts[kind] = a
	}
	return s, nil
}

// encoding/json accepts case aliases; saved metadata and changes require the
// same canonical spelling. strictJournalJSON separately rejects duplicates.
func geoJSONFields(data []byte, required, optional []string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return errors.New("missing geodata field")
		}
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, value := range fields {
		if !allowed[key] || strings.TrimSpace(string(value)) == "null" {
			return errors.New("unknown or null geodata field")
		}
	}
	return nil
}

func editGeoSchedule(ctx context.Context, root string, edit func(*GeoSchedule) error) (GeoSchedule, error) {
	initial := GeoSchedule{SchemaVersion: 1, Attempts: map[string]GeoAttempt{}}
	return editGeoMetadata(ctx, root, GeoScheduleFile, initial, decodeGeoSchedule, edit)
}

func editGeoMetadata[T any](ctx context.Context, root, name string, initial T, decode func([]byte) (T, error), edit func(*T) error) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	dir, err := platform.OpenDirNoFollow(root)
	if err != nil {
		return zero, err
	}
	defer dir.Close()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		visible, err := platform.OpenDirNoFollow(root)
		if err != nil {
			return err
		}
		defer visible.Close()
		a, ea := dir.Stat()
		b, eb := visible.Stat()
		if ea != nil || eb != nil || !os.SameFile(a, b) {
			return errors.New("geodata schedule root changed")
		}
		return nil
	}
	if err = unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return zero, err
	}
	defer unix.Flock(int(dir.Fd()), unix.LOCK_UN)
	if err = check(); err != nil {
		return zero, err
	}
	before, err := readImage(dir, name, maxGeoScheduleBytes)
	if err != nil {
		return zero, err
	}
	s := initial
	if before.Exists {
		s, err = decode(before.Data)
		if err != nil {
			return zero, err
		}
	}
	if edit != nil {
		if err = edit(&s); err != nil {
			return zero, err
		}
		data, err := json.Marshal(s)
		if err != nil {
			return zero, err
		}
		if _, err = decode(data); err != nil {
			return zero, err
		}
		if err = check(); err != nil {
			return zero, err
		}
		next := makeImage(data)
		if err = replaceImage(dir, name, next, before, maxGeoScheduleBytes); err != nil {
			return zero, err
		}
		if err = check(); err != nil {
			return zero, fmt.Errorf("%w: %v", ErrJournalUncertain, err)
		}
		after, err := readImage(dir, name, maxGeoScheduleBytes)
		if err != nil || !after.equal(next) {
			return zero, fmt.Errorf("%w: schedule readback mismatch", ErrJournalUncertain)
		}
	}
	if err = check(); err != nil {
		return zero, err
	}
	return s, nil
}

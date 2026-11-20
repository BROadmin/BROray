package routepolicy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestGeoScheduleDefaultAndIndependentCAS(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	s, err := ReadGeoSchedule(ctx, root)
	if err != nil || s.Enabled || s.SchemaVersion != 1 || s.Attempts == nil {
		t.Fatalf("missing-only default: %+v %v", s, err)
	}
	if _, err := os.Stat(filepath.Join(root, GeoScheduleFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read created file: %v", err)
	}
	rev := GeoScheduleRevision(s)
	s, err = SaveGeoSchedule(ctx, root, rev, true)
	if err != nil || !s.Enabled || GeoScheduleRevision(s) == rev {
		t.Fatalf("enable: %+v %v", s, err)
	}
	if _, err = SaveGeoSchedule(ctx, root, rev, false); !errors.Is(err, ErrGeoScheduleChanged) {
		t.Fatalf("stale toggle: %v", err)
	}
	enabledRev := GeoScheduleRevision(s)
	a := GeoAttempt{OperationID: "operation-1", Source: "USER", StartedAt: "2026-11-17T00:00:00Z", RetryAt: "2026-11-17T00:15:00Z", Status: "running"}
	s, err = WriteGeoAttempt(ctx, root, "geoip", "", a)
	if err != nil || !s.Enabled || s.Attempts["geoip"] != a || GeoScheduleRevision(s) != enabledRev {
		t.Fatalf("status invalidated setting: %+v %v", s, err)
	}
	s, err = SaveGeoSchedule(ctx, root, enabledRev, false)
	if err != nil || s.Enabled || s.Attempts["geoip"] != a {
		t.Fatalf("toggle lost attempt: %+v %v", s, err)
	}
	a.Status = "failed"
	a.CompletedAt = "2026-11-17T00:01:00Z"
	a.RetryAt = "2026-11-17T00:16:00Z"
	a.ErrorCode = "GEODATA_FETCH_FAILED"
	s, err = WriteGeoAttempt(ctx, root, "geoip", "operation-1", a)
	if err != nil || s.Enabled || s.Attempts["geoip"] != a {
		t.Fatalf("finish lost setting: %+v %v", s, err)
	}
	a.OperationID = "operation-2"
	a.Status = "running"
	a.CompletedAt = ""
	a.ErrorCode = ""
	if _, err = WriteGeoAttempt(ctx, root, "geoip", "", a); err != nil {
		t.Fatal(err)
	}
	a.OperationID = "operation-1"
	a.Status = "ok"
	a.CompletedAt = "2026-11-17T00:01:00Z"
	if _, err = WriteGeoAttempt(ctx, root, "geoip", "operation-1", a); !errors.Is(err, ErrGeoScheduleChanged) {
		t.Fatalf("stale finisher: %v", err)
	}
	s, err = ReadGeoSchedule(ctx, root)
	if err != nil || s.Enabled || s.Attempts["geoip"].OperationID != "operation-2" {
		t.Fatalf("readback: %+v %v", s, err)
	}
}

func TestGeoScheduleStrictSavedData(t *testing.T) {
	for _, data := range []string{
		`null`, `{}`, `{"schemaVersion":1,"enabled":null,"attempts":{}}`,
		`{"schemaVersion":1,"enabled":false,"attempts":null}`, `{"schemaVersion":1,"enabled":false}`,
		`{"schemaVersion":1,"enabled":false,"Enabled":true,"attempts":{}}`,
		`{"schemaVersion":1,"enabled":false,"attempts":{},"extra":true}`,
		`{"schemaVersion":1,"enabled":false,"attempts":{"geoip":null}}`,
		`{"schemaVersion":1,"enabled":false,"attempts":{"geoip":{}}}`,
		`{"schemaVersion":1,"enabled":false,"attempts":{"other":{}}}`, strings.Repeat(" ", 8193),
	} {
		t.Run(data[:min(50, len(data))], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, GeoScheduleFile)
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadGeoSchedule(context.Background(), root); err == nil {
				t.Fatal("invalid data accepted")
			}
			if _, err := SaveGeoSchedule(context.Background(), root, "", false); err == nil {
				t.Fatal("invalid data replaced")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != data {
				t.Fatal("invalid original mutated")
			}
		})
	}
}

func TestGeoScheduleUnsafePathsAndLock(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"symlink", "fifo", "parent", "locked"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, GeoScheduleFile)
			switch kind {
			case "symlink":
				outside := filepath.Join(t.TempDir(), "foreign")
				if err := os.WriteFile(outside, []byte(`{"schemaVersion":1,"enabled":true,"attempts":{}}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(target, 0600); err != nil {
					t.Fatal(err)
				}
			case "parent":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(root, link); err != nil {
					t.Fatal(err)
				}
				root = link
			case "locked":
				f, err := os.Open(root)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
				defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
			}
			if _, err := ReadGeoSchedule(ctx, root); err == nil {
				t.Fatal("unsafe read accepted")
			}
			if _, err := SaveGeoSchedule(ctx, root, "", false); err == nil {
				t.Fatal("unsafe write accepted")
			}
		})
	}
}

func TestGeoScheduleChangeStrict(t *testing.T) {
	rev := strings.Repeat("a", 64)
	r, enabled, err := DecodeGeoScheduleChange([]byte(`{"enabled":true,"expectedRevision":"` + rev + `"}`))
	if err != nil || !enabled || r != rev {
		t.Fatalf("valid: %q %v %v", r, enabled, err)
	}
	for _, data := range []string{`null`, `{}`, `{"enabled":null,"expectedRevision":"` + rev + `"}`, `{"enabled":true,"Enabled":false,"expectedRevision":"` + rev + `"}`, `{"enabled":true,"expectedRevision":"` + rev + `","attempts":{}}`, `{"enabled":true,"expectedRevision":"bad"}`} {
		if _, _, err := DecodeGeoScheduleChange([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestGeoScheduleExactKeysAndAttemptSchema(t *testing.T) {
	ctx := context.Background()
	for _, data := range []string{
		`{"schemaVersion":1,"Enabled":true,"attempts":{}}`,
		`{"schemaVersion":1,"enabled":false,"Attempts":{}}`,
		`{"schemaVersion":1,"enabled":false,"attempts":{"geoip":{"operationId":"op-1","source":"USER","startedAt":"bad","retryAt":"2026-11-17T00:15:00Z","status":"running"}}}`,
		`{"schemaVersion":1,"enabled":false,"attempts":{"geoip":{"operationId":"op-1","source":"USER","startedAt":"2026-11-17T00:00:00Z","retryAt":"2026-11-17T00:15:00Z","status":"ok"}}}`,
		`{"schemaVersion":1,"enabled":false,"attempts":{"geoip":{"operationId":"op-1","source":"USER","startedAt":"2026-11-17T00:00:00Z","retryAt":"2026-11-17T00:15:00Z","status":"running","errorCode":null}}}`,
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, GeoScheduleFile), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadGeoSchedule(ctx, root); err == nil {
			t.Errorf("accepted malformed saved fields: %s", data)
		}
	}
	if _, _, err := DecodeGeoScheduleChange([]byte(`{"Enabled":true,"expectedRevision":"` + strings.Repeat("a", 64) + `"}`)); err == nil {
		t.Error("accepted noncanonical setting key")
	}
}

func TestGeoScheduleParentReplacementBeforeWrite(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "routes")
	moved := filepath.Join(parent, "original")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := editGeoSchedule(context.Background(), root, func(s *GeoSchedule) error {
		if err := os.Rename(root, moved); err != nil {
			return err
		}
		if err := os.Mkdir(root, 0700); err != nil {
			return err
		}
		s.Enabled = true
		return nil
	})
	if err == nil {
		t.Fatal("replaced root accepted")
	}
	for _, p := range []string{root, moved} {
		if _, err := os.Stat(filepath.Join(p, GeoScheduleFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("wrote into replaced root")
		}
	}
}

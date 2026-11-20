package servers

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func inactiveFixture(t *testing.T) (*fakeEnv, string, string, string) {
	t.Helper()
	e := newEnv(t)
	a := e.addServer(t, "trojan://pw@a.example:443#A")
	b := e.addServer(t, "trojan://pw@b.example:443#B")
	c := e.addServer(t, "trojan://pw@c.example:443#C")
	if err := setActiveID(e.d, a); err != nil {
		t.Fatal(err)
	}
	return e, a, b, c
}

func TestDeleteInactiveAPIBoundaries(t *testing.T) {
	for _, kind := range []string{"get", "no-header", "foreign-origin", "duplicate", "unknown", "valid"} {
		t.Run(kind, func(t *testing.T) {
			e, a, _, _ := inactiveFixture(t)
			method := "POST"
			body := `{"activeId":` + jsonString(a) + `}`
			headers := map[string]string{"X-BROray-Request": "operations", "Origin": "http://example.com"}
			want := 400
			switch kind {
			case "get":
				method = "GET"
				want = 405
			case "no-header":
				headers = nil
				want = 403
			case "foreign-origin":
				headers["Origin"] = "https://foreign.example"
				want = 403
			case "duplicate":
				body = `{"activeId":` + jsonString(a) + `,"activeId":` + jsonString(a) + `}`
			case "unknown":
				body = `{"activeId":` + jsonString(a) + `,"extra":true}`
			case "valid":
				want = 200
			}
			r := call(t, method, "/api/servers/delete-inactive.cgi", body, headers)
			if r.Code != want {
				t.Fatalf("status%d expected%d: %s", r.Code, want, r.Raw)
			}
			ids, err := RecordIDs(e.d)
			if err != nil {
				t.Fatal(err)
			}
			if want == 200 {
				if len(ids) != 1 || ids[0] != a {
					t.Fatal("API did not keep active")
				}
			} else if len(ids) != 3 {
				t.Fatal("rejected request deleted records")
			}
		})
	}
}

func TestDeleteInactivePreservesActiveAndRemovesOtherSources(t *testing.T) {
	e, a, b, c := inactiveFixture(t)
	r, err := ReadRecord(e.d, c)
	if err != nil {
		t.Fatal(err)
	}
	r["source"] = map[string]any{"type": "subscription", "subscriptionId": "sub-00000000000000aa", "nodeIndex": 1, "enabled": true}
	if err := WriteRecord(e.d, r); err != nil {
		t.Fatal(err)
	}
	active, _ := os.ReadFile(RecordPath(e.d, a))
	config := []byte("unchanged active configuration")
	if err := os.WriteFile(e.d.Root.XrayConfig(), config, 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, b, c} {
		path := QualityPath(e.d, id)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"status":"available"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := e.s.deleteInactive(a)
	if err != nil {
		t.Fatal(err)
	}
	result := out.(map[string]any)
	if result["removed"] != 2 || result["activeId"] != a {
		t.Fatal(result)
	}
	after, _ := os.ReadFile(RecordPath(e.d, a))
	afterConfig, _ := os.ReadFile(e.d.Root.XrayConfig())
	if ActiveID(e.d) != a || !bytes.Equal(active, after) || !bytes.Equal(config, afterConfig) {
		t.Fatal("active state changed")
	}
	if _, err := os.Stat(QualityPath(e.d, a)); err != nil {
		t.Fatal("active quality removed", err)
	}
	for _, id := range []string{b, c} {
		for _, path := range []string{RecordPath(e.d, id), QualityPath(e.d, id)} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("remaining deleted entry", path, err)
			}
		}
	}
	out, err = e.s.deleteInactive(a)
	if err != nil || out.(map[string]any)["removed"] != 0 {
		t.Fatal("repeat not harmless", out, err)
	}
}

func TestDeleteInactiveRefusesAmbiguousStateBeforeDeletion(t *testing.T) {
	for _, kind := range []string{"active-id-mismatch", "inactive-id-mismatch", "stale", "missing-active", "symlink", "invalid-record", "quality-directory", "quality-parent-symlink", "busy", "reserve"} {
		t.Run(kind, func(t *testing.T) {
			e, a, b, c := inactiveFixture(t)
			expected := a
			switch kind {
			case "active-id-mismatch", "inactive-id-mismatch":
				id, other := b, a
				if kind == "active-id-mismatch" {
					id, other = a, b
				}
				record, err := ReadRecord(e.d, id)
				if err != nil {
					t.Fatal(err)
				}
				record["id"] = other
				raw, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(RecordPath(e.d, id), raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "stale":
				expected = b
			case "missing-active":
				if err := os.Remove(ActiveFile(e.d)); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				path := RecordPath(e.d, b)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(RecordPath(e.d, a), path); err != nil {
					t.Fatal(err)
				}
			case "invalid-record":
				if err := os.WriteFile(RecordPath(e.d, b), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "quality-directory":
				if err := os.MkdirAll(QualityPath(e.d, b), 0700); err != nil {
					t.Fatal(err)
				}
			case "quality-parent-symlink":
				dir := filepath.Dir(QualityPath(e.d, b))
				if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), dir); err != nil {
					t.Fatal(err)
				}
			case "busy":
				release, ok := e.d.Lock.TryAcquire("test", "hold", "")
				if !ok {
					t.Fatal("fixture lock")
				}
				defer release()
			case "reserve":
				writeReserveAutomatic(t, e, a, b, true)
			}
			if _, err := e.s.deleteInactive(expected); err == nil {
				t.Fatal("ambiguous bulk deletion allowed")
			}
			for _, id := range []string{a, b, c} {
				if _, err := os.Lstat(RecordPath(e.d, id)); err != nil {
					t.Fatal("preflight already deleted a record", id, err)
				}
			}
		})
	}
}

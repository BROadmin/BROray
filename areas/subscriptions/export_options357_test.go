package subscriptions

import (
	"fmt"
	"strings"
	"testing"
)

func TestExportOptions357(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		json, clash bool
		want        int
	}{
		{"default", "", false, false, 1},
		{"json", `,"fetchJSON":true`, true, false, 2},
		{"clash", `,"fetchClash":true`, false, true, 2},
		{"both", `,"fetchJSON":true,"fetchClash":true`, true, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			p := newAutoFixture355(t)
			id := e.create(t, p.URL+"/sub/invented", tc.extra)
			if err := e.refreshSync(t, id); err != nil {
				t.Fatal(err)
			}
			if n := len(liveIDs(t, e)); n != tc.want {
				t.Fatalf("want %d records got %d", tc.want, n)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			requested := func(prefix string) bool {
				for key, n := range p.requests {
					if strings.HasPrefix(key, prefix+"|") && n > 0 {
						return true
					}
				}
				return false
			}
			if requested("/download/video/invented") != tc.json || requested("/download/lesson/invented") != tc.clash {
				t.Fatal("additional requests differ from selected formats")
			}
			if !tc.json && !tc.clash && p.requests["/sub/invented|text/html"] != 0 {
				t.Fatal("default performed discovery")
			}
			r := e.record(t, id)
			if r["fetchJSON"] != tc.json || r["fetchClash"] != tc.clash {
				t.Fatal("settings not persisted")
			}
		})
	}
}

func TestExportOptions357DirectAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		want       int
	}{{"json", "/download/video/invented", 2}, {"clash", "/download/lesson/invented", 1}, {"legacy", "/sub/invented", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			p := newAutoFixture355(t)
			id := e.create(t, p.URL+tc.path, "")
			r := e.record(t, id)
			delete(r, "fetchJSON")
			delete(r, "fetchClash")
			r["alternateExports"] = []any{"json", "clash"}
			if err := e.s.writeRecord(r); err != nil {
				t.Fatal(err)
			}
			if err := e.refreshSync(t, id); err != nil {
				t.Fatal(err)
			}
			if len(liveIDs(t, e)) != tc.want {
				t.Fatal("direct payload or legacy default broken")
			}
		})
	}
}

func TestExportOptions357UpdateValidation(t *testing.T) {
	e := newEnv(t)
	p := newAutoFixture355(t)
	id := e.create(t, p.URL+"/sub/invented", `,"fetchJSON":true,"fetchClash":true`)
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	// Saving settings does not delete existing nodes. A successful refresh reconciles them.
	if _, err := e.s.updateSettings(id, map[string]any{"fetchJSON": false}); err != nil {
		t.Fatal(err)
	}
	if len(liveIDs(t, e)) != 3 {
		t.Fatal("settings save removed nodes")
	}
	r := e.record(t, id)
	if r["fetchJSON"] != false || r["fetchClash"] != true {
		t.Fatal("independent update lost setting")
	}
	p.change(func() { p.jsonStatus = 500 })
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal("disabled broken endpoint blocked update", err)
	}
	if len(liveIDs(t, e)) != 2 {
		t.Fatal("disabled alternate records not reconciled")
	}
	for _, key := range []string{"fetchJSON", "fetchClash"} {
		for _, v := range []any{"true", 1, nil} {
			if validateBody(map[string]any{key: v}) == nil {
				t.Fatal(fmt.Sprint(key, " accepts nonboolean"))
			}
			bad := clone(r)
			bad[key] = v
			if validRecord(bad) {
				t.Fatal("invalid persisted setting accepted")
			}
		}
	}
}

func (e *env) createWithExports357(t *testing.T, url, extra string) string {
	t.Helper()
	return e.create(t, url, `,"fetchJSON":true,"fetchClash":true`+extra)
}

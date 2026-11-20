package subscriptions

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/areas/servers"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dpiSourceLink(password string) string {
	return "trojan://" + password + "@dpi.example:443?type=tcp&security=tls&sni=dpi.example#DPI"
}
func dpiSourceManaged(rec servers.Record) {
	tr := rec["transport"].(map[string]any)
	old, present := tr["finalMask"]
	rec["dpi"] = map[string]any{"schema": 1, "preset": "tlshello-v1", "originalMaskPresent": present, "originalMask": old}
	tr["finalMask"] = map[string]any{"tcp": []any{map[string]any{"type": "fragment", "settings": map[string]any{"packets": "tlshello", "length": "100-200", "delay": "1-2", "maxSplit": 64}}}}
}
func dpiSourceFixture(t *testing.T) (*env, string) {
	t.Helper()
	e := newEnv(t)
	if _, err := e.sync(t, "replace", dpiSourceLink("old")); err != nil {
		t.Fatal(err)
	}
	id := liveIDs(t, e)[0]
	rec, err := servers.ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	dpiSourceManaged(rec)
	if err := servers.WriteRecord(e.d, rec); err != nil {
		t.Fatal(err)
	}
	return e, id
}
func TestDPISourceUnchangedChoice(t *testing.T) {
	for _, policy := range []string{"replace", "retain-unmatched"} {
		t.Run(policy, func(t *testing.T) {
			e, id := dpiSourceFixture(t)
			path := servers.RecordPath(e.d, id)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.sync(t, policy, dpiSourceLink("old"))
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) || result.Unchanged != 1 || result.Total != 1 {
				t.Fatal("local DPI choice or exact unchanged record lost", result, err)
			}
		})
	}
}
func TestDPISourceChangedRequiresValidation(t *testing.T) {
	e, id := dpiSourceFixture(t)
	old, err := os.ReadFile(servers.RecordPath(e.d, id))
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.sync(t, "replace", dpiSourceLink("new"))
	var se *subErr
	if !errors.As(err, &se) || se.Code != "DPI_REVALIDATION_REQUIRED" {
		t.Fatal("changed managed profile committed without owned native validation", err)
	}
	after, readErr := os.ReadFile(servers.RecordPath(e.d, id))
	if readErr != nil || !bytes.Equal(old, after) {
		t.Fatal("old catalog changed before validation", readErr)
	}
}

func TestDPISourceNewNodesAndPartialRetention(t *testing.T) {
	for _, mode := range []string{"new", "partial", "rename"} {
		t.Run(mode, func(t *testing.T) {
			e, id := dpiSourceFixture(t)
			links := []string{dpiSourceLink("old"), "trojan://new@another.example:443#New"}
			policy := "replace"
			if mode == "partial" {
				links = links[1:]
				policy = "retain-unmatched"
			}
			if mode == "rename" {
				links = []string{strings.Replace(dpiSourceLink("old"), "#DPI", "#Renamed", 1)}
			}
			if _, err := e.sync(t, policy, links...); err != nil {
				t.Fatal(err)
			}
			got, err := servers.ReadRecord(e.d, id)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := got["dpi"]; !ok {
				t.Fatal("local choice lost")
			}
			for _, other := range liveIDs(t, e) {
				if other != id {
					rec, err := servers.ReadRecord(e.d, other)
					if err != nil {
						t.Fatal(err)
					}
					if _, on := rec["dpi"]; on {
						t.Fatal("new server inherited DPI")
					}
				}
			}
			if mode == "rename" && got["name"] != "Renamed" {
				t.Fatal("rename lost")
			}
		})
	}
}
func TestDPISourceDisabledChoice(t *testing.T) {
	e, id := dpiSourceFixture(t)
	if err := e.s.setEnabled(subX, false); err != nil {
		t.Fatal(err)
	}
	rows := e.s.disabledServers(subX)
	if len(rows) != 1 {
		t.Fatal("disabled fixture missing")
	}
	beforeConfig := configHash(rows[0].rec)
	// setEnabled moves exact bytes; the first disabled refresh updates the
	// source.enabled metadata, while preserving the local connection choice.
	result, err := e.s.syncCatalog(sourceRefreshContext(), subX, stageLinks(t, false, dpiSourceLink("old")), false, "disabled", "replace")
	if err != nil || result.Updated != 1 {
		t.Fatal(result, err)
	}
	rows = e.s.disabledServers(subX)
	if len(rows) != 1 || rows[0].id != id || configHash(rows[0].rec) != beforeConfig || sourceOf(rows[0].rec)["enabled"] != false {
		t.Fatal("disabled choice changed")
	}
	enabled, err := servers.ManagedDPI(rows[0].rec)
	if err != nil || !enabled {
		t.Fatal("disabled DPI lost", err)
	}
	before := rows[0].raw
	result, err = e.s.syncCatalog(sourceRefreshContext(), subX, stageLinks(t, false, dpiSourceLink("old")), false, "disabled", "replace")
	if err != nil || result.Unchanged != 1 {
		t.Fatal(result, err)
	}
	rows = e.s.disabledServers(subX)
	if len(rows) != 1 || !bytes.Equal(rows[0].raw, before) {
		t.Fatal("repeat disabled refresh rewrote unchanged record")
	}
}
func TestDPISourceRejectsAmbiguousAndRemoteChoices(t *testing.T) {
	for _, kind := range []string{"custom", "forged", "conflicting-copies"} {
		t.Run(kind, func(t *testing.T) {
			e, id := dpiSourceFixture(t)
			incoming := stageLinks(t, true, dpiSourceLink("old"))
			want := "SERVER_SYNC_DPI_CONFLICT"
			switch kind {
			case "custom":
				incoming[0]["transport"].(map[string]any)["finalMask"] = map[string]any{"tcp": []any{}}
			case "forged":
				dpiSourceManaged(incoming[0])
			case "conflicting-copies":
				extra := stageLinks(t, true, strings.Replace(dpiSourceLink("old"), "sni=dpi.example", "sni=other.example", 1))[0]
				if err := servers.WriteRecord(e.d, extra); err != nil {
					t.Fatal(err)
				}
				if err := e.s.writeActive(id); err != nil {
					t.Fatal(err)
				}
				want = "SERVER_SYNC_AMBIGUOUS_IDENTITY"
			}
			before := map[string][]byte{}
			for _, f := range e.s.liveServers() {
				before[f.path] = f.raw
			}
			_, err := e.s.syncCatalog(sourceRefreshContext(), subX, incoming, true, "u1", "replace")
			var se *subErr
			if !errors.As(err, &se) || se.Code != want {
				t.Fatal("unsafe match accepted", kind, err)
			}
			for path, raw := range before {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, got) {
					t.Fatal("old catalog changed", err)
				}
			}
		})
	}
}
func TestDPISourceValidationFencePrecedesPruningAndRuntime(t *testing.T) {
	e, id := dpiSourceFixture(t)
	extra := stageLinks(t, true, "trojan://stale@stale.example:443#Stale")[0]
	if err := servers.WriteRecord(e.d, extra); err != nil {
		t.Fatal(err)
	}
	if err := e.s.writeActive(id); err != nil {
		t.Fatal(err)
	}
	calls := 0
	e.d.Svc.ServersPrepareSourceRefreshCatalog = func(context.Context, string, []string) error { calls++; return nil }
	e.d.Svc.ServersPrepareSourceRefresh = func(context.Context, string, []string, []string) (string, error) { calls++; return "", nil }
	e.d.Svc.ServersReapplySourceRefresh = func(context.Context, string, string, string) (string, error) { calls++; return "", nil }
	before := map[string][]byte{}
	for _, f := range e.s.liveServers() {
		before[f.path] = f.raw
	}
	_, err := e.sync(t, "replace", dpiSourceLink("changed"))
	var se *subErr
	if !errors.As(err, &se) || se.Code != "DPI_REVALIDATION_REQUIRED" || calls != 0 {
		t.Fatal("validation fence too late", err, calls)
	}
	for path, raw := range before {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, got) {
			t.Fatal("catalog pruned or overwritten", err)
		}
	}
	markers, err := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
	if err != nil || len(markers) != 0 {
		t.Fatal("marker created before validation", markers, err)
	}
	if servers.ActiveID(e.d) != id {
		t.Fatal("selection changed")
	}
	plan, err := e.s.planNativeRefresh(subX, stageLinks(t, true, dpiSourceLink("changed")), true, "replace")
	if err != nil || len(plan.dpiChanged) != 1 || plan.dpiChanged[0]["id"] != id {
		t.Fatal("changed profile not explicitly staged for validation", err)
	}
	if _, ok := plan.dpiChanged[0]["dpi"]; !ok {
		t.Fatal("planned choice lost")
	}
}

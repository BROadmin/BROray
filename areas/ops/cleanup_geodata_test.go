package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/modules/geodata"
)

func TestCleanupGeodataConsolidatesLegacyFiles(t *testing.T) {
	a := testArea(t)
	root := filepath.Join(a.d.Root.Runtime(), geodata.AssetDirectory)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := geodata.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	// GeoIPList containing FI and the IPv4 range 1.2.3.0/24.
	asset := []byte{10, 14, 10, 2, 'F', 'I', 18, 8, 10, 4, 1, 2, 3, 0, 16, 24}
	selection := geodata.Selection{CountryCodes: []string{"fi"}}
	revisions := geodata.Revisions{GeoIP: strings.Repeat("a", 40)}
	var versions []geodata.Snapshot
	revision := ""
	for i := 0; i < 2; i++ {
		candidate, err := store.Prepare(ctx, selection, revisions,
			func(context.Context, geodata.Source) ([]byte, error) { return asset, nil },
			func(context.Context, geodata.Snapshot) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.Commit(ctx, candidate, revision)
		if err != nil {
			t.Fatal(err)
		}
		revision = snapshot.Revision
		versions = append(versions, snapshot)
	}
	old := filepath.Join(root, versions[0].Assets[0].Path)
	active := filepath.Join(root, versions[1].Assets[0].Path)
	replace := func() {
		t.Helper()
		if err := os.WriteFile(old+".fixture", asset, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(old+".fixture", old); err != nil {
			t.Fatal(err)
		}
	}
	replace() // Legacy installs save separate files.
	preview := func() string {
		t.Helper()
		r := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
		if r.Code != 200 || data(r)["candidateCount"] != float64(1) || !strings.Contains(r.Raw, "\"geodata\"") {
			t.Fatalf("legacy duplicate missing from cleanup: %s", r.Raw)
		}
		return data(r)["token"].(string)
	}
	token := preview()
	replace() // Same bytes with a changed inode must invalidate the plan.
	stale := call(t, "POST", "/api/broray/cleanup.cgi", "{\"token\":\""+token+"\"}", brorayHdr)
	if stale.Code != 409 || errCode(stale) != "CLEANUP_PLAN_CHANGED" {
		t.Fatalf("replaced object accepted: %d %s", stale.Code, stale.Raw)
	}
	applied := call(t, "POST", "/api/broray/cleanup.cgi", "{\"token\":\""+preview()+"\"}", brorayHdr)
	if applied.Code != 200 || data(applied)["consolidatedCount"] != float64(1) || data(applied)["deletedCount"] != float64(0) {
		t.Fatalf("cleanup failed: %d %s", applied.Code, applied.Raw)
	}
	first, e1 := os.Stat(old)
	second, e2 := os.Stat(active)
	if e1 != nil || e2 != nil || !os.SameFile(first, second) {
		t.Fatalf("old path not preserved as a shared file: %v, %v", e1, e2)
	}
	repeated := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
	if repeated.Code != 200 || data(repeated)["candidateCount"] != float64(0) {
		t.Fatalf("shared file is still listed as a duplicate: %s", repeated.Raw)
	}
	restored, err := store.Rollback(ctx, versions[1].Revision)
	if err != nil || restored.Revision != versions[0].Revision {
		t.Fatalf("rollback after cleanup: %v, %v", restored, err)
	}
}

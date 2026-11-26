package geodata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGeodataDedupReusesHistoricalBytes(t *testing.T) {
	store, root := testStore(t)
	changed := func(context.Context, Source) ([]byte, error) {
		return geoIPFixture("FI", []byte{2, 3, 4, 0}, 24), nil
	}
	var snapshots []Snapshot
	revision := ""
	for _, fetch := range []Fetch{goodFetch, changed, goodFetch} {
		candidate, err := store.Prepare(testContext, testSelection, testRevisions, fetch, fixtureCore)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.Commit(testContext, candidate, revision)
		if err != nil {
			t.Fatal(err)
		}
		revision = snapshot.Revision
		snapshots = append(snapshots, snapshot)
	}
	var files []os.FileInfo
	for _, snapshot := range snapshots {
		stat, err := os.Stat(filepath.Join(root, snapshot.Assets[0].Path))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, stat)
	}
	if !os.SameFile(files[0], files[2]) || os.SameFile(files[1], files[2]) {
		t.Fatal("expected identical historical bytes to share storage")
	}
	for i := 1; i >= 0; i-- {
		got, err := store.Rollback(testContext, revision)
		if err != nil || got.Revision != snapshots[i].Revision {
			t.Fatalf("rollback to generation %d: %v, %v", i, got, err)
		}
		revision = got.Revision
	}
}

package geodata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testContext = context.Background()
var testRevisions = Revisions{GeoIP: strings.Repeat("a", 40)}
var testSelection = Selection{CountryCodes: []string{"fi"}}

func TestStoreRollbackAcrossRestart(t *testing.T) {
	store, root := testStore(t)
	first, err := store.Prepare(testContext, testSelection, testRevisions, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Commit(testContext, first, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Prepare(testContext, Selection{CountryCodes: []string{"de", "fi"}}, testRevisions, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Commit(testContext, second, a.Revision)
	if err != nil {
		t.Fatal(err)
	}
	offline, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	if _, err := offline.Rollback(testContext, a.Revision); !errors.Is(err, ErrStale) {
		t.Fatalf("stale rollback: %v", err)
	}
	restored, err := offline.Rollback(testContext, b.Revision)
	if err != nil || restored.Revision != a.Revision {
		t.Fatalf("rollback: %v %v", restored, err)
	}
	for _, asset := range b.Assets {
		if _, err := os.Stat(filepath.Join(root, asset.Path)); err != nil {
			t.Fatal("rollback removed immutable generation")
		}
	}
	empty, err := offline.Rollback(testContext, a.Revision)
	if err != nil || empty.Revision != "" {
		t.Fatalf("first activation rollback: %v", err)
	}
	loaded, err := offline.Load(testContext)
	if err != nil || loaded.Revision != "" {
		t.Fatalf("empty rollback readback: %v", err)
	}
}

func goodFetch(_ context.Context, source Source) ([]byte, error) {
	return geoIPFixture(strings.ToUpper(source.Tags[0]), []byte{1, 2, 3, 0}, 24), nil
}

func TestStoreRollbackFromDamagedCurrentPayload(t *testing.T) {
	store, root := testStore(t)
	first, err := store.Prepare(testContext, testSelection, testRevisions, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Commit(testContext, first, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Prepare(testContext, testSelection, Revisions{GeoIP: strings.Repeat("b", 40)}, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	next, err := store.Commit(testContext, second, old.Revision)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, next.Assets[0].Path)
	if err := os.WriteFile(path+".damaged", []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".damaged", path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(testContext); err == nil {
		t.Fatal("damaged active payload accepted for use")
	}
	got, err := store.Rollback(testContext, next.Revision)
	if err != nil || got.Revision != old.Revision {
		t.Fatalf("valid predecessor could not be restored: %v", err)
	}
	got, err = store.Load(testContext)
	if err != nil || got.Revision != old.Revision {
		t.Fatal("rollback readback failed")
	}
}
func fixtureCore(_ context.Context, snapshot Snapshot) error { return nil }
func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, root
}

func TestStoreCommitAndOfflineLoad(t *testing.T) {
	store, root := testStore(t)
	empty, err := store.Load(testContext)
	if err != nil || empty.Revision != "" {
		t.Fatalf("empty load: %v %v", empty, err)
	}
	validated := false
	candidate, err := store.Prepare(testContext, testSelection, testRevisions, goodFetch, func(_ context.Context, snapshot Snapshot) error {
		validated = true
		if len(snapshot.Assets) != 1 || snapshot.Assets[0].SHA256 == "" {
			t.Fatal("incomplete candidate")
		}
		_, err := os.ReadFile(filepath.Join(root, snapshot.Assets[0].Path))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Load(testContext)
	if err != nil || before.Revision != "" {
		t.Fatal("prepare changed active data")
	}
	after, err := store.Commit(testContext, candidate, "")
	if err != nil {
		t.Fatal(err)
	}
	if !validated || after.Revision == "" {
		t.Fatal("unvalidated/empty commit")
	}
	offline, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	loaded, err := offline.Load(testContext)
	if err != nil || loaded.Revision != after.Revision || loaded.Selection.CountryCodes[0] != "fi" {
		t.Fatalf("offline: %v %v", loaded, err)
	}
}

func TestStoreFailuresPreserveWorkingSnapshot(t *testing.T) {
	store, _ := testStore(t)
	candidate, err := store.Prepare(testContext, testSelection, testRevisions, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Commit(testContext, candidate, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		fetch    Fetch
		validate CoreValidator
	}{
		{"network", func(context.Context, Source) ([]byte, error) { return nil, errors.New("offline") }, fixtureCore},
		{"corrupt", func(context.Context, Source) ([]byte, error) { return []byte("error"), nil }, fixtureCore},
		{"core", goodFetch, func(context.Context, Snapshot) error { return errors.New("core rejected") }},
		{"missing-core-validator", goodFetch, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.Prepare(testContext, testSelection, testRevisions, tc.fetch, tc.validate); err == nil {
				t.Fatal("invalid candidate accepted")
			}
			loaded, err := store.Load(testContext)
			if err != nil || loaded.Revision != current.Revision {
				t.Fatal("working data lost")
			}
		})
	}
}

func TestStoreStaleAndTamperedCandidate(t *testing.T) {
	store, root := testStore(t)
	first, err := store.Prepare(testContext, testSelection, testRevisions, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Prepare(testContext, testSelection, testRevisions, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Commit(testContext, first, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(testContext, second, ""); !errors.Is(err, ErrStale) {
		t.Fatalf("stale candidate: %v", err)
	}
	fresh, err := store.Prepare(testContext, testSelection, testRevisions, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, fresh.next.Assets[0].Path), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(testContext, fresh, current.Revision); err == nil || errors.Is(err, ErrStale) {
		t.Fatal("tampered candidate committed")
	}
	loaded, err := store.Load(testContext)
	if err != nil || loaded.Revision != current.Revision {
		t.Fatal("stale commit changed working data")
	}
}

func TestStoreIndependentLockAndCandidateOwnership(t *testing.T) {
	store, root := testStore(t)
	other, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	candidate, err := store.Prepare(testContext, testSelection, testRevisions, func(ctx context.Context, source Source) ([]byte, error) {
		if _, err := other.Load(ctx); err == nil {
			t.Fatal("independent store entered held transaction")
		}
		return goodFetch(ctx, source)
	}, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Commit(testContext, candidate, ""); err == nil {
		t.Fatal("other store consumed candidate")
	}
	snapshot := candidate.Snapshot()
	snapshot.Assets[0].Tags[0] = "de"
	if candidate.next.Assets[0].Tags[0] != "fi" {
		t.Fatal("exposed candidate mutable")
	}
	if err := store.Abort(testContext, candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, candidate.next.Generation)); !os.IsNotExist(err) {
		t.Fatal("aborted candidate remains")
	}
	if _, err := store.Commit(testContext, candidate, ""); err == nil {
		t.Fatal("aborted candidate committed")
	}
}

func TestStoreRejectsReplacedCandidateDirectory(t *testing.T) {
	store, root := testStore(t)
	candidate, err := store.Prepare(testContext, testSelection, testRevisions, goodFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, candidate.next.Generation)
	if err := os.Rename(path, path+"-owned"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "foreign")
	if err := os.WriteFile(marker, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(testContext, candidate, ""); err == nil {
		t.Fatal("replacement committed")
	}
	if err := store.Abort(testContext, candidate); err == nil {
		t.Fatal("replacement cleaned up")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "preserve" {
		t.Fatal("foreign entry changed")
	}
}

func TestStoreRejectsRootReplacementDuringValidation(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "managed")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.Prepare(testContext, testSelection, testRevisions, goodFetch, func(context.Context, Snapshot) error {
		if err := os.Rename(root, root+"-owned"); err != nil {
			return err
		}
		if err := os.Mkdir(root, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, "foreign"), []byte("preserve"), 0600)
	})
	if err == nil {
		t.Fatal("replaced root accepted")
	}
	data, err := os.ReadFile(filepath.Join(root, "foreign"))
	if err != nil || string(data) != "preserve" {
		t.Fatal("foreign root changed")
	}
}

func TestStoreRejectsSymlinksAndCanceledPrepare(t *testing.T) {
	store, root := testStore(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if linked, err := Open(filepath.Join(root, "linked")); err == nil {
		linked.Close()
		t.Fatal("symlink root accepted")
	}
	ctx, cancel := context.WithCancel(testContext)
	cancel()
	if _, err := store.Prepare(ctx, testSelection, testRevisions, goodFetch, fixtureCore); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled prepare: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "foreign"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "foreign"), filepath.Join(root, "current.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(testContext); err == nil {
		t.Fatal("foreign manifest accepted")
	}
	b, err := os.ReadFile(filepath.Join(outside, "foreign"))
	if err != nil || string(b) != "preserve" {
		t.Fatal("foreign bytes changed")
	}
}

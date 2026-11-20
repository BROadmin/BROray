package geodata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reuseFetch(_ context.Context, source Source) ([]byte, error) {
	if source.Kind == GeoIP {
		return geoIPFixture("FI", []byte{1, 2, 3, 0}, 24), nil
	}
	return append(geoSiteFixture("YOUTUBE", "youtube.example", 2), geoSiteFixture("CATEGORY-ADS", "ads.example", 2)...), nil
}

func TestPrepareReusesOnlyVerifiedUnchangedSources(t *testing.T) {
	store, _ := testStore(t)
	sel := Selection{CountryCodes: []string{"fi"}, GeoSiteTags: []string{"youtube"}}
	rev := Revisions{GeoIP: strings.Repeat("a", 40), GeoSite: strings.Repeat("b", 40)}
	first, err := store.Prepare(testContext, sel, rev, reuseFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Commit(testContext, first, "")
	if err != nil {
		t.Fatal(err)
	}
	rev.GeoIP = strings.Repeat("c", 40)
	calls := []Kind{}
	next, err := store.PrepareReusing(testContext, sel, rev, func(ctx context.Context, source Source) ([]byte, error) {
		calls = append(calls, source.Kind)
		if source.Kind != GeoIP {
			return nil, errors.New("unchanged GeoSite source offline")
		}
		return reuseFetch(ctx, source)
	}, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != GeoIP {
		t.Fatalf("unexpected fetches: %v", calls)
	}
	current, err := store.Commit(testContext, next, old.Revision)
	if err != nil {
		t.Fatal(err)
	}
	validated := false
	next, err = store.PrepareReusing(testContext, sel, rev, func(context.Context, Source) ([]byte, error) { t.Fatal("pinned data fetched again"); return nil, nil }, func(context.Context, Snapshot) error { validated = true; return nil })
	if err != nil || !validated {
		t.Fatalf("offline revalidation: %v", err)
	}
	if err := store.Abort(testContext, next); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(testContext)
	if err != nil || got.Revision != current.Revision {
		t.Fatal("prepare changed current pointer")
	}
}

func TestPrepareReuseValidatesNewTagsAndCorruptCache(t *testing.T) {
	store, root := testStore(t)
	rev := Revisions{GeoSite: strings.Repeat("a", 40)}
	first, err := store.Prepare(testContext, Selection{GeoSiteTags: []string{"youtube"}}, rev, reuseFetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Commit(testContext, first, "")
	if err != nil {
		t.Fatal(err)
	}
	offline := func(context.Context, Source) ([]byte, error) {
		t.Fatal("same common file redownloaded")
		return nil, nil
	}
	next, err := store.PrepareReusing(testContext, Selection{GeoSiteTags: []string{"category-ads"}}, rev, offline, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	if next.Snapshot().Assets[0].Tags[0] != "category-ads" {
		t.Fatal("selection metadata not updated")
	}
	if err := store.Abort(testContext, next); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareReusing(testContext, Selection{GeoSiteTags: []string{"unknown"}}, rev, offline, fixtureCore); err == nil {
		t.Fatal("unknown new tag accepted")
	}
	if err := os.WriteFile(filepath.Join(root, old.Assets[0].Path), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareReusing(testContext, Selection{GeoSiteTags: []string{"youtube"}}, rev, offline, fixtureCore); err == nil {
		t.Fatal("corrupt cache accepted")
	}
}

package subscriptions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
)

func activeReplacementFixture(t *testing.T) (*env, string) {
	t.Helper()
	e := newEnv(t)
	if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@b.example:443#B"); err != nil {
		t.Fatal(err)
	}
	old := ""
	for _, r := range e.s.liveServers() {
		if r.rec["address"] == "a.example" {
			old = r.id
		}
	}
	if old == "" {
		t.Fatal("fixture active missing")
	}
	if err := e.s.writeActive(old); err != nil {
		t.Fatal(err)
	}
	return e, old
}

func TestSyncRemovedActiveSwitchesWithinRefreshedSubscription(t *testing.T) {
	e, old := activeReplacementFixture(t)
	calls := 0
	e.d.Svc.ServersReapplyActive = func(context.Context, string) error {
		calls++
		id := servers.ActiveID(e.d)
		r, err := servers.ReadRecord(e.d, id)
		if err != nil || id == old || r["address"] != "b.example" || !belongsTo(r, subX) {
			t.Fatal("replacement not from refreshed subscription", err)
		}
		return nil
	}
	result, err := e.sync(t, "replace", "trojan://pw@b.example:443#B")
	if err != nil || calls != 1 || result.Impact != "removed-replaced" || result.Removed != 1 || result.Total != 1 || servers.ActiveID(e.d) == "" {
		t.Fatalf("replacement: %+v %v calls%d", result, err, calls)
	}
	if _, err := os.Stat(servers.RecordPath(e.d, old)); !os.IsNotExist(err) {
		t.Fatal("withdrawn server retained", err)
	}
}

func TestSyncRemovedActiveValidatesRefreshedCandidates(t *testing.T) {
	for _, allInvalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "skip-invalid", true: "all-invalid"}[allInvalid], func(t *testing.T) {
			e, old := activeReplacementFixture(t)
			testActiveConfig = func(_ context.Context, _ *compat.Deps, r servers.Record) error {
				if allInvalid || r["address"] == "b.example" {
					return errors.New("invalid candidate")
				}
				return nil
			}
			result, err := e.sync(t, "replace", "trojan://pw@b.example:443#B", "trojan://pw@c.example:443#C")
			if allInvalid {
				if err == nil || servers.ActiveID(e.d) != old || len(liveIDs(t, e)) != 2 {
					t.Fatalf("invalid candidates changed old catalog: %+v %v", result, err)
				}
			} else {
				r, readErr := servers.ReadRecord(e.d, servers.ActiveID(e.d))
				if err != nil || readErr != nil || r["address"] != "c.example" {
					t.Fatalf("valid replacement not selected: %+v %v %v", result, err, readErr)
				}
			}
		})
	}
}

func TestSyncRemovedActiveReplacementRollsBackFailures(t *testing.T) {
	for _, kind := range []string{"apply", "interface", "remove"} {
		t.Run(kind, func(t *testing.T) {
			e, old := activeReplacementFixture(t)
			runtime := old
			e.d.Svc.ServersReapplyActive = func(context.Context, string) error {
				id := servers.ActiveID(e.d)
				if kind == "apply" && id != old {
					return errors.New("apply failed")
				}
				runtime = id
				return nil
			}
			e.d.Svc.KeeneticEnsureInterface = func(context.Context) error {
				if kind == "interface" && servers.ActiveID(e.d) != old {
					return errors.New("interface failed")
				}
				return nil
			}
			if kind == "remove" {
				path := servers.QualityPath(e.d, old)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(path, "keep"), "blocked")
			}
			result, err := e.sync(t, "replace", "trojan://pw@b.example:443#B")
			if err == nil || servers.ActiveID(e.d) != old || runtime != old || len(liveIDs(t, e)) != 2 {
				t.Fatalf("replacement rollback: %+v %v active%s runtime%s", result, err, servers.ActiveID(e.d), runtime)
			}
		})
	}
}

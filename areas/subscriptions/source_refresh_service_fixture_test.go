package subscriptions

import (
	"bytes"
	"context"
	"errors"
	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/compat"
	"os"
	"path/filepath"
	"testing"
)

func sourceRefreshContext() context.Context { return compat.WithFence(context.Background()) }

// These are service-boundary doubles for subscription catalog tests. Actual
// guarded child lifecycle, reserve and daemon recovery are tested in servers
// and cmd/broray with the pinned native Xray; these callbacks prove no router IO.
func sourceRefreshTestServices(t *testing.T, e *env) {
	t.Helper()
	previous := map[string]servers.Record{}
	e.d.Svc.ServersPrepareSourceRefreshCatalog = func(ctx context.Context, source string, changed []string) error {
		if !compat.FenceHeld(ctx) {
			return errors.New("fixture owner requires protected context")
		}
		previous = map[string]servers.Record{}
		for _, f := range e.s.liveServers() {
			previous[f.id] = deepCopy(f.rec)
		}
		return nil
	}
	e.d.Svc.ServersPrepareSourceRefresh = func(ctx context.Context, source string, removed, candidates []string) (string, error) {
		if !compat.FenceHeld(ctx) {
			return "", errors.New("fixture owner requires protected context")
		}
		old := servers.ActiveID(e.d)
		affected := false
		for _, id := range removed {
			affected = affected || id == old
		}
		if !affected {
			return old, nil
		}
		next := ""
		for _, id := range candidates {
			r, err := servers.ReadRecord(e.d, id)
			if err != nil {
				return "", err
			}
			if testActiveConfig(ctx, e.d, r) == nil {
				next = id
				break
			}
		}
		if next == "" {
			return "", compat.ErrXrayCandidateRejected
		}
		if err := e.s.writeActive(next); err != nil {
			return "", err
		}
		err := e.d.Svc.ReapplyActiveServer(ctx, "subscription-update")
		if err == nil {
			err = e.d.Svc.EnsureInterface(ctx)
		}
		if err != nil {
			restore := e.s.writeActive(old)
			if restore == nil {
				restore = e.d.Svc.ReapplyActiveServer(ctx, "subscription-rollback")
			}
			if restore == nil {
				restore = e.d.Svc.EnsureInterface(ctx)
			}
			return "", errors.Join(err, restore, compat.ErrReserveTransactionPending)
		}
		return next, nil
	}
	e.d.Svc.ServersReapplySourceRefresh = func(ctx context.Context, source, id, previousName string) (string, error) {
		if !compat.FenceHeld(ctx) {
			return "", errors.New("fixture owner requires protected context")
		}
		rec, err := servers.ReadRecord(e.d, id)
		if err != nil {
			return "", err
		}
		if err := testActiveConfig(ctx, e.d, rec); err != nil {
			return "", errors.Join(compat.ErrXrayCandidateRejected, err)
		}
		old, ok := previous[id]
		if !ok {
			return "", errors.New("fixture old profile absent")
		}
		if configHash(old) != configHash(rec) {
			err = e.d.Svc.ReapplyActiveServer(ctx, "subscription-update")
		} else if nameOrID(old) != nameOrID(rec) {
			err = e.d.Svc.EnsureInterface(ctx)
		}
		if err != nil {
			return "", errors.Join(err, compat.ErrReserveTransactionPending)
		}
		return id, nil
	}
	e.d.Svc.ServersRenameSourceRefresh = e.d.Svc.ServersReapplySourceRefresh
}

func assertSourceRefreshPending(t *testing.T, e *env, err error) {
	t.Helper()
	if !errors.Is(err, compat.ErrReserveTransactionPending) {
		t.Fatal("pending ownership cause missing", err)
	}
	markers, globErr := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
	if globErr != nil || len(markers) != 1 {
		t.Fatal("pending marker missing", globErr)
	}
	backup, readErr := os.ReadFile(filepath.Join(filepath.Dir(markers[0]), "backup.json"))
	if readErr != nil || len(backup) == 0 {
		t.Fatal("recovery backup missing", readErr)
	}
}

func TestSourceRefreshNativeFailureIsolation(t *testing.T) {
	for _, kind := range []string{"reapply", "validation", "name"} {
		t.Run(kind, func(t *testing.T) {
			e, id := activeReplacementFixture(t)
			before, err := os.ReadFile(servers.RecordPath(e.d, id))
			if err != nil {
				t.Fatal(err)
			}
			next := "trojan://new@a.example:443#A"
			switch kind {
			case "reapply":
				e.reapplyErr = errors.New("synthetic runtime uncertainty")
			case "validation":
				e.testErr = errors.New("synthetic candidate rejection")
			case "name":
				e.ensureErr = errors.New("synthetic description uncertainty")
				next = "trojan://pw@a.example:443#Renamed"
			}
			_, err = e.sync(t, "replace", next, "trojan://pw@b.example:443#B")
			if err == nil || servers.ActiveID(e.d) != id {
				t.Fatal("failed refresh lost original selected identity", err)
			}
			got, readErr := os.ReadFile(servers.RecordPath(e.d, id))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if kind == "validation" {
				if !errors.Is(err, compat.ErrXrayCandidateRejected) || !bytes.Equal(got, before) {
					t.Fatal("confirmed pre-runtime rejection did not restore exact catalog", err)
				}
				markers, _ := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
				if len(markers) != 0 {
					t.Fatal("confirmed rollback retained marker")
				}
				return
			}
			assertSourceRefreshPending(t, e, err)
			if bytes.Equal(got, before) {
				t.Fatal("uncertain runtime caused unsafe catalog rollback")
			}
			e.reapplyErr = nil
			e.ensureErr = nil
			if _, err = e.sync(t, "replace", "trojan://pw@a.example:443#A"); err == nil {
				t.Fatal("pending source recovery did not block retry")
			}
			after, err := os.ReadFile(servers.RecordPath(e.d, id))
			if err != nil || !bytes.Equal(got, after) {
				t.Fatal("blocked retry changed catalog", err)
			}
			for _, reason := range e.reapplied {
				if reason == "subscription-rollback" {
					t.Fatal("catalog invoked generic rollback")
				}
			}
		})
	}
}

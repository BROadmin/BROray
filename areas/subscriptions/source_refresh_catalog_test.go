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

func TestSourceRefreshCatalogUsesNativeOwner(t *testing.T) {
	for _, kind := range []string{"replace", "profile", "rename", "partial", "rejected", "uncertain", "postcommit-drift", "cleanup-marker-drift", "missing-provider", "legacy", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			e, old := activeReplacementFixture(t)
			oldPath := servers.RecordPath(e.d, old)
			oldBytes, err := os.ReadFile(oldPath)
			if err != nil {
				t.Fatal(err)
			}
			before := map[string][]byte{}
			for _, member := range e.s.liveServers() {
				before[member.path] = member.raw
			}
			pre, replace, reapply, rename, legacy := 0, 0, 0, 0, 0
			e.d.Svc.ServersReapplyActive = func(context.Context, string) error { legacy++; return nil }
			e.d.Svc.KeeneticEnsureInterface = func(context.Context) error { legacy++; return nil }
			e.d.Svc.ServersPrepareSourceRefreshCatalog = func(ctx context.Context, source string, ids []string) error {
				pre++
				if !compat.FenceHeld(ctx) || source != subX {
					t.Fatal("pre-stage owner scope")
				}
				if kind == "rename" && len(ids) != 0 {
					t.Fatal("rename-only record reached reserve pre-disable set", ids)
				}
				got, err := os.ReadFile(oldPath)
				if err != nil || !bytes.Equal(got, oldBytes) || servers.ActiveID(e.d) != old {
					t.Fatal("pre-stage must see old profile before catalog writes", err)
				}
				return nil
			}
			e.d.Svc.ServersPrepareSourceRefresh = func(ctx context.Context, source string, removed, candidates []string) (string, error) {
				replace++
				if !compat.FenceHeld(ctx) || source != subX || len(removed) != 1 || removed[0] != old || len(candidates) == 0 || servers.ActiveID(e.d) != old {
					t.Fatal("native replacement scope or pointer written early")
				}
				if got, err := os.ReadFile(oldPath); err != nil || !bytes.Equal(got, oldBytes) {
					t.Fatal("old active removed before owner", err)
				}
				for _, id := range candidates {
					r, err := servers.ReadRecord(e.d, id)
					if err != nil || id == old || !belongsTo(r, subX) {
						t.Fatal("candidate not from staged refreshed source", id, err)
					}
				}
				if kind == "rejected" {
					return "", compat.ErrXrayCandidateRejected
				}
				next := candidates[0]
				if err := e.s.writeActive(next); err != nil {
					t.Fatal(err)
				}
				if kind == "uncertain" {
					return "", errors.Join(compat.ErrReserveTransactionPending, errors.New("after selection uncertainty"))
				}
				if kind == "postcommit-drift" {
					foreign := filepath.Join(t.TempDir(), "foreign")
					if err := os.WriteFile(foreign, []byte("foreign-preserved"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(oldPath); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(foreign, oldPath); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						got, err := os.ReadFile(foreign)
						if err != nil || string(got) != "foreign-preserved" {
							t.Error("foreign data changed", err)
						}
					})
				}
				if kind == "cleanup-marker-drift" {
					markers, e := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
					if e != nil || len(markers) != 1 {
						t.Fatal("fixture marker missing", e)
					}
					if e := os.Remove(markers[0]); e != nil {
						t.Fatal(e)
					}
					if e := os.Mkdir(markers[0], 0700); e != nil {
						t.Fatal(e)
					}
				}
				return next, nil
			}
			checkRetained := func(ctx context.Context, source, id, previousName string) (string, error) {
				if !compat.FenceHeld(ctx) || source != subX || id != old || previousName != "A" || servers.ActiveID(e.d) != old {
					t.Fatal("same-ID owner scope")
				}
				got, err := os.ReadFile(oldPath)
				if err != nil || bytes.Equal(got, oldBytes) {
					t.Fatal("new profile not staged", err)
				}
				return old, nil
			}
			e.d.Svc.ServersReapplySourceRefresh = func(ctx context.Context, source, id, previousName string) (string, error) {
				reapply++
				return checkRetained(ctx, source, id, previousName)
			}
			e.d.Svc.ServersRenameSourceRefresh = func(ctx context.Context, source, id, previousName string) (string, error) {
				rename++
				return checkRetained(ctx, source, id, previousName)
			}
			links := []string{"trojan://pw@b.example:443#B"}
			policy := "replace"
			switch kind {
			case "profile":
				links = []string{"trojan://new-password@a.example:443#A", "trojan://pw@b.example:443#B"}
			case "rename":
				links = []string{"trojan://pw@a.example:443#Renamed", "trojan://pw@b.example:443#B"}
			case "partial":
				policy = "retain-unmatched"
			case "missing-provider":
				e.d.Svc.ServersPrepareSourceRefreshCatalog = nil
			case "malformed":
				if err := os.WriteFile(oldPath, []byte("{broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx := compat.WithFence(context.Background())
			if kind == "legacy" {
				ctx = context.Background()
			}
			result, err := e.s.syncCatalog(ctx, subX, stageLinks(t, true, links...), true, "native-owner", policy)
			if legacy != 0 {
				t.Fatal("refresh used generic runtime/description mutation", legacy)
			}
			pending := kind == "uncertain" || kind == "postcommit-drift" || kind == "cleanup-marker-drift"
			failed := pending || kind == "rejected" || kind == "missing-provider" || kind == "legacy" || kind == "malformed"
			if (err != nil) != failed {
				t.Fatal("result", kind, result, err)
			}
			if pending {
				if !errors.Is(err, compat.ErrReserveTransactionPending) || servers.ActiveID(e.d) == old {
					t.Fatal("uncertain native commit rolled back or lost pending cause", err)
				}
				markers, _ := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
				if len(markers) != 1 {
					t.Fatal("missing recovery marker")
				}
				assertSourceRefreshPending(t, e, err)
				if _, err := servers.ReadRecord(e.d, servers.ActiveID(e.d)); err != nil {
					t.Fatal("selected candidate removed", err)
				}
				return
			}
			if failed {
				if servers.ActiveID(e.d) != old {
					t.Fatal("failed refresh changed selection")
				}
				if kind != "malformed" {
					for path, raw := range before {
						got, e := os.ReadFile(path)
						if e != nil || !bytes.Equal(raw, got) {
							t.Fatal("failed refresh lost old catalog", path, e)
						}
					}
				}
				return
			}
			if pre != 1 {
				t.Fatal("old profile pre-staging owner absent", pre)
			}
			if kind == "replace" {
				if replace != 1 || reapply != 0 || servers.ActiveID(e.d) == old || result.Removed != 1 || result.Impact != "removed-replaced" {
					t.Fatal("replacement not committed through owner", result)
				}
				if _, err := os.Lstat(oldPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("vanished record retained", err)
				}
			} else if kind == "partial" {
				if replace != 0 || reapply != 0 || result.Retained != 1 || servers.ActiveID(e.d) != old {
					t.Fatal("partial response treated as disappearance", result)
				}
			} else if kind == "rename" {
				if rename != 1 || reapply != 0 || replace != 0 || servers.ActiveID(e.d) != old {
					t.Fatal("rename did not use the display-only owner", rename, reapply, replace)
				}
			} else if rename != 0 || reapply != 1 || replace != 0 || servers.ActiveID(e.d) != old {
				t.Fatal("retained active owner not called", reapply, replace)
			}
			markers, _ := filepath.Glob(filepath.Join(e.d.Root.Tmp(), "server-subscription-sync.*", "rollback-required"))
			if len(markers) != 0 {
				t.Fatal("successful commit left recovery marker")
			}
		})
	}
}

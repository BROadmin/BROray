package subscriptions

import (
	"bytes"
	"context"
	"github.com/BROadmin/broray/areas/servers"
	"os"
	"testing"
)

func TestSourceRefreshLegacyCatalogBoundaries(t *testing.T) {
	for _, kind := range []string{"foreign-active-destination", "inactive-compatible"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			stage := stageLinks(t, true, "trojan://pw@a.example:443#A")
			id := stage[0]["id"].(string)
			calls := 0
			e.d.Svc.ServersPrepareSourceRefreshCatalog = func(context.Context, string, []string) error { calls++; return nil }
			var old []byte
			if kind == "foreign-active-destination" {
				foreign := deepCopy(stage[0])
				foreign["source"] = map[string]any{"type": "manual"}
				foreign["name"] = "Manual"
				if err := servers.WriteRecord(e.d, foreign); err != nil {
					t.Fatal(err)
				}
				if err := e.s.writeActive(id); err != nil {
					t.Fatal(err)
				}
				var err error
				old, err = os.ReadFile(servers.RecordPath(e.d, id))
				if err != nil {
					t.Fatal(err)
				}
			}
			result, err := e.s.syncCatalog(context.Background(), subX, stage, true, "legacy-boundary", "replace")
			if calls != 0 {
				t.Fatal("legacy refresh borrowed runtime owner")
			}
			if kind == "foreign-active-destination" {
				now, readErr := os.ReadFile(servers.RecordPath(e.d, id))
				if err == nil || readErr != nil || !bytes.Equal(old, now) || servers.ActiveID(e.d) != id {
					t.Fatal("legacy refresh overwrote foreign active record", err, readErr)
				}
			} else {
				if err != nil || result.Total != 1 {
					t.Fatal("inactive legacy compatibility", result, err)
				}
				next := stageLinks(t, true, "trojan://next@a.example:443#A")
				result, err = e.s.syncCatalog(context.Background(), subX, next, true, "legacy-next", "replace")
				rec, readErr := servers.ReadRecord(e.d, id)
				if err != nil || readErr != nil || result.Updated != 1 || rec["password"] != "next" || calls != 0 {
					t.Fatal("inactive continuity", result, err, readErr)
				}
			}
		})
	}
}

package subscriptions

import (
	"context"
	"errors"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func rotationLink(sni, sid string) string {
	return "vless://11111111-1111-4111-8111-111111111111@node.example:443?type=xhttp&security=reality&pbk=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG&path=%2F&sni=" + sni + "&sid=" + sid + "#Node"
}

func TestSyncSNIRotation(t *testing.T) {
	for _, change := range []struct{ name, sni, sid string }{{"sni", "new.example", "aa"}, {"sid", "old.example", "bb"}, {"both", "new.example", "bb"}} {
		t.Run(change.name, func(t *testing.T) {
			e := newEnv(t)
			if _, err := e.sync(t, "replace", rotationLink("old.example", "aa")); err != nil {
				t.Fatal(err)
			}
			id := liveIDs(t, e)[0]
			if err := e.s.writeActive(id); err != nil {
				t.Fatal(err)
			}
			r, err := e.sync(t, "retain-unmatched", rotationLink(change.sni, change.sid))
			if err != nil || r.Added != 0 || r.Updated != 1 || r.Retained != 0 || r.Total != 1 || r.Impact != "configuration-changed" {
				t.Fatalf("rotation: %+v %v", r, err)
			}
			if servers.ActiveID(e.d) != id || len(e.reapplied) != 1 {
				t.Fatal("active identity/runtime not preserved")
			}
			rec, _ := servers.ReadRecord(e.d, id)
			if rec["reality"].(map[string]any)["serverName"] != change.sni {
				t.Fatal("new parameters not applied")
			}
		})
	}
}

func TestSyncAccumulatedRotationDuplicates(t *testing.T) {
	for _, policy := range []string{"replace", "retain-unmatched"} {
		t.Run(policy, func(t *testing.T) {
			e := newEnv(t)
			old := stageLinks(t, true, rotationLink("one.example", "aa"), rotationLink("two.example", "bb"), rotationLink("three.example", "cc"))
			for _, rec := range old {
				if err := servers.WriteRecord(e.d, rec); err != nil {
					t.Fatal(err)
				}
			}
			active := old[0]["id"].(string)
			if err := e.s.writeActive(active); err != nil {
				t.Fatal(err)
			}
			// The incoming node exactly matches a different old copy. Active ID must win.
			r, err := e.sync(t, policy, rotationLink("three.example", "cc"))
			if err != nil || r.Total != 1 || r.Removed != 2 || r.Retained != 0 {
				t.Fatalf("consolidate: %+v %v", r, err)
			}
			if ids := liveIDs(t, e); len(ids) != 1 || ids[0] != active || servers.ActiveID(e.d) != active {
				t.Fatal("wrong survivor")
			}
		})
	}
}

func TestSyncRotationBoundaries(t *testing.T) {
	e := newEnv(t)
	old := stageLinks(t, true, rotationLink("old.example", "aa"))
	manual := deepCopy(old[0])
	manual["id"] = "manual-copy"
	manual["source"] = map[string]any{"type": "manual"}
	foreign := deepCopy(old[0])
	foreign["id"] = "subscription-other-copy"
	foreign["source"].(map[string]any)["subscriptionId"] = "sub-other"
	for _, rec := range []servers.Record{old[0], manual, foreign} {
		if err := servers.WriteRecord(e.d, rec); err != nil {
			t.Fatal(err)
		}
	}
	r, err := e.sync(t, "replace", rotationLink("new.example", "bb"))
	if err != nil || r.Total != 1 || len(liveIDs(t, e)) != 3 {
		t.Fatalf("source boundary: %+v %v", r, err)
	}
	for _, rec := range []servers.Record{manual, foreign} {
		got, err := servers.ReadRecord(e.d, rec["id"].(string))
		if err != nil || canonicalHash(got) != canonicalHash(rec) {
			t.Fatal("other source changed")
		}
	}
	// Multiple distinct current variants cannot be guessed into one historical node.
	_, err = e.sync(t, "replace", rotationLink("a.example", "cc"), rotationLink("b.example", "dd"))
	var se *subErr
	if !errors.As(err, &se) || se.Code != "SERVER_SYNC_AMBIGUOUS_IDENTITY" {
		t.Fatalf("ambiguity not rejected: %v", err)
	}
	// Disabled subscription follows the same identity rules.
	if err := e.s.setEnabled(subX, false); err != nil {
		t.Fatal(err)
	}
	id := e.s.disabledServers(subX)[0].id
	r, err = e.s.syncCatalog(context.Background(), subX, stageLinks(t, false, rotationLink("last.example", "ee")), false, "disabled", "replace")
	if err != nil || r.Updated != 1 || e.s.disabledServers(subX)[0].id != id {
		t.Fatalf("disabled: %+v %v", r, err)
	}
}

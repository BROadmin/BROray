package subscriptions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func TestSyncActiveWithdrawal(t *testing.T) {
	e := newEnv(t)
	if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@b.example:443#B"); err != nil {
		t.Fatal(err)
	}
	var id string
	for _, r := range e.s.liveServers() {
		if r.rec["address"] == "a.example" {
			id = r.id
		}
	}
	if err := e.s.writeActive(id); err != nil {
		t.Fatal(err)
	}
	writeFile(t, servers.QualityPath(e.d, id), `{"status":"available"}`)
	// Partial response cannot prove disappearance and must retain the active node.
	r, err := e.sync(t, "retain-unmatched", "trojan://pw@b.example:443#B")
	if err != nil || r.Retained != 1 || servers.ActiveID(e.d) != id {
		t.Fatalf("partial: %+v %v", r, err)
	}
	r, err = e.sync(t, "replace", "trojan://pw@b.example:443#B")
	if err != nil || r.Removed != 1 || r.Total != 1 || r.Impact != "removed-disconnected" {
		t.Fatalf("withdrawal: %+v %v", r, err)
	}
	if servers.ActiveID(e.d) != "" || len(liveIDs(t, e)) != 1 || len(r.Warnings) != 1 {
		t.Fatal("inconsistent withdrawal")
	}
	if _, err := os.Stat(servers.QualityPath(e.d, id)); !os.IsNotExist(err) {
		t.Fatal("orphaned quality")
	}
}

func TestSyncAppliedChangeRemovalRollback(t *testing.T) {
	for _, nameOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "sni", true: "name"}[nameOnly], func(t *testing.T) {
			e := newEnv(t)
			old := rotationLink("old.example", "aa")
			if _, err := e.sync(t, "replace", old, "trojan://pw@other.example:443#Other"); err != nil {
				t.Fatal(err)
			}
			var active, other string
			for _, f := range e.s.liveServers() {
				if f.rec["protocol"] == "vless" {
					active = f.id
				} else {
					other = f.id
				}
			}
			if err := e.s.writeActive(active); err != nil {
				t.Fatal(err)
			}
			field := func() string {
				r, _ := servers.ReadRecord(e.d, active)
				if nameOnly {
					return r["name"].(string)
				}
				return r["reality"].(map[string]any)["serverName"].(string)
			}
			runtime := field()
			before := runtime
			e.d.Svc.ServersReapplyActive = func(context.Context, string) error { runtime = field(); return nil }
			e.d.Svc.KeeneticEnsureInterface = func(context.Context) error { runtime = field(); return nil }
			blocked := servers.QualityPath(e.d, other)
			if err := os.MkdirAll(blocked, 0o700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(blocked, "keep"), "fixture")
			next := rotationLink("new.example", "bb")
			if nameOnly {
				next = strings.Replace(old, "#Node", "#Renamed", 1)
			}
			if _, err := e.sync(t, "replace", next); err == nil {
				t.Fatal("expected removal failure")
			}
			if field() != before || runtime != before {
				t.Fatalf("rollback differs: saved=%q runtime=%q expected=%q", field(), runtime, before)
			}
		})
	}
}

func TestSyncWithdrawalRemovalFailureRollback(t *testing.T) {
	e := newEnv(t)
	if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@b.example:443#B"); err != nil {
		t.Fatal(err)
	}
	ids := liveIDs(t, e)
	if err := e.s.writeActive(ids[0]); err != nil {
		t.Fatal(err)
	}
	quality := `{"status":"available","rating":"good"}`
	writeFile(t, servers.QualityPath(e.d, ids[0]), quality)
	blocked := servers.QualityPath(e.d, ids[1])
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(blocked, "keep"), "fixture")
	_, err := e.sync(t, "replace", "trojan://pw@c.example:443#C")
	if err == nil {
		t.Fatal("remove error ignored")
	}
	if len(liveIDs(t, e)) != 2 || servers.ActiveID(e.d) != ids[0] {
		t.Fatal("catalog/pointer not rolled back")
	}
	got, readErr := os.ReadFile(servers.QualityPath(e.d, ids[0]))
	if readErr != nil || string(got) != quality {
		t.Fatal("quality lost during rollback")
	}
}

package subscriptions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/modules/reserve"
)

func TestLegacySourceRemovalReserveGuard(t *testing.T) {
	for _, kind := range []string{"disabled-roster", "enabled-roster", "unrelated", "invalid-preferences", "pending"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			sub := e.create(t, "https://example.com/legacy", "")
			if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@foreign.example:443#Foreign"); err != nil {
				t.Fatal(err)
			}
			owned, foreign := "", ""
			for _, f := range e.s.liveServers() {
				if f.rec["address"] == "a.example" {
					sourceOf(f.rec)["subscriptionId"] = sub
					owned = f.id
				} else {
					sourceOf(f.rec)["subscriptionId"] = "sub-00000000000000ff"
					foreign = f.id
				}
				if err := servers.WriteRecord(e.d, f.rec); err != nil {
					t.Fatal(err)
				}
			}
			prefs := map[string]any{"schemaVersion": 1, "enabled": kind == "enabled-roster", "primaryServerId": owned, "backupServerIds": []string{foreign}}
			if kind == "unrelated" {
				prefs["primaryServerId"] = foreign
				prefs["backupServerIds"] = []string{}
			}
			data, err := json.Marshal(prefs)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "invalid-preferences" {
				data = []byte("broken preferences")
			}
			p := e.d.Path("config", "system", "server-reserve.json")
			if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(p, data, 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "pending" {
				if err = os.WriteFile(filepath.Join(e.d.Root.Config(), reserve.TransactionJournalFile), []byte(`{"unconfirmed":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			response := call(t, "POST", "/api/subscriptions/delete.cgi?id="+sub, `{}`, nil)
			if kind == "unrelated" {
				if response.Status != 200 || exists2(e.s.recPath(sub)) || exists2(servers.RecordPath(e.d, owned)) {
					t.Fatal("safe legacy deletion refused", response.Status, response.Raw)
				}
			} else {
				want := 409
				if kind == "invalid-preferences" {
					want = 500
				}
				if response.Status != want || !exists2(e.s.recPath(sub)) || !exists2(servers.RecordPath(e.d, owned)) {
					t.Fatal("legacy removal bypassed reserve guard", kind, response.Status, response.Raw)
				}
			}
			if !exists2(servers.RecordPath(e.d, foreign)) {
				t.Fatal("foreign server removed")
			}
			got, err := os.ReadFile(p)
			if err != nil || string(got) != string(data) {
				t.Fatal("legacy guard mutated preferences", err)
			}
		})
	}
}

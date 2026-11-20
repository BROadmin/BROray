package subscriptions

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func TestSubscriptionDeleteSourceOwnershipAndErrors(t *testing.T) {
	for _, kind := range []string{"live", "last-error", "disabled", "foreign-disabled", "active", "recovery-lock", "global-busy"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			sub := e.create(t, "https://example.com/subscription", "")
			if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@b.example:443#B", "trojan://pw@foreign.example:443#Foreign"); err != nil {
				t.Fatal(err)
			}
			var owned []string
			var preserved []string
			images := map[string][]byte{}
			for _, f := range e.s.liveServers() {
				foreign := f.rec["address"] == "foreign.example"
				sourceOf(f.rec)["subscriptionId"] = sub
				if foreign {
					sourceOf(f.rec)["subscriptionId"] = "sub-00000000000000ff"
				}
				if err := servers.WriteRecord(e.d, f.rec); err != nil {
					t.Fatal(err)
				}
				path := f.path
				if (!foreign && kind == "disabled") || (foreign && kind == "foreign-disabled") {
					path = filepath.Join(e.s.disabledDir(sub), filepath.Base(f.path))
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(f.path, path); err != nil {
						t.Fatal(err)
					}
				}
				quality := servers.QualityPath(e.d, f.id)
				if err := os.MkdirAll(filepath.Dir(quality), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(quality, []byte(`{"fixture":"preserve-or-remove-with-owner"}`), 0600); err != nil {
					t.Fatal(err)
				}
				for _, p := range []string{path, quality} {
					b, err := os.ReadFile(p)
					if err != nil {
						t.Fatal(err)
					}
					images[p] = b
					if foreign {
						preserved = append(preserved, p)
					} else {
						owned = append(owned, p)
					}
				}
			}
			wantCode := ""
			switch kind {
			case "last-error":
				rec := e.record(t, sub)
				rec["lastError"] = "Предыдущее обновление не удалось"
				rec["lastUpdateStatus"] = "error"
				if err := e.s.writeRecord(rec); err != nil {
					t.Fatal(err)
				}
			case "active":
				for _, f := range e.s.liveServers() {
					if belongsTo(f.rec, sub) {
						if err := e.s.writeActive(f.id); err != nil {
							t.Fatal(err)
						}
						break
					}
				}
				wantCode = "ACTIVE_SERVER_CONFLICT"
			case "recovery-lock":
				p := e.d.RunPath("subscriptions", sub+".lock")
				if err := os.MkdirAll(p, 0700); err != nil {
					t.Fatal(err)
				}
				wantCode = "UPDATE_ALREADY_RUNNING"
			case "global-busy":
				release, ok := e.d.Lock.TryAcquire("test", "hold", "")
				if !ok {
					t.Fatal("cannot acquire fixture lock")
				}
				defer release()
				wantCode = "OPERATION_BUSY"
			}
			subBefore, err := os.ReadFile(e.s.recPath(sub))
			if err != nil {
				t.Fatal(err)
			}
			r := call(t, "POST", "/api/subscriptions/delete.cgi?id="+sub, `{}`, nil)
			if wantCode != "" {
				if r.Status != 409 || r.errCode() != wantCode {
					t.Fatalf("expected refusal %s: %s", wantCode, r.Raw)
				}
				preserved = append(preserved, owned...)
				images[e.s.recPath(sub)] = subBefore
				preserved = append(preserved, e.s.recPath(sub))
			} else {
				if r.Status != 200 || r.data()["removed"] != float64(2) {
					t.Errorf("expected removal of two owned records: %s", r.Raw)
				}
				for _, p := range append(owned, e.s.recPath(sub)) {
					if _, err := os.Lstat(p); !os.IsNotExist(err) {
						t.Errorf("owned entry remains: %s %v", p, err)
					}
				}
			}
			for _, p := range preserved {
				after, err := os.ReadFile(p)
				if err != nil || !bytes.Equal(after, images[p]) {
					t.Errorf("protected entry changed: %s %v", p, err)
				}
			}
		})
	}
}

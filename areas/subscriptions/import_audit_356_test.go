package subscriptions

// Audit regressions from 2026-11-12. All endpoints, credentials and profiles
// are invented. These test requirements, not acceptance of the released build.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func auditJSON356(sni, sid string) string {
	return fmt.Sprintf(`{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"node.example","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"xhttp","security":"reality","realitySettings":{"serverName":%q,"shortId":%q,"publicKey":"abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG","fingerprint":"chrome"},"xhttpSettings":{"path":"/","mode":"auto"}}}]}`, sni, sid)
}

func TestAudit356SelfExportAndRotation(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(fmt.Sprint("rotate-", rotate), func(t *testing.T) {
			e := newEnv(t)
			var mu sync.Mutex
			phase, reads := 0, 0
			p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Header.Get("Accept") == "text/html" {
					fmt.Fprint(w, `<html><script>window.__SUB_PAGE_DATA__={"subJsonUrl":"/download/video/invented"};</script></html>`)
					return
				}
				reads++
				if phase == 0 {
					fmt.Fprint(w, auditJSON356("old.example", "aa"))
					return
				}
				fmt.Fprint(w, auditJSON356(fmt.Sprintf("new-%d.example", reads), fmt.Sprintf("%02x", reads)))
			}))
			defer p.Close()
			id := e.createWithExports357(t, p.URL+"/download/video/invented", "")
			if err := e.refreshSync(t, id); err != nil {
				t.Fatal(err)
			}
			if !rotate {
				if reads != 1 {
					t.Fatalf("same JSON URL fetched %d times in one update; want one data snapshot", reads)
				}
				return
			}
			old := liveIDs(t, e)[0]
			if err := e.s.writeActive(old); err != nil {
				t.Fatal(err)
			}
			before := snapshotAutoCatalog355(t, e)
			mu.Lock()
			phase, reads = 1, 0
			mu.Unlock()
			err := e.refreshSync(t, id)
			if err != nil {
				t.Logf("failed commit preserved catalog=%t active=%t", reflect.DeepEqual(before, snapshotAutoCatalog355(t, e)), servers.ActiveID(e.d) == old)
				t.Fatalf("one endpoint rotating SNI/ShortID blocked whole subscription: %v", err)
			}
			if len(liveIDs(t, e)) != 1 || servers.ActiveID(e.d) != old {
				t.Fatal("rotation duplicated or lost active identity")
			}
		})
	}
}

func TestAudit356ExactDuplicatesMustNotPreventWithdrawal(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	body := "trojan://pw@a.example:443#A\ntrojan://pw@b.example:443#B"
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mu.Lock(); defer mu.Unlock(); fmt.Fprint(w, body) }))
	defer p.Close()
	id := e.createWithExports357(t, p.URL, " ")
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	manual, err := servers.ParseLink("trojan://manual@manual.example:443", "manual-protected", map[string]any{"type": "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if err = servers.WriteRecord(e.d, manual); err != nil {
		t.Fatal(err)
	}
	mb, _ := os.ReadFile(servers.RecordPath(e.d, "manual-protected"))
	mu.Lock()
	body = "trojan://pw@a.example:443#A\ntrojan://pw@a.example:443#A duplicate name"
	mu.Unlock()
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	ma, _ := os.ReadFile(servers.RecordPath(e.d, "manual-protected"))
	if string(ma) != string(mb) {
		t.Fatal("manual record changed")
	}
	r := e.record(t, id)
	if r["lastUpdateStatus"] != "success" || len(e.s.liveServers()) != 2 {
		t.Fatalf("valid repeated node treated as parse failure; withdrawn B retained: status=%v catalog=%d result=%v", r["lastUpdateStatus"], len(e.s.liveServers()), r["lastUpdateResult"])
	}
}

func TestAudit356DuplicateAdvertisedJSON(t *testing.T) {
	one := `{"protocol":"trojan","settings":{"address":"a.example","port":443,"password":"pw"},"streamSettings":{"security":"tls"}}`
	st, err := stageDocuments(subX, []sourceDocument{{body: []byte("trojan://pw@a.example:443#A")}, {body: []byte(`{"outbounds":[` + one + `,` + one + `]}`), format: "json"}}, newMetadata(), true, nil)
	if err != nil || st.Rejected != 0 || st.Accepted != 1 {
		t.Fatalf("valid exact duplicates abort advertised export: accepted=%d rejected=%d err=%v", st.Accepted, st.Rejected, err)
	}
}

func TestAudit356ClashAWGExtensions(t *testing.T) {
	p := map[string]any{"name": "invented-awg", "type": "wireguard", "server": "awg.example", "port": 443, "ip": "10.0.0.2", "private-key": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "public-key": "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", "amnezia-wg-option": map[string]any{"jc": float64(4), "header-protection-key": "invented-extension", "content-padding-addition": float64(10), "rekey-after-time": float64(180), "disable-cookies": true}}
	st := clashStage(t, clashBody(p))
	if st.Accepted != 1 {
		t.Fatalf("fixture rejected: %+v", st)
	}
	raw, _ := json.Marshal(st.Records[0]["amneziawg"])
	var profile struct {
		Interface  map[string][]string `json:"interface"`
		Parameters map[string]any      `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"HeaderProtectionKey", "ContentPaddingAddition", "RekeyAfterTime", "DisableCookies"} {
		if len(profile.Interface[field]) == 0 {
			t.Errorf("accepted AWG extension %s missing from normalized interface (raw clashOptions still retained)", field)
		}
	}
}

func TestAudit356WithdrawalAndFailureBoundaries(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprint("active-", active), func(t *testing.T) {
			e := newEnv(t)
			if _, err := e.sync(t, "replace", "trojan://pw@a.example:443#A", "trojan://pw@b.example:443#B"); err != nil {
				t.Fatal(err)
			}
			var removed string
			for _, f := range e.s.liveServers() {
				if f.rec["address"] == "a.example" {
					removed = f.id
				}
			}
			for _, id := range []string{"manual-protected", "subscription-other-protected"} {
				source := map[string]any{"type": "manual"}
				if strings.HasPrefix(id, "subscription-") {
					source = map[string]any{"type": "subscription", "subscriptionId": "sub-other", "importKey": strings.Repeat("a", 64), "nodeIndex": 1, "enabled": true}
				}
				r, err := servers.ParseLink("trojan://pw@a.example:443", id, source)
				if err != nil {
					t.Fatal(err)
				}
				if err := servers.WriteRecord(e.d, r); err != nil {
					t.Fatal(err)
				}
			}
			if active {
				if err := e.s.writeActive(removed); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotAutoCatalog355(t, e)
			res, err := e.s.syncCatalog(context.Background(), subX, stageLinks(t, true, "trojan://pw@b.example:443#B"), true, "audit", "replace")
			if err != nil || res.Removed != 1 || servers.RecordExists(e.d, removed) {
				t.Fatalf("withdrawal: %+v %v", res, err)
			}
			after := snapshotAutoCatalog355(t, e)
			for path, data := range before {
				if strings.Contains(path, "protected") && after[path] != data {
					t.Fatal("manual/other subscription changed")
				}
			}
			if active && servers.ActiveID(e.d) != "" {
				t.Fatal("removed active pointer retained")
			}
		})
	}
}

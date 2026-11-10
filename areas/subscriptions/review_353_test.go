package subscriptions

import (
	"context"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func TestReview353CloudActiveCredentialRefresh(t *testing.T) {
	e := newEnv(t)
	body := `{"protocol":"vless","tag":"Cloud","settings":{"address":"cloud.example.test","port":443,"id":"00000000-0000-4000-8000-000000000001","encryption":"mlkem768x25519plus.native.0rtt.` + strings.Repeat("A", 43) + `"},"mux":{"enabled":true},"streamSettings":{"network":"xdrive","security":"none","xdriveSettings":{"service":"template","remoteFolder":"test-folder","template":{"auth":{"type":"basic","username":"synthetic","password":"synthetic-old"},"list":{"url":"https://storage.example.test/{folder}","namesRegex":"(.*)"},"get":{"url":"https://storage.example.test/{folder}/{name}"},"put":{"url":"https://storage.example.test/{folder}/{name}","method":"PUT"},"delete":{"url":"https://storage.example.test/{folder}/{name}","method":"DELETE"}}}}}`
	stage := func(raw string) []servers.Record {
		ex, err := extractNodes([]byte(`{"outbounds":[`+raw+`]}`), newMetadata())
		if err != nil {
			t.Fatal(err)
		}
		st, err := stageNodes(subX, ex, true, nil)
		if err != nil || st.Accepted != 1 || st.Rejected != 0 {
			t.Fatalf("fixture failed to stage: %+v %v", st, err)
		}
		return st.Records
	}
	if _, err := e.s.syncCatalog(context.Background(), subX, stage(body), true, "old", "replace"); err != nil {
		t.Fatal(err)
	}
	id := liveIDs(t, e)[0]
	if err := e.s.writeActive(id); err != nil {
		t.Fatal(err)
	}
	result, err := e.s.syncCatalog(context.Background(), subX, stage(strings.Replace(body, "synthetic-old", "synthetic-new", 1)), true, "new", "replace")
	if err != nil || result.Added != 0 || result.Removed != 0 || result.Updated != 1 || result.Impact != "configuration-changed" {
		t.Fatalf("credential rotation treated as withdrawal: %+v %v", result, err)
	}
	if servers.ActiveID(e.d) != id || len(e.reapplied) != 1 {
		t.Fatal("active ID or reapply was lost")
	}
}

package subscriptions

import (
	"context"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

const nativeMasqueJSON = `{"protocol":"masque","tag":"MASQUE 测试","settings":{"address":"proxy.example.test","port":443,"remoteDNS":["1.1.1.1"]},"streamSettings":{"network":"masque","security":"tls","tlsSettings":{"alpn":["h2"],"serverName":"proxy.example.test"},"masqueSettings":{"user":"synthetic-private-user","pass":"synthetic-private-pass"}}}`

func TestNativeJSONSizeBoundary(t *testing.T) {
	// Preserve the existing 2 MiB subscription limit after strict decoding.
	body := []byte(strings.Repeat(" ", 1024*1024) + `{"outbounds":[` + obTrojan + `]}`)
	if _, err := extractNodes(body, newMetadata()); err != nil {
		t.Fatal("previously valid subscription size rejected", err)
	}
}

func TestNativeJSONImportNodes(t *testing.T) {
	body := []byte(`{"remarks":"Подписка","dns":{"servers":["ignored"]},"inbounds":[{"port":6666}],"outbounds":[` + nativeMasqueJSON + `,{"protocol":"trojan","tag":"Old","settings":{"servers":[{"address":"old.example.test","port":443,"password":"synthetic"}]},"streamSettings":{"network":"raw","security":"tls"}}, {"protocol":"freedom"}]}`)
	ex, err := extractNodes(body, newMetadata())
	if err != nil {
		t.Fatal(err)
	}
	st, err := stageNodes(subX, ex, true, nil)
	if err != nil || st.Accepted != 2 || st.Rejected != 0 {
		t.Fatalf("native JSON not staged: accepted=%d rejected=%d error=%v", st.Accepted, st.Rejected, err)
	}
	if st.Records[0]["protocol"] != "masque" || !strings.Contains(st.Records[0]["name"].(string), "MASQUE") {
		t.Fatal("name or order changed")
	}
	if len(st.Warnings) == 0 {
		t.Fatal("foreign profile execution warning missing")
	}
	ob, e := servers.Outbound(st.Records[0])
	if e != nil || ob["protocol"] != "masque" {
		t.Fatal("native outbound lost")
	}
	for _, rec := range st.Records {
		if _, ok := rec["inbounds"]; ok {
			t.Fatal("foreign inbound imported")
		}
	}
}

func TestStrictRefreshPreservesCatalog(t *testing.T) {
	e := newEnv(t)
	if _, err := e.sync(t, "replace", linkA); err != nil {
		t.Fatal(err)
	}
	old := liveIDs(t, e)[0]
	before, err := servers.ReadRecord(e.d, old)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"outbounds":[` + nativeMasqueJSON + `,` + strings.Replace(nativeMasqueJSON, `"pass":"synthetic-private-pass"`, `"pass":"synthetic-private-pass","unsupported":"synthetic-private-value"`, 1) + `]}`)
	ex, err2 := extractNodes(body, newMetadata())
	if err2 != nil {
		t.Fatal(err2)
	}
	st, err2 := stageNodes(subX, ex, true, nil)
	if err2 != nil || st.Accepted != 1 || st.Rejected != 1 {
		t.Fatalf("wrong partial classification: %v", err2)
	}
	if strings.Contains(strings.Join(st.Warnings, " "), "synthetic-private") {
		t.Fatal("error leaks credentials")
	}
	res, err := e.s.syncCatalog(context.Background(), subX, st.Records, true, "native-partial", "retain-unmatched")
	if err != nil || res.Retained != 1 || len(liveIDs(t, e)) != 2 {
		t.Fatal("partial import removed old record", err)
	}
	after, err := servers.ReadRecord(e.d, old)
	if err != nil || canonicalHash(before) != canonicalHash(after) {
		t.Fatal("old record changed")
	}
	// Duplicate keys are rejected before a response can replace the catalog.
	duplicate := []byte(`{"outbounds":[` + strings.Replace(nativeMasqueJSON, `"port":443`, `"port":443,"port":8443`, 1) + `]}`)
	if _, err := extractNodes(duplicate, newMetadata()); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
}

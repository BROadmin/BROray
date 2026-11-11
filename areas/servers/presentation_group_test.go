package servers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

const presentationURI = "vless://synthetic-account@EXAMPLE.test:443?type=xhttp&security=reality&sni=one.test&pbk=synthetic-peer&sid=ab&path=%2Fvpn"

func presentationRecord(t *testing.T, id, suffix string) Record {
	t.Helper()
	r, err := ParseLink(presentationURI+suffix, id, map[string]any{"type": "subscription", "subscriptionId": id, "nodeIndex": 0})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func presentationSummary(t *testing.T, records ...Record) (map[string]map[string]any, map[string]any) {
	t.Helper()
	e := newEnv(t)
	stored := map[string][]byte{}
	for _, rec := range records {
		if err := WriteRecord(e.d, rec); err != nil {
			t.Fatal(err)
		}
		stored[asString(rec["id"])], _ = os.ReadFile(RecordPath(e.d, asString(rec["id"])))
	}
	if err := os.MkdirAll(e.d.Path("config", "subscriptions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(e.d.Path("config", "subscriptions", "json.json"), map[string]any{"id": "json", "name": "JSON <source>"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.d.Path("config", "active-server"), []byte("json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	summary, err := e.s.summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for id, before := range stored {
		after, err := os.ReadFile(RecordPath(e.d, id))
		if err != nil || string(after) != string(before) {
			t.Fatal("summary changed durable record", id)
		}
	}
	rows := map[string]map[string]any{}
	for _, raw := range summary["servers"].([]any) {
		row := raw.(map[string]any)
		rows[asString(row["id"])] = row
	}
	return rows, summary
}

func TestPresentationGroupVariantsPreserveRecords(t *testing.T) {
	uri := presentationRecord(t, "uri", "")
	js := presentationRecord(t, "json", "&spx=%2Fclient&fp=firefox&extra="+url.QueryEscape(`{"xPaddingBytes":"100-200","xmux":{"maxConnections":1}}`))
	clash := normalize(uri)
	clash["id"], clash["name"], clash["source"] = "clash", "Same label", map[string]any{"type": "subscription", "subscriptionId": "missing"}
	clash["address"] = "example.test"
	for _, block := range []string{"tls", "reality"} {
		clash[block].(map[string]any)["serverName"] = "two.test"
	}
	clash["reality"].(map[string]any)["shortId"] = "cd"
	before, _ := json.Marshal([]Record{uri, js, clash})
	rows, summary := presentationSummary(t, uri, js, clash)
	if summary["total"] != 3 || len(rows) != 3 {
		t.Fatal("presentation lost independently owned records")
	}
	if get(summary, "activeServer", "id") != "json" || rows["json"]["active"] != true {
		t.Error("group changed exact active record")
	}
	for id, row := range rows {
		if row["displayGroupId"] != "clash" {
			t.Errorf("%s: expected stable representative clash, got %v", id, row["displayGroupId"])
		}
	}
	if rows["json"]["subscriptionName"] != "JSON <source>" || rows["clash"]["subscriptionName"] != "missing" {
		t.Error("local source names/fallback missing")
	}
	reversed, _ := presentationSummary(t, clash, js, uri)
	if reversed["uri"]["displayGroupId"] != rows["uri"]["displayGroupId"] {
		t.Error("group id depends on record order")
	}
	after, _ := json.Marshal([]Record{uri, js, clash})
	if string(before) != string(after) {
		t.Error("presentation mutated records")
	}
}

func TestPresentationGroupCredentialAndPolicyBoundaries(t *testing.T) {
	base := "vless://synthetic-account@example.test:443?type=xhttp&security=tls"
	for _, tc := range []struct{ name, a, b string }{
		{"password", "trojan://one@example.test:443", "trojan://two@example.test:443"},
		{"username", "http://one:pw@example.test:443", "http://two:pw@example.test:443"},
		{"proxy-password", "socks://one:pw@example.test:443", "socks://one:other@example.test:443"},
		{"protocol", "http://one:pw@example.test:443", "socks://one:pw@example.test:443"},
		{"method", "ss://" + base64.RawStdEncoding.EncodeToString([]byte("aes-128-gcm:pw")) + "@example.test:443", "ss://" + base64.RawStdEncoding.EncodeToString([]byte("aes-256-gcm:pw")) + "@example.test:443"},
		{"port", base, strings.Replace(base, ":443?", ":444?", 1)},
		{"security", base, strings.Replace(base, "security=tls", "security=none", 1)},
		{"verification", base, base + "&vcn=verify.test"},
		{"encryption", base, base + "&encryption=mlkem768x25519plus.native.0rtt." + strings.Repeat("A", 43)},
		{"pin", base, base + "&pcs=" + strings.Repeat("ab", 32)},
		{"service", "vless://test@example.test:443?type=grpc&security=tls&serviceName=one", "vless://test@example.test:443?type=grpc&security=tls&serviceName=two"},
		{"download", base, base + "&extra=" + url.QueryEscape(`{"downloadSettings":{"network":"xhttp","address":"download.test","port":443,"security":"tls","tlsSettings":{"serverName":"download.test"}}}`)},
		{"stream-auth-header", "vless://test@example.test:443?type=ws", "vless://test@example.test:443?type=ws&streamOptions=" + url.QueryEscape(`{"wsSettings":{"headers":{"Authorization":"synthetic-token"}}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := ParseLink(tc.a, "a", nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := ParseLink(tc.b, "b", nil)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := presentationSummary(t, a, b)
			if rows["a"]["displayGroupId"] == rows["b"]["displayGroupId"] {
				t.Fatal("unsafe credential or security-policy merge")
			}
		})
	}
}

func TestPresentationGroupEquivalentXHTTPSerialization(t *testing.T) {
	for _, mode := range []string{"auto", "stream-up"} {
		t.Run(mode, func(t *testing.T) {
			uri := presentationRecord(t, "uri", "&mode="+mode+"&extra="+url.QueryEscape(`{"mode":"`+mode+`","headers":{},"noGRPCHeader":false,"noSSEHeader":false,"xPaddingBytes":"100-200","xPaddingObfsMode":true,"xmux":{"maxConnections":1},"scMaxEachPostBytes":10000,"scMinPostsIntervalMs":30,"scMaxBufferedPosts":8,"scStreamUpServerSecs":60}`))
			js := presentationRecord(t, "json", "&mode="+mode)
			rows, _ := presentationSummary(t, uri, js)
			if rows["uri"]["displayGroupId"] != rows["json"]["displayGroupId"] {
				t.Fatal("equivalent URI/JSON XHTTP mode/default/client tuning produced separate cards")
			}
		})
	}
}

func TestPresentationGroupFullWireGuardProfile(t *testing.T) {
	ini := "[Interface]\nPrivateKey = " + awgPrivate + "\nAddress = 10.0.0.2/32\nDNS = 1.1.1.1\n[Peer]\nPublicKey = " + awgPublic + "\nEndpoint = wg.example:443\nAllowedIPs = 0.0.0.0/0\n"
	a, err := ParseLink(ini, "wg-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseLink("# serialization only\n"+ini, "wg-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseLink(strings.Replace(ini, "1.1.1.1", "8.8.8.8", 1), "wg-c", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := presentationSummary(t, a, b, c)
	if rows["wg-a"]["displayGroupId"] != "wg-a" || rows["wg-b"]["displayGroupId"] != "wg-a" || rows["wg-c"]["displayGroupId"] != "wg-c" {
		t.Fatal("WG semantic profile grouping mismatch")
	}
}

func TestPresentationGroupSeparatesIdentity(t *testing.T) {
	base := presentationRecord(t, "base", "")
	for name, mutate := range map[string]func(Record){
		"auth":     func(r Record) { r["uuid"] = "different-account" },
		"peer":     func(r Record) { r["reality"].(map[string]any)["publicKey"] = "another-peer" },
		"mldsa":    func(r Record) { r["reality"].(map[string]any)["mldsa65Verify"] = strings.Repeat("A", 2603) },
		"endpoint": func(r Record) { r["address"] = "other.test" },
		"path": func(r Record) {
			r["xhttp"].(map[string]any)["path"] = "/other"
			r["transport"].(map[string]any)["path"] = "/other"
		},
		"host": func(r Record) { r["transport"].(map[string]any)["host"] = "routing.test" },
		"header": func(r Record) {
			r["xhttp"].(map[string]any)["extra"] = map[string]any{"headers": map[string]any{"Authorization": "other-token"}}
			r["transport"].(map[string]any)["extra"] = r["xhttp"].(map[string]any)["extra"]
		},
	} {
		t.Run(name, func(t *testing.T) {
			other := normalize(base)
			other["id"] = "other"
			mutate(other)
			rows, _ := presentationSummary(t, base, other)
			if rows["base"]["displayGroupId"] == rows["other"]["displayGroupId"] {
				t.Fatal("unsafe identity merge")
			}
		})
	}
	for _, kind := range []string{"xdrive", "masque"} {
		t.Run(kind, func(t *testing.T) {
			a, err := ParseNativeOutbound(nativeTestOutbound(kind), "Same label", "cloud-a", nil)
			if err != nil {
				t.Fatal(err)
			}
			b := normalize(a)
			b["id"] = "cloud-b"
			if kind == "xdrive" {
				b["xdrive"].(map[string]any)["remoteFolder"] = "other-folder"
			} else {
				b["masque"].(map[string]any)["pass"] = "other-pass"
			}
			rows, _ := presentationSummary(t, base, a, b)
			if rows["cloud-a"]["displayGroupId"] == rows["cloud-b"]["displayGroupId"] || rows["base"]["displayGroupId"] == rows["cloud-a"]["displayGroupId"] {
				t.Fatal("unsafe cloud grouping")
			}
		})
	}
}

func TestPresentationGroupParsedAWG(t *testing.T) {
	a, err := ParseLink(awgLink(t, true, nil), "awg-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseLink(awgLink(t, true, func(c map[string]any) { c["config"] = "# serialization comment\n" + c["config"].(string) }), "awg-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseLink(awgLink(t, true, func(c map[string]any) { c["config"] = strings.Replace(c["config"].(string), awgPublic, awgPrivate, 1) }), "awg-c", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := presentationSummary(t, a, b, c)
	if rows["awg-a"]["displayGroupId"] != "awg-a" || rows["awg-b"]["displayGroupId"] != "awg-a" {
		t.Error("serialization-only AWG variants not grouped")
	}
	if rows["awg-c"]["displayGroupId"] == rows["awg-a"]["displayGroupId"] {
		t.Error("different AWG peer grouped")
	}
}

package subscriptions

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func sharedClashNode() map[string]any {
	return map[string]any{"type": "vless", "name": "Shared Clash", "server": "edge.example.test", "port": 443,
		"uuid": parserCoverageID, "tls": false, "network": "ws", "ws-opts": map[string]any{"path": "/shared"}}
}

func TestSharedImportClashDocumentForms(t *testing.T) {
	node := sharedClashNode()
	profile := map[string]any{"proxies": []any{node}, "dns": map[string]any{"nameserver": []any{"synthetic-private-dns"}},
		"proxy-providers": map[string]any{"remote": map[string]any{"url": "https://provider.example.test/private-export"}}}
	yaml := "proxies:\n  - name: Shared Clash\n    type: vless\n    server: edge.example.test\n    port: 443\n    uuid: " + parserCoverageID + "\n    tls: false\n    network: ws\n    ws-opts:\n      path: /shared\n"
	control := sharedFormsStage(t, yaml)
	for _, form := range []struct{ name, body string }{
		{"yaml-profile", yaml},
		{"yaml-bare", "name: Shared Clash\ntype: vless\nserver: edge.example.test\nport: 443\nuuid: " + parserCoverageID + "\ntls: false\nnetwork: ws\nws-opts: {path: /shared}\n"},
		{"json-profile", sharedFormsJSON(t, profile)},
		{"json-bare", sharedFormsJSON(t, node)},
		{"base64-yaml", base64.StdEncoding.EncodeToString([]byte(yaml))},
		{"base64-json", base64.RawURLEncoding.EncodeToString([]byte(sharedFormsJSON(t, profile)))},
	} {
		t.Run(form.name, func(t *testing.T) {
			manual, err := servers.ParseLink(form.body, "shared-clash", nil)
			if err != nil {
				t.Fatalf("manual supported Clash document rejected: %v", err)
			}
			st := sharedFormsStage(t, form.body)
			if st.Received != 1 || st.Accepted != 1 {
				t.Fatal("Clash singleton count changed")
			}
			sharedFormsSameConnection(t, manual, control.Records[0])
			sharedFormsSameConnection(t, st.Records[0], control.Records[0])
			if manual["importProfile"] != nil {
				t.Fatal("supported mapped Clash server was marked unavailable")
			}
			for _, record := range []servers.Record{manual, st.Records[0]} {
				encoded := sharedFormsJSON(t, record)
				if strings.Contains(encoded, "synthetic-private-dns") || strings.Contains(encoded, "private-export") {
					t.Fatal("document policy/provider was copied into a server")
				}
			}
		})
	}
}

func TestSharedImportClashUnavailableSubscription(t *testing.T) {
	unsupported := map[string]any{"type": "tuic", "name": "Unavailable", "server": "unavailable.example.test", "port": 443,
		"uuid": parserCoverageID, "password": "synthetic-private-password"}
	unknown := sharedClashNode()
	unknown["synthetic-private-field-name"] = "synthetic-private-field-value"
	body := sharedFormsJSON(t, map[string]any{"proxies": []any{sharedClashNode(), unsupported, unknown, unsupported}})
	st := sharedFormsStage(t, body)
	if st.Received != 4 || st.Parsed != 4 || st.Accepted != 3 || st.Rejected != 0 {
		t.Fatal("valid unavailable nodes were dropped or counted incorrectly")
	}
	for i := 1; i < 3; i++ {
		if st.Records[i]["importProfile"] == nil {
			t.Fatal("unavailable node lost its typed private source")
		}
		if _, err := servers.Outbound(st.Records[i]); err == nil {
			t.Fatal("unavailable node generated a reduced outbound")
		}
	}
	if strings.Contains(strings.Join(st.Warnings, " "), "synthetic-private") {
		t.Fatal("unavailable-node warnings exposed private fields")
	}
	if _, err := servers.ParseLink(body, "shared-clash", nil); err == nil {
		t.Fatal("manual singleton import selected one of multiple Clash nodes")
	}
}

func TestSharedImportClashDocumentStrictness(t *testing.T) {
	for _, body := range []string{
		"proxies:\n  - {type: vless, type: tuic, name: Bad, server: edge.example.test, port: 443}\n",
		"proxies: []\n---\nproxies: []\n",
		"proxies: &loop [*loop]\n",
		"proxies: [{type: vless, name: Bad, server: edge.example.test, port: 443, uuid: false, synthetic-private-field: value}]\n",
		`{"proxies":[{"type":"tuic","name":"Bad","server":"edge.example.test","port":443,"port":8443}]}`,
	} {
		if _, err := servers.ParseLink(body, "shared-clash", nil); err == nil {
			t.Fatal("ambiguous or malformed manual Clash document accepted")
		}
		if ex, err := extractNodes([]byte(body), newMetadata()); err == nil {
			if st, stageErr := stageNodes("shared-clash", ex, true, nil); stageErr == nil || st.Accepted > 0 {
				t.Fatal("ambiguous or malformed Clash document retained as unavailable")
			}
		}
	}
}

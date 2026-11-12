package servers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/clashschema"
)

func unavailableClashFixture(kind string) map[string]any {
	node := map[string]any{"type": kind, "name": "Synthetic unavailable", "server": "edge.example.test", "port": 443,
		"uuid": "00000000-0000-4000-8000-000000000001", "password": "synthetic-private-password"}
	if kind == "vless" {
		delete(node, "password")
		node["synthetic-private-extension-name"] = map[string]any{"token": "synthetic-private-extension-value"}
	}
	return node
}

func unavailableClashDocument(t *testing.T, node map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"proxies": []any{node},
		"dns":             map[string]any{"nameserver": []any{"synthetic-private-router-dns"}},
		"rules":           []any{"MATCH,synthetic-private-route"},
		"proxy-providers": map[string]any{"remote": map[string]any{"url": "https://provider.example.test/private-subscription"}}})
	if err != nil {
		t.Fatal("could not encode synthetic Clash document")
	}
	return string(b)
}

func unavailableClashRecord(t *testing.T, kind string) Record {
	t.Helper()
	record, err := ParseLink(unavailableClashDocument(t, unavailableClashFixture(kind)), "unavailable-clash", nil)
	if err != nil {
		t.Fatalf("valid unavailable client profile was not retained: %v", err)
	}
	return record
}

func TestClashUnavailablePrivateRecord(t *testing.T) {
	for _, kind := range []string{"tuic", "vless"} {
		t.Run(kind, func(t *testing.T) {
			record := unavailableClashRecord(t, kind)
			if record["protocol"] != kind || record["address"] != "edge.example.test" || Validate(record, "unavailable-clash") != nil {
				t.Fatal("unavailable profile lost its actual protocol or valid record shape")
			}
			profile, ok := record["importProfile"].(map[string]any)
			if !ok || profile["format"] != "clash" || profile["sourceCommit"] != clashschema.SourceCommit {
				t.Fatal("private typed source and pinned schema provenance missing")
			}
			node, ok := profile["node"].(map[string]any)
			if !ok || node["type"] != kind {
				t.Fatal("private normalized server node missing")
			}
			private, err := json.Marshal(profile)
			if err != nil || !strings.Contains(string(private), "synthetic-private") {
				t.Fatal("unmapped private server values were discarded")
			}
			if strings.Contains(string(private), "synthetic-private-router-dns") || strings.Contains(string(private), "private-subscription") || strings.Contains(string(private), "synthetic-private-route") {
				t.Fatal("router policy or provider URL escaped endpoint-only extraction")
			}
			for _, label := range []string{"dns", "routing", "rules", "proxy-groups", "proxy-providers"} {
				if has(record, label) || has(node, label) {
					t.Fatal("foreign router policy stored as a server field")
				}
			}
			masked, err := json.Marshal(MaskRecord(record))
			if err != nil || strings.Contains(string(masked), "synthetic-private") || strings.Contains(string(masked), "importProfile") {
				t.Fatal("private unavailable source escaped public masking")
			}
			warnings, _ := json.Marshal(record["importWarnings"])
			if len(warnings) == 0 || string(warnings) == "null" || strings.Contains(string(warnings), "synthetic-private") {
				t.Fatal("explicit unavailable warning missing or private")
			}
			view := connectionSupport(record)
			if view["supported"] != false || view["code"] != "PROTOCOL_NOT_SUPPORTED" || requireConnectionSupport(record) == nil {
				t.Fatal("unavailable source was declared connectable")
			}
			if _, err := Outbound(record); err == nil {
				t.Fatal("unavailable source generated a reduced Xray outbound")
			}
			if _, err := XrayConfig(record, GenSettings{}, "unavailable-clash"); err == nil {
				t.Fatal("unavailable source generated an executable Xray config")
			}
			record["connectionSupport"] = map[string]any{"supported": true}
			if connectionSupport(record)["supported"] != false {
				t.Fatal("persisted arbitrary flag bypassed capability revalidation")
			}
		})
	}
}

func TestClashUnavailableIdentityAndBackup(t *testing.T) {
	record := unavailableClashRecord(t, "tuic")
	wantKey, err := CanonicalKey(record)
	if err != nil || len(wantKey) != 64 {
		t.Fatal("unavailable profile has no canonical identity")
	}
	renamed := normalize(record)
	renamed["name"], renamed["id"] = "Another label", "another-id"
	renamed["source"] = map[string]any{"type": "subscription", "subscriptionId": "synthetic"}
	get(renamed, "importProfile", "node").(map[string]any)["name"] = "Another label"
	if key, err := CanonicalKey(renamed); err != nil || key != wantKey {
		t.Fatal("display name or source changed unavailable connection identity")
	}
	repinned := normalize(record)
	repinned["importProfile"].(map[string]any)["sourceCommit"] = "different-schema-metadata"
	if key, err := CanonicalKey(repinned); err != nil || key != wantKey {
		t.Fatal("schema provenance changed unavailable connection identity")
	}
	if Validate(repinned, "unavailable-clash") == nil {
		t.Fatal("identity metadata exclusion bypassed source provenance validation")
	}
	changed := normalize(record)
	get(changed, "importProfile", "node").(map[string]any)["password"] = "synthetic-private-rotated"
	if key, err := CanonicalKey(changed); err != nil || key == wantKey {
		t.Fatal("different unavailable credentials merged into one connection")
	}
	raw, err := Marshal(record)
	if err != nil {
		t.Fatal("unavailable record cannot be backed up")
	}
	restored, err := DecodeBackupRecord(raw, "unavailable-clash")
	if err != nil || !jsonValueEqual(get(restored, "importProfile"), get(record, "importProfile")) {
		t.Fatal("backup discarded private unavailable fields")
	}
	if key, err := CanonicalKey(restored); err != nil || key != wantKey || connectionSupport(restored)["supported"] != false {
		t.Fatal("backup changed identity or enabled unavailable connection")
	}
}

func TestClashUnavailableRevalidatesStoredSource(t *testing.T) {
	control := unavailableClashRecord(t, "tuic")
	for _, mutate := range []struct {
		name string
		fn   func(Record)
	}{
		{"format", func(r Record) { r["importProfile"].(map[string]any)["format"] = "unknown" }},
		{"source-commit", func(r Record) { r["importProfile"].(map[string]any)["sourceCommit"] = "unverified" }},
		{"node-type", func(r Record) { get(r, "importProfile", "node").(map[string]any)["type"] = "invented-protocol" }},
		{"node-port", func(r Record) { get(r, "importProfile", "node").(map[string]any)["port"] = "synthetic-private-invalid" }},
		{"protocol", func(r Record) { r["protocol"] = "vless" }},
		{"address", func(r Record) { r["address"] = "different.example.test" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			r := normalize(control)
			mutate.fn(r)
			if err := Validate(r, "unavailable-clash"); err == nil || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("tampered private source accepted or leaked")
			}
			if _, err := Outbound(r); err == nil {
				t.Fatal("tampered unavailable source became runnable")
			}
		})
	}
}

func TestClashUnavailableNeverAbsorbsMalformedSupportedNode(t *testing.T) {
	for _, mutation := range []map[string]any{
		{"port": 0}, {"port": "bad"}, {"uuid": false}, {"tls": "true"}, {"server": ""},
	} {
		node := unavailableClashFixture("vless")
		for key, value := range mutation {
			node[key] = value
		}
		if _, err := ParseLink(unavailableClashDocument(t, node), "invalid-clash", nil); err == nil {
			t.Fatal("malformed supported field was stored as unavailable instead of rejected")
		}
	}
}

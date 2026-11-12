package servers

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/xrayschema"
)

func unavailableXrayFixture(kind string) map[string]any {
	out := fullXray357Base("vless")
	out["tag"] = "Synthetic unavailable"
	settings := out["settings"].(map[string]any)
	if kind == "reverse" {
		settings["reverse"] = map[string]any{"tag": "synthetic-private-bridge", "sniffing": map[string]any{"enabled": true}}
	} else {
		settings["level"] = 4
		out["synthetic-private-extension-name"] = map[string]any{"token": "synthetic-private-extension-value"}
	}
	return out
}

func unavailableXrayRecord(t *testing.T, kind string) Record {
	t.Helper()
	rec, err := ParseLink(fullXray357JSON(t, unavailableXrayFixture(kind)), "xray-unavailable", nil)
	if err != nil {
		t.Fatalf("valid unavailable Xray source rejected: %v", err)
	}
	return rec
}

func TestXrayUnavailablePrivateSourceForms(t *testing.T) {
	for _, kind := range []string{"reverse", "level-extension"} {
		out := unavailableXrayFixture(kind)
		profile := map[string]any{"outbounds": []any{out}, "dns": map[string]any{"servers": []any{"synthetic-private-router-dns"}}, "routing": map[string]any{"domainStrategy": "synthetic-private-route"}}
		for name, body := range map[string]string{
			"bare":    fullXray357JSON(t, out),
			"profile": fullXray357JSON(t, profile),
			"array":   fullXray357JSON(t, []any{out}),
			"base64":  base64.StdEncoding.EncodeToString([]byte(fullXray357JSON(t, profile))),
		} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				rec, err := ParseLink(body, "xray-unavailable", nil)
				if err != nil || Validate(rec, "xray-unavailable") != nil {
					t.Fatalf("valid unavailable Xray document rejected: %v", err)
				}
				if rec["protocol"] != "vless" || rec["address"] != "edge.example.test" {
					t.Fatal("source protocol or validated endpoint lost")
				}
				private, ok := rec["importProfile"].(map[string]any)
				if !ok || private["format"] != "xray" || private["sourceCommit"] != xrayschema.SourceCommit {
					t.Fatal("pinned private Xray source missing")
				}
				node, ok := private["node"].(map[string]any)
				if !ok || !jsonValueEqual(node, out) {
					t.Fatal("unavailable source was reduced or replaced by validation projection")
				}
				encoded := fullXray357JSON(t, rec)
				if strings.Contains(encoded, "synthetic-private-router-dns") || strings.Contains(encoded, "synthetic-private-route") {
					t.Fatal("root DNS or routing policy copied into server source")
				}
				masked := fullXray357JSON(t, MaskRecord(rec))
				if strings.Contains(masked, "synthetic-private") || strings.Contains(masked, "importProfile") {
					t.Fatal("private unsupported source escaped masking")
				}
				warnings := fullXray357JSON(t, rec["importWarnings"])
				if warnings == "null" || strings.Contains(warnings, "synthetic-private") {
					t.Fatal("source unavailable warning missing or private")
				}
				if connectionSupport(rec)["supported"] != false || requireConnectionSupport(rec) == nil {
					t.Fatal("validation projection was declared connectable")
				}
				if _, err := Outbound(rec); err == nil {
					t.Fatal("unavailable Xray source generated a reduced outbound")
				}
				if _, err := XrayConfig(rec, GenSettings{}, "xray-unavailable"); err == nil {
					t.Fatal("unavailable Xray source generated executable config")
				}
				rec["connectionSupport"] = map[string]any{"supported": true}
				if connectionSupport(rec)["supported"] != false {
					t.Fatal("stored support flag bypassed source revalidation")
				}
			})
		}
	}
}

func TestXrayUnavailableIdentityBackupAndRevalidation(t *testing.T) {
	rec := unavailableXrayRecord(t, "reverse")
	want, err := CanonicalKey(rec)
	if err != nil || len(want) != 64 {
		t.Fatal("unavailable source lacks canonical identity")
	}
	renamed := normalize(rec)
	renamed["name"], renamed["id"] = "Renamed", "renamed"
	get(renamed, "importProfile", "node").(map[string]any)["tag"] = "Renamed"
	if key, err := CanonicalKey(renamed); err != nil || key != want {
		t.Fatal("display tag changed source identity")
	}
	repinned := normalize(rec)
	repinned["importProfile"].(map[string]any)["sourceCommit"] = "different-schema-metadata"
	if key, err := CanonicalKey(repinned); err != nil || key != want {
		t.Fatal("schema provenance changed source identity")
	}
	if Validate(repinned, "xray-unavailable") == nil {
		t.Fatal("identity metadata exclusion bypassed source provenance validation")
	}
	changed := normalize(rec)
	get(changed, "importProfile", "node", "settings", "reverse").(map[string]any)["tag"] = "synthetic-private-other-bridge"
	if key, err := CanonicalKey(changed); err != nil || key == want {
		t.Fatal("different reverse dependency merged as same source")
	}
	raw, err := Marshal(rec)
	if err != nil {
		t.Fatal("unavailable source backup failed")
	}
	restored, err := DecodeBackupRecord(raw, "xray-unavailable")
	if err != nil || !jsonValueEqual(rec["importProfile"], restored["importProfile"]) {
		t.Fatal("backup lost unavailable private source")
	}
	if key, err := CanonicalKey(restored); err != nil || key != want || connectionSupport(restored)["supported"] != false {
		t.Fatal("restored identity or unavailable status changed")
	}
	for name, mutate := range map[string]func(Record){
		"source-commit": func(r Record) { r["importProfile"].(map[string]any)["sourceCommit"] = "unverified" },
		"endpoint":      func(r Record) { get(r, "importProfile", "node", "settings").(map[string]any)["port"] = 0 },
		"tls": func(r Record) {
			get(r, "importProfile", "node", "streamSettings", "tlsSettings").(map[string]any)["minVersion"] = "synthetic-private-invalid"
		},
		"summary-address":      func(r Record) { r["address"] = "different.example.test" },
		"no-capability-reason": func(r Record) { delete(get(r, "importProfile", "node", "settings").(map[string]any), "reverse") },
	} {
		t.Run(name, func(t *testing.T) {
			bad := normalize(rec)
			mutate(bad)
			if err := Validate(bad, "xray-unavailable"); err == nil || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("tampered source accepted or exposed in validation error")
			}
			if _, err := Outbound(bad); err == nil {
				t.Fatal("tampered unavailable source became executable")
			}
		})
	}
}

func TestXrayUnavailableMalformedMappedFieldsReject(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"port": func(out map[string]any) { out["settings"].(map[string]any)["port"] = 0 },
		"uuid": func(out map[string]any) { out["settings"].(map[string]any)["id"] = false },
		"tls": func(out map[string]any) {
			get(out, "streamSettings", "tlsSettings").(map[string]any)["minVersion"] = "synthetic-private-invalid"
		},
		"reverse": func(out map[string]any) { out["settings"].(map[string]any)["reverse"] = map[string]any{"tag": false} },
	} {
		t.Run(name, func(t *testing.T) {
			out := unavailableXrayFixture("reverse")
			mutate(out)
			_, err := ParseLink(fullXray357JSON(t, out), "xray-invalid", nil)
			if err == nil || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("malformed mapped field became an unavailable source or leaked")
			}
		})
	}
}

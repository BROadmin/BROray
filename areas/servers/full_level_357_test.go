package servers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/xrayschema"
)

// Exact core 676eaf6 uses policy.DefaultManager without a root policy stanza.
// Level 1 has a distinct idle timeout; every other level uses SessionDefault.
func fullLevel357Fixture(protocol string, legacy bool, value any) (map[string]any, string) {
	o, path := fullXray357EmailFixture(protocol, legacy, "")
	account := o["settings"].(map[string]any)
	if legacy {
		switch protocol {
		case "vless", "vmess":
			account = account["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
		case "http", "socks":
			account = account["servers"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
		default:
			account = account["servers"].([]any)[0].(map[string]any)
		}
	}
	delete(account, "email")
	account["level"] = value
	return o, strings.TrimSuffix(path, "email") + "level"
}

func TestFullLevel357Preservation(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "http", "socks", "trojan", "shadowsocks"} {
		maximum := "4294967295"
		if protocol == "trojan" || protocol == "shadowsocks" {
			maximum = "255" // The pinned JSON config declares byte, not uint32.
		}
		for _, legacy := range []bool{false, true} {
			for _, level := range []string{"0", "1", "8", maximum} {
				t.Run(fmt.Sprintf("%s/legacy-%t/%s", protocol, legacy, level), func(t *testing.T) {
					o, path := fullLevel357Fixture(protocol, legacy, json.Number(level))
					r := fullXray357Parse(t, o)
					if fmt.Sprint(r["accountLevel"]) != level || r["network"] == "unavailable" || has(r, "importProfile") {
						t.Fatal("valid account level must be retained and runnable under local core policy")
					}
					out := fullXray357Roundtrip(t, r, nil)
					if _, flat := fullXray357At(out, "settings.address"); flat {
						path = "settings.level"
					}
					fullXray357Assert(t, out, path, json.Number(level))
					if has(MaskRecord(r), "accountLevel") {
						t.Fatal("private account metadata escaped the public record")
					}
				})
			}
		}
	}
	t.Run("native-xdrive", func(t *testing.T) {
		o := nativeTestOutbound("xdrive")
		user := get(o, "settings", "vnext").([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
		user["level"] = json.Number("4294967295")
		r := fullXray357Parse(t, o)
		fullXray357Roundtrip(t, r, map[string]any{"settings.vnext.0.users.0.level": json.Number("4294967295")})
	})
}

func TestFullLevel357Validation(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "http", "socks", "trojan", "shadowsocks"} {
		bad := []any{nil, "8", "synthetic-private-value", true, -1, 0.5, json.Number("4294967296")}
		if protocol == "trojan" || protocol == "shadowsocks" {
			bad = append(bad, 256, json.Number("4294967295"))
		}
		for _, legacy := range []bool{false, true} {
			for i, value := range bad {
				t.Run(fmt.Sprintf("%s/legacy-%t/invalid-%d", protocol, legacy, i), func(t *testing.T) {
					o, _ := fullLevel357Fixture(protocol, legacy, value)
					fullXray357Reject(t, o)
					base := fullXray357Parse(t, fullXray357Base(protocol))
					base["accountLevel"] = value
					if Validate(base, "level-test") == nil {
						t.Fatal("malformed stored account level accepted")
					}
					if _, err := Outbound(base); err == nil {
						t.Fatal("malformed account level reached core output")
					}
				})
			}
		}
	}
}

func TestFullLevel357IdentityAndLocalPolicy(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "http", "socks", "trojan", "shadowsocks"} {
		t.Run(protocol, func(t *testing.T) {
			baseInput, _ := fullLevel357Fixture(protocol, false, 0)
			delete(baseInput["settings"].(map[string]any), "level")
			base := fullXray357Parse(t, baseInput)
			baseKey, err := presentationKey(base)
			if err != nil || has(base, "accountLevel") {
				t.Fatal("absent level changed")
			}
			canonical := map[string]bool{}
			for _, value := range []int{0, 1, 8} {
				o, _ := fullLevel357Fixture(protocol, false, value)
				r := fullXray357Parse(t, o)
				key, err := CanonicalKey(r)
				if err != nil || canonical[key] {
					t.Fatal("exact private account level lost in canonical identity")
				}
				canonical[key] = true
				display, err := presentationKey(r)
				if err != nil || (display == baseKey) != (value != 1) {
					t.Fatal("display identity must follow the actual local default policy, including special level 1")
				}
				profile := map[string]any{"outbounds": []any{o}, "policy": map[string]any{"levels": map[string]any{"8": map[string]any{"connIdle": 9}}}, "dns": map[string]any{"servers": []any{"192.0.2.53"}}, "routing": map[string]any{"domainStrategy": "AsIs"}}
				imported, err := ParseLink(fullXray357JSON(t, profile), "level-profile", nil)
				if err != nil {
					t.Fatal("profile level import failed")
				}
				cfg, err := XrayConfig(imported, GenSettings{}, "level-profile")
				if err != nil {
					t.Fatal("local default policy disabled an ordinary account level")
				}
				var root map[string]any
				if json.Unmarshal(cfg, &root) != nil || has(root, "policy") || has(root, "dns") || has(root, "routing") {
					t.Fatal("provider process policy was applied")
				}
			}
		})
	}
}

func TestFullLevel357NoAuth(t *testing.T) {
	for _, protocol := range []string{"http", "socks"} {
		for _, level := range []int{0, 1, 8} {
			t.Run(fmt.Sprintf("%s/%d", protocol, level), func(t *testing.T) {
				o, _ := fullLevel357Fixture(protocol, false, level)
				s := o["settings"].(map[string]any)
				delete(s, "user")
				delete(s, "pass")
				r := fullXray357Parse(t, o)
				fullXray357Roundtrip(t, r, map[string]any{"settings.level": level, "settings.address": "edge.example.test"})
				if has(r, "username") && asString(r["username"]) != "" {
					t.Fatal("preserving level invented authentication")
				}
				delete(s, "level")
				base := fullXray357Parse(t, o)
				a, ea := presentationKey(r)
				b, eb := presentationKey(base)
				if ea != nil || eb != nil || a != b {
					t.Fatal("core-ignored metadata changed an unauthenticated endpoint's display identity")
				}
			})
		}
	}
}

func fullLevel357C3Record(t *testing.T, source map[string]any) Record {
	t.Helper()
	r := fullXray357Parse(t, source)
	return normalize(Record{"schemaVersion": 2, "id": r["id"], "name": r["name"], "source": r["source"],
		"protocol": r["protocol"], "address": r["address"], "port": r["port"], "network": "unavailable", "security": "none",
		"importProfile": map[string]any{"format": "xray", "sourceCommit": xrayschema.SourceCommit, "node": source}})
}

func TestFullLevel357C3WrapperCompatibility(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "http", "socks", "trojan", "shadowsocks"} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy-%t", protocol, legacy), func(t *testing.T) {
				o, _ := fullLevel357Fixture(protocol, legacy, 8)
				r := fullLevel357C3Record(t, o)
				if Validate(r, "saved-c3") != nil {
					t.Fatal("valid saved C3 level-only wrapper rejected")
				}
				body, _ := Marshal(r)
				restored, err := DecodeBackupRecord(body, "saved-c3")
				if err != nil || fullXray357JSON(t, restored["importProfile"]) != fullXray357JSON(t, r["importProfile"]) {
					t.Fatal("C3 private source lost during backup restore")
				}
				if _, err := Outbound(restored); err == nil {
					t.Fatal("historical wrapper was automatically activated")
				}
				if _, err := XrayConfig(restored, GenSettings{}, "saved-c3"); err == nil {
					t.Fatal("historical wrapper reached process configuration")
				}
				if has(MaskRecord(restored), "importProfile") {
					t.Fatal("C3 private source escaped public output")
				}
				projection := fullXray357Parse(t, o)
				if _, err := WrapUnavailableXray(projection, o); err == nil {
					t.Fatal("compatibility exception created a new unsupported wrapper")
				}
			})
		}
	}
	for _, value := range []any{0, nil, "8", -1, json.Number("4294967296")} {
		o, _ := fullLevel357Fixture("vless", false, 8)
		r := fullLevel357C3Record(t, o)
		get(r, "importProfile", "node", "settings").(map[string]any)["level"] = value
		if Validate(r, "saved-c3-invalid") == nil {
			t.Fatal("historical level exception admitted malformed or neutral-only source")
		}
	}
	o, _ := fullLevel357Fixture("vless", false, 8)
	r := fullLevel357C3Record(t, o)
	get(r, "importProfile", "node", "settings").(map[string]any)["port"] = 0
	if Validate(r, "saved-c3-invalid") == nil {
		t.Fatal("historical exception bypassed endpoint validation")
	}
}

func TestFullLevel357ExplicitProxyAccount(t *testing.T) {
	for _, protocol := range []string{"http", "socks"} {
		for _, level := range []int{0, 1, 8} {
			t.Run(fmt.Sprintf("%s/%d", protocol, level), func(t *testing.T) {
				o, path := fullLevel357Fixture(protocol, true, level)
				account := get(o, "settings", "servers").([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
				account["user"], account["pass"] = "", ""
				r := fullXray357Parse(t, o)
				if r["proxyAccount"] != true {
					t.Fatal("explicit empty legacy account presence lost")
				}
				fullXray357Roundtrip(t, r, map[string]any{path: level, "settings.servers.0.users.0.user": "", "settings.servers.0.users.0.pass": ""})
				if has(MaskRecord(r), "proxyAccount") {
					t.Fatal("private account marker escaped public output")
				}
				flat, _ := fullLevel357Fixture(protocol, false, level)
				delete(flat["settings"].(map[string]any), "user")
				delete(flat["settings"].(map[string]any), "pass")
				noAccount := fullXray357Parse(t, flat)
				a, _ := presentationKey(r)
				b, _ := presentationKey(noAccount)
				if a == b {
					t.Fatal("explicit account merged with absent account")
				}
				for _, bad := range []any{nil, "true", 1} {
					invalid := normalize(r)
					invalid["proxyAccount"] = bad
					if Validate(invalid, "bad-account") == nil {
						t.Fatal("malformed account-presence marker accepted")
					}
				}
			})
		}
	}
	for _, protocol := range []string{"vless", "vmess", "trojan", "shadowsocks"} {
		r := fullXray357Parse(t, fullXray357Base(protocol))
		r["proxyAccount"] = true
		if Validate(r, "wrong-account-protocol") == nil {
			t.Fatal("proxy account marker accepted on unrelated protocol")
		}
	}
}

func TestFullLevel357SS2022IgnoredMetadata(t *testing.T) {
	var baseKey string
	for _, level := range []int{0, 1, 8} {
		o, path := fullLevel357Fixture("shadowsocks", false, level)
		s := o["settings"].(map[string]any)
		s["method"], s["password"] = "2022-blake3-aes-128-gcm", "EREREREREREREREREREREQ=="
		s["email"] = "synthetic-private-value@example.test"
		r := fullXray357Parse(t, o)
		fullXray357Roundtrip(t, r, map[string]any{path: level, "settings.servers.0.email": s["email"]})
		warnings := fmt.Sprint(r["importWarnings"])
		if !strings.Contains(warnings, "2022") || !strings.Contains(warnings, "level") || !strings.Contains(warnings, "email") || strings.Contains(warnings, "synthetic-private-value") {
			t.Fatal("missing source-grounded SS2022 ignored-metadata warning")
		}
		key, err := presentationKey(r)
		if err != nil {
			t.Fatal(err)
		}
		if level == 0 {
			baseKey = key
		} else if key != baseKey {
			t.Fatal("SS2022 ignored level changed display identity")
		}
	}
}

func TestFullLevel357ExplicitProxyPassword(t *testing.T) {
	for _, protocol := range []string{"http", "socks"} {
		t.Run(protocol, func(t *testing.T) {
			o, path := fullLevel357Fixture(protocol, true, 8)
			account := get(o, "settings", "servers").([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
			account["user"], account["pass"] = "", "synthetic-private-value"
			r := fullXray357Parse(t, o)
			if r["proxyAccount"] != true || r["password"] != "synthetic-private-value" {
				t.Fatal("explicit empty-user password lost")
			}
			fullXray357Roundtrip(t, r, map[string]any{path: 8, "settings.servers.0.users.0.user": "", "settings.servers.0.users.0.pass": "synthetic-private-value"})
			if strings.Contains(fullXray357JSON(t, MaskRecord(r)), "synthetic-private-value") || strings.Contains(fmt.Sprint(r["importWarnings"]), "synthetic-private-value") {
				t.Fatal("explicit account password disclosed")
			}
			if ValidateClientFields(protocol, "raw", "none", map[string]any{"password": "synthetic-private-value"}) == nil {
				t.Fatal("private password carrier accepted without explicit account presence")
			}
			if ValidateClientFields("vless", "raw", "none", map[string]any{"proxyAccount": true, "password": "synthetic-private-value"}) == nil {
				t.Fatal("private password carrier accepted on unrelated protocol")
			}
			delete(r, "proxyAccount")
			if Validate(r, "missing-account-presence") == nil {
				t.Fatal("empty username password accepted without explicit account marker")
			}
			if _, err := Outbound(r); err == nil {
				t.Fatal("invalid no-account record reached core output")
			}
			if _, err := ParseLink(protocol+"://:synthetic-private-value@edge.example.test:443", "unchanged-uri", nil); err == nil {
				t.Fatal("JSON account presence broadened URI credential rules")
			}
		})
	}
}

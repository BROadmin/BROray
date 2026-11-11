package subscriptions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

// Legacy wrappers use the same endpoint/account contract as flat JSON, but
// malformed containers must not fall through to flat defaults or lose users.
// Keep this corpus separate from framing, native transports and URI aliases.
func parserLegacyFixture(proto string) (out map[string]any, listPath, accountPath string) {
	out = parserCoverageBase(proto)
	flat := out["settings"].(map[string]any)
	endpoint := map[string]any{"address": flat["address"], "port": flat["port"]}
	account := map[string]any{}
	for key, value := range flat {
		if key != "address" && key != "port" {
			account[key] = value
		}
	}
	switch proto {
	case "vless", "vmess":
		endpoint["users"] = []any{account}
		out["settings"] = map[string]any{"vnext": []any{endpoint}}
		return out, "settings.vnext", "settings.vnext.0.users.0"
	case "http", "socks":
		endpoint["users"] = []any{account}
		out["settings"] = map[string]any{"servers": []any{endpoint}}
		return out, "settings.servers", "settings.servers.0.users.0"
	case "trojan", "shadowsocks":
		out["settings"] = map[string]any{"servers": []any{flat}}
		return out, "settings.servers", "settings.servers.0"
	default:
		panic("unsupported legacy test fixture")
	}
}

func TestParserLegacyMalformedContainers(t *testing.T) {
	for _, proto := range []string{"vless", "vmess", "trojan", "shadowsocks", "http", "socks"} {
		for _, bad := range []struct {
			name  string
			value any
		}{{"null", nil}, {"object-instead-of-array", map[string]any{}}, {"empty", []any{}}, {"null-element", []any{nil}}, {"scalar-element", []any{"invalid"}}} {
			t.Run(proto+"/endpoint-list/"+bad.name, func(t *testing.T) {
				in, listPath, _ := parserLegacyFixture(proto)
				parserCoverageSet(in, listPath, bad.value)
				parserCoverageStage(t, in, false)
			})
		}
		t.Run(proto+"/settings/mixed-flat-and-legacy", func(t *testing.T) {
			in, _, _ := parserLegacyFixture(proto)
			parserCoverageSet(in, "settings.address", "other.example.test")
			parserCoverageStage(t, in, false)
		})
		if proto == "trojan" || proto == "shadowsocks" {
			continue
		}
		for _, bad := range []struct {
			name  string
			value any
		}{{"null", nil}, {"object-instead-of-array", map[string]any{}}, {"null-element", []any{nil}}, {"scalar-element", []any{"invalid"}}} {
			t.Run(proto+"/users/"+bad.name, func(t *testing.T) {
				in, listPath, _ := parserLegacyFixture(proto)
				parserCoverageSet(in, listPath+".0.users", bad.value)
				parserCoverageStage(t, in, false)
			})
		}
		if proto == "vless" || proto == "vmess" {
			t.Run(proto+"/users/empty-required-list", func(t *testing.T) {
				in, listPath, _ := parserLegacyFixture(proto)
				parserCoverageSet(in, listPath+".0.users", []any{})
				parserCoverageStage(t, in, false)
			})
		}
		// HTTP/SOCKS empty users are intentionally accepted; their existing
		// TestParserFieldCoverageProxy users/count-0 test asserts no-auth output.
		t.Run(proto+"/endpoint/mixed-flat-account-and-users", func(t *testing.T) {
			in, listPath, _ := parserLegacyFixture(proto)
			key := "id"
			if proto == "http" || proto == "socks" {
				key = "user"
			}
			parserCoverageSet(in, listPath+".0."+key, "synthetic-account")
			parserCoverageStage(t, in, false)
		})
	}
	for _, proto := range []string{"http", "socks"} {
		t.Run(proto+"/servers/reject-two-endpoints", func(t *testing.T) {
			in, listPath, _ := parserLegacyFixture(proto)
			first, _ := parserCoverageGet(in, listPath+".0")
			other, _, _ := parserLegacyFixture(proto)
			parserCoverageSet(other, listPath+".0.address", "other.example.test")
			second, _ := parserCoverageGet(other, listPath+".0")
			parserCoverageSet(in, listPath, []any{first, second})
			parserCoverageStage(t, in, false)
		})
	}
}

func parserLegacyStageMany(t *testing.T, input map[string]any, count int) *staged {
	t.Helper()
	body, err := json.Marshal(map[string]any{"outbounds": []any{input}})
	if err != nil {
		t.Fatal(err)
	}
	ex, extractErr := extractNodes(body, newMetadata())
	if extractErr != nil {
		t.Fatal("valid legacy document rejected", extractErr)
	}
	st, stageErr := stageNodes("legacy-shapes", ex, true, nil)
	if stageErr != nil || st == nil {
		t.Fatal("valid legacy expansion rejected", stageErr)
	}
	if st.Received != count || st.Parsed != count || st.Accepted != count || st.Rejected != 0 || len(st.Records) != count {
		t.Fatalf("wrong expansion counts: received=%d parsed=%d accepted=%d rejected=%d expected=%d", st.Received, st.Parsed, st.Accepted, st.Rejected, count)
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for i, record := range st.Records {
		id, _ := record["id"].(string)
		source := record["source"].(map[string]any)
		key, _ := source["importKey"].(string)
		if id == "" || ids[id] || len(key) != 64 || keys[key] {
			t.Fatal("expanded endpoints/accounts did not retain distinct identities")
		}
		ids[id], keys[key] = true, true
		parserCoverageAssert(t, map[string]any(record), "source.subscriptionId", "legacy-shapes", false)
		parserCoverageAssert(t, map[string]any(record), "source.nodeIndex", i+1, false)
	}
	return st
}

func TestParserLegacyEndpointExpansion(t *testing.T) {
	// BROray deliberately extracts independent records from legacy arrays.
	// This is not a claim that the original multi-target outbound is accepted
	// intact by the pinned core, which requires a single endpoint/account.
	for _, proto := range []string{"vless", "vmess"} {
		for _, size := range []struct {
			name             string
			endpoints, users int
		}{{"two-endpoints", 2, 1}, {"two-users", 1, 2}, {"two-endpoints-two-users", 2, 2}} {
			t.Run(proto+"/"+size.name, func(t *testing.T) {
				in := parserCoverageBase(proto)
				endpoints := []any{}
				type expected struct {
					address, id string
					port        int
				}
				wants := []expected{}
				for i := 0; i < size.endpoints; i++ {
					address, port := fmt.Sprintf("edge%d.example.test", i+1), 443+i
					users := []any{}
					for j := 0; j < size.users; j++ {
						id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i*2+j+1)
						account := map[string]any{"id": id, "encryption": "none"}
						if proto == "vmess" {
							account = map[string]any{"id": id, "security": "aes-128-gcm"}
						}
						users = append(users, account)
						wants = append(wants, expected{address: address, port: port, id: id})
					}
					endpoints = append(endpoints, map[string]any{"address": address, "port": port, "users": users})
				}
				in["settings"] = map[string]any{"vnext": endpoints}
				st := parserLegacyStageMany(t, in, len(wants))
				for i, want := range wants {
					out, err := servers.Outbound(st.Records[i])
					if err != nil {
						t.Fatal(err)
					}
					parserCoverageAssert(t, out, "settings.vnext.0.address", want.address, false)
					parserCoverageAssert(t, out, "settings.vnext.0.port", want.port, false)
					parserCoverageAssert(t, out, "settings.vnext.0.users.0.id", want.id, false)
					if proto == "vless" {
						parserCoverageAssert(t, out, "settings.vnext.0.users.0.encryption", "none", false)
					} else {
						parserCoverageAssert(t, out, "settings.vnext.0.users.0.security", "aes-128-gcm", false)
					}
				}
			})
		}
	}
	for _, proto := range []string{"trojan", "shadowsocks"} {
		t.Run(proto+"/two-endpoints", func(t *testing.T) {
			in := parserCoverageBase(proto)
			endpoints := []any{}
			for i := 0; i < 2; i++ {
				endpoint := map[string]any{"address": fmt.Sprintf("edge%d.example.test", i+1), "port": 443 + i, "password": fmt.Sprintf("synthetic-pass-%d", i+1)}
				if proto == "shadowsocks" {
					endpoint["method"] = "chacha20-ietf-poly1305"
				}
				endpoints = append(endpoints, endpoint)
			}
			in["settings"] = map[string]any{"servers": endpoints}
			st := parserLegacyStageMany(t, in, 2)
			for i, record := range st.Records {
				out, err := servers.Outbound(record)
				if err != nil {
					t.Fatal(err)
				}
				parserCoverageAssert(t, out, "settings.servers", []any{endpoints[i]}, false)
			}
		})
	}
}

func TestParserLegacyAccountMetadata(t *testing.T) {
	for _, proto := range []string{"vless", "vmess", "trojan", "shadowsocks", "http", "socks"} {
		t.Run(proto+"/level-email/accepted-with-warning", func(t *testing.T) {
			in, listPath, accountPath := parserLegacyFixture(proto)
			parserCoverageSet(in, accountPath+".level", 0)
			parserCoverageSet(in, accountPath+".email", "coverage@example.test")
			st, out := parserCoverageStage(t, in, true)
			warnings := strings.Join(st.Warnings, " ")
			settings, err := json.Marshal(out["settings"])
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"level", "email"} {
				if !strings.Contains(warnings, key) {
					t.Fatal("nested metadata omission lacks warning")
				}
				if bytes.Contains(settings, []byte(`"`+key+`":`)) {
					t.Fatal("nested metadata unexpectedly rendered")
				}
			}
			parserCoverageAssert(t, out, "protocol", proto, false)
			parserCoverageAssert(t, out, listPath+".0.address", "edge.example.test", false)
			parserCoverageAssert(t, out, listPath+".0.port", 443, false)
			switch proto {
			case "vless", "vmess":
				parserCoverageAssert(t, out, accountPath+".id", parserCoverageID, false)
			case "http", "socks":
				parserCoverageAssert(t, out, accountPath+".user", "user:@%/", false)
				parserCoverageAssert(t, out, accountPath+".pass", "synthetic-pass:@%/", false)
			default:
				parserCoverageAssert(t, out, accountPath+".password", "synthetic-pass:@%/", false)
			}
		})
		for _, bad := range []struct {
			field, name string
			value       any
		}{
			{"level", "null", nil}, {"level", "string", "0"},
			{"email", "null", nil}, {"email", "number", 7},
		} {
			t.Run(proto+"/"+bad.field+"/reject-"+bad.name, func(t *testing.T) {
				in, _, accountPath := parserLegacyFixture(proto)
				parserCoverageSet(in, accountPath+"."+bad.field, bad.value)
				parserCoverageStage(t, in, false)
			})
		}
	}
}

func TestParserLegacyAccountFieldTypes(t *testing.T) {
	for _, tc := range []struct{ proto, field string }{{"vless", "flow"}, {"shadowsocks", "method"}} {
		for _, form := range []string{"flat", "legacy"} {
			for _, bad := range []struct {
				name  string
				value any
			}{{"null", nil}, {"number", 7}} {
				t.Run(tc.proto+"/"+tc.field+"/"+form+"/reject-"+bad.name, func(t *testing.T) {
					in, path := parserCoverageBase(tc.proto), "settings"
					if form == "legacy" {
						in, _, path = parserLegacyFixture(tc.proto)
					}
					parserCoverageSet(in, path+"."+tc.field, bad.value)
					parserCoverageStage(t, in, false)
				})
			}
		}
	}
}

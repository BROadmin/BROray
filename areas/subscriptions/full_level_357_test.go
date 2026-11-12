package subscriptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/xrayschema"
)

func subscriptionLevel357Fixture(protocol string, legacy bool, level any) map[string]any {
	s := map[string]any{"address": "level.example.test", "port": 443, "level": level}
	switch protocol {
	case "vless":
		s["id"], s["encryption"] = "00000000-0000-4000-8000-000000000357", "none"
	case "vmess":
		s["id"], s["security"] = "00000000-0000-4000-8000-000000000357", "auto"
	case "http", "socks":
		s["user"], s["pass"] = "synthetic-user", "synthetic-password"
	case "trojan":
		s["password"] = "synthetic-password"
	case "shadowsocks":
		s["password"], s["method"] = "synthetic-password", "aes-128-gcm"
	}
	o := map[string]any{"protocol": protocol, "tag": "Level", "settings": s}
	if protocol == "trojan" {
		o["streamSettings"] = map[string]any{"security": "tls", "tlsSettings": map[string]any{"serverName": "level.example.test"}}
	}
	if legacy {
		switch protocol {
		case "vless", "vmess", "http", "socks":
			account := map[string]any{}
			for key, value := range s {
				if key != "address" && key != "port" {
					account[key] = value
				}
			}
			group := "servers"
			if protocol == "vless" || protocol == "vmess" {
				group = "vnext"
			}
			o["settings"] = map[string]any{group: []any{map[string]any{"address": s["address"], "port": s["port"], "users": []any{account}}}}
		default:
			o["settings"] = map[string]any{"servers": []any{s}}
		}
	}
	return o
}

func TestFullLevel357SubscriptionParity(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "http", "socks", "trojan", "shadowsocks"} {
		maximum := "4294967295"
		if protocol == "trojan" || protocol == "shadowsocks" {
			maximum = "255"
		}
		for _, legacy := range []bool{false, true} {
			for _, level := range []string{"0", "1", "8", maximum} {
				t.Run(fmt.Sprintf("%s/legacy-%t/%s", protocol, legacy, level), func(t *testing.T) {
					o := subscriptionLevel357Fixture(protocol, legacy, json.Number(level))
					st, out := parserCoverageStage(t, o, true)
					r := st.Records[0]
					if fmt.Sprint(r["accountLevel"]) != level || r["network"] == "unavailable" {
						t.Fatal("subscription did not retain a runnable account level")
					}
					body, _ := json.Marshal(o)
					manual, err := servers.ParseLink(string(body), "level-manual", nil)
					if err != nil {
						t.Fatal("manual entry disagrees with subscription")
					}
					manualOut, err := servers.Outbound(manual)
					a, _ := json.Marshal(out)
					b, _ := json.Marshal(manualOut)
					if err != nil || string(a) != string(b) {
						t.Fatal("manual/subscription level semantics differ")
					}
					if _, present := servers.MaskRecord(r)["accountLevel"]; present {
						t.Fatal("public subscription record exposes account metadata")
					}
				})
			}
		}
	}
}

func TestFullLevel357SubscriptionInvalid(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "http", "socks", "trojan", "shadowsocks"} {
		for _, legacy := range []bool{false, true} {
			for i, value := range []any{nil, true, "synthetic-private-value", -1, 0.5, json.Number("4294967296")} {
				t.Run(fmt.Sprintf("%s/legacy-%t/invalid-%d", protocol, legacy, i), func(t *testing.T) {
					st, _ := parserCoverageStage(t, subscriptionLevel357Fixture(protocol, legacy, value), false)
					if st.Accepted != 0 || strings.Contains(strings.Join(st.Warnings, " "), "synthetic-private-value") {
						t.Fatal("malformed account level retained or disclosed")
					}
				})
			}
		}
	}
}

func TestFullLevel357ActiveRefresh(t *testing.T) {
	for _, failApply := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback-%t", failApply), func(t *testing.T) {
			e := newEnv(t)
			o := subscriptionLevel357Fixture("vless", false, 8)
			delete(o["settings"].(map[string]any), "level")
			old, _ := json.Marshal(o)
			if _, err := e.sync(t, "replace", string(old)); err != nil {
				t.Fatal("cannot stage pre-C4 account")
			}
			id := liveIDs(t, e)[0]
			if err := e.s.writeActive(id); err != nil {
				t.Fatal(err)
			}
			if failApply {
				e.reapplyErr = errors.New("synthetic apply failure")
			}
			o["settings"].(map[string]any)["level"] = 8
			next, _ := json.Marshal(o)
			result, err := e.sync(t, "replace", string(next))
			if failApply {
				if err == nil {
					t.Fatal("active level update bypassed transactional apply")
				}
			} else if err != nil || result.Added != 0 || result.Removed != 0 || result.Total != 1 || result.Updated != 1 || result.Impact != "configuration-changed" || len(e.reapplied) != 1 {
				t.Fatal("equivalent level update did not use existing active replacement path")
			}
			ids := liveIDs(t, e)
			if len(ids) != 1 || ids[0] != id || servers.ActiveID(e.d) != id {
				t.Fatal("account metadata changed or deleted the active server identity")
			}
			r, readErr := servers.ReadRecord(e.d, id)
			if readErr != nil {
				t.Fatal("active record disappeared")
			}
			if failApply {
				if _, exists := r["accountLevel"]; exists {
					t.Fatal("failed replacement did not restore old account")
				}
			} else if fmt.Sprint(r["accountLevel"]) != "8" {
				t.Fatal("successful active replacement discarded account level")
			}
		})
	}
}

func TestFullLevel357C3SubscriptionCompatibility(t *testing.T) {
	for _, action := range []string{"retain-unmatched", "refresh"} {
		t.Run(action, func(t *testing.T) {
			e := newEnv(t)
			source := subscriptionLevel357Fixture("vless", false, 8)
			body, _ := json.Marshal(source)
			projection := stageLinks(t, true, string(body))[0]
			// Frozen C3 storage shape, with wholly synthetic source and account.
			id := "subscription-c3-level-only"
			old := servers.Record{"schemaVersion": 2, "id": id, "name": projection["name"], "source": projection["source"],
				"protocol": "vless", "address": projection["address"], "port": projection["port"], "network": "unavailable", "security": "none",
				"importProfile": map[string]any{"format": "xray", "sourceCommit": xrayschema.SourceCommit, "node": source}}
			key, _ := servers.CanonicalKey(old)
			old["source"].(map[string]any)["importKey"] = key
			stored, _ := json.Marshal(old)
			path := servers.RecordPath(e.d, id)
			if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, stored, 0600) != nil {
				t.Fatal("cannot write synthetic saved C3 fixture")
			}
			if action == "retain-unmatched" {
				source["settings"].(map[string]any)["address"] = "different.example.test"
				body, _ = json.Marshal(source)
				result, err := e.sync(t, "retain-unmatched", string(body))
				if err != nil || result.Retained != 1 || result.Total != 2 {
					t.Fatal("saved C3 wrapper blocked partial subscription refresh/remap")
				}
				retained, err := servers.ReadRecord(e.d, id)
				if err != nil || servers.Validate(retained, "retained-c3") != nil || retained["network"] != "unavailable" {
					t.Fatal("retained C3 source became invalid or auto-activated")
				}
				if _, err := servers.Outbound(retained); err == nil {
					t.Fatal("retained wrapper became executable")
				}
			} else {
				result, err := e.sync(t, "replace", string(body))
				if err != nil || result.Total != 1 {
					t.Fatal("C3 wrapper blocked explicit subscription refresh")
				}
				ids := liveIDs(t, e)
				if len(ids) != 1 {
					t.Fatal("refresh left stale wrapper duplicates")
				}
				fresh, err := servers.ReadRecord(e.d, ids[0])
				if err != nil || fresh["network"] == "unavailable" || fmt.Sprint(fresh["accountLevel"]) != "8" {
					t.Fatal("explicit refresh did not replace old wrapper with validated mapped account")
				}
				if _, err := servers.Outbound(fresh); err != nil {
					t.Fatal("fresh mapped account is not runnable")
				}
			}
			if servers.ActiveID(e.d) != "" || len(e.reapplied) != 0 {
				t.Fatal("compatibility migration automatically activated a server")
			}
		})
	}
}

func TestFullLevel357SubscriptionExplicitProxyAccount(t *testing.T) {
	for _, protocol := range []string{"http", "socks"} {
		o := subscriptionLevel357Fixture(protocol, true, 1)
		account := o["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
		account["user"], account["pass"] = "", ""
		st, out := parserCoverageStage(t, o, true)
		if st.Records[0]["proxyAccount"] != true {
			t.Fatal("subscription lost explicit empty legacy account")
		}
		parserCoverageAssert(t, out, "settings.servers.0.users.0.level", 1, false)
		parserCoverageAssert(t, out, "settings.servers.0.users.0.user", "", false)
		parserCoverageAssert(t, out, "settings.servers.0.users.0.pass", "", false)
	}
}

func TestFullLevel357SubscriptionExplicitProxyPassword(t *testing.T) {
	for _, protocol := range []string{"http", "socks"} {
		t.Run(protocol, func(t *testing.T) {
			o := subscriptionLevel357Fixture(protocol, true, 8)
			account := o["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
			account["user"], account["pass"] = "", "synthetic-private-value"
			st, out := parserCoverageStage(t, o, true)
			if st.Records[0]["proxyAccount"] != true || st.Records[0]["password"] != "synthetic-private-value" {
				t.Fatal("subscription lost explicit empty-user password")
			}
			parserCoverageAssert(t, out, "settings.servers.0.users.0.level", 8, false)
			parserCoverageAssert(t, out, "settings.servers.0.users.0.user", "", false)
			parserCoverageAssert(t, out, "settings.servers.0.users.0.pass", "synthetic-private-value", false)
			masked, _ := json.Marshal(servers.MaskRecord(st.Records[0]))
			if strings.Contains(string(masked), "synthetic-private-value") || strings.Contains(strings.Join(st.Warnings, " "), "synthetic-private-value") {
				t.Fatal("subscription disclosed explicit account password")
			}
		})
	}
}

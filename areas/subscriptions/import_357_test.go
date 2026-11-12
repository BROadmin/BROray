package subscriptions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestImport357SelfExportAliases(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			e := newEnv(t)
			var reads atomic.Int32
			p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/entry" {
					http.Redirect(w, r, "/data", http.StatusFound)
					return
				}
				if r.Header.Get("Accept") == "text/html" {
					if conflict {
						fmt.Fprint(w, `<html><script>window.__SUB_PAGE_DATA__={"subJsonUrl":"/entry","subClashUrl":"/data"};</script></html>`)
					} else {
						fmt.Fprint(w, `<html><script>window.__SUB_PAGE_DATA__={"subJsonUrl":"/data"};</script></html>`)
					}
					return
				}
				reads.Add(1)
				if conflict {
					fmt.Fprint(w, "proxies:\n  - {name: A, type: trojan, server: node.example, port: 443, password: invented}\n")
				} else {
					fmt.Fprint(w, auditJSON356("node.example", "aa"))
				}
			}))
			defer p.Close()
			path := "/data#fragment"
			if conflict {
				path = "/entry"
			}
			id := e.createWithExports357(t, p.URL+path, "")
			err := e.refreshSync(t, id)
			if conflict {
				if err == nil || len(liveIDs(t, e)) != 0 {
					t.Fatal("conflicting self export declarations accepted")
				}
			} else if err != nil || reads.Load() != 1 {
				t.Fatalf("normalized self response reads=%d err=%v", reads.Load(), err)
			}
		})
	}
}

func TestImport357RotationAcrossDocuments(t *testing.T) {
	for _, two := range []bool{false, true} {
		t.Run(fmt.Sprint(two), func(t *testing.T) {
			a, b := rotationLink("one.example", "aa"), rotationLink("two.example", "bb")
			extra := []string{b}
			if two {
				extra = append(extra, rotationLink("three.example", "cc"))
			}
			j, _ := json.Marshal(extra)
			st, err := stageDocuments(subX, []sourceDocument{{body: []byte(a)}, {body: j, format: "json"}}, newMetadata(), true, nil)
			want := 1
			if two {
				want = 3
			}
			if err != nil || st.Accepted != want || st.Rejected != 0 {
				t.Fatalf("rotation accepted=%d want=%d rejected=%d err=%v", st.Accepted, want, st.Rejected, err)
			}
			if !two {
				b, _ := json.Marshal(sourceOf(st.Records[0])["formats"])
				if string(b) != `["uri","json"]` {
					t.Fatal("missing provenance", string(b))
				}
			}
		})
	}
}

func TestImport357SelfExportTypeIsChecked(t *testing.T) {
	e := newEnv(t)
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "text/html" {
			fmt.Fprint(w, `<html><script>window.__SUB_PAGE_DATA__={"subClashUrl":"/same"};</script></html>`)
			return
		}
		fmt.Fprint(w, auditJSON356("node.example", "aa"))
	}))
	defer p.Close()
	id := e.createWithExports357(t, p.URL+"/same", "")
	if err := e.refreshSync(t, id); err == nil {
		t.Fatal("self-export with wrong declared format accepted")
	}
	if len(liveIDs(t, e)) != 0 {
		t.Fatal("invalid self-export mutated catalog")
	}
}

func TestImport357AllAWGExtensions(t *testing.T) {
	fields := map[string]any{"header-protection-key": strings.Repeat("ab", 32), "content-padding-addition": "10-20", "rekey-after-time": "120-180", "rekey-timeout": 5, "reject-after-time": 180, "keepalive-timeout": 25, "max-handshake-attempts": 15, "random-trailers": true, "disable-cookies": false, "jc": 4}
	mapping := map[string]string{"header-protection-key": "HeaderProtectionKey", "content-padding-addition": "ContentPaddingAddition", "rekey-after-time": "RekeyAfterTime", "rekey-timeout": "RekeyTimeout", "reject-after-time": "RejectAfterTime", "keepalive-timeout": "KeepaliveTimeout", "max-handshake-attempts": "MaxHandshakeAttempts", "random-trailers": "RandomTrailers", "disable-cookies": "DisableCookies"}
	p := map[string]any{"name": "synthetic", "type": "wireguard", "server": "node.example", "port": 443, "ip": "10.0.0.2", "private-key": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "public-key": "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", "amnezia-wg-option": fields}
	st := clashStage(t, clashBody(p))
	if st.Accepted != 1 {
		t.Fatal(st)
	}
	profile := st.Records[0]["amneziawg"].(map[string]any)
	iface := profile["interface"].(map[string]any)
	for k, name := range mapping {
		if fmt.Sprint(iface[name]) != "["+fmt.Sprint(fields[k])+"]" {
			t.Errorf("extension %s missing or altered", name)
		}
	}
	fields["header-protection-key"] = "injected\nPrivateKey = forbidden"
	bad := clashStage(t, clashBody(p))
	if bad.Accepted != 0 {
		t.Fatal("INI newline injection accepted")
	}
}

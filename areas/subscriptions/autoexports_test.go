package subscriptions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/areas/servers"
)

const autoJSON355 = `{"outbounds":[{"protocol":"trojan","settings":{"address":"a.example","port":443,"password":"pw"},"streamSettings":{"security":"tls"}},{"protocol":"trojan","settings":{"address":"json.example","port":443,"password":"invented-json"},"streamSettings":{"security":"tls"}}]}`
const autoClash355 = "proxies:\n - {name: invented-clash, type: ss, server: clash.example, port: 8388, cipher: aes-128-gcm, password: invented-clash}\n"
const autoPage355 = `<html><script>window.X_UI_BASE_PATH="/sub/";window.__SUB_PAGE_DATA__={"subJsonUrl":"/download/video/invented","subClashUrl":"/download/lesson/invented"};</script></html>`

type autoFixture355 struct {
	mu                                           sync.Mutex
	primary, page, jsonBody, clashBody, redirect string
	probeStatus, jsonStatus                      int
	htmlPrimary                                  bool
	requests                                     map[string]int
	*httptest.Server
}

func newAutoFixture355(t *testing.T) *autoFixture355 {
	p := &autoFixture355{primary: linkA, page: autoPage355, jsonBody: autoJSON355, clashBody: autoClash355, requests: map[string]int{}}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.requests[r.URL.Path+"|"+r.Header.Get("Accept")]++
		w.Header().Set("subscription-userinfo", "upload=500; download=700; total=10000")
		switch r.URL.Path {
		case "/sub/invented":
			if r.Header.Get("Accept") == "text/html" {
				if p.probeStatus != 0 {
					w.WriteHeader(p.probeStatus)
				}
				fmt.Fprint(w, p.page)
			} else {
				w.Header().Set("subscription-userinfo", "upload=5; download=7; total=100")
				if p.htmlPrimary {
					fmt.Fprint(w, p.page)
				} else {
					fmt.Fprint(w, p.primary)
				}
			}
		case "/download/video/invented":
			if p.redirect != "" {
				http.Redirect(w, r, p.redirect, http.StatusFound)
				return
			}
			if p.jsonStatus != 0 {
				w.WriteHeader(p.jsonStatus)
			}
			fmt.Fprint(w, p.jsonBody)
		case "/download/lesson/invented":
			fmt.Fprint(w, p.clashBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *autoFixture355) change(fn func()) { p.mu.Lock(); defer p.mu.Unlock(); fn() }

func snapshotAutoCatalog355(t *testing.T, e *env) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{servers.ServersDir(e.d), e.s.disabledRoot(), e.d.Path("config", "servers-trash")} {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			b, err := os.ReadFile(path)
			if err == nil {
				out[path] = string(b)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// A missing HTML representation probe would leave both alternate-only nodes
// out of the real catalog. All provider credentials and paths here are invented.
func TestAutoExports355ServiceMerge(t *testing.T) {
	e := newEnv(t)
	primary := base64.StdEncoding.EncodeToString([]byte(linkA))
	page := `<html><script>window.X_UI_BASE_PATH="/sub/";window.__SUB_PAGE_DATA__={"subJsonUrl":"/download/video/invented","subClashUrl":"/download/lesson/invented"};</script></html>`
	jp := `{"outbounds":[{"protocol":"trojan","settings":{"address":"a.example","port":443,"password":"pw"},"streamSettings":{"security":"tls"}},{"protocol":"trojan","settings":{"address":"json.example","port":443,"password":"invented-json"},"streamSettings":{"security":"tls"}}]}`
	cp := "proxies:\n - {name: invented-clash, type: ss, server: clash.example, port: 8388, cipher: aes-128-gcm, password: invented-clash}\n"
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sub/invented":
			w.Header().Set("subscription-userinfo", "upload=5; download=7; total=100")
			if r.Header.Get("Accept") == "text/html" {
				fmt.Fprint(w, page)
			} else {
				fmt.Fprint(w, primary)
			}
		case "/download/video/invented":
			fmt.Fprint(w, jp)
		case "/download/lesson/invented":
			fmt.Fprint(w, cp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer p.Close()
	id := e.createWithExports357(t, p.URL+"/sub/invented", "")
	if err := e.refreshSync(t, id); err != nil {
		t.Fatalf("%v result=%v", err, e.record(t, id)["lastUpdateResult"])
	}
	ids := liveIDs(t, e)
	if len(ids) != 3 {
		t.Fatalf("want 3 merged nodes, got %d", len(ids))
	}
	addresses := map[string]bool{}
	for _, sid := range ids {
		r, err := servers.ReadRecord(e.d, sid)
		if err != nil {
			t.Fatal(err)
		}
		addresses[r["address"].(string)] = true
		if sourceOf(r)["subscriptionId"] != id {
			t.Fatal("alternate acquired another owner")
		}
		if r["address"] == "a.example" {
			b, _ := json.Marshal(sourceOf(r)["formats"])
			if string(b) != `["uri","json"]` {
				t.Fatalf("duplicate provenance: %s", b)
			}
		}
	}
	if !addresses["json.example"] || !addresses["clash.example"] {
		t.Fatal("alternate nodes missing")
	}
	r := e.record(t, id)
	counts := r["lastUpdateResult"].(map[string]any)
	if !numIs(counts["accepted"], 3) || !numIs(counts["received"], 4) || !numIs(counts["rejected"], 0) {
		t.Fatalf("counts: %v", counts)
	}
	if r["lastUpdateStatus"] != "success" || len(e.s.recordIDs()) != 1 {
		t.Fatal("merge status/owner")
	}
	if !strings.Contains(fmt.Sprint(counts["warnings"]), "JSON") {
		t.Fatal("format summary missing")
	}
}

// These cases catch silent disappearance/partial acceptance of an advertised
// source, which would otherwise let replace sync withdraw export-only nodes.
func TestAutoExports355AtomicFailures(t *testing.T) {
	for _, failure := range []string{"fetch", "parse", "partial", "probe", "disappeared", "one-disappeared", "cross-origin", "private", "redirect", "malformed", "oversize"} {
		t.Run(failure, func(t *testing.T) {
			e := newEnv(t)
			p := newAutoFixture355(t)
			id := e.createWithExports357(t, p.URL+"/sub/invented", "")
			if err := e.refreshSync(t, id); err != nil {
				t.Fatal(err)
			}
			var active string
			for _, f := range e.s.liveServers() {
				if f.rec["address"] == "a.example" {
					active = f.id
				}
			}
			if err := e.s.writeActive(active); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(e.s.disabledDir("unrelated"), "invented.json"), "invented-disabled")
			writeFile(t, e.d.Path("config", "servers-trash", "invented.json"), "invented-trash")
			before := snapshotAutoCatalog355(t, e)
			unapproved := newProvider(t, autoJSON355)
			p.change(func() {
				p.primary = strings.Replace(linkA, "pw@", "new@", 1)
				switch failure {
				case "fetch":
					p.jsonStatus = 503
				case "parse":
					p.jsonBody = "invented-secret-unrecognized"
				case "partial":
					p.jsonBody = strings.Replace(autoJSON355, `"outbounds":[`, `"outbounds":[{"protocol":"tuic","invented-secret":"do-not-log"},`, 1)
				case "probe":
					p.probeStatus = 503
				case "disappeared":
					p.page = "<html>no exports</html>"
				case "one-disappeared":
					p.page = `<html><a href="/download/video/invented">JSON</a></html>`
				case "cross-origin":
					p.page = `<html><a href="` + unapproved.URL + `/invented-secret">JSON</a></html>`
				case "private":
					p.page = `<html><a href="http://192.168.1.1/invented-secret">JSON</a></html>`
				case "redirect":
					p.redirect = unapproved.URL + "/invented-secret"
				case "malformed":
					p.page = `<html><script>window.__SUB_PAGE_DATA__={"subJsonUrl":42};</script></html>`
				case "oversize":
					p.page = "<html>" + strings.Repeat("a", maxDiscoveryBytes) + "</html>"
				}
			})
			if err := e.refreshSync(t, id); err == nil {
				t.Fatal("incomplete source accepted")
			}
			if !reflect.DeepEqual(before, snapshotAutoCatalog355(t, e)) || servers.ActiveID(e.d) != active || len(e.reapplied) != 0 {
				t.Fatal("failed preparation changed catalog/active state")
			}
			r := e.record(t, id)
			if len(rememberedFormats(r)) != 2 {
				t.Fatal("discovery guard forgotten")
			}
			public, _ := json.Marshal(e.s.public(r, false, e.s.liveServers()))
			if strings.Contains(string(public), "invented-secret") || strings.Contains(string(public), "/download/") || strings.Contains(string(public), "do-not-log") {
				t.Fatal("private source data leaked")
			}
			unapproved.mu.Lock()
			count := len(unapproved.seen)
			unapproved.mu.Unlock()
			if count != 0 {
				t.Fatal("unapproved origin received a request")
			}
		})
	}
}

func TestAutoExports355RefreshAndWithdrawal(t *testing.T) {
	e := newEnv(t)
	p := newAutoFixture355(t)
	id := e.createWithExports357(t, p.URL+"/sub/invented", "")
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	before := snapshotAutoCatalog355(t, e)
	ids := liveIDs(t, e)
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, liveIDs(t, e)) || !reflect.DeepEqual(before, snapshotAutoCatalog355(t, e)) {
		t.Fatal("repeat refresh rewrote stable records")
	}
	meta := e.record(t, id)["providerMetadata"].(map[string]any)
	usage := meta["usage"].(map[string]any)
	if !numIs(usage["upload"], 5) || !numIs(usage["download"], 7) || !numIs(usage["total"], 100) {
		t.Fatalf("alternate metadata displaced primary: %v", usage)
	}
	p.change(func() {
		p.jsonBody = strings.Replace(autoJSON355, `,{"protocol":"trojan","settings":{"address":"json.example","port":443,"password":"invented-json"},"streamSettings":{"security":"tls"}}`, "", 1)
	})
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	if len(liveIDs(t, e)) != 2 {
		t.Fatal("genuine withdrawal retained old alternate-only node")
	}
	for _, f := range e.s.liveServers() {
		if f.rec["address"] == "json.example" {
			t.Fatal("withdrawn node remains")
		}
	}
	if e.record(t, id)["url"] != p.URL+"/sub/invented" {
		t.Fatal("primary URL changed")
	}
}

func TestAutoExports355HTMLPrimaryAndDirect(t *testing.T) {
	for _, mode := range []string{"html-primary", "unsupported-probe", "direct-json", "direct-clash", "json-link-array", "disabled", "xdrive"} {
		t.Run(mode, func(t *testing.T) {
			e := newEnv(t)
			p := newAutoFixture355(t)
			extra := ""
			want := 3
			p.change(func() {
				switch mode {
				case "html-primary":
					p.htmlPrimary = true
				case "unsupported-probe":
					p.probeStatus = 406
					want = 1
				case "direct-json":
					p.primary = autoJSON355
					p.probeStatus = 406
					want = 2
				case "direct-clash":
					p.primary = autoClash355
					p.probeStatus = 406
					want = 1
				case "json-link-array":
					p.jsonBody = `["` + linkA + `","trojan://invented-json@json.example:443#JSON"]`
				case "disabled":
					extra = `,"enabled":false`
				case "xdrive":
					x := `{"protocol":"vless","tag":"invented-xdrive","settings":{"address":"cloud.example.invalid","port":443,"id":"11111111-1111-4111-8111-111111111111","encryption":"mlkem768x25519plus.native.0rtt.` + strings.Repeat("A", 43) + `"},"streamSettings":{"network":"xdrive","security":"none","xdriveSettings":{"service":"google-drive","remoteFolder":"invented-folder","secrets":["invented-client","invented-secret","invented-refresh"]}}}`
					p.jsonBody = `{"outbounds":[` + x + `]}`
				}
			})
			id := e.createWithExports357(t, p.URL+"/sub/invented", extra)
			if err := e.refreshSync(t, id); err != nil {
				t.Fatal(err)
			}
			files := e.s.liveServers()
			if mode == "disabled" {
				files = e.s.disabledServers(id)
				if len(liveIDs(t, e)) != 0 {
					t.Fatal("disabled import went live")
				}
			}
			if len(files) != want {
				t.Fatalf("want %d records got %d", want, len(files))
			}
			if mode == "xdrive" {
				found := false
				for _, f := range files {
					if f.rec["network"] == "xdrive" {
						found = true
						if f.rec["xdrive"].(map[string]any)["remoteFolder"] != "invented-folder" {
							t.Fatal("cloud settings lost")
						}
					}
				}
				if !found {
					t.Fatal("native alternate missing")
				}
			}
			if mode == "html-primary" {
				p.mu.Lock()
				count := p.requests["/sub/invented|text/html"]
				p.mu.Unlock()
				if count != 0 {
					t.Fatal("HTML primary was probed again")
				}
			}
		})
	}
}

func TestAutoExports355Discovery(t *testing.T) {
	base := "https://provider.example/sub/invented"
	for _, tc := range []struct {
		name, body string
		count      int
		path       string
		bad        bool
	}{
		{"assignment", autoPage355, 2, "/download/video/invented", false},
		{"anchors", `<a href="/export?a=1&amp;b=2"><b>JSON</b></a><a type="application/yaml" href="/clash">download</a>`, 2, "/export?a=1&b=2", false},
		{"escaped", `<script>window.__SUB_PAGE_DATA__={"subJsonUrl":"\/export?x=1\u0026y=2"};</script>`, 1, "/export?x=1&y=2", false},
		{"duplicates", `<a href="/json">JSON</a><a href="https://PROVIDER.example:443/json#x">JSON</a>`, 1, "/json", false},
		{"script-inert", `<script>const s='<a href="https://evil.example/export">JSON</a>';</script><style>.x{content:'<a>JSON</a>'}</style>`, 0, "", false},
		{"ambiguous", `<a href="/a">JSON</a><a href="/b">JSON</a>`, 0, "", true},
		{"mixed-label", `<a href="/a">JSON Clash</a>`, 0, "", true},
		{"cross-origin", `<a href="https://evil.example/invented-secret">JSON</a>`, 0, "", true},
		{"downgrade", `<a href="http://provider.example/invented-secret">JSON</a>`, 0, "", true},
		{"private", `<a href="http://192.168.1.1/invented-secret">JSON</a>`, 0, "", true},
		{"javascript", `<a href="javascript:alert('invented-secret')">JSON</a>`, 0, "", true},
		{"credentials", `<a href="https://u:invented-secret@provider.example/a">JSON</a>`, 0, "", true},
		{"missing-href", `<a>JSON</a>`, 0, "", true},
		{"duplicate-field", `<script>window.__SUB_PAGE_DATA__={"subJsonUrl":"/a","subJsonUrl":"/b"};</script>`, 0, "", true},
		{"wrong-type", `<script>window.__SUB_PAGE_DATA__={"subJsonUrl":42};</script>`, 0, "", true},
		{"expression", `<script>window.__SUB_PAGE_DATA__=JSON.parse("invented-secret");</script>`, 0, "", true},
		{"multiple-assignment", `<script>window.__SUB_PAGE_DATA__={"subJsonUrl":"/a"};window.__SUB_PAGE_DATA__={"subJsonUrl":"/b"};</script>`, 0, "", true},
		{"missing-close", `<a href="/a">JSON`, 0, "", true},
		{"oversize", `<html>` + strings.Repeat("a", maxDiscoveryBytes) + `</html>`, 0, "", true},
		{"tokens", strings.Repeat("<i></i>", maxHTMLTokens), 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			links, err := discoverExports(context.Background(), []byte(tc.body), base)
			if (err != nil) != tc.bad {
				t.Fatalf("error=%v expected invalid=%v", err, tc.bad)
			}
			if err != nil {
				if strings.Contains(err.Error(), "invented-secret") {
					t.Fatal("private URL leaked")
				}
				return
			}
			if len(links) != tc.count {
				t.Fatalf("want %d URLs got %d", tc.count, len(links))
			}
			if len(links) > 0 && links[0].url != "https://provider.example"+tc.path {
				t.Fatalf("URL resolution mismatch: %s", links[0].url)
			}
		})
	}
}

func TestAutoExports355Cancellation(t *testing.T) {
	e := newEnv(t)
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		if r.Header.Get("Accept") == "text/html" {
			fmt.Fprint(w, `<html><a href="/slow">JSON</a></html>`)
		} else {
			fmt.Fprint(w, linkA)
		}
	}))
	defer p.Close()
	id := e.createWithExports357(t, p.URL+"/sub/invented", "")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	prep := &prepared{}
	if err := e.s.runFetch(ctx, e.record(t, id), prep, errPrepTimeout); err != context.DeadlineExceeded {
		t.Fatalf("cancellation lost: %v", err)
	}
	if len(liveIDs(t, e)) != 0 {
		t.Fatal("cancelled discovery applied catalog")
	}
}

func TestAutoExports355RedirectIdentifiers(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	var headers []http.Header
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Header.Clone())
		mu.Unlock()
		if r.URL.Path == "/export" {
			fmt.Fprint(w, autoJSON355)
			return
		}
		if r.Header.Get("Accept") == "text/html" {
			fmt.Fprint(w, `<html><a href="/export">JSON</a></html>`)
		} else {
			fmt.Fprint(w, linkA)
		}
	}))
	defer final.Close()
	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, final.URL+"/final", 302) }))
	defer start.Close()
	id := e.createWithExports357(t, start.URL+"/sub/invented", `,"sendDeviceInfo":true`)
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(headers) != 3 {
		t.Fatalf("want final, probe, export requests; got %d", len(headers))
	}
	for _, h := range headers {
		if h.Get("X-Hwid") != "" || h.Get("X-Device-Os") != "" {
			t.Fatal("identifier reintroduced after cross-origin redirect")
		}
	}
}

func TestAutoExports355KnownFailureLabel(t *testing.T) {
	for _, format := range []string{"json", "clash"} {
		t.Run(format, func(t *testing.T) {
			e := newEnv(t)
			p := newAutoFixture355(t)
			p.change(func() {
				if format == "json" {
					p.jsonStatus = 503
				} else {
					p.clashBody = "unrecognized-invented-secret"
				}
			})
			id := e.createWithExports357(t, p.URL+"/sub/invented", "")
			err := e.refreshSync(t, id)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), format) {
				t.Fatal("missing safe failed format label", err)
			}
			if len(rememberedFormats(e.record(t, id))) != 2 {
				t.Fatal("first discovered formats lost after failure")
			}
			p.change(func() { p.page = "<html>no exports</html>"; p.jsonStatus = 0; p.clashBody = autoClash355 })
			if err := e.refreshSync(t, id); err == nil {
				t.Fatal("discovery failure following failed first export was accepted")
			}
		})
	}
}

func TestAutoExports355StrictIdentity(t *testing.T) {
	base := linkB + "&sid=01"
	variants := []string{base, strings.Replace(base, "0f6c1b2a", "1f6c1b2a", 1), strings.Replace(base, "pbk=PBK", "pbk=OTHER", 1), strings.Replace(base, "sid=01", "sid=02", 1), strings.Replace(base, "sni=a.com", "sni=b.com", 1), strings.Replace(base, "type=tcp", "type=tcp&headerType=http&host=front.example&path=%2F", 1)}
	// Put query additions before the URI fragment, independent of parser rules.
	for i, v := range variants {
		variants[i] = strings.Replace(v, "#NL&sid=", "&sid=", 1)
	}
	enc, _ := json.Marshal(variants)
	st, err := stageDocuments(subX, []sourceDocument{{body: []byte(variants[0])}, {body: enc, format: "json"}}, newMetadata(), true, nil)
	if err != nil || st.Accepted != len(variants) || st.Rejected != 0 {
		t.Fatalf("connection variants collapsed: accepted=%d rejected=%d err=%v", st.Accepted, st.Rejected, err)
	}
	awgJSON, _ := json.Marshal([]string{subscriptionAWG("123"), subscriptionAWG("456")})
	st, err = stageDocuments(subX, []sourceDocument{{body: []byte(subscriptionAWG("123"))}, {body: awgJSON, format: "json"}}, newMetadata(), true, nil)
	if err != nil || st.Accepted != 2 || st.Rejected != 0 {
		t.Fatal("AWG extensions collapsed", err)
	}
}

func TestAutoExports355Bounds(t *testing.T) {
	t.Run("combined-documents", func(t *testing.T) {
		e := newEnv(t)
		p := newAutoFixture355(t)
		p.change(func() { p.primary = strings.Repeat("a", maxBodyBytes); p.jsonBody = strings.Repeat("b", maxBodyBytes) })
		id := e.createWithExports357(t, p.URL+"/sub/invented", "")
		prep := &prepared{}
		if err := e.s.runFetch(context.Background(), e.record(t, id), prep, errPrepTimeout); err != nil {
			t.Fatal(err)
		}
		if prep.OK || prep.Code != "CONTENT_TOO_LARGE" {
			t.Fatal("combined byte limit missing", prep.Code)
		}
	})
	t.Run("combined-nodes", func(t *testing.T) {
		st, err := stageDocuments(subX, []sourceDocument{{body: []byte(strings.Repeat(linkA+"\n", maxNodes))}, {body: []byte(autoJSON355), format: "json"}}, newMetadata(), true, nil)
		if err == nil || err.Code != "CONTENT_TOO_LARGE" || st.Received != maxNodes {
			t.Fatal("combined node budget missing", err)
		}
	})
	t.Run("conflicting-schema", func(t *testing.T) {
		_, err := discoverExports(context.Background(), []byte(`<a href="/export">JSON</a><a href="/export">Clash</a>`), "https://provider.example/sub/invented")
		if err == nil {
			t.Fatal("conflicting advertised schemas silently hidden")
		}
	})
	t.Run("json-link-array-invalid", func(t *testing.T) {
		for _, body := range []string{`["` + linkA + `",42]`, `["` + linkA + `",{}]`, `["` + linkA + `","invented-invalid"]`, `["` + linkA + `\n` + linkA + `"]`} {
			if _, err := extractNodes([]byte(body), newMetadata()); err == nil {
				t.Fatal("mixed or malformed JSON link array accepted")
			}
		}
	})
}

func TestAutoExports355DiscoveryStateValidation(t *testing.T) {
	e := newEnv(t)
	p := newAutoFixture355(t)
	id := e.createWithExports357(t, p.URL+"/sub/invented", "")
	if err := e.refreshSync(t, id); err != nil {
		t.Fatal(err)
	}
	rec := e.record(t, id)
	for _, v := range []any{"https://invented-secret.example/export", []any{"json", "invented-secret"}, []any{"json", "json"}, []any{"json", "clash", "json"}} {
		r := clone(rec)
		r["alternateExports"] = v
		if validRecord(r) {
			t.Fatal("invalid discovery state may persist or leak through backup")
		}
	}
	back, _ := json.Marshal(e.s.backupExport())
	if !strings.Contains(string(back), `"alternateExports":["json","clash"]`) || strings.Contains(string(back), "/download/") {
		t.Fatal("backup discovery state missing or contains private export URLs")
	}
}

// Unicode case folding may change UTF-8 byte lengths. Discovery must continue
// indexing the original document safely, even around raw script/style text.
func TestAutoExports355UnicodeHTML(t *testing.T) {
	unicode := strings.Repeat("\u0130\u212a", 64)
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"before-script", unicode + autoPage355, 2},
		{"inside-script", `<html><script>const label="` + unicode + `";window.__SUB_PAGE_DATA__={"subJsonUrl":"/json"};</script></html>`, 1},
		{"inside-style", `<html><style>.x{content:"` + unicode + `"}</style><a href="/json">JSON</a></html>`, 1},
		{"before-anchor", unicode + `<a href="/json">JSON</a>`, 1},
		{"inside-anchor", `<a href="/json"><b>` + unicode + `</b> JSON</a>`, 1},
		{"anchor-before-script", `<a href="/download/video/invented">` + unicode + ` JSON</a>` + autoPage355, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Unicode HTML discovery panicked: %T", r)
				}
			}()
			links, err := discoverExports(context.Background(), []byte(tc.body), "https://provider.example/sub/invented")
			if err != nil || len(links) != tc.count {
				t.Fatalf("Unicode HTML changed discovery: count=%d err=%v", len(links), err)
			}
		})
	}
}

package keenetic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// rciFake answers the two RCI reads and the logout made by the native WebUI.
// Shapes follow KeeneticOS 5.2 WebUI (whoami.user, show.sc.user[name].tag).
type rciFake struct {
	cookie   string
	user     string // whoami.user
	tags     any    // value of "tag" for that user; nil omits the field
	users    map[string]any
	whoami   any // overrides the whoami object when non-nil
	status   int
	oversize bool
	rciCalls int
	deletes  int
	noCookie bool
	order    []string
}

func newRCIFake(cookie, user string, tags any) rciFake {
	return rciFake{cookie: cookie, user: user, tags: tags, status: 200}
}

func (f *rciFake) serve(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.URL.Path == "/rci/":
		f.rciCalls++
		if c, e := r.Cookie(f.cookie); e != nil || c.Value == "" {
			f.noCookie = true
		}
		var q map[string]any
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" ||
			json.NewDecoder(r.Body).Decode(&q) != nil || len(q) != 1 {
			w.WriteHeader(400)
			return true
		}
		if f.status != 200 {
			w.WriteHeader(f.status)
			return true
		}
		if f.oversize {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"whoami":{"user":"` + strings.Repeat("a", rciMaxResponse) + `"}}`))
			return true
		}
		var out any
		switch {
		case q["whoami"] != nil:
			f.order = append(f.order, "whoami")
			who := f.whoami
			if who == nil {
				who = map[string]any{"user": f.user, "id": "synthetic-session", "password": "normal"}
			}
			out = map[string]any{"whoami": who}
		case q["show"] != nil:
			f.order = append(f.order, "show.sc.user")
			users := map[string]any{"other-admin": map[string]any{"tag": []string{"cli", "http"}}}
			for k, v := range f.users {
				users[k] = v
			}
			if f.users == nil {
				entry := map[string]any{}
				if f.tags != nil {
					entry["tag"] = f.tags
				}
				users[f.user] = entry
			}
			out = map[string]any{"show": map[string]any{"sc": map[string]any{"user": users}}}
		default:
			w.WriteHeader(400)
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return true
	case r.URL.Path == "/auth" && r.Method == http.MethodDelete:
		f.deletes++
		f.order = append(f.order, "logout")
		if c, e := r.Cookie(f.cookie); e != nil || c.Value == "" {
			f.noCookie = true
		}
		w.WriteHeader(200)
		return true
	}
	return false
}

// The decision table is the native WebUI's: administrator = cli|http, and
// adminRightsRequired pages (incl. OPKG) are hidden for readonly and manager.
func TestRightsMirrorKeeneticWebUIOPKGRule(t *testing.T) {
	for _, tc := range []struct {
		tags []string
		full bool
	}{
		{[]string{"cli", "http", "opt", "cifs", "printers", "webdav", "http-proxy"}, true}, // KN-2710 admin, read-only on router 2026-11-01
		{[]string{"http"}, true},
		{[]string{"cli"}, true},
		{[]string{"http", "readonly"}, false},
		{[]string{"cli", "http", "manager"}, false},
		{[]string{"manager"}, false},
		{[]string{"readonly"}, false},
		{[]string{"opt", "cifs", "webdav", "http-proxy"}, false}, // file/proxy access only
		{[]string{"ipsec-l2tp", "vpn", "sstp"}, false},           // VPN-only account
		{[]string{}, false},
		{[]string{"httpx", "clix", "HTTP"}, false}, // exact element match only
	} {
		if got := (Rights{User: "u", Tags: tc.tags}).FullAdministrator(); got != tc.full {
			t.Fatalf("%v: full=%v want %v", tc.tags, got, tc.full)
		}
	}
}

func TestParseTagsFailClosed(t *testing.T) {
	ok := map[string][]string{``: {}, `["cli","http"]`: {"cli", "http"}, `"http"`: {"http"}, `[]`: {}}
	for raw, want := range ok {
		got, err := parseTags(json.RawMessage(raw))
		if err != nil || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%q -> %v %v", raw, got, err)
		}
	}
	for _, raw := range []string{`{"http":true}`, `[1,2]`, `["http",null]`, `true`, `[["http"]]`} {
		if _, err := parseTags(json.RawMessage(raw)); !errors.Is(err, ErrProtocol) {
			t.Fatalf("%q accepted", raw)
		}
	}
}

func rightsCase(t *testing.T, mutate func(*rciFake)) (*fakeKeenetic, error) {
	t.Helper()
	f := newFake()
	mutate(&f.rights)
	v := setup(t, f)
	err := v.Verify(context.Background(), f.login, f.password)
	return f, err
}

func TestNDW2RightsAreAppliedAfterPasswordAndSessionIsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		tags any
		want error
	}{
		{"router_admin_tags", []string{"cli", "http", "opt", "cifs", "printers", "webdav", "http-proxy"}, nil},
		{"single_string_http", "http", nil},
		{"readonly", []string{"http", "readonly"}, ErrForbidden},
		{"manager", []string{"http", "manager"}, ErrForbidden},
		{"no_web_rights", []string{"cifs", "webdav"}, ErrForbidden},
		{"vpn_only", []string{"ipsec-l2tp"}, ErrForbidden},
		{"tag_field_absent", nil, ErrForbidden},
		{"substring_is_not_tag", "httpx", ErrForbidden},
		{"tag_object", map[string]any{"http": true}, ErrProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := rightsCase(t, func(r *rciFake) { r.tags = tc.tags })
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if f.rights.rciCalls != 2 || f.rights.deletes != 1 || f.rights.noCookie {
				t.Fatalf("rci=%d logout=%d noCookie=%v", f.rights.rciCalls, f.rights.deletes, f.rights.noCookie)
			}
			if strings.Join(f.rights.order, ",") != "whoami,show.sc.user,logout" {
				t.Fatalf("order %v", f.rights.order)
			}
		})
	}
}

func TestRCIRefusalAndMalformedRightsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*rciFake)
		want   error
	}{
		{"rci_403", func(r *rciFake) { r.status = 403 }, ErrForbidden},
		{"rci_401", func(r *rciFake) { r.status = 401 }, ErrForbidden},
		{"rci_500", func(r *rciFake) { r.status = 500 }, ErrProtocol},
		{"oversize", func(r *rciFake) { r.oversize = true }, ErrProtocol},
		{"whoami_without_user", func(r *rciFake) { r.whoami = map[string]any{"id": "x"} }, ErrProtocol},
		{"whoami_control_char", func(r *rciFake) { r.whoami = map[string]any{"user": "te\x01st"} }, ErrProtocol},
		{"user_missing_in_config", func(r *rciFake) { r.users = map[string]any{"someone-else": map[string]any{"tag": []string{"http"}}} }, ErrProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := rightsCase(t, tc.mutate)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if f.rights.deletes != 1 {
				t.Fatalf("Keenetic session not closed: %d", f.rights.deletes)
			}
		})
	}
}

// Rights are looked up for the identity KeeneticOS reports (whoami), never by
// the typed login: a typed admin name cannot borrow another entry's tags.
func TestWhoamiIdentityIsAuthoritative(t *testing.T) {
	f, err := rightsCase(t, func(r *rciFake) {
		r.user = "limited"
		r.users = map[string]any{
			"test":    map[string]any{"tag": []string{"cli", "http"}},
			"limited": map[string]any{"tag": []string{"http", "readonly"}},
		}
	})
	if !errors.Is(err, ErrForbidden) || f.rights.deletes != 1 {
		t.Fatalf("err=%v", err)
	}
}

func TestWrongPasswordMakesNoRightsQuery(t *testing.T) {
	f := newFake()
	v := setup(t, f)
	if err := v.Verify(context.Background(), f.login, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if f.rights.rciCalls != 0 || f.rights.deletes != 0 {
		t.Fatalf("rci=%d logout=%d", f.rights.rciCalls, f.rights.deletes)
	}
}

func TestNDW4RightsAreAppliedAfterPassword(t *testing.T) {
	for _, tc := range []struct {
		tags []string
		want error
	}{
		{[]string{"cli", "http", "opt"}, nil},
		{[]string{"http", "readonly"}, ErrForbidden},
		{[]string{"http", "manager"}, ErrForbidden},
	} {
		rights := newRCIFake("synthetic-ndm", syntheticNDW4User, tc.tags)
		f := &ndw4Fake{scheme: "both-data", rights: &rights}
		svr := httptest.NewServer(f)
		uri, _ := url.Parse(svr.URL + "/auth")
		// Allow the synthetic password proof to finish under ARM64 QEMU/race.
		v := &Verifier{endpoint: *uri, requestTimeout: 30 * time.Second, connectTimeout: time.Second,
			memoryAvailable: func() (uint64, error) { return 1 << 30, nil }}
		err := v.Verify(context.Background(), syntheticNDW4User, syntheticNDW4Password)
		svr.Close()
		if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
			t.Fatalf("%v: err=%v want %v", tc.tags, err, tc.want)
		}
		if f.bad || f.postCount != 3 || rights.rciCalls != 2 || rights.deletes != 1 || rights.noCookie {
			t.Fatalf("%v: bad=%v posts=%d rci=%d logout=%d noCookie=%v", tc.tags, f.bad, f.postCount, rights.rciCalls, rights.deletes, rights.noCookie)
		}
	}
}

func TestRCIURLIsSameOriginRoot(t *testing.T) {
	u, _ := url.Parse("http://192.168.1.1:80/auth?x=1#y")
	if got := rciURL(*u); got != "http://192.168.1.1:80/rci/" {
		t.Fatal(got)
	}
}

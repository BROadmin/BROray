package ops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

func TestBROrayOwnedKeenDNSOrigin(t *testing.T) {
	testArea(t)
	const owned = "https://broray.owner.netcraze.link"
	previous := compat.KeenDNSOrigin
	t.Cleanup(func() { compat.KeenDNSOrigin = previous })
	compat.KeenDNSOrigin = func(context.Context) (string, bool) { return owned, true }
	paths := []string{
		"/api/broray/update-check.cgi", "/api/broray/update-start.cgi",
		"/api/broray/reinstall.cgi", "/api/broray/restore.cgi",
		"/api/broray/backup-export.cgi", "/api/broray/backup-preview.cgi",
		"/api/broray/backup-import.cgi", "/api/broray/backup-undo.cgi",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			// KeenDNS removes the ordinary Origin header. Only the current,
			// owned publication may supply the explicit page-origin fallback.
			r := call(t, http.MethodPost, path, "{}", map[string]string{
				"Content-Type": "application/json", "X-BROray-Request": "1", "X-BROray-Origin": owned,
			})
			if r.Code == http.StatusForbidden {
				t.Fatalf("owned KeenDNS page rejected: %d %s", r.Code, r.Raw)
			}
		})
	}
	for _, tc := range []struct {
		name, marker, origin, fallback string
		owned                          bool
	}{
		{"missing marker", "", "", owned, true},
		{"wrong marker", "operations", "", owned, true},
		{"missing origins", "1", "", "", true},
		{"foreign fallback", "1", "", "https://broray.foreign.example", true},
		{"LAN fallback without native Origin", "1", "", "http://example.com", true},
		{"native and fallback disagree", "1", "http://example.com", owned, true},
		{"foreign native Origin", "1", "https://evil.example", owned, true},
		{"publication unowned", "1", "", owned, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compat.KeenDNSOrigin = func(context.Context) (string, bool) { return owned, tc.owned }
			r := call(t, http.MethodPost, paths[0], "{}", map[string]string{
				"Content-Type": "application/json", "X-BROray-Request": tc.marker,
				"Origin": tc.origin, "X-BROray-Origin": tc.fallback,
			})
			if r.Code != http.StatusForbidden {
				t.Fatalf("unsafe origin accepted: %d %s", r.Code, r.Raw)
			}
		})
	}
	t.Run("request headers preserved", func(t *testing.T) {
		compat.KeenDNSOrigin = func(context.Context) (string, bool) { return owned, true }
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("X-BROray-Request", "1")
		r.Header.Set("X-BROray-Origin", owned)
		c := &cgi.Req{R: r, W: httptest.NewRecorder()}
		if !requireBROrayOrigin(c) {
			t.Fatal("owned fallback rejected")
		}
		if r.Header.Get("X-BROray-Request") != "1" || r.Header.Get("Origin") != "" {
			t.Fatal("origin validation modified the original request")
		}
	})
}

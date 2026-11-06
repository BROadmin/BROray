package ops

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/BROadmin/broray/internal/cgi"
)

func TestUninstallPublicHostRejectedBeforeWorker(t *testing.T) {
	a := testArea(t)
	var started atomic.Bool
	a.uninstall = uninstallHooks{
		preflight: func(context.Context, bool) error {
			started.Store(true)
			return errors.New("fixture stopped before side effects")
		},
	}

	r := httptest.NewRequest(http.MethodPost, "https://broray.example.net/api/broray/uninstall.cgi",
		strings.NewReader(`{"mode":"normal","confirmation":"УДАЛИТЬ BROray"}`))
	r.Header.Set("X-BROray-Request", "1")
	r.Header.Set("Origin", "https://broray.example.net")
	w := httptest.NewRecorder()
	if !cgi.Serve(w, r, "/api/broray/uninstall.cgi", nil) {
		t.Fatal("uninstall route not registered")
	}
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "REMOTE_UNINSTALL_FORBIDDEN") {
		t.Fatalf("public-host uninstall must fail before worker: HTTP %d: %s", w.Code, w.Body.String())
	}
	if started.Load() || a.page.running() {
		t.Fatal("public-host uninstall started a worker")
	}
}

func TestLocalUninstallAccessBoundary(t *testing.T) {
	cases := []struct {
		host   string
		remote string
		want   bool
	}{
		{"192.168.1.1", "192.168.1.50:12345", true},
		{"192.168.1.1:8080", "127.0.0.1:12345", true},
		{"10.0.0.1", "10.1.2.3:12345", true},
		{"172.31.0.1", "[::1]:12345", true},
		{"broray.example.net", "127.0.0.1:12345", false},
		{"192.168.1.1", "203.0.113.5:12345", false},
		{"[::1]", "127.0.0.1:12345", false},
		{"192.168.1.1:bad", "127.0.0.1:12345", false},
		{"192.168.001.1", "127.0.0.1:12345", false},
	}
	for _, tc := range cases {
		if got := localUninstallAccess(tc.host, tc.remote); got != tc.want {
			t.Errorf("host=%q remote=%q: got %t, want %t", tc.host, tc.remote, got, tc.want)
		}
	}
}

package servers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/web"
)

type availabilityVerifier struct{}

func (availabilityVerifier) Verify(context.Context, string, string) error { return nil }

// Exercise the real WebUI/auth handlers alongside a blocked VPN probe. No
// upstream traffic or live router is involved; only the probe boundary is held.
func TestWebUIIndependentOfVPNFailures(t *testing.T) {
	for _, state := range []string{"one-server-down", "all-servers-down", "xray-stopped", "probe-pending"} {
		t.Run(state, func(t *testing.T) {
			e := newEnv(t)
			a := e.addServer(t, "trojan://synthetic@one.example:443#One")
			b := e.addServer(t, "trojan://synthetic@two.example:443#Two")
			if _, err := e.s.activate(a); err != nil {
				t.Fatal(err)
			}
			if err := writeQuality(e.d, a, map[string]any{"status": "unavailable", "rating": "unavailable"}); err != nil {
				t.Fatal(err)
			}
			other := "unavailable"
			if state == "one-server-down" {
				other = "available"
			}
			if err := writeQuality(e.d, b, map[string]any{"status": other}); err != nil {
				t.Fatal(err)
			}
			e.health.Store("unhealthy")
			_, health, _ := e.s.activeMeasure(context.Background(), a)
			if err := e.s.updateState(func(st map[string]any) bool { st["activeHealth"] = health; return true }); err != nil {
				t.Fatal(err)
			}
			if state == "xray-stopped" {
				e.running.Store(false)
			}
			if state == "probe-pending" {
				entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
				e.s.activeProbe = func(ctx context.Context, _, _ string) map[string]any {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
					}
					return health
				}
				go func() { defer close(done); e.s.activeMeasure(context.Background(), a) }()
				<-entered
				t.Cleanup(func() { close(release); <-done })
			}
			srv := httptest.NewServer(web.NewServer(nil, availabilityVerifier{}))
			defer srv.Close()
			client := &http.Client{Timeout: time.Second}
			login, err := client.Post(srv.URL+"/api/login.cgi", "application/json", strings.NewReader(`{"login":"synthetic","password":"synthetic"}`))
			if err != nil {
				t.Fatal(err)
			}
			cookies := login.Cookies()
			login.Body.Close()
			if login.StatusCode != 200 || len(cookies) != 1 {
				t.Fatal("login unavailable")
			}
			for _, path := range []string{"/", "/assets/js/common.js", "/home.html", "/servers.html", "/xray.html", "/api/session.cgi", "/api/servers/summary.cgi"} {
				req, _ := http.NewRequest("GET", srv.URL+path, nil)
				req.AddCookie(cookies[0])
				started := time.Now()
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("%s blocked by %s: %v", path, state, err)
				}
				_, err = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != 200 {
					t.Fatalf("%s: status=%d err=%v", path, resp.StatusCode, err)
				}
				t.Logf("%s %s: HTTP200 in %s", state, path, time.Since(started))
			}
		})
	}
}

package installer

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// Install copies the test executable. Only its external daemon startup is
// substituted; filesystem installation and the Unix HTTP boundary are real.
func TestMain(m *testing.M) {
	if os.Getenv("BRORAY_INSTALL_TEST_CHILD") == "1" && len(os.Args) > 1 {
		if os.Args[1] == "start" || (len(os.Args) > 2 && os.Args[1] == "ctl" && os.Args[2] == "recover") {
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestInitialPublicationRefusesUnsafeOrFailedStates(t *testing.T) {
	const disabled = `{"success":true,"data":{"state":"disabled","canEnable":true,"keenDns":{"available":true}}}`
	const ready = `{"success":true,"data":{"state":"enabled","enabled":true,"consistent":true,"address":"https://broray.fixture.keenetic.pro/","keenDns":{"available":true}}}`
	for _, tc := range []struct {
		name, first, post, last, want string
		status, posts                 int
	}{
		{name: "missing-domain", first: `{"success":true,"data":{"state":"disabled","reason":{"code":"KEENDNS_ADDRESS_UNAVAILABLE"}}}`, want: "KEENDNS_ADDRESS_UNAVAILABLE"},
		{name: "foreign-conflict", first: `{"success":true,"data":{"state":"conflict","canEnable":true,"keenDns":{"available":true},"reason":{"code":"WEB_ACCESS_OWNERSHIP_CONFLICT"}}}`, want: "WEB_ACCESS_OWNERSHIP_CONFLICT"},
		{name: "recovery", first: `{"success":true,"data":{"state":"recovery","canEnable":true,"keenDns":{"available":true},"reason":{"code":"WEB_ACCESS_RECOVERY_REQUIRED"}}}`, want: "WEB_ACCESS_RECOVERY_REQUIRED"},
		{name: "drift", first: `{"success":true,"data":{"state":"enabled","enabled":true,"canEnable":true,"keenDns":{"available":true},"reason":{"code":"WEB_ACCESS_LAN_CHANGED"}}}`, want: "WEB_ACCESS_LAN_CHANGED"},
		{name: "already-owned", first: ready},
		{name: "post-error", first: disabled, post: `{"success":false,"error":{"code":"OPERATION_BUSY","message":"PRIVATE-TEXT"}}`, want: "OPERATION_BUSY", posts: 1},
		{name: "false-success", first: disabled, post: `{"success":true}`, last: disabled, want: "WEB_ACCESS_NOT_CONFIRMED", posts: 1},
		{name: "bad-json", first: `<html>PRIVATE-TEXT</html>`, want: "WEB_ACCESS_INVALID_RESPONSE"},
		{name: "null-status", first: `{"success":true,"data":null}`, want: "WEB_ACCESS_NOT_CONFIRMED"},
		{name: "http-failure", first: `{"success":false,"error":{"code":"WEB_ACCESS_STATUS_FAILED"}}`, status: 503, want: "WEB_ACCESS_STATUS_FAILED"},
		{name: "unsafe-error", first: `{"success":false,"error":{"code":"PRIVATE-TEXT"}}`, want: "WEB_ACCESS_HTTP_200"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "bw-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			root := platform.Root{Dir: dir}
			if err = root.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			ln, err := net.Listen("unix", root.DaemonSocket())
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			posts, gets := 0, 0
			srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				body := tc.first
				if r.Method == "POST" {
					posts++
					body = tc.post
				} else {
					gets++
					if gets > 1 {
						body = tc.last
					}
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				w.Write([]byte(body))
			})}
			go srv.Serve(ln)
			defer srv.Close()
			var rep Report
			publishInitialWebUI(context.Background(), root, &rep)
			mu.Lock()
			defer mu.Unlock()
			if posts != tc.posts {
				t.Fatalf("unexpected write count %d want %d", posts, tc.posts)
			}
			warnings := strings.Join(rep.Warnings, "\n")
			if tc.want == "" {
				if warnings != "" || len(rep.Steps) != 1 {
					t.Fatalf("confirmed address not reported: %+v", rep)
				}
			} else {
				if !strings.Contains(warnings, tc.want) || !strings.Contains(warnings, "ctl post") || strings.Contains(warnings, "PRIVATE-TEXT") {
					t.Fatalf("missing safe actionable warning: %+v", rep)
				}
				if strings.Contains(strings.Join(rep.Steps, "\n"), "KeenDNS WebUI:") {
					t.Fatal("failed publication reported as success")
				}
			}
		})
	}
}

func TestInitialPublicationDeadline(t *testing.T) {
	dir, err := os.MkdirTemp("", "bw-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	root := platform.Root{Dir: dir}
	if err = root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", root.DaemonSocket())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release })}
	go srv.Serve(ln)
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Report, 1)
	go func() { var rep Report; publishInitialWebUI(ctx, root, &rep); done <- rep }()
	<-entered
	cancel()
	select {
	case rep := <-done:
		if !strings.Contains(strings.Join(rep.Warnings, " "), "WEB_ACCESS_CONTROL_UNAVAILABLE") {
			t.Fatalf("%+v", rep)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publication ignored cancellation")
	}
}

func TestInitialPublicationWaitsForBootstrapLock(t *testing.T) {
	dir, err := os.MkdirTemp("", "bw-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	root := platform.Root{Dir: dir}
	if err = root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", root.DaemonSocket())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	posts := 0
	enabled := false
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "POST" {
			posts++
			if posts == 1 {
				w.WriteHeader(409)
				w.Write([]byte(`{"success":false,"error":{"code":"OPERATION_BUSY"}}`))
				return
			}
			enabled = true
		}
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"state": state, "enabled": enabled, "consistent": enabled, "canEnable": !enabled, "address": "https://broray.fixture.keenetic.pro/", "keenDns": map[string]bool{"available": true}}})
	})}
	go srv.Serve(ln)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var rep Report
	publishInitialWebUI(ctx, root, &rep)
	mu.Lock()
	defer mu.Unlock()
	if !enabled || posts != 2 || len(rep.Warnings) != 0 {
		t.Fatalf("first Xray bootstrap prevented publication: posts=%d report=%+v", posts, rep)
	}
}

func TestInstallPublishesFreshWebUI(t *testing.T) {
	for _, tc := range []struct {
		name              string
		existing, noStart bool
		wantCalls         int
	}{
		{"fresh", false, false, 3}, {"existing-config", true, false, 0}, {"no-start", false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "bw-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			old := Opt
			Opt = filepath.Join(dir, "opt")
			t.Cleanup(func() { Opt = old })
			root := platform.Root{Dir: filepath.Join(Opt, "broray")}
			if err = root.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Dir(InitScript()), 0755); err != nil {
				t.Fatal(err)
			}
			if tc.existing {
				if err = platform.SaveConfig(root, platform.DefaultConfig()); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("BRORAY_INSTALL_TEST_CHILD", "1")
			ln, err := net.Listen("unix", root.DaemonSocket())
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			calls := []string{}
			enabled := false
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.Method == "POST" {
					if r.URL.Path != "/api/keenetic/web-access-enable.cgi" || r.Header.Get("X-BROray-Request") != "1" || r.Header.Get("Origin") != "http://broray" {
						http.Error(w, "bad request", 400)
						return
					}
					enabled = true
				}
				state := "disabled"
				if enabled {
					state = "enabled"
				}
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"state": state, "enabled": enabled, "consistent": enabled, "canEnable": !enabled, "address": "https://broray.fixture.keenetic.pro/", "localAddress": "http://192.0.2.1:8080/", "keenDns": map[string]any{"available": true}, "reason": map[string]string{"code": "WEB_ACCESS_DISABLED"}}})
			})}
			go server.Serve(ln)
			t.Cleanup(func() { server.Close() })
			rep, err := Install(root, "3.5.6-test", !tc.noStart)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(calls) != tc.wantCalls {
				t.Fatalf("publication requests=%v; want %d after installation; report=%+v", calls, tc.wantCalls, rep)
			}
			if tc.wantCalls > 0 && (!enabled || !strings.Contains(strings.Join(rep.Steps, "\n"), "https://broray.fixture.keenetic.pro/")) {
				t.Fatalf("publication not confirmed with address: %+v", rep)
			}
			if tc.noStart && len(rep.Warnings) == 0 {
				t.Fatal("no-start must explain manual publication after starting daemon")
			}
		})
	}
}

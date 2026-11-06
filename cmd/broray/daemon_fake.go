//go:build brorayfake

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

// Host-test build only (-tags brorayfake, used by update_e2e_test.go):
// `broray daemon` becomes a minimal stand-in that owns the daemon lock and
// serves the lifecycle part of the control socket, so the real launcher,
// installer apply, init script, start and recover can be exercised on a
// temporary root. Never part of a router build.
//
//	BRORAY_FAKE_DAEMON=<version>:<mode>[,…]  broken (exit at once) | noping (never ready) | hang (ignores shutdown) | delay<N> (hold lock N seconds after accepting shutdown)
//	BRORAY_FAKE_CRASH_AFTER=<version>:<journal state>  installer exits right after writing that state
//	BRORAY_FAKE_TRUSTED_KEY=<minisign public key>     trusted for the test channel
func init() {
	daemonOverride = fakeDaemon
	if st := fakeSetting("BRORAY_FAKE_CRASH_AFTER"); st != "" {
		installer.CrashAfter = st
	}
	if k := os.Getenv("BRORAY_FAKE_TRUSTED_KEY"); k != "" {
		update.TrustExtra(update.TrustedKey{Name: "host test", PublicKey: k, Channels: []string{"test"}, Test: true})
	}
}

// fakeSetting returns the value for this binary's version in a
// "<version>:<value>,…" list.
func fakeSetting(env string) string {
	for _, item := range strings.Split(os.Getenv(env), ",") {
		if v, val, ok := strings.Cut(item, ":"); ok && v == app.Version {
			return val
		}
	}
	return ""
}

func fakeDaemon(root platform.Root) int {
	mode := fakeSetting("BRORAY_FAKE_DAEMON")
	if err := root.EnsureDirs(); err != nil {
		return 1
	}
	lock, err := platform.TryLock(root.DaemonLock())
	if err != nil {
		if errors.Is(err, platform.ErrLocked) {
			return 3
		}
		return 1
	}
	defer lock.Release()
	if mode == "broken" {
		return 1
	}
	_ = os.WriteFile(root.Run()+"/fake-daemon.pid", []byte(strconv.Itoa(os.Getpid())), 0o600)
	token, _ := control.EnsureToken(root)
	stopCh := make(chan struct{}, 1)
	stop := func() {
		if mode == "hang" {
			return
		}
		select {
		case stopCh <- struct{}{}:
		default:
		}
	}
	mux := http.NewServeMux()
	control.Mount(mux, app.Version, token, stop)
	var h http.Handler = mux
	if mode == "noping" {
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == control.PingPath {
				w.WriteHeader(503)
				return
			}
			mux.ServeHTTP(w, r)
		})
	}
	_ = os.Remove(root.DaemonSocket())
	ln, err := net.Listen("unix", root.DaemonSocket())
	if err != nil {
		return 1
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sig:
	case <-stopCh:
	}
	shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shut) // graceful, like the real daemon: the shutdown reply is delivered
	if seconds, err := strconv.Atoi(strings.TrimPrefix(mode, "delay")); strings.HasPrefix(mode, "delay") && err == nil && seconds > 0 && seconds <= 90 {
		time.Sleep(time.Duration(seconds) * time.Second)
	}
	_ = os.Remove(root.DaemonSocket())
	_ = os.Remove(root.Run() + "/fake-daemon.pid")
	return 0
}

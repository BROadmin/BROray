package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/web"
)

// devVerifierOverride is set only by the dev build tag (dev_auth.go).
var devVerifierOverride keenetic.CredentialVerifier

// lazyVerifier resolves the management endpoint (LAN IP:80) at first use so
// the daemon can start before KeeneticOS answers RCI.
type lazyVerifier struct {
	a    *app.App
	mu   sync.Mutex
	v    *keenetic.Verifier
	host string
}

func (l *lazyVerifier) Verify(ctx context.Context, login, password string) error {
	host := l.a.Cfg.Keenetic.ManagementHost
	if host == "" {
		lan, err := l.a.LAN(ctx)
		if err != nil {
			return keenetic.ErrUnavailable
		}
		host = lan
	}
	l.mu.Lock()
	if l.v == nil || l.host != host {
		addr, err := netip.ParseAddr(host)
		if err != nil {
			l.mu.Unlock()
			return keenetic.ErrUnavailable
		}
		v, err := keenetic.NewVerifier(keenetic.ManagementEndpoint{Address: addr, Port: uint16(l.a.Cfg.Keenetic.ManagementPort)})
		if err != nil {
			l.mu.Unlock()
			return keenetic.ErrUnavailable
		}
		l.v, l.host = v, host
	}
	v := l.v
	l.mu.Unlock()
	return v.Verify(ctx, login, password)
}

func runDaemon(root platform.Root) int {
	if err := root.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, "broray: "+err.Error())
		return 1
	}
	lock, err := platform.TryLock(root.DaemonLock())
	if err != nil {
		if errors.Is(err, platform.ErrLocked) {
			fmt.Fprintln(os.Stderr, "broray: daemon already running (lock held)")
			return 3
		}
		fmt.Fprintln(os.Stderr, "broray: lock: "+err.Error())
		return 1
	}
	defer lock.Release()
	cfg, err := platform.LoadConfig(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "broray: config: "+err.Error())
		return 1
	}
	logf, _ := os.OpenFile(root.Logs()+"/brorayd.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	var out io.Writer = os.Stderr
	if logf != nil {
		defer logf.Close()
		out = io.MultiWriter(os.Stderr, logf)
	}
	logger := log.New(out, "", log.LstdFlags)
	logger.Printf("BROray %s starting (root %s, transport %s)", app.Version, root.Dir, cfg.Keenetic.WriteTransport)
	a := app.New(root, cfg, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := compat.NewDeps(ctx, a)
	// Register all area endpoints before opening the listener.
	if err := compat.StartAreas(deps); err != nil {
		logger.Printf("startup aborted: %v", err)
		return 1
	}
	go deps.Queue.Run(ctx)

	// Control socket (root-only, no auth).
	_ = os.Remove(root.DaemonSocket())
	sock, err := net.Listen("unix", root.DaemonSocket())
	if err != nil {
		logger.Printf("control socket: %v", err)
		return 1
	}
	_ = os.Chmod(root.DaemonSocket(), 0o600)
	stopCh := make(chan struct{}, 1)
	// The shutdown token lets an installer of any version stop this daemon
	// through the socket (design §6); it is created once and kept.
	token, err := control.EnsureToken(root)
	if err != nil {
		logger.Printf("control token: %v (authenticated shutdown unavailable)", err)
	}
	ctl := web.NewControlServer(a)
	ctlMux := http.NewServeMux()
	control.Mount(ctlMux, app.Version, token, func() {
		select {
		case stopCh <- struct{}{}:
		default:
		}
	})
	ctlMux.Handle("/", ctl)
	ctlSrv := &http.Server{Handler: ctlMux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = ctlSrv.Serve(sock) }()

	// LAN listener.
	bind := cfg.ListenAddress
	if bind == "" {
		lctx, lcancel := context.WithTimeout(ctx, 20*time.Second)
		if lan, err := a.LAN(lctx); err == nil {
			bind = lan
		} else {
			logger.Printf("LAN address unknown (%v); binding all interfaces", err)
			bind = "0.0.0.0"
		}
		lcancel()
	}
	addr := net.JoinHostPort(bind, strconv.Itoa(cfg.WebPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Printf("listen %s: %v", addr, err)
		return 1
	}
	var verifier keenetic.CredentialVerifier = &lazyVerifier{a: a}
	if devVerifierOverride != nil {
		logger.Printf("WARNING: dev credential verifier active (BRORAY_DEV_LOGIN); not a router build")
		verifier = devVerifierOverride
	}
	// No WriteTimeout: routes export/delete/resume hold one POST open for the
	// whole operation (3.x contract; the page waits up to 8 h).
	webSrv := &http.Server{Handler: web.NewServer(a, verifier), ReadHeaderTimeout: 15 * time.Second, ReadTimeout: 120 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		if err := webSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			logger.Printf("web: %v", err)
		}
	}()
	logger.Printf("WebUI on http://%s/", addr)

	// dev1 App.Boot/App.Loops are replaced by the areas (Xray start and
	// interface reconcile: areas/xray, areas/keenetic; health, auto-switch:
	// areas/servers; subscription schedule: areas/subscriptions).

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	select {
	case s := <-sig:
		logger.Printf("signal %v: stopping", s)
	case <-stopCh:
		logger.Printf("stop requested via control socket")
	}
	cancel()
	// Stop new WebUI requests before waiting for an active UDP check. Its
	// temporary-route removal has a fresh 60-second context; allow five more
	// seconds for the job to finish. The installer stop wait must exceed this
	// daemon budget (UPD-3) before a candidate is used on a router.
	_ = ln.Close()
	shut, scancel := context.WithTimeout(context.Background(), 85*time.Second)
	defer scancel()
	udpWait, udpCancel := context.WithTimeout(shut, 65*time.Second)
	udpErr := deps.Svc.WaitKeeneticUDPJobs(udpWait)
	udpCancel()
	if udpErr != nil {
		logger.Printf("UDP route cleanup before shutdown: %v", udpErr)
	}
	webShut, webCancel := context.WithTimeout(shut, 10*time.Second)
	_ = webSrv.Shutdown(webShut)
	webCancel()
	_ = a.Xray.Stop(shut)
	_ = ctlSrv.Shutdown(shut)
	_ = os.Remove(root.DaemonSocket())
	logger.Printf("stopped")
	if udpErr != nil {
		return 1
	}
	return 0
}

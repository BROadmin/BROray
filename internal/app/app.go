// Package app holds the daemon-wide dependencies (config, Keenetic
// transport, the supervised Xray process, HTTP client, logger). All WebUI
// functions live in the areas/* packages (3.x CGI contracts); the dev1
// Servers/Subs/Routes stores remain only for the installer's legacy receipts.
package app

import (
	"context"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/ops"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/health"
	"github.com/BROadmin/broray/modules/routes"
	"github.com/BROadmin/broray/modules/servers"
	"github.com/BROadmin/broray/modules/subscriptions"
	"github.com/BROadmin/broray/modules/xray"
)

// Version is a var only so host tests can build binaries of other versions
// (-ldflags -X); release builds never override it.
var Version = "3.5.0"

type App struct {
	Root    platform.Root
	Cfg     platform.Config
	T       keenetic.Transport
	K       *keenetic.Adapter
	Servers *servers.Store
	Subs    *subscriptions.Store
	Routes  *routes.Store
	Xray    *xray.Process
	Ops     *ops.Manager
	Monitor *health.Monitor
	HTTP    *http.Client
	Log     *log.Logger

	mu            sync.Mutex
	lanIP         string
	lanAt         time.Time
	cooldownUntil time.Time
	startedAt     time.Time
}

func New(root platform.Root, cfg platform.Config, logger *log.Logger) *App {
	t := keenetic.NewTransport(cfg.Keenetic.WriteTransport, cfg.Keenetic.RCIURL)
	tr := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second}
	a := &App{
		Root: root, Cfg: cfg, T: t,
		K:         keenetic.NewAdapter(t, root.Receipts(), cfg.Keenetic.RouteMetric),
		Servers:   servers.NewStore(root.Servers()),
		Subs:      subscriptions.NewStore(root.Subscriptions()),
		Routes:    routes.NewStore(root, cfg.Routes.Repository, cfg.Routes.Commit),
		Xray:      xray.NewProcess(root),
		Ops:       ops.NewManager(root.Operations()),
		Monitor:   &health.Monitor{},
		HTTP:      &http.Client{Transport: tr, Timeout: 90 * time.Second},
		Log:       logger,
		startedAt: time.Now(),
	}
	return a
}

// LAN returns the address the WebUI and the SOCKS inbound bind to.
func (a *App) LAN(ctx context.Context) (string, error) {
	if a.Cfg.ListenAddress != "" {
		return a.Cfg.ListenAddress, nil
	}
	a.mu.Lock()
	if a.lanIP != "" && time.Since(a.lanAt) < 5*time.Minute {
		ip := a.lanIP
		a.mu.Unlock()
		return ip, nil
	}
	a.mu.Unlock()
	ip, err := a.K.LANAddress(ctx, a.Cfg.Keenetic.LANInterface)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.lanIP, a.lanAt = ip, time.Now()
	a.mu.Unlock()
	return ip, nil
}

func (a *App) SaveConfig() error { return platform.SaveConfig(a.Root, a.Cfg) }

func (a *App) LogPath() string { return filepath.Join(a.Root.Logs(), "brorayd.log") }

// App_ops exposes the current operation for the API.
func (a *App) App_ops() (ops.Record, bool) { return a.Ops.Current() }

func pinnedTag(configured string) string {
	t, err := xray.ResolveTag(configured)
	if err != nil {
		return configured + " (not pinned)"
	}
	return t
}

// Package xray ports the 3.x "xray" WebUI area (contract:
// docs/contracts/keenetic-xray.md §3): status, lifecycle, config check,
// pinned-version catalog, the asynchronous install job, diagnostics and the
// Xray log rotation. It also provides Services.XrayApplyConfig/XrayRunning.
package xray

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

func init() { compat.Area("xray", 50, setup) }

type service struct {
	d      *compat.Deps
	lifeMu sync.Mutex // start/stop/restart, config apply and the binary swap

	cacheMu  sync.Mutex
	cache    map[string]any
	cacheAt  time.Time
	cachePID string

	jobs sync.WaitGroup
}

var (
	current atomic.Pointer[service]
	regOnce sync.Once
)

func newService(d *compat.Deps) *service { return &service{d: d} }

func setup(d *compat.Deps) error {
	s := newService(d)
	current.Store(s)
	regOnce.Do(register)
	d.Svc.XrayApplyConfig = func(ctx context.Context, cfg []byte) error { return current.Load().ApplyConfig(ctx, cfg) }
	d.Svc.XrayRunning = func() bool { return d.App.Xray.Running() }
	compat.RegisterSnapshot("xray", func(ctx context.Context) (map[string]any, error) {
		return current.Load().computeStatus(ctx), nil
	})
	if d.Maintenance {
		return nil
	}
	s.recoverInstall()
	go s.logMaintenance(d.Ctx)
	go func() {
		s.bootstrapInstall(d.Ctx)
		s.bootStart(d.Ctx)
	}()
	return nil
}

func register() {
	cgi.RegisterRaw("/api/xray/status.cgi", func(c *cgi.Req) {
		// The 3.x CGI prints the pre-enveloped status as is.
		cgi.WriteJSON(c.W, 200, map[string]any{"success": true, "data": current.Load().statusCached(c.R.Context()), "error": nil})
	})
	for _, a := range []string{"start", "stop", "restart"} {
		action := a
		cgi.Register("/api/xray/"+action+".cgi", func(c *cgi.Req) (any, error) { return current.Load().handleLifecycle(c, action) }, cgi.POST())
	}
	cgi.Register("/api/xray/check-config.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleCheckConfig(c) }, cgi.POST())
	cgi.Register("/api/xray/update-check.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleUpdateCheck(c) }, cgi.POST())
	cgi.Register("/api/xray/diagnostics.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleDiagnostics(c) })
	invalid := cgi.TooLarge(cgi.E(400, "XRAY_INSTALL_REQUEST_INVALID", "Некорректный запрос выбора версии Xray. Обновите список версий."))
	cgi.RegisterRaw("/api/xray/install.cgi", func(c *cgi.Req) { current.Load().handleLaunch(c, "install") }, cgi.POST(), cgi.Limit(4096), invalid)
	cgi.RegisterRaw("/api/xray/update.cgi", func(c *cgi.Req) { current.Load().handleLaunch(c, "update") }, cgi.POST())
	cgi.RegisterRaw("/api/xray/reinstall.cgi", func(c *cgi.Req) { current.Load().handleLaunch(c, "reinstall") }, cgi.POST())
	cgi.Register("/api/xray/operation-status.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleOperationStatus(c) })
}

// bootStart is 3.x S24broray start at boot: run Xray when its binary and
// config exist. Deviation: also requires an active server (config/active-server),
// so a deactivated BROray stays stopped after a router restart.
func (s *service) bootStart(ctx context.Context) {
	for _, p := range []string{s.d.Root.XrayBinary(), s.d.Root.XrayConfig(), s.d.Path("config", "active-server")} {
		if _, err := os.Stat(p); err != nil {
			return
		}
	}
	if s.d.App.Xray.Running() {
		return
	}
	waitClock(ctx, s.d.Log.Printf)
	if out, err := s.lifecycle(ctx, "start"); err != nil {
		s.d.Log.Printf("xray: boot start failed: %v %s", err, out)
		return
	}
	s.d.Log.Printf("xray: started at boot")
}

// Boot clock guard (CODEX-REPORT-EMU, XRAY_26_10_31_ARMV7_VM_INIT_PANIC):
// Xray 26.10.31 panics at start when the system clock is near 1970 (it
// derives the uTLS Chrome version from the date). A router without a
// battery clock may start BROray before NTP; wait for a sane clock, then
// start anyway.
var (
	clockNow      = time.Now
	clockMinYear  = 2025
	clockWaitStep = 5 * time.Second
	clockWaitMax  = 10 * time.Minute
)

func waitClock(ctx context.Context, logf func(string, ...any)) {
	if clockNow().Year() >= clockMinYear {
		return
	}
	logf("xray: system clock %s is not set yet; waiting up to %s for NTP before starting Xray", clockNow().UTC().Format(time.RFC3339), clockWaitMax)
	deadline := time.Now().Add(clockWaitMax)
	for clockNow().Year() < clockMinYear && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(clockWaitStep):
		}
	}
	if clockNow().Year() < clockMinYear {
		logf("xray: clock still not set after %s; starting Xray anyway", clockWaitMax)
	}
}

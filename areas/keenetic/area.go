// Package keenetic ports the 3.x "keenetic" WebUI area (contract:
// docs/contracts/keenetic-xray.md §2): the managed ProxyN → Xray SOCKS
// interface (exact R14C01 grammar, receipt config/interface.json, staged
// writes with rollback), the KeenDNS WebUI publication and the startup
// reconciliation. It provides Services.KeeneticEnsureInterface and
// compat.KeenDNSOrigin.
package keenetic

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	kn "github.com/BROadmin/broray/internal/keenetic"
)

func init() { compat.Area("keenetic", 50, setup) }

type service struct {
	d  *compat.Deps
	mu sync.Mutex // every ProxyN / KeenDNS write and the reconcile

	originMu sync.Mutex
	origin   string
	originOK bool
	originAt time.Time

	ipv6Mu         sync.Mutex // one IPv6 detection at a time (ipv6.go)
	ipv6           kn.IPv6Facts
	ipv6At         time.Time
	ipv6Stamp      string
	ipv6Refreshing bool
	// «Проверить UDP» background check (udpcheck.go)
	udpMu       sync.Mutex
	udpRunning  bool
	udpStopping bool
	udpOp       string
	udpJobs     sync.WaitGroup
	// SOCKS5 UDP capability (udpcap.go)
	capMu    sync.Mutex
	fw       string
	fwRead   bool
	runToken string // this daemon run (a record without a known firmware applies only to it)
}

var (
	current atomic.Pointer[service]
	regOnce sync.Once
)

func newService(d *compat.Deps) *service { return &service{d: d, runToken: compat.RandHex(8)} }

func setup(d *compat.Deps) error {
	s := newService(d)
	current.Store(s)
	regOnce.Do(register)
	d.Svc.KeeneticEnsureInterface = func(ctx context.Context) error { return current.Load().EnsureInterface(ctx) }
	d.Svc.KeeneticIPv6 = func(ctx context.Context) map[string]any { return current.Load().ipv6Fact(ctx) }
	d.Svc.KeeneticWaitUDPJobs = func(ctx context.Context) error { return current.Load().waitUDPJobs(ctx) }
	compat.KeenDNSOrigin = func(ctx context.Context) (string, bool) { return current.Load().keenDNSOrigin(ctx) }
	compat.RegisterSnapshot("keenetic", func(ctx context.Context) (map[string]any, error) { return current.Load().status(ctx) })
	compat.RegisterUninstallStep("publish", func(ctx context.Context) error { return current.Load().uninstallWeb(ctx) })
	compat.RegisterUninstallStep("keenetic", func(ctx context.Context) error { return current.Load().uninstallInterface(ctx) })
	if d.Maintenance {
		return nil
	}
	go s.reconcile(d.Ctx)
	go s.recoverUDPRouteAtStart(d.Ctx)
	return nil
}

func register() {
	cgi.Register("/api/keenetic/status.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleStatus(c) })
	for _, a := range []string{"check-upstream", "create", "repair", "sync-description", "delete"} {
		action := a
		cgi.Register("/api/keenetic/"+action+".cgi", func(c *cgi.Req) (any, error) { return current.Load().handleAction(c, action) }, cgi.POST())
	}
	cgi.Register("/api/keenetic/check-udp.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleUDPCheck(c) }, cgi.POST(), cgi.Status(202))
	cgi.Register("/api/keenetic/check-udp-status.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleUDPStatus(c) })
	cgi.Register("/api/keenetic/web-access-status.cgi", func(c *cgi.Req) (any, error) { return current.Load().handleWebStatus(c) })
	for _, a := range []string{"enable", "disable"} {
		action := a
		cgi.Register("/api/keenetic/web-access-"+action+".cgi", func(c *cgi.Req) (any, error) { return current.Load().handleWebAction(c, action) }, cgi.POST())
	}
}

// Package ops ports the 3.x HOME, BROray-page and OPERATIONS WebUI areas
// (contract: docs/contracts/home-broray-ops.md):
//
//   - /api/home/summary.cgi and the home snapshot collector (30 s loop that
//     calls every module's compat.SnapshotProducer and stores
//     run/home-snapshots/<module>.json exactly like 3.x);
//   - /api/broray/* (info, update check/start, reinstall, status, restore,
//     cleanup plan/execute, uninstall) on top of the 3.5.0 installer;
//   - /api/operations/* over the in-process coordinator (d.Queue).
//
// The AUTH endpoints live in internal/web (auth.go/server.go).
package ops

import (
	"context"
	"io/fs"
	"regexp"
	"sync"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/webui"
)

func init() { compat.Area("ops", 0, setup) }

// area is the state of the ops area. Handlers are registered once per
// process (the cgi registry is global) and always act on the current area,
// so tests can install a fresh one per case.
type area struct {
	d   *compat.Deps
	now func() time.Time

	page    *pageOps
	cleanMu sync.Mutex

	collMu   sync.Mutex
	collLast map[string]collectResult // last collector result per module

	// Replaceable steps (tests).
	uninstall uninstallHooks
	parts     func() []compat.BackupPart // settings backup parts; nil = compat registry
	updater   updaterHooks
}

var (
	curMu        sync.RWMutex
	cur          *area
	registerOnce sync.Once
)

func current() *area {
	curMu.RLock()
	defer curMu.RUnlock()
	return cur
}

func install(a *area) {
	curMu.Lock()
	cur = a
	curMu.Unlock()
	registerOnce.Do(register)
}

func newArea(d *compat.Deps) *area {
	a := &area{d: d, now: time.Now, collLast: map[string]collectResult{}}
	a.page = newPageOps(a)
	a.uninstall = defaultUninstallHooks(a)
	a.updater = defaultUpdaterHooks(a)
	return a
}

func setup(d *compat.Deps) error {
	a := newArea(d)
	if err := a.recoverIncompleteBackups(); err != nil {
		return err
	}
	install(a)
	compat.RegisterSnapshot("broray", func(ctx context.Context) (map[string]any, error) {
		return a.info(ctx, false), nil
	})
	compat.RegisterBackupPart(a.settingsPart())
	if !d.Maintenance {
		go a.collectLoop(d.Ctx)
		a.resumeWatch(d.Ctx)
	}
	return nil
}

func register() {
	registerHome()
	registerBROray()
	registerOperations()
	registerBackup()
}

// webUIBuild is the build id of the embedded WebUI (<html data-build-id>).
var webUIBuild = func() string {
	if b, err := fs.ReadFile(webui.Files, "static/index.html"); err == nil {
		if m := regexp.MustCompile(`data-build-id="(WebUI-[A-Za-z0-9.\-]+)"`).FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return "WebUI-3.5.0-dev6"
}()

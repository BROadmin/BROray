// Package routes ports the 3.x "routes" WebUI area (contract: docs/contracts):
// built-in route catalogs, the Keenetic static-route engine
// (preflight/export/delete/resume with rollback) and custom BAT route sets.
package routes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
)

func init() { compat.Area("routes", 50, setup) }

// setup registers the endpoints, the home snapshot and the uninstall step.
// It does no Keenetic writes; the only goroutine is the scheduled catalog
// check (schedule.go), which is not started in maintenance (CLI uninstall) mode.
//
// A prepare error (layout not writable) is returned for the log, but the
// handlers and the uninstall step are registered anyway: an uninstall must
// still be able to remove the routes BROray installed.
func setup(d *compat.Deps) error {
	e := newEngine(d)
	perr := e.prepare()
	setCurrent(e)
	registerHandlers()
	e.defineScheduleQueue()
	registerBackup(e)
	if perr != nil {
		return perr
	}
	if !d.Maintenance {
		go e.scheduleLoop(e.ctx)
	}
	return nil
}

// installedBundles lists bundles whose registry records routes BROray owns.
func (e *engine) installedBundles() []string {
	entries, _ := os.ReadDir(e.p("installed", "bundles"))
	var out []string
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(en.Name(), ".json")
		r, ok := readObj(e.registryFile(id))
		if !ok || !validBundleID(id) {
			continue
		}
		if r["installedVersion"] != nil || len(anyList(r["managedRouteKeys"])) > 0 {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// uninstallRoutes removes every route BROray installed (all catalog and
// custom bundles, by the installed registries) with the normal delete path:
// owned keys only, batch verification, save + running==startup check,
// rollback on failure. Nothing installed → nil. A resumable pause or any
// failure is an error (the uninstall aborts).
func (e *engine) uninstallRoutes(ctx context.Context) error {
	ids := e.installedBundles()
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		var rel compat.Release
		ok := false
		if compat.FenceHeld(ctx) {
			rel, ok = func() {}, true // the uninstall holds the fence
		} else {
			rel, ok = e.lock.TryAcquire("routes", "uninstall:delete", id)
		}
		if !ok {
			if h, held := e.lock.Current(); held && h.Pending && h.Scope == "routes" {
				e.lock.ClearPending()
				rel, ok = e.lock.TryAcquire("routes", "uninstall:delete", id)
			}
		}
		if !ok {
			return errors.New("ROUTES_OPERATION_BUSY: " + msgBusy)
		}
		var op *compat.Operation
		if e.queue != nil {
			op = e.queue.StartOperation("", "route_operation", "uninstall:delete", "USER", "protected", "")
		}
		_, err := e.runDelete(ctx, id, func() string {
			if op != nil {
				return op.ID
			}
			return ""
		}())
		if errors.Is(err, errPaused) {
			err = fmt.Errorf("ROUTES_DELETE_FAILED: удаление маршрутов набора %s приостановлено после ошибки", id)
		}
		if op != nil {
			op.Finish(err)
		}
		rel()
		if err != nil {
			if !strings.HasPrefix(err.Error(), "ROUTES_") {
				err = fmt.Errorf("ROUTES_DELETE_FAILED: %s: %w", id, err)
			}
			return err
		}
	}
	return nil
}

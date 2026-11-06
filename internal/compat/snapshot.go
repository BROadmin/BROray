package compat

import (
	"context"
	"fmt"
	"sync"
)

// SnapshotFunc produces a module's home snapshot `data` object (3.x
// bin/broray-home-snapshot refresh <module>): the same object the module's
// own status endpoint returns, including its `health` object
// ({schemaVersion:1,module,availability,severity,operational,consistent,
// actionRequired,freshness,reasons,facts,lastOperation}).
type SnapshotFunc func(ctx context.Context) (map[string]any, error)

var (
	snapMu    sync.RWMutex
	producers = map[string]SnapshotFunc{}
)

// RegisterSnapshot registers the producer of a home module snapshot.
// Modules: xray keenetic servers subscriptions dns routes broray.
func RegisterSnapshot(module string, fn SnapshotFunc) {
	snapMu.Lock()
	defer snapMu.Unlock()
	producers[module] = fn
}

// SnapshotProducer returns a registered producer.
func SnapshotProducer(module string) (SnapshotFunc, bool) {
	snapMu.RLock()
	defer snapMu.RUnlock()
	fn, ok := producers[module]
	return fn, ok
}

// AreaNames lists the registered areas (diagnostics report: the 3.5.0
// counterpart of the 3.x background services is the in-process areas).
func AreaNames() []string {
	areasMu.Lock()
	defer areasMu.Unlock()
	out := make([]string, 0, len(areas))
	for _, a := range areas {
		out = append(out, a.name)
	}
	return out
}

// UninstallStep removes the Keenetic objects an area owns (by its own
// receipts) during BROray uninstall. Any error aborts the uninstall before
// anything is deleted locally. Stages, in execution order: "routes", "dot",
// "publish" (KeenDNS publication), "keenetic" (the managed ProxyN). A step
// with nothing to remove returns nil.
type UninstallStep func(ctx context.Context) error

var (
	uninstallMu    sync.Mutex
	uninstallSteps = map[string][]UninstallStep{}
)

// RegisterUninstallStep adds an area's cleanup step to an uninstall stage.
func RegisterUninstallStep(stage string, fn UninstallStep) {
	uninstallMu.Lock()
	defer uninstallMu.Unlock()
	uninstallSteps[stage] = append(uninstallSteps[stage], fn)
}

// UninstallSteps returns the steps registered for a stage.
func UninstallSteps(stage string) []UninstallStep {
	uninstallMu.Lock()
	defer uninstallMu.Unlock()
	return append([]UninstallStep(nil), uninstallSteps[stage]...)
}

// UninstallStages is the order the uninstall runs area cleanup steps.
var UninstallStages = []string{"routes", "dot", "publish", "keenetic"}

// RunUninstallSteps runs every registered step of every stage in order and
// stops at the first failure.
func RunUninstallSteps(ctx context.Context) error {
	ctx = WithFence(ctx) // CLI: the daemon is stopped, nothing else runs
	for _, st := range UninstallStages {
		for _, fn := range UninstallSteps(st) {
			if err := fn(ctx); err != nil {
				return fmt.Errorf("%s: %w", st, err)
			}
		}
	}
	return nil
}

package routes

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
)

var errEmptyConfig = errors.New("ROUTES_CONFIG_RUNNING_CONFIG_EMPTY")

const managedMetric = 1200

// engine is the routes area runtime: 3.x /opt/broray/routes/* files, the
// Keenetic transport, the global fence and the operations registry.
type engine struct {
	d       *compat.Deps
	root    string // <data>/routes
	data    string // <data> (config/interface.json)
	T       keenetic.Transport
	lock    *compat.GlobalLock
	queue   *compat.Coordinator
	log     *log.Logger
	ctx     context.Context
	http    *http.Client
	rawBase string
	apiBase string // GitHub API of the scheduled catalog check

	schedMu      sync.Mutex // catalog-schedule-status.json read-modify-write
	schedRetryAt time.Time  // in-memory backoff when status cannot be persisted

	cacheMu sync.Mutex
	cache   *rcSnapshot
	cacheAt time.Time

	mu                sync.Mutex
	active            map[string]bool            // bundles with a running progress owned by this process
	stops             map[string]*routeStopState // operation-bound stop requests for this process
	rollbackUncertain atomic.Bool
	markerWrite       func(string, any) error
	txMu              sync.Mutex
	liveTransactions  map[string]bool

	importMu sync.Mutex // the 3.x routes/locks/operation.lock resource lease
}

// Tunables (tests shorten them).
var (
	verifyBatchSize  = 16
	convergeAttempts = 12
	sleepFn          = func(ctx context.Context, d time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}
	saveFn = keenetic.Save
)

// RawBase is the catalog download host (dev builds may point it at a local mirror).
var RawBase = "https://raw.githubusercontent.com"

func newEngine(d *compat.Deps) *engine {
	e := &engine{
		d: d, root: d.Path("routes"), data: d.Path(), lock: d.Lock, queue: d.Queue, log: d.Log, ctx: d.Ctx,
		rawBase: RawBase, apiBase: GitHubAPI, active: map[string]bool{}, stops: map[string]*routeStopState{},
		markerWrite:      writeObj,
		liveTransactions: map[string]bool{},
	}
	if e.log == nil {
		e.log = log.New(io.Discard, "", 0)
	}
	if e.ctx == nil {
		e.ctx = context.Background()
	}
	if d.App != nil {
		e.T = d.App.T
		e.http = d.App.HTTP
	}
	if e.http == nil {
		e.http = &http.Client{Timeout: 25 * time.Second}
	}
	return e
}

// ---------- paths ----------

func (e *engine) p(parts ...string) string {
	return filepath.Join(append([]string{e.root}, parts...)...)
}
func (e *engine) configFile() string            { return e.p("config.json") }
func (e *engine) bundlesFile() string           { return e.p("bundles.json") }
func (e *engine) customFile() string            { return e.p("custom.json") }
func (e *engine) stateFile(id string) string    { return e.p("state", id+".json") }
func (e *engine) manifestFile(id string) string { return e.p("manifests", id+".json") }
func (e *engine) registryFile(id string) string {
	return e.p("installed", "bundles", id+".json")
}
func (e *engine) globalRegistryFile() string     { return e.p("installed", "routes.json") }
func (e *engine) catalogDir(id string) string    { return e.p("catalog", id) }
func (e *engine) progressFile(id string) string  { return e.p("operations", id+".json") }
func (e *engine) counterFile(id string) string   { return e.p("operations", id+".counter") }
func (e *engine) preflightFile(id string) string { return e.p("preflight", id+".json") }
func (e *engine) rollbackMarker() string         { return e.p("rollback-required.json") }
func (e *engine) previewsDir() string            { return e.p("tmp", "user-previews") }
func (e *engine) interfaceReceipt() string       { return filepath.Join(e.data, "config", "interface.json") }

// ---------- seeding (3.x state-seed + routes-runtime-repair) ----------

func seedRoutesConfig() obj {
	return obj{
		"schemaVersion": 1, "managedInterface": "Proxy0", "routeComment": "BROray",
		"routeIdentity": toAny([]string{"family", "network", "prefix", "interface", "gateway", "metric", "automatic", "exclusive"}),
		"ownershipPolicy": obj{"adoptExistingRoutes": false, "modifyExternalRoutes": false, "deleteExternalRoutes": false,
			"touchOtherInterfaces": false, "deleteOnlyExactManagedMatch": true, "restoreMissingRoutesAutomatically": false},
		"managedMetric": managedMetric,
	}
}

func seedState(id string) obj {
	return obj{"schemaVersion": 1, "bundleId": id, "status": "not_checked", "availableVersion": nil, "downloadedVersion": nil,
		"installedVersion": nil, "routeCount": 0, "lastCheckedAt": nil, "lastVerifiedAt": nil, "lastDownloadedAt": nil,
		"lastExportedAt": nil, "lastDeletedAt": nil, "lastError": nil, "checkResult": nil, "verifyResult": nil,
		"downloadResult": nil, "exportBuild": nil, "preflight": nil, "exportResult": nil, "deleteResult": nil,
		"updatedAt": stamp()}
}

func seedRegistry(id, iface string) obj {
	return obj{"schemaVersion": 1, "bundleId": id, "installedVersion": nil, "routeKeys": []any{}, "managedRouteKeys": []any{},
		"externalRouteKeys": []any{}, "targetInterface": iface, "managedMetric": managedMetric, "installedAt": nil,
		"removedAt": nil, "updatedAt": stamp()}
}

// prepare creates the runtime layout and seeds missing files. Existing
// files are never replaced, except the built-in manifests whose
// targetInterface follows routes/config.json (3.x install/repair).
func (e *engine) prepare() error {
	for _, dir := range []string{"", "state", "manifests", "catalog", "installed/bundles", "operations", "preflight", "backup", "tmp", "transactions"} {
		if err := platform.MkdirAllDurable(e.p(dir), 0o700); err != nil {
			return err
		}
	}
	if !fileExists(e.configFile()) {
		if err := writeObj(e.configFile(), seedRoutesConfig()); err != nil {
			return err
		}
	}
	iface := e.managedInterface()
	if iface == "" {
		iface = "Proxy0"
	}
	if !fileExists(e.bundlesFile()) {
		if err := writeObj(e.bundlesFile(), obj{"schemaVersion": 1, "bundles": toAny(builtinIDs())}); err != nil {
			return err
		}
	} else if b, ok := readObj(e.bundlesFile()); ok {
		ids := strList(b["bundles"])
		have := map[string]bool{}
		for _, id := range ids {
			have[id] = true
		}
		changed := false
		for _, id := range builtinIDs() {
			if !have[id] {
				ids = append(ids, id)
				changed = true
			}
		}
		if changed {
			b["bundles"] = toAny(ids)
			_ = writeObj(e.bundlesFile(), b)
		}
	}
	for _, b := range builtins {
		mf := b.manifestFor(iface)
		if cur, ok := readObj(e.manifestFile(b.ID)); !ok || !sameJSON(cur, mf) {
			if err := writeObj(e.manifestFile(b.ID), mf); err != nil {
				return err
			}
		}
		if !fileExists(e.stateFile(b.ID)) {
			if err := writeObj(e.stateFile(b.ID), seedState(b.ID)); err != nil {
				return err
			}
		}
		if !fileExists(e.registryFile(b.ID)) {
			if err := writeObj(e.registryFile(b.ID), seedRegistry(b.ID, iface)); err != nil {
				return err
			}
		}
	}
	// 3.x package-setup.sh repair_route_runtime ends with
	// broray_routes_sync_ensure_global_registry: the shared registry exists
	// from installation on, so the first summary works before any plan.
	// Best-effort: when it cannot be built (inconsistent receipts) the
	// summary reports ROUTES_GLOBAL_REGISTRY_UNAVAILABLE, but the area —
	// above all its uninstall step — must stay available.
	if !fileExists(e.globalRegistryFile()) && !e.ensureGlobalRegistry(iface) {
		e.log.Printf("routes: warning: ROUTES_GLOBAL_REGISTRY_UNAVAILABLE: не удалось создать общий реестр маршрутов")
	}
	return nil
}

func sameJSON(a, b any) bool {
	x, err1 := compat.Canonical(a)
	y, err2 := compat.Canonical(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// ---------- configuration ----------

var reProxy = regexp.MustCompile(`^Proxy[0-9]+$`)

func (e *engine) routesConfig() (obj, bool) { return readObj(e.configFile()) }

// managedInterface returns routes/config.json managedInterface ("" if unset).
func (e *engine) managedInterface() string {
	c, _ := e.routesConfig()
	return str(c, "managedInterface")
}

func (e *engine) managedMetricConfig() int {
	c, _ := e.routesConfig()
	if c == nil {
		return managedMetric
	}
	n, ok := intOf(c["managedMetric"])
	if !ok || n < 0 {
		return managedMetric
	}
	return n
}

// interfaceDisplay is config/interface.json .description when it is
// exactly "BROray" or "BROray - …", else "BROray".
func (e *engine) interfaceDisplay() string {
	p := e.interfaceReceipt()
	if st, err := os.Lstat(p); err != nil || !st.Mode().IsRegular() {
		return "BROray"
	}
	r, ok := readObj(p)
	if !ok {
		return "BROray"
	}
	d := str(r, "description")
	if d == "BROray" || len(d) > len("BROray - ") && d[:len("BROray - ")] == "BROray - " {
		return d
	}
	return "BROray"
}

// policyValid is the full routes/config.json policy check
// (routes-router-sync.sh:1107-1124).
func (e *engine) policyValid(iface string) bool {
	c, ok := e.routesConfig()
	if !ok || intOr(c["schemaVersion"], 0) != 1 || str(c, "managedInterface") != iface || !reProxy.MatchString(iface) {
		return false
	}
	if intOr(c["managedMetric"], 0) != managedMetric || str(c, "routeComment") != "BROray" {
		return false
	}
	pol := sub(c, "ownershipPolicy")
	return pol != nil && pol["adoptExistingRoutes"] == false && pol["modifyExternalRoutes"] == false &&
		pol["deleteExternalRoutes"] == false && pol["touchOtherInterfaces"] == false && pol["deleteOnlyExactManagedMatch"] == true
}

// bundleListed reports whether id is in routes/bundles.json.
func (e *engine) bundleListed(id string) bool {
	b, ok := readObj(e.bundlesFile())
	if !ok || intOr(b["schemaVersion"], 0) != 1 {
		return false
	}
	for _, x := range strList(b["bundles"]) {
		if x == id {
			return true
		}
	}
	return false
}

// customEntry returns the routes/custom.json entry for id.
func (e *engine) customEntry(id string) (obj, bool) {
	c, ok := readObj(e.customFile())
	if !ok {
		return nil, false
	}
	for _, x := range anyList(c["bundles"]) {
		if m, ok := x.(obj); ok && str(m, "id") == id {
			return m, true
		}
	}
	return nil, false
}

func (e *engine) customIDs() []string {
	c, ok := readObj(e.customFile())
	if !ok || intOr(c["schemaVersion"], 0) != 1 {
		return nil
	}
	var out []string
	for _, x := range anyList(c["bundles"]) {
		if m, ok := x.(obj); ok {
			if id := str(m, "id"); validBundleID(id) {
				out = append(out, id)
			}
		}
	}
	return out
}

func (e *engine) bundleName(id string) string {
	if m, ok := e.customEntry(id); ok {
		if n := str(m, "name"); n != "" {
			return n
		}
		return id
	}
	if mf, ok := readObj(e.manifestFile(id)); ok {
		if n := str(mf, "name"); n != "" {
			return n
		}
		if n := str(mf, "id"); n != "" {
			return n
		}
	}
	return id
}

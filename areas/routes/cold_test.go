package routes

import (
	"os"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

// DEV4_ROUTER_ROUTES_GLOBAL_REGISTRY_UNAVAILABLE: on a clean install the
// summary must work before the first plan (3.x package-setup.sh
// repair_route_runtime → broray_routes_sync_ensure_global_registry).
func TestColdInstallSummaryHasGlobalRegistry(t *testing.T) {
	env := newEnv(t)
	if _, err := os.Stat(env.e.globalRegistryFile()); err != nil {
		t.Fatalf("installed/routes.json not created by prepare: %v", err)
	}
	if _, serr := env.e.summaryAll(); serr != nil {
		t.Fatalf("cold summary: %s %s", serr.code, serr.message)
	}
}

// A global registry that cannot be rebuilt (inconsistent receipts) must not
// disable the area: handlers and the uninstall step stay available and the
// summary reports the registry problem.
func TestSetupSurvivesUnavailableGlobalRegistry(t *testing.T) {
	env := newEnv(t)
	id := builtins[0].ID
	reg := seedRegistry(id, "Proxy0")
	reg["managedRouteKeys"] = []any{routeKey("1.2.3.0", 24, "Proxy0")}
	reg["installedVersion"] = obj{"contentSha256": "x"}
	if err := writeObj(env.e.registryFile(id), reg); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(env.e.globalRegistryFile())
	if err := newEngine(env.d).prepare(); err != nil {
		t.Fatalf("prepare must be best-effort for the global registry: %v", err)
	}
	d := *env.d
	d.Maintenance = true // no schedule goroutine
	t.Cleanup(func() { setCurrent(env.e) })
	if err := setup(&d); err != nil {
		t.Fatalf("setup: %v", err)
	}
	e := current()
	if e == env.e {
		t.Fatal("setup did not install its engine")
	}
	if _, serr := e.summaryAll(); serr == nil || serr.code != "ROUTES_GLOBAL_REGISTRY_UNAVAILABLE" {
		t.Fatalf("summary: %v", serr)
	}
	if got := e.installedBundles(); len(got) != 1 || got[0] != id {
		t.Fatalf("uninstall would miss the installed set: %v", got)
	}
}

// Even a layout error keeps the engine current, so uninstall can run.
func TestSetupRegistersOnLayoutError(t *testing.T) {
	env := newEnv(t)
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(root.Data() + "/routes")
	if err := os.WriteFile(root.Data()+"/routes", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := *env.d
	d.Root, d.Maintenance = root, true
	t.Cleanup(func() { setCurrent(env.e) })
	_ = setup(&d) // the error is logged by StartAreas
	if current() == env.e || current().root != root.Data()+"/routes" {
		t.Fatal("engine not installed after a prepare error")
	}
}

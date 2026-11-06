// Package platform owns the on-device layout, durable file writes and the
// daemon's ownership lock. Nothing here talks to KeeneticOS.
package platform

import (
	"os"
	"path/filepath"
)

// Root is the installation root. The layout follows BRORAY-3.5.0-UPDATER-DESIGN §5
// and keeps the 3.x location of the Xray binary (runtime/xray).
type Root struct{ Dir string }

func DefaultRoot() Root {
	if v := os.Getenv("BRORAY_ROOT"); v != "" {
		return Root{Dir: v}
	}
	return Root{Dir: "/opt/broray"}
}

func (r Root) Launcher() string   { return filepath.Join(r.Dir, "launcher") }
func (r Root) Versions() string   { return filepath.Join(r.Dir, "versions") }
func (r Root) Current() string    { return filepath.Join(r.Dir, "current") }
func (r Root) Config() string     { return filepath.Join(r.Dir, "config") }
func (r Root) Data() string       { return filepath.Join(r.Dir, "data") }
func (r Root) State() string      { return filepath.Join(r.Dir, "state") }
func (r Root) Run() string        { return filepath.Join(r.Dir, "run") }
func (r Root) Logs() string       { return filepath.Join(r.Dir, "logs") }
func (r Root) Tmp() string        { return filepath.Join(r.Dir, "tmp") }
func (r Root) Runtime() string    { return filepath.Join(r.Dir, "runtime") }
func (r Root) XrayBinary() string { return filepath.Join(r.Runtime(), "xray") }
func (r Root) XrayConfig() string { return filepath.Join(r.Config(), "config.json") }

func (r Root) ConfigFile() string    { return filepath.Join(r.Config(), "config.v1.json") }
func (r Root) Servers() string       { return filepath.Join(r.Data(), "servers") }
func (r Root) Subscriptions() string { return filepath.Join(r.Data(), "subscriptions") }
func (r Root) Routes() string        { return filepath.Join(r.Data(), "routes") }
func (r Root) RouteCatalog() string  { return filepath.Join(r.Routes(), "catalog") }
func (r Root) RouteUser() string     { return filepath.Join(r.Routes(), "user") }
func (r Root) Receipts() string      { return filepath.Join(r.State(), "keenetic") }
func (r Root) Operations() string    { return filepath.Join(r.State(), "operations.jsonl") }
func (r Root) DaemonLock() string    { return filepath.Join(r.Run(), "brorayd.lock") }
func (r Root) DaemonSocket() string  { return filepath.Join(r.Run(), "brorayd.sock") }

// Update/launcher layout (docs/BRORAY-3.5.0-UPDATER-DESIGN.md §5-§7).
func (r Root) LauncherLock() string  { return filepath.Join(r.Run(), "launcher.lock") }  // held by `broray launcher apply`
func (r Root) InstallerLock() string { return filepath.Join(r.Run(), "installer.lock") } // held by `installer apply` and `ctl recover`
func (r Root) UpdateJournal() string { return filepath.Join(r.State(), "update-journal.json") }
func (r Root) UpdateState() string   { return filepath.Join(r.State(), "update") } // per-operation progress, init-script backup
func (r Root) ControlToken() string  { return filepath.Join(r.State(), "control.token") }
func (r Root) ServerRouteState() string {
	return filepath.Join(r.Run(), "xray-server-route")
}

// Dirs lists every directory the daemon expects to exist with its mode.
func (r Root) Dirs() map[string]os.FileMode {
	return map[string]os.FileMode{
		r.Dir: 0o700, r.Versions(): 0o700, r.Config(): 0o700, r.Data(): 0o700,
		r.State(): 0o700, r.Run(): 0o700, r.Logs(): 0o700, r.Tmp(): 0o700,
		r.Runtime(): 0o755, r.Servers(): 0o700, r.Subscriptions(): 0o700,
		r.Routes(): 0o700, r.RouteCatalog(): 0o700, r.RouteUser(): 0o700, r.Receipts(): 0o700,
	}
}

func (r Root) EnsureDirs() error {
	for dir, mode := range r.Dirs() {
		if err := os.MkdirAll(dir, mode); err != nil {
			return err
		}
	}
	return nil
}

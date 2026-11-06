// Package compat is the shared foundation of the 3.x-compatible areas
// (areas/*): dependencies, area registration, 3.x time and JSON formats,
// the global operation lock and the background-operations coordinator.
//
// Data layout rule for every area: a 3.x path /opt/broray/<X> becomes
// Deps.Path("<X>") = <root>/data/<X>; RAM state of 3.x (/tmp/broray-*,
// /opt/var/run/broray/*) becomes Deps.RunPath(...). The Xray binary and
// config keep their 3.5.0 locations (Root.XrayBinary, Root.XrayConfig).
package compat

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/platform"
)

// Deps is what an area gets at setup.
type Deps struct {
	Root  platform.Root
	App   *app.App // Cfg, T (Keenetic transport), K (adapter), Xray process, HTTP client
	Lock  *GlobalLock
	Queue *Coordinator
	Log   *log.Logger
	Ctx   context.Context // cancelled at daemon shutdown; areas start loops with it
	Svc   *Services       // cross-area calls (services.go)
	// Maintenance is true when areas are set up outside the daemon (CLI
	// uninstall): register endpoints, services and uninstall steps, but
	// start NO goroutines, timers, Xray or Keenetic writes in Setup.
	Maintenance bool
}

// Path maps a 3.x /opt/broray/<parts...> path into the 3.5.0 data dir.
func (d *Deps) Path(parts ...string) string {
	return filepath.Join(append([]string{d.Root.Data()}, parts...)...)
}

// RunPath maps 3.x RAM state into <root>/run.
func (d *Deps) RunPath(parts ...string) string {
	return filepath.Join(append([]string{d.Root.Run()}, parts...)...)
}

// Setup is an area's entry point: register CGI endpoints (internal/cgi),
// queue actions (Deps.Queue.Define) and start background loops on d.Ctx.
type Setup func(d *Deps) error

type area struct {
	name  string
	order int
	setup Setup
}

var (
	areasMu sync.Mutex
	areas   []area
)

// Area registers an area from its package init(). Lower order runs first.
func Area(name string, order int, setup Setup) {
	areasMu.Lock()
	defer areasMu.Unlock()
	areas = append(areas, area{name, order, setup})
}

// StartAreas runs every registered area's Setup in order. Ops runs first so
// ambiguous backup recovery stops startup before any other area can mutate.
// Other area setup failures remain best-effort.
func StartAreas(d *Deps) error {
	if d.Lock != nil && d.Lock.Ambiguous() {
		return fmt.Errorf("ambiguous global operation lock record")
	}
	areasMu.Lock()
	list := append([]area(nil), areas...)
	areasMu.Unlock()
	sort.SliceStable(list, func(i, j int) bool { return list[i].order < list[j].order })
	for _, a := range list {
		if err := a.setup(d); err != nil {
			d.Log.Printf("area %s: setup failed: %v", a.name, err)
			if a.name == "ops" {
				return fmt.Errorf("critical area %s: %w", a.name, err)
			}
		}
	}
	return nil
}

// NewDeps builds the shared dependencies.
func NewDeps(ctx context.Context, a *app.App) *Deps {
	d := &Deps{Root: a.Root, App: a, Log: a.Log, Ctx: ctx, Svc: &Services{}}
	d.Lock = NewGlobalLock(d.RunPath("global-operation.json"), a.Root.LauncherLock(), a.Root.InstallerLock())
	d.Queue = NewCoordinator(d)
	return d
}

// ---------- time formats ----------

// Stamp is 3.x `date '+%Y-%m-%dT%H:%M:%S%z'` (local time, offset without colon).
func Stamp(t time.Time) string { return t.Format("2006-01-02T15:04:05-0700") }

// StampUTC is 3.x `date -u '+%Y-%m-%dT%H:%M:%SZ'` (operations, journal).
func StampUTC(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// Now is Stamp(time.Now()).
func Now() string { return Stamp(time.Now()) }

// ---------- hashing / ids ----------

// SHA256Hex returns the lowercase hex sha256 of b.
func SHA256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// RandHex returns n random bytes as lowercase hex.
func RandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// OperationID returns a 3.x-shaped id op-<YYYYmmddHHMMSS>-<pid>-<12 hex>.
func OperationID() string {
	return "op-" + time.Now().UTC().Format("20060102150405") + "-" + itoa(os.Getpid()) + "-" + RandHex(6)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

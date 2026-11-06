// Package subscriptions ports the 3.x "subscriptions" WebUI area (contract:
// docs/contracts/subscriptions.md): subscription records, download with
// address pinning, payload detection (links, Base64, Xray JSON), provider
// information, server catalog sync with identity continuity, enable/disable,
// the queue action subscriptions:refresh and the automatic scheduler.
package subscriptions

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func init() { compat.Area("subscriptions", 50, setup) }

type svc struct {
	d        *compat.Deps
	logMu    sync.Mutex
	progMu   sync.Mutex
	progress map[string][2]int
	wg       sync.WaitGroup
}

var (
	registerOnce sync.Once
	current      atomic.Pointer[svc]
)

func cur() *svc { return current.Load() }

func newSvc(d *compat.Deps) *svc { return &svc{d: d, progress: map[string][2]int{}} }

func setup(d *compat.Deps) error {
	s := newSvc(d)
	current.Store(s)
	registerOnce.Do(register)
	s.defineActions()
	registerBackup(s)
	compat.RegisterSnapshot("subscriptions", func(ctx context.Context) (map[string]any, error) { return s.summary(), nil })
	if d.Maintenance {
		return nil
	}
	// 3.x package setup created these; deleted-subscriptions is preserved but never used (no trash in 3.x).
	for _, dir := range []string{s.subDir(), s.runDir(), d.Path("deleted-subscriptions")} {
		_ = os.MkdirAll(dir, 0o700)
	}
	s.recoverStale()
	go s.schedulerLoop(d.Ctx)
	return nil
}

// schedulerInterval is BRORAY_SUB_SCHEDULER_INTERVAL (bin/broray-subscription-scheduler).
var schedulerInterval = 60 * time.Second

func (s *svc) schedulerLoop(ctx context.Context) {
	t := time.NewTicker(schedulerInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.enqueueDue()
		}
	}
}

// enqueueDue is broray_subscription_enqueue_due: every enabled, auto-updated
// record whose nextUpdateEpoch has passed is queued as SUBSCRIPTION_AUTO
// (priority 4) with the record hash as context; nothing while automation is paused.
func (s *svc) enqueueDue() int {
	if s.d.Queue.Paused() {
		return 0
	}
	n := 0
	nowEpoch := now().Unix()
	for _, id := range s.recordIDs() {
		rec, raw, err := s.readRecord(id)
		if err != nil || rec["enabled"] != true || rec["autoUpdateEnabled"] != true {
			continue
		}
		next, ok := num(rec["nextUpdateEpoch"])
		if !ok || next <= 0 || next > float64(nowEpoch) {
			continue
		}
		if validID(id) != nil {
			return n
		}
		if _, err := s.d.Queue.Enqueue(actionRefresh, id, "SUBSCRIPTION_AUTO", compat.SHA256Hex(raw)); err != nil {
			s.d.Log.Printf("subscriptions: enqueue %s: %v", id, err)
			continue
		}
		n++
	}
	return n
}

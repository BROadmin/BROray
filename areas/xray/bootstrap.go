package xray

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	mxray "github.com/BROadmin/broray/modules/xray"
)

// bootstrapRetry are the pauses between failed first-install attempts
// (WAN may come up after the daemon at router start); the last repeats.
var bootstrapRetry = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}

// bootstrapInstall replaces the Xray binary 3.x shipped inside its package:
// when runtime/xray is missing, the daemon installs the newest version
// pinned in this build (archive sha256 verified) as a SYSTEM background
// operation, retrying until it succeeds. The 3.x Xray page needs an
// installed version to offer updates, so this runs without user action.
func (s *service) bootstrapInstall(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		if s.maintenanceBlocksAuto() {
			return
		}
		wait := bootstrapRetry[min(attempt, len(bootstrapRetry)-1)]
		if _, err := os.Stat(s.d.Root.XrayBinary()); err == nil {
			return
		}
		tags := mxray.PinnedTags()
		if len(tags) == 0 {
			return
		}
		_, sum, ok := mxray.PinnedDigest(tags[0])
		if !ok {
			s.d.Log.Printf("xray: no pinned archive for this architecture; first install skipped")
			return
		}
		if release, ok := s.d.Lock.TryAcquire("xray", "xray:install", "xray"); ok {
			if s.maintenanceBlocksAuto() {
				release()
				return
			}
			op := s.d.Queue.StartOperation("", "xray_maintenance", "xray:install", "SYSTEM", "cooperative", "")
			work := filepath.Join(s.opsDir(), op.ID)
			if os.MkdirAll(work, 0o700) == nil {
				_ = os.WriteFile(filepath.Join(work, "operation-id"), []byte(op.ID+"\n"), 0o600)
				_ = compat.WriteJSON(s.pointerPath(), map[string]any{"backgroundOperationId": op.ID, "operation": "install",
					"operationRunning": true, "running": true, "success": nil, "result": nil, "error": nil})
				jctx, cancel := context.WithCancel(ctx)
				op.SetCancel(cancel)
				s.runJob(jctx, op, work, "bootstrap", installRequest{Tag: tags[0], ArchiveSHA256: sum})
				cancel()
			} else {
				op.Finish(os.ErrPermission)
			}
			release()
			if _, err := os.Stat(s.d.Root.XrayBinary()); err == nil {
				s.d.Log.Printf("xray: first install of %s done", tags[0])
				s.bootStart(ctx)
				return
			}
			s.d.Log.Printf("xray: first install of %s failed; retry in %s", tags[0], wait)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

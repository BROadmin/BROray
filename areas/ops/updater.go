package ops

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// BROray page operations (update / reinstall / uninstall) share one
// "last operation" record, readable through update-status.cgi like the 3.x
// pointer /opt/var/lib/broray/last-operation. It is persisted to
// state/broray-operation.json so an interrupted operation is reported after
// a daemon restart instead of disappearing.
//
// Update and reinstall: BROray 3.5.0 updates through the launcher of the
// installed version + the installer of the target version
// (docs/BRORAY-3.5.0-UPDATER-DESIGN.md, docs/RELEASE-CHANNEL.md); see
// update.go for the check, the start and the progress watcher.

type pageOps struct {
	a   *area
	mu  sync.Mutex
	cur *pageOp
}

type pageOp struct {
	OperationID string    `json:"operationId"`
	Operation   string    `json:"operation"`
	State       string    `json:"state"` // queued|running|success|error
	Stage       string    `json:"stage"`
	Progress    int       `json:"progress"`
	Message     string    `json:"message"`
	Error       string    `json:"error,omitempty"`
	Running     bool      `json:"running"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Log         []string  `json:"log"`

	// Update/reinstall/restore run in a detached launcher whose JSON
	// progress lines go to ProgressFile; WatchLock is held while it runs.
	// A restarted daemon resumes reading at ProgressOffset (not public).
	ProgressFile   string `json:"progressFile,omitempty"`
	ProgressOffset int64  `json:"progressOffset,omitempty"`
	WatchLock      string `json:"watchLock,omitempty"`
}

const pageLogLines = 80

func newPageOps(a *area) *pageOps {
	p := &pageOps{a: a}
	var op pageOp
	if err := compat.ReadJSON(p.file(), &op); err == nil && op.OperationID != "" {
		if op.Running && op.ProgressFile != "" {
			// An update stops this daemon on purpose and is still being
			// finished by the detached launcher: keep it running; setup
			// resumes the progress watcher (resumeWatch).
			p.cur = &op
			return p
		}
		if op.Running {
			// The daemon stopped while the operation ran (an uninstall
			// hands off to the installer, which stops the daemon first).
			op.Running, op.State, op.Progress = false, "error", 100
			op.Message = "Операция " + op.OperationID + " не подтверждена: BROray был перезапущен до её завершения. Проверьте состояние установки."
			op.Error = op.Message
			op.UpdatedAt = time.Now()
		}
		p.cur = &op
	}
	return p
}

func (p *pageOps) file() string { return p.a.d.Path("state", "broray-operation.json") }

// running reports whether an update/reinstall/uninstall is in progress.
func (p *pageOps) running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cur != nil && p.cur.Running
}

// begin publishes a new running operation; false if one is running.
func (p *pageOps) begin(op *pageOp) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur != nil && p.cur.Running {
		return false
	}
	op.UpdatedAt = p.a.now()
	p.cur = op
	p.saveLocked()
	return true
}

// update changes the state and appends a log line.
func (p *pageOps) update(fn func(op *pageOp), logLine string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil {
		return
	}
	fn(p.cur)
	p.cur.UpdatedAt = p.a.now()
	if logLine != "" {
		p.cur.Log = append(p.cur.Log, compat.StampUTC(p.cur.UpdatedAt)+"  "+logLine)
		if len(p.cur.Log) > pageLogLines {
			p.cur.Log = p.cur.Log[len(p.cur.Log)-pageLogLines:]
		}
	}
	p.saveLocked()
}

func (p *pageOps) saveLocked() {
	_ = os.MkdirAll(p.a.d.Path("state"), 0o700)
	_ = compat.WriteJSON(p.file(), p.cur)
}

func (op *pageOp) public() map[string]any {
	var e any
	if op.Error != "" {
		e = op.Error
	}
	return map[string]any{"ok": true, "operationId": op.OperationID, "operation": op.Operation, "state": op.State,
		"stage": op.Stage, "progress": op.Progress, "message": op.Message, "error": e, "running": op.Running,
		"updatedAt": compat.StampUTC(op.UpdatedAt)}
}

// snapshot returns a copy of the current operation (nil if none).
func (p *pageOps) snapshot() *pageOp {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil {
		return nil
	}
	c := *p.cur
	c.Log = nil
	return &c
}

// lastOperation is the info/health lastOperation object (nil if none).
func (p *pageOps) lastOperation() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil {
		return nil
	}
	return p.cur.public()
}

// status is update-status.cgi (status_output, upd.sh:459-494).
func (p *pageOps) status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil {
		return map[string]any{"schemaVersion": 1, "engine": "broray-installer/1", "durable": true, "ok": true, "operationId": nil,
			"operation": nil, "state": "idle", "stage": "idle", "progress": 0, "message": "Операции ещё не выполнялись.",
			"error": nil, "running": false, "mutationStarted": false, "rollbackPerformed": false, "logTail": "",
			"updatedAt": compat.StampUTC(p.a.now())}
	}
	out := p.cur.public()
	out["logTail"] = strings.Join(p.cur.Log, "\n")
	return out
}

// pageOperationID keeps the 3.x prefix and adds a random suffix to avoid
// reusing progress files for operations accepted within the same second.
func (a *area) pageOperationID(op string) string {
	return fmt.Sprintf("%s-%s-%d-%s", op, a.now().UTC().Format("20060102150405"), os.Getpid(), compat.RandHex(8))
}

func registerBROray() {
	big := []cgi.Opt{cgi.POST(), cgi.TooLarge(tooLarge413)}
	cgi.RegisterRaw("/api/broray/info.cgi", func(c *cgi.Req) {
		writeC(c.W, 200, current().info(c.R.Context(), true))
	})
	cgi.RegisterRaw("/api/broray/update-status.cgi", func(c *cgi.Req) {
		writeC(c.W, 200, current().page.status())
	})
	cgi.RegisterRaw("/api/broray/update-check.cgi", func(c *cgi.Req) {
		if !requireBROrayOrigin(c) {
			return
		}
		current().updateCheck(c)
	}, big...)
	cgi.RegisterRaw("/api/broray/update-start.cgi", func(c *cgi.Req) {
		if !requireBROrayOrigin(c) {
			return
		}
		current().updateStart(c, "update")
	}, big...)
	cgi.RegisterRaw("/api/broray/reinstall.cgi", func(c *cgi.Req) {
		if !requireBROrayOrigin(c) {
			return
		}
		current().updateStart(c, "reinstall")
	}, big...)
	cgi.RegisterRaw("/api/broray/restore.cgi", func(c *cgi.Req) {
		if !requireBROrayOrigin(c) {
			return
		}
		current().restore(c)
	}, big...)
	cgi.RegisterRaw("/api/broray/cleanup-plan.cgi", func(c *cgi.Req) {
		if !requireBROrayHeader(c) {
			return
		}
		current().cleanupPlanHandler(c)
	}, big...)
	cgi.RegisterRaw("/api/broray/cleanup.cgi", func(c *cgi.Req) {
		if !requireBROrayHeader(c) {
			return
		}
		current().cleanupHandler(c)
	}, big...)
	cgi.RegisterRaw("/api/broray/uninstall.cgi", func(c *cgi.Req) {
		if !requireBROrayHeader(c) {
			return
		}
		current().uninstallHandler(c)
	}, big...)
}

package compat

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Operation is one background operation as shown by the operations panel
// (operation_public, lib/operation-public.jq:39-52). Queued requests get one
// automatically; long direct operations (routes export/delete, Xray
// install, subscription legacy refresh, uninstall) create their own with
// StartOperation so they appear in the panel and the journal.
type Operation struct {
	c             *Coordinator
	mu            sync.Mutex
	ID            string
	Type          string
	ActionName    string
	Source        string
	State         string // starting|running|completed|failed|aborted|recovered
	PhaseName     string
	Revision      int
	Cancelability string // cooperative|protected
	cancelReq     bool
	cancelFn      context.CancelFunc
	StartedAt     time.Time
	UpdatedAt     time.Time
	FinishedAt    time.Time
	ErrorCode     string
	RequestID     string
	ResourceLocks []string
}

// StartOperation registers a running operation. id "" → a fresh op-… id.
// cancelability: "cooperative" (Cancel() cancels ctx given to SetCancel) or "protected".
func (c *Coordinator) StartOperation(id, typ, action, source, cancelability, requestID string) *Operation {
	c.mu.Lock()
	op := c.startOperationLocked(id, typ, action, source, cancelability, requestID)
	c.mu.Unlock()
	c.journal.Append(op, "started", "pending", "")
	return op
}

// startOperationLocked registers the operation while c.mu is held, so a queued
// request and its operation can become visible in the same critical section.
func (c *Coordinator) startOperationLocked(id, typ, action, source, cancelability, requestID string) *Operation {
	if id == "" {
		id = OperationID()
	}
	now := time.Now()
	op := &Operation{c: c, ID: id, Type: typ, ActionName: action, Source: source, State: "running", PhaseName: "starting",
		Revision: 1, Cancelability: cancelability, StartedAt: now, UpdatedAt: now, RequestID: requestID, ResourceLocks: []string{"global"}}
	c.ops = append(c.ops, op)
	c.trimOpsLocked()
	return op
}

func (c *Coordinator) trimOpsLocked() {
	finished := 0
	for i := len(c.ops) - 1; i >= 0; i-- {
		if c.ops[i].isFinished() {
			finished++
			if finished > receiptLimit {
				c.ops = append(c.ops[:i], c.ops[i+1:]...)
			}
		}
	}
}

func (o *Operation) isFinished() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return !o.FinishedAt.IsZero()
}

// Phase changes the phase (starting|working|checking|fetching|parsing|committing|switching|waiting|recovering|finished).
func (o *Operation) Phase(p string) {
	o.mu.Lock()
	if o.PhaseName == p {
		o.mu.Unlock()
		return
	}
	o.PhaseName, o.UpdatedAt = p, time.Now()
	o.Revision++
	o.mu.Unlock()
	o.c.journal.Append(o, "phase_changed", "pending", "")
}

// SetCancelability switches between cooperative and protected stages.
func (o *Operation) SetCancelability(v string) {
	o.mu.Lock()
	o.Cancelability = v
	o.mu.Unlock()
}

// SetCancel attaches the cancel func used by Cancel() for direct operations.
func (o *Operation) SetCancel(fn context.CancelFunc) {
	o.mu.Lock()
	o.cancelFn = fn
	o.mu.Unlock()
}

// CancelRequested reports whether a stop was requested.
func (o *Operation) CancelRequested() bool { o.mu.Lock(); defer o.mu.Unlock(); return o.cancelReq }

// Finish marks the operation completed (err nil), aborted (cancelled) or failed.
func (o *Operation) Finish(err error) {
	o.mu.Lock()
	if !o.FinishedAt.IsZero() {
		o.mu.Unlock()
		return
	}
	switch {
	case err == nil:
		o.State = "completed"
	case o.cancelReq || errors.Is(err, context.Canceled) || errors.Is(err, errCancelled):
		o.State = "aborted"
		o.ErrorCode = "CANCELLED"
	default:
		o.State = "failed"
		o.ErrorCode = codeOf(err)
	}
	o.PhaseName, o.FinishedAt, o.UpdatedAt = "finished", time.Now(), time.Now()
	o.Revision++
	state := o.State
	o.mu.Unlock()
	ev, res := "completed", "success"
	switch state {
	case "aborted":
		ev, res = "aborted", "cancelled"
	case "failed":
		ev, res = "failed", "failure"
	}
	o.c.journal.Append(o, ev, res, "")
}

// Public is operation_public (lib/operation-public.jq:39-52): every field is
// projected through the 3.x enums, so an area passing a free-form type,
// phase or error code can never leak it to the panel.
func (o *Operation) Public() map[string]any {
	o.mu.Lock()
	defer o.mu.Unlock()
	ts := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return StampUTC(t)
	}
	owner, reason := "ACTIVE", "identity_matches"
	if !o.FinishedAt.IsZero() {
		owner, reason = "FINISHED", "operation_finished"
	}
	typ := PublicType(o.Type)
	if typ == "unknown" {
		typ = PublicType(o.ActionName)
	}
	cancelability := enumOr(o.Cancelability, "protected", "cooperative", "protected")
	if typ == "route_operation" || routeAction(o.ActionName) {
		cancelability = "protected"
	}
	var ec any
	if o.ErrorCode != "" {
		if enumHas(o.ErrorCode, publicErrorCodes...) {
			ec = o.ErrorCode
		} else if o.State == "failed" {
			ec = "OPERATION_FAILED"
		}
	}
	locks := []string{}
	for _, l := range o.ResourceLocks {
		if enumHas(l, "global", "active-observer", "background-prepare") {
			locks = append(locks, l)
		}
	}
	return map[string]any{"operationId": PublicOperationID(o.ID), "type": typ, "source": PublicSource(o.Source),
		"state":   enumOr(o.State, "unknown", "starting", "running", "completed", "failed", "aborted", "recovered"),
		"phase":   enumOr(o.PhaseName, "unknown", publicPhases...),
		"running": o.FinishedAt.IsZero(), "revision": o.Revision, "cancelability": cancelability, "cancelRequested": o.cancelReq,
		"startedAt": ts(o.StartedAt), "updatedAt": ts(o.UpdatedAt), "finishedAt": ts(o.FinishedAt), "errorCode": ec,
		"ownerStatus": owner, "ownerReason": reason, "requestId": PublicRequestID(o.RequestID), "resourceLocks": locks}
}

var (
	publicPhases     = []string{"starting", "working", "checking", "fetching", "parsing", "committing", "switching", "waiting", "recovering", "finished"}
	publicErrorCodes = []string{"CANCELLED", "OPERATION_FAILED", "OWNER_DISAPPEARED", "OWNER_CHANGED", "OPERATION_BUSY", "DOMAIN_OPERATION_BUSY",
		"STATE_UNAVAILABLE", "AUTOMATION_PAUSED", "CHILDREN_UNCONFIRMED", "CANCEL_NOT_SUPPORTED", "OWNER_UNCONFIRMED", "OWNER_PUBLICATION_FAILED"}
	publicTypes   = []string{"subscription_update", "server_operation", "auto_switch", "xray_maintenance", "dns_operation", "router_operation", "route_operation", "updater_maintenance"}
	publicSources = []string{"USER", "SCHEDULER", "SUBSCRIPTION_AUTO", "SERVER_CHECK_AUTO", "AUTO_SWITCH", "UPDATER", "SYSTEM_RECOVERY"}
	reOpID        = regexp.MustCompile(`^op-[0-9]{14}-([0-9]{1,10})-[0-9a-f]{12}$`)
	reOpQueueID   = regexp.MustCompile(`^op-q-[0-9a-f]{32}$`)
	reRequestID   = regexp.MustCompile(`^q-[0-9a-f]{32}$`)
)

func enumHas(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}

func enumOr(v, fallback string, values ...string) string {
	if enumHas(v, values...) {
		return v
	}
	return fallback
}

// ValidOperationID is operation_id of operation-public.jq:10-17.
func ValidOperationID(id string) bool {
	if reOpQueueID.MatchString(id) {
		return true
	}
	m := reOpID.FindStringSubmatch(id)
	if m == nil {
		return false
	}
	pid, err := strconv.ParseInt(m[1], 10, 64)
	return err == nil && pid > 1 && pid <= 2147483647
}

// PublicOperationID returns id or nil when it is not a 3.x operation id.
func PublicOperationID(id string) any {
	if ValidOperationID(id) {
		return id
	}
	return nil
}

// PublicRequestID returns a q-<32 hex> id or nil.
func PublicRequestID(id string) any {
	if reRequestID.MatchString(id) {
		return id
	}
	return nil
}

// PublicSource is the source enum (UNKNOWN fallback).
func PublicSource(s string) string { return enumOr(s, "UNKNOWN", publicSources...) }

func routeAction(s string) bool {
	return strings.HasPrefix(s, "custom:") || strings.HasPrefix(s, "preflight:") ||
		enumHas(s, "check", "download", "build-export", "verify", "plan", "export", "delete", "resume", "route_operation")
}

// PublicType is operation_type: an action string ("servers:quality") or an
// already public type maps to the public type enum, else "unknown".
func PublicType(s string) string {
	switch {
	case strings.HasPrefix(s, "subscriptions:"):
		return "subscription_update"
	case strings.HasPrefix(s, "servers:"):
		return "server_operation"
	case s == "auto-switch":
		return "auto_switch"
	case s == "system:platform-preflight":
		return "updater_maintenance"
	case strings.HasPrefix(s, "xray:"):
		return "xray_maintenance"
	case strings.HasPrefix(s, "dot:"):
		return "dns_operation"
	case strings.HasPrefix(s, "keenetic:"):
		return "router_operation"
	case routeAction(s):
		return "route_operation"
	}
	return enumOr(s, "unknown", publicTypes...)
}

// Operations returns operation_public rows sorted by startedAt descending.
func (c *Coordinator) Operations() []map[string]any {
	c.mu.Lock()
	ops := append([]*Operation(nil), c.ops...)
	c.mu.Unlock()
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].StartedAt.After(ops[j].StartedAt) })
	out := make([]map[string]any, 0, len(ops))
	for _, o := range ops {
		out = append(out, o.Public())
	}
	return out
}

// Operation finds an operation by id.
func (c *Coordinator) Operation(id string) (*Operation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.ops {
		if o.ID == id {
			return o, true
		}
	}
	return nil, false
}

// CancelOperation implements ops_cancel (operation-coordinator.sh:1046-1057).
func (c *Coordinator) CancelOperation(id string) (map[string]any, error) {
	o, ok := c.Operation(id)
	if !ok {
		return nil, &QueueError{"STATE_UNAVAILABLE"}
	}
	o.mu.Lock()
	if !o.FinishedAt.IsZero() {
		o.mu.Unlock()
		return map[string]any{"ok": true, "alreadyFinished": true}, nil
	}
	if o.Cancelability != "cooperative" || o.Type == "route_operation" {
		o.mu.Unlock()
		return nil, &QueueError{"CANCEL_NOT_SUPPORTED"}
	}
	o.cancelReq = true
	fn := o.cancelFn
	o.mu.Unlock()
	c.journal.Append(o, "cancel_requested", "pending", "")
	if fn != nil {
		fn()
	} else {
		c.cancelRunning(id)
	}
	return map[string]any{"ok": true, "cancelRequested": true}, nil
}

// GlobalFence classifies the global operation fence for status.cgi
// (operation-coordinator.sh:1172-1185): absent | managed_active (held by a
// live in-process owner) | managed_stale (a pending fence left by a stopped
// resumable operation; no live owner). The daemon is the only fence owner,
// so "ambiguous" cannot occur in 3.5.0.
func (c *Coordinator) GlobalFence() string {
	h, ok := c.d.Lock.Current()
	switch {
	case !ok:
		if c.d.Lock.Busy() {
			return "managed_active" // a detached updater owns its kernel lock
		}
		return "absent"
	case h.Pending:
		return "managed_stale"
	}
	return "managed_active"
}

// Snapshot is the GET /api/operations/status.cgi object.
func (c *Coordinator) Snapshot() map[string]any {
	errs := []string{}
	fence := c.GlobalFence()
	if fence == "ambiguous" {
		errs = append(errs, "OWNER_UNCONFIRMED")
	}
	ok := len(errs) == 0
	return map[string]any{"ok": ok, "complete": ok, "capturedAt": StampUTC(time.Now()), "operations": c.Operations(),
		"queue": c.QueueRows(), "errors": errs, "automationPaused": c.Paused(), "globalFence": fence}
}

// StopBackground implements ops_stop_background (operation-coordinator.sh:
// 1059-1076): pause automation, then request cancellation of every running
// operation. Protected/route operations keep running and are reported so.
func (c *Coordinator) StopBackground() (map[string]any, error) {
	if err := c.SetPaused(true); err != nil {
		return nil, &QueueError{"STATE_UNAVAILABLE"}
	}
	c.mu.Lock()
	ops := append([]*Operation(nil), c.ops...)
	c.mu.Unlock()
	rows := []map[string]any{}
	for _, o := range ops {
		if o.isFinished() {
			continue
		}
		res, err := c.CancelOperation(o.ID)
		row := map[string]any{"operationId": PublicOperationID(o.ID), "cancelRequested": false, "protected": false, "ok": err == nil}
		switch {
		case err == nil:
			row["cancelRequested"] = res["cancelRequested"] == true
		case err.(*QueueError).Code == "CANCEL_NOT_SUPPORTED":
			row["protected"], row["ok"] = true, true
		}
		rows = append(rows, row)
	}
	return map[string]any{"ok": true, "automationPaused": true, "operations": rows}, nil
}

// EmergencyRecover implements ops_emergency_recover (operation-coordinator.sh:
// 1078-1122) for the in-process coordinator: stop-background (pause +
// cancellation requests), then classify the global fence. A fence held by
// a live owner is never broken (result ACTIVE, retryable while a
// cooperative cancellation is in flight); a pending resumable fence belongs
// to its domain (domain_pending). pending reports a blocking result of the
// BROray page operations (update/reinstall/uninstall → "updater_pending").
// Blocked → error RECOVERY_BLOCKED (HTTP 409) with the same body.
func (c *Coordinator) EmergencyRecover(pending func() string) (map[string]any, error) {
	stopped, err := c.StopBackground()
	if err != nil {
		return map[string]any{"ok": false, "result": "unsafe_lock", "automationPaused": c.Paused(), "operations": []map[string]any{},
			"retryable": false, "errorCode": "STATE_UNAVAILABLE"}, err
	}
	result, blocked, retryable := "absent", false, false
	if h, held := c.d.Lock.Current(); held {
		blocked = true
		if h.Pending {
			result = "domain_pending"
		} else {
			result = "ACTIVE"
			for _, row := range stopped["operations"].([]map[string]any) {
				if row["cancelRequested"] == true {
					retryable = true
				}
			}
		}
	}
	if !blocked && pending != nil {
		if r := pending(); r != "" {
			result, blocked = r, true
		}
	}
	out := map[string]any{"ok": !blocked, "result": result, "automationPaused": true, "operations": stopped["operations"],
		"retryable": retryable, "errorCode": nil}
	if blocked {
		out["errorCode"] = "RECOVERY_BLOCKED"
		return out, &QueueError{"RECOVERY_BLOCKED"}
	}
	return out, nil
}

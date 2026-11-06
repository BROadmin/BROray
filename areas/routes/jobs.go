package routes

import (
	"errors"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

const (
	msgBusy          = "Другая конфликтующая операция уже выполняется."
	msgBusyCustom    = "Другая конфликтующая операция уже выполняется или ожидает продолжения."
	msgBusyRemove    = "Другая операция выполняется или ожидает продолжения."
	msgRoutesBusyCLI = "Другая операция с маршрутами уже выполняется."
	msgRollbackBlock = "Предыдущий откат маршрутов не подтверждён; новые изменения заблокированы до восстановления."
)

// job is one admitted protected route job (3.x broray_route_job_run):
// it owns the global fence and appears in the operations panel.
type job struct {
	e                *engine
	action           string
	bundle           string
	op               *compat.Operation
	rel              compat.Release
	tookPending      bool
	keepPending      bool
	pendingResumable bool
	done             bool
}

// admit is broray_routes_api_lock_acquire + coordinator begin: refuse while
// another operation owns the fence or a routes progress file is running or
// resumable (except resume/preflight:resume of that same bundle).
func (e *engine) admit(action, bundle, busyMsg string) (*job, *cgi.APIError) {
	if e.operationBlocking(action, bundle) {
		return nil, cgi.E(409, "ROUTES_OPERATION_BUSY", busyMsg)
	}
	var rel compat.Release
	var ok, took bool
	if action == "resume" || action == "preflight:resume" {
		rel, ok = e.lock.TakePending("routes", bundle, action)
		took = ok
	}
	if !ok {
		rel, ok = e.lock.TryAcquire("routes", action, bundle)
	}
	if !ok {
		return nil, cgi.E(409, "ROUTES_OPERATION_BUSY", busyMsg)
	}
	j := &job{e: e, action: action, bundle: bundle, rel: rel, tookPending: took}
	if e.queue != nil {
		j.op = e.queue.StartOperation("", "route_operation", action, "USER", "protected", "")
		j.op.Phase("working")
	}
	return j, nil
}

func (j *job) opID() string {
	if j.op == nil {
		return ""
	}
	return j.op.ID
}

// pend keeps the fence pending after the job ends (resumable stop).
func (j *job) pend() { j.keepPending, j.pendingResumable = true, true }

// block retains the global lease without offering resume after an uncertain
// rollback or an unfinalized durable transaction.
func (j *job) block() { j.keepPending, j.pendingResumable = true, false }

// end finishes the operation record and releases (or parks) the fence.
func (j *job) end(err error) error {
	if j == nil || j.done {
		return nil
	}
	j.done = true
	var pendingErr error
	if j.keepPending {
		pendingErr = j.e.lock.MarkPending(j.pendingResumable)
		err = errors.Join(err, pendingErr)
	}
	if j.op != nil {
		j.op.Finish(err)
	}
	j.rel()
	return pendingErr
}

// globalOperation is broray_routes_api_lock_read_json.
func (e *engine) globalOperation() obj {
	if h, ok := e.lock.Current(); ok && !h.Pending {
		return obj(e.lock.JSON())
	}
	for _, p := range e.resumableProgress() {
		return obj{"active": true, "pending": true, "resumable": true, "pid": nil, "scope": "routes", "action": "resume-required",
			"operation": p["operation"], "bundleId": p["bundleId"], "startedAt": p["startedAt"], "updatedAt": p["updatedAt"], "stale": false}
	}
	if _, ok := e.lock.Current(); ok {
		return obj(e.lock.JSON())
	}
	return obj{"active": false, "pending": false, "resumable": false, "pid": nil, "scope": nil, "action": nil,
		"bundleId": nil, "startedAt": nil, "updatedAt": nil, "stale": false}
}

// operationStatus is broray_routes_page_operation_status.
func (e *engine) operationStatus() obj {
	g := e.globalOperation()
	bundleID, _ := g["bundleId"].(string)
	bundleType, bundleName := "catalog", bundleID
	if bundleID != "" {
		if c, ok := e.customEntry(bundleID); ok {
			bundleType = "custom"
			if n := str(c, "name"); n != "" {
				bundleName = n
			}
		} else if mf, ok := readObj(e.manifestFile(bundleID)); ok {
			if n := str(mf, "name"); n != "" {
				bundleName = n
			} else if n := str(mf, "id"); n != "" {
				bundleName = n
			}
		}
	}
	var p obj
	if bundleID != "" {
		p = e.progressOrIdle(bundleID)
	} else {
		p = idleProgress("")
	}
	pid, _ := p["bundleId"].(string)
	running := boolOf(p["running"])
	// jq `$p.resumable // $g.resumable // false` (false falls through)
	resumable := boolOf(p["resumable"]) || boolOf(g["resumable"])
	var outID, outType, outName any
	switch {
	case bundleID != "":
		outID = bundleID
	case pid != "":
		outID = pid
	}
	if bundleID != "" || pid != "" {
		outType, outName = bundleType, bundleName
	}
	action := g["action"]
	if action == nil {
		action = p["operation"]
	}
	return obj{
		"schemaVersion": 1, "generatedAt": stamp(), "canStop": boolOf(p["canStop"]),
		"active":  boolOf(g["active"]) || running || boolOf(p["resumable"]),
		"pending": boolOf(g["pending"]), "resumable": resumable, "running": running,
		"bundleId": outID, "bundleType": outType, "bundleName": outName, "action": action,
		"globalOperation": g, "progress": p,
	}
}

// routesBusy reports an active (not pending) routes job (3.x: the resource
// lease directory routes/locks/operation.lock exists).
func (e *engine) routesBusy() bool {
	h, ok := e.lock.Current()
	return ok && !h.Pending && h.Scope == "routes"
}

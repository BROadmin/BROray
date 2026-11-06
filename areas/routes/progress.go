package routes

import (
	"os"
	"strconv"
	"strings"
)

// progress is the persistent route-operation progress
// (lib/routes-operation-progress.sh). The file is written on
// begin/update/complete/fail/pause; per-route ticks only rewrite the
// counter file "<current>\t<route>".
type progress struct {
	e         *engine
	bundle    string
	opID      string
	operation string
	phase     string
	current   int
	total     int
	message   string
	route     string
	running   bool
	success   any // nil | bool
	rolled    bool
	resumable bool
	stopped   bool
	errRoute  string
	resumed   bool
	startedAt string
	doneAt    string
}

func idleProgress(bundle string) obj {
	return obj{"schemaVersion": 2, "kind": "routes", "bundleId": bundle, "operation": nil, "phase": "idle", "current": 0,
		"total": 0, "percent": 0, "currentRoute": nil, "message": "Операция не выполняется.", "running": false,
		"success": nil, "rolledBack": false, "resumable": false, "stopRequested": false, "stoppedByUser": false,
		"resumed": false, "errorRoute": nil, "pid": nil, "startedAt": nil, "updatedAt": nil, "completedAt": nil}
}

func percentOf(current, total int, running bool, success any) int {
	if total <= 0 {
		if !running && success == true {
			return 100
		}
		return 0
	}
	if current > total {
		current = total
	}
	p := current * 100 / total
	if p > 100 {
		p = 100
	}
	return p
}

func (p *progress) object() obj {
	return obj{
		"schemaVersion": 2, "kind": "routes", "backgroundOperationId": nullStr(p.opID), "canStop": false,
		"bundleId": p.bundle, "operation": p.operation, "phase": p.phase, "current": p.current, "total": p.total,
		"percent": percentOf(p.current, p.total, p.running, p.success), "currentRoute": nullStr(p.route),
		"message": p.message, "running": p.running, "success": p.success, "rolledBack": p.rolled,
		"resumable": p.resumable, "stopRequested": false, "stoppedByUser": p.stopped, "resumed": p.resumed,
		"errorRoute": nullStr(p.errRoute), "pid": os.Getpid(), "startedAt": p.startedAt, "updatedAt": stamp(),
		"completedAt": nullStr(p.doneAt),
	}
}

func (p *progress) write() {
	_ = writeObj(p.e.progressFile(p.bundle), p.object())
	p.writeCounter()
}

func (p *progress) writeCounter() {
	r := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(p.route)
	_ = writeLight(p.e.counterFile(p.bundle), []byte(strconv.Itoa(p.current)+"\t"+r+"\n"))
}

// beginProgress starts a progress record; the bundle is marked active for
// this process so readers do not report it as interrupted.
func (e *engine) beginProgress(bundle, operation, opID string, total, current int, resumed bool, message string) *progress {
	e.mu.Lock()
	e.active[bundle] = true
	if e.stops == nil {
		e.stops = map[string]*routeStopState{}
	}
	e.stops[bundle] = &routeStopState{operationID: opID}
	e.mu.Unlock()
	p := &progress{e: e, bundle: bundle, opID: opID, operation: operation, phase: "preparing", current: current, total: total,
		message: message, running: true, resumed: resumed, startedAt: stamp()}
	p.write()
	return p
}

func (p *progress) update(phase string, current int, message, route string) {
	if current > p.total {
		current = p.total
	}
	p.phase, p.current, p.message, p.route, p.running, p.success, p.doneAt = phase, current, message, route, true, nil, ""
	p.write()
}

func (p *progress) tick(current int, route string) {
	if current > p.total {
		current = p.total
	}
	p.current, p.route = current, route
	p.writeCounter()
}

func (p *progress) finish() {
	p.e.mu.Lock()
	delete(p.e.active, p.bundle)
	delete(p.e.stops, p.bundle)
	p.e.mu.Unlock()
}

func (p *progress) complete(message string) {
	p.phase, p.current, p.message, p.route = "completed", p.total, message, ""
	p.running, p.success, p.rolled, p.resumable, p.stopped, p.errRoute, p.doneAt = false, true, false, false, false, "", stamp()
	p.write()
	p.finish()
}

func (p *progress) fail(message string, rolledBack bool) {
	p.phase = "failed"
	if rolledBack {
		p.phase = "rolled_back"
	}
	p.message, p.route, p.running, p.success, p.rolled = message, "", false, false, rolledBack
	p.resumable, p.stopped, p.errRoute, p.doneAt = false, false, "", stamp()
	p.write()
	p.finish()
}

func (p *progress) pause(message string, stoppedByUser bool, errRoute string) {
	p.phase, p.success = "failed_resumable", any(false)
	if stoppedByUser {
		p.phase, p.success = "paused", nil
	}
	p.message, p.route, p.running, p.rolled, p.resumable = message, "", false, false, true
	p.stopped, p.errRoute, p.doneAt = stoppedByUser, errRoute, stamp()
	p.write()
	p.finish()
}

func (e *engine) isActive(bundle string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.active[bundle]
}

// readProgress is broray_routes_progress_read: validated progress with the
// counter overlay; a running record not owned by a live operation of this
// process is reported as interrupted. ok=false when there is no valid file.
func (e *engine) readProgress(bundle string) (obj, bool) {
	if !validBundleID(bundle) {
		return nil, false
	}
	m, ok := readObj(e.progressFile(bundle))
	if !ok {
		return nil, false
	}
	sv := intOr(m["schemaVersion"], 0)
	if (sv != 1 && sv != 2) || str(m, "kind") != "routes" || str(m, "bundleId") != bundle {
		return nil, false
	}
	if _, ok := m["running"].(bool); !ok {
		return nil, false
	}
	cur, ok1 := intOf(m["current"])
	total, ok2 := intOf(m["total"])
	if !ok1 || !ok2 {
		return nil, false
	}
	route := ""
	if b, err := os.ReadFile(e.counterFile(bundle)); err == nil {
		line := strings.SplitN(string(b), "\n", 2)[0]
		parts := strings.SplitN(line, "\t", 2)
		cur = 0
		if n, err := strconv.Atoi(parts[0]); err == nil && n >= 0 && allDigits(parts[0]) {
			cur = n
		}
		if len(parts) == 2 {
			route = parts[1]
		}
	}
	if cur < 0 {
		cur = 0
	}
	if cur > total {
		cur = total
	}
	m["current"] = cur
	m["currentRoute"] = nullStr(route)
	if total > 0 {
		m["percent"] = cur * 100 / total
	} else if m["success"] == true {
		m["percent"] = 100
	} else {
		m["percent"] = 0
	}
	m["canStop"], m["stopRequested"] = e.routeStopSnapshot(bundle, str(m, "backgroundOperationId"))
	if boolOf(m["running"]) && !e.isActive(bundle) {
		m["phase"], m["running"], m["success"], m["rolledBack"], m["resumable"] = "interrupted", false, false, false, false
		m["canStop"], m["stopRequested"], m["stoppedByUser"], m["currentRoute"] = false, false, false, nil
		m["message"] = "Операция неожиданно завершилась. Выполните проверку набора перед продолжением."
		if m["completedAt"] == nil {
			m["completedAt"] = m["updatedAt"]
		}
	}
	return m, true
}

// progressOrIdle returns readProgress or the idle object.
func (e *engine) progressOrIdle(bundle string) obj {
	if m, ok := e.readProgress(bundle); ok {
		return m
	}
	return idleProgress(bundle)
}

// resumeValues is broray_routes_progress_resume_values.
func (e *engine) resumeValues(bundle, operation string, remaining int) (base, total int, resumed bool) {
	m, ok := readObj(e.progressFile(bundle))
	if ok && boolOf(m["resumable"]) && str(m, "operation") == operation {
		cur, ok1 := intOf(m["current"])
		tot, ok2 := intOf(m["total"])
		if ok1 && ok2 && cur >= 0 && cur <= tot && tot-cur == remaining {
			return cur, tot, true
		}
	}
	return 0, remaining, false
}

// pendingResumable lists bundles whose progress is resumable and not running.
func (e *engine) resumableProgress() []obj {
	entries, _ := os.ReadDir(e.p("operations"))
	var out []obj
	for _, en := range entries {
		name := en.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		m, ok := e.readProgress(id)
		if !ok {
			continue
		}
		if !boolOf(m["running"]) && boolOf(m["resumable"]) {
			out = append(out, m)
		}
	}
	return out
}

// operationBlocking reports whether a routes progress file blocks new
// operations (coordinator rule, operation-coordinator.sh:549-600).
func (e *engine) operationBlocking(action, bundle string) bool {
	entries, _ := os.ReadDir(e.p("operations"))
	for _, en := range entries {
		name := en.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		m, ok := e.readProgress(id)
		if !ok {
			continue
		}
		if boolOf(m["running"]) || boolOf(m["resumable"]) {
			if (action == "resume" || action == "preflight:resume") && id == bundle && !boolOf(m["running"]) {
				continue
			}
			return true
		}
	}
	return false
}

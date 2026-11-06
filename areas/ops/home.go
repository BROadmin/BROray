package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

// Home snapshots (bin/broray-home-snapshotd, bin/broray-home-snapshot,
// lib/home-snapshot.sh): a background loop refreshes every module in a fixed
// order every 30 s; summary.cgi only reads the stored snapshots.

var homeModules = []string{"xray", "keenetic", "servers", "subscriptions", "dns", "routes", "broray"}

const (
	snapshotInterval = 30 * time.Second
	snapshotTimeout  = 60 * time.Second
	snapshotFresh    = 90
	snapshotExpire   = 600
)

type collectResult struct {
	At    time.Time
	OK    bool
	Error string
}

var errNoProducer = errors.New("SNAPSHOT_PRODUCER_MISSING")

func (a *area) snapshotPath(module string) string {
	return a.d.RunPath("home-snapshots", module+".json")
}

func (a *area) collectLoop(ctx context.Context) {
	t := time.NewTimer(time.Second) // let the other areas register their producers first
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a.collectAll(ctx)
		t.Reset(snapshotInterval)
	}
}

// collectAll refreshes every module in the 3.x order. A failing or missing
// producer keeps the previous snapshot (it turns stale, then expired).
func (a *area) collectAll(ctx context.Context) {
	for _, m := range homeModules {
		if ctx.Err() != nil {
			return
		}
		err := a.refreshModule(ctx, m)
		res := collectResult{At: a.now(), OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		a.collMu.Lock()
		prev, seen := a.collLast[m]
		a.collLast[m] = res
		a.collMu.Unlock()
		if a.d.Log != nil && (!seen || prev.OK != res.OK) {
			if err != nil {
				a.d.Log.Printf("HOME_SNAPSHOT_REFRESH=%s:FAIL %v", m, err)
			} else if seen {
				a.d.Log.Printf("HOME_SNAPSHOT_REFRESH=%s:PASS", m)
			}
		}
	}
}

// refreshModule runs one producer and atomically publishes its snapshot.
func (a *area) refreshModule(ctx context.Context, module string) (err error) {
	fn, ok := compat.SnapshotProducer(module)
	if !ok {
		return errNoProducer
	}
	cctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("SNAPSHOT_PRODUCER_PANIC: %v", p)
		}
	}()
	data, err := fn(cctx)
	if err != nil {
		return err
	}
	if data == nil {
		return errors.New("SNAPSHOT_DATA_INVALID")
	}
	if module == "routes" {
		data = projectRoutes(data)
	}
	// Round-trip through JSON so the stored object is plain data.
	b, err := compat.Marshal(data)
	if err != nil {
		return err
	}
	var plain map[string]any
	if err := jsonUnmarshal(b, &plain); err != nil || plain == nil {
		return errors.New("SNAPSHOT_DATA_INVALID")
	}
	now := a.now()
	if err := os.MkdirAll(filepath.Dir(a.snapshotPath(module)), 0o700); err != nil {
		return err
	}
	return compat.WriteJSON(a.snapshotPath(module), map[string]any{"schemaVersion": 1, "module": module,
		"capturedAt": compat.StampUTC(now), "capturedEpoch": now.Unix(), "data": plain})
}

// projectRoutes applies the 3.x home projection of the routes page summary
// (bin/broray-home-snapshot:49-67) when the producer returned the page
// summary (it has "totals"); an already projected object is kept.
func projectRoutes(page map[string]any) map[string]any {
	totals, ok := page["totals"].(map[string]any)
	if !ok {
		return page
	}
	or := func(v, def any) any {
		if v == nil || v == false {
			return def
		}
		return v
	}
	health, _ := page["health"].(map[string]any)
	var actionRequired, running any = true, false
	if health != nil {
		actionRequired = or(health["actionRequired"], true)
	}
	if g, ok := page["globalOperation"].(map[string]any); ok {
		running = or(g["running"], false)
	}
	var h any
	if v, ok := page["health"]; ok {
		h = v
	}
	return map[string]any{
		"schemaVersion":            2,
		"availableBundles":         or(totals["bundleCount"], 0),
		"installedBundles":         or(totals["verifiedInstalledCount"], 0),
		"verifiedInstalledBundles": or(totals["verifiedInstalledCount"], 0),
		"recordedInstalledBundles": or(totals["recordedInstalledCount"], 0),
		"installedRouteCount":      or(totals["presentRouteCount"], 0),
		"attentionCount":           or(totals["attentionCount"], 0),
		"actionRequired":           actionRequired,
		"operationRunning":         running,
		"managedInterface":         or(page["managedInterface"], "ProxyN"),
		"routerSnapshot":           or(page["routerSnapshot"], nil),
		"bundles":                  or(page["bundles"], []any{}),
		"health":                   h,
		"updatedAt":                or(page["generatedAt"], nil),
	}
}

// readSnapshot is broray_home_snapshot_read: data + _snapshot, or ok=false.
func (a *area) readSnapshot(module string) (map[string]any, bool) {
	p := a.snapshotPath(module)
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		return nil, false
	}
	var snap map[string]any
	b, err := os.ReadFile(p)
	if err != nil || jsonUnmarshal(b, &snap) != nil || snap == nil {
		return nil, false
	}
	sv, ok1 := snap["schemaVersion"].(float64)
	mod, ok2 := snap["module"].(string)
	capturedAt, ok3 := snap["capturedAt"].(string)
	epoch, ok4 := snap["capturedEpoch"].(float64)
	data, ok5 := snap["data"].(map[string]any)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) || sv != 1 || mod != module {
		return nil, false
	}
	age := float64(a.now().Unix()) - epoch
	if age < 0 {
		age = 0
	}
	state := "expired"
	switch {
	case age <= snapshotFresh:
		state = "fresh"
	case age <= snapshotExpire:
		state = "stale"
	}
	data["_snapshot"] = map[string]any{"schemaVersion": 1, "module": module, "capturedAt": capturedAt,
		"capturedEpoch": jsonNum(epoch), "ageSeconds": jsonNum(age), "freshness": state}
	return data, true
}

// jsonNum keeps integral numbers integral in the output (jq prints 12, not 12.0).
func jsonNum(f float64) any {
	if f == float64(int64(f)) {
		return int64(f)
	}
	return f
}

func registerHome() {
	cgi.RegisterRaw("/api/home/summary.cgi", func(c *cgi.Req) {
		a := current()
		data, err := a.homeSummary()
		if err != nil {
			cgi.WriteErr(c.W, cgi.E(500, "HOME_SUMMARY_FAILED", "Не удалось прочитать сохранённые состояния модулей."))
			return
		}
		cgi.WriteOK(c.W, data)
	})
}

// installedRelease is the 3.5.0 counterpart of the release manifest identity.
func installedRelease() map[string]any {
	return map[string]any{"version": app.Version, "releaseId": app.Version, "candidateId": app.Version, "webUIBuild": webUIBuild}
}

func severityRank(v any) int {
	switch v {
	case "error":
		return 50
	case "busy":
		return 40
	case "warning":
		return 30
	case "unknown":
		return 20
	case "ok":
		return 10
	}
	return 20
}

// alt is jq's `a // b`: a unless it is null or false.
func alt(a, b any) any {
	if a == nil || a == false {
		return b
	}
	return a
}

func moduleHealth(module string, value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{"schemaVersion": 1, "module": module, "availability": "unavailable", "severity": "error",
			"operational": false, "consistent": false, "actionRequired": true,
			"freshness": map[string]any{"state": "unknown", "checkedAt": nil},
			"reasons":   []any{map[string]any{"code": "MODULE_SNAPSHOT_UNAVAILABLE", "message": "Валидный снимок модуля ещё не опубликован.", "details": nil}},
			"facts":     map[string]any{}, "lastOperation": nil}
	}
	var base map[string]any
	if h, ok := value["health"].(map[string]any); ok {
		base = make(map[string]any, len(h))
		for k, v := range h {
			base[k] = v
		}
	} else {
		base = map[string]any{"schemaVersion": 1, "module": module, "availability": "partial", "severity": "unknown",
			"operational": false, "consistent": false, "actionRequired": true,
			"freshness": map[string]any{"state": "unknown", "checkedAt": nil},
			"reasons":   []any{map[string]any{"code": "STATUS_CONTRACT_MISSING", "message": "Снимок модуля не содержит единого контракта состояния.", "details": nil}},
			"facts":     map[string]any{}, "lastOperation": nil}
	}
	snap, _ := value["_snapshot"].(map[string]any)
	var state, capturedAt, age any = "unknown", nil, nil
	if snap != nil {
		state = alt(snap["freshness"], "unknown")
		capturedAt = alt(snap["capturedAt"], nil)
		age = alt(snap["ageSeconds"], nil)
	}
	raise := func() {
		sev := alt(base["severity"], "unknown")
		if severityRank(sev) < severityRank("warning") {
			sev = "warning"
		}
		base["severity"] = sev
		base["actionRequired"] = true
	}
	switch state {
	case "fresh":
		base["freshness"] = map[string]any{"state": "fresh", "checkedAt": capturedAt}
	case "stale", "expired":
		raise()
		base["freshness"] = map[string]any{"state": state, "checkedAt": capturedAt}
		code, msg := "MODULE_SNAPSHOT_STALE", "Показано последнее известное состояние; фоновое обновление задерживается."
		if state == "expired" {
			code, msg = "MODULE_SNAPSHOT_EXPIRED", "Показано последнее известное состояние; фоновое обновление давно не завершалось."
		}
		reasons, _ := alt(base["reasons"], []any{}).([]any)
		base["reasons"] = append(append([]any{}, reasons...), map[string]any{"code": code, "message": msg,
			"details": map[string]any{"capturedAt": capturedAt, "ageSeconds": age}})
	default:
		raise()
		base["freshness"] = map[string]any{"state": "unknown", "checkedAt": capturedAt}
	}
	return base
}

// patchDNS rewrites the DNS_TEST_REQUIRED reason text (summary.cgi:66-89).
func patchDNS(dns map[string]any) {
	h, ok := dns["health"].(map[string]any)
	if !ok {
		return
	}
	reasons, ok := h["reasons"].([]any)
	if !ok {
		return
	}
	num := func(v any) string {
		v = alt(v, 0)
		if f, ok := v.(float64); ok {
			return fmt.Sprint(jsonNum(f))
		}
		b, _ := compat.Marshal(v)
		return string(trimNL(b))
	}
	out := make([]any, len(reasons))
	for i, r := range reasons {
		if m, ok := r.(map[string]any); ok && m["code"] == "DNS_TEST_REQUIRED" {
			cp := make(map[string]any, len(m))
			for k, v := range m {
				cp[k] = v
			}
			cp["message"] = "Проверено " + num(dns["selectedTestedCount"]) + " из " + num(dns["selectedCount"]) + " выбранных серверов DNS-over-TLS."
			r = cp
		}
		out[i] = r
	}
	h["reasons"] = out
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b
}

// homeSummary builds the summary.cgi data object (summary.cgi:91-220).
func (a *area) homeSummary() (map[string]any, error) {
	values := map[string]map[string]any{}
	errs := []any{}
	// Read order of summary.cgi (determines the order of `errors`).
	for _, m := range []string{"xray", "keenetic", "servers", "subscriptions", "dns", "routes", "broray"} {
		if v, ok := a.readSnapshot(m); ok {
			values[m] = v
		} else {
			errs = append(errs, m)
		}
	}
	if dns := values["dns"]; dns != nil {
		patchDNS(dns)
	}
	updatedAt := compat.StampUTC(a.now())
	order := []string{"xray", "servers", "subscriptions", "dns", "routes", "keenetic", "broray"}
	modules := make([]any, 0, len(order))
	healths := make([]map[string]any, 0, len(order))
	var severity any = "unknown"
	best := -1
	issues := []any{}
	reasons := []any{}
	operational, consistent, actionRequired := true, true, false
	anyExpired, anyStale, allFresh := false, false, true
	for _, m := range order {
		h := moduleHealth(m, values[m])
		healths = append(healths, h)
		modules = append(modules, map[string]any{"id": m, "health": h})
		sev := alt(h["severity"], "unknown")
		if r := severityRank(sev); r >= best { // jq max_by: the last maximum wins
			best, severity = r, sev
		}
		if h["operational"] != true {
			operational = false
		}
		if h["consistent"] != true {
			consistent = false
		}
		ar, isBool := h["actionRequired"].(bool)
		if !isBool || ar {
			actionRequired = true
		}
		fstate := any(nil)
		if f, ok := h["freshness"].(map[string]any); ok {
			fstate = f["state"]
		}
		switch fstate {
		case "expired":
			anyExpired = true
		case "stale":
			anyStale = true
		}
		if fstate != "fresh" {
			allFresh = false
		}
		if sev != "ok" {
			rs, _ := alt(h["reasons"], []any{}).([]any)
			if rs == nil {
				rs = []any{}
			}
			issueAR := true
			if isBool {
				issueAR = ar
			}
			issues = append(issues, map[string]any{"module": m, "severity": sev, "actionRequired": issueAR, "reasons": rs})
			for _, r := range rs {
				if rm, ok := r.(map[string]any); ok {
					cp := make(map[string]any, len(rm)+1)
					for k, v := range rm {
						cp[k] = v
					}
					cp["module"] = m
					reasons = append(reasons, cp)
				}
			}
		}
	}
	fresh := "unknown"
	switch {
	case anyExpired:
		fresh = "expired"
	case anyStale:
		fresh = "stale"
	case allFresh:
		fresh = "fresh"
	}
	availability := "available"
	if len(errs) > 0 {
		availability = "partial"
	}
	val := func(m string) any {
		if v := values[m]; v != nil {
			return v
		}
		return nil
	}
	return map[string]any{
		"schemaVersion":    3,
		"installedRelease": installedRelease(),
		"xray":             val("xray"), "servers": val("servers"), "subscriptions": val("subscriptions"),
		"dns": val("dns"), "keenetic": val("keenetic"), "routes": val("routes"), "broray": val("broray"),
		"snapshotRuntime":  map[string]any{"readOnly": true, "collectorProcessesStarted": 0, "aggregateLock": false},
		"collectorRuntime": map[string]any{"mode": "snapshot-only", "maxConcurrentOperations": 0, "budgetSeconds": 0, "elapsedSeconds": 0},
		"modules":          modules,
		"errors":           errs,
		"issues":           issues,
		"health": map[string]any{"schemaVersion": 1, "module": "home", "availability": availability, "severity": severity,
			"operational": operational, "consistent": consistent, "actionRequired": actionRequired,
			"freshness": map[string]any{"state": fresh, "checkedAt": updatedAt}, "reasons": reasons,
			"facts":         map[string]any{"moduleCount": len(modules), "unavailableCount": len(errs), "issueCount": len(issues)},
			"lastOperation": nil},
		"healthy":   severity == "ok",
		"updatedAt": updatedAt,
	}, nil
}

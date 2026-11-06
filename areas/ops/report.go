package ops

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"golang.org/x/sys/unix"
)

// The diagnostics report (lib/operation-report.sh + operation-report-facts.sh):
// read-only, bounded facts that 3.5.0 actually knows. complete is always
// false like 3.x (network continuity and KeeneticOS are never probed).

const reportLimit = 1 << 20

var reKernel = regexp.MustCompile(`^[0-9][A-Za-z0-9._+-]{0,63}$`)

func uname() (machine, release string) {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return "", ""
	}
	return unix.ByteSliceToString(u.Machine[:]), unix.ByteSliceToString(u.Release[:])
}

func uptimeSeconds() any {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return nil
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return nil
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil || v < 0 || v >= 3155760000 {
		return nil
	}
	return int64(v)
}

// reportAutomation is ops_report_automation over the 3.x file formats the
// servers/subscriptions areas keep (config/system/server-auto-switch.json,
// config/subscriptions/*.json).
func (a *area) reportAutomation() map[string]any {
	var autoSwitch, serverCheck any
	complete := false
	if m, ok := compat.ReadObject(a.d.Path("config", "system", "server-auto-switch.json")); ok {
		e, ok1 := m["enabled"].(bool)
		q, ok2 := m["qualityRefreshEnabled"].(bool)
		if ok1 && ok2 {
			autoSwitch, serverCheck, complete = e, q, true
		}
	}
	subComplete := true
	count, automatic := 0, 0
	var enabled any = false
	dir := a.d.Path("config", "subscriptions")
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		subComplete = false
	} else if len(files) > 32 {
		subComplete = false
	} else {
		for _, f := range files {
			count++
			m, ok := compat.ReadObject(f)
			if !ok {
				subComplete = false
				continue
			}
			e, ok1 := m["enabled"].(bool)
			u, ok2 := m["autoUpdateEnabled"].(bool)
			if !ok1 || !ok2 {
				subComplete = false
				continue
			}
			if e && u {
				automatic++
				enabled = true
			}
		}
	}
	errs := []string{}
	if !complete {
		errs = append(errs, "AUTOMATION_SETTINGS_UNAVAILABLE")
	}
	var autoCount any = automatic
	if !subComplete {
		enabled, autoCount = nil, nil
		errs = append(errs, "SUBSCRIPTION_SETTINGS_UNAVAILABLE")
	}
	return map[string]any{"autoSwitch": autoSwitch, "serverCheck": serverCheck, "subscriptionUpdate": enabled,
		"complete": complete && subComplete, "subscriptionRecordsRead": count, "automaticSubscriptions": autoCount, "errors": errs}
}

// reportServices: the 3.5.0 counterparts of the 3.x background services are
// the coordinator worker, the home snapshot collector and the in-process areas.
func (a *area) reportServices() []map[string]any {
	svc := func(name string, running bool, errCode any) map[string]any {
		state := "running"
		if !running {
			state = "stopped"
		}
		return map[string]any{"service": name, "state": state, "running": running, "ready": running, "complete": true, "errorCode": errCode}
	}
	out := []map[string]any{svc("operations", a.d.Queue.WorkerRunning(), nil)}
	a.collMu.Lock()
	collected := len(a.collLast) > 0
	a.collMu.Unlock()
	out = append(out, svc("home-snapshot", collected, nil))
	names := compat.AreaNames()
	sort.Strings(names)
	for _, n := range names {
		out = append(out, svc("area:"+n, true, nil))
	}
	return out
}

func (a *area) reportXray() map[string]any {
	out := map[string]any{"state": "unknown", "identity": nil, "cachedVersion": nil, "cache": nil, "complete": false,
		"errorCode": "XRAY_IDENTITY_UNCONFIRMED"}
	if snap, ok := a.readSnapshot("xray"); ok {
		if s, ok := snap["_snapshot"].(map[string]any); ok {
			out["cache"] = map[string]any{"capturedAt": s["capturedAt"], "ageSeconds": s["ageSeconds"], "freshness": s["freshness"]}
		}
		if v, ok := snap["version"].(string); ok && len(v) <= 24 {
			out["cachedVersion"] = v
		}
	}
	if a.d.App == nil || a.d.App.Xray == nil {
		return out
	}
	// The daemon supervises Xray as its own child: the handle is the identity.
	if a.d.App.Xray.Running() {
		st := a.d.App.Xray.Status(a.d.Ctx)
		out["state"] = "running"
		out["identity"] = map[string]any{"verified": true, "pid": st.PID, "sameBoot": true, "role": "persistent-xray"}
		out["complete"], out["errorCode"] = true, nil
	} else {
		out["state"] = "stopped"
		out["complete"], out["errorCode"] = true, nil
	}
	return out
}

// reportUpdater: 3.5.0 has no separate updater service; the last BROray
// page operation is reported when it is an update/reinstall (updater_public).
func (a *area) reportUpdater() map[string]any {
	out := map[string]any{"lastOperation": nil, "cache": nil, "liveState": "unknown", "complete": false, "errorCode": "UPDATER_STATUS_UNAVAILABLE"}
	if last := a.page.lastOperation(); last != nil {
		if op, _ := last["operation"].(string); op == "update" || op == "reinstall" {
			out["lastOperation"] = map[string]any{"operationId": last["operationId"], "state": last["state"], "running": last["running"],
				"operation": op, "updatedAt": last["updatedAt"], "rollbackPerformed": false}
			out["complete"], out["errorCode"] = true, nil
		}
	}
	return out
}

func (a *area) report() map[string]any {
	snap := a.d.Queue.Snapshot()
	journal := a.d.Queue.Journal().Events()
	automation := a.reportAutomation()
	services := a.reportServices()
	xr := a.reportXray()
	upd := a.reportUpdater()
	machine, kernel := uname()
	switch machine {
	case "aarch64", "armv7l", "mips", "mipsel", "x86_64":
	default:
		machine = "unknown"
	}
	if !reKernel.MatchString(kernel) {
		kernel = ""
	}
	ops, _ := snap["operations"].([]map[string]any)
	active, terminal := []map[string]any{}, []map[string]any{}
	for _, o := range ops {
		if o["running"] != false {
			active = append(active, o)
		} else if len(terminal) < 20 {
			terminal = append(terminal, o)
		}
	}
	okServices := 0
	for _, s := range services {
		if s["complete"] == true {
			okServices++
		}
	}
	unavailable := []string{"keeneticOS", "vpnContinuity", "updaterLiveIdentity"}
	if okServices != len(services) {
		unavailable = append(unavailable, "serviceStates")
	}
	if xr["complete"] != true {
		unavailable = append(unavailable, "xrayIdentity")
	}
	if upd["complete"] != true {
		unavailable = append(unavailable, "updaterStatus")
	}
	if kernel == "" {
		unavailable = append(unavailable, "kernel")
	}
	if automation["complete"] != true {
		unavailable = append(unavailable, "automationSettings")
	}
	errSet := map[string]bool{}
	for _, list := range []any{snap["errors"], journal["errors"], automation["errors"]} {
		if l, ok := list.([]string); ok {
			for _, e := range l {
				errSet[e] = true
			}
		}
	}
	for _, s := range services {
		if s["complete"] != true {
			if e, ok := s["errorCode"].(string); ok {
				errSet[e] = true
			}
		}
	}
	for _, x := range []map[string]any{xr, upd} {
		if x["complete"] != true {
			if e, ok := x["errorCode"].(string); ok {
				errSet[e] = true
			}
		}
	}
	errs := []string{}
	for e := range errSet {
		errs = append(errs, e)
	}
	sort.Strings(errs)
	var k any
	if kernel != "" {
		k = kernel
	}
	updaterRequest := "absent"
	if a.page.running() {
		updaterRequest = "present"
	}
	domain := "clear"
	if h, ok := a.d.Lock.Current(); ok && h.Pending {
		domain = "pending"
	}
	scheduler := "stopped"
	if a.d.Queue.WorkerRunning() {
		scheduler = "running"
	}
	automationDetails := map[string]any{}
	for key, v := range automation {
		if key != "autoSwitch" && key != "serverCheck" && key != "subscriptionUpdate" {
			automationDetails[key] = v
		}
	}
	return map[string]any{
		"schemaVersion": 1, "reportKind": "broray-diagnostics", "capturedAt": compat.StampUTC(a.now()), "redactionPolicy": "allowlist-v1",
		"complete": false, "snapshotConsistent": false, "consistency": map[string]any{"operations": "serialized", "runtime": "sampled"},
		"summary": strconv.Itoa(len(active)) + " активных операций; " + strconv.Itoa(okServices) + " из " + strconv.Itoa(len(services)) +
			" фоновых служб проверены. Сетевая непрерывность этим отчётом не проверяется.",
		"unavailable":       unavailable,
		"build":             map[string]any{"appVersion": app.Version, "candidateId": app.Version, "webuiBuild": webUIBuild},
		"platform":          map[string]any{"architecture": machine, "kernel": k, "uptimeSeconds": uptimeSeconds()},
		"services":          map[string]any{"xray": xr["state"], "scheduler": scheduler, "updater": "unknown"},
		"serviceDetails":    services,
		"runtimeDetails":    map[string]any{"xray": xr, "updater": upd, "proxyInterface": a.reportProxyInterface()},
		"automation":        map[string]any{"paused": snap["automationPaused"], "subscriptionUpdate": automation["subscriptionUpdate"], "serverCheck": automation["serverCheck"], "autoSwitch": automation["autoSwitch"]},
		"automationDetails": automationDetails,
		"operations":        append(active, terminal...),
		"fences":            map[string]any{"global": snap["globalFence"], "updaterRequest": updaterRequest, "domainPending": domain},
		"events":            journal["events"],
		"journal":           map[string]any{"complete": journal["complete"], "truncated": journal["truncated"]},
		"snapshotComplete":  snap["complete"],
		"errors":            errs,
	}
}

func (a *area) reportHandler(c *cgi.Req) {
	rep := a.report()
	b, err := compat.Marshal(rep)
	if err != nil || len(b) > reportLimit {
		compat.WriteOpsError(c.W, &compat.QueueError{Code: "REPORT_UNAVAILABLE"})
		return
	}
	c.W.Header().Set("Content-Disposition", `attachment; filename="BROray-diagnostics.json"`)
	cgi.WriteJSON(c.W, 200, rep)
}

// reportProxyInterface: the managed ProxyN facts from the last keenetic
// snapshot (3.5.0), incl. whether SOCKS5 UDP is enabled.
func (a *area) reportProxyInterface() map[string]any {
	out := map[string]any{"interfaceName": nil, "exists": nil, "ownershipConfirmed": nil, "socks5UdpEnabled": nil,
		"matchesExpected": nil, "capturedAt": nil, "complete": false}
	snap, ok := a.readSnapshot("keenetic")
	if !ok {
		return out
	}
	for _, k := range []string{"interfaceName", "exists", "ownershipConfirmed", "socks5UdpEnabled", "matchesExpected"} {
		out[k] = snap[k]
	}
	if s, ok := snap["_snapshot"].(map[string]any); ok {
		out["capturedAt"] = s["capturedAt"]
	}
	out["complete"] = true
	return out
}

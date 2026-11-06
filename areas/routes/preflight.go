package routes

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/BROadmin/broray/internal/compat"
)

const preflightTTL = 120

// writePolicySHA binds preflight tokens to the static-route write grammar.
// 3.x required a separately staged Keenetic write-policy carrier; in 3.5.0
// the grammar is compiled into the binary (allowRouteCommand).
var writePolicySHA = sha256Hex([]byte("static-routes\nip route <A.B.C.D> <mask> ProxyN 1200\nno ip route <A.B.C.D> <mask> ProxyN\nsystem configuration save\n"))

// deletePlan is the in-process delete preflight plan
// (routes-operation-preflight.sh:89-240).
func (e *engine) deletePlan(ctx context.Context, id string) (obj, bool) {
	cfg, _ := e.routesConfig()
	iface := str(cfg, "managedInterface")
	reg, ok1 := readObj(e.globalRegistryFile())
	breg, ok2 := readObj(e.registryFile(id))
	if cfg == nil || !ok1 || !ok2 || !reProxy.MatchString(iface) || intOr(cfg["managedMetric"], 0) != managedMetric {
		return nil, false
	}
	_, a1 := breg["routeKeys"].([]any)
	_, a2 := breg["managedRouteKeys"].([]any)
	_, a3 := breg["externalRouteKeys"].([]any)
	if intOr(breg["schemaVersion"], 0) != 1 || str(breg, "bundleId") != id || breg["installedVersion"] == nil ||
		str(breg, "targetInterface") != iface || intOr(breg["managedMetric"], 0) != managedMetric || !a1 || !a2 || !a3 {
		return nil, false
	}
	if _, isArr := reg["routes"].([]any); intOr(reg["schemaVersion"], 0) != 1 || str(reg, "managedInterface") != iface ||
		intOr(reg["managedMetric"], 0) != managedMetric || !isArr {
		return nil, false
	}
	snap, err := e.fetchConfig(ctx)
	if err != nil || !snap.complete() {
		return nil, false
	}
	byKey := map[string][]obj{}
	for _, x := range anyList(reg["routes"]) {
		m, _ := x.(obj)
		byKey[str(m, "key")] = append(byKey[str(m, "key")], m)
	}
	items := []any{}
	blocking := []any{}
	cnt := map[string]int{}
	for _, key := range strList(breg["managedRouteKeys"]) {
		regs := byKey[key]
		if len(regs) != 1 {
			it := obj{"key": key, "status": "conflict", "reason": "registry_mismatch"}
			items, blocking = append(items, it), append(blocking, it)
			cnt["conflict"]++
			continue
		}
		m := regs[0]
		r := registryRoute(m)
		dest := r.Network + "/" + itoa(r.Prefix)
		idc := snap.identityCount(dest, iface)
		at := snap.attributeCount(dest, iface, r.Metric)
		owners := strList(m["owners"])
		owner := false
		for _, o := range owners {
			if o == id {
				owner = true
			}
		}
		status := "conflict"
		switch {
		case m["createdByBROray"] != true || m["managed"] != true || str(m, "interface") != iface || !owner:
		case len(owners) > 1:
			status = "shared_keep"
		case idc > 1:
		case idc == 1 && at == 1:
			status = "delete"
		case idc == 0:
			status = "already_absent"
		}
		it := obj{"key": key, "route": m, "identityMatchCount": idc, "attributeMatchCount": at, "status": status}
		items = append(items, it)
		if status == "conflict" {
			blocking = append(blocking, it)
		}
		cnt[status]++
	}
	total := len(anyList(breg["routeKeys"]))
	msg := "Предварительная проверка удаления завершена."
	if len(blocking) > 0 {
		msg = "Удаление заблокировано: реестр владения или конфигурация Keenetic содержит конфликт."
	}
	return obj{"schemaVersion": 1, "bundleId": id, "mode": "delete", "targetInterface": iface, "managedMetric": managedMetric,
		"identityModel": "destination-interface-gateway", "contentSha256": orNull(sub(breg, "installedVersion"), "contentSha256"),
		"canApply": len(blocking) == 0,
		"summary": obj{"total": total, "toCreate": 0, "managedExisting": 0, "toDelete": cnt["delete"], "sharedKept": cnt["shared_keep"],
			"alreadyAbsent": cnt["already_absent"], "conflicts": len(blocking), "externalExisting": len(anyList(breg["externalRouteKeys"])),
			"unchangedRoutes": 0, "addedRoutes": 0, "removedRoutes": total},
		"items": items, "blocking": blocking, "message": msg}, true
}

func dirKB(dir string) int {
	total := int64(0)
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			total += 4
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += (info.Size() + 4095) / 4096 * 4
		}
		return nil
	})
	return int(total)
}

func freeKB(path string) int {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return int(uint64(st.Bavail) * uint64(st.Bsize) / 1024)
}

// finalizePreflight is broray_routes_operation_preflight_finalize.
func (e *engine) finalizePreflight(ctx context.Context, id, requested, resolved, savedOp string, plan obj) (obj, bool) {
	state, ok1 := readObj(e.stateFile(id))
	breg, ok2 := readObj(e.registryFile(id))
	if !ok1 || !ok2 || plan == nil {
		return nil, false
	}
	iface := str(plan, "targetInterface")
	mode := str(plan, "mode")
	summary := sub(plan, "summary")
	routeCount := intOr(summary["total"], 0)
	planReady := plan["canApply"] == true
	if intOr(plan["managedMetric"], 0) != managedMetric {
		return nil, false
	}
	var contentSHA string
	if resolved == "delete" {
		contentSHA = str(sub(breg, "installedVersion"), "contentSha256")
		mode = "delete"
	} else {
		contentSHA = str(plan, "contentSha256")
		if contentSHA == "" {
			contentSHA = str(sub(state, "downloadedVersion"), "contentSha256")
		}
	}
	display := e.interfaceDisplay()
	available, connected, up := e.interfaceUp(ctx, iface)
	interfaceOK := connected && up
	free := freeKB(e.data)
	required := 2048 + dirKB(e.catalogDir(id))*2 + (routeCount+3)/4
	if required < 4096 {
		required = 4096
	}
	storageOK := free >= required
	localOK := contentSHA != "" && routeCount > 0
	writePolicyOK := true
	resumeCur, resumeTotal, resumeOp := 0, 0, ""
	if requested == "resume" {
		p, ok := readObj(e.progressFile(id))
		if !ok {
			return nil, false
		}
		var ok1, ok2 bool
		resumeCur, ok1 = intOf(p["current"])
		resumeTotal, ok2 = intOf(p["total"])
		if !ok1 || !ok2 || resumeCur < 0 || resumeCur > resumeTotal {
			return nil, false
		}
		resumeOp = str(p, "operation")
	}
	ready := planReady && interfaceOK && storageOK && localOK && writePolicyOK
	now := nowFn()
	canonical := id + "|" + requested + "|" + resolved + "|" + savedOp + "|" + mode + "|" + itoa(int(now.Unix())) + "|" + contentSHA + "|" +
		itoa(routeCount) + "|" + itoa(resumeCur) + "|" + itoa(resumeTotal) + "|none|" + writePolicySHA + "|" + itoa(os.Getpid()) + "|" + compat.RandHex(16)
	token := sha256Hex([]byte(canonical))
	msg := "Операция заблокирована: локальный набор не готов."
	switch {
	case ready:
		msg = "Предварительная проверка завершена. Операция готова к запуску."
	case !planReady:
		msg = "Операция заблокирована: обнаружены конфликты маршрутов или владения."
	case !interfaceOK:
		msg = "Операция заблокирована: Keenetic или управляемый интерфейс недоступен."
	case !storageOK:
		msg = "Операция заблокирована: недостаточно свободного места в /opt."
	case !writePolicyOK:
		msg = "Операция заблокирована: протокол записи статических маршрутов Keenetic CLI не привязан."
	}
	operation := savedOp
	if operation == "" {
		operation = mode
	}
	g := func(k string) any {
		if v, ok := summary[k]; ok && v != nil {
			return v
		}
		return 0
	}
	res := obj{
		"schemaVersion": 1, "token": token, "bundleId": id, "requestedAction": requested, "resolvedAction": resolved,
		"operation": operation, "mode": mode, "ready": ready, "checkedAt": compat.Stamp(now), "checkedEpoch": now.Unix(),
		"expiresAfterSeconds": preflightTTL, "contentSha256": nullStr(contentSHA), "targetInterface": iface,
		"targetInterfaceDisplay": display, "managedMetric": managedMetric,
		"checks": obj{
			"operationLock": obj{"ok": true, "message": "Конфликтующих операций не обнаружено."},
			"ndmc":          obj{"ok": e.T != nil, "path": transportName(e)},
			"keenetic": obj{"ok": interfaceOK, "available": available, "interface": display, "technicalInterface": iface,
				"connected": connected, "stateUp": up},
			"storage":             obj{"ok": storageOK, "freeKb": free, "requiredKb": required},
			"localSet":            obj{"ok": localOK, "routeCount": routeCount, "duplicateRouteCount": 0, "invalidRouteCount": 0},
			"dnsOverTls":          obj{"required": false, "ok": true, "binding": "none"},
			"keeneticWritePolicy": obj{"ok": writePolicyOK, "path": "static-routes", "checkRc": 0, "protocolSha256": writePolicySHA},
			"ownership":           obj{"ok": planReady, "conflictCount": g("conflicts")},
		},
		"summary": obj{"total": g("total"), "alreadyPresent": g("managedExisting"), "toCreate": g("toCreate"), "toDelete": g("toDelete"),
			"sharedKept": g("sharedKept"), "alreadyAbsent": g("alreadyAbsent"), "conflicts": g("conflicts"),
			"externalKept": g("externalExisting"), "unchanged": g("unchangedRoutes"), "addedToBundle": g("addedRoutes"),
			"removedFromBundle": g("removedRoutes")},
		"resume":  obj{"operation": nullStr(resumeOp), "current": resumeCur, "total": resumeTotal},
		"message": msg,
	}
	if err := writeObj(e.preflightFile(id), res); err != nil {
		return nil, false
	}
	return res, true
}

func transportName(e *engine) any {
	if e.T == nil {
		return nil
	}
	return e.T.Name()
}

// validatePreflight is broray_routes_operation_preflight_validate:
// 2 = required, 3 = expired, 4 = changed, 1 = other failure; 0 = ok
// (the stored preflight is deleted: single use).
func (e *engine) validatePreflight(id, requested, token string) int {
	if !isHex64(token) {
		return 2
	}
	f, ok := readObj(e.preflightFile(id))
	if !ok {
		return 2
	}
	checked, okc := intOf(f["checkedEpoch"])
	ttl, okt := intOf(f["expiresAfterSeconds"])
	if intOr(f["schemaVersion"], 0) != 1 || str(f, "bundleId") != id || str(f, "requestedAction") != requested ||
		str(f, "token") != token || f["ready"] != true {
		return 2
	}
	if _, isNum := numOf(f["checkedEpoch"]); !isNum {
		return 2
	}
	if _, isNum := numOf(f["expiresAfterSeconds"]); !isNum {
		return 2
	}
	if !okc || !okt || checked < 0 || ttl < 0 {
		return 3
	}
	age := int(nowFn().Unix()) - checked
	if age < 0 || age > ttl {
		return 3
	}
	if str(sub(sub(f, "checks"), "keeneticWritePolicy"), "protocolSha256") != writePolicySHA {
		return 4
	}
	content := str(f, "contentSha256")
	switch requested {
	case "export":
		s, _ := readObj(e.stateFile(id))
		cur := str(sub(s, "downloadedVersion"), "contentSha256")
		if cur == "" || cur != content {
			return 4
		}
	case "delete":
		r, _ := readObj(e.registryFile(id))
		cur := str(sub(r, "installedVersion"), "contentSha256")
		if cur == "" || cur != content {
			return 4
		}
	case "resume":
		p, ok := readObj(e.progressFile(id))
		if !ok {
			return 4
		}
		rs := sub(f, "resume")
		if intOr(rs["current"], 0) != intOr(p["current"], 0) || intOr(rs["total"], 0) != intOr(p["total"], 0) || str(rs, "operation") != str(p, "operation") {
			return 4
		}
	}
	if err := os.Remove(e.preflightFile(id)); err != nil {
		return 1
	}
	return 0
}

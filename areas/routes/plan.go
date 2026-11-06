package routes

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// planRoute is one export-plan route (lib/routes-export-build.sh).
type planRoute struct {
	Key     string
	Network string
	Prefix  int
	Mask    string
	Iface   string
	Metric  int
	Comment string
}

func routeKey(network string, prefix int, iface string) string {
	return "ipv4|" + network + "/" + strconv.Itoa(prefix) + "|" + iface + "|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified"
}

func (r planRoute) dest() string { return r.Network + "/" + strconv.Itoa(r.Prefix) }

func (r planRoute) object() obj {
	return obj{"key": r.Key, "family": "ipv4", "network": r.Network, "prefix": r.Prefix, "mask": r.Mask, "targetInterface": r.Iface,
		"gatewayToken": "0.0.0.0", "metric": r.Metric, "automatic": nil, "exclusive": nil, "comment": r.Comment}
}

// registryRoute converts a global registry entry into a planRoute.
func registryRoute(m obj) planRoute {
	metric := intOr(m["metric"], managedMetric)
	comment := str(m, "comment")
	if comment == "" {
		comment = "BROray"
	}
	return planRoute{Key: str(m, "key"), Network: str(m, "network"), Prefix: intOr(m["prefix"], 0), Mask: str(m, "mask"),
		Iface: str(m, "interface"), Metric: metric, Comment: comment}
}

func registryEntry(r planRoute, owner, now string) obj {
	return obj{"key": r.Key, "family": "ipv4", "network": r.Network, "prefix": r.Prefix, "mask": r.Mask, "interface": r.Iface,
		"gatewayToken": "0.0.0.0", "metric": r.Metric, "automatic": nil, "comment": r.Comment, "createdByBROray": true,
		"managed": true, "routerCommentPersisted": false, "actualStatus": "present", "owners": []any{owner},
		"createdAt": now, "updatedAt": now}
}

// catalogValid is broray_routes_runtime_catalog_valid (built-in bundles).
func (e *engine) catalogValid(id string) bool {
	cat := e.catalogDir(id)
	for _, f := range []string{"routes.json", "version.json", "source-files.json"} {
		if !regularFile(filepath.Join(cat, f)) {
			return false
		}
	}
	r, ok1 := readObj(filepath.Join(cat, "routes.json"))
	v, ok2 := readObj(filepath.Join(cat, "version.json"))
	var fa any
	if !ok1 || !ok2 || readJSONAny(filepath.Join(cat, "source-files.json"), &fa) != nil {
		return false
	}
	files, isArr := fa.([]any)
	if !isArr || len(files) == 0 {
		return false
	}
	if intOr(r["schemaVersion"], 0) != 1 || intOr(v["schemaVersion"], 0) != 1 || str(r, "bundleId") != id || str(v, "bundleId") != id {
		return false
	}
	if !isHex64(str(r, "contentSha256")) || str(r, "contentSha256") != str(v, "contentSha256") {
		return false
	}
	if str(r, "targetInterface") == "" || str(r, "targetInterface") != str(v, "targetInterface") || str(r, "routeComment") != "BROray" {
		return false
	}
	rc, ok := intOf(r["routeCount"])
	routes := anyList(r["routes"])
	if !ok || rc <= 0 || len(routes) != rc || intOr(v["routeCount"], -1) != rc {
		return false
	}
	for _, x := range routes {
		m, _ := x.(obj)
		p, ok := intOf(orNull(m, "prefix"))
		if str(m, "family") != "ipv4" || str(m, "network") == "" || !ok || p < 1 || p > 32 {
			return false
		}
	}
	for _, x := range files {
		m, _ := x.(obj)
		size, ok := numOf(orNull(m, "sizeBytes"))
		if str(m, "name") == "" || !isHex64(str(m, "sha256")) || !ok || size < 0 {
			return false
		}
	}
	return sameJSON(v["sourceFiles"], files) && intOr(v["sourceFileCount"], -1) == len(files)
}

// networkAligned is the build-export alignment check.
func networkAligned(network string, prefix int) bool {
	n, ok := ipToNum(network)
	if !ok || prefix < 1 || prefix > 32 {
		return false
	}
	for _, p := range strings.Split(network, ".") {
		if !allDigits(p) {
			return false
		}
	}
	mask := uint32(0xffffffff) << uint(32-prefix)
	return n&^mask == 0
}

// runBuildExport is `broray-routes build-export <id>`; it writes
// keenetic-routes.bat, export-plan.json and state.exportBuild.
func (e *engine) runBuildExport(id string) (string, []planRoute, obj, error) {
	if isBuiltinID(id) && !e.catalogValid(id) {
		return "", nil, nil, cliErr("ROUTES_CATALOG_INVALID: локальный каталог повреждён или не скачан. Проверить обновления / Скачать заново.")
	}
	if !fileExists(e.bundlesFile()) {
		return "", nil, nil, cliErr("Реестр разрешённых наборов недоступен.")
	}
	if !e.bundleListed(id) {
		return "", nil, nil, cliErr("Набор маршрутов не разрешён: %s", id)
	}
	cfg, ok := e.routesConfig()
	if !ok {
		return "", nil, nil, cliErr("Конфигурация маршрутов недоступна.")
	}
	cat := e.catalogDir(id)
	if !fileExists(filepath.Join(cat, "routes.json")) {
		return "", nil, nil, cliErr("Проверенная версия набора не скачана.")
	}
	if !fileExists(filepath.Join(cat, "version.json")) {
		return "", nil, nil, cliErr("Сведения о скачанной версии отсутствуют.")
	}
	state, ok := readObj(e.stateFile(id))
	if !ok {
		return "", nil, nil, cliErr("Состояние набора недоступно.")
	}
	iface := str(cfg, "managedInterface")
	if !reProxy.MatchString(iface) {
		return "", nil, nil, cliErr("Некорректный управляемый интерфейс Keenetic.")
	}
	if str(cfg, "routeComment") != "BROray" {
		return "", nil, nil, cliErr("Некорректная метка маршрутов BROray.")
	}
	pol := sub(cfg, "ownershipPolicy")
	if intOr(cfg["schemaVersion"], 0) != 1 || intOr(cfg["managedMetric"], 0) != managedMetric || pol == nil ||
		pol["touchOtherInterfaces"] != false || pol["modifyExternalRoutes"] != false || pol["deleteExternalRoutes"] != false {
		return "", nil, nil, cliErr("Политика защиты пользовательских маршрутов повреждена.")
	}
	r, _ := readObj(filepath.Join(cat, "routes.json"))
	v, _ := readObj(filepath.Join(cat, "version.json"))
	if str(r, "targetInterface") != iface {
		return "", nil, nil, cliErr("Каталог предназначен не для %s.", iface)
	}
	if str(r, "routeComment") != "BROray" {
		return "", nil, nil, cliErr("Метка каталога не совпадает с политикой BROray.")
	}
	contentSHA := str(r, "contentSha256")
	if contentSHA == "" || contentSHA != str(v, "contentSha256") {
		return "", nil, nil, cliErr("Версия каталога не прошла сверку SHA-256.")
	}
	if !isHex64(strings.ToLower(contentSHA)) {
		return "", nil, nil, cliErr("Некорректный SHA-256 каталога.")
	}
	count, ok := intOf(r["routeCount"])
	if !ok || count < 0 {
		return "", nil, nil, cliErr("Некорректное количество маршрутов.")
	}
	if count == 0 {
		return "", nil, nil, cliErr("Каталог маршрутов пуст.")
	}
	list := anyList(r["routes"])
	if intOr(r["schemaVersion"], 0) != 1 || str(r, "bundleId") != id || len(list) != count {
		return "", nil, nil, cliErr("Каталог маршрутов повреждён.")
	}
	if e.rollbackBlocked() {
		return "", nil, nil, cliErr(msgRollbackBlock)
	}
	seen := map[string]bool{}
	routes := make([]planRoute, 0, count)
	var bat strings.Builder
	for _, x := range list {
		m, _ := x.(obj)
		p, ok := intOf(orNull(m, "prefix"))
		if str(m, "family") != "ipv4" || !ok || p < 1 || p > 32 {
			return "", nil, nil, cliErr("Каталог маршрутов повреждён.")
		}
		net := str(m, "network")
		d := net + "/" + strconv.Itoa(p)
		if seen[d] {
			return "", nil, nil, cliErr("В каталоге обнаружены дубли маршрутов.")
		}
		seen[d] = true
		if !networkAligned(net, p) {
			return "", nil, nil, cliErr("Не удалось сформировать файл Keenetic.")
		}
		mask := maskString(p)
		bat.WriteString("route add " + net + " mask " + mask + " 0.0.0.0 metric 1200 :: rem BROray\r\n")
		routes = append(routes, planRoute{Key: routeKey(net, p, iface), Network: net, Prefix: p, Mask: mask, Iface: iface, Metric: managedMetric, Comment: "BROray"})
	}
	norm, err := os.ReadFile(filepath.Join(cat, "normalized.txt"))
	if err != nil || sha256Hex(norm) != contentSHA {
		return "", nil, nil, cliErr("Нормализованный каталог изменился во время операции.")
	}
	batBytes := []byte(bat.String())
	batSHA := sha256Hex(batBytes)
	now := stamp()
	planRoutes := make([]any, len(routes))
	for i, rt := range routes {
		planRoutes[i] = rt.object()
	}
	plan := obj{"schemaVersion": 1, "bundleId": id, "sourceCommit": str(v, "sourceCommit"), "sourceDate": str(v, "sourceDate"),
		"contentSha256": contentSHA, "targetInterface": iface, "managedMetric": managedMetric, "routeComment": "BROray",
		"routeFile": "keenetic-routes.bat", "routeFileSha256": batSHA, "routeCount": count, "preparedAt": now,
		"routerApplied": false, "routes": planRoutes}
	if err := writeLight(filepath.Join(cat, "keenetic-routes.bat"), batBytes); err != nil {
		return "", nil, nil, cliErr("Не удалось сохранить файл Keenetic.")
	}
	if err := writeObj(filepath.Join(cat, "export-plan.json"), plan); err != nil {
		return "", nil, nil, cliErr("Не удалось сохранить план экспорта.")
	}
	state["exportBuild"] = obj{"result": "prepared", "message": "Маршруты готовы к экспорту", "sourceCommit": str(v, "sourceCommit"),
		"contentSha256": contentSHA, "routeFileSha256": batSHA, "routeFile": filepath.Join(cat, "keenetic-routes.bat"),
		"planFile": filepath.Join(cat, "export-plan.json"), "routeCount": count, "targetInterface": iface, "managedMetric": managedMetric,
		"routeComment": "BROray", "routerApplied": false, "preparedAt": now}
	state["lastError"] = nil
	state["updatedAt"] = now
	if err := writeObj(e.stateFile(id), state); err != nil {
		return "", nil, nil, cliErr("Не удалось сохранить локальное состояние.")
	}
	out := strings.Join([]string{"Маршруты готовы к экспорту", "Набор: " + id, "Интерфейс будущего экспорта: " + iface,
		"Маршрутов: " + itoa(count), "Метка: BROray", "Файл: " + filepath.Join(cat, "keenetic-routes.bat"),
		"План: " + filepath.Join(cat, "export-plan.json"), "Маршруты Keenetic не изменялись."}, "\n") + "\n"
	return out, routes, plan, nil
}

// ensureGlobalRegistry validates installed/routes.json or rebuilds it from
// the bundle registries and export plans (routes-router-sync.sh:978-1081).
func (e *engine) ensureGlobalRegistry(iface string) bool {
	if g, ok := readObj(e.globalRegistryFile()); ok {
		routes, isArr := g["routes"].([]any)
		if isArr && len(routes) == 0 && intOr(g["schemaVersion"], 0) == 1 && str(g, "managedInterface") != iface {
			g["managedInterface"] = iface
			return writeObj(e.globalRegistryFile(), g) == nil
		}
		if intOr(g["schemaVersion"], 0) != 1 || str(g, "managedInterface") != iface || intOr(g["managedMetric"], managedMetric) != managedMetric || !isArr {
			return false
		}
		keys := map[string]bool{}
		for _, x := range routes {
			m, _ := x.(obj)
			k, ok := m["key"].(string)
			if !ok || keys[k] || str(m, "interface") != iface || intOr(m["metric"], managedMetric) != managedMetric ||
				m["createdByBROray"] != true || m["managed"] != true || len(strList(m["owners"])) == 0 {
				return false
			}
			if _, isArr := m["owners"].([]any); !isArr {
				return false
			}
			keys[k] = true
		}
		return true
	}
	if fileExists(e.globalRegistryFile()) {
		return false
	}
	now := stamp()
	byKey := map[string]obj{}
	var order []string
	entries, _ := os.ReadDir(e.p("installed", "bundles"))
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		reg, ok := readObj(e.p("installed", "bundles", en.Name()))
		id := str(reg, "bundleId")
		if !ok || !validBundleID(id) || intOr(reg["schemaVersion"], 0) != 1 {
			return false
		}
		if _, isArr := reg["managedRouteKeys"].([]any); !isArr {
			return false
		}
		keys := strList(reg["managedRouteKeys"])
		if len(keys) == 0 {
			continue
		}
		if reg["installedVersion"] == nil {
			return false
		}
		plan, ok := readObj(filepath.Join(e.catalogDir(id), "export-plan.json"))
		if !ok {
			return false
		}
		want := map[string]bool{}
		for _, k := range keys {
			want[k] = true
		}
		claimed := 0
		for _, x := range anyList(plan["routes"]) {
			m, _ := x.(obj)
			k := str(m, "key")
			if !want[k] {
				continue
			}
			claimed++
			r := planRoute{Key: k, Network: str(m, "network"), Prefix: intOr(m["prefix"], 0), Mask: str(m, "mask"), Iface: str(m, "targetInterface"),
				Metric: intOr(m["metric"], managedMetric), Comment: str(m, "comment")}
			if cur, ok := byKey[k]; ok {
				cur["owners"] = toAny(sortedUnique(append(strList(cur["owners"]), id)))
				cur["updatedAt"] = now
			} else {
				ent := registryEntry(r, id, now)
				ent["actualStatus"] = "unknown"
				byKey[k] = ent
				order = append(order, k)
			}
		}
		if claimed < len(keys) {
			return false
		}
	}
	sort.Strings(order) // jq to_entries of an object: keys sorted
	routes := make([]any, 0, len(order))
	for _, k := range order {
		routes = append(routes, byKey[k])
	}
	return writeObj(e.globalRegistryFile(), obj{"schemaVersion": 1, "managedInterface": iface, "managedMetric": managedMetric, "routes": routes, "updatedAt": now}) == nil
}

type desiredItem struct {
	Route      planRoute
	Status     string
	Identity   int
	Attribute  int
	Target     int
	Registered bool
	Owners     []string
}

type obsoleteItem struct {
	Key       string
	Route     *planRoute
	Remaining []string
	Status    string
	Reason    string
	Identity  int
	Attribute int
	Target    int
}

type syncPlan struct {
	BundleID   string
	Mode       string
	Iface      string
	ContentSHA string
	CheckedAt  string
	CanApply   bool
	Summary    obj
	Desired    []desiredItem
	Obsolete   []obsoleteItem
	Blocking   []any
	Message    string
	Snap       *rcSnapshot
}

func (p *syncPlan) object() obj {
	desired := make([]any, len(p.Desired))
	for i, d := range p.Desired {
		desired[i] = d.object()
	}
	obsolete := make([]any, len(p.Obsolete))
	for i, o := range p.Obsolete {
		obsolete[i] = o.object()
	}
	return obj{"schemaVersion": 1, "bundleId": p.BundleID, "mode": p.Mode, "targetInterface": p.Iface, "managedMetric": managedMetric,
		"identityModel": "destination-interface-gateway", "contentSha256": p.ContentSHA, "checkedAt": p.CheckedAt,
		"canApply": p.CanApply, "summary": p.Summary, "desired": desired, "obsolete": obsolete, "blocking": p.Blocking, "message": p.Message}
}

func (d desiredItem) object() obj {
	return obj{"route": d.Route.object(), "status": d.Status, "identityMatchCount": d.Identity, "attributeMatchCount": d.Attribute,
		"targetMatchCount": d.Target, "registered": d.Registered, "existingOwners": toAny(d.Owners)}
}

func (o obsoleteItem) object() obj {
	if o.Route == nil {
		return obj{"key": o.Key, "status": o.Status, "reason": o.Reason}
	}
	return obj{"key": o.Key, "route": o.Route.object(), "remainingOwners": toAny(o.Remaining), "status": o.Status,
		"identityMatchCount": o.Identity, "attributeMatchCount": o.Attribute, "targetMatchCount": o.Target}
}

// planError carries the 3.x stderr markers used by verify.cgi.
type planError struct{ details string }

func (p *planError) Error() string { return p.details }

func planFail(marker, msg string) error {
	if marker != "" {
		return &planError{marker + "\nОШИБКА: " + msg}
	}
	return &planError{"ОШИБКА: " + msg}
}

// buildSyncPlan is broray_routes_sync_build_plan_core (fresh running-config read).
func (e *engine) buildSyncPlan(ctx context.Context, id string) (*syncPlan, error) {
	const failMsg = "Не удалось построить безопасный план установки."
	cfg, _ := e.routesConfig()
	iface := str(cfg, "managedInterface")
	plan, ok1 := readObj(filepath.Join(e.catalogDir(id), "export-plan.json"))
	reg, ok2 := readObj(e.globalRegistryFile())
	breg, ok3 := readObj(e.registryFile(id))
	state, ok4 := readObj(e.stateFile(id))
	if !ok1 || !ok2 || !ok3 || !ok4 || !fileExists(e.bundlesFile()) || cfg == nil {
		return nil, planFail("ROUTES_PLAN_INPUT_UNAVAILABLE", failMsg)
	}
	if !e.bundleListed(id) || !e.policyValid(iface) {
		return nil, planFail("", failMsg)
	}
	if intOr(plan["schemaVersion"], 0) != 1 || str(plan, "bundleId") != id || str(plan, "targetInterface") != iface ||
		intOr(plan["managedMetric"], 0) != managedMetric || str(plan, "routeComment") != "BROray" || len(anyList(plan["routes"])) == 0 {
		return nil, planFail("", failMsg)
	}
	if str(sub(state, "downloadedVersion"), "contentSha256") != str(plan, "contentSha256") {
		return nil, planFail("ROUTES_PLAN_LOCAL_VERSION_MISMATCH", failMsg)
	}
	snap, err := e.fetchConfig(ctx)
	if err != nil {
		return nil, planFail("ROUTES_PLAN_RUNNING_CONFIG_READ_FAILED", failMsg)
	}
	if !snap.complete() {
		var reasons []string
		for _, r := range snap.Routes {
			if !r.Known {
				reasons = append(reasons, r.Reason)
			}
		}
		return nil, planFail("ROUTES_PLAN_RUNNING_CONFIG_UNSUPPORTED count="+itoa(snap.Unknown)+" reasons="+strings.Join(sortedUnique(reasons), ","), failMsg)
	}
	regByKey := map[string][]obj{}
	for _, x := range anyList(reg["routes"]) {
		m, _ := x.(obj)
		regByKey[str(m, "key")] = append(regByKey[str(m, "key")], m)
	}
	var desired []desiredItem
	desiredKeys := map[string]bool{}
	var desiredOrder []string
	for _, x := range anyList(plan["routes"]) {
		m, _ := x.(obj)
		r := planRoute{Key: str(m, "key"), Network: str(m, "network"), Prefix: intOr(m["prefix"], 0), Mask: str(m, "mask"),
			Iface: str(m, "targetInterface"), Metric: intOr(m["metric"], managedMetric), Comment: str(m, "comment")}
		desiredKeys[r.Key] = true
		desiredOrder = append(desiredOrder, r.Key)
		id1 := snap.identityCount(r.dest(), iface)
		at := snap.attributeCount(r.dest(), iface, r.Metric)
		tg := snap.targetCount(r.dest(), iface)
		regs := regByKey[r.Key]
		status := "conflict"
		switch {
		case tg > id1 || id1 > 1 || len(regs) > 1:
		case id1 == 1 && at != 1:
		case id1 == 1:
			if len(regs) == 1 && regs[0]["createdByBROray"] == true && regs[0]["managed"] == true {
				status = "managed_existing"
			} else {
				status = "external_existing"
			}
		case tg == 0:
			status = "create"
		}
		var owners []string
		if len(regs) > 0 {
			owners = sortedUnique(strList(regs[0]["owners"]))
		}
		desired = append(desired, desiredItem{Route: r, Status: status, Identity: id1, Attribute: at, Target: tg, Registered: len(regs) == 1, Owners: owners})
	}
	oldKeys := strList(breg["routeKeys"])
	var obsolete []obsoleteItem
	for _, key := range strList(breg["managedRouteKeys"]) {
		if desiredKeys[key] {
			continue
		}
		regs := regByKey[key]
		if len(regs) != 1 {
			obsolete = append(obsolete, obsoleteItem{Key: key, Status: "conflict", Reason: "registry_missing"})
			continue
		}
		rr := registryRoute(regs[0])
		var remaining []string
		for _, o := range strList(regs[0]["owners"]) {
			if o != id {
				remaining = append(remaining, o)
			}
		}
		remaining = sortedUnique(remaining)
		id1 := snap.identityCount(rr.dest(), iface)
		at := snap.attributeCount(rr.dest(), iface, rr.Metric)
		tg := snap.targetCount(rr.dest(), iface)
		status := "conflict"
		switch {
		case tg > id1 || id1 > 1:
		case id1 == 1 && at != 1:
		case len(remaining) > 0:
			if id1 == 1 {
				status = "shared_keep"
			} else if tg == 0 {
				status = "shared_restore"
			}
		default:
			if id1 == 1 {
				status = "delete"
			} else if tg == 0 {
				status = "already_absent"
			}
		}
		rcopy := rr
		obsolete = append(obsolete, obsoleteItem{Key: key, Route: &rcopy, Remaining: remaining, Status: status, Identity: id1, Attribute: at, Target: tg})
	}
	blocking := []any{}
	for i, d := range desired {
		if d.Status == "conflict" || d.Status == "external_existing" {
			blocking = append(blocking, desired[i].object())
		}
	}
	for _, o := range obsolete {
		if o.Status == "conflict" {
			blocking = append(blocking, o.object())
		}
	}
	oldSet := map[string]bool{}
	for _, k := range oldKeys {
		oldSet[k] = true
	}
	added, unchanged := 0, 0
	for _, k := range sortedUnique(desiredOrder) {
		if oldSet[k] {
			unchanged++
		} else {
			added++
		}
	}
	removed := 0
	for _, k := range sortedUnique(oldKeys) {
		if !desiredKeys[k] {
			removed++
		}
	}
	cnt := func(st string) int {
		n := 0
		for _, d := range desired {
			if d.Status == st {
				n++
			}
		}
		return n
	}
	ocnt := func(st string) int {
		n := 0
		for _, o := range obsolete {
			if o.Status == st {
				n++
			}
		}
		return n
	}
	mode := "none"
	switch {
	case state["installedVersion"] == nil:
		mode = "install"
	case str(sub(state, "installedVersion"), "contentSha256") != str(sub(state, "downloadedVersion"), "contentSha256"):
		mode = "update"
	case cnt("create") > 0:
		mode = "restore"
	}
	msg := "Изменения в Keenetic не требуются."
	switch {
	case len(blocking) > 0:
		msg = "Обнаружены конфликты или внешние маршруты на управляемом интерфейсе."
	case mode == "install":
		msg = "План установки маршрутов подготовлен."
	case mode == "update":
		msg = "План обновления маршрутов подготовлен."
	case mode == "restore":
		msg = "План восстановления маршрутов подготовлен."
	}
	return &syncPlan{BundleID: id, Mode: mode, Iface: iface, ContentSHA: str(plan, "contentSha256"), CheckedAt: stamp(),
		CanApply: len(blocking) == 0, Desired: desired, Obsolete: obsolete, Blocking: blocking, Message: msg, Snap: snap,
		Summary: obj{"total": len(desired), "addedRoutes": added, "removedRoutes": removed, "unchangedRoutes": unchanged,
			"toCreate": cnt("create"), "managedExisting": cnt("managed_existing"), "externalExisting": cnt("external_existing"),
			"conflicts": len(blocking), "toDelete": ocnt("delete"), "sharedKept": ocnt("shared_keep"),
			"sharedToRestore": ocnt("shared_restore"), "alreadyAbsent": ocnt("already_absent")}}, nil
}

// runPlan is `broray-routes plan <id>` = build-export + broray_routes_sync_plan.
func (e *engine) runPlan(ctx context.Context, id string) (*syncPlan, error) {
	if _, _, _, err := e.runBuildExport(id); err != nil {
		return nil, &planError{err.Error()}
	}
	if e.rollbackBlocked() {
		return nil, planFail("", msgRollbackBlock)
	}
	if !e.ensureGlobalRegistry(e.managedInterface()) {
		return nil, planFail("", "ROUTES_PLAN_REGISTRY_INVALID: не удалось восстановить или проверить общий реестр маршрутов.")
	}
	return e.buildSyncPlan(ctx, id)
}

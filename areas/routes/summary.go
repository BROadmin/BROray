package routes

import (
	"context"

	"github.com/BROadmin/broray/internal/compat"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const pageConfigTTL = 30 * time.Second

// versionEqual is version_equal of routes-page-summary.sh:382-391.
func versionEqual(a, b obj) bool {
	if a == nil || b == nil {
		return false
	}
	if str(a, "contentSha256") != "" && str(b, "contentSha256") != "" {
		return str(a, "contentSha256") == str(b, "contentSha256")
	}
	if str(a, "sourceSetSha256") != "" && str(b, "sourceSetSha256") != "" {
		return str(a, "sourceSetSha256") == str(b, "sourceSetSha256")
	}
	return str(a, "sourceCommit") == str(b, "sourceCommit") && str(a, "sourceDate") == str(b, "sourceDate")
}

func orNull(m obj, k string) any {
	if m == nil {
		return nil
	}
	return m[k]
}

// stateProjection is broray_routes_page_collect_states.
func stateProjection(s obj) obj {
	g := func(k string) any { return orNull(s, k) }
	sv := g("schemaVersion")
	if sv == nil {
		sv = 1
	}
	status := g("status")
	if status == nil {
		status = "unknown"
	}
	rc := g("routeCount")
	if rc == nil {
		rc = 0
	}
	return obj{"schemaVersion": sv, "bundleId": g("bundleId"), "status": status, "routeCount": rc,
		"availableVersion": g("availableVersion"), "downloadedVersion": g("downloadedVersion"), "installedVersion": g("installedVersion"),
		"lastCheckedAt": g("lastCheckedAt"), "lastVerifiedAt": g("lastVerifiedAt"), "lastDownloadedAt": g("lastDownloadedAt"),
		"lastExportedAt": g("lastExportedAt"), "lastDeletedAt": g("lastDeletedAt"), "lastError": g("lastError"),
		"checkResult": g("checkResult"), "verifyResult": g("verifyResult"), "downloadResult": g("downloadResult"),
		"exportBuild": g("exportBuild"), "exportResult": g("exportResult"), "deleteResult": g("deleteResult"),
		"customImport": g("customImport"), "scheduledUpdate": g("scheduledUpdate"), "updatedAt": g("updatedAt")}
}

// scheduleReviewRequired: the scheduled check marked the bundle «Требует
// проверки» and the owner has not installed that version since.
func scheduleReviewRequired(state obj) bool {
	su := sub(state, "scheduledUpdate")
	if str(su, "status") != "review_required" {
		return false
	}
	sha := str(su, "contentSha256")
	return sha == "" || sha != str(sub(state, "installedVersion"), "contentSha256")
}

func (e *engine) metadata(id string) (obj, bool) {
	if c, ok := e.customEntry(id); ok {
		def := func(k string, d any) any {
			if v := c[k]; v != nil {
				return v
			}
			return d
		}
		return obj{"id": c["id"], "name": def("name", c["id"]), "description": def("description", "Пользовательский набор маршрутов."),
			"sourceProvider": def("sourceProvider", "local-upload"), "canonicalRouteCount": def("canonicalRouteCount", 0),
			"exportRouteCount": def("exportRouteCount", 0), "sourceFileCount": def("sourceFileCount", 0),
			"createdAt": c["createdAt"], "updatedAt": c["updatedAt"]}, true
	}
	mf, ok := readObj(e.manifestFile(id))
	if !ok {
		return nil, false
	}
	src := sub(mf, "source")
	files := 0
	for _, f := range anyList(orNull(src, "files")) {
		if fm, ok := f.(obj); ok {
			if en, ok := fm["enabled"].(bool); !ok || en {
				files++
			}
		}
	}
	name := mf["name"]
	if name == nil {
		name = mf["id"]
	}
	provider := orNull(src, "provider")
	if provider == nil {
		provider = "github"
	}
	idv := mf["id"]
	if idv == nil {
		idv = ""
	}
	return obj{"id": idv, "name": name, "description": nil, "sourceProvider": provider,
		"sourceDirectory": orNull(src, "directory"), "sourceFileCount": files, "targetInterface": mf["targetInterface"]}, true
}

func (e *engine) scopeIDs(scope string) []string {
	switch scope {
	case "catalog":
		return builtinIDs()
	case "custom":
		return e.customIDs()
	}
	return append(builtinIDs(), e.customIDs()...)
}

func expectedKey(key string) string {
	p := strings.Split(key, "|")
	get := func(i int) string {
		if i < len(p) {
			return p[i]
		}
		return ""
	}
	metric := 1200
	seg := "metric:1200"
	if len(p) > 4 {
		seg = p[4]
	}
	if parts := strings.SplitN(seg, ":", 3); len(parts) >= 2 {
		if n, err := strconv.ParseFloat(parts[1], 64); err == nil {
			metric = int(n)
		}
	}
	return get(1) + "|" + get(2) + "|" + strconv.Itoa(metric)
}

// pageSummary is broray_routes_page_summary <scope>.
func (e *engine) pageSummary(ctx context.Context, scope string) obj {
	gop := e.globalOperation()
	opStatus := e.operationStatus()
	snap, err := e.configSnapshot(ctx, pageConfigTTL)
	actualAvailable := err == nil && snap != nil
	actualIndex := map[string]int{}
	var actualRoutes []rcRoute
	source, fetchedAt, fetchedEpoch := "unavailable", any(stamp()), int64(0)
	if actualAvailable {
		source, fetchedAt, fetchedEpoch = snap.Source, snap.FetchedAt, snap.FetchedEpoch
		actualRoutes = snap.Routes
		for _, r := range snap.Routes {
			if r.Known && r.Gateway == "0.0.0.0" {
				actualIndex[r.Destination+"|"+r.Interface+"|"+strconv.Itoa(r.metricOr(1000))]++
			} else if !r.Known {
				// unknown lines have null destination/interface: key "||1000"
				actualIndex["||1000"]++
			}
		}
	}
	reg, ok := readObj(e.globalRegistryFile())
	if !ok {
		reg = obj{"routes": []any{}}
	}
	regRoutes := anyList(reg["routes"])
	cfg, _ := e.routesConfig()
	mi := str(cfg, "managedInterface")
	metricCfg := managedMetric
	if cfg != nil {
		if n, ok := intOf(cfg["managedMetric"]); ok && n >= 0 {
			metricCfg = n
		}
	}
	activeProgress := sub(opStatus, "progress")
	if activeProgress == nil {
		activeProgress = obj{}
	}
	opBundle, _ := opStatus["bundleId"].(string)

	bundles := []any{}
	var bundleObjs []obj
	for _, id := range e.scopeIDs(scope) {
		meta, ok := e.metadata(id)
		if !ok {
			continue
		}
		mid, _ := meta["id"].(string)
		state := obj{"bundleId": mid, "status": "not_checked", "routeCount": 0}
		if s, ok := readObj(e.stateFile(id)); ok {
			if _, ok := s["bundleId"].(string); ok && str(s, "bundleId") == mid {
				state = stateProjection(s)
			}
		}
		var expected []string
		var bundleReg obj
		if r, ok := readObj(e.registryFile(id)); ok && str(r, "bundleId") == mid {
			bundleReg = r
			for _, k := range strList(r["routeKeys"]) {
				expected = append(expected, expectedKey(k))
			}
		}
		present, missing, dup := 0, 0, 0
		for _, k := range expected {
			switch n := actualIndex[k]; {
			case n == 1:
				present++
			case n == 0:
				missing++
			default:
				dup++
			}
		}
		expectedCount := len(expected)
		registered := bundleReg != nil && bundleReg["installedVersion"] != nil && expectedCount > 0
		unique, shared, owned := 0, 0, 0
		for _, x := range regRoutes {
			rm, _ := x.(obj)
			owners := strList(orNull(rm, "owners"))
			has := false
			for _, o := range owners {
				if o == mid {
					has = true
				}
			}
			if !has {
				continue
			}
			owned++
			if len(owners) == 1 {
				unique++
			} else if len(owners) > 1 {
				shared++
			}
		}
		available, downloaded, installed := sub(state, "availableVersion"), sub(state, "downloadedVersion"), sub(state, "installedVersion")
		downloadRequired := available != nil && !versionEqual(available, downloaded)
		keeneticUpdateRequired := downloaded != nil && (installed == nil || !versionEqual(downloaded, installed))
		complete := expectedCount > 0 && missing == 0 && dup == 0
		drift := registered && actualAvailable && !complete
		verification := currentVerification(state, actualAvailable, registered, complete)
		attention := state["lastError"] != nil || drift || downloadRequired || keeneticUpdateRequired || (verification == nil && downloaded != nil) ||
			scheduleReviewRequired(state)

		card := cloneObj(state)
		card["verifyResult"] = nil
		card["lastVerifiedAt"] = nil
		if verification != nil {
			card["verifyResult"] = verification
			card["lastVerifiedAt"] = state["lastVerifiedAt"]
		}
		card["id"] = mid
		card["metadata"] = meta
		var cacheAge any
		if fetchedEpoch > 0 {
			cacheAge = nowFn().Unix() - fetchedEpoch
		}
		ifA := func(v any) any {
			if actualAvailable {
				return v
			}
			return nil
		}
		var completeV, actualInstalled, driftV any
		if actualAvailable && expectedCount > 0 {
			completeV = complete
		}
		status := "partial"
		switch {
		case !actualAvailable:
			status = "unavailable"
		case !registered:
			status, actualInstalled = "not_registered", false
		case complete:
			status, actualInstalled = "complete", true
		case present == 0:
			status, actualInstalled = "absent", false
		}
		if actualAvailable && registered {
			driftV = drift
		}
		card["routerPresence"] = obj{"available": actualAvailable, "registered": registered, "source": source, "checkedAt": fetchedAt,
			"cacheAgeSeconds": cacheAge, "expectedRouteCount": expectedCount, "presentRouteCount": ifA(present),
			"missingRouteCount": ifA(missing), "duplicateRouteCount": ifA(dup), "complete": completeV,
			"actualInstalled": actualInstalled, "drift": driftV, "status": status, "missingRoutes": []any{}}
		card["ownership"] = obj{"uniqueRouteCount": unique, "sharedRouteCount": shared, "totalOwnedRouteCount": owned}
		card["recordedInstalled"] = installed != nil
		card["verifiedInstalled"] = actualAvailable && complete && registered
		card["sourceUpdateAvailable"] = downloadRequired
		card["keeneticUpdateRequired"] = keeneticUpdateRequired
		card["actionRequired"] = attention
		card["attention"] = attention
		if opBundle == mid && opBundle != "" {
			card["operationProgress"] = activeProgress
		} else {
			card["operationProgress"] = idleProgress(mid)
		}
		card["globalOperation"] = gop
		bundles = append(bundles, card)
		bundleObjs = append(bundleObjs, card)
	}

	count := func(pred func(obj) bool) int {
		n := 0
		for _, b := range bundleObjs {
			if pred(b) {
				n++
			}
		}
		return n
	}
	sumPresence := func(k string) int {
		n := 0
		for _, b := range bundleObjs {
			n += intOr(sub(b, "routerPresence")[k], 0)
		}
		return n
	}
	sumOwn := func(k string) int {
		n := 0
		for _, b := range bundleObjs {
			n += intOr(sub(b, "ownership")[k], 0)
		}
		return n
	}
	verified := count(func(b obj) bool { return b["verifiedInstalled"] == true })
	recorded := count(func(b obj) bool { return b["recordedInstalled"] == true })
	attentionCount := count(func(b obj) bool { return b["attention"] == true })
	errorCount := count(func(b obj) bool { return b["lastError"] != nil })
	presentTotal := sumPresence("presentRouteCount")

	// health (routes-page-summary.sh:511-547)
	busy := boolOf(gop["running"]) // never produced: "busy" unreachable as in 3.x
	severity := "ok"
	switch {
	case errorCount > 0:
		severity = "error"
	case busy:
		severity = "busy"
	case !actualAvailable || attentionCount > 0:
		severity = "warning"
	}
	consistent := actualAvailable
	for _, b := range bundleObjs {
		if !((b["recordedInstalled"] != true || b["verifiedInstalled"] == true) && b["keeneticUpdateRequired"] != true) {
			consistent = false
		}
	}
	type reason struct{ code, message, details string }
	var reasons []reason
	if !actualAvailable {
		reasons = append(reasons, reason{"KEENETIC_ROUTES_UNAVAILABLE", "Не удалось получить фактические маршруты Keenetic.", ""})
	}
	for _, b := range bundleObjs {
		id, _ := b["id"].(string)
		name := id
		if n, ok := sub(b, "metadata")["name"].(string); ok && n != "" {
			name = n
		}
		switch {
		case b["lastError"] != nil:
			code, msg := "ROUTES_ERROR", "Ошибка набора маршрутов."
			if le, ok := b["lastError"].(obj); ok {
				if c, ok := le["code"].(string); ok && c != "" {
					code = c
				}
				if m, ok := le["message"].(string); ok && m != "" {
					msg = m
				}
			}
			reasons = append(reasons, reason{code, msg, id})
		case b["keeneticUpdateRequired"] == true:
			reasons = append(reasons, reason{"KEENETIC_UPDATE_REQUIRED", "Набор «" + name + "» требует установки или обновления в Keenetic.", id})
		case sub(b, "routerPresence")["drift"] == true:
			reasons = append(reasons, reason{"ROUTES_DRIFT", "Фактические маршруты набора «" + name + "» расходятся с реестром BROray.", id})
		case b["attention"] == true:
			reasons = append(reasons, reason{"ROUTES_ATTENTION", "Набор «" + name + "» требует проверки.", id})
		}
	}
	// unique_by(.code + "|" + (.details // "")): sorted by that key, first kept
	sort.SliceStable(reasons, func(i, j int) bool {
		return reasons[i].code+"|"+reasons[i].details < reasons[j].code+"|"+reasons[j].details
	})
	reasonList := []any{}
	seen := map[string]bool{}
	for _, r := range reasons {
		k := r.code + "|" + r.details
		if seen[k] {
			continue
		}
		seen[k] = true
		reasonList = append(reasonList, obj{"code": r.code, "message": r.message, "details": nullStr(r.details)})
	}
	availability, fresh := "available", "fresh"
	if !actualAvailable {
		availability, fresh = "partial", "unknown"
	}
	var checkedAt any
	if actualAvailable {
		checkedAt = fetchedAt
	} else {
		checkedAt = fetchedAt // the unavailable placeholder also carries fetchedAt
	}
	health := obj{"schemaVersion": 1, "module": "routes", "availability": availability, "severity": severity,
		"operational": actualAvailable && errorCount == 0, "consistent": consistent,
		"actionRequired": !actualAvailable || attentionCount > 0,
		"freshness":      obj{"state": fresh, "checkedAt": checkedAt}, "reasons": reasonList,
		"facts": obj{"bundleCount": len(bundleObjs), "verifiedInstalledCount": verified, "recordedInstalledCount": recorded,
			"attentionCount": attentionCount, "presentRouteCount": presentTotal},
		"lastOperation": opStatus}
	var srcV any = source
	routeCount := len(actualRoutes)
	return obj{
		"schemaVersion": 1, "scope": scope, "generatedAt": stamp(), "managedInterface": nullStr(mi),
		"managedInterfaceDisplay": e.interfaceDisplay(), "managedMetric": metricCfg,
		"routerSnapshot":  obj{"available": actualAvailable, "source": srcV, "checkedAt": fetchedAt, "routeCount": routeCount},
		"globalOperation": gop, "operation": opStatus,
		"totals": obj{"bundleCount": len(bundleObjs), "installedCount": verified, "verifiedInstalledCount": verified,
			"recordedInstalledCount": recorded, "registeredCount": count(func(b obj) bool { return sub(b, "routerPresence")["registered"] == true }),
			"attentionCount": attentionCount, "expectedRouteCount": sumPresence("expectedRouteCount"), "presentRouteCount": presentTotal,
			"uniqueRouteCount": sumOwn("uniqueRouteCount"), "sharedOwnershipCount": sumOwn("sharedRouteCount")},
		"health":  health,
		"bundles": bundles,
	}
}

// currentVerification filters verifyResult to the one still describing the
// downloaded version and the present router state (routes-page-summary.sh:434-457).
func currentVerification(state obj, actualAvailable, registered, complete bool) obj {
	v := sub(state, "verifyResult")
	downloaded := sub(state, "downloadedVersion")
	if v == nil || downloaded == nil {
		return nil
	}
	if sub(v, "local")["valid"] != true {
		return nil
	}
	if str(v, "contentSha256") == "" || str(v, "contentSha256") != str(downloaded, "contentSha256") {
		return nil
	}
	k := sub(v, "keenetic")
	status := str(k, "status")
	in := func(list ...string) bool {
		for _, s := range list {
			if s == status {
				return true
			}
		}
		return false
	}
	if !actualAvailable {
		avail, has := k["available"]
		if has && avail == false && status == "unavailable" {
			return v
		}
		return nil
	}
	if k["available"] != true {
		return nil
	}
	if registered {
		if complete {
			if in("complete", "update_pending", "conflict") {
				return v
			}
			return nil
		}
		if in("restore_required", "conflict") {
			return v
		}
		return nil
	}
	if in("not_installed", "conflict") {
		return v
	}
	return nil
}

// overview is broray_routes_page_overview.
func (e *engine) overview(ctx context.Context) obj {
	s := e.pageSummary(ctx, "all")
	reg, ok := readObj(e.globalRegistryFile())
	routes := []any{}
	if ok {
		routes = anyList(reg["routes"])
	}
	shared := 0
	for _, x := range routes {
		if rm, ok := x.(obj); ok && len(anyList(rm["owners"])) > 1 {
			shared++
		}
	}
	totals := func(custom bool) obj {
		total, inst, att, rc := 0, 0, 0, 0
		for _, x := range anyList(s["bundles"]) {
			b := x.(obj)
			id, _ := b["id"].(string)
			if strings.HasPrefix(id, "user-") != custom {
				continue
			}
			total++
			if b["verifiedInstalled"] == true {
				inst++
			}
			if b["attention"] == true {
				att++
			}
			rc += intOr(b["routeCount"], 0)
		}
		return obj{"total": total, "installed": inst, "attention": att, "routeCount": rc}
	}
	var updated []string
	for _, x := range anyList(s["bundles"]) {
		if u, ok := x.(obj)["updatedAt"].(string); ok {
			updated = append(updated, u)
		}
	}
	sort.Strings(updated)
	var last any
	if len(updated) > 0 {
		last = updated[len(updated)-1]
	}
	return obj{"schemaVersion": 1, "generatedAt": s["generatedAt"], "managedInterface": s["managedInterface"],
		"managedInterfaceDisplay": s["managedInterfaceDisplay"], "globalOperation": s["globalOperation"], "operation": s["operation"],
		"totalManagedRoutes": len(routes), "sharedRoutes": shared, "custom": totals(true), "catalog": totals(false),
		"lastUpdatedAt": last, "health": s["health"], "ipv6": e.ipv6(ctx)}
}

// ipv6 is the keenetic area's cached IPv6 fact (additive, 3.5.0): route sets
// are IPv4 only, so the page warns when the router has global IPv6.
func (e *engine) ipv6(ctx context.Context) map[string]any {
	if e.d == nil || e.d.Svc == nil {
		return (&compat.Services{}).IPv6(ctx)
	}
	return e.d.Svc.IPv6(ctx)
}

// homeSnapshot is the bin/broray-home-snapshot routes projection.
func (e *engine) homeSnapshot(ctx context.Context) (map[string]any, error) {
	page := e.pageSummary(ctx, "all")
	t := sub(page, "totals")
	h := sub(page, "health")
	// 3.x `($page.health.actionRequired // true)` turned false into true and
	// `$page.globalOperation.running // false` was never set; 3.5.0 reports
	// the evident intent (deliberate deviation).
	actionRequired := true
	if v, ok := h["actionRequired"].(bool); ok {
		actionRequired = v
	}
	op := sub(page, "operation")
	gop := sub(page, "globalOperation")
	operationRunning := boolOf(op["running"]) || (boolOf(gop["active"]) && !boolOf(gop["pending"]) && str(gop, "scope") == "routes")
	mi := page["managedInterface"]
	if mi == nil {
		mi = "ProxyN"
	}
	return map[string]any{"schemaVersion": 2, "availableBundles": t["bundleCount"], "installedBundles": t["verifiedInstalledCount"],
		"verifiedInstalledBundles": t["verifiedInstalledCount"], "recordedInstalledBundles": t["recordedInstalledCount"],
		"installedRouteCount": t["presentRouteCount"], "attentionCount": t["attentionCount"], "actionRequired": actionRequired,
		"operationRunning": operationRunning, "managedInterface": mi,
		"routerSnapshot": page["routerSnapshot"], "bundles": page["bundles"], "health": h, "updatedAt": page["generatedAt"]}, nil
}

// presence is broray_routes_presence_bundle (missingRoutes ≤ 50).
func (e *engine) presence(ctx context.Context, id string, ttl time.Duration) (obj, bool) {
	r, ok := readObj(e.registryFile(id))
	if !ok || intOr(r["schemaVersion"], 0) != 1 || str(r, "bundleId") != id {
		return nil, false
	}
	keys, isArr := r["routeKeys"].([]any)
	if !isArr {
		return nil, false
	}
	if len(keys) == 0 {
		return obj{"available": true, "registered": false, "source": "running-config", "checkedAt": stamp(), "cacheAgeSeconds": 0,
			"expectedRouteCount": 0, "presentRouteCount": 0, "missingRouteCount": 0, "duplicateRouteCount": 0, "complete": false,
			"actualInstalled": false, "drift": false, "status": "not_registered", "missingRoutes": []any{}}, true
	}
	snap, err := e.configSnapshot(ctx, ttl)
	if err != nil {
		return obj{"available": false, "registered": true, "source": "running-config", "checkedAt": stamp(), "cacheAgeSeconds": nil,
			"expectedRouteCount": len(keys), "presentRouteCount": nil, "missingRouteCount": nil, "duplicateRouteCount": nil,
			"complete": nil, "actualInstalled": nil, "drift": nil, "status": "unavailable", "missingRoutes": []any{}}, true
	}
	bundleIface := str(r, "targetInterface")
	present, missing, dup := 0, 0, 0
	missingList := []any{}
	for _, kv := range keys {
		key, _ := kv.(string)
		parts := strings.Split(key, "|")
		dest := ""
		if len(parts) > 1 {
			dest = parts[1]
		}
		iface := bundleIface
		if len(parts) > 2 {
			iface = parts[2]
		}
		metric := 1200
		seg := "metric:1200"
		if len(parts) > 4 {
			seg = parts[4]
		}
		if sp := strings.SplitN(seg, ":", 3); len(sp) >= 2 {
			if n, err := strconv.ParseFloat(sp[1], 64); err == nil {
				metric = int(n)
			}
		}
		matches := 0
		for _, rt := range snap.Routes {
			d, i := "", ""
			if rt.Known {
				d, i = rt.Destination, rt.Interface
			}
			gw := rt.Gateway
			if !rt.Known {
				gw = "0.0.0.0"
			}
			if d == dest && i == iface && gw == "0.0.0.0" && rt.metricOr(1000) == metric {
				matches++
			}
		}
		if matches == 1 {
			present++
		} else {
			missing++
			if len(missingList) < 50 {
				missingList = append(missingList, obj{"key": key, "destination": dest, "interface": iface, "metric": metric, "matchCount": matches})
			}
		}
		if matches > 1 {
			dup++
		}
	}
	complete := missing == 0 && dup == 0 && len(keys) > 0
	var actualInstalled any
	status := "partial"
	switch {
	case complete:
		actualInstalled, status = true, "complete"
	case present == 0:
		actualInstalled, status = false, "absent"
	}
	return obj{"available": true, "registered": true, "source": "running-config", "checkedAt": snap.FetchedAt,
		"cacheAgeSeconds": nowFn().Unix() - snap.FetchedEpoch, "expectedRouteCount": len(keys), "presentRouteCount": present,
		"missingRouteCount": missing, "duplicateRouteCount": dup, "complete": complete, "actualInstalled": actualInstalled,
		"drift": !complete, "status": status, "missingRoutes": missingList}, true
}

// ---------- summary.cgi (lib/routes-summary.sh) ----------

type summaryError struct{ code, message string }

func (s *summaryError) Error() string { return s.code + ":" + s.message }

func routeVersionKey(v obj) string {
	switch {
	case str(v, "contentSha256") != "":
		return "sha256:" + str(v, "contentSha256")
	case str(v, "sourceCommit") != "":
		return "commit:" + str(v, "sourceCommit")
	case str(v, "sourceDate") != "":
		return "date:" + str(v, "sourceDate")
	}
	b, _ := compat.Canonical(v)
	return strings.TrimSpace(string(b))
}

func sourceVersionKey(v obj) string {
	if str(v, "sourceSetSha256") != "" {
		return "sources:" + str(v, "sourceSetSha256")
	}
	return routeVersionKey(v)
}

func freshnessFromAge(age int64, fresh, stale int64) string {
	switch {
	case age <= fresh:
		return "fresh"
	case age <= stale:
		return "stale"
	}
	return "expired"
}

func (e *engine) bundleSummary(id string) (obj, *summaryError) {
	if !validBundleID(id) || !e.bundleListed(id) {
		return nil, &summaryError{"ROUTES_BUNDLE_NOT_FOUND", "Набор маршрутов не найден."}
	}
	s, ok := readObj(e.stateFile(id))
	if !fileExists(e.stateFile(id)) {
		return nil, &summaryError{"ROUTES_STATE_UNAVAILABLE", "Локальное состояние маршрутов недоступно."}
	}
	if !fileExists(e.registryFile(id)) {
		return nil, &summaryError{"ROUTES_BUNDLE_REGISTRY_UNAVAILABLE", "Реестр набора маршрутов недоступен."}
	}
	if !fileExists(e.configFile()) {
		return nil, &summaryError{"ROUTES_CONFIG_UNAVAILABLE", "Конфигурация маршрутов недоступна."}
	}
	mi := e.managedInterface()
	if !reProxy.MatchString(mi) {
		return nil, &summaryError{"ROUTES_CONFIG_INVALID", "Некорректный управляемый интерфейс Keenetic."}
	}
	if !fileExists(e.globalRegistryFile()) {
		return nil, &summaryError{"ROUTES_GLOBAL_REGISTRY_UNAVAILABLE", "Глобальный реестр маршрутов недоступен."}
	}
	objOrNull := func(m obj, k string) bool { _, isObj := m[k].(obj); return m[k] == nil || isObj }
	if !ok || intOr(s["schemaVersion"], 0) != 1 || str(s, "bundleId") != id {
		return nil, &summaryError{"ROUTES_STATE_INVALID", "Локальное состояние маршрутов повреждено."}
	}
	if _, isStr := s["status"].(string); !isStr || !objOrNull(s, "availableVersion") || !objOrNull(s, "downloadedVersion") ||
		!objOrNull(s, "installedVersion") || !objOrNull(s, "verifyResult") {
		return nil, &summaryError{"ROUTES_STATE_INVALID", "Локальное состояние маршрутов повреждено."}
	}
	b, ok := readObj(e.registryFile(id))
	_, a1 := b["routeKeys"].([]any)
	_, a2 := b["managedRouteKeys"].([]any)
	_, a3 := b["externalRouteKeys"].([]any)
	if !ok || intOr(b["schemaVersion"], 0) != 1 || str(b, "bundleId") != id || !a1 || !a2 || !a3 || !objOrNull(b, "installedVersion") {
		return nil, &summaryError{"ROUTES_BUNDLE_REGISTRY_INVALID", "Реестр набора маршрутов поврежден."}
	}
	g, ok := readObj(e.globalRegistryFile())
	if _, isArr := g["routes"].([]any); !ok || intOr(g["schemaVersion"], 0) != 1 || str(g, "managedInterface") == "" || !isArr {
		return nil, &summaryError{"ROUTES_GLOBAL_REGISTRY_INVALID", "Глобальный реестр маршрутов поврежден."}
	}
	if _, isNum := numOf(g["managedMetric"]); !isNum {
		return nil, &summaryError{"ROUTES_GLOBAL_REGISTRY_INVALID", "Глобальный реестр маршрутов поврежден."}
	}
	operationRunning := e.routesBusy()
	// verification freshness: lastVerifiedAt, else the state file mtime
	var epoch int64
	if lv := str(s, "lastVerifiedAt"); lv != "" {
		if t, err := time.Parse("2006-01-02T15:04:05-0700", lv); err == nil {
			epoch = t.Unix()
		}
	}
	if epoch == 0 {
		if st, err := os.Stat(e.stateFile(id)); err == nil {
			epoch = st.ModTime().Unix()
		}
	}
	age := nowFn().Unix() - epoch
	if age < 0 {
		age = 0
	}
	freshness := freshnessFromAge(age, 3600, 86400)

	sameRoute := func(a, b obj) bool { return a != nil && b != nil && routeVersionKey(a) == routeVersionKey(b) }
	sameSource := func(a, b obj) bool { return a != nil && b != nil && sourceVersionKey(a) == sourceVersionKey(b) }
	av, dv, iv := sub(s, "availableVersion"), sub(s, "downloadedVersion"), sub(s, "installedVersion")
	available, downloaded := av != nil, dv != nil
	installed := iv != nil && b["installedVersion"] != nil
	downloadRequired := available && !sameSource(av, dv)
	keeneticUpdateRequired := downloaded && (iv == nil || !sameRoute(dv, iv))
	keeneticUpdateAvailable := downloaded && iv != nil && !sameRoute(dv, iv)
	sourceUpdateAvailable := available && dv != nil && !sameSource(av, dv)
	updateAvailable := sourceUpdateAvailable || keeneticUpdateAvailable
	verify := sub(s, "verifyResult")
	verifyMatches := downloaded && verify != nil && str(verify, "contentSha256") != "" && str(verify, "contentSha256") == str(dv, "contentSha256")
	localInvalid := verifyMatches && sub(verify, "local")["valid"] == false
	verificationCurrent := verifyMatches && sub(verify, "local")["valid"] == true
	verificationRequired := downloaded && !localInvalid && !verificationCurrent
	vk := sub(verify, "keenetic")
	verificationConflict := verificationCurrent && str(vk, "status") == "conflict"
	verificationRetry := verificationCurrent && (vk["available"] == false || str(vk, "status") == "unavailable")
	installedRouteCount := len(anyList(b["routeKeys"]))
	managedCount := len(anyList(b["managedRouteKeys"]))
	externalCount := len(anyList(b["externalRouteKeys"]))
	globalOwned := 0
	for _, x := range anyList(g["routes"]) {
		for _, o := range strList(orNull(x.(obj), "owners")) {
			if o == id {
				globalOwned++
				break
			}
		}
	}
	gm, _ := numOf(g["managedMetric"])
	consistent := ((iv == nil && b["installedVersion"] == nil) || (iv != nil && sub(b, "installedVersion") != nil && sameRoute(iv, sub(b, "installedVersion")))) &&
		installedRouteCount == managedCount+externalCount && managedCount == globalOwned && str(g, "managedInterface") == mi && gm == 1200
	type opItem struct {
		typ, at string
		msg     any
	}
	var items []opItem
	addItem := func(typ, atKey, resKey string) {
		if at := str(s, atKey); at != "" {
			items = append(items, opItem{typ, at, orNull(sub(s, resKey), "message")})
		}
	}
	addItem("check", "lastCheckedAt", "checkResult")
	addItem("download", "lastDownloadedAt", "downloadResult")
	addItem("verify", "lastVerifiedAt", "verifyResult")
	addItem("install", "lastExportedAt", "exportResult")
	addItem("delete", "lastDeletedAt", "deleteResult")
	sort.SliceStable(items, func(i, j int) bool { return items[i].at < items[j].at })
	var lastOp any
	if len(items) > 0 {
		it := items[len(items)-1]
		lastOp = obj{"type": it.typ, "completedAt": it.at, "success": true, "message": it.msg}
	}
	var errV any
	if s["lastError"] != nil {
		errV = s["lastError"]
	} else if !consistent {
		errV = obj{"code": "ROUTES_STATE_INCONSISTENT", "message": "Локальное состояние и реестры маршрутов не согласованы."}
	}
	state := "empty"
	switch {
	case operationRunning:
		state = "busy"
	case localInvalid || verificationConflict || errV != nil:
		state = "error"
	case installed && updateAvailable:
		state = "update_available"
	case installed:
		state = "installed"
	case downloaded:
		state = "downloaded"
	case available:
		state = "available"
	}
	rec := "check"
	switch {
	case operationRunning:
		rec = "wait"
	case localInvalid:
		rec = "download"
	case verificationRetry:
		rec = "verify"
	case errV != nil:
		rec = "review"
	case downloadRequired:
		rec = "download"
	case verificationRequired:
		rec = "verify"
	case verificationConflict:
		rec = "review"
	case keeneticUpdateRequired && iv != nil:
		rec = "update"
	case keeneticUpdateRequired:
		rec = "install"
	}
	health := "ok"
	switch {
	case errV != nil:
		health = "error"
	case operationRunning:
		health = "busy"
	case keeneticUpdateRequired || downloadRequired || verificationRequired || verificationRetry:
		health = "warning"
	case freshness == "expired":
		health = "warning"
	}
	var localSetValid any
	if localInvalid {
		localSetValid = false
	} else if verificationCurrent {
		localSetValid = true
	}
	rc := s["routeCount"]
	if rc == nil {
		rc = 0
	}
	return obj{"schemaVersion": 1, "bundleId": id, "state": state, "sourceState": s["status"], "healthState": health,
		"installed": installed, "recordedInstalled": installed,
		"verifiedInstalled": installed && verificationCurrent && vk["checked"] == true && vk["complete"] == true,
		"downloaded":        downloaded, "available": available, "updateAvailable": updateAvailable, "sourceUpdateAvailable": sourceUpdateAvailable,
		"keeneticUpdateAvailable": keeneticUpdateAvailable, "downloadRequired": downloadRequired,
		"downloadActionRequired": downloadRequired || localInvalid, "verificationRequired": verificationRequired,
		"verificationCurrent": verificationCurrent, "localSetValid": localSetValid, "verificationConflict": verificationConflict,
		"verificationRetryRequired": verificationRetry, "keeneticUpdateRequired": keeneticUpdateRequired,
		"exportRequired": keeneticUpdateRequired, "operationRunning": operationRunning, "recommendedAction": rec,
		"routeCount": rc, "installedRouteCount": installedRouteCount, "managedRouteCount": managedCount,
		"externalRouteCount": externalCount, "globalOwnedRouteCount": globalOwned, "managedInterface": g["managedInterface"],
		"managedMetric": g["managedMetric"], "availableVersion": s["availableVersion"], "downloadedVersion": s["downloadedVersion"],
		"installedVersion": s["installedVersion"], "verifyResult": s["verifyResult"], "lastCheckedAt": s["lastCheckedAt"],
		"lastVerifiedAt": s["lastVerifiedAt"], "verificationFreshness": obj{"state": freshness, "ageSeconds": age},
		"lastDownloadedAt": s["lastDownloadedAt"], "lastExportedAt": s["lastExportedAt"], "lastDeletedAt": s["lastDeletedAt"],
		"lastOperation": lastOp, "registryConsistent": consistent,
		"consistent": consistent && !keeneticUpdateRequired && !verificationConflict && !verificationRetry,
		"actionRequired": downloadRequired || verificationRequired || verificationConflict || verificationRetry ||
			keeneticUpdateRequired || freshness != "fresh" || errV != nil,
		"error": errV, "updatedAt": s["updatedAt"]}, nil
}

func (e *engine) summaryAll() (obj, *summaryError) {
	b, ok := readObj(e.bundlesFile())
	if !ok {
		return nil, &summaryError{"ROUTES_BUNDLES_UNAVAILABLE", "Реестр наборов маршрутов недоступен."}
	}
	g, ok := readObj(e.globalRegistryFile())
	if !ok {
		return nil, &summaryError{"ROUTES_GLOBAL_REGISTRY_UNAVAILABLE", "Глобальный реестр маршрутов недоступен."}
	}
	var list []obj
	for _, id := range strList(b["bundles"]) {
		s, err := e.bundleSummary(id)
		if err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	installedRouteCount := len(anyList(g["routes"]))
	cnt := func(f func(obj) bool) int {
		n := 0
		for _, x := range list {
			if f(x) {
				n++
			}
		}
		return n
	}
	recorded := cnt(func(x obj) bool { return x["recordedInstalled"] == true })
	verified := cnt(func(x obj) bool { return x["verifiedInstalled"] == true })
	attention := cnt(func(x obj) bool { return x["actionRequired"] == true })
	errs := cnt(func(x obj) bool { return x["healthState"] == "error" })
	busy := cnt(func(x obj) bool { return x["healthState"] == "busy" })
	warn := cnt(func(x obj) bool { return x["healthState"] == "warning" })
	all := func(f func(obj) bool) bool { return cnt(f) == len(list) }
	anyOf := func(f func(obj) bool) bool { return cnt(f) > 0 }
	severity := "ok"
	switch {
	case errs > 0:
		severity = "error"
	case busy > 0:
		severity = "busy"
	case warn > 0 || attention > 0:
		severity = "warning"
	}
	reasons := []any{}
	for _, x := range list {
		id, _ := x["bundleId"].(string)
		switch {
		case x["error"] != nil:
			code, msg := any("ROUTES_ERROR"), any("Ошибка набора маршрутов.")
			if eo, ok := x["error"].(obj); ok {
				if eo["code"] != nil {
					code = eo["code"]
				}
				if eo["message"] != nil {
					msg = eo["message"]
				}
			}
			reasons = append(reasons, obj{"code": code, "message": msg, "details": id})
		case x["keeneticUpdateRequired"] == true:
			reasons = append(reasons, obj{"code": "KEENETIC_UPDATE_REQUIRED", "message": "Набор «" + id + "» подготовлен, но не подтверждён в Keenetic.", "details": id})
		case x["verificationRequired"] == true:
			reasons = append(reasons, obj{"code": "ROUTES_VERIFICATION_REQUIRED", "message": "Для набора «" + id + "» требуется проверка.", "details": id})
		case sub(x, "verificationFreshness")["state"] == "expired":
			reasons = append(reasons, obj{"code": "ROUTES_VERIFICATION_EXPIRED", "message": "Проверка набора «" + id + "» устарела.", "details": id})
		}
	}
	updates := cnt(func(x obj) bool {
		return !strings.HasPrefix(str(x, "bundleId"), "user-") && x["updateAvailable"] == true
	})
	var firstErr any
	for _, x := range list {
		if x["error"] != nil {
			firstErr = obj{"bundleId": x["bundleId"], "details": x["error"]}
			break
		}
	}
	freshState := "fresh"
	if anyOf(func(x obj) bool { return sub(x, "verificationFreshness")["state"] == "expired" }) {
		freshState = "expired"
	} else if anyOf(func(x obj) bool { return sub(x, "verificationFreshness")["state"] == "stale" }) {
		freshState = "stale"
	}
	var lv []string
	var lastOps []obj
	for _, x := range list {
		if s, ok := x["lastVerifiedAt"].(string); ok {
			lv = append(lv, s)
		}
		if lo, ok := x["lastOperation"].(obj); ok {
			lastOps = append(lastOps, lo)
		}
	}
	sort.Strings(lv)
	var checkedAt, lastOp any
	if len(lv) > 0 {
		checkedAt = lv[len(lv)-1]
	}
	sort.SliceStable(lastOps, func(i, j int) bool { return str(lastOps[i], "completedAt") < str(lastOps[j], "completedAt") })
	if len(lastOps) > 0 {
		lastOp = lastOps[len(lastOps)-1]
	}
	bundles := make([]any, len(list))
	for i, x := range list {
		bundles[i] = x
	}
	out := obj{"schemaVersion": 1, "statusContractVersion": 1, "bundles": bundles, "availableBundles": len(list),
		"installedBundles": verified, "verifiedInstalledBundles": verified, "recordedInstalledBundles": recorded,
		"installed": verified > 0, "recordedInstalled": recorded > 0, "downloaded": anyOf(func(x obj) bool { return x["downloaded"] == true }),
		"updatesAvailableCount": updates,
		"sourceUpdateAvailable": anyOf(func(x obj) bool {
			return !strings.HasPrefix(str(x, "bundleId"), "user-") && x["sourceUpdateAvailable"] == true
		}),
		"keeneticUpdateRequired": anyOf(func(x obj) bool { return x["keeneticUpdateRequired"] == true }),
		"operationRunning":       anyOf(func(x obj) bool { return x["operationRunning"] == true }),
		"actionRequired":         attention > 0, "attentionCount": attention, "installedRouteCount": installedRouteCount,
		"healthy": severity == "ok", "registryConsistent": all(func(x obj) bool { return x["registryConsistent"] == true }),
		"consistent": all(func(x obj) bool { return x["consistent"] == true }), "error": firstErr,
		"health": obj{"schemaVersion": 1, "module": "routes", "availability": "available", "severity": severity,
			"operational": errs == 0, "consistent": all(func(x obj) bool { return x["consistent"] == true }),
			"actionRequired": attention > 0, "freshness": obj{"state": freshState, "checkedAt": checkedAt}, "reasons": reasons,
			"facts": obj{"availableBundles": len(list), "recordedInstalledBundles": recorded, "verifiedInstalledBundles": verified,
				"attentionCount": attention, "installedRouteCount": installedRouteCount},
			"lastOperation": lastOp},
		"updatedAt": stamp()}
	out["updateAvailable"] = updates > 0
	st := "empty"
	switch {
	case out["operationRunning"] == true:
		st = "busy"
	case severity == "error":
		st = "error"
	case severity == "warning":
		st = "attention"
	case updates > 0:
		st = "update_available"
	case verified > 0:
		st = "installed"
	}
	out["state"] = st
	return out, nil
}

package dns

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

// classified is one actual.dot[] entry of the status (RD:1024-1062).
type classified struct {
	liveRec
	CatalogMatchIDs    []string `json:"catalogMatchIds"`
	InCatalog          bool     `json:"inCatalog"`
	Classification     string   `json:"classification"`
	Managed            bool     `json:"managed"`
	Ownership          string   `json:"ownership"`
	DeleteEligible     bool     `json:"deleteEligible"`
	SelectorMatchCount int      `json:"selectorMatchCount"`
}

func sameEndpoint(a liveRec, c catEntry) bool {
	return a.Address == c.Address && a.EffectivePort == 853 && a.SNI == c.SNI
}

func sameFull(a, b liveRec) bool {
	return a.Address == b.Address && a.PortRaw == b.PortRaw && a.EffectivePort == b.EffectivePort && a.PortState == b.PortState &&
		a.SNI == b.SNI && a.SPKI == b.SPKI && a.Interface == b.Interface && a.Domain == b.Domain
}

func selectorCount(dot []liveRec, address string, port int) int {
	n := 0
	for _, r := range dot {
		if r.Address == address && r.EffectivePort == port {
			n++
		}
	}
	return n
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// classify classifies the live records against catalog, receipts and quarantine.
func classify(obs observed, receipts []receipt, quarantined []any) []classified {
	out := make([]classified, 0, len(obs.Dot))
	for _, e := range obs.Dot {
		var ids []string
		if e.clean() {
			for _, c := range catalog {
				if sameEndpoint(e, c) {
					ids = append(ids, c.ID)
				}
			}
		}
		if ids == nil {
			ids = []string{}
		}
		sc := selectorCount(obs.Dot, e.Address, e.EffectivePort)
		matching := 0
		for _, r := range receipts {
			if r.ProtoSHA == writeProtocolSHA256 && contains(ids, r.ID) && sameFull(r.rec(), e) {
				matching++
			}
		}
		qc := 0
		for _, q := range quarantined {
			qm, _ := q.(map[string]any)
			rc, _ := qm["receipt"].(map[string]any)
			addrOK := rc["address"] == any(e.Address)
			portOK := e.EffectivePort == 853
			if v, ok := rc["effectivePort"]; ok && v != nil && v != false {
				p, isNum := num(v)
				portOK = isNum && p == float64(e.EffectivePort)
			}
			sniOK := true
			if v, ok := rc["sni"]; ok && v != nil && v != false {
				sniOK = v == any(e.SNI)
			}
			if addrOK && portOK && sniOK {
				qc++
			}
		}
		cls := "not-in-broray-catalog"
		switch {
		case !e.clean():
			cls = "unknown"
		case sc != 1:
			cls = "selector-collision"
		case len(ids) > 1:
			cls = "ambiguous"
		case len(ids) == 1 && (e.SPKI != "" || e.Interface != "" || e.Domain != ""):
			cls = "catalog-endpoint-with-extra-attributes"
		case len(ids) == 1:
			cls = "catalog:" + ids[0]
		}
		own := "external"
		if matching == 1 && sc == 1 {
			own = "broray"
		} else if qc > 0 {
			own = "quarantined"
		}
		eligible := e.clean() && sc == 1 && len(ids) == 1 && e.SPKI == "" && e.Interface == "" && e.Domain == ""
		c := classified{liveRec: e, CatalogMatchIDs: ids, InCatalog: eligible, Classification: cls, Managed: own == "broray",
			Ownership: own, DeleteEligible: eligible, SelectorMatchCount: sc}
		c.ParseError = !e.clean()
		out = append(out, c)
	}
	return out
}

type statusInputs struct {
	f             files
	obs           observed
	runningOK     bool
	ndmcAvailable bool
	recovery      bool
	now           time.Time
}

// buildStatus is the jq program of broray_dot_status (RD:1004-1232).
func buildStatus(in statusInputs) map[string]any {
	now := in.now.Unix()
	c, st, r := in.f.config, in.f.state, in.obs
	receipts := in.f.receipts
	quarantined := anyList(c["quarantinedReceipts"])
	if quarantined == nil {
		quarantined = []any{}
	}
	selectedIDs := in.f.requested()
	cls := classify(r, receipts, quarantined)

	managedPresent := 0
	for _, rc := range receipts {
		n := 0
		for _, x := range cls {
			if x.DeleteEligible && x.Ownership == "broray" && sameFull(rc.rec(), x.liveRec) {
				n++
			}
		}
		if n == 1 {
			managedPresent++
		}
	}
	allManagedDeleteEligible := len(receipts) > 0 && managedPresent == len(receipts)

	rows := testRows(st)
	servers := make([]any, 0, len(catalog))
	type sel struct {
		c       catEntry
		present bool
		test    *testRow
	}
	var selected []sel
	for _, p := range catalog {
		o := p.object()
		isSel := contains(selectedIDs, p.ID)
		present, managed := false, false
		for _, x := range cls {
			if contains(x.CatalogMatchIDs, p.ID) {
				if x.InCatalog {
					present = true
				}
				if x.Managed {
					managed = true
				}
			}
		}
		var last *testRow
		for i := range rows {
			if rows[i].matches(p) {
				last = &rows[i]
			}
		}
		o["selected"], o["present"], o["managed"] = isSel, present, managed
		if last != nil {
			o["test"] = last.raw
		} else {
			o["test"] = nil
		}
		servers = append(servers, o)
		if isSel {
			selected = append(selected, sel{p, present, last})
		}
	}
	tested, presentSel := 0, 0
	var conflictIDs, targetIDs, missing []string
	for _, s := range selected {
		if s.test != nil && s.test.fresh(now, testTTL) {
			tested++
		}
		if s.present {
			presentSel++
		}
		eligible := 0
		for _, x := range cls {
			if x.DeleteEligible && contains(x.CatalogMatchIDs, s.c.ID) {
				eligible++
			}
		}
		if selectorCount(r.Dot, s.c.Address, 853) > 0 && eligible != 1 {
			conflictIDs = append(conflictIDs, s.c.ID)
		}
		if eligible == 1 {
			targetIDs = append(targetIDs, s.c.ID)
		}
		inCat := false
		for _, x := range cls {
			if x.InCatalog && contains(x.CatalogMatchIDs, s.c.ID) {
				inCat = true
			}
		}
		if !inCat {
			missing = append(missing, s.c.ID)
		}
	}
	outside := 0
	for _, rc := range receipts {
		in := false
		for _, s := range selected {
			if s.c.ID == rc.ID {
				in = true
			}
		}
		if !in {
			outside++
		}
	}
	external := 0
	for _, x := range cls {
		if !x.Managed {
			external++
		}
	}
	projected := r.TotalSecure - outside + len(missing)
	matches := len(selected) > 0 && presentSel == len(selected) && outside == 0
	var effective []string
	for _, rc := range receipts {
		effective = append(effective, rc.ID)
	}
	effective = uniqueSorted(effective)
	overLimit := len(selectedIDs) > maxServers
	observationDet := in.runningOK && in.ndmcAvailable && r.Determinate
	installState := "not-installed"
	if !observationDet {
		installState = "unknown"
	} else if len(selected) > 0 && presentSel == len(selected) {
		installState = "installed"
	}
	var installed any = installState == "installed"
	if installState == "unknown" {
		installed = nil
	}
	drift := len(receipts) > 0 && !allManagedDeleteEligible
	writeEnabled := true
	mutation := writeEnabled && in.runningOK && in.ndmcAvailable && !in.recovery && r.Determinate
	var mutationBlocked any
	switch {
	case !writeEnabled:
		mutationBlocked = "physical-write-protocol-required"
	case in.recovery:
		mutationBlocked = "recovery-required"
	case !in.runningOK || !in.ndmcAvailable:
		mutationBlocked = "keenetic-unavailable"
	case !r.Determinate:
		mutationBlocked = "dot-observation-underdetermined"
	}
	deleteEligible := mutation && len(selected) > 0 && len(conflictIDs) == 0 && len(targetIDs) > 0
	var deleteBlocked any
	switch {
	case !writeEnabled:
		deleteBlocked = "physical-write-protocol-required"
	case !mutation:
		deleteBlocked = mutationBlocked
	case len(selected) == 0:
		deleteBlocked = "selection-required"
	case len(conflictIDs) > 0:
		deleteBlocked = "selected-dot-selector-conflict"
	case len(targetIDs) == 0:
		deleteBlocked = "selected-dot-record-not-present"
	}
	severity := "ok"
	switch {
	case in.recovery, !in.runningOK || !in.ndmcAvailable, !r.Determinate:
		severity = "error"
	case !writeEnabled, overLimit || projected > maxServers, len(selected) == 0, tested != len(selected), !matches:
		severity = "warning"
	}
	operational := !(in.recovery || !in.runningOK || !in.ndmcAvailable || !r.Determinate)
	consistent := !in.recovery && in.runningOK && in.ndmcAvailable && r.Determinate && !overLimit && projected <= maxServers &&
		len(selected) > 0 && tested == len(selected) && matches
	reason := func(code, msg string) []any {
		return []any{map[string]any{"code": code, "message": msg, "details": nil}}
	}
	itoa := strconv.Itoa
	var reasons []any
	switch {
	case in.recovery:
		reasons = reason("DOT_RECOVERY_REQUIRED", "Предыдущая DNS-over-TLS транзакция требует проверки и восстановления.")
	case !in.runningOK:
		reasons = reason("KEENETIC_CONFIG_UNAVAILABLE", "Не удалось прочитать фактическую конфигурацию Keenetic.")
	case !in.ndmcAvailable:
		reasons = reason("NDMC_UNAVAILABLE", "Команда управления Keenetic недоступна.")
	case !r.Determinate:
		reasons = reason("DOT_OBSERVATION_UNDERDETERMINED", "Фактические DoT-записи не удалось однозначно разобрать и сверить с runtime.")
	case !writeEnabled:
		reasons = reason("DOT_PHYSICAL_WRITE_PROTOCOL_REQUIRED", "Чтение DoT доступно, но в этих bytes не включён замороженный обратимый Keenetic CLI protocol.")
	case overLimit:
		reasons = reason("DNS_SELECTION_OVER_LIMIT", "Сохранено "+itoa(len(selectedIDs))+" выбранных серверов при максимуме "+itoa(maxServers)+
			". Авторитетных receipt: "+itoa(len(receipts))+". Оставьте не более "+itoa(maxServers)+" серверов.")
	case projected > maxServers:
		reasons = reason("DNS_CAPACITY_EXCEEDED", "После применения будет "+itoa(projected)+" защищённых DNS-серверов при максимуме "+itoa(maxServers)+".")
	case len(selected) == 0:
		reasons = reason("DNS_SELECTION_REQUIRED", "Выберите хотя бы один DNS-over-TLS-сервер.")
	case tested != len(selected):
		reasons = reason("DNS_TEST_REQUIRED", "Проверено "+itoa(tested)+" из "+itoa(len(selected))+" выбранных серверов.")
	case !matches:
		reasons = reason("DNS_APPLY_REQUIRED", "Выбранная конфигурация отличается от фактической конфигурации Keenetic.")
	default:
		reasons = []any{}
	}
	cfgState := "apply-required"
	switch {
	case in.recovery:
		cfgState = "recovery-required"
	case !in.runningOK || !in.ndmcAvailable:
		cfgState = "unavailable"
	case !r.Determinate:
		cfgState = "observation-unknown"
	case overLimit:
		cfgState = "selection-over-limit"
	case len(selected) == 0:
		cfgState = "selection-required"
	case projected > maxServers:
		cfgState = "limit-exceeded"
	case matches:
		cfgState = "matches-selection"
	case tested != len(selected):
		cfgState = "test-required"
	}
	migration := any("none")
	if overLimit {
		migration = "selection-over-limit"
	} else if v, ok := c["migrationState"]; ok && v != nil && v != false {
		migration = v
	}
	var matchesSel any = matches
	if !observationDet {
		matchesSel = nil
	}
	obsState := "unknown"
	if observationDet {
		obsState = "determinate"
	}
	availability := "unavailable"
	if in.runningOK {
		availability = "available"
	}
	slots := maxServers - r.TotalSecure
	if slots < 0 {
		slots = 0
	}
	tests := st["tests"]
	if tests == nil {
		tests = []any{}
	}
	lastOp := st["lastOperation"]
	managed := in.f.managed
	if managed == nil {
		managed = []any{}
	}
	return map[string]any{
		"schemaVersion":          5,
		"maxServers":             maxServers,
		"maxServersSource":       "Официальный максимум Keenetic: до 8 DoT/DoH-серверов",
		"runningConfigFormat":    "flat-or-dns-proxy-block",
		"runningConfigAvailable": in.runningOK,
		"ndmcAvailable":          in.ndmcAvailable,
		"testAvailable":          true,
		"supported":              in.runningOK && in.ndmcAvailable,
		"recoveryRequired":       in.recovery,
		"writeContract":          map[string]any{"candidateId": writeCandidate, "protocolEnabled": writeEnabled, "protocolSha256": writeProtocolSHA256},
		"writeProtocolEnabled":   writeEnabled,
		"mutationAvailable":      mutation,
		"mutationBlockedReason":  mutationBlocked,
		"deleteEligible":         deleteEligible,
		"deleteBlockedReason":    deleteBlocked,
		"requestedIds":           strSlice(selectedIDs),
		"effectiveIds":           strSlice(effective),
		"selectedIds":            strSlice(selectedIDs),
		"selectedCount":          len(selected),
		"effectiveCount":         len(effective),
		"selectionOverLimit":     overLimit,
		"migrationState":         migration,
		"selectedTestedCount":    tested,
		"selectedPresentCount":   presentSel,
		"missingSelectedCount":   len(selected) - presentSel,
		"projectedTotal":         projected,
		"capacityExceeded":       projected > maxServers,
		"availableSlots":         slots,
		"managed":                managed,
		"quarantinedReceipts":    quarantined,
		"managedPresentCount":    managedPresent,
		"externalDotCount":       external,
		"servers":                servers,
		"actual": map[string]any{"dot": cls, "dohCount": r.DohCount, "totalSecure": r.TotalSecure, "determinate": r.Determinate,
			"runtimeReconciled": r.RuntimeReconciled, "runtime": r.Runtime},
		"installed":          installed,
		"installationState":  installState,
		"observationState":   obsState,
		"matchesSelection":   matchesSel,
		"drift":              drift,
		"configurationState": cfgState,
		"health": map[string]any{"schemaVersion": 1, "module": "dns", "availability": availability, "severity": severity,
			"operational": operational, "consistent": consistent, "actionRequired": severity != "ok",
			"freshness": map[string]any{"state": "fresh", "checkedAt": compat.StampUTC(in.now)}, "reasons": reasons,
			"facts": map[string]any{"requestedCount": len(selectedIDs), "effectiveCount": len(effective), "managedCount": len(receipts),
				"managedPresentCount": managedPresent, "quarantinedReceiptCount": len(quarantined), "maxServers": maxServers,
				"projectedTotal": projected},
			"lastOperation": lastOp},
		"tests":          tests,
		"testTtlSeconds": testTTL,
		"lastTestedAt":   st["lastTestedAt"],
		"lastAppliedAt":  st["lastAppliedAt"],
		"lastDeletedAt":  st["lastDeletedAt"],
		"lastOperation":  lastOp,
		"lastError":      st["lastError"],
		"updatedAt":      st["updatedAt"],
	}
}

// status is broray_dot_status: ensure files, observe Keenetic, classify.
func (s *service) status(ctx context.Context) (map[string]any, error) {
	f, err := s.ensureFiles()
	if err != nil {
		return nil, err
	}
	obs, oerr := s.fetchObserved(ctx)
	runningOK := oerr == nil
	if !runningOK {
		obs = unavailableObserved()
	}
	return buildStatus(statusInputs{f: f, obs: obs, runningOK: runningOK, ndmcAvailable: true, recovery: exists(s.markerPath()), now: s.now()}), nil
}

// statusCached is broray_dot_status_cached (RD:852-908): 30 s cache keyed
// by the bytes of config.json and state.json.
func (s *service) statusCached(ctx context.Context) (map[string]any, error) {
	if _, err := s.ensureFiles(); err != nil {
		return nil, err
	}
	hash := s.inputHash()
	now := s.now().Unix()
	if hash != "" {
		if m, err := readMap(s.cachePath()); err == nil && numIs(m["schemaVersion"], 1) && m["inputHash"] == hash {
			if exp, ok := num(m["expiresEpoch"]); ok && exp > float64(now) {
				if data, ok := m["data"].(map[string]any); ok {
					return data, nil
				}
			}
		}
	}
	data, err := s.status(ctx)
	if err != nil {
		return nil, err
	}
	if hash != "" {
		_ = compat.WriteJSON(s.cachePath(), map[string]any{"schemaVersion": 1, "inputHash": hash, "createdEpoch": now,
			"expiresEpoch": now + statusCacheSeconds, "data": data})
	}
	return data, nil
}

func (s *service) inputHash() string {
	var parts []byte
	for _, p := range []string{s.configPath(), s.statePath()} {
		b, err := readFile(p)
		if err != nil {
			return ""
		}
		parts = append(parts, []byte(compat.SHA256Hex(b)+"  "+p+"\n")...)
	}
	return compat.SHA256Hex(parts)
}

func sortedIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

package routes

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// errPaused marks a delete that stopped resumably (3.x exit 76): the HTTP
// layer reports success with operationProgress.resumable == true.
var errPaused = errors.New("ROUTES_OPERATION_PAUSED")

type deleteRun struct {
	e           *engine
	ctx         context.Context
	id          string
	iface       string
	local       *fileBackup
	deleted     []planRoute // rollback intents (commands sent)
	completed   []planRoute // verified deletions
	prog        *progress
	saveNeed    bool
	committed   bool
	paused      bool
	txFile      string
	lastError   string
	resumeBase  int
	prevDeleted int
	prevAbsent  int
}

func (d *deleteRun) transaction(phase string, saved, committed bool, message string) error {
	if d.txFile == "" {
		return nil
	}
	return writeObj(d.txFile, obj{"schemaVersion": 2, "operation": "delete", "bundleId": d.id, "phase": phase, "deletedCount": len(d.completed),
		"configurationSaved": saved, "committed": committed, "message": message, "updatedAt": stamp()})
}

// fail is broray_routes_delete_error + broray_routes_delete_cleanup: roll
// the deletions back (re-add in reverse) and restore the local files.
func (d *deleteRun) fail(message string) error {
	e := d.e
	d.lastError = message
	if d.committed || d.paused {
		return cliErr("ОШИБКА: %s", message)
	}
	rolledBack, rollbackFailed := false, false
	if len(d.deleted) > 0 || d.local != nil {
		if d.rollback() {
			rolledBack = true
		} else {
			rollbackFailed = true
			d.lastError = "Откат удаления не подтверждён running/startup-config."
		}
		if d.local != nil && !d.local.restore() {
			rollbackFailed = true
		}
	}
	if rollbackFailed {
		rolledBack = false
		d.lastError = "Восстановление не завершено; резервные копии сохранены."
	} else if rolledBack {
		if err := d.transaction("rolled_back", false, false, message); err != nil {
			rolledBack, rollbackFailed = false, true
			d.lastError = "Откат выполнен, но итог транзакции не записан; резервные копии сохранены."
		}
	}
	if rollbackFailed {
		if err := e.writeRollbackMarker(d.txFile, d.lastError); err != nil {
			d.lastError += " Блокирующий маркер не записан; lease сохранён."
		}
	}
	if d.prog != nil {
		msg := d.lastError
		if rolledBack {
			msg += " Изменения операции отменены."
		}
		d.prog.fail(msg, rolledBack)
	}
	return cliErr("ОШИБКА: %s", message)
}

func (d *deleteRun) rollback() bool {
	e := d.e
	ctx := context.WithoutCancel(d.ctx)
	failed, changed := false, false
	for i := len(d.deleted) - 1; i >= 0; i-- {
		r := d.deleted[i]
		snap, err := e.fetchConfig(ctx)
		if err != nil || !snap.complete() {
			failed = true
			continue
		}
		id := snap.identityCount(r.dest(), r.Iface)
		if id == 1 && snap.attributeCount(r.dest(), r.Iface, r.Metric) == 1 {
			continue
		}
		if id != 0 {
			failed = true
			continue
		}
		if _, err := e.runCommands(ctx, []string{addCmd(r)}, 8*time.Second); err != nil {
			failed = true
			continue
		}
		if e.waitState(ctx, true, []planRoute{r}) {
			changed = true
		} else {
			failed = true
		}
	}
	if changed {
		if err := e.save(ctx); err != nil {
			failed = true
		}
	}
	return !failed
}

// pause is broray_routes_delete_pause (failed_resumable, exit 76).
func (d *deleteRun) pause(message, errRoute string) error {
	e := d.e
	if d.saveNeed {
		if err := e.save(d.ctx); err != nil {
			return d.fail("Частичное удаление не сошлось между running/startup-config.")
		}
	}
	if err := d.partialCommit(message, errRoute); err != nil {
		return d.fail("Не удалось сохранить состояние удаления для продолжения.")
	}
	d.paused = true
	if err := d.transaction("paused", d.saveNeed, false, message); err != nil {
		_ = e.writeRollbackMarker(d.txFile, "Частичное удаление сохранено, но итог транзакции не записан.")
		d.prog.fail("Частичное удаление сохранено, но итог транзакции не записан; новые изменения заблокированы.", false)
		return cliErr("ОШИБКА: Не удалось записать итог транзакции удаления: %v", err)
	}
	d.prog.pause(message, false, errRoute)
	return errPaused
}

func (d *deleteRun) partialCommit(message, errRoute string) error {
	e, id := d.e, d.id
	now := stamp()
	reg, ok1 := readObj(e.globalRegistryFile())
	breg, ok2 := readObj(e.registryFile(id))
	state, ok3 := readObj(e.stateFile(id))
	if !ok1 || !ok2 || !ok3 {
		return errors.New("partial")
	}
	keys := map[string]bool{}
	for _, r := range d.completed {
		keys[r.Key] = true
	}
	var routes []any
	for _, x := range anyList(reg["routes"]) {
		m, _ := x.(obj)
		if keys[str(m, "key")] {
			var rest []string
			for _, o := range strList(m["owners"]) {
				if o != id {
					rest = append(rest, o)
				}
			}
			m["owners"] = toAny(sortedUnique(rest))
			m["updatedAt"] = now
			if len(rest) == 0 {
				continue
			}
		}
		routes = append(routes, m)
	}
	if routes == nil {
		routes = []any{}
	}
	reg["routes"], reg["updatedAt"] = routes, now
	minus := func(list []string) []any {
		out := []any{}
		for _, k := range list {
			if !keys[k] {
				out = append(out, k)
			}
		}
		return out
	}
	breg["routeKeys"] = minus(strList(breg["routeKeys"]))
	breg["managedRouteKeys"] = minus(strList(breg["managedRouteKeys"]))
	breg["externalRouteKeys"] = minus(strList(breg["externalRouteKeys"]))
	breg["updatedAt"] = now
	state["status"] = "installed"
	state["lastVerifiedAt"] = nil
	state["verifyResult"] = nil
	state["preflight"] = nil
	state["resumeOperation"] = obj{"operation": "delete", "resumable": true, "current": d.prog.current, "total": d.prog.total,
		"deleted": d.prevDeleted + len(d.completed), "alreadyAbsent": d.prevAbsent, "message": message,
		"errorRoute": nullStr(errRoute), "updatedAt": now}
	if errRoute == "" {
		state["lastError"] = nil
	} else {
		state["lastError"] = obj{"code": "ROUTES_DELETE_PAUSED_AFTER_ERROR", "message": message, "route": errRoute, "resumable": true}
	}
	state["updatedAt"] = now
	for _, w := range []struct {
		p string
		v obj
	}{{e.globalRegistryFile(), reg}, {e.registryFile(id), breg}, {e.stateFile(id), state}} {
		if err := writeObj(w.p, w.v); err != nil {
			return err
		}
	}
	return nil
}

// runDelete is `broray-routes delete <id>` (routes-router-delete.sh:869-1379).
func (e *engine) runDelete(ctx context.Context, id, opID string) (string, error) {
	d := &deleteRun{e: e, ctx: ctx, id: id}
	errOut := func(msg string) (string, error) { return "", cliErr("ОШИБКА: %s", msg) }
	if !fileExists(e.bundlesFile()) {
		return errOut("Реестр наборов маршрутов недоступен.")
	}
	if !e.bundleListed(id) {
		return errOut("Набор маршрутов не найден.")
	}
	cat := e.catalogDir(id)
	planFile := filepath.Join(cat, "export-plan.json")
	resultFile := filepath.Join(cat, "router-delete-result.json")
	for _, f := range []string{e.configFile(), e.globalRegistryFile(), e.registryFile(id), e.stateFile(id), planFile} {
		if !fileExists(f) {
			return errOut("Не найден файл: " + f)
		}
	}
	cfg, _ := e.routesConfig()
	iface := str(cfg, "managedInterface")
	if !reProxy.MatchString(iface) {
		return errOut("Некорректный управляемый интерфейс Keenetic.")
	}
	d.iface = iface
	pol := sub(cfg, "ownershipPolicy")
	if intOr(cfg["managedMetric"], 0) != managedMetric || pol == nil || pol["deleteOnlyExactManagedMatch"] != true ||
		pol["deleteExternalRoutes"] != false || pol["touchOtherInterfaces"] != false {
		return errOut("Политика удаления повреждена.")
	}
	breg, _ := readObj(e.registryFile(id))
	_, a1 := breg["routeKeys"].([]any)
	_, a2 := breg["managedRouteKeys"].([]any)
	if str(breg, "bundleId") != id || breg["installedVersion"] == nil || intOr(breg["managedMetric"], 0) != managedMetric || !a1 || !a2 {
		return errOut("Набор не установлен или его реестр повреждён.")
	}
	reg, _ := readObj(e.globalRegistryFile())
	regByKey := map[string]obj{}
	for _, x := range anyList(reg["routes"]) {
		m, _ := x.(obj)
		regByKey[str(m, "key")] = m
	}
	owns := func(m obj) bool {
		for _, o := range strList(m["owners"]) {
			if o == id {
				return true
			}
		}
		return false
	}
	managedKeys := strList(breg["managedRouteKeys"])
	for _, k := range managedKeys {
		m, ok := regByKey[k]
		if !ok || str(m, "interface") != iface || intOr(m["metric"], 0) != managedMetric || m["createdByBROray"] != true || m["managed"] != true || !owns(m) {
			return errOut("Реестр владения не подтверждает все управляемые маршруты.")
		}
	}
	if e.rollbackBlocked() {
		return errOut(msgRollbackBlock)
	}
	stampS := nowFn().Format("20060102-150405")
	d.txFile = e.p("transactions", "delete-"+id+"-"+stampS+".json")
	actual, err := e.fetchConfig(ctx)
	if err != nil {
		return errOut("Не удалось прочитать полный список настроенных маршрутов Keenetic.")
	}
	if !actual.complete() {
		return errOut("running-config содержит неподтверждённую сериализацию статического маршрута.")
	}
	// candidates: registry routes owned only by this bundle (shared ones are kept)
	managedSet := map[string]bool{}
	for _, k := range managedKeys {
		managedSet[k] = true
	}
	var candidates []planRoute
	shared := 0
	for _, x := range anyList(reg["routes"]) {
		m, _ := x.(obj)
		if !managedSet[str(m, "key")] || !owns(m) {
			continue
		}
		if len(strList(m["owners"])) == 1 {
			candidates = append(candidates, registryRoute(m))
		} else if len(strList(m["owners"])) > 1 {
			shared++
		}
	}
	var physical []planRoute
	for _, r := range candidates {
		idc := actual.identityCount(r.dest(), r.Iface)
		if idc == 0 {
			continue
		}
		if idc != 1 || actual.attributeCount(r.dest(), r.Iface, r.Metric) != 1 {
			return errOut("Не удалось сопоставить реестр BROray с running-config.")
		}
		physical = append(physical, r)
	}
	total := len(strList(breg["routeKeys"]))
	externalCount := len(anyList(breg["externalRouteKeys"]))
	alreadyAbsent := len(managedKeys) - shared - len(physical)
	if alreadyAbsent < 0 {
		alreadyAbsent = 0
	}
	state, _ := readObj(e.stateFile(id))
	d.prevDeleted = intOr(sub(state, "resumeOperation")["deleted"], 0)
	d.prevAbsent = intOr(sub(state, "resumeOperation")["alreadyAbsent"], 0)
	d.local = backupFiles(e.globalRegistryFile(), e.registryFile(id), e.stateFile(id), planFile, resultFile)
	if d.local == nil {
		return errOut("Не удалось прочитать исходные локальные данные маршрутов.")
	}
	base, rtotal, resumed := e.resumeValues(id, "delete", len(physical))
	if !resumed {
		d.prevDeleted, d.prevAbsent = 0, 0
	}
	d.resumeBase = base
	d.prog = e.beginProgress(id, "delete", opID, rtotal, base, resumed, fmt.Sprintf("Подготовка удаления: %d из %d.", base, rtotal))
	d.prog.update("applying", base, "Удаление маршрутов из Keenetic.", "")
	e.enableRouteStop(id, opID)
	if err := d.transaction("checked", false, false, "Проверка владения завершена."); err != nil {
		d.prog.fail("Не удалось записать начало транзакции удаления; маршруты не изменены.", false)
		return errOut("Не удалось записать начало транзакции удаления.")
	}
	e.beginLiveTransaction(d.txFile)
	defer e.endLiveTransaction(d.txFile)
	processed := base
	for start := 0; start < len(physical); start += verifyBatchSize {
		if e.routeStopRequested(id, opID) {
			return d.stopOut("Удаление маршрутов остановлено по запросу пользователя.")
		}
		end := start + verifyBatchSize
		if end > len(physical) {
			end = len(physical)
		}
		batch := physical[start:end]
		cmds := make([]string, len(batch))
		for i, r := range batch {
			if r.Iface != iface {
				return d.failOut("План удаления вышел за пределы " + iface + ".")
			}
			if r.Metric != managedMetric {
				return d.failOut("План удаления содержит чужую метрику.")
			}
			cmds[i] = removeCmd(r)
			d.deleted = append(d.deleted, r)
		}
		n, cmdErr := e.runCommands(ctx, cmds, 8*time.Second)
		if n > 0 {
			d.saveNeed = true
		}
		done := batch[:n]
		if cmdErr != nil {
			// verify and commit the part that was applied, then pause at the failed route
			if len(done) > 0 && e.waitState(ctx, false, done) {
				d.commitBatch(done, &processed)
			}
			r := batch[n]
			return d.pauseOut(fmt.Sprintf("Не удалось удалить %s. %s Выполнено %d из %d. Можно продолжить после устранения причины.",
				r.dest(), strings.TrimSpace(cmdErr.Error()), d.prog.current, d.prog.total), r.dest())
		}
		if !e.waitState(ctx, false, batch) {
			msg := "Пакетное удаление маршрутов не сошлось в running-config."
			if end == len(physical) {
				msg = "Итоговое пакетное удаление маршрутов не сошлось в running-config."
			}
			return d.pauseOut(fmt.Sprintf("%s Выполнено %d из %d. Можно продолжить после устранения причины.", msg, d.prog.current, d.prog.total),
				batch[len(batch)-1].dest())
		}
		d.commitBatch(batch, &processed)
		d.transaction("deleting", false, false, "Удаляются управляемые маршруты.")
		if e.routeStopRequested(id, opID) {
			return d.stopOut("Удаление маршрутов остановлено по запросу пользователя.")
		}
	}
	d.prog.update("verifying", processed, "Проверка удаления маршрутов в Keenetic.", "")
	after, err := e.fetchConfig(ctx)
	if err != nil {
		return d.failOut("Не удалось повторно прочитать running-config после удаления.")
	}
	if !after.complete() {
		return d.failOut("Итоговый running-config содержит неподтверждённую сериализацию.")
	}
	for _, r := range physical {
		if after.identityCount(r.dest(), r.Iface) != 0 {
			return d.failOut("Итог удаления не подтверждён полной конфигурацией Keenetic.")
		}
	}
	if e.closeRouteStop(id, opID) {
		return d.stopOut("Удаление маршрутов остановлено по запросу пользователя.")
	}
	deletedTotal := d.prevDeleted + len(d.completed)
	absentTotal := d.prevAbsent + alreadyAbsent
	if err := d.commitFinal(total, deletedTotal, shared, absentTotal, externalCount); err != nil {
		return d.failOut("Не удалось установить локальное состояние удаления.")
	}
	d.prog.update("saving", processed, "Сохранение конфигурации Keenetic.", "")
	if d.saveNeed {
		if err := e.save(ctx); err != nil {
			return d.failOut("Конфигурация Keenetic не сошлась между running/startup-config.")
		}
	}
	d.committed = true
	message := "Маршруты удалены из Keenetic"
	if err := d.transaction("committed", d.saveNeed, true, message); err != nil {
		_ = e.writeRollbackMarker(d.txFile, "Маршруты удалены, но итог транзакции не записан.")
		d.prog.fail("Маршруты удалены, но итог транзакции не записан; новые изменения заблокированы.", false)
		return errOut("Маршруты удалены, но итог транзакции не записан.")
	}
	d.prog.complete(fmt.Sprintf("Удаление маршрутов из Keenetic завершено: %d из %d.", rtotal, rtotal))
	out := []string{message, "Набор: " + id, "Удалено физических маршрутов BROray: " + itoa(deletedTotal),
		"Сохранено общих маршрутов: " + itoa(shared), "Уже отсутствовало в Keenetic: " + itoa(absentTotal),
		"Не затронуто внешних маршрутов: " + itoa(externalCount), "Интерфейс удаления: " + iface, "Метрика владения BROray: 1200",
		"Команда удаления: сеть + маска + " + iface, "Конфигурация сохранена: " + fmt.Sprint(d.saveNeed), "Локально скачанный набор сохранён."}
	return strings.Join(out, "\n") + "\n", nil
}

func (d *deleteRun) failOut(msg string) (string, error) { return "", d.fail(msg) }

func (d *deleteRun) pauseOut(msg, route string) (string, error) {
	err := d.pause(msg, route)
	if errors.Is(err, errPaused) {
		out := msg + "\nВыполнено: " + itoa(d.prog.current) + " из " + itoa(d.prog.total) + "\nОперацию можно продолжить позднее.\n"
		return out, errPaused
	}
	return "", err
}

func (d *deleteRun) stopOut(message string) (string, error) {
	if d.saveNeed {
		if err := d.e.save(d.ctx); err != nil {
			return d.failOut("Не удалось сохранить подтверждённые изменения удаления при остановке.")
		}
	}
	if err := d.partialCommit(message, ""); err != nil {
		return d.failOut("Не удалось сохранить состояние удаления для продолжения.")
	}
	d.paused = true
	if err := d.transaction("paused", d.saveNeed, false, message); err != nil {
		_ = d.e.writeRollbackMarker(d.txFile, "Удаление остановлено, но итог транзакции не записан.")
		d.prog.fail("Удаление остановлено, но итог транзакции не записан; новые изменения заблокированы.", false)
		return "", cliErr("ОШИБКА: Не удалось записать итог транзакции удаления: %v", err)
	}
	d.prog.pause(message+" Подтверждённые изменения сохранены; операцию можно продолжить.", true, "")
	return message + "\nВыполнено: " + itoa(d.prog.current) + " из " + itoa(d.prog.total) + "\n", errPaused
}

func (d *deleteRun) commitBatch(batch []planRoute, processed *int) {
	for _, r := range batch {
		d.completed = append(d.completed, r)
		*processed++
		d.prog.tick(*processed, r.dest())
	}
}

// commitFinal installs the local delete result (broray_routes_delete_prepare).
func (d *deleteRun) commitFinal(total, deleted, shared, absent, external int) error {
	e, id := d.e, d.id
	now := stamp()
	reg, _ := readObj(e.globalRegistryFile())
	breg, _ := readObj(e.registryFile(id))
	state, _ := readObj(e.stateFile(id))
	cat := e.catalogDir(id)
	plan, _ := readObj(filepath.Join(cat, "export-plan.json"))
	if reg == nil || breg == nil || state == nil || plan == nil {
		return errors.New("local")
	}
	keys := map[string]bool{}
	for _, k := range strList(breg["routeKeys"]) {
		keys[k] = true
	}
	var routes []any
	for _, x := range anyList(reg["routes"]) {
		m, _ := x.(obj)
		if keys[str(m, "key")] {
			var rest []string
			has := false
			for _, o := range strList(m["owners"]) {
				if o == id {
					has = true
				} else {
					rest = append(rest, o)
				}
			}
			if has {
				if len(rest) == 0 {
					continue
				}
				m["owners"] = toAny(rest)
				m["updatedAt"] = now
			}
		}
		routes = append(routes, m)
	}
	if routes == nil {
		routes = []any{}
	}
	reg["schemaVersion"], reg["managedInterface"], reg["managedMetric"], reg["routes"], reg["updatedAt"] = 1, d.iface, managedMetric, routes, now
	breg["installedVersion"], breg["routeKeys"], breg["managedRouteKeys"], breg["externalRouteKeys"] = nil, []any{}, []any{}, []any{}
	breg["targetInterface"], breg["managedMetric"], breg["installedAt"], breg["removedAt"], breg["updatedAt"] = d.iface, managedMetric, nil, now, now
	message := "Маршруты удалены из Keenetic"
	if state["downloadedVersion"] != nil {
		state["status"] = "downloaded"
	} else {
		state["status"] = "not_checked"
	}
	state["installedVersion"] = nil
	state["lastDeletedAt"] = now
	state["lastVerifiedAt"] = nil
	state["verifyResult"] = nil
	state["deleteResult"] = obj{"result": "removed", "message": message, "total": total, "deleted": deleted, "sharedKept": shared,
		"externalKept": external, "targetInterface": d.iface, "managedMetric": managedMetric, "configurationSaved": deleted > 0, "completedAt": now}
	state["exportResult"] = nil
	state["preflight"] = nil
	state["lastError"] = nil
	state["updatedAt"] = now
	delete(state, "resumeOperation")
	plan["routerApplied"], plan["configurationSaved"], plan["removedAt"] = false, false, now
	plan["deleteResult"] = obj{"message": message, "deleted": deleted, "sharedKept": shared, "externalKept": external, "managedMetric": managedMetric}
	result := obj{"schemaVersion": 1, "bundleId": id, "targetInterface": d.iface, "managedMetric": managedMetric, "message": message,
		"summary":       obj{"total": total, "deleted": deleted, "sharedKept": shared, "externalKept": external},
		"routerChanged": deleted > 0, "configurationSaved": deleted > 0, "completedAt": now}
	for _, w := range []struct {
		p string
		v obj
	}{{e.globalRegistryFile(), reg}, {e.registryFile(id), breg}, {e.stateFile(id), state},
		{filepath.Join(cat, "export-plan.json"), plan}, {filepath.Join(cat, "router-delete-result.json"), result}} {
		if err := writeObj(w.p, w.v); err != nil {
			return err
		}
	}
	return nil
}

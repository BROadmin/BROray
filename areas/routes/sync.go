package routes

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/keenetic"
	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

// ---------- Keenetic access (exact 3.x grammar, routes-router-config.sh:68-126) ----------

// allowRouteCommand is broray_routes_static_write_command_allowed.
func allowRouteCommand(cmd string) bool {
	if cmd == "system configuration save" {
		return true
	}
	f := strings.Split(cmd, " ")
	if len(f) == 6 && f[0] == "ip" && f[1] == "route" && isIPv4Token(f[2]) && isIPv4Token(f[3]) && reProxy.MatchString(f[4]) && f[5] == "1200" {
		return keenetic.Allow(keenetic.AreaRoutes, cmd) == nil
	}
	if len(f) == 6 && f[0] == "no" && f[1] == "ip" && f[2] == "route" && isIPv4Token(f[3]) && isIPv4Token(f[4]) && reProxy.MatchString(f[5]) {
		return keenetic.Allow(keenetic.AreaRoutes, cmd) == nil
	}
	return false
}

func addCmd(r planRoute) string {
	return "ip route " + r.Network + " " + r.Mask + " " + r.Iface + " 1200"
}
func removeCmd(r planRoute) string { return "no ip route " + r.Network + " " + r.Mask + " " + r.Iface }

// runCommands sends one batch through the transport after the allowlist
// check. It returns how many leading commands succeeded and the error of
// the first failed one.
func (e *engine) runCommands(ctx context.Context, cmds []string, perCmd time.Duration) (int, error) {
	for _, c := range cmds {
		if !allowRouteCommand(c) {
			return 0, fmt.Errorf("ROUTES_COMMAND_REFUSED: %s", c)
		}
	}
	cctx, cancel := context.WithTimeout(ctx, perCmd*time.Duration(len(cmds)+1))
	defer cancel()
	res, err := e.T.Parse(cctx, cmds)
	if err != nil {
		return 0, err
	}
	for i := range cmds {
		if i >= len(res) || !res[i].OK {
			msg := "failed"
			if i < len(res) && res[i].Error() != "" {
				msg = res[i].Error()
			}
			return i, errors.New(cmds[i] + ": " + msg)
		}
	}
	return len(cmds), nil
}

// waitState re-reads running-config (≤ convergeAttempts, 1 s apart) until
// every route is present with metric 1200 (present) or absent.
func (e *engine) waitState(ctx context.Context, present bool, routes []planRoute) bool {
	for attempt := 1; attempt <= convergeAttempts; attempt++ {
		if s, err := e.fetchConfig(ctx); err == nil && s.complete() {
			ok := true
			for _, r := range routes {
				id := s.identityCount(r.dest(), r.Iface)
				if present {
					ok = ok && id == 1 && s.attributeCount(r.dest(), r.Iface, r.Metric) == 1
				} else {
					ok = ok && id == 0
				}
			}
			if ok {
				return true
			}
		}
		if attempt < convergeAttempts {
			if sleepFn(ctx, time.Second) != nil {
				return false
			}
		}
	}
	return false
}

// save is `system configuration save` + running==startup convergence.
func (e *engine) save(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, err := saveFn(cctx, e.T)
	e.invalidateCache()
	return err
}

func (e *engine) interfaceUp(ctx context.Context, iface string) (available, connected, up bool) {
	var v struct {
		Connected string `json:"connected"`
		State     string `json:"state"`
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := e.T.Show(cctx, "show/interface/"+iface, &v); err != nil {
		return false, false, false
	}
	return true, v.Connected == "yes", v.State == "up"
}

type baseline struct {
	routes   string
	nonRoute string
}

// captureBaseline is broray_routes_sync_capture_baseline.
func (e *engine) captureBaseline(ctx context.Context, planSnap *rcSnapshot) (*baseline, bool) {
	run, err := e.fetchConfig(ctx)
	if err != nil || !run.complete() || run.routeStateKey() != planSnap.routeStateKey() {
		return nil, false
	}
	st, err := e.fetchStartup(ctx)
	if err != nil || !st.complete() || st.routeStateKey() != run.routeStateKey() || st.nonRouteCanonical() != run.nonRouteCanonical() {
		return nil, false
	}
	return &baseline{routes: run.routeStateKey(), nonRoute: run.nonRouteCanonical()}, true
}

func (e *engine) matchesBaseline(ctx context.Context, b *baseline) bool {
	run, err := e.fetchConfig(ctx)
	if err != nil || !run.complete() || run.routeStateKey() != b.routes {
		return false
	}
	st, err := e.fetchStartup(ctx)
	return err == nil && st.complete() && st.routeStateKey() == b.routes && st.nonRouteCanonical() == run.nonRouteCanonical() &&
		run.nonRouteCanonical() == b.nonRoute
}

func (e *engine) writeRollbackMarker(work, message string) error {
	e.rollbackUncertain.Store(true)
	if fileExists(e.rollbackMarker()) {
		return nil
	}
	write := e.markerWrite
	if write == nil {
		write = writeObj
	}
	return write(e.rollbackMarker(), obj{"schemaVersion": 1, "kind": "routes-rollback-required", "blocksNewMutations": true, "work": work, "message": message})
}

// fileBackup keeps the original bytes of local files for transactional restore.
type fileBackup struct {
	files map[string][]byte // nil value: file did not exist
}

func backupFiles(paths ...string) *fileBackup {
	b, err := backupFilesChecked(paths...)
	if err != nil {
		return nil
	}
	return b
}

func backupFilesChecked(paths ...string) (*fileBackup, error) {
	b := &fileBackup{files: map[string][]byte{}}
	for _, p := range paths {
		data, err := platform.ReadRegularNoFollow(p)
		if os.IsNotExist(err) {
			b.files[p] = nil
			continue
		}
		if err != nil {
			return nil, err
		}
		b.files[p] = data
	}
	return b, nil
}

func (b *fileBackup) restore() bool {
	ok := true
	for p, data := range b.files {
		if data == nil {
			if err := platform.RemoveRegularNoFollow(p); err != nil && !os.IsNotExist(err) {
				ok = false
			}
			continue
		}
		if err := platform.WriteFileAtomicNoFollow(p, data, 0o600); err != nil {
			ok = false
		}
	}
	return ok
}

func (b *fileBackup) save(dir string) error {
	return b.saveWithMkdir(dir, platform.MkdirAllDurable)
}

func (b *fileBackup) saveWithMkdir(dir string, mkdirAll func(string, os.FileMode) error) error {
	if err := mkdirAll(dir, 0o700); err != nil {
		return err
	}
	parent, err := platform.OpenDirNoFollow(filepath.Dir(dir))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(dir)
	if name == "." || name == string(filepath.Separator) {
		return errors.New("ROUTES_BACKUP_DIR_UNOWNED")
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), dir)
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("ROUTES_BACKUP_DIR_UNOWNED")
	}
	if err := parent.Sync(); err != nil {
		return err
	}
	destinations := make(map[string][]byte, len(b.files))
	names := make([]string, 0, len(b.files))
	for p, data := range b.files {
		name := filepath.Base(filepath.Dir(p)) + "-" + filepath.Base(p)
		if data == nil {
			name += ".missing"
		}
		if _, exists := destinations[name]; exists {
			return fmt.Errorf("ROUTES_BACKUP_NAME_COLLISION: %s", name)
		}
		destinations[name] = data
		names = append(names, name)
	}
	sort.Strings(names)
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	occupied := make(map[string]bool, len(entries))
	for _, entry := range entries {
		occupied[entry.Name()] = true
	}
	for _, name := range names {
		if name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			return errors.New("ROUTES_BACKUP_NAME_INVALID")
		}
		if occupied[name] {
			return fmt.Errorf("ROUTES_BACKUP_DESTINATION_OCCUPIED: %s", filepath.Join(dir, name))
		}
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		fd, err := unix.Openat(int(directory.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if err != nil {
			return err
		}
		f := os.NewFile(uintptr(fd), path)
		n, writeErr := f.Write(destinations[name])
		if writeErr == nil && n != len(destinations[name]) {
			writeErr = io.ErrShortWrite
		}
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		fd, err = unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		readback := os.NewFile(uintptr(fd), path)
		got, readErr := io.ReadAll(readback)
		closeErr = readback.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(got, destinations[name]) {
			return errors.New("ROUTES_BACKUP_MISMATCH")
		}
	}
	return directory.Sync()
}

// ---------- export (broray_routes_sync_apply) ----------

type syncRun struct {
	e          *engine
	ctx        context.Context
	id         string
	opID       string
	plan       *syncPlan
	base       *baseline
	local      *fileBackup
	resumeBak  *fileBackup
	added      []planRoute
	deleted    []planRoute
	changed    bool
	committed  bool
	resumed    bool
	resumeBase int
	prog       *progress
	txDir      string
	rollingBak bool
}

func (s *syncRun) transaction(phase, message string) error {
	if s.txDir == "" {
		return nil
	}
	return writeObj(filepath.Join(s.txDir, "transaction.json"), obj{"schemaVersion": 2, "operation": "sync", "bundleId": s.id,
		"phase": phase, "message": message, "updatedAt": stamp()})
}

// abort is broray_routes_sync_abort: restore local files, roll the router
// back and verify it against the baseline; on failure leave the blocking
// marker.
func (s *syncRun) abort(message string) error {
	e := s.e
	ok := true
	if s.local != nil && !s.local.restore() {
		ok = false
		message += " Локальные данные не восстановлены; резервная копия сохранена."
	}
	if !s.rollbackRouter() {
		ok = false
		message += " Откат маршрутов не подтверждён running/startup-config."
	}
	switch {
	case ok && s.resumed && s.resumeBak != nil:
		if s.resumeBak.restore() {
			if err := s.transaction("resume_attempt_rolled_back", message+" Попытка продолжения отменена; прежнее resumable-состояние восстановлено."); err != nil {
				ok = false
				message += " Не удалось записать итог отката транзакции."
			} else if s.prog != nil {
				s.prog.finish()
			}
		} else {
			ok = false
			message += " Маршруты восстановлены, но resumable-состояние не восстановлено."
		}
	case ok:
		if err := s.transaction("rolled_back", message); err != nil {
			ok = false
			message += " Не удалось записать итог отката транзакции."
		} else if s.prog != nil {
			s.prog.fail(message+" Изменения операции отменены.", true)
		}
	}
	if !ok {
		_ = s.transaction("rollback_failed", message)
		if err := e.writeRollbackMarker(s.txDir, message); err != nil {
			message += " Не удалось записать блокирующий маркер; lease сохранён."
		}
		if s.prog != nil {
			s.prog.fail(message+" Требуется восстановление; новые изменения маршрутов заблокированы.", false)
		}
	}
	return cliErr("ОШИБКА: %s", message)
}

// stop rolls the current export attempt back to its verified baseline. The
// previous resumable count (if any) remains valid only after both router and
// local files are restored. Failed restoration blocks further route writes.
func (s *syncRun) stop(message string) error {
	ok := true
	if s.local != nil && !s.local.restore() {
		ok = false
		message += " Локальные данные не восстановлены; резервная копия сохранена."
	}
	if !s.rollbackRouter() {
		ok = false
		message += " Откат маршрутов не подтверждён running/startup-config."
	}
	if !ok {
		_ = s.transaction("rollback_failed", message)
		if err := s.e.writeRollbackMarker(s.txDir, message); err != nil {
			message += " Не удалось записать блокирующий маркер; lease сохранён."
		}
		s.prog.fail(message+" Требуется восстановление; новые изменения маршрутов заблокированы.", false)
		return cliErr("ОШИБКА: %s", message)
	}
	if err := s.transaction("paused", message); err != nil {
		_ = s.e.writeRollbackMarker(s.txDir, "Откат подтверждён, но итог транзакции не записан.")
		s.prog.fail(message+" Не удалось записать итог транзакции; новые изменения маршрутов заблокированы.", false)
		return cliErr("ОШИБКА: %s", message)
	}
	s.prog.current = s.resumeBase
	s.prog.pause(message+" Изменения этой попытки отменены; операцию можно продолжить.", true, "")
	return errPaused
}

// rollbackRouter is broray_routes_sync_rollback_router.
func (s *syncRun) rollbackRouter() bool {
	if !s.changed {
		return true
	}
	e := s.e
	ctx := context.WithoutCancel(s.ctx)
	if s.base == nil {
		return false
	}
	snap, err := e.fetchConfig(ctx)
	if err != nil || !snap.complete() {
		return false
	}
	failed, changed := false, false
	for i := len(s.deleted) - 1; i >= 0; i-- {
		r := s.deleted[i]
		id := snap.identityCount(r.dest(), r.Iface)
		if id == 1 && snap.attributeCount(r.dest(), r.Iface, r.Metric) == 1 {
			continue
		}
		if id != 0 {
			failed = true
			continue
		}
		if _, err := e.runCommands(ctx, []string{addCmd(r)}, 10*time.Second); err != nil {
			failed = true
		} else {
			changed = true
		}
	}
	for i := len(s.added) - 1; i >= 0; i-- {
		r := s.added[i]
		id := snap.identityCount(r.dest(), r.Iface)
		if id == 0 {
			continue
		}
		if id != 1 || snap.attributeCount(r.dest(), r.Iface, r.Metric) != 1 {
			failed = true
			continue
		}
		if _, err := e.runCommands(ctx, []string{removeCmd(r)}, 10*time.Second); err != nil {
			failed = true
		} else {
			changed = true
		}
	}
	if changed {
		_ = e.save(ctx)
	}
	limit := convergeAttempts
	if limit > 30 {
		limit = 30
	}
	verified := false
	for attempt := 1; attempt <= limit; attempt++ {
		if e.matchesBaseline(ctx, s.base) {
			verified = true
			break
		}
		if attempt < limit {
			_ = sleepFn(ctx, time.Second)
		}
	}
	if !verified {
		failed = true
	}
	if !failed {
		_ = os.Remove(e.rollbackMarker())
	}
	return !failed
}

// runExport is `broray-routes export <id>`: build-export, then the
// transactional apply of routes-router-sync.sh:1577-1922.
func createRouteTransactionDir(transactions, prefix string, beforeCreate func()) (string, error) {
	if prefix == "" || filepath.Base(prefix) != prefix {
		return "", errors.New("Некорректное имя транзакции маршрутов.")
	}
	parent, err := platform.OpenDirNoFollow(transactions)
	if err != nil {
		return "", fmt.Errorf("Каталог транзакций маршрутов не подтверждён: %w", err)
	}
	defer parent.Close()
	owned, err := parent.Stat()
	if err != nil || !owned.IsDir() {
		return "", fmt.Errorf("Каталог транзакций маршрутов не принадлежит BROray: %v", err)
	}
	verifyParent := func() error {
		current, err := platform.OpenDirNoFollow(transactions)
		if err != nil {
			return err
		}
		defer current.Close()
		info, err := current.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(owned, info) {
			return errors.New("каталог транзакций маршрутов изменился во время создания")
		}
		return nil
	}
	if beforeCreate != nil {
		beforeCreate()
	}
	if err := verifyParent(); err != nil {
		return "", fmt.Errorf("Каталог транзакций маршрутов не подтверждён: %w", err)
	}
	for attempt := 0; attempt < 16; attempt++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("Не удалось создать имя транзакции маршрутов: %w", err)
		}
		name := prefix + hex.EncodeToString(random[:])
		if err := unix.Mkdirat(int(parent.Fd()), name, 0o700); errors.Is(err, unix.EEXIST) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("Не удалось создать каталог транзакции маршрутов: %w", err)
		}
		if err := parent.Sync(); err != nil {
			return "", fmt.Errorf("Не удалось подтвердить каталог транзакции маршрутов: %w", err)
		}
		if err := verifyParent(); err != nil {
			if removeErr := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); removeErr != nil {
				return "", fmt.Errorf("Каталог транзакций маршрутов изменился: %w; пустой каталог транзакции не удалён: %v", err, removeErr)
			}
			if syncErr := parent.Sync(); syncErr != nil {
				return "", fmt.Errorf("Каталог транзакций маршрутов изменился: %w; удаление пустого каталога не подтверждено: %v", err, syncErr)
			}
			return "", fmt.Errorf("Каталог транзакций маршрутов изменился: %w", err)
		}
		return filepath.Join(transactions, name), nil
	}
	return "", errors.New("Не удалось создать уникальное имя транзакции маршрутов.")
}

func (e *engine) runExport(ctx context.Context, id, opID string) (string, error) {
	s := &syncRun{e: e, ctx: ctx, id: id, opID: opID}
	if e.rollbackBlocked() {
		return "", cliErr("ОШИБКА: %s", msgRollbackBlock)
	}
	cat := e.catalogDir(id)
	resultFile := filepath.Join(cat, "router-export-result.json")
	var err error
	s.local, err = backupFilesChecked(e.globalRegistryFile(), e.registryFile(id), e.stateFile(id),
		filepath.Join(cat, "export-plan.json"), filepath.Join(cat, "keenetic-routes.bat"), resultFile)
	if err != nil {
		return "", cliErr("ОШИБКА: Не удалось прочитать исходные локальные данные маршрутов: %v", err)
	}
	transactions := e.p("transactions")
	s.txDir, err = createRouteTransactionDir(transactions, "sync-"+id+"-", nil)
	if err != nil {
		return "", cliErr("ОШИБКА: %v", err)
	}
	if err := s.local.save(filepath.Join(s.txDir, "original")); err != nil {
		return "", cliErr("ОШИБКА: Не удалось сохранить резервную копию маршрутов: %v", err)
	}
	if err := s.transaction("captured", "Исходные локальные данные сохранены; Keenetic ещё не изменён."); err != nil {
		return "", cliErr("ОШИБКА: Не удалось записать начало транзакции маршрутов: %v", err)
	}
	txFile := filepath.Join(s.txDir, "transaction.json")
	e.beginLiveTransaction(txFile)
	defer e.endLiveTransaction(txFile)
	if _, _, _, err := e.runBuildExport(id); err != nil {
		return "", s.abort(err.Error())
	}
	iface := e.managedInterface()
	if !e.ensureGlobalRegistry(iface) {
		return "", s.abort("Не удалось восстановить или проверить общий реестр маршрутов.")
	}
	if err := e.save(ctx); err != nil {
		return "", s.abort("Не удалось сохранить текущую конфигурацию Keenetic перед установкой маршрутов.")
	}
	plan, err := e.buildSyncPlan(ctx, id)
	if err != nil {
		return "", s.abort("Не удалось построить безопасный план установки.")
	}
	s.plan = plan
	if !plan.CanApply {
		return "", s.abort("План содержит конфликты или внешние маршруты на управляемом ProxyN.")
	}
	base, ok := e.captureBaseline(ctx, plan.Snap)
	if !ok {
		return "", s.abort("Running/startup-config не совпадают до операции; изменения не начаты.")
	}
	s.base = base
	if _, conn, up := e.interfaceUp(ctx, plan.Iface); !conn || !up {
		return "", s.abort(plan.Iface + " не подключён или не находится в состоянии up.")
	}
	if err := writeObj(filepath.Join(s.txDir, "plan.json"), plan.object()); err != nil {
		return "", s.abort("Не удалось сохранить план транзакции до изменения маршрутов.")
	}
	if err := s.transaction("planned", "Безопасный план подготовлен; Keenetic ещё не изменён."); err != nil {
		return "", s.abort("Не удалось подтвердить план транзакции до изменения маршрутов.")
	}

	var toCreate, toDelete []planRoute
	for _, d := range plan.Desired {
		if d.Status == "create" {
			toCreate = append(toCreate, d.Route)
		}
	}
	for _, o := range plan.Obsolete {
		if o.Status == "shared_restore" && o.Route != nil {
			toCreate = append(toCreate, *o.Route)
		}
	}
	for _, o := range plan.Obsolete {
		if o.Status == "delete" && o.Route != nil {
			toDelete = append(toDelete, *o.Route)
		}
	}
	remaining := len(toCreate) + len(toDelete)
	mode := plan.Mode
	base0, total, resumed := e.resumeValues(id, mode, remaining)
	s.resumed = resumed
	s.resumeBase = base0
	s.resumeBak = backupFiles(e.progressFile(id), e.counterFile(id))
	if s.resumeBak == nil {
		return "", s.abort("Не удалось прочитать исходное состояние продолжения операции.")
	}
	msg := map[string]string{"install": "Установка маршрутов в Keenetic.", "update": "Обновление маршрутов в Keenetic.",
		"restore": "Восстановление маршрутов в Keenetic."}[mode]
	if msg == "" {
		mode, msg = "install", "Установка маршрутов в Keenetic."
	}
	processed := base0
	s.prog = e.beginProgress(id, mode, opID, total, processed, resumed, fmt.Sprintf("Подготовка операции: %d из %d.", processed, total))
	s.prog.update("applying", processed, msg, "")
	s.changed = true
	e.enableRouteStop(id, opID)

	apply := func(list []planRoute, add bool) error {
		for start := 0; start < len(list); start += verifyBatchSize {
			if e.routeStopRequested(id, opID) {
				return s.stop("Установка маршрутов остановлена по запросу пользователя.")
			}
			end := start + verifyBatchSize
			if end > len(list) {
				end = len(list)
			}
			batch := list[start:end]
			cmds := make([]string, len(batch))
			for i, r := range batch {
				if add {
					cmds[i] = addCmd(r)
					s.added = append(s.added, r)
				} else {
					cmds[i] = removeCmd(r)
					s.deleted = append(s.deleted, r)
				}
			}
			n, err := e.runCommands(ctx, cmds, 10*time.Second)
			for i := 0; i < n; i++ {
				processed++
				s.prog.tick(processed, batch[i].dest())
			}
			if err != nil {
				r := batch[n]
				if add {
					return s.abort("Не удалось добавить маршрут " + r.dest() + ".")
				}
				return s.abort("Не удалось удалить устаревший маршрут " + r.dest() + ".")
			}
			if !e.waitState(ctx, add, batch) {
				last := end == len(list)
				switch {
				case add && last:
					return s.abort("Итоговое пакетное добавление маршрутов не сошлось в running-config.")
				case add:
					return s.abort("Пакетное добавление маршрутов не сошлось в running-config.")
				case last:
					return s.abort("Итоговое пакетное удаление маршрутов не сошлось в running-config.")
				default:
					return s.abort("Пакетное удаление маршрутов не сошлось в running-config.")
				}
			}
			if e.routeStopRequested(id, opID) {
				return s.stop("Установка маршрутов остановлена по запросу пользователя.")
			}
		}
		return nil
	}
	if err := apply(toCreate, true); err != nil {
		if errors.Is(err, errPaused) {
			return "Операция остановлена с откатом; можно продолжить позднее.\n", err
		}
		return "", err
	}
	if err := apply(toDelete, false); err != nil {
		if errors.Is(err, errPaused) {
			return "Операция остановлена с откатом; можно продолжить позднее.\n", err
		}
		return "", err
	}
	s.prog.update("verifying", processed, "Проверка маршрутов в Keenetic.", "")
	after, err := e.fetchConfig(ctx)
	if err != nil {
		return "", s.abort("Не удалось повторно прочитать running-config после изменений.")
	}
	ok = true
	for _, d := range plan.Desired {
		ok = ok && after.identityCount(d.Route.dest(), plan.Iface) == 1 && after.attributeCount(d.Route.dest(), plan.Iface, d.Route.Metric) == 1
	}
	for _, o := range plan.Obsolete {
		if o.Route == nil {
			continue
		}
		switch o.Status {
		case "shared_keep", "shared_restore":
			ok = ok && after.identityCount(o.Route.dest(), plan.Iface) == 1 && after.attributeCount(o.Route.dest(), plan.Iface, o.Route.Metric) == 1
		case "delete", "already_absent":
			ok = ok && after.identityCount(o.Route.dest(), plan.Iface) == 0
		}
	}
	if !ok {
		return "", s.abort("Итоговая конфигурация Keenetic не совпала с безопасным планом.")
	}
	s.transaction("router_verified", "Изменения в Keenetic применены и проверены.")
	if e.closeRouteStop(id, opID) {
		return "Операция остановлена с откатом; можно продолжить позднее.\n", s.stop("Установка маршрутов остановлена по запросу пользователя.")
	}
	if err := s.commitLocal(); err != nil {
		return "", s.abort(err.Error())
	}
	s.committed = true
	s.transaction("local_committed", "Локальные реестры обновлены.")
	s.prog.update("saving", processed, "Сохранение конфигурации Keenetic.", "")
	if err := e.save(ctx); err != nil {
		return "", s.abort("Не удалось сохранить конфигурацию Keenetic.")
	}
	if err := s.transaction("committed", "Маршруты применены, проверены и сохранены."); err != nil {
		_ = e.writeRollbackMarker(s.txDir, "Маршруты применены, но итог транзакции не записан.")
		s.prog.fail("Маршруты применены, но итог транзакции не записан; новые изменения заблокированы.", false)
		return "", cliErr("ОШИБКА: Маршруты применены, но итог транзакции не записан: %v", err)
	}
	s.prog.complete(fmt.Sprintf("%s Выполнено %d из %d.", msg, total, total))
	res, _ := readObj(resultFile)
	sum := sub(res, "summary")
	out := []string{str(res, "message"), "Набор: " + id, "Управляемый интерфейс: " + plan.Iface,
		"Всего маршрутов: " + itoa(intOr(sum["total"], 0)), "Добавлено: " + itoa(len(s.added)), "Удалено: " + itoa(len(s.deleted)),
		"Сохранено общих: " + itoa(intOr(sum["sharedKept"], 0)), "Конфигурация сохранена: true"}
	return strings.Join(out, "\n") + "\n", nil
}

// commitLocal is broray_routes_sync_prepare_local + the atomic installs.
func (s *syncRun) commitLocal() error {
	e, plan, id := s.e, s.plan, s.id
	now := stamp()
	state, ok := readObj(e.stateFile(id))
	reg, ok2 := readObj(e.globalRegistryFile())
	breg, ok3 := readObj(e.registryFile(id))
	cat := e.catalogDir(id)
	eplan, ok4 := readObj(filepath.Join(cat, "export-plan.json"))
	if !ok || !ok2 || !ok3 || !ok4 {
		return errors.New("Не удалось подготовить новые локальные реестры.")
	}
	created := intOr(sub(state, "resumeOperation")["created"], 0)
	deleted := intOr(sub(state, "resumeOperation")["deleted"], 0)
	for range s.added {
		created++
	}
	deleted += len(s.deleted)
	sm := plan.Summary
	total := intOr(sm["total"], 0)
	message := map[string]string{"install": "Маршруты установлены в Keenetic", "update": "Маршруты обновлены в Keenetic",
		"restore": "Маршруты восстановлены в Keenetic"}[plan.Mode]
	if message == "" {
		message = "Маршруты в Keenetic проверены"
	}
	// global registry: drop this owner, then own every desired create/managed_existing route
	var routes []any
	idx := map[string]int{}
	for _, x := range anyList(reg["routes"]) {
		m, _ := x.(obj)
		owners := strList(m["owners"])
		has := false
		var rest []string
		for _, o := range owners {
			if o == id {
				has = true
			} else {
				rest = append(rest, o)
			}
		}
		if has {
			m["owners"] = toAny(sortedUnique(rest))
			m["updatedAt"] = now
			if len(rest) == 0 {
				continue
			}
		}
		idx[str(m, "key")] = len(routes)
		routes = append(routes, m)
	}
	var keys []string
	for _, d := range plan.Desired {
		keys = append(keys, d.Route.Key)
		if d.Status != "create" && d.Status != "managed_existing" {
			continue
		}
		if i, ok := idx[d.Route.Key]; ok {
			m := routes[i].(obj)
			m["owners"] = toAny(sortedUnique(append(strList(m["owners"]), id)))
			m["actualStatus"] = "present"
			m["updatedAt"] = now
		} else {
			idx[d.Route.Key] = len(routes)
			routes = append(routes, registryEntry(d.Route, id, now))
		}
	}
	if routes == nil {
		routes = []any{}
	}
	reg["schemaVersion"], reg["managedInterface"], reg["managedMetric"], reg["routes"], reg["updatedAt"] = 1, plan.Iface, managedMetric, routes, now
	ukeys := toAny(sortedUnique(keys))
	breg["schemaVersion"], breg["bundleId"], breg["installedVersion"] = 1, id, state["downloadedVersion"]
	breg["routeKeys"], breg["managedRouteKeys"], breg["externalRouteKeys"] = ukeys, clone(ukeys), []any{}
	breg["targetInterface"], breg["managedMetric"], breg["removedAt"], breg["updatedAt"] = plan.Iface, managedMetric, nil, now
	if breg["installedAt"] == nil {
		breg["installedAt"] = now
	}
	dv := sub(state, "downloadedVersion")
	state["status"] = "installed"
	state["installedVersion"] = state["downloadedVersion"]
	state["lastExportedAt"] = now
	state["lastVerifiedAt"] = now
	state["verifyResult"] = obj{"result": "complete", "success": true,
		"message": fmt.Sprintf("Набор исправен. В Keenetic найдено %d из %d маршрутов.", total, total), "checkedAt": now,
		"contentSha256": orNull(dv, "contentSha256"),
		"local":         obj{"valid": true, "routeCount": total, "sourceFileCount": intOr(orNull(dv, "sourceFileCount"), 0), "duplicateRouteCount": 0, "invalidRouteCount": 0},
		"keenetic": obj{"checked": true, "available": true, "status": "complete", "expectedRouteCount": total, "presentRouteCount": total,
			"missingRouteCount": 0, "conflictCount": 0, "externalRouteCount": 0, "complete": true, "updatePending": false,
			"mode": plan.Mode, "canApply": true, "targetInterface": plan.Iface, "managedMetric": managedMetric}}
	state["preflight"] = plan.object()
	state["exportResult"] = obj{"result": "installed", "operation": plan.Mode, "message": message, "total": total, "created": created,
		"deleted": deleted, "sharedKept": sm["sharedKept"], "sharedRestored": sm["sharedToRestore"], "alreadyAbsent": sm["alreadyAbsent"],
		"unchanged": sm["unchangedRoutes"], "targetInterface": plan.Iface, "managedMetric": managedMetric, "configurationSaved": true, "completedAt": now}
	state["lastError"] = nil
	state["resumeOperation"] = nil
	state["updatedAt"] = now
	eplan["routerApplied"], eplan["configurationSaved"], eplan["appliedAt"] = true, true, now
	eplan["applyResult"] = obj{"operation": plan.Mode, "message": message, "created": created, "deleted": deleted,
		"sharedKept": sm["sharedKept"], "sharedRestored": sm["sharedToRestore"]}
	summary := cloneObj(sm)
	summary["created"], summary["deleted"] = created, deleted
	result := obj{"schemaVersion": 1, "bundleId": id, "targetInterface": plan.Iface, "managedMetric": managedMetric,
		"sourceCommit": orNull(dv, "sourceCommit"), "contentSha256": orNull(dv, "contentSha256"), "operation": plan.Mode, "message": message,
		"summary": summary, "routerChanged": created+deleted > 0, "configurationSaved": true, "completedAt": now}
	for _, w := range []struct {
		p string
		v any
		m string
	}{
		{e.globalRegistryFile(), reg, "Не удалось записать общий реестр."},
		{e.registryFile(id), breg, "Не удалось записать реестр набора."},
		{e.stateFile(id), state, "Не удалось записать состояние набора."},
		{filepath.Join(cat, "export-plan.json"), eplan, "Не удалось записать план набора."},
		{filepath.Join(cat, "router-export-result.json"), result, "Не удалось записать результат установки."},
	} {
		if err := writeObj(w.p, w.v); err != nil {
			return errors.New(w.m)
		}
	}
	return nil
}

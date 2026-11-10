package xray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BROadmin/broray/areas/servers"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	mxray "github.com/BROadmin/broray/modules/xray"
)

// Install job tuning (tests override).
var (
	// DownloadBase is the release host handed to modules/xray.Installer ("" = github.com).
	DownloadBase = ""
	// DownloadClient overrides the installer HTTP client (tests).
	DownloadClient   *http.Client
	installStartWait = 180 * time.Second
)

const keepOperationDirs = 20

var reOperationID = regexp.MustCompile(`^op-[0-9]{14}-[0-9]+-[0-9a-f]{12}$|^op-q-[0-9a-f]{32}$`)

func (s *service) pointerPath() string { return s.d.RunPath("xray-web-operation.json") }
func (s *service) opsDir() string      { return s.d.RunPath("xray-operations") }

// installRequest is the validated X8 body (broray_xray_install_request_valid).
type installRequest struct {
	Tag             string
	CurrentVersion  string
	ArchiveSHA256   string
	AllowDowngrade  bool
	AllowPrerelease bool
	AllowUntested   bool
	AllowPartial    bool
}

func parseInstallRequest(body []byte) (installRequest, bool) {
	var r installRequest
	var m map[string]json.RawMessage
	b := strings.TrimSpace(string(body))
	if len(body) == 0 || len(body) > 4096 || b == "" || b[0] != '{' {
		return r, false
	}
	dec := json.NewDecoder(strings.NewReader(b))
	if dec.Decode(&m) != nil || m == nil {
		return r, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return r, false
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "allowPartial" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "allowDowngrade,allowPrerelease,allowUntested,archiveSha256,currentVersion,tag" {
		return r, false
	}
	str := func(k string, p *string) bool { return json.Unmarshal(m[k], p) == nil && string(m[k]) != "null" }
	boolean := func(k string, p *bool) bool {
		raw := strings.TrimSpace(string(m[k]))
		return (raw == "true" || raw == "false") && json.Unmarshal(m[k], p) == nil
	}
	if !str("tag", &r.Tag) || !validTag(r.Tag) || !str("currentVersion", &r.CurrentVersion) || !str("archiveSha256", &r.ArchiveSHA256) {
		return r, false
	}
	if len(r.ArchiveSHA256) != 64 || strings.Trim(r.ArchiveSHA256, "0123456789abcdef") != "" {
		return r, false
	}
	if !boolean("allowDowngrade", &r.AllowDowngrade) || !boolean("allowPrerelease", &r.AllowPrerelease) || !boolean("allowUntested", &r.AllowUntested) {
		return r, false
	}
	if _, has := m["allowPartial"]; has && !boolean("allowPartial", &r.AllowPartial) {
		return r, false
	}
	return r, true
}

// handleLaunch is X8/X10/X11 (lib/xray-web-launch.sh).
func (s *service) handleLaunch(c *cgi.Req, mode string) {
	fail := func(e *cgi.APIError) { cgi.WriteErr(c.W, e) }
	if exists(s.d.RunPath("xray-web-operation.pid")) {
		fail(cgi.E(409, "XRAY_LEGACY_OWNER_UNCONFIRMED", "Владелец предыдущей операции Xray не подтверждён. Откройте диагностику операций."))
		return
	}
	release, ok := s.d.Lock.TryAcquire("xray", "xray:"+mode, "xray")
	if !ok {
		fail(cgi.E(409, "OPERATION_BUSY", "Другая конфликтующая операция BROray уже выполняется."))
		return
	}
	op := s.d.Queue.StartOperation("", "xray_maintenance", "xray:"+mode, "USER", "cooperative", "")
	abort := func(e *cgi.APIError) {
		op.Finish(errors.New(e.Code + ": " + e.Message))
		release()
		fail(e)
	}
	work := filepath.Join(s.opsDir(), op.ID)
	if err := os.MkdirAll(s.opsDir(), 0o700); err != nil || os.Mkdir(work, 0o700) != nil {
		abort(cgi.E(500, "XRAY_OPERATION_STATE_FAILED", "Не удалось подготовить каталог операции Xray."))
		return
	}
	if os.WriteFile(filepath.Join(work, "operation-id"), []byte(op.ID+"\n"), 0o600) != nil {
		abort(cgi.E(500, "XRAY_OPERATION_STATE_FAILED", "Не удалось записать идентификатор операции Xray."))
		return
	}
	var req installRequest
	if mode == "install" {
		var ok bool
		if req, ok = parseInstallRequest(c.Body()); !ok || c.R.ContentLength <= 0 {
			abort(cgi.E(400, "XRAY_INSTALL_REQUEST_INVALID", "Некорректный запрос выбора версии Xray. Обновите список версий."))
			return
		}
		_ = os.WriteFile(filepath.Join(work, "request.json"), c.Body(), 0o600)
	}
	pointer := map[string]any{"backgroundOperationId": op.ID, "operation": mode, "operationRunning": true, "running": true,
		"success": nil, "result": nil, "error": nil}
	if compat.WriteJSON(s.pointerPath(), pointer) != nil {
		abort(cgi.E(503, "XRAY_OPERATION_STATE_FAILED", "Не удалось сохранить начало операции Xray."))
		return
	}
	ctx, cancel := context.WithCancel(s.d.Ctx)
	op.SetCancel(cancel)
	s.jobs.Add(1)
	go func() {
		defer s.jobs.Done()
		defer release()
		defer cancel()
		s.runJob(ctx, op, work, mode, req)
	}()
	cgi.WriteOKStatus(c.W, 202, map[string]any{"accepted": true, "operation": mode, "operationId": op.ID, "pid": nil})
}

// jobLog appends to <work>/worker.log (exposed as logTail).
type jobLog struct {
	mu   sync.Mutex
	path string
}

func (l *jobLog) printf(format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format("15:04:05"), fmt.Sprintf(format, a...))
}

type jobError struct {
	msg     string
	details string
}

func (e *jobError) Error() string { return e.msg }

func jerr(msg string) error { return &jobError{msg: msg} }

// runJob is the worker: checks, download, verify, swap, restart, finish.
func (s *service) runJob(ctx context.Context, op *compat.Operation, work, mode string, req installRequest) {
	log := &jobLog{path: filepath.Join(work, "worker.log")}
	log.printf("Xray %s: операция %s начата", mode, op.ID)
	op.Phase("fetching")
	out, err := s.install(ctx, op, log, mode, req)
	cancelled := ctx.Err() != nil && s.d.Ctx.Err() == nil && errors.Is(err, context.Canceled) || op.CancelRequested()
	var errText any
	success := err == nil
	if err != nil {
		var je *jobError
		switch {
		case cancelled:
			errText = "Операция Xray отменена."
		case errors.As(err, &je):
			errText = je.msg
		default:
			errText = "Операция Xray завершилась с ошибкой. Откройте диагностику."
		}
		if out == nil {
			out = map[string]any{"success": false, "error": errText}
			if errors.As(err, &je) && je.details != "" {
				out["details"] = je.details
			}
		} else if e, ok := out["error"].(string); ok {
			errText = e
		}
		log.printf("Ошибка: %v", errText)
	} else {
		log.printf("%v", out["message"])
	}
	public := mode
	if mode == "bootstrap" {
		public = "install"
	}
	_ = compat.WriteJSON(s.pointerPath(), map[string]any{"backgroundOperationId": op.ID, "operation": public, "success": success,
		"running": false, "operationRunning": false, "pid": nil, "workerPid": nil, "result": out, "error": errText,
		"completedAt": compat.StampUTC(time.Now())})
	s.invalidateStatus()
	if err != nil && !cancelled {
		op.Finish(fmt.Errorf("XRAY_INSTALL_FAILED: %v", errText))
	} else {
		op.Finish(err)
	}
}

// selectTarget applies the selected/legacy checks (broray_xray_selected_check,
// xray-releases.sh:229-268): the release is re-resolved on GitHub, its
// official archive digest must still equal the confirmed one, and the
// owner's consents must cover untested, partially compatible, pre-release
// and downgrade targets. Returns the tag and the confirmed archive SHA-256.
func (s *service) selectTarget(ctx context.Context, mode string, req installRequest) (string, string, error) {
	current := installedVersion(s.versionOutput(ctx, s.d.Root.XrayBinary()))
	if _, err := mxray.CurrentAsset(); err != nil {
		return "", "", jerr("Архитектура устройства не поддерживается этим пакетом BROray.")
	}
	switch mode {
	case "bootstrap":
		// First install by the daemon: the version pinned (and tested) in
		// this build, verified against its compiled-in digest.
		_, sum, ok := mxray.PinnedDigest(req.Tag)
		if !ok || current != "" {
			return "", "", jerr("Не удалось подтвердить выбранный официальный релиз Xray.")
		}
		return req.Tag, sum, nil
	case "update", "reinstall":
		if current == "" {
			return "", "", jerr("Не удалось определить установленную версию Xray.")
		}
		catalog, _, err := s.fetchCatalog(ctx, current)
		if err != nil {
			return "", "", jerr("Не удалось получить список официальных релизов Xray.")
		}
		var latest, installed *release
		for i := range catalog {
			if latest == nil && !catalog[i].Prerelease {
				latest = &catalog[i]
			}
			if strings.TrimPrefix(catalog[i].Tag, "v") == current {
				installed = &catalog[i]
			}
		}
		if mode == "update" {
			if latest == nil || versionKey(latest.Tag) <= versionKey(current) {
				return "", "", jerr("Новая версия Xray отсутствует.")
			}
			req = installRequest{Tag: latest.Tag, CurrentVersion: current, ArchiveSHA256: latest.SHA256}
		} else {
			if latest != nil && versionKey(current) > versionKey(latest.Tag) {
				return "", "", jerr("Автоматическое понижение версии запрещено.")
			}
			if installed == nil {
				if latest != nil && versionKey(latest.Tag) > versionKey(current) {
					return "", "", jerr("Для доступной новой версии нужно использовать update.")
				}
				return "", "", jerr("Не удалось подтвердить выбранный официальный релиз Xray.")
			}
			req = installRequest{Tag: installed.Tag, CurrentVersion: current, ArchiveSHA256: installed.SHA256}
		}
	}
	if current != req.CurrentVersion {
		return "", "", jerr("Установленная версия изменилась. Обновите список и повторите выбор.")
	}
	if id := servers.ActiveID(s.d); id != "" {
		rec, err := servers.ReadRecord(s.d, id)
		if err != nil {
			return "", "", jerr("Не удалось проверить требования активного сервера к версии Xray.")
		}
		if err := servers.CheckCoreSupport(rec, req.Tag); err != nil {
			return "", "", jerr(err.Error())
		}
	}
	rel, err := s.releaseByTag(ctx, req.Tag)
	if err != nil {
		return "", "", jerr("Не удалось подтвердить выбранный официальный релиз Xray.")
	}
	if rel.SHA256 != req.ArchiveSHA256 {
		return "", "", jerr("Контрольная сумма выбранного релиза изменилась. Повторите проверку версий.")
	}
	switch compatibilityFor(rel.Tag, rel.SHA256)["status"] {
	case "incompatible":
		if !req.AllowPartial {
			return "", "", jerr("Подтвердите установку частично совместимой версии Xray после ознакомления с её ограничениями.")
		}
	case "untested":
		if !req.AllowUntested {
			return "", "", jerr("Подтвердите установку версии, не проверявшейся на совместимость с BROray.")
		}
	}
	if rel.Prerelease && !req.AllowPrerelease {
		return "", "", jerr("Подтвердите установку предварительного релиза.")
	}
	if current != "" && versionKey(rel.Tag) < versionKey(current) && !req.AllowDowngrade {
		return "", "", jerr("Подтвердите понижение версии Xray.")
	}
	return rel.Tag, rel.SHA256, nil
}

func (s *service) install(ctx context.Context, op *compat.Operation, log *jobLog, mode string, req installRequest) (map[string]any, error) {
	tag, sum, err := s.selectTarget(ctx, mode, req)
	if err != nil {
		return nil, err
	}
	bin := s.d.Root.XrayBinary()
	previous := installedVersion(s.versionOutput(ctx, bin))
	var curSize int64
	if st, ok := regular(bin); ok {
		curSize = st.Size()
	}
	free := freeKB(s.d.Root.Runtime()) * 1024
	if need := xrayStorageRequired(curSize, 0); free < need {
		return nil, jerr(fmt.Sprintf("На /opt недостаточно места для атомарной замены Xray: требуется %d байт, доступно %d байт.", need, free))
	}
	inst := mxray.NewInstaller(s.d.Root, s.d.App.Cfg.Xray.Repo, tag)
	if inst.Repo == "" {
		inst.Repo = "XTLS/Xray-core"
	}
	inst.SHA256 = sum
	inst.BaseURL = DownloadBase
	if DownloadClient != nil {
		inst.Client = DownloadClient
	}
	last := time.Time{}
	inst.Progress = func(stage string, done, total int64) {
		if stage != "download" || time.Since(last) >= 2*time.Second || done == total {
			last = time.Now()
			if stage == "download" {
				log.printf("Загрузка %s: %d из %d байт", tag, done, total)
			} else {
				log.printf("Этап: %s", stage)
			}
		}
	}
	log.printf("Загрузка официального архива %s и проверка SHA-256", tag)
	candidate, version, err := inst.Prepare(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.printf("%v", err)
		msg := "Не удалось загрузить и проверить официальный архив Xray."
		if strings.HasPrefix(err.Error(), "XRAY_DIGEST_MISMATCH") {
			msg = "Контрольная сумма загруженного архива Xray не совпадает с официальной."
		}
		return nil, &jobError{msg: msg, details: err.Error()}
	}
	defer os.Remove(candidate)
	if version != strings.TrimPrefix(tag, "v") {
		return nil, jerr("Версия загруженного Xray не совпадает с выбранной.")
	}
	if cfg := s.d.Root.XrayConfig(); fileExists(cfg) {
		if out, ok := s.runTest(ctx, candidate, cfg); !ok {
			return nil, &jobError{msg: "Новая версия Xray несовместима с действующей конфигурацией BROray.", details: tailLines(out, 20)}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	op.Phase("switching")
	op.SetCancelability("protected")
	log.printf("Замена исполняемого файла Xray")
	return s.swap(context.WithoutCancel(ctx), log, mode, candidate, previous, version)
}

// swap replaces runtime/xray by the verified candidate. The previous binary
// is kept only for the duration of the swap (runtime/xray.old) and removed
// once the new one is confirmed; on failure it is moved back.
func (s *service) swap(ctx context.Context, log *jobLog, mode, candidate, previous, version string) (map[string]any, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	bin := s.d.Root.XrayBinary()
	old := bin + ".old"
	wasRunning := s.d.App.Xray.Running()
	if wasRunning {
		if err := s.d.App.Xray.Stop(ctx); err != nil || !s.waitStopped(ctx) {
			return nil, jerr("Не удалось остановить Xray перед заменой.")
		}
	}
	hadOld := fileExists(bin)
	if hadOld {
		_ = os.Remove(old)
		if err := os.Rename(bin, old); err != nil {
			s.restartIf(ctx, wasRunning)
			return nil, jerr("Не удалось подготовить замену Xray.")
		}
	}
	reason := ""
	if err := os.Chmod(candidate, 0o755); err != nil {
		reason = "Не удалось подготовить новый Xray."
	} else if err := os.Rename(candidate, bin); err != nil {
		reason = "Не удалось установить новый Xray."
	}
	if reason == "" && installedVersion(s.versionOutput(ctx, bin)) != version {
		reason = "Новый Xray не прошёл проверку версии."
	}
	if reason == "" && wasRunning {
		if err := s.d.App.Xray.Start(ctx); err != nil || !s.waitRunning(ctx) || !s.waitSocks(ctx, installStartWait) {
			reason = "Новый Xray не запустился."
		}
	}
	if reason == "" {
		_ = os.Remove(old)
		sum := fileSHA256(bin)
		msg := "Новая версия Xray безопасно установлена."
		if mode == "reinstall" {
			msg = "Текущая версия Xray безопасно переустановлена."
		}
		if wasRunning {
			s.recordDeviceTrial(version, sum, mode)
		}
		return map[string]any{"success": true, "installed": true, "mode": mode, "previousVersion": nullStr(previous), "version": version,
			"sha256": sum, "running": s.d.App.Xray.Running(), "message": msg}, nil
	}
	log.printf("%s Откат к предыдущему Xray.", reason)
	_ = s.d.App.Xray.Stop(ctx)
	restored := true
	if hadOld {
		if err := os.Rename(old, bin); err != nil {
			restored = false
		}
	} else {
		_ = os.Remove(bin)
	}
	if restored && wasRunning {
		restored = s.d.App.Xray.Start(ctx) == nil && s.waitRunning(ctx)
	}
	msg := reason + " Предыдущий Xray восстановлен."
	if !restored {
		msg = reason + " Восстановление не подтверждено. Резервный бинарник и блокировка сохранены."
	}
	return map[string]any{"success": false, "rolledBack": true, "rollbackSuccess": restored, "error": msg}, jerr(msg)
}

func (s *service) restartIf(ctx context.Context, running bool) {
	if running {
		_ = s.d.App.Xray.Start(ctx)
	}
}

// recoverInstall repairs what a daemon stop during an install left behind:
// a candidate, a swap in progress, a pointer still marked running.
func (s *service) recoverInstall() {
	bin := s.d.Root.XrayBinary()
	_ = os.Remove(bin + ".new")
	if old := bin + ".old"; fileExists(old) {
		if !fileExists(bin) || installedVersion(s.versionOutput(context.Background(), bin)) == "" {
			_ = os.Rename(old, bin)
			s.d.Log.Printf("xray: restored the previous binary after an interrupted install")
		} else {
			_ = os.Remove(old)
		}
	}
	if m, ok := compat.ReadObject(s.pointerPath()); ok && m["running"] == true {
		m["running"], m["operationRunning"], m["success"] = false, false, false
		m["error"] = "Операция Xray прервана. Откройте диагностику."
		m["completedAt"] = compat.StampUTC(time.Now())
		if m["result"] == nil {
			m["result"] = map[string]any{}
		}
		_ = compat.WriteJSON(s.pointerPath(), m)
	}
}

// pruneOperations keeps the newest operation directories (and the current one).
func (s *service) pruneOperations() {
	entries, err := os.ReadDir(s.opsDir())
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	cur := ""
	if m, ok := compat.ReadObject(s.pointerPath()); ok {
		cur, _ = m["backgroundOperationId"].(string)
	}
	for i, n := range names {
		if i >= keepOperationDirs && n != cur {
			_ = os.RemoveAll(filepath.Join(s.opsDir(), n))
		}
	}
}

// handleOperationStatus is X9 (lib/xray-web-status.sh).
func (s *service) handleOperationStatus(c *cgi.Req) (any, error) {
	var result any
	pointer, ok := compat.ReadObject(s.pointerPath())
	id := ""
	if ok {
		result = pointer
		id, _ = pointer["backgroundOperationId"].(string)
		if !reOperationID.MatchString(id) {
			id = ""
		}
	}
	var running any = false
	complete := true
	var job any
	var logTail any
	if id != "" {
		if op, found := s.d.Queue.Operation(id); found {
			pub := op.Public()
			job = pub
			running = pub["running"]
			if running == nil {
				complete = false
			}
		} else if pointer["running"] == false {
			if _, isBool := pointer["success"].(bool); isBool {
				running = false
			} else {
				running, complete = nil, false
			}
		} else {
			running, complete = nil, false
		}
		if b, err := readTail(filepath.Join(s.opsDir(), id, "worker.log"), 4096); err == nil && len(b) > 0 {
			logTail = string(b)
		}
	} else if exists(s.d.RunPath("xray-web-operation.pid")) {
		running, complete = nil, false
	}
	mode := "reinstall"
	if ok {
		if m, _ := pointer["operation"].(string); m == "install" || m == "update" || m == "reinstall" {
			mode = m
		}
	}
	var opID any
	if ok {
		if v, has := pointer["backgroundOperationId"]; has && v != nil && v != false {
			opID = v
		}
	}
	if pub, isMap := job.(map[string]any); isMap && pub["running"] == false && ok && pointer["running"] == true {
		r := map[string]any{}
		for k, v := range pointer {
			r[k] = v
		}
		r["running"], r["operationRunning"], r["success"] = false, false, false
		if pub["state"] == "aborted" {
			r["error"] = "Операция Xray отменена."
		} else {
			r["error"] = "Операция Xray прервана. Откройте диагностику."
		}
		result = r
	}
	if !complete {
		return nil, cgi.E(503, "XRAY_OWNER_UNCONFIRMED", "Владелец операции Xray не подтверждён. Откройте диагностику операций.")
	}
	return map[string]any{"operation": mode, "operationId": opID, "operationRunning": running, "complete": complete, "pid": nil,
		"backgroundOperation": job, "result": result, "logTail": logTail}, nil
}

func readTail(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := st.Size() - n
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// recordDeviceTrial writes run/xray-lifecycle-test.json after an install on
// this router that started the new binary and confirmed its SOCKS inbound
// (DEV5_XRAY_DIAG_PHYSICAL_TRIAL_UNRECORDED: 3.x only read the file, nothing
// on the router wrote it).
func (s *service) recordDeviceTrial(version, sum, mode string) {
	now := time.Now()
	_ = os.MkdirAll(s.d.Root.Run(), 0o700)
	_ = compat.WriteJSON(s.d.RunPath("xray-lifecycle-test.json"), map[string]any{
		"schemaVersion": 1, "success": true, "version": version, "sha256": sum, "mode": mode,
		"checks": []string{"version", "start", "socks"}, "checkedAt": compat.StampUTC(now),
		"details": version + ": версия, запуск и SOCKS подтверждены на этом Keenetic " + compat.Stamp(now) + "."})
}

package ops

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

// update-check / update-start / reinstall / restore on top of the 3.5.0
// launcher (internal/update) and installer (internal/installer).
//
//   - update-check verifies the signed manifest of the configured channel
//     in-process and caches the summary in state/update-check.json; info.cgi
//     derives updateAvailable / reinstallSupported from a fresh (≤ 1 h) cache.
//   - update-start / reinstall start `broray launcher apply` detached (own
//     session: it outlives this daemon, which the installer stops). Its JSON
//     progress lines go to state/update/<id>.jsonl and are mirrored into the
//     page operation (update-status.cgi). The daemon that comes up after the
//     switch — new version, or old one after a rollback — resumes reading
//     where the previous one stopped and records the launcher's result.
//   - restore runs `broray ctl recover --restart` when the update journal
//     shows an interrupted operation; otherwise the 3.x 409 stays.

const (
	updateEngine    = "broray-launcher/1"
	checkFreshness  = time.Hour
	watchPollPeriod = 300 * time.Millisecond
)

// spawnGrace: the launcher may not hold its lock yet right after the start.
const spawnGrace = 20 * time.Second

// updaterHooks are replaced in tests.
type updaterHooks struct {
	fetcher *update.Fetcher
	// spawn starts `<this binary> args...` detached, stdout → out, stderr → errOut.
	spawn      func(args []string, out, errOut string) error
	spawnGrace time.Duration
}

func defaultUpdaterHooks(a *area) updaterHooks {
	return updaterHooks{fetcher: update.NewFetcher(), spawn: a.spawnDetached, spawnGrace: spawnGrace}
}

func (a *area) checkCacheFile() string { return a.d.Path("state", "update-check.json") }

// checkCache returns the last check and whether it is fresh: ≤ 1 h old,
// made by this version for the channel configured now.
func (a *area) checkCache() (update.CheckResult, bool, bool) {
	var r update.CheckResult
	if compat.ReadJSON(a.checkCacheFile(), &r) != nil || r.CheckedAt.IsZero() {
		return r, false, false
	}
	if r.Current != app.Version {
		// Made by the version before an update: says nothing about this one.
		return update.CheckResult{}, false, false
	}
	cfg, _ := platform.LoadConfig(a.d.Root)
	age := a.now().Sub(r.CheckedAt)
	fresh := age >= 0 && age <= checkFreshness && r.Channel == cfg.Update.Channel
	return r, true, fresh
}

func relation3x(r string) string {
	if r == "none" || r == "" {
		return "uncomparable"
	}
	return r
}

// updateCheck is update-check.cgi (3.x success shape, 3.x HTTP mapping of
// the codes). Refusals keep the Russian reason; nothing is cached for them
// except for a newer release this router cannot take (arch, minimum source).
func (a *area) updateCheck(c *cgi.Req) {
	cfg, err := platform.LoadConfig(a.d.Root)
	if err != nil {
		writeC(c.W, 503, cError(update.ErrChannelInvalid, "Настройки канала обновлений (config.v1.json) не читаются: "+err.Error()+"."))
		return
	}
	src, err := update.SourceFrom(cfg, "")
	if err != nil {
		writeC(c.W, update.HTTPStatus(update.Code(err)), cError(update.Code(err), update.Message(err)))
		return
	}
	ctx, cancel := context.WithTimeout(c.R.Context(), 60*time.Second)
	defer cancel()
	chk, err := a.updater.fetcher.Check(ctx, src, a.now())
	if err != nil {
		a.d.Log.Printf("update-check %s: %v", src.URL, err)
		writeC(c.W, update.HTTPStatus(update.Code(err)), cError(update.Code(err), update.Message(err)))
		return
	}
	r := update.Summarize(chk, app.Version, update.Platform(), a.now())
	_ = os.MkdirAll(a.d.Path("state"), 0o700)
	_ = compat.WriteJSON(a.checkCacheFile(), r)
	if r.ProblemCode != "" {
		writeC(c.W, update.HTTPStatus(r.ProblemCode), cError(r.ProblemCode, r.ProblemMessage))
		return
	}
	var avail any
	if r.Latest != "" {
		avail = r.Latest
	}
	writeC(c.W, 200, map[string]any{"ok": true, "updateAvailable": r.UpdateAvailable,
		"currentCandidate": app.Version, "currentRelease": app.Version,
		"availableVersion": avail, "availablePackageVersion": avail, "candidateId": avail,
		"releaseRelation": relation3x(r.Relation), "candidateRelation": relation3x(r.Relation),
		"checkedAt": compat.StampUTC(r.CheckedAt), "engine": updateEngine,
		"channel": r.Channel, "reinstallAvailable": r.ReinstallAvailable, "currentRevoked": r.CurrentRevoked, "notes": r.Notes})
}

// updateStart is update-start.cgi / reinstall.cgi: conflicts first (409
// OPERATION_BUSY), then the target from a fresh check, then the layout.
func (a *area) updateStart(c *cgi.Req, op string) {
	busy := cError("OPERATION_BUSY", "Другая операция BROray уже принята или выполняется.")
	if a.page.running() || a.d.Lock.Busy() || platform.Held(a.d.Root.LauncherLock()) || platform.Held(a.d.Root.InstallerLock()) {
		writeC(c.W, 409, busy)
		return
	}
	r, _, fresh := a.checkCache()
	args := []string{"launcher", "apply", "--channel", r.Channel}
	switch {
	case op == "update" && !fresh:
		writeC(c.W, 409, cError("UPDATE_CHECK_REQUIRED", "Сначала выполните свежую проверку обновления."))
		return
	case op == "update" && !r.UpdateAvailable:
		writeC(c.W, 409, cError(update.ErrNotAvailable, "Новая версия не обнаружена."))
		return
	case op == "update":
		args = append(args, "--version", r.Latest)
	case !fresh || !r.ReinstallAvailable:
		writeC(c.W, 503, cError("REINSTALL_SOURCE_UNAVAILABLE", "Источник установленной версии не подтверждён."))
		return
	default:
		args = append(args, "--reinstall")
	}
	if registered, _ := a.layout(); !registered {
		writeC(c.W, 503, cError("UPDATE_LAYOUT_INVALID", "BROray установлен не установщиком 3.5.0: обновление через WebUI недоступно."))
		return
	}
	release, ok := a.d.Lock.TryAcquire("system", op, "")
	if !ok {
		writeC(c.W, 409, busy)
		return
	}
	id := a.pageOperationID(op)
	target := r.Latest
	if op == "reinstall" {
		target = app.Version
	}
	if !a.startDetached(id, op, append(args, "--operation-id", id), a.d.Root.LauncherLock(),
		"Запрос принят: загрузчик проверит канал «"+r.Channel+"» и установит версию "+target+".", release) {
		writeC(c.W, 503, cError("UPDATER_START_FAILED", "Не удалось запустить загрузчик обновления."))
		return
	}
	writeC(c.W, 202, map[string]any{"ok": true, "accepted": true, "operationId": id, "operation": op, "state": "queued", "engine": updateEngine})
}

// restore is restore.cgi: the 3.x 409 unless the update journal shows an
// interrupted operation, which `ctl recover --restart` then completes or
// rolls back (this daemon is stopped and started again if needed).
func (a *area) restore(c *cgi.Req) {
	j, _ := installer.LoadJournal(a.d.Root)
	if j == nil || j.Terminal() || platform.Held(a.d.Root.InstallerLock()) {
		cgi.WriteErr(c.W, cgi.E(409, "RESTORE_UNAVAILABLE", "Восстановление резервной копии недоступно для компактной схемы."))
		return
	}
	if a.page.running() || a.d.Lock.Busy() {
		writeC(c.W, 409, cError("OPERATION_BUSY", "Другая операция BROray уже принята или выполняется."))
		return
	}
	release, ok := a.d.Lock.TryAcquire("system", "restore", "")
	if !ok {
		writeC(c.W, 409, cError("OPERATION_BUSY", "Другая операция BROray уже принята или выполняется."))
		return
	}
	id := a.pageOperationID("restore")
	if !a.startDetached(id, "restore", []string{"ctl", "recover", "--restart", "--progress", "json"}, a.d.Root.InstallerLock(),
		"Завершается прерванная операция "+j.OperationID+" ("+j.From+" → "+j.To+", этап «"+j.State+"»).", release) {
		writeC(c.W, 503, cError("UPDATER_START_FAILED", "Не удалось запустить восстановление."))
		return
	}
	writeC(c.W, 202, map[string]any{"ok": true, "accepted": true, "operationId": id, "operation": "restore", "state": "queued", "engine": updateEngine})
}

// startDetached publishes the page operation, starts the process and the
// watcher. release (the global fence) is returned when the watcher ends.
func (a *area) startDetached(id, op string, args []string, watchLock, msg string, release compat.Release) bool {
	dir := a.d.Path("state", "update")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		release()
		return false
	}
	progress := filepath.Join(dir, id+".jsonl")
	if !a.page.begin(&pageOp{OperationID: id, Operation: op, State: "queued", Stage: "queued", Progress: 0, Message: msg,
		Running: true, ProgressFile: progress, WatchLock: watchLock}) {
		release()
		return false
	}
	a.page.update(func(*pageOp) {}, msg)
	pruneUpdateFiles(dir, id)
	if err := a.updater.spawn(args, progress, filepath.Join(dir, id+".log")); err != nil {
		release()
		m := "Не удалось запустить " + args[0] + ": " + err.Error() + "."
		a.page.update(func(p *pageOp) {
			p.State, p.Stage, p.Progress, p.Running, p.Message, p.Error = "error", "queued", 100, false, m, m
		}, m)
		return false
	}
	go a.watch(a.d.Ctx, id, release, time.Now())
	return true
}

var updateLogName = regexp.MustCompile(`^(update|reinstall|restore)-([0-9]{14})-[0-9]+(?:-[0-9a-f]{16})?\.(jsonl|log)$`)

// pruneUpdateFiles keeps the files of the last few owned operations.
func pruneUpdateFiles(dir, keep string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type oldFile struct {
		name  string
		stamp string
	}
	var old []oldFile
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue
		}
		match := updateLogName.FindStringSubmatch(e.Name())
		if match != nil && e.Name()[:len(e.Name())-len(filepath.Ext(e.Name()))] != keep {
			old = append(old, oldFile{name: e.Name(), stamp: match[2]})
		}
	}
	sort.Slice(old, func(i, j int) bool {
		if old[i].stamp == old[j].stamp {
			return old[i].name < old[j].name
		}
		return old[i].stamp < old[j].stamp
	})
	for len(old) > 10 {
		_ = os.Remove(filepath.Join(dir, old[0].name))
		old = old[1:]
	}
}

// resumeWatch continues an update that stopped the previous daemon.
func (a *area) resumeWatch(ctx context.Context) {
	p := a.page.snapshot()
	if p == nil || !p.Running || p.ProgressFile == "" {
		return
	}
	a.page.update(func(*pageOp) {}, "BROray "+app.Version+" запущен; ожидается итог операции.")
	go a.watch(ctx, p.OperationID, func() {}, time.Now())
}

// watch mirrors the progress file into the page operation until the
// launcher's result event, or until the launcher is gone without one.
func (a *area) watch(ctx context.Context, id string, release compat.Release, started time.Time) {
	releaseOnce := sync.OnceFunc(release)
	defer releaseOnce()
	for {
		op := a.page.snapshot()
		if op == nil || op.OperationID != id || !op.Running {
			return
		}
		// Lock first, then read: the launcher writes its result before it
		// releases the lock, so "free lock + no result" is a real failure.
		alive, lockErr := platform.HeldChecked(op.WatchLock)
		if a.drainProgress(op, releaseOnce) {
			return
		}
		if lockErr == nil && !alive && time.Since(started) > a.updater.spawnGrace {
			m := "Операция " + id + " не подтверждена: загрузчик завершился без итогового результата. Проверьте состояние установки."
			releaseOnce()
			a.page.update(func(p *pageOp) {
				p.State, p.Progress, p.Running, p.Message, p.Error = "error", 100, false, m, m
			}, m)
			return
		}
		select {
		case <-ctx.Done():
			return // this daemon is stopping (the installer stops it); the next one resumes
		case <-time.After(watchPollPeriod):
		}
	}
}

// drainProgress applies new progress lines; true once the result arrived.
func (a *area) drainProgress(op *pageOp, release compat.Release) bool {
	f, err := os.Open(op.ProgressFile)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.Seek(op.ProgressOffset, io.SeekStart); err != nil {
		return false
	}
	rd := bufio.NewReader(f)
	off := op.ProgressOffset
	prev, prevMsg := op.Stage, op.Message
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return false // partial line: read again next time
		}
		off += int64(len(line))
		var ev update.Event
		if json.Unmarshal(line, &ev) != nil || ev.Event == "" {
			a.page.update(func(p *pageOp) { p.ProgressOffset = off }, "")
			continue
		}
		done := ev.Event == "result" && (ev.Source == "launcher" || ev.Source == "recover")
		if done {
			release()
		}
		a.page.update(func(p *pageOp) {
			p.ProgressOffset = off
			applyEvent(p, ev, done)
		}, eventLogLine(prev, prevMsg, ev, done))
		if done {
			return true
		}
		if ev.Event == "stage" {
			prev, prevMsg = ev.Stage, ev.Message
		}
	}
}

// eventLogLine: one log line per stage change, log event and result (the
// download progress repeats its stage and only updates the message).
func eventLogLine(prevStage, prevMsg string, ev update.Event, done bool) string {
	switch {
	case done && ev.Message == prevMsg:
		return "" // the installer's last stage already said it
	case done:
		return ev.Message
	case ev.Event == "log":
		return ev.Message
	case ev.Event == "stage" && ev.Stage != prevStage:
		return ev.Message
	}
	return ""
}

func applyEvent(p *pageOp, ev update.Event, done bool) {
	switch {
	case done:
		ok := ev.OK != nil && *ev.OK
		p.Running, p.Progress = false, 100
		p.Message = ev.Message
		if ok {
			p.State, p.Stage, p.Error = "success", "complete", ""
			if p.Message == "" {
				p.Message = "Операция завершена."
			}
		} else {
			p.State = "error"
			if ev.State != "" {
				p.Stage = ev.State
			}
			if p.Message == "" {
				p.Message = "Операция завершилась ошибкой."
			}
			p.Error = p.Message
		}
	case ev.Event == "stage":
		p.State, p.Stage = "running", ev.Stage
		if ev.Progress > 0 {
			p.Progress = ev.Progress
		}
		if ev.Message != "" {
			p.Message = ev.Message
		}
	}
}

// spawnDetached starts this binary in its own session so it survives the
// daemon it is about to stop (like the uninstall handoff).
func (a *area) spawnDetached(args []string, out, errOut string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// A matching path may already be a link or a file from another attempt.
	// Exclusive creation refuses it before the child writes any output.
	so, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer so.Close()
	se, err := os.OpenFile(errOut, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer se.Close()
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "BRORAY_ROOT="+a.d.Root.Dir)
	cmd.Dir = "/"
	cmd.Stdout, cmd.Stderr = so, se
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// recoveryPending: an interrupted update journal that no installer is
// working on (S24broray start normally clears it at boot).
func (a *area) recoveryPending() (*installer.Journal, bool) {
	j, err := installer.LoadJournal(a.d.Root)
	if err != nil || j == nil || j.Terminal() || platform.Held(a.d.Root.InstallerLock()) {
		return nil, false
	}
	return j, true
}

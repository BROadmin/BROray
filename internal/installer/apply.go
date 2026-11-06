package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

// `installer apply` is the target-version half of the update (design §3,
// §5-§7): this binary — just downloaded and verified by the launcher of
// the installed version — copies itself into versions/<v>, stops the
// daemon through its control socket (never by PID), switches current
// atomically, starts the new daemon and confirms readiness, or rolls back.
// No step reboots the router. Every state change is journaled first.

// ApplyOptions are the ABI v1 arguments plus host settings.
type ApplyOptions struct {
	Root         platform.Root
	ABI          int
	From         string // version the launcher runs (informational; current is read from disk)
	Channel      string
	OperationID  string
	Operation    string // update | reinstall (informational; the mode follows from the versions)
	Version      string // version of this binary = the target
	Self         string // path of this binary
	Progress     *update.Emitter
	Log          io.Writer
	StopTimeout  time.Duration
	ReadyTimeout time.Duration

	restart bool // Recover: the daemon may be running (see RecoverWithRestart)
}

func (o *ApplyOptions) defaults() {
	if o.Progress == nil {
		o.Progress = update.NewEmitter(io.Discard, "installer")
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.StopTimeout <= 0 {
		// The daemon can spend up to 85 seconds on UDP route cleanup and
		// shutdown. Keep a margin so an accepted shutdown can finish.
		o.StopTimeout = 100 * time.Second
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = 90 * time.Second
	}
}

// ApplyResult is the final state of Apply (also emitted as the result event).
type ApplyResult struct {
	OK                bool
	State             string // committed | rolled-back | failed | rollback-failed
	Code              string
	Message           string
	RollbackPerformed bool
	Detail            string // full error chain (stderr diagnostics)
}

func (r ApplyResult) event(version, from string) update.Event {
	return update.Event{Event: "result", OK: update.Bool(r.OK), State: r.State, Code: r.Code, Message: r.Message,
		Version: version, From: from, RollbackPerformed: r.RollbackPerformed}
}

func failed(state string, err error) ApplyResult {
	code := update.Code(err)
	if code == "" {
		code = "UPDATE_INSTALL_FAILED"
	}
	return ApplyResult{State: state, Code: code, Message: update.Message(err), Detail: err.Error()}
}

// Apply performs the update. It never returns before the outcome is
// journaled; the result event is the last line of the progress stream.
func Apply(o ApplyOptions) (res ApplyResult) {
	o.defaults()
	e := o.Progress
	from := ""
	defer func() {
		if res.Detail != "" {
			fmt.Fprintf(o.Log, "installer apply %s: %s: %s\n", o.Version, res.State, res.Detail)
		}
		e.Emit(res.event(o.Version, from))
	}()
	if o.ABI != update.ABI {
		return failed(JFailed, update.Errorf("UPDATE_ABI_UNSUPPORTED", nil, "Установщик %s не поддерживает контракт загрузчика ABI %d.", o.Version, o.ABI))
	}
	if !update.ValidVersion(o.Version) {
		return failed(JFailed, update.Errorf("UPDATE_VERSION_INVALID", nil, "Некорректная версия установщика %q.", o.Version))
	}
	lock, err := platform.TryLock(o.Root.InstallerLock())
	if err != nil {
		return failed(JFailed, update.Errorf(update.ErrBusy, err, "Другая операция установщика BROray уже выполняется."))
	}
	defer lock.Release()
	pj, err := LoadJournal(o.Root)
	if err != nil {
		return failed(JFailed, update.Errorf("UPDATE_JOURNAL_INVALID", err, "Журнал обновления %s повреждён; операция не начата.", o.Root.UpdateJournal()))
	}
	if pj != nil && !pj.Terminal() {
		e.Log("Найдена незавершённая операция " + pj.OperationID + " (" + pj.State + "); сначала она завершается или откатывается.")
		if _, err := recoverLocked(o, pj, true); err != nil {
			return failed(JFailed, update.Errorf("UPDATE_RECOVERY_FAILED", err, "Не удалось завершить предыдущую операцию обновления; новая операция не начата."))
		}
	}
	if _, err := os.Lstat(o.Root.Current() + ".new"); err == nil {
		return failed(JFailed, update.Errorf("UPDATE_LAYOUT_INVALID", nil,
			"Временный объект current.new уже существует; обновление не начато, объект сохранён для проверки."))
	} else if !errors.Is(err, os.ErrNotExist) {
		return failed(JFailed, update.Errorf("UPDATE_LAYOUT_INVALID", err,
			"Не удалось проверить временный объект current.new; обновление не начато."))
	}
	prev, err := os.Readlink(o.Root.Current())
	if err != nil {
		return failed(JFailed, update.Errorf("UPDATE_LAYOUT_INVALID", err, "BROray установлен не установщиком 3.5.0 (нет ссылки %s); обновление невозможно.", o.Root.Current()))
	}
	from = filepath.Base(prev)
	if !update.ValidVersion(from) || prev != filepath.Join("versions", from) {
		return failed(JFailed, update.Errorf("UPDATE_LAYOUT_INVALID", nil,
			"Ссылка current указывает вне каталога установленной версии; обновление не начато."))
	}
	if o.From != "" && o.From != from {
		e.Log(fmt.Sprintf("Загрузчик сообщил версию %s, ссылка current указывает на %s; используется %s.", o.From, prev, from))
	}
	to := o.Version
	j := &Journal{OperationID: o.OperationID, Operation: o.Operation, Mode: "switch", From: from, To: to, Channel: o.Channel,
		PreviousTarget: prev, NewTarget: filepath.Join("versions", to), InitBackup: filepath.Join(o.Root.UpdateState(), "S24broray.prev")}
	if j.Operation == "" {
		j.Operation = "update"
	}
	if to == from {
		j.Mode = "replace"
	}
	oldBin := filepath.Join(o.Root.Dir, prev, "broray")
	oldInfo, err := os.Lstat(oldBin)
	if err != nil || !oldInfo.Mode().IsRegular() || oldInfo.Mode().Perm()&0o111 == 0 {
		return failed(JFailed, update.Errorf("UPDATE_LAYOUT_INVALID", err,
			"Текущий бинарник BROray не подтверждён; обновление не начато."))
	}
	j.PreviousSHA256, err = fileSHA256(oldBin)
	if err != nil {
		return failed(JFailed, update.Errorf("UPDATE_LAYOUT_INVALID", err,
			"Не удалось проверить текущий бинарник BROray; обновление не начато."))
	}
	j.OwnedVersions = ownedVersionsForOperation(pj, from, j.PreviousSHA256, j.Mode == "switch")
	verDir := filepath.Join(o.Root.Versions(), to)

	// --- staging -------------------------------------------------------
	e.Stage("staging", 45, "Версия "+to+" раскладывается в отдельный каталог "+verDir+".")
	st, err := os.Stat(o.Self)
	if err != nil {
		return failed(JFailed, err)
	}
	if err := update.CheckSpace(map[string]int64{o.Root.Versions(): st.Size()}); err != nil {
		return failed(JFailed, err)
	}
	if j.Mode == "switch" {
		if err := os.Mkdir(verDir, 0o755); err != nil {
			return failed(JFailed, update.Errorf("UPDATE_TARGET_EXISTS", err,
				"Каталог версии %s уже существует; обновление не начато, содержимое сохранено.", to))
		}
		j.CreatedDir = true
	} else {
		if err := validateReplaceTarget(verDir); err != nil {
			return failed(JFailed, update.Errorf("UPDATE_REPLACE_TARGET_UNOWNED", err,
				"Каталог текущей версии содержит посторонние или незавершённые файлы; переустановка не начата."))
		}
	}
	staged := filepath.Join(verDir, "broray.new")
	if err := copyFile(o.Self, staged, 0o755); err != nil {
		return failed(JFailed, errors.Join(
			update.Errorf("UPDATE_STAGING_FAILED", err, "Не удалось скопировать новую версию в %s.", verDir),
			cleanupStaged(o.Root, j)))
	}
	j.StagedSHA256, err = fileSHA256(staged)
	if err != nil {
		return failed(JFailed, err)
	}
	if err := checkBinary(staged, to); err != nil {
		return failed(JFailed, errors.Join(
			update.Errorf("UPDATE_STAGING_FAILED", err, "Скопированная версия %s не запускается.", to),
			cleanupStaged(o.Root, j)))
	}
	if j.Mode == "switch" {
		if err := os.Rename(staged, filepath.Join(verDir, "broray")); err != nil {
			return failed(JFailed, errors.Join(err, cleanupStaged(o.Root, j)))
		}
	}
	_ = platform.SyncDir(verDir)
	if err := j.set(o.Root, JPrepared); err != nil {
		return failed(JFailed, errors.Join(err, cleanupStaged(o.Root, j)))
	}

	// --- stopping ------------------------------------------------------
	if err := j.set(o.Root, JStopping); err != nil {
		return abortBeforeSwitch(o, j, nil, err)
	}
	e.Stage("stopping", 60, "Служба BROray останавливается командой через управляющий сокет (без остановки по PID).")
	stopStarted := time.Now()
	dl, was, err := stopDaemon(o.Root, o.StopTimeout)
	j.DaemonWasRunning = was
	if err != nil {
		return abortBeforeSwitch(o, j, nil, err)
	}
	if was {
		e.Log(fmt.Sprintf("Служба BROray остановилась за %d с; блокировка демона получена.", int(time.Since(stopStarted).Round(time.Second)/time.Second)))
	}

	// --- switching -----------------------------------------------------
	if err := j.set(o.Root, JSwitching); err != nil {
		return abortBeforeSwitch(o, j, dl, err)
	}
	if j.Mode == "replace" {
		e.Stage("switching", 70, "Бинарник версии "+to+" заменяется проверенной копией из канала (прежний сохранён до подтверждения).")
	} else {
		e.Stage("switching", 70, "Ссылка current атомарно переключается на "+j.NewTarget+".")
	}
	if err := switchForward(o.Root, j); err != nil {
		return rollback(o, j, dl, update.Errorf("UPDATE_SWITCH_FAILED", err, "Не удалось переключить версию."))
	}
	_ = dl.Release()

	// --- started -------------------------------------------------------
	if err := j.set(o.Root, JStarted); err != nil {
		return rollback(o, j, nil, err)
	}
	e.Stage("starting", 82, "Запускается BROray "+to+".")
	if err := startDaemon(o.Root); err != nil {
		return rollback(o, j, nil, update.Errorf("UPDATE_START_FAILED", err, "BROray %s не запустился.", to))
	}
	e.Stage("health", 92, "Проверяется готовность BROray "+to+" через управляющий сокет.")
	if err := waitReady(o.Root, to, o.ReadyTimeout); err != nil {
		return rollback(o, j, nil, update.Errorf("UPDATE_HEALTH_FAILED", err, "BROray %s не подтвердил готовность за %d с.", to, int(o.ReadyTimeout/time.Second)))
	}

	// --- committed -----------------------------------------------------
	if err := commit(o.Root, j); err != nil {
		// A failed journal write leaves JStarted recoverable. A failure after
		// the durable commit is a cleanup error, not a rollback request.
		if durable, loadErr := LoadJournal(o.Root); loadErr == nil && durable != nil && durable.State == JCommitted {
			return failed(JCommitted, update.Errorf("UPDATE_CLEANUP_FAILED", err,
				"Новая версия работает, но очистка сохранённой копии требует проверки."))
		}
		return failed(JStarted, err)
	}
	if j.Mode == "switch" {
		// Only after the new daemon is healthy and committed may an older,
		// unambiguous BROray version be removed.
		prune(o.Root, e, pj, from, to)
	}
	msg := "BROray обновлён до версии " + to + "."
	if j.Mode == "replace" {
		msg = "Версия " + to + " переустановлена из канала и проверена."
	}
	e.Stage("complete", 100, msg)
	return ApplyResult{OK: true, State: JCommitted, Message: msg}
}

// prune removes only obsolete versions proven by the previous journal. An
// unknown directory is retained even if it resembles a BROray installation.
func prune(root platform.Root, e *update.Emitter, prior *Journal, from, to string) bool {
	if !priorJournalMatchesCurrent(prior, from) {
		e.Log("Неподтверждённые старые каталоги версий сохранены.")
		return false
	}
	return pruneOwnedVersions(root, e, ownedVersionsFromPrior(prior, from), from, to)
}

// A committed switch can be replayed after a crash between the durable
// journal write and pruning. Keep its previous version for rollback.
func pruneCommitted(root platform.Root, e *update.Emitter, j *Journal) bool {
	if j == nil || j.State != JCommitted || j.Mode != "switch" ||
		!priorJournalMatchesCurrent(j, j.To) {
		return false
	}
	return pruneOwnedVersions(root, e, ownedVersionsFromPrior(j, j.To), j.From, j.To)
}

func pruneOwnedVersions(root platform.Root, e *update.Emitter, records []OwnedVersion, previous, currentVersion string) bool {
	current, err := os.Readlink(root.Current())
	if err != nil || current != filepath.Join("versions", currentVersion) {
		e.Log("Очистка старой версии пропущена: текущая ссылка не подтверждена.")
		return false
	}
	entries, err := os.ReadDir(root.Versions())
	if err != nil {
		e.Log("Не удалось проверить каталог версий: " + err.Error())
		return false
	}
	removed := false
	for _, record := range records {
		if !validOwnedVersion(record) || record.Version == previous || record.Version == currentVersion {
			continue
		}
		var candidate os.DirEntry
		for _, ent := range entries {
			if ent.Name() == record.Version {
				candidate = ent
				break
			}
		}
		if candidate == nil {
			continue
		}
		if !ownedVersionDir(root, candidate) {
			e.Log("Неподтверждённый каталог версии " + record.Version + " сохранён.")
			continue
		}
		dir := filepath.Join(root.Versions(), record.Version)
		bin := filepath.Join(dir, "broray")
		sum, err := fileSHA256(bin)
		if err != nil || sum != record.SHA256 {
			e.Log("Каталог версии " + record.Version + " сохранён: контрольная сумма не совпала с журналом.")
			continue
		}
		if err := os.Remove(bin); err != nil {
			e.Log("Не удалось удалить подтверждённый бинарник старой версии " + record.Version + ": " + err.Error())
			continue
		}
		if err := os.Remove(dir); err != nil {
			e.Log("Бинарник старой версии удалён, но каталог " + record.Version + " сохранён: " + err.Error())
			continue
		}
		removed = true
		if err := platform.SyncDir(root.Versions()); err != nil {
			e.Log("Удалена старая версия " + record.Version + ", но сохранность очистки не подтверждена: " + err.Error())
			continue
		}
		e.Log("Удалена подтверждённая старая версия " + record.Version + ".")
	}
	return removed
}

func validOwnedVersion(record OwnedVersion) bool {
	if !update.ValidVersion(record.Version) || len(record.SHA256) != 64 {
		return false
	}
	b, err := hex.DecodeString(record.SHA256)
	return err == nil && len(b) == sha256.Size
}

// A prior journal can carry deletion proof only when its target layout and
// terminal outcome agree with the version that is current now.
func priorJournalMatchesCurrent(prior *Journal, current string) bool {
	if prior == nil || !update.ValidVersion(prior.From) || !update.ValidVersion(prior.To) ||
		prior.PreviousTarget != filepath.Join("versions", prior.From) ||
		prior.NewTarget != filepath.Join("versions", prior.To) {
		return false
	}
	if prior.Mode != "switch" && (prior.Mode != "replace" || prior.From != prior.To) {
		return false
	}
	switch prior.State {
	case JCommitted:
		return prior.To == current
	case JFailed, JRolledBack:
		return prior.From == current
	default:
		return false
	}
}

func ownedVersionsFromPrior(prior *Journal, current string) []OwnedVersion {
	if !priorJournalMatchesCurrent(prior, current) {
		return nil
	}
	var records []OwnedVersion
	seen := make(map[string]bool)
	for _, record := range prior.OwnedVersions {
		if !validOwnedVersion(record) || seen[record.Version] {
			continue
		}
		seen[record.Version] = true
		records = append(records, record)
	}
	// Journals written before ownedVersions existed still prove the version
	// that was current before their successful switch.
	if prior.State == JCommitted && prior.Mode == "switch" {
		old := OwnedVersion{Version: prior.From, SHA256: prior.PreviousSHA256}
		if validOwnedVersion(old) && !seen[old.Version] {
			records = append(records, old)
		}
	}
	return records
}

func ownedVersionsForOperation(prior *Journal, current, currentSHA string, switching bool) []OwnedVersion {
	records := ownedVersionsFromPrior(prior, current)
	if !switching {
		return records
	}
	verified := OwnedVersion{Version: current, SHA256: currentSHA}
	if !validOwnedVersion(verified) {
		return records
	}
	for i := range records {
		if records[i].Version == current {
			records[i] = verified
			return records
		}
	}
	return append(records, verified)
}

func ownedVersionDir(root platform.Root, ent os.DirEntry) bool {
	if !ent.IsDir() || !update.ValidVersion(ent.Name()) {
		return false
	}
	dir := filepath.Join(root.Versions(), ent.Name())
	children, err := os.ReadDir(dir)
	if err != nil || len(children) != 1 || children[0].Name() != "broray" {
		return false
	}
	info, err := os.Lstat(filepath.Join(dir, "broray"))
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// checkBinary runs `<bin> version` and expects "BROray <version>".
func checkBinary(bin, version string) error {
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	if got := strings.TrimSpace(string(out)); got != "BROray "+version {
		return fmt.Errorf("reports %q", got)
	}
	return nil
}

func cleanupStaged(root platform.Root, j *Journal) error {
	verDir := filepath.Join(root.Versions(), j.To)
	if j.Mode == "switch" && j.CreatedDir && filepath.Join("versions", j.To) != filepath.Clean(j.PreviousTarget) {
		info, err := os.Lstat(verDir)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("staged version path is not a real directory: %s", verDir)
		}
		entries, err := os.ReadDir(verDir)
		if err != nil {
			return err
		}
		if len(entries) > 1 {
			return fmt.Errorf("staged version contains unexpected entries: %s", verDir)
		}
		if len(entries) == 1 {
			name := entries[0].Name()
			if name != "broray.new" && name != "broray" {
				return fmt.Errorf("staged version contains unowned entry: %s", name)
			}
			staged := filepath.Join(verDir, name)
			bin, err := os.Lstat(staged)
			if err != nil {
				return err
			}
			if !bin.Mode().IsRegular() || bin.Mode().Perm()&0o111 == 0 || j.StagedSHA256 == "" {
				return fmt.Errorf("staged binary ownership is unverified: %s", staged)
			}
			sha, err := fileSHA256(staged)
			if err != nil {
				return err
			}
			if sha != j.StagedSHA256 {
				return fmt.Errorf("staged binary checksum differs from journal: %s", staged)
			}
			if err := os.Remove(staged); err != nil {
				return err
			}
		}
		return os.Remove(verDir)
	} else if j.Mode == "replace" && j.StagedSHA256 != "" {
		staged := filepath.Join(verDir, "broray.new")
		if info, err := os.Lstat(staged); err == nil && info.Mode().IsRegular() {
			if got, err := fileSHA256(staged); err == nil && got == j.StagedSHA256 {
				return os.Remove(staged)
			}
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func validateReplaceTarget(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("version target is not a directory: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].Name() != "broray" {
		return fmt.Errorf("version target has unexpected entries: %s", dir)
	}
	bin, err := os.Lstat(filepath.Join(dir, "broray"))
	if err != nil {
		return err
	}
	if !bin.Mode().IsRegular() || bin.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("version binary is not a regular executable: %s", dir)
	}
	return nil
}

// stopDaemon stops a running daemon through the socket and returns the
// daemon lock held by this process. Not running → the lock, was=false.
func stopDaemon(root platform.Root, timeout time.Duration) (*platform.Lock, bool, error) {
	if l, err := platform.TryLock(root.DaemonLock()); err == nil {
		return l, false, nil
	} else if !errors.Is(err, platform.ErrLocked) {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := control.Shutdown(ctx, root); err != nil {
		// The reply can be lost when the daemon exits right after it; the
		// lock tells whether it really stopped.
		if l, lerr := control.WaitLock(root.DaemonLock(), 5*time.Second); lerr == nil {
			return l, true, nil
		}
		return nil, true, update.Errorf("UPDATE_DAEMON_STOP_FAILED", err, "BROray не принял команду остановки через управляющий сокет; ничего не изменено.")
	}
	l, err := control.WaitLock(root.DaemonLock(), timeout)
	if err != nil {
		return nil, true, update.Errorf("UPDATE_DAEMON_STOP_TIMEOUT", err, "Служба BROray не завершилась за %d с после команды остановки; обновление отменено, ничего не изменено. Остановка по PID не выполняется.", int(timeout/time.Second))
	}
	return l, true, nil
}

func startDaemon(root platform.Root) error {
	var cmd *exec.Cmd
	if _, err := os.Stat(InitScript()); err == nil {
		cmd = exec.Command(InitScript(), "start")
	} else {
		cmd = exec.Command(filepath.Join(root.Current(), "broray"), "start")
	}
	cmd.Env = append(os.Environ(), "BRORAY_ROOT="+root.Dir)
	cmd.Dir = "/"
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// waitReady waits until the daemon answers ping with version.
func waitReady(root platform.Root, version string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error = errors.New("no answer")
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		v, err := control.Ping(ctx, root)
		cancel()
		if err == nil && v == version {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("daemon reports version %s", v)
		}
		last = err
		time.Sleep(500 * time.Millisecond)
	}
	return last
}

// switchForward writes the new init script (old one backed up) and makes
// the new binary current.
func preflightSwitchForward(root platform.Root, j *Journal) error {
	if !update.ValidVersion(j.From) || !update.ValidVersion(j.To) ||
		j.PreviousTarget != filepath.Join("versions", j.From) ||
		j.NewTarget != filepath.Join("versions", j.To) {
		return fmt.Errorf("switch journal version targets are invalid")
	}
	current, err := os.Readlink(root.Current())
	if err != nil || current != j.PreviousTarget {
		return fmt.Errorf("current changed before version switch: %q: %v", current, err)
	}
	old := filepath.Join(root.Versions(), j.From, "broray")
	if present, err := checkJournalBinary(old, j.PreviousSHA256); err != nil || !present {
		return fmt.Errorf("previous binary changed before version switch: %s: %v", old, err)
	}
	if j.InitBackup != "" {
		if filepath.Clean(j.InitBackup) != filepath.Join(root.UpdateState(), "S24broray.prev") {
			return fmt.Errorf("init backup path is not owned: %s", j.InitBackup)
		}
		if _, err := os.Lstat(j.InitBackup); err == nil {
			return fmt.Errorf("init backup already exists: %s", j.InitBackup)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	verDir := filepath.Join(root.Versions(), j.To)
	if j.Mode == "switch" {
		if err := validateReplaceTarget(verDir); err != nil {
			return err
		}
		if present, err := checkJournalBinary(filepath.Join(verDir, "broray"), j.StagedSHA256); err != nil || !present {
			return fmt.Errorf("staged binary changed before version switch: %s: %v", verDir, err)
		}
	} else if j.Mode == "replace" {
		info, err := os.Lstat(verDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("replace version directory is not owned: %s: %v", verDir, err)
		}
		entries, err := os.ReadDir(verDir)
		if err != nil {
			return err
		}
		if len(entries) != 2 || entries[0].Name() != "broray" || entries[1].Name() != "broray.new" {
			return fmt.Errorf("replace version directory changed before switch: %s", verDir)
		}
		if present, err := checkJournalBinary(filepath.Join(verDir, "broray.new"), j.StagedSHA256); err != nil || !present {
			return fmt.Errorf("staged replacement changed before version switch: %s: %v", verDir, err)
		}
	} else {
		return fmt.Errorf("unknown switch mode %q", j.Mode)
	}
	return nil
}

func switchForward(root platform.Root, j *Journal) error {
	if err := preflightSwitchForward(root, j); err != nil {
		return err
	}
	if err := checkOwnedUninstallInit(root); err != nil {
		return fmt.Errorf("init script ownership changed before switch: %w", err)
	}
	if b, err := os.ReadFile(InitScript()); err == nil {
		if err := os.MkdirAll(filepath.Dir(j.InitBackup), 0o700); err != nil {
			return err
		}
		if err := platform.WriteFileAtomic(j.InitBackup, b, 0o755); err != nil {
			return err
		}
		if err := platform.WriteFileAtomic(InitScript(), []byte(renderInitScript(root)), 0o755); err != nil {
			return err
		}
	}
	if j.Mode == "replace" {
		dir := filepath.Join(root.Versions(), j.To)
		if err := os.Rename(filepath.Join(dir, "broray"), filepath.Join(dir, "broray.old")); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(dir, "broray.new"), filepath.Join(dir, "broray")); err != nil {
			return err
		}
		return platform.SyncDir(dir)
	}
	return pointCurrent(root, j.NewTarget, j)
}

// pointCurrent atomically replaces the current symlink.
func pointCurrent(root platform.Root, target string, j *Journal) error {
	tmp := root.Current() + ".new"
	if link, err := os.Readlink(tmp); err == nil {
		if link != j.NewTarget && link != j.PreviousTarget {
			return fmt.Errorf("current.new points to unexpected target %q", link)
		}
		if err := os.Remove(tmp); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("current.new is not an expected temporary symlink: %w", err)
	}
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, root.Current()); err != nil {
		return err
	}
	return platform.SyncDir(root.Dir)
}

func checkJournalBinary(path, wantSHA string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || wantSHA == "" {
		return false, fmt.Errorf("binary ownership is unverified: %s", path)
	}
	got, err := fileSHA256(path)
	if err != nil {
		return false, err
	}
	if got != wantSHA {
		return false, fmt.Errorf("binary checksum differs from journal: %s", path)
	}
	return true, nil
}

func checkJournalInitBackup(root platform.Root, j *Journal) (bool, error) {
	if j.InitBackup == "" {
		return false, nil
	}
	if filepath.Clean(j.InitBackup) != filepath.Join(root.UpdateState(), "S24broray.prev") {
		return false, fmt.Errorf("init backup path is not owned: %s", j.InitBackup)
	}
	info, err := os.Lstat(j.InitBackup)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return false, fmt.Errorf("init backup is not a regular executable: %s", j.InitBackup)
	}
	b, err := os.ReadFile(j.InitBackup)
	if err != nil {
		return false, err
	}
	if !isOwnedInitTemplate(root, b) {
		return false, fmt.Errorf("init backup contents are not an owned template: %s", j.InitBackup)
	}
	return true, nil
}

// switchBack is idempotent: it restores the state before switchForward,
// whatever part of it had happened.
func switchBack(root platform.Root, j *Journal) error {
	dir := filepath.Join(root.Versions(), j.To)
	backupPresent, err := checkJournalInitBackup(root, j)
	if err != nil {
		return err
	}
	if j.Mode == "replace" {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("replace version directory is not owned: %s: %v", dir, err)
		}
		old := filepath.Join(dir, "broray.old")
		current := filepath.Join(dir, "broray")
		staged := filepath.Join(dir, "broray.new")
		oldPresent, err := checkJournalBinary(old, j.PreviousSHA256)
		if err != nil {
			return err
		}
		stagedPresent, err := checkJournalBinary(staged, j.StagedSHA256)
		if err != nil {
			return err
		}
		if oldPresent {
			if _, err := os.Lstat(current); err == nil {
				if _, err := checkJournalBinary(current, j.StagedSHA256); err != nil {
					return err
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := os.Rename(old, current); err != nil {
				return err
			}
		} else {
			present, err := checkJournalBinary(current, j.PreviousSHA256)
			if err != nil {
				return err
			}
			if !present {
				return fmt.Errorf("previous binary is missing: %s", current)
			}
		}
		if stagedPresent {
			if err := os.Remove(staged); err != nil {
				return err
			}
		}
		if err := platform.SyncDir(dir); err != nil {
			return err
		}
	} else {
		current, err := os.Readlink(root.Current())
		if err != nil || (current != j.NewTarget && current != j.PreviousTarget) {
			return fmt.Errorf("current is not an expected version symlink: %q: %v", current, err)
		}
		if err := pointCurrent(root, j.PreviousTarget, j); err != nil {
			return err
		}
		if j.CreatedDir && filepath.Join("versions", j.To) != filepath.Clean(j.PreviousTarget) {
			if err := cleanupStaged(root, j); err != nil {
				return err
			}
		}
	}
	if backupPresent {
		b, err := os.ReadFile(j.InitBackup)
		if err != nil {
			return err
		}
		if err := platform.WriteFileAtomic(InitScript(), b, 0o755); err != nil {
			return err
		}
		if err := os.Remove(j.InitBackup); err != nil {
			return err
		}
	}
	return nil
}

func commit(root platform.Root, j *Journal) error {
	if err := validateCommitJournal(j); err != nil {
		return err
	}
	// Verify every cleanup target before recording the irreversible state.
	// Cleanup is repeated after that write so a changed target is never removed.
	if _, err := checkJournalInitBackup(root, j); err != nil {
		return err
	}
	if j.Mode == "replace" {
		old := filepath.Join(root.Versions(), j.To, "broray.old")
		if _, err := checkJournalBinary(old, j.PreviousSHA256); err != nil {
			return err
		}
	} else if j.Mode != "switch" {
		return fmt.Errorf("unknown commit mode %q", j.Mode)
	}
	if err := j.set(root, JCommitted); err != nil {
		return err
	}
	_, err := cleanupCommitted(root, j)
	return err
}

func validateCommitJournal(j *Journal) error {
	if j == nil || !update.ValidVersion(j.From) || !update.ValidVersion(j.To) {
		return fmt.Errorf("committed journal versions are invalid")
	}
	if j.Mode != "switch" && j.Mode != "replace" {
		return fmt.Errorf("unknown commit mode %q", j.Mode)
	}
	if j.Mode == "replace" && j.From != j.To {
		return fmt.Errorf("replacement journal versions differ")
	}
	return nil
}

// cleanupCommitted only removes still-owned rollback copies. A crash or
// cleanup error after the durable commit can be retried by Recover.
func cleanupCommitted(root platform.Root, j *Journal) (bool, error) {
	if err := validateCommitJournal(j); err != nil {
		return false, err
	}
	backupPresent, err := checkJournalInitBackup(root, j)
	if err != nil {
		return false, err
	}
	old := filepath.Join(root.Versions(), j.To, "broray.old")
	oldPresent := false
	if j.Mode == "replace" {
		oldPresent, err = checkJournalBinary(old, j.PreviousSHA256)
		if err != nil {
			return false, err
		}
	} else if j.Mode != "switch" {
		return false, fmt.Errorf("unknown commit mode %q", j.Mode)
	}
	if oldPresent {
		if err := os.Remove(old); err != nil {
			return false, err
		}
	}
	if backupPresent {
		if err := os.Remove(j.InitBackup); err != nil {
			return oldPresent, err
		}
	}
	return oldPresent || backupPresent, nil
}

// abortBeforeSwitch: nothing was switched; remove the staged copy and,
// if this run stopped the daemon, start it again.
func abortBeforeSwitch(o ApplyOptions, j *Journal, dl *platform.Lock, cause error) ApplyResult {
	o.defaults()
	cleanupErr := cleanupStaged(o.Root, j)
	if cleanupErr != nil {
		cause = errors.Join(cause, fmt.Errorf("staged cleanup refused: %w", cleanupErr))
	}
	if dl != nil {
		_ = dl.Release()
		if j.DaemonWasRunning {
			if err := startDaemon(o.Root); err != nil {
				return abortRestartFailed(o, j, cause, err)
			}
			if err := waitReady(o.Root, j.From, o.ReadyTimeout); err != nil {
				return abortRestartFailed(o, j, cause, err)
			}
		}
	}
	if cleanupErr != nil {
		j.Error = cause.Error()
		if err := j.set(o.Root, j.State); err != nil {
			cause = errors.Join(cause, fmt.Errorf("save recoverable journal: %w", err))
		}
		return failed("abort-failed", update.Errorf("UPDATE_CLEANUP_FAILED", cause,
			"Версия не переключалась, но очистка временной версии требует проверки; операция сохранена для восстановления."))
	}
	j.Error = cause.Error()
	_ = j.set(o.Root, JFailed)
	return failed(JFailed, cause)
}

func abortRestartFailed(o ApplyOptions, j *Journal, cause, restartErr error) ApplyResult {
	durable, err := LoadJournal(o.Root)
	if err == nil && durable != nil && !durable.Terminal() {
		durable.Error = cause.Error() + "; previous daemon restart: " + restartErr.Error()
		err = durable.set(o.Root, durable.State)
	}
	if err != nil {
		restartErr = errors.Join(restartErr, fmt.Errorf("save recoverable abort journal: %w", err))
	}
	msg := "Обновление прервано до переключения версии; прежняя версия " + j.From + " осталась на диске, но служба не подтвердила запуск. После устранения причины выполните broray ctl recover --restart."
	o.Progress.Stage("abort-failed", 95, msg)
	return failed("abort-failed", update.Errorf("UPDATE_ABORT_RESTART_FAILED", restartErr, "%s", msg))
}

// rollback returns to the previous version after a failure past the
// switch. dl is the daemon lock when this process already holds it.
func rollback(o ApplyOptions, j *Journal, dl *platform.Lock, cause error) ApplyResult {
	e := o.Progress
	j.Error = cause.Error()
	_ = j.set(o.Root, JRollingBack)
	e.Stage("rollback", 90, update.Message(cause)+" Возвращается предыдущая версия "+j.From+".")
	if dl == nil {
		var err error
		if dl, _, err = stopDaemon(o.Root, o.StopTimeout); err != nil {
			r := failed("rollback-failed", update.Errorf("UPDATE_ROLLBACK_FAILED", err,
				"%s Откат не выполнен: новая версия не остановилась. Он будет завершён при следующем запуске BROray (broray ctl recover).", update.Message(cause)))
			r.RollbackPerformed = false
			return r
		}
	}
	if err := switchBack(o.Root, j); err != nil {
		_ = dl.Release()
		return failed("rollback-failed", update.Errorf("UPDATE_ROLLBACK_FAILED", err, "%s Откат не завершён; выполните broray ctl recover.", update.Message(cause)))
	}
	_ = dl.Release()
	if j.DaemonWasRunning {
		if err := startDaemon(o.Root); err != nil {
			return rollbackRestartFailed(o, j, cause, err)
		}
		if err := waitReady(o.Root, j.From, o.ReadyTimeout); err != nil {
			return rollbackRestartFailed(o, j, cause, err)
		}
	}
	_ = j.set(o.Root, JRolledBack)
	code := update.Code(cause)
	if code == "" {
		code = "UPDATE_INSTALL_FAILED"
	}
	msg := update.Message(cause) + " Восстановлена версия " + j.From + "; перезагрузка не требуется."
	e.Stage("rolled-back", 100, msg)
	return ApplyResult{State: JRolledBack, Code: code, Message: msg, RollbackPerformed: true, Detail: cause.Error()}
}

func rollbackRestartFailed(o ApplyOptions, j *Journal, cause, restartErr error) ApplyResult {
	j.Error = cause.Error() + "; previous daemon restart: " + restartErr.Error()
	if err := j.set(o.Root, JRollingBack); err != nil {
		restartErr = errors.Join(restartErr, fmt.Errorf("save recoverable rollback journal: %w", err))
	}
	msg := update.Message(cause) + " Предыдущая версия " + j.From + " возвращена на диск, но служба не подтвердила запуск. После устранения причины выполните broray ctl recover --restart."
	o.Progress.Stage("rollback-failed", 95, msg)
	return failed("rollback-failed", update.Errorf("UPDATE_ROLLBACK_FAILED", restartErr, "%s", msg))
}

// RecoverOutcome is what Recover did.
type RecoverOutcome string

const (
	RecoverNothing    RecoverOutcome = "nothing"     // no journal or already terminal
	RecoverBusy       RecoverOutcome = "busy"        // an installer holds the lock: left alone
	RecoverCleaned    RecoverOutcome = "failed"      // interrupted before the switch; staged copy removed
	RecoverCommitted  RecoverOutcome = "committed"   // the new version was running and is kept
	RecoverRolledBack RecoverOutcome = "rolled-back" // previous version restored
)

// Recover completes or rolls back an interrupted operation (`broray ctl
// recover`, run by S24broray before start). restart: the daemon may be
// running (WebUI) — stop it through the socket if a rollback needs it and
// start it again afterwards; without restart a running daemon blocks a
// rollback.
func Recover(o ApplyOptions) (RecoverOutcome, error) {
	o.defaults()
	lock, err := platform.TryLock(o.Root.InstallerLock())
	if errors.Is(err, platform.ErrLocked) {
		return RecoverBusy, nil
	}
	if err != nil {
		return "", err
	}
	defer lock.Release()
	j, err := LoadJournal(o.Root)
	if err != nil {
		return "", err
	}
	if j == nil {
		return RecoverNothing, nil
	}
	if j.State == JCommitted {
		cleaned, err := cleanupCommitted(o.Root, j)
		if err != nil {
			return "", err
		}
		if pruneCommitted(o.Root, o.Progress, j) {
			cleaned = true
		}
		if cleaned {
			return RecoverCommitted, nil
		}
		return RecoverNothing, nil
	}
	if j.Terminal() {
		return RecoverNothing, nil
	}
	return recoverLocked(o, j, o.restart)
}

// RecoverWithRestart is Recover for a caller whose daemon may be running.
func RecoverWithRestart(o ApplyOptions) (RecoverOutcome, error) {
	o.restart = true
	return Recover(o)
}

func recoverLocked(o ApplyOptions, j *Journal, restart bool) (RecoverOutcome, error) {
	o.defaults()
	e := o.Progress
	switch j.State {
	case JPrepared, JStopping:
		// Nothing was switched: the installed version is still current.
		if err := cleanupStaged(o.Root, j); err != nil {
			return "", fmt.Errorf("staged cleanup refused; recoverable journal preserved: %w", err)
		}
		if restart {
			if !platform.Held(o.Root.DaemonLock()) {
				if err := startDaemon(o.Root); err != nil {
					return "", preSwitchRecoverRestartFailed(o, j, err)
				}
			}
			if err := waitReady(o.Root, j.From, o.ReadyTimeout); err != nil {
				return "", preSwitchRecoverRestartFailed(o, j, err)
			}
		}
		j.Error = "interrupted at " + j.State + "; nothing was switched"
		e.Log("Операция " + j.OperationID + " прервана до переключения; версия " + j.From + " осталась текущей.")
		return RecoverCleaned, j.set(o.Root, JFailed)
	case JStarted:
		if platform.Held(o.Root.DaemonLock()) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			v, err := control.Ping(ctx, o.Root)
			cancel()
			if err == nil && v == j.To {
				e.Log("Новая версия " + j.To + " работает; операция " + j.OperationID + " подтверждается.")
				if err := commit(o.Root, j); err != nil {
					return "", err
				}
				pruneCommitted(o.Root, e, j)
				return RecoverCommitted, nil
			}
		}
	case JSwitching, JRollingBack:
	default:
		return "", fmt.Errorf("unknown journal state %q", j.State)
	}
	// Switched (or possibly switched) without a confirmed new daemon:
	// return to the previous version.
	var dl *platform.Lock
	if platform.Held(o.Root.DaemonLock()) {
		if !restart {
			return "", errors.New("daemon is running; a rollback needs it stopped (use `broray ctl recover --restart`)")
		}
		var err error
		if dl, _, err = stopDaemon(o.Root, o.StopTimeout); err != nil {
			return "", err
		}
	} else {
		var err error
		if dl, err = platform.TryLock(o.Root.DaemonLock()); err != nil {
			return "", err
		}
	}
	if j.State != JRollingBack {
		j.Error = "interrupted at " + j.State
		if err := j.set(o.Root, JRollingBack); err != nil {
			_ = dl.Release()
			return "", err
		}
	}
	err := switchBack(o.Root, j)
	_ = dl.Release()
	if err != nil {
		return "", err
	}
	if restart || j.DaemonWasRunning {
		if err := startDaemon(o.Root); err != nil {
			return "", recoverRestartFailed(o, j, err)
		}
		if err := waitReady(o.Root, j.From, o.ReadyTimeout); err != nil {
			return "", recoverRestartFailed(o, j, err)
		}
	}
	e.Log("Операция " + j.OperationID + " прервана после переключения; восстановлена версия " + j.From + ".")
	return RecoverRolledBack, j.set(o.Root, JRolledBack)
}

func preSwitchRecoverRestartFailed(o ApplyOptions, j *Journal, restartErr error) error {
	j.Error = "previous daemon restart before switch: " + restartErr.Error()
	if err := j.set(o.Root, j.State); err != nil {
		restartErr = errors.Join(restartErr, fmt.Errorf("save recoverable pre-switch journal: %w", err))
	}
	return update.Errorf("UPDATE_RECOVERY_FAILED", restartErr,
		"Версия %s осталась текущей, но служба не подтвердила запуск. После устранения причины выполните broray ctl recover --restart.", j.From)
}

func recoverRestartFailed(o ApplyOptions, j *Journal, restartErr error) error {
	j.Error = "previous daemon restart after interrupted update: " + restartErr.Error()
	if err := j.set(o.Root, JRollingBack); err != nil {
		restartErr = errors.Join(restartErr, fmt.Errorf("save recoverable rollback journal: %w", err))
	}
	return update.Errorf("UPDATE_RECOVERY_FAILED", restartErr,
		"Версия %s возвращена на диск, но служба не подтвердила запуск. После устранения причины выполните broray ctl recover --restart.", j.From)
}

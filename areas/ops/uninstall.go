package ops

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/modules/routes"
	"github.com/BROadmin/broray/modules/xray"
)

// Uninstall (uninstall.cgi → broray_system_uninstall_start + worker,
// lib/broray-page.sh:2581-3967) on top of the 3.5.0 installer.
//
// Order, as the installer requires and the 3.x worker did: everything that
// talks to KeeneticOS runs first, inside the daemon, with progress published
// to update-status; any failure stops the uninstall with nothing deleted
// locally. Only then the daemon hands off to `broray installer uninstall
// [--keep-data]` (a detached process), whose first step stops this daemon
// through its control socket and whose last steps preserve data (normal
// mode) and remove the files. The daemon is therefore stopped only at the
// very end; no reboot is involved. As in 3.x, the terminal success is not
// observable: the WebUI disappears and the page ends with its
// "не подтверждён" message.

const (
	phraseNormal = "УДАЛИТЬ BROray"
	phraseFull   = "УДАЛИТЬ BROray ПОЛНОСТЬЮ"
)

type uninstallStage struct {
	name     string
	progress int
	message  string
}

var uninstallStages = []uninstallStage{
	{"preflight", 5, "Проверяются условия удаления."},
	{"services", 10, "Останавливаются фоновые задачи BROray."},
	{"routes", 20, "Удаляются маршруты BROray из Keenetic."},
	{"dot", 30, "Удаляются управляемые записи DNS-over-TLS BROray."},
	{"keenetic", 40, "Удаляется управляемый ProxyN BROray."},
	{"servers", 50, "Отключается активный сервер."},
	{"xray", 60, "Останавливается Xray."},
	// Last Keenetic change before the handoff: a page opened through
	// https://broray.<KeenDNS> loses its connection here, so everything
	// else is already visible to the owner by then.
	{"publish", 75, "Удаляется адрес WebUI через KeenDNS. Если страница открыта по этому адресу, она перестанет обновляться — удаление продолжится без неё; итог будет в журнале /opt/var/log/broray-uninstall-*.log."},
	{"remove", 82, ""}, // message depends on the mode
}

// uninstallHooks are the side-effecting steps (replaced in tests).
type uninstallHooks struct {
	preflight func(ctx context.Context, full bool) error
	stage     func(ctx context.Context, stage string) error // receipt-based Keenetic cleanup of the installer
	server    func(ctx context.Context)
	stopXray  func(ctx context.Context) (wasRunning bool, err error)
	startXray func(ctx context.Context) error
	purge     func() error              // full mode: remove <Opt>/broray-preserved
	handoff   func(keepData bool) error // start `broray installer uninstall` (stops the daemon)
}

func defaultUninstallHooks(a *area) uninstallHooks {
	return uninstallHooks{
		preflight: a.uninstallPreflight,
		stage:     a.installerCleanupStage,
		server:    func(ctx context.Context) { xray.RemoveServerRoute(ctx, a.d.Root) },
		stopXray: func(ctx context.Context) (bool, error) {
			if a.d.App == nil || a.d.App.Xray == nil {
				return false, nil
			}
			was := a.d.App.Xray.Running()
			return was, a.d.App.Xray.Stop(ctx)
		},
		startXray: func(ctx context.Context) error {
			if a.d.App == nil || a.d.App.Xray == nil {
				return nil
			}
			return a.d.App.Xray.Start(ctx)
		},
		purge:   func() error { return os.RemoveAll(filepath.Join(installer.Opt, "broray-preserved")) },
		handoff: a.installerHandoff,
	}
}

func (a *area) uninstallHandler(c *cgi.Req) {
	if !localUninstallAccess(c.R.Host, c.R.RemoteAddr) {
		cgi.WriteErr(c.W, cgi.E(403, "REMOTE_UNINSTALL_FORBIDDEN", "Удаление доступно только при прямом локальном подключении к IPv4-адресу роутера."))
		return
	}
	body, ok := brorayBody(c)
	if !ok {
		return
	}
	m, ok := jsonObject(body)
	if !ok {
		cgi.WriteErr(c.W, cgi.E(400, "REQUEST_JSON_INVALID", "Тело запроса должно быть корректным JSON-объектом."))
		return
	}
	modeRaw, hasMode := m["mode"]
	confRaw, hasConf := m["confirmation"]
	mode, confirmation := jqRaw(modeRaw, hasMode), jqRaw(confRaw, hasConf)
	// From here every answer is HTTP 200 (uninstall.cgi streams the backend).
	var expected string
	switch mode {
	case "normal":
		expected = phraseNormal
	case "full":
		expected = phraseFull
	default:
		writeC(c.W, 200, cError("INVALID_MODE", "Неизвестный режим удаления."))
		return
	}
	if confirmation != expected {
		writeC(c.W, 200, cError("CONFIRMATION_REQUIRED", "Фраза подтверждения не совпала."))
		return
	}
	conflict := cError("OPERATION_CONFLICT", "Другая операция BROray выполняется или требует восстановления.")
	if a.page.running() {
		writeC(c.W, 200, conflict)
		return
	}
	release, ok := a.d.Lock.TryAcquire("system", "uninstall", "")
	if !ok {
		writeC(c.W, 200, conflict)
		return
	}
	id := a.pageOperationID("uninstall")
	first := uninstallStages[0]
	if !a.page.begin(&pageOp{OperationID: id, Operation: "uninstall", State: "running", Stage: first.name,
		Progress: first.progress, Message: first.message, Running: true}) {
		release()
		writeC(c.W, 200, conflict)
		return
	}
	a.page.update(func(*pageOp) {}, "Удаление принято: режим "+mode+".")
	go a.runUninstall(a.d.Ctx, mode == "full", release)
	writeC(c.W, 200, map[string]any{"ok": true, "accepted": true, "operationId": id, "operation": "uninstall", "engine": "canonical-current-operation"})
}

// localUninstallAccess requires both a private IPv4 destination and a local
// peer. KeenDNS requests arrive with a public Host, including when proxied by
// the router itself. The CLI uninstall path does not use this WebUI boundary.
func localUninstallAccess(host, remote string) bool {
	destination, err := netip.ParseAddr(host)
	if err != nil {
		pair, pairErr := netip.ParseAddrPort(host)
		if pairErr != nil {
			return false
		}
		destination = pair.Addr()
	}
	if !destination.Is4() || !destination.IsPrivate() {
		return false
	}
	peerHost, _, err := net.SplitHostPort(remote)
	if err != nil {
		peerHost = remote
	}
	peer, err := netip.ParseAddr(peerHost)
	return err == nil && (peer.IsPrivate() || peer.IsLoopback())
}

func (a *area) runUninstall(ctx context.Context, full bool, release compat.Release) {
	h := a.uninstall
	wasPaused := a.d.Queue.Paused()
	xrayWasRunning, xrayStopped := false, false
	fail := func(stage uninstallStage, err error) {
		if xrayStopped && xrayWasRunning {
			if e := h.startXray(context.WithoutCancel(ctx)); e != nil {
				a.page.update(func(*pageOp) {}, "Xray не удалось запустить снова: "+e.Error())
			}
		}
		if !wasPaused {
			_ = a.d.Queue.SetPaused(false)
		}
		release()
		msg := "Удаление остановлено на этапе «" + stage.name + "»: " + humanError(err) +
			" Файлы и данные BROray не удалялись; устраните причину и повторите удаление."
		a.page.update(func(op *pageOp) {
			op.State, op.Stage, op.Progress, op.Running = "error", stage.name, 100, false
			op.Message, op.Error = msg, msg
		}, msg)
	}
	for _, st := range uninstallStages {
		st := st
		if st.name == "remove" {
			st.message = "Сохраняются пользовательские данные и удаляется BROray. WebUI станет недоступен."
			if full {
				st.message = "Удаляются BROray и все его данные. WebUI станет недоступен."
			}
		}
		a.page.update(func(op *pageOp) { op.Stage, op.Progress, op.Message = st.name, st.progress, st.message }, st.message)
		var err error
		switch st.name {
		case "preflight":
			err = h.preflight(ctx, full)
		case "services":
			if _, e := a.d.Queue.StopBackground(); e != nil {
				err = errors.New("UNINSTALL_SERVICES_UNCONFIRMED: не удалось остановить фоновые задачи.")
			}
		case "routes", "dot", "publish", "keenetic":
			for _, step := range compat.UninstallSteps(st.name) {
				if err = safeStep(compat.WithFence(ctx), step); err != nil {
					break
				}
			}
			if err == nil {
				err = h.stage(ctx, st.name)
			}
		case "servers":
			h.server(ctx)
		case "xray":
			xrayWasRunning, err = h.stopXray(ctx)
			xrayStopped = err == nil
		case "remove":
			if err = h.handoff(!full); err != nil {
				err = fmt.Errorf("UNINSTALL_HANDOFF_FAILED: не удалось запустить установщик: %v.", err)
			}
		}
		if err == nil && ctx.Err() != nil {
			err = errors.New("UNINSTALL_INTERRUPTED: BROray завершает работу.")
		}
		if err != nil {
			fail(st, err)
			return
		}
	}
	// The installer now stops the daemon (and with it this goroutine). The
	// global fence stays held so nothing else starts meanwhile.
	a.page.update(func(*pageOp) {}, "Установщик запущен; BROray останавливается.")
}

func safeStep(ctx context.Context, fn compat.UninstallStep) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("UNINSTALL_STEP_PANIC: %v", p)
		}
	}()
	return fn(ctx)
}

// humanError turns "CODE: text" into "text" for the page message.
func humanError(err error) string {
	m := err.Error()
	if i := strings.Index(m, ": "); i > 0 && strings.ToUpper(m[:i]) == m[:i] && !strings.Contains(m[:i], " ") {
		m = m[i+2:]
	}
	m = strings.TrimSpace(m)
	if m != "" && !strings.HasSuffix(m, ".") {
		m += "."
	}
	return m
}

// uninstallPreflight: root rights (the installer refuses otherwise), the
// installer binary, and for a normal uninstall a writable preservation dir.
func (a *area) uninstallPreflight(ctx context.Context, full bool) error {
	if os.Geteuid() != 0 {
		return errors.New("UNINSTALL_ROOT_REQUIRED: для удаления BROray должен работать от root.")
	}
	if _, err := os.Executable(); err != nil {
		return errors.New("UNINSTALL_INSTALLER_UNAVAILABLE: исполняемый файл BROray не найден.")
	}
	if !full {
		dir := filepath.Join(installer.Opt, "broray-preserved")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("UNINSTALL_PRESERVE_UNAVAILABLE: каталог %s недоступен.", dir)
		}
	}
	return nil
}

// installerCleanupStage removes the objects recorded in the installer's
// receipts (internal/installer cleanupKeenetic, split by stage): installed
// route bundles, managed DoT upstreams, the KeenDNS publication, the managed
// ProxyN. Stops at the first failure.
func (a *area) installerCleanupStage(ctx context.Context, stage string) error {
	if a.d.App == nil || a.d.App.K == nil {
		return nil
	}
	k := a.d.App.K
	switch stage {
	case "routes":
		rs := a.d.App.Routes
		if rs == nil {
			return nil
		}
		in := rs.LoadInstalled()
		if in.Interface == "" || len(in.Bundles) == 0 {
			return nil
		}
		var all []routes.Route
		seen := map[string]bool{}
		for _, list := range in.Bundles {
			for _, r := range list {
				if !seen[r.Key()] {
					seen[r.Key()] = true
					all = append(all, r)
				}
			}
		}
		if err := k.RouteBatch(ctx, in.Interface, nil, all, 100, nil); err != nil {
			return fmt.Errorf("UNINSTALL_ROUTES_FAILED: маршруты не удалены: %v", err)
		}
		in.Bundles = map[string][]routes.Route{}
		if err := rs.SaveInstalled(in); err != nil {
			return fmt.Errorf("UNINSTALL_ROUTES_FAILED: квитанция маршрутов не сохранена: %v", err)
		}
	case "dot":
		if rec := k.LoadDoT(); len(rec.Managed) > 0 {
			if _, err := k.ApplyDoT(ctx, nil); err != nil {
				return fmt.Errorf("UNINSTALL_DOT_FAILED: записи DNS-over-TLS не удалены: %v", err)
			}
		}
	case "publish":
		if _, ok := k.LoadWeb(); ok {
			if err := k.DisableWeb(ctx); err != nil {
				return fmt.Errorf("UNINSTALL_PUBLISH_FAILED: публикация KeenDNS не удалена: %v", err)
			}
		}
	case "keenetic":
		if _, ok := k.LoadInterface(); ok {
			if err := k.DeleteInterface(ctx); err != nil {
				return fmt.Errorf("UNINSTALL_INTERFACE_FAILED: управляемый ProxyN не удалён: %v", err)
			}
		}
	}
	return nil
}

// installerHandoff starts `<binary> installer uninstall [--keep-data]` in
// its own session so it survives the daemon it stops.
func (a *area) installerHandoff(keepData bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"installer", "uninstall"}
	if keepData {
		args = append(args, "--keep-data")
	}
	// The installer's own report must survive the removal of the BROray
	// directory and a router restart: keep it on /opt, outside BROray.
	logDir := filepath.Join(installer.Opt, "var", "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(logDir, "broray-uninstall-"+time.Now().UTC().Format("20060102-150405")+".log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	a.page.update(func(*pageOp) {}, "Журнал установщика: "+logPath)
	defer logf.Close()
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "BRORAY_ROOT="+a.d.Root.Dir, "BRORAY_UNINSTALL_LOG="+logPath)
	cmd.Dir = "/"
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap if it ends while the daemon still runs
	return nil
}

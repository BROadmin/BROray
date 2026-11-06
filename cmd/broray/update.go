package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

// runLauncher is the launcher of the installed version (design §3):
//
//	broray launcher check [--channel C]
//	broray launcher apply [--channel C] [--version V] [--reinstall] [--operation-id ID]
//
// Progress: JSON lines on stdout; diagnostics on stderr.
func runLauncher(root platform.Root, args []string) int {
	if len(args) == 0 || (args[0] != "apply" && args[0] != "check") {
		fmt.Fprintln(os.Stderr, "usage: broray launcher check|apply [--channel C] [--version V] [--reinstall] [--operation-id ID]")
		return 2
	}
	fs := flag.NewFlagSet("launcher "+args[0], flag.ContinueOnError)
	channel := fs.String("channel", "", "release channel (default: update.channel of config.v1.json)")
	version := fs.String("version", "", "target version (default: newest)")
	reinstall := fs.Bool("reinstall", false, "reinstall the installed version from the channel")
	opID := fs.String("operation-id", "", "operation id (WebUI)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	cfg, err := platform.LoadConfig(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "broray launcher: config: "+err.Error())
		return 1
	}
	src, err := update.SourceFrom(cfg, *channel)
	if err != nil {
		printJSON(map[string]any{"ok": false, "code": update.Code(err), "message": update.Message(err)})
		return 1
	}
	if args[0] == "check" {
		f := update.NewFetcher()
		chk, err := f.Check(context.Background(), src, time.Now())
		if err != nil {
			printJSON(map[string]any{"ok": false, "code": update.Code(err), "message": update.Message(err), "detail": err.Error()})
			return 1
		}
		printJSON(map[string]any{"ok": true, "result": update.Summarize(chk, app.Version, update.Platform(), time.Now())})
		return 0
	}
	if *opID == "" {
		*opID = fmt.Sprintf("update-%s-%d", time.Now().UTC().Format("20060102150405"), os.Getpid())
	}
	err = update.Apply(context.Background(), update.ApplyOptions{
		Root: root, Source: src, Current: app.Version, Version: *version, Reinstall: *reinstall,
		OperationID: *opID, Progress: update.NewEmitter(os.Stdout, "launcher"), Stderr: os.Stderr,
	})
	return boolExit(err == nil)
}

// runInstallerApply is `installer apply` (ABI v1) of this binary.
func runInstallerApply(args []string) int {
	fs := flag.NewFlagSet("installer apply", flag.ContinueOnError)
	abi := fs.Int("abi", 0, "launcher ABI")
	from := fs.String("from", "", "installed version or none")
	state := fs.String("state", "", "installation root (/opt/broray)")
	channel := fs.String("channel", "", "channel")
	progress := fs.String("progress", "", "json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *progress != "json" || *state == "" {
		fmt.Fprintln(os.Stderr, "usage: broray installer apply --abi 1 --from <version|none> --state <root> --channel <c> --progress json")
		return 2
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "installer apply: "+err.Error())
		return 1
	}
	o := installer.ApplyOptions{
		Root: platform.Root{Dir: *state}, ABI: *abi, From: *from, Channel: *channel, Version: app.Version, Self: self,
		OperationID: os.Getenv("BRORAY_UPDATE_OPERATION_ID"), Operation: os.Getenv("BRORAY_UPDATE_OPERATION"),
		Progress: update.NewEmitter(os.Stdout, "installer"), Log: os.Stderr,
		StopTimeout: envSeconds("BRORAY_UPDATE_STOP_TIMEOUT"), ReadyTimeout: envSeconds("BRORAY_UPDATE_READY_TIMEOUT"),
	}
	if *from == "none" {
		// A clean install through the launcher is not part of this version:
		// the terminal path is `broray installer install`.
		update.NewEmitter(os.Stdout, "installer").Emit(update.Event{Event: "result", OK: update.Bool(false), State: "failed",
			Code: "UPDATE_LAYOUT_INVALID", Message: "Чистая установка выполняется командой broray installer install."})
		return 1
	}
	return boolExit(installer.Apply(o).OK)
}

// runRecover is `broray ctl recover [--restart] [--progress json]` (local,
// no socket). With --progress json (WebUI restore) it prints progress
// events and a final result event instead of the summary object.
func runRecover(root platform.Root, args []string) int {
	jsonProgress := contains(args, "json") && contains(args, "--progress")
	var progress io.Writer = io.Discard
	if jsonProgress {
		progress = os.Stdout
	}
	em := update.NewEmitter(progress, "recover")
	o := installer.ApplyOptions{Root: root, Log: os.Stderr, Progress: em,
		StopTimeout: envSeconds("BRORAY_UPDATE_STOP_TIMEOUT"), ReadyTimeout: envSeconds("BRORAY_UPDATE_READY_TIMEOUT")}
	var out installer.RecoverOutcome
	var err error
	em.Stage("recover", 50, "Проверяется журнал операции обновления.")
	if contains(args, "--restart") {
		out, err = installer.RecoverWithRestart(o)
	} else {
		out, err = installer.Recover(o)
	}
	if jsonProgress {
		msg := map[installer.RecoverOutcome]string{
			installer.RecoverNothing:    "Незавершённых операций обновления нет.",
			installer.RecoverBusy:       "Установщик уже выполняет операцию; восстановление не требуется.",
			installer.RecoverCleaned:    "Прерванная до переключения операция отменена; текущая версия не менялась.",
			installer.RecoverCommitted:  "Новая версия работает; операция обновления подтверждена.",
			installer.RecoverRolledBack: "Прерванная операция откатана; восстановлена предыдущая версия.",
		}[out]
		ev := update.Event{Event: "result", OK: update.Bool(err == nil), State: string(out), Message: msg}
		if err != nil {
			ev.Code, ev.State, ev.Message = "UPDATE_RECOVERY_FAILED", "failed", "Восстановление не выполнено: "+err.Error()
		}
		em.Emit(ev)
		return boolExit(err == nil)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "broray ctl recover: "+err.Error())
		printJSON(map[string]any{"ok": false, "error": err.Error()})
		return 1
	}
	if out != installer.RecoverNothing {
		printJSON(map[string]any{"ok": true, "outcome": out})
	}
	return 0
}

func envSeconds(name string) time.Duration {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

// broray is the single multi-call binary of BROray 3.5.0:
//
//	broray daemon                 run the service (WebUI :8080, Xray, loops)
//	broray ctl <cmd> ...          talk to the running daemon over its socket
//	broray installer <cmd>        install | uninstall [--keep-data] | status | apply (ABI v1, run by the launcher)
//	broray launcher check|apply   release channel: verify the signed manifest, download, run installer apply
//	broray diag udp ...           read-only UDP path probe (STUN)
//	broray version
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	root := platform.DefaultRoot()
	switch os.Args[1] {
	case "daemon":
		if daemonOverride != nil {
			os.Exit(daemonOverride(root))
		}
		os.Exit(runDaemon(root))
	case "start":
		os.Exit(runStart(root))
	case "ctl":
		os.Exit(runCtl(root, os.Args[2:]))
	case "installer":
		os.Exit(runInstaller(root, os.Args[2:]))
	case "diag":
		os.Exit(runDiag(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Println("BROray " + app.Version)
	case "launcher":
		os.Exit(runLauncher(root, os.Args[2:]))
	case "worker":
		fmt.Fprintln(os.Stderr, os.Args[1]+": not part of dev1 (operations run inside the daemon)")
		os.Exit(2)
	default:
		usage()
		os.Exit(2)
	}
}

// daemonOverride is set only by the host-test build (-tags brorayfake).
var daemonOverride func(platform.Root) int

func usage() {
	fmt.Fprintln(os.Stderr, "usage: broray daemon | ctl <cmd> | installer install|uninstall|status|apply | launcher check|apply | version")
}

func printJSON(v any) { printJSONTo(os.Stdout, v) }

func printJSONTo(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// uninstallLogEnv is set by the WebUI uninstall (areas/ops), whose own log
// file already receives this process's stdout and stderr.
const uninstallLogEnv = "BRORAY_UNINSTALL_LOG"

// openUninstallLog creates <opt>/var/log/broray-uninstall-<UTC>.log.
func openUninstallLog(opt string, now time.Time) (*os.File, string, error) {
	dir := filepath.Join(opt, "var", "log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, "broray-uninstall-"+now.UTC().Format("20060102-150405")+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	return f, path, err
}

func runInstaller(root platform.Root, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: broray installer install [--no-start] | uninstall [--keep-data] | status | preflight")
		return 2
	}
	switch args[0] {
	case "preflight":
		rep, err := installer.Preflight(root, app.Version)
		printJSON(map[string]any{"ok": err == nil, "error": errString(err), "report": rep})
		return boolExit(err == nil)
	case "install":
		start := !contains(args, "--no-start")
		rep, err := installer.Install(root, app.Version, start)
		printJSON(map[string]any{"ok": err == nil, "error": errString(err), "report": rep, "version": app.Version})
		return boolExit(err == nil)
	case "uninstall":
		// DEV5_CLI_UNINSTALL_NO_DURABLE_LOG: the terminal uninstall keeps the
		// same durable log as the WebUI one (/opt/var/log, outside BROray, so
		// it survives the removal and a router restart). When the WebUI
		// started us, its log already captures stdout/stderr.
		out := io.Writer(os.Stdout)
		installer.Progress = os.Stderr
		if os.Getenv(uninstallLogEnv) == "" {
			if f, path, err := openUninstallLog(installer.Opt, time.Now()); err == nil {
				defer f.Close()
				fmt.Fprintln(os.Stderr, "uninstall log: "+path)
				installer.Progress = io.MultiWriter(os.Stderr, f)
				out = io.MultiWriter(os.Stdout, f)
			} else {
				fmt.Fprintln(os.Stderr, "WARNING: uninstall log not created: "+err.Error())
			}
		}
		rep, err := installer.Uninstall(context.Background(), root, contains(args, "--keep-data"))
		printJSONTo(out, map[string]any{"ok": err == nil, "error": errString(err), "report": rep})
		return boolExit(err == nil)
	case "status":
		printJSON(installer.Status(root))
		return 0
	case "apply":
		return runInstallerApply(args[1:])
	}
	fmt.Fprintln(os.Stderr, "unknown installer command: "+args[0])
	return 2
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func boolExit(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

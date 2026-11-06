package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenUninstallLog(t *testing.T) {
	opt := t.TempDir()
	f, path, err := openUninstallLog(opt, time.Date(2026, 11, 2, 21, 5, 6, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if path != filepath.Join(opt, "var", "log", "broray-uninstall-20261102-210506.log") {
		t.Fatal(path)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", st, err)
	}
}

// DEV5_CLI_UNINSTALL_NO_DURABLE_LOG (CODEX-REPORT-EMU E3): `broray installer
// uninstall` from a terminal must leave /opt/var/log/broray-uninstall-*.log
// with the UTC steps and the final JSON.
func TestCLIUninstallWritesDurableLog(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	bin := os.Getenv("BRORAY_E2E_MAIN_BIN")
	if bin == "" {
		bin = filepath.Join(t.TempDir(), "broray")
		if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
	}
	root, opt := t.TempDir(), t.TempDir()
	for _, d := range []string{"etc/init.d", "bin", "var"} {
		_ = os.MkdirAll(filepath.Join(opt, d), 0o755)
	}
	env := append(os.Environ(), "BRORAY_ROOT="+root, "BRORAY_OPT="+opt)
	run := func(args ...string) string {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, _ := cmd.CombinedOutput()
		return string(out)
	}
	if out := run("installer", "install", "--no-start"); !strings.Contains(out, `"ok": true`) {
		t.Fatalf("install: %s", out)
	}
	out := run("installer", "uninstall", "--keep-data")
	logs, _ := filepath.Glob(filepath.Join(opt, "var", "log", "broray-uninstall-*.log"))
	if len(logs) != 1 {
		t.Fatalf("durable log missing (%v); output:\n%s", logs, out)
	}
	b, _ := os.ReadFile(logs[0])
	// The host has no Keenetic (ndmc), so the uninstall stops at the first
	// Keenetic stage; the log must still hold the final JSON with the reason.
	if !strings.Contains(string(b), `"ok"`) || !strings.Contains(string(b), `"error"`) {
		t.Fatalf("log content:\n%s", b)
	}
}

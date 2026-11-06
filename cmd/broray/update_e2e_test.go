package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/control"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
	"golang.org/x/sys/unix"
)

// End-to-end update tests: real binaries of four versions (built with
// -tags brorayfake, so `broray daemon` is a small stand-in that owns the
// lock and the control socket), the real installer layout, S24broray,
// `broray start`, the launcher, `installer apply` and `ctl recover`, a
// file:// test channel signed with an ephemeral key.

const (
	v1 = "3.5.0-t1"
	v2 = "3.5.0-t2"
	v3 = "3.5.0-t3"
)

var (
	fakeOnce sync.Once
	fakeDir  string
	fakeErr  error
)

func fakeBinaries(t *testing.T) map[string]string {
	t.Helper()
	if dir := os.Getenv("BRORAY_E2E_BIN_DIR"); dir != "" {
		bins := make(map[string]string)
		for _, v := range []string{v1, v2, v3} {
			p := filepath.Join(dir, v, "broray")
			if st, err := os.Stat(p); err != nil || st.Mode()&0o111 == 0 {
				t.Fatalf("prebuilt fake binary %s unavailable: %v", p, err)
			}
			bins[v] = p
		}
		return bins
	}
	if testing.Short() {
		t.Skip("builds binaries")
	}
	if os.Geteuid() != 0 {
		t.Skip("installer install needs root")
	}
	fakeOnce.Do(func() {
		fakeDir, fakeErr = os.MkdirTemp("", "broray-fake-")
		if fakeErr != nil {
			return
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, v := range []string{v1, v2, v3} {
			wg.Add(1)
			go func(v string) {
				defer wg.Done()
				out, err := exec.Command("go", "build", "-tags", "brorayfake", "-ldflags",
					"-X github.com/BROadmin/broray/internal/app.Version="+v, "-o", filepath.Join(fakeDir, v, "broray"), ".").CombinedOutput()
				if err != nil {
					mu.Lock()
					fakeErr = fmt.Errorf("build %s: %v\n%s", v, err, out)
					mu.Unlock()
				}
			}(v)
		}
		wg.Wait()
	})
	if fakeErr != nil {
		t.Fatal(fakeErr)
	}
	return map[string]string{v1: filepath.Join(fakeDir, v1, "broray"), v2: filepath.Join(fakeDir, v2, "broray"), v3: filepath.Join(fakeDir, v3, "broray")}
}

type e2e struct {
	t    *testing.T
	bins map[string]string
	root platform.Root
	opt  string
	ch   string // channel directory
	key  update.SecretKey
	env  []string
}

func newE2E(t *testing.T, installed string, env ...string) *e2e {
	t.Helper()
	bins := fakeBinaries(t)
	base := t.TempDir()
	e := &e2e{t: t, bins: bins, root: platform.Root{Dir: filepath.Join(base, "broray")}, opt: filepath.Join(base, "opt"), ch: filepath.Join(base, "channel")}
	for _, d := range []string{"etc/init.d", "bin", "var/log"} {
		if err := os.MkdirAll(filepath.Join(e.opt, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.MkdirAll(e.ch, 0o755)
	var err error
	if e.key, err = update.GenerateKey(); err != nil {
		t.Fatal(err)
	}
	e.env = append(os.Environ(), "BRORAY_ROOT="+e.root.Dir, "BRORAY_OPT="+e.opt, "BRORAY_FAKE_TRUSTED_KEY="+e.key.Public().Encode(),
		"BRORAY_UPDATE_STOP_TIMEOUT=4", "BRORAY_UPDATE_READY_TIMEOUT=4")
	e.env = append(e.env, env...)
	if out, code := e.run(bins[installed], "installer", "install", "--no-start"); code != 0 {
		t.Fatalf("install: %s", out)
	}
	cfg := platform.DefaultConfig()
	cfg.Update.Channel, cfg.Update.ChannelURL = "test", "file://"+filepath.Join(e.ch, "manifest.json")
	if err := platform.SaveConfig(e.root, cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.powerOff)
	e.initd("start")
	e.wantRunning(installed)
	return e
}

func (e *e2e) run(bin string, args ...string) (string, int) {
	cmd := exec.Command(bin, args...)
	cmd.Env = e.env
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		e.t.Fatalf("%s %v: %v", bin, args, err)
	}
	return string(out), code
}

func (e *e2e) initd(action string) string {
	e.t.Helper()
	out, code := e.run(filepath.Join(e.opt, "etc/init.d/S24broray"), action)
	if code != 0 {
		e.t.Fatalf("S24broray %s: %s", action, out)
	}
	return out
}

// powerOff simulates power loss only for this test's own fake daemon.
// A pidfd keeps the process identity stable while its executable and
// BRORAY_ROOT are checked; a stale or foreign PID is left untouched.
func (e *e2e) powerOff() {
	pidFile := filepath.Join(e.root.Run(), "fake-daemon.pid")
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return
	}
	killed, err := killOwnedFakeDaemon(e.root, pid)
	if err != nil {
		e.t.Errorf("cannot safely stop fake daemon: %v", err)
		return
	}
	if !killed {
		return
	}
	l, err := control.WaitLock(e.root.DaemonLock(), 5*time.Second)
	if err != nil {
		e.t.Errorf("fake daemon lock remained held after scoped stop: %v", err)
		return
	}
	_ = l.Release()
	_ = os.Remove(pidFile)
}

func killOwnedFakeDaemon(root platform.Root, pid int) (bool, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	proc := filepath.Join("/proc", strconv.Itoa(pid))
	exe, err := os.Readlink(filepath.Join(proc, "exe"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root.Versions(), strings.TrimSuffix(exe, " (deleted)"))
	if err != nil || filepath.Base(rel) != "broray" || filepath.Dir(filepath.Dir(rel)) != "." || !update.ValidVersion(filepath.Base(filepath.Dir(rel))) {
		return false, nil
	}
	environ, err := os.ReadFile(filepath.Join(proc, "environ"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	owned := append([]byte("BRORAY_ROOT="+root.Dir), 0)
	if !bytes.Contains(append([]byte{0}, environ...), append([]byte{0}, owned...)) {
		return false, nil
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func TestPowerOffKeepsForeignPID(t *testing.T) {
	e := &e2e{t: t, root: platform.Root{Dir: filepath.Join(t.TempDir(), "broray")}}
	if err := os.MkdirAll(e.root.Run(), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := exec.Command("/bin/sleep", "30")
	if err := foreign.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- foreign.Wait() }()
	defer func() {
		_ = foreign.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()
	if err := os.WriteFile(filepath.Join(e.root.Run(), "fake-daemon.pid"), []byte(strconv.Itoa(foreign.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	e.powerOff()
	select {
	case <-done:
		t.Fatal("cleanup killed a process that does not belong to this BROray test")
	case <-time.After(200 * time.Millisecond):
	}
}

// publish writes a signed manifest listing the given versions; the last
// one is published as a release tarball, the others as bare binaries.
func (e *e2e) publish(versions ...string) {
	e.t.Helper()
	m := update.Manifest{SchemaVersion: 1, Channel: "test", IssuedAt: time.Now().Add(-time.Minute).UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Revoked: []string{}}
	for i, v := range versions {
		bin, _ := os.ReadFile(e.bins[v])
		var a update.Artifact
		if i == len(versions)-1 {
			tarball := filepath.Join(e.ch, "broray-"+v+"-x86_64.tar.gz")
			dir := filepath.Join(e.t.TempDir(), "broray-"+v)
			_ = os.MkdirAll(dir, 0o755)
			_ = os.WriteFile(filepath.Join(dir, "broray"), bin, 0o755)
			if out, err := exec.Command("tar", "-C", filepath.Dir(dir), "-czf", tarball, filepath.Base(dir)).CombinedOutput(); err != nil {
				e.t.Fatalf("tar: %v %s", err, out)
			}
			tb, _ := os.ReadFile(tarball)
			a = update.Artifact{URL: filepath.Base(tarball), Size: int64(len(tb)), SHA256: sha(tb), Format: "tar.gz", Entry: "broray-" + v + "/broray", UnpackedSize: int64(len(bin))}
		} else {
			name := "broray-" + v + ".bin"
			_ = os.WriteFile(filepath.Join(e.ch, name), bin, 0o644)
			a = update.Artifact{URL: name, Size: int64(len(bin)), SHA256: sha(bin)}
		}
		m.Releases = append(m.Releases, update.Release{Version: v, Installer: map[string]update.Artifact{update.Platform(): a}})
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	p := filepath.Join(e.ch, "manifest.json")
	_ = os.WriteFile(p, raw, 0o644)
	_ = os.WriteFile(p+".sig", e.key.Sign(raw, "e2e", "e2e"), 0o644)
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// launch runs `current/broray launcher apply` and returns the result event.
func (e *e2e) launch(args ...string) (update.Event, string) {
	e.t.Helper()
	out, _ := e.run(filepath.Join(e.root.Current(), "broray"), append([]string{"launcher", "apply", "--channel", "test"}, args...)...)
	var last update.Event
	for _, l := range strings.Split(out, "\n") {
		var ev update.Event
		if json.Unmarshal([]byte(l), &ev) == nil && ev.Event == "result" && ev.Source == "launcher" {
			last = ev
		}
	}
	if last.OK == nil {
		e.t.Fatalf("no launcher result:\n%s", out)
	}
	return last, out
}

func (e *e2e) current() string {
	t, err := os.Readlink(e.root.Current())
	if err != nil {
		e.t.Fatal(err)
	}
	return filepath.Base(t)
}

func (e *e2e) versions() string {
	ents, _ := os.ReadDir(e.root.Versions())
	var names []string
	for _, x := range ents {
		names = append(names, x.Name())
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

func (e *e2e) journal() *installer.Journal {
	j, err := installer.LoadJournal(e.root)
	if err != nil {
		e.t.Fatal(err)
	}
	return j
}

func (e *e2e) wantRunning(version string) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := control.Ping(ctx, e.root)
	if err != nil || v != version {
		e.t.Fatalf("daemon: want %s, got %q %v", version, v, err)
	}
}

func (e *e2e) state(want string, versions string, running string) {
	e.t.Helper()
	if got := e.current(); got != want {
		e.t.Fatalf("current = %s, want %s", got, want)
	}
	if got := e.versions(); got != versions {
		e.t.Fatalf("versions = %q, want %q", got, versions)
	}
	if running != "" {
		e.wantRunning(running)
	}
	if bin, _ := os.ReadFile(filepath.Join(e.opt, "etc/init.d/S24broray")); !strings.Contains(string(bin), "ctl recover") {
		e.t.Fatal("init script lost recover")
	}
}

func TestUpdateEndToEnd(t *testing.T) {
	e := newE2E(t, v1)
	e.publish(v1, v2, v3)
	res, out := e.launch("--version", v2)
	if !*res.OK || res.State != "committed" {
		t.Fatalf("%+v\n%s", res, out)
	}
	for _, stage := range []string{"check", "download", "verify", "staging", "stopping", "switching", "starting", "health", "complete"} {
		if !strings.Contains(out, `"stage":"`+stage+`"`) {
			t.Errorf("stage %s missing", stage)
		}
	}
	e.state(v2, v1+" "+v2, v2)
	if j := e.journal(); j.State != installer.JCommitted || j.From != v1 || j.To != v2 || j.Mode != "switch" {
		t.Fatalf("journal %+v", j)
	}
	if st, _ := os.Stat(e.root.ControlToken()); st.Mode().Perm() != 0o600 {
		t.Fatal("token mode")
	}
	// newest (tarball artefact); at most two versions stay
	res, out = e.launch()
	if !*res.OK {
		t.Fatalf("%+v\n%s", res, out)
	}
	e.state(v3, v2+" "+v3, v3)
	// nothing newer: refused without changes
	res, _ = e.launch()
	if *res.OK || res.Code != update.ErrNotAvailable {
		t.Fatalf("%+v", res)
	}
	// reinstall of the current version (binary swap inside versions/<v>)
	res, out = e.launch("--reinstall")
	if !*res.OK {
		t.Fatalf("%+v\n%s", res, out)
	}
	e.state(v3, v2+" "+v3, v3)
	if j := e.journal(); j.Mode != "replace" || j.State != installer.JCommitted {
		t.Fatalf("%+v", j)
	}
	if _, err := os.Stat(filepath.Join(e.root.Versions(), v3, "broray.old")); err == nil {
		t.Fatal("broray.old left behind")
	}
	if ents, _ := os.ReadDir(e.root.Tmp()); len(ents) != 0 {
		t.Fatalf("tmp: %v", ents)
	}
}

func TestReinstallPreservesAmbiguousStageEntries(t *testing.T) {
	for _, name := range []string{"broray.old", "broray.new"} {
		t.Run(name, func(t *testing.T) {
			e := newE2E(t, v1)
			e.publish(v1, v2)
			foreign := filepath.Join(e.root.Versions(), v1, name)
			if err := os.WriteFile(foreign, []byte("FOREIGN"), 0o600); err != nil {
				t.Fatal(err)
			}
			res, out := e.launch("--reinstall")
			if res.OK != nil && *res.OK {
				t.Fatalf("reinstall accepted ambiguous %s: %+v\n%s", name, res, out)
			}
			if got, err := os.ReadFile(foreign); err != nil || string(got) != "FOREIGN" {
				t.Fatalf("ambiguous %s changed: %d bytes, error %v", name, len(got), err)
			}
		})
	}
}

func TestUpdatePreservesAmbiguousVersionsEntry(t *testing.T) {
	e := newE2E(t, v1)
	unknown := filepath.Join(e.root.Versions(), "notes")
	if err := os.WriteFile(unknown, []byte("owner sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.publish(v1, v2)
	res, out := e.launch("--version", v2)
	if !*res.OK {
		t.Fatalf("update failed: %+v\n%s", res, out)
	}
	if b, err := os.ReadFile(unknown); err != nil || string(b) != "owner sentinel" {
		t.Fatalf("ambiguous entry removed or changed: %q, %v", b, err)
	}
	e.wantRunning(v2)
}

func TestUpdateRefusesExistingTargetDirectory(t *testing.T) {
	e := newE2E(t, v1)
	target := filepath.Join(e.root.Versions(), v2)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "owner-sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.publish(v1, v2)
	res, out := e.launch("--version", v2)
	if *res.OK {
		t.Fatalf("ambiguous existing target accepted: %+v\n%s", res, out)
	}
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "keep" {
		t.Fatalf("existing target removed or changed: %q, %v", b, err)
	}
	e.wantRunning(v1)
}

func TestUpdatePreservesAmbiguousCurrentTemp(t *testing.T) {
	e := newE2E(t, v1)
	tmp := e.root.Current() + ".new"
	if err := os.WriteFile(tmp, []byte("owner sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.publish(v1, v2)
	res, out := e.launch("--version", v2)
	if *res.OK {
		t.Fatalf("ambiguous current.new accepted: %+v\n%s", res, out)
	}
	if b, err := os.ReadFile(tmp); err != nil || string(b) != "owner sentinel" {
		t.Fatalf("ambiguous current.new removed or changed: %q, %v", b, err)
	}
	e.wantRunning(v1)
}

func TestRecoverPreservesAmbiguousCurrentTemp(t *testing.T) {
	e := newE2E(t, v1, "BRORAY_FAKE_CRASH_AFTER="+v2+":"+installer.JSwitching)
	e.publish(v1, v2)
	e.launch()
	if j := e.journal(); j.State != installer.JSwitching {
		t.Fatalf("expected interrupted switch, got %+v", j)
	}
	e.powerOff()
	tmp := e.root.Current() + ".new"
	if err := os.WriteFile(tmp, []byte("owner sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover", "--restart")
	if code == 0 {
		t.Fatalf("recover accepted ambiguous current.new: %s", out)
	}
	if b, err := os.ReadFile(tmp); err != nil || string(b) != "owner sentinel" {
		t.Fatalf("recover removed or changed ambiguous current.new: %q, %v", b, err)
	}
	if j := e.journal(); j.State != installer.JRollingBack {
		t.Fatalf("interrupted recovery must remain recoverable, got %+v", j)
	}
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
	if out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover", "--restart"); code != 0 {
		t.Fatalf("recover after removing test sentinel: code %d, %s", code, out)
	}
	e.state(v1, v1, v1)
}

func TestUpdateReadinessFailureRollsBack(t *testing.T) {
	for _, mode := range []string{"noping", "broken"} {
		t.Run(mode, func(t *testing.T) {
			e := newE2E(t, v1, "BRORAY_FAKE_DAEMON="+v2+":"+mode)
			e.publish(v1, v2)
			res, out := e.launch()
			if *res.OK || !res.RollbackPerformed || res.State != "rolled-back" || !strings.Contains(res.Message, "Восстановлена версия "+v1) {
				t.Fatalf("%+v\n%s", res, out)
			}
			e.state(v1, v1, v1)
			if j := e.journal(); j.State != installer.JRolledBack || j.Error == "" {
				t.Fatalf("%+v", j)
			}
		})
	}
}

func TestRollbackKeepsJournalWhenOldDaemonFailsToRestart(t *testing.T) {
	e := newE2E(t, v1)
	e.env = append(e.env, "BRORAY_FAKE_DAEMON="+v1+":broken,"+v2+":broken")
	e.publish(v1, v2)
	res, out := e.launch()
	if res.State == installer.JRolledBack || res.RollbackPerformed || strings.Contains(res.Message, "Восстановлена версия") {
		t.Fatalf("rollback falsely claimed a running old daemon: %+v\n%s", res, out)
	}
	if j := e.journal(); j.State != installer.JRollingBack {
		t.Fatalf("failed restart must leave a recoverable journal, got %+v", j)
	}
	clean := e.env[:0]
	for _, item := range e.env {
		if !strings.HasPrefix(item, "BRORAY_FAKE_DAEMON=") {
			clean = append(clean, item)
		}
	}
	e.env = append(clean, "BRORAY_FAKE_DAEMON="+v2+":broken")
	if out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover", "--restart"); code != 0 {
		t.Fatalf("recover after old daemon is startable: code %d, %s", code, out)
	}
	if j := e.journal(); j.State != installer.JRolledBack {
		t.Fatalf("recover did not finish rollback: %+v", j)
	}
	e.state(v1, v1, v1)
}

func TestRecoverKeepsJournalWhenOldDaemonFailsToRestart(t *testing.T) {
	e := newE2E(t, v1, "BRORAY_FAKE_CRASH_AFTER="+v2+":"+installer.JSwitching)
	e.publish(v1, v2)
	e.launch()
	if j := e.journal(); j.State != installer.JSwitching {
		t.Fatalf("expected interrupted switch, got %+v", j)
	}
	e.powerOff()
	e.env = append(e.env, "BRORAY_FAKE_DAEMON="+v1+":broken")
	out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover", "--restart")
	if code == 0 {
		t.Fatalf("recover falsely succeeded while old daemon cannot start: %s", out)
	}
	if j := e.journal(); j.State != installer.JRollingBack {
		t.Fatalf("failed restart must leave a recoverable journal, got %+v", j)
	}
	clean := e.env[:0]
	for _, item := range e.env {
		if !strings.HasPrefix(item, "BRORAY_FAKE_DAEMON=") {
			clean = append(clean, item)
		}
	}
	e.env = clean
	if out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover", "--restart"); code != 0 {
		t.Fatalf("recover after old daemon is startable: code %d, %s", code, out)
	}
	if j := e.journal(); j.State != installer.JRolledBack {
		t.Fatalf("recover did not finish rollback: %+v", j)
	}
	e.state(v1, v1, v1)
}

func TestPreSwitchRecoverKeepsJournalWhenOldDaemonFailsToRestart(t *testing.T) {
	e := newE2E(t, v1, "BRORAY_FAKE_CRASH_AFTER="+v2+":"+installer.JStopping)
	e.publish(v1, v2)
	e.launch()
	if j := e.journal(); j.State != installer.JStopping {
		t.Fatalf("expected interrupted stop, got %+v", j)
	}
	e.powerOff()
	e.env = append(e.env, "BRORAY_FAKE_DAEMON="+v1+":broken")
	out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover", "--restart")
	if code == 0 {
		t.Fatalf("recover falsely succeeded while old daemon cannot start: %s", out)
	}
	if j := e.journal(); j.State != installer.JStopping {
		t.Fatalf("failed restart must leave a recoverable journal, got %+v", j)
	}
	clean := e.env[:0]
	for _, item := range e.env {
		if !strings.HasPrefix(item, "BRORAY_FAKE_DAEMON=") {
			clean = append(clean, item)
		}
	}
	e.env = clean
	if out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover", "--restart"); code != 0 {
		t.Fatalf("recover after old daemon is startable: code %d, %s", code, out)
	}
	if j := e.journal(); j.State != installer.JFailed {
		t.Fatalf("recover did not finish pre-switch cleanup: %+v", j)
	}
	e.state(v1, v1, v1)
}

func TestUpdateRefusedWhenDaemonKeepsLock(t *testing.T) {
	e := newE2E(t, v1, "BRORAY_FAKE_DAEMON="+v1+":hang")
	e.publish(v1, v2)
	initBefore, _ := os.ReadFile(filepath.Join(e.opt, "etc/init.d/S24broray"))
	res, out := e.launch()
	if *res.OK || res.Code != "UPDATE_DAEMON_STOP_TIMEOUT" || res.RollbackPerformed {
		t.Fatalf("%+v\n%s", res, out)
	}
	e.state(v1, v1, v1)
	if j := e.journal(); j.State != installer.JFailed {
		t.Fatalf("%+v", j)
	}
	if initAfter, _ := os.ReadFile(filepath.Join(e.opt, "etc/init.d/S24broray")); string(initAfter) != string(initBefore) {
		t.Fatal("init script changed")
	}
}

func TestUpdateWaitsForSlowAcceptedShutdown(t *testing.T) {
	// The product daemon can hold its lock beyond 60 seconds while it removes
	// a temporary UDP route. Zero selects the installer's production default.
	e := newE2E(t, v1, "BRORAY_FAKE_DAEMON="+v1+":delay61", "BRORAY_UPDATE_STOP_TIMEOUT=0")
	e.publish(v1, v2)
	res, out := e.launch()
	if !*res.OK || res.State != "committed" {
		t.Fatalf("accepted shutdown must complete update: %+v\n%s", res, out)
	}
	if !strings.Contains(out, "Служба BROray остановилась за ") || !strings.Contains(out, "блокировка демона получена.") {
		t.Fatalf("missing slow-stop duration in update log:\n%s", out)
	}
	e.state(v2, v1+" "+v2, v2)
}

// TestUpdateInterruptedRecover: the installer dies right after each
// journal state (power loss); after the reboot S24broray start runs
// `ctl recover` and exactly one version is current and running.
func TestUpdateInterruptedRecover(t *testing.T) {
	for _, c := range []struct {
		state, want, versions string
	}{
		{installer.JPrepared, v1, v1},
		{installer.JStopping, v1, v1},
		{installer.JSwitching, v1, v1},
		{installer.JStarted, v1, v1},
		{installer.JCommitted, v2, v1 + " " + v2},
	} {
		t.Run(c.state, func(t *testing.T) {
			e := newE2E(t, v1, "BRORAY_FAKE_CRASH_AFTER="+v2+":"+c.state)
			e.publish(v1, v2)
			res, out := e.launch()
			if *res.OK || res.Code != update.ErrInstallerFailed {
				t.Fatalf("%+v\n%s", res, out)
			}
			if j := e.journal(); j.State != c.state {
				t.Fatalf("journal %s, want %s", j.State, c.state)
			}
			e.powerOff()
			e.initd("start") // boot: recover, then start
			e.state(c.want, c.versions, c.want)
			if j := e.journal(); !j.Terminal() {
				t.Fatalf("journal not terminal: %+v", j)
			}
			// a second recover is a no-op
			if out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover"); code != 0 || strings.TrimSpace(out) != "" {
				t.Fatalf("second recover: %d %s", code, out)
			}
		})
	}
	// "started" with the new daemon already answering (WebUI restore):
	// recover commits instead of rolling back.
	t.Run("started-running", func(t *testing.T) {
		e := newE2E(t, v1, "BRORAY_FAKE_CRASH_AFTER="+v2+":"+installer.JStarted)
		e.publish(v1, v2)
		e.launch()
		if out, code := e.run(filepath.Join(e.root.Current(), "broray"), "start"); code != 0 {
			t.Fatal(out)
		}
		out, code := e.run(filepath.Join(e.root.Current(), "broray"), "ctl", "recover", "--restart")
		if code != 0 || !strings.Contains(out, `"committed"`) {
			t.Fatalf("%d %s", code, out)
		}
		e.state(v2, v1+" "+v2, v2)
	})
	// "switching" while a daemon runs: plain recover refuses, --restart
	// stops it through the socket, rolls back and starts the old version.
	t.Run("switching-running", func(t *testing.T) {
		e := newE2E(t, v1, "BRORAY_FAKE_CRASH_AFTER="+v2+":"+installer.JSwitching)
		e.publish(v1, v2)
		e.launch()
		if out, code := e.run(e.bins[v1], "start"); code != 0 {
			t.Fatal(out)
		}
		if _, code := e.run(e.bins[v1], "ctl", "recover"); code == 0 {
			t.Fatal("recover must refuse a rollback under a running daemon")
		}
		out, code := e.run(e.bins[v1], "ctl", "recover", "--restart")
		if code != 0 || !strings.Contains(out, `"rolled-back"`) {
			t.Fatalf("%d %s", code, out)
		}
		e.state(v1, v1, v1)
	})
}

// TestInstallerApplyGuards: wrong ABI, busy installer lock, no layout.
func TestInstallerApplyGuards(t *testing.T) {
	e := newE2E(t, v1)
	bin := e.bins[v2]
	out, code := e.run(bin, "installer", "apply", "--abi", "2", "--from", v1, "--state", e.root.Dir, "--channel", "test", "--progress", "json")
	if code == 0 || !strings.Contains(out, "UPDATE_ABI_UNSUPPORTED") {
		t.Fatalf("%d %s", code, out)
	}
	l, err := platform.TryLock(e.root.InstallerLock())
	if err != nil {
		t.Fatal(err)
	}
	out, code = e.run(bin, "installer", "apply", "--abi", "1", "--from", v1, "--state", e.root.Dir, "--channel", "test", "--progress", "json")
	l.Release()
	if code == 0 || !strings.Contains(out, "OPERATION_BUSY") {
		t.Fatalf("%d %s", code, out)
	}
	e.state(v1, v1, v1)
	// apply directly (no launcher) works too and keeps the ABI contract
	out, code = e.run(bin, update.InstallerArgs(v1, e.root.Dir, "test")...)
	if code != 0 || !strings.Contains(out, `"state":"committed"`) {
		t.Fatalf("%d %s", code, out)
	}
	e.state(v2, v1+" "+v2, v2)
}

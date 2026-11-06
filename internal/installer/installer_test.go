package installer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

func TestAbortBeforeSwitchKeepsJournalWhenRestartFails(t *testing.T) {
	root, _ := fixture(t)
	if err := os.WriteFile(InitScript(), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := &Journal{OperationID: "abort-restart-test", Mode: "switch", From: "3.5.0-t1", To: "3.5.0-t2", DaemonWasRunning: true}
	if err := j.set(root, JStopping); err != nil {
		t.Fatal(err)
	}
	dl, err := platform.TryLock(root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	o := ApplyOptions{Root: root, Progress: update.NewEmitter(io.Discard, "test"), Log: io.Discard}
	res := abortBeforeSwitch(o, j, dl, errors.New("injected switch journal failure"))
	if res.State == JFailed || res.Code != "UPDATE_ABORT_RESTART_FAILED" {
		t.Fatalf("failed old daemon restart was reported terminal: %+v", res)
	}
	got, err := LoadJournal(root)
	if err != nil || got == nil || got.State != JStopping {
		t.Fatalf("failed restart must leave recoverable stopping journal: %+v, %v", got, err)
	}
}

// fixture builds <tmp>/opt with a BROray root holding a data sentinel, a
// config and the init/bin entries, and points Opt at it.
func fixture(t *testing.T) (platform.Root, string) {
	t.Helper()
	opt := filepath.Join(t.TempDir(), "opt")
	old := Opt
	Opt = opt
	t.Cleanup(func() { Opt = old })
	root := platform.Root{Dir: filepath.Join(opt, "broray")}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := platform.DefaultConfig()
	cfg.Keenetic.WriteTransport = "rci"
	cfg.Keenetic.RCIURL = "http://127.0.0.1:1" // nothing listens: any Keenetic call fails
	if err := platform.SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Data(), "owner-data.txt"), []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{InitScript(), BinLink()} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o755)
	}
	return root, opt
}

func TestInstallPreservesForeignTempSymlink(t *testing.T) {
	opt := filepath.Join(t.TempDir(), "opt")
	old := Opt
	Opt = opt
	t.Cleanup(func() { Opt = old })
	if err := os.MkdirAll(filepath.Dir(InitScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	root := platform.Root{Dir: filepath.Join(opt, "broray")}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	verDir := filepath.Join(root.Versions(), "3.5.0-test")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(opt, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("FOREIGN"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(verDir, "broray.new")); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, "3.5.0-test", false); err == nil {
		t.Fatal("install accepted preexisting temporary binary symlink")
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "FOREIGN" {
		t.Fatalf("foreign file changed: %d bytes, error %v", len(got), err)
	}
}

func TestInstallPreservesForeignCurrentTemp(t *testing.T) {
	opt := filepath.Join(t.TempDir(), "opt")
	old := Opt
	Opt = opt
	t.Cleanup(func() { Opt = old })
	if err := os.MkdirAll(filepath.Dir(InitScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	root := platform.Root{Dir: filepath.Join(opt, "broray")}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	tmp := root.Current() + ".new"
	if err := os.WriteFile(tmp, []byte("FOREIGN"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, "3.5.0-test", false); err == nil {
		t.Fatal("install accepted foreign current.new")
	}
	if got, err := os.ReadFile(tmp); err != nil || string(got) != "FOREIGN" {
		t.Fatalf("foreign current.new changed: %d bytes, error %v", len(got), err)
	}
}

func TestInstallPreservesForeignBinLink(t *testing.T) {
	opt := filepath.Join(t.TempDir(), "opt")
	old := Opt
	Opt = opt
	t.Cleanup(func() { Opt = old })
	for _, path := range []string{InitScript(), BinLink()} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	root := platform.Root{Dir: filepath.Join(opt, "broray")}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BinLink(), []byte("FOREIGN"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, "3.5.0-test", false); err == nil {
		t.Fatal("install accepted foreign bin link")
	}
	info, err := os.Lstat(BinLink())
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("foreign bin link replaced: %v, %v", info, err)
	}
	if got, err := os.ReadFile(BinLink()); err != nil || string(got) != "FOREIGN" {
		t.Fatalf("foreign bin link changed: %d bytes, error %v", len(got), err)
	}
}

func TestCopyFileRejectsExistingSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	foreign := filepath.Join(dir, "foreign")
	dst := filepath.Join(dir, "destination")
	if err := os.WriteFile(src, []byte("SOURCE"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("FOREIGN"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, dst); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst, 0o755); err == nil {
		t.Fatal("copyFile accepted existing symlink")
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "FOREIGN" {
		t.Fatalf("foreign file changed: %d bytes, error %v", len(got), err)
	}
}

func TestInstallCleanThenAdvanceVersion(t *testing.T) {
	opt := filepath.Join(t.TempDir(), "opt")
	old := Opt
	Opt = opt
	t.Cleanup(func() { Opt = old })
	if err := os.MkdirAll(filepath.Dir(InitScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	root := platform.Root{Dir: filepath.Join(opt, "broray")}
	for _, version := range []string{"3.5.0-dev5", "3.5.0-dev6"} {
		if _, err := Install(root, version, false); err != nil {
			t.Fatalf("install %s: %v", version, err)
		}
	}
	if target, err := os.Readlink(root.Current()); err != nil || target != filepath.Join("versions", "3.5.0-dev6") {
		t.Fatalf("current: %q, %v", target, err)
	}
	if target, err := os.Readlink(BinLink()); err != nil || target != filepath.Join(root.Current(), "broray") {
		t.Fatalf("bin link: %q, %v", target, err)
	}
	if _, err := Install(root, "3.5.0-dev6", false); err == nil {
		t.Fatal("same-version install should use the journalled reinstall path")
	}
	if target, err := os.Readlink(root.Current()); err != nil || target != filepath.Join("versions", "3.5.0-dev6") {
		t.Fatalf("current changed after rejected repeat: %q, %v", target, err)
	}
}

func TestInstallRejectsSymlinkedRootBeforeWriting(t *testing.T) {
	opt := filepath.Join(t.TempDir(), "opt")
	old := Opt
	Opt = opt
	t.Cleanup(func() { Opt = old })
	if err := os.MkdirAll(filepath.Dir(InitScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(opt, "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	root := platform.Root{Dir: filepath.Join(opt, "broray")}
	if err := os.Symlink(foreign, root.Dir); err != nil {
		t.Fatal(err)
	}
	_, installErr := Install(root, "3.5.0-dev6", false)
	entries, err := os.ReadDir(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if installErr == nil || len(entries) != 0 {
		t.Fatalf("symlinked root accepted or foreign tree changed: install error %v, entries %d", installErr, len(entries))
	}
}

func TestInstallRejectsSymlinkedVersionsBeforeWriting(t *testing.T) {
	opt := filepath.Join(t.TempDir(), "opt")
	old := Opt
	Opt = opt
	t.Cleanup(func() { Opt = old })
	if err := os.MkdirAll(filepath.Dir(InitScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	root := platform.Root{Dir: filepath.Join(opt, "broray")}
	if err := os.Mkdir(root.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(opt, "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, root.Versions()); err != nil {
		t.Fatal(err)
	}
	_, installErr := Install(root, "3.5.0-dev6", false)
	entries, err := os.ReadDir(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if installErr == nil || len(entries) != 0 {
		t.Fatalf("symlinked versions accepted or foreign tree changed: install error %v, entries %d", installErr, len(entries))
	}
}

func TestCleanupStagedPreservesUnownedReplaceTemp(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-test")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(verDir, "broray.new")
	if err := os.WriteFile(foreign, []byte("FOREIGN"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanupStaged(root, &Journal{Mode: "replace", To: "3.5.0-test"})
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "FOREIGN" {
		t.Fatalf("unowned staged entry changed: %d bytes, error %v", len(got), err)
	}
}

func TestCleanupStagedPreservesForeignSwitchEntry(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-new")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(verDir, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("FOREIGN"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupStaged(root, &Journal{Mode: "switch", CreatedDir: true, To: "3.5.0-new", PreviousTarget: "versions/3.5.0-old"}); err == nil {
		t.Fatal("foreign staged entry was not reported")
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("foreign switch entry removed: %q %v", b, err)
	}
}

func TestCleanupStagedPreservesSymlinkedSwitchTarget(t *testing.T) {
	root, opt := fixture(t)
	target := filepath.Join(opt, "foreign-version")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root.Versions(), "3.5.0-new")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := cleanupStaged(root, &Journal{Mode: "switch", CreatedDir: true, To: "3.5.0-new", PreviousTarget: "versions/3.5.0-old"}); err == nil {
		t.Fatal("symlinked staged version was not reported")
	}
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatalf("foreign switch link removed: %q %v", got, err)
	}
}

func TestCleanupStagedRemovesOnlyMatchingSwitchBinary(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-new")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(verDir, "broray")
	if err := os.WriteFile(staged, []byte("OWNED"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA256(staged)
	if err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", CreatedDir: true, To: "3.5.0-new", PreviousTarget: "versions/3.5.0-old", StagedSHA256: sha}
	if err := cleanupStaged(root, j); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(verDir); !os.IsNotExist(err) {
		t.Fatalf("owned staged version remains: %v", err)
	}
}

func TestCleanupStagedPreservesChecksumMismatch(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-new")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(verDir, "broray")
	if err := os.WriteFile(staged, []byte("FOREIGN"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", CreatedDir: true, To: "3.5.0-new", PreviousTarget: "versions/3.5.0-old", StagedSHA256: strings.Repeat("0", 64)}
	if err := cleanupStaged(root, j); err == nil {
		t.Fatal("checksum mismatch was not reported")
	}
	if b, err := os.ReadFile(staged); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("mismatched staged binary changed: %q %v", b, err)
	}
}

func TestRecoverPreservesJournalOnAmbiguousStagedVersion(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-new")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(verDir, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("FOREIGN"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := &Journal{OperationID: "cleanup-refusal", Mode: "switch", CreatedDir: true, From: "3.5.0-old", To: "3.5.0-new", PreviousTarget: "versions/3.5.0-old"}
	if err := j.set(root, JPrepared); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverLocked(ApplyOptions{Root: root}, j, false); err == nil {
		t.Fatal("recovery accepted ambiguous staged version")
	}
	got, err := LoadJournal(root)
	if err != nil || got == nil || got.State != JPrepared {
		t.Fatalf("recoverable journal lost: %+v %v", got, err)
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("foreign staged entry changed: %q %v", b, err)
	}
}

func TestSwitchBackPreservesForeignVersionContents(t *testing.T) {
	root, _ := fixture(t)
	if err := os.Symlink("versions/3.5.0-new", root.Current()); err != nil {
		t.Fatal(err)
	}
	verDir := filepath.Join(root.Versions(), "3.5.0-new")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(verDir, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("FOREIGN"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", CreatedDir: true, From: "3.5.0-old", To: "3.5.0-new",
		PreviousTarget: "versions/3.5.0-old", NewTarget: "versions/3.5.0-new"}
	if err := switchBack(root, j); err == nil {
		t.Fatal("rollback accepted foreign version contents")
	}
	if got, err := os.Readlink(root.Current()); err != nil || got != j.PreviousTarget {
		t.Fatalf("old version was not repointed: %q %v", got, err)
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("rollback removed foreign version contents: %q %v", b, err)
	}
}

func TestSwitchBackPreservesForeignReplaceStagedBinary(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-old")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(verDir, "broray.new")
	current := filepath.Join(verDir, "broray")
	if err := os.WriteFile(current, []byte("OWNED"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousSHA, err := fileSHA256(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("FOREIGN"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "replace", From: "3.5.0-old", To: "3.5.0-old", PreviousSHA256: previousSHA, StagedSHA256: strings.Repeat("0", 64)}
	if err := switchBack(root, j); err == nil {
		t.Fatal("rollback accepted foreign replacement")
	}
	if b, err := os.ReadFile(staged); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("rollback removed foreign replacement: %q %v", b, err)
	}
}

func TestCommitPreservesForeignPreviousBinary(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-old")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(verDir, "broray.old")
	if err := os.WriteFile(previous, []byte("FOREIGN"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "replace", From: "3.5.0-old", To: "3.5.0-old", PreviousSHA256: strings.Repeat("0", 64)}
	if err := j.set(root, JStarted); err != nil {
		t.Fatal(err)
	}
	if err := commit(root, j); err == nil {
		t.Fatal("commit accepted foreign previous binary")
	}
	if b, err := os.ReadFile(previous); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("commit removed foreign previous binary: %q %v", b, err)
	}
	got, err := LoadJournal(root)
	if err != nil || got == nil || got.State != JStarted {
		t.Fatalf("commit lost recoverable journal: %+v %v", got, err)
	}
}

func TestSwitchBackRemovesOwnedVersionAfterRepoint(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-new")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(verDir, "broray")
	if err := os.WriteFile(bin, []byte("OWNED"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA256(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("versions/3.5.0-new", root.Current()); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", CreatedDir: true, From: "3.5.0-old", To: "3.5.0-new",
		PreviousTarget: "versions/3.5.0-old", NewTarget: "versions/3.5.0-new", StagedSHA256: sha}
	if err := switchBack(root, j); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(root.Current()); err != nil || got != j.PreviousTarget {
		t.Fatalf("current points to %q: %v", got, err)
	}
	if _, err := os.Lstat(verDir); !os.IsNotExist(err) {
		t.Fatalf("owned version remains: %v", err)
	}
}

func TestReplaceRollbackAndCommitOwnBinary(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-old")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(verDir, "broray.old")
	current := filepath.Join(verDir, "broray")
	if err := os.WriteFile(old, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevSHA, err := fileSHA256(old)
	if err != nil {
		t.Fatal(err)
	}
	stagedSHA, err := fileSHA256(current)
	if err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "replace", From: "3.5.0-old", To: "3.5.0-old", PreviousSHA256: prevSHA, StagedSHA256: stagedSHA}
	if err := switchBack(root, j); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(current); err != nil || string(b) != "OLD" {
		t.Fatalf("previous version not restored: %q %v", b, err)
	}
	if err := os.WriteFile(old, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := j.set(root, JStarted); err != nil {
		t.Fatal(err)
	}
	if err := commit(root, j); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatalf("owned old binary remains: %v", err)
	}
	got, err := LoadJournal(root)
	if err != nil || got == nil || got.State != JCommitted {
		t.Fatalf("commit journal: %+v %v", got, err)
	}
}

func TestCommitPreservesForeignInitBackup(t *testing.T) {
	root, _ := fixture(t)
	backup := filepath.Join(root.UpdateState(), "S24broray.prev")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("FOREIGN"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", InitBackup: backup, From: "3.5.0-old", To: "3.5.0-new"}
	if err := j.set(root, JStarted); err != nil {
		t.Fatal(err)
	}
	if err := commit(root, j); err == nil {
		t.Fatal("commit accepted foreign init backup")
	}
	if b, err := os.ReadFile(backup); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("foreign init backup changed: %q %v", b, err)
	}
}

func TestSwitchForwardPreservesForeignInitBackup(t *testing.T) {
	root, _ := fixture(t)
	for _, version := range []string{"3.5.0-old", "3.5.0-new"} {
		verDir := filepath.Join(root.Versions(), version)
		if err := os.Mkdir(verDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(verDir, "broray"), []byte(version), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prevSHA, err := fileSHA256(filepath.Join(root.Versions(), "3.5.0-old", "broray"))
	if err != nil {
		t.Fatal(err)
	}
	stagedSHA, err := fileSHA256(filepath.Join(root.Versions(), "3.5.0-new", "broray"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(InitScript(), []byte(renderInitScript(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root.UpdateState(), "S24broray.prev")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("FOREIGN"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("versions/3.5.0-old", root.Current()); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", From: "3.5.0-old", To: "3.5.0-new",
		PreviousTarget: "versions/3.5.0-old", NewTarget: "versions/3.5.0-new",
		PreviousSHA256: prevSHA, StagedSHA256: stagedSHA, InitBackup: backup}
	if err := switchForward(root, j); err == nil {
		t.Fatal("switch overwrote a foreign init backup")
	}
	if b, err := os.ReadFile(backup); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("foreign init backup changed: %q %v", b, err)
	}
}

func TestSwitchForwardRejectsChangedCurrent(t *testing.T) {
	root, _ := fixture(t)
	for _, version := range []string{"3.5.0-old", "3.5.0-new"} {
		verDir := filepath.Join(root.Versions(), version)
		if err := os.Mkdir(verDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(verDir, "broray"), []byte(version), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prevSHA, err := fileSHA256(filepath.Join(root.Versions(), "3.5.0-old", "broray"))
	if err != nil {
		t.Fatal(err)
	}
	stagedSHA, err := fileSHA256(filepath.Join(root.Versions(), "3.5.0-new", "broray"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(InitScript()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("versions/3.5.0-foreign", root.Current()); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", From: "3.5.0-old", To: "3.5.0-new",
		PreviousTarget: "versions/3.5.0-old", NewTarget: "versions/3.5.0-new",
		PreviousSHA256: prevSHA, StagedSHA256: stagedSHA}
	if err := switchForward(root, j); err == nil {
		t.Fatal("switch accepted changed current target")
	}
	if got, err := os.Readlink(root.Current()); err != nil || got != "versions/3.5.0-foreign" {
		t.Fatalf("foreign current target changed: %q %v", got, err)
	}
}

func TestSwitchForwardRejectsChangedStagedBinary(t *testing.T) {
	root, _ := fixture(t)
	oldDir := filepath.Join(root.Versions(), "3.5.0-old")
	if err := os.Mkdir(oldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldBin := filepath.Join(oldDir, "broray")
	if err := os.WriteFile(oldBin, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevSHA, err := fileSHA256(oldBin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(InitScript()); err != nil {
		t.Fatal(err)
	}
	verDir := filepath.Join(root.Versions(), "3.5.0-new")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(verDir, "broray"), []byte("FOREIGN"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("versions/3.5.0-old", root.Current()); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", From: "3.5.0-old", To: "3.5.0-new",
		PreviousTarget: "versions/3.5.0-old", NewTarget: "versions/3.5.0-new",
		PreviousSHA256: prevSHA, StagedSHA256: strings.Repeat("0", 64)}
	if err := switchForward(root, j); err == nil {
		t.Fatal("switch accepted changed staged binary")
	}
	if got, err := os.Readlink(root.Current()); err != nil || got != j.PreviousTarget {
		t.Fatalf("current target changed: %q %v", got, err)
	}
}

func TestSwitchForwardRejectsChangedPreviousBinary(t *testing.T) {
	root, _ := fixture(t)
	if err := os.Remove(InitScript()); err != nil {
		t.Fatal(err)
	}
	verDir := filepath.Join(root.Versions(), "3.5.0-old")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(verDir, "broray")
	if err := os.WriteFile(current, []byte("FOREIGN"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(verDir, "broray.new")
	if err := os.WriteFile(staged, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA256(staged)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("versions/3.5.0-old", root.Current()); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "replace", From: "3.5.0-old", To: "3.5.0-old",
		PreviousTarget: "versions/3.5.0-old", NewTarget: "versions/3.5.0-old",
		PreviousSHA256: strings.Repeat("0", 64), StagedSHA256: sha}
	if err := switchForward(root, j); err == nil {
		t.Fatal("replace accepted changed previous binary")
	}
	if b, err := os.ReadFile(current); err != nil || string(b) != "FOREIGN" {
		t.Fatalf("foreign previous binary changed: %q %v", b, err)
	}
}

func TestSwitchForwardOwnedSwitchAndReplace(t *testing.T) {
	t.Run("switch", func(t *testing.T) {
		root, _ := fixture(t)
		if err := os.Remove(InitScript()); err != nil {
			t.Fatal(err)
		}
		for _, version := range []string{"3.5.0-old", "3.5.0-new"} {
			verDir := filepath.Join(root.Versions(), version)
			if err := os.Mkdir(verDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(verDir, "broray"), []byte(version), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		prevSHA, err := fileSHA256(filepath.Join(root.Versions(), "3.5.0-old", "broray"))
		if err != nil {
			t.Fatal(err)
		}
		stagedSHA, err := fileSHA256(filepath.Join(root.Versions(), "3.5.0-new", "broray"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("versions/3.5.0-old", root.Current()); err != nil {
			t.Fatal(err)
		}
		j := &Journal{Mode: "switch", From: "3.5.0-old", To: "3.5.0-new",
			PreviousTarget: "versions/3.5.0-old", NewTarget: "versions/3.5.0-new",
			PreviousSHA256: prevSHA, StagedSHA256: stagedSHA}
		if err := switchForward(root, j); err != nil {
			t.Fatal(err)
		}
		if got, err := os.Readlink(root.Current()); err != nil || got != j.NewTarget {
			t.Fatalf("current target: %q %v", got, err)
		}
	})
	t.Run("replace", func(t *testing.T) {
		root, _ := fixture(t)
		if err := os.Remove(InitScript()); err != nil {
			t.Fatal(err)
		}
		verDir := filepath.Join(root.Versions(), "3.5.0-old")
		if err := os.Mkdir(verDir, 0o700); err != nil {
			t.Fatal(err)
		}
		old := filepath.Join(verDir, "broray")
		staged := filepath.Join(verDir, "broray.new")
		if err := os.WriteFile(old, []byte("OLD"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(staged, []byte("NEW"), 0o755); err != nil {
			t.Fatal(err)
		}
		prevSHA, err := fileSHA256(old)
		if err != nil {
			t.Fatal(err)
		}
		stagedSHA, err := fileSHA256(staged)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("versions/3.5.0-old", root.Current()); err != nil {
			t.Fatal(err)
		}
		j := &Journal{Mode: "replace", From: "3.5.0-old", To: "3.5.0-old",
			PreviousTarget: "versions/3.5.0-old", NewTarget: "versions/3.5.0-old",
			PreviousSHA256: prevSHA, StagedSHA256: stagedSHA}
		if err := switchForward(root, j); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(old); err != nil || string(b) != "NEW" {
			t.Fatalf("replacement not installed: %q %v", b, err)
		}
		if b, err := os.ReadFile(filepath.Join(verDir, "broray.old")); err != nil || string(b) != "OLD" {
			t.Fatalf("previous binary not retained: %q %v", b, err)
		}
	})
}

func sentinelCount(t *testing.T, dir string) int {
	n := 0
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "owner-data.txt" {
			if b, _ := os.ReadFile(p); string(b) == "SENTINEL" {
				n++
			}
		}
		return nil
	})
	return n
}

func TestUninstallKeepDataControl(t *testing.T) {
	root, opt := ownedUninstallFixture(t)
	rep, err := Uninstall(context.Background(), root, true)
	if err != nil {
		t.Fatalf("uninstall: %v %+v", err, rep)
	}
	if _, err := os.Stat(root.Dir); !os.IsNotExist(err) {
		t.Fatal("root still exists")
	}
	if n := sentinelCount(t, filepath.Join(opt, "broray-preserved")); n != 1 {
		t.Fatalf("preserved sentinel count %d", n)
	}
	matches, _ := filepath.Glob(filepath.Join(opt, "broray-preserved", "*", "config.v1.json"))
	if len(matches) != 1 {
		t.Fatalf("config not preserved: %v", matches)
	}
}

// DEV1_UNINSTALL_KEEP_DATA_LOSS_ON_PRESERVE_FAILURE: a regular file where the
// preservation directory must go. Uninstall must fail and delete nothing.
func TestUninstallKeepDataBlockedPreserveParent(t *testing.T) {
	root, opt := ownedUninstallFixture(t)
	if err := os.WriteFile(filepath.Join(opt, "broray-preserved"), []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Uninstall(context.Background(), root, true)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_PRESERVE_FAILED") {
		t.Fatalf("want UNINSTALL_PRESERVE_FAILED, got %v", err)
	}
	if n := sentinelCount(t, root.Dir); n != 1 {
		t.Fatalf("original data lost: sentinel count %d", n)
	}
	for _, p := range []string{InitScript(), BinLink(), root.ConfigFile()} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed after failed preservation", p)
		}
	}
}

func TestUninstallKeepDataExistingTarget(t *testing.T) {
	root, opt := ownedUninstallFixture(t)
	// Read-only parent: Mkdir of the timestamp directory fails.
	parent := filepath.Join(opt, "broray-preserved")
	if err := os.MkdirAll(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(parent, 0o500)
	defer os.Chmod(parent, 0o700)
	if os.Geteuid() == 0 {
		// root ignores directory permissions; use a pre-existing target instead.
		_ = os.Chmod(parent, 0o700)
		dst, err := preserveData(root, mustTime("2026-01-01T00:00:00Z"))
		if err != nil {
			t.Fatal(err)
		}
		// put the data back to simulate a second run in the same second
		if err := os.Rename(filepath.Join(dst, "data"), root.Data()); err != nil {
			t.Fatal(err)
		}
		if _, err := preserveData(root, mustTime("2026-01-01T00:00:00Z")); err == nil {
			t.Fatal("existing target accepted")
		}
		if n := sentinelCount(t, root.Data()); n != 1 {
			t.Fatalf("data lost on existing target: %d", n)
		}
		return
	}
	_, err := Uninstall(context.Background(), root, true)
	if err == nil {
		t.Fatal("expected failure")
	}
	if n := sentinelCount(t, root.Dir); n != 1 {
		t.Fatalf("data lost: %d", n)
	}
}

// A Keenetic receipt exists but KeeneticOS is unreachable: uninstall must
// stop before deleting receipts, data or binaries.
func TestUninstallKeepsEverythingWhenKeeneticCleanupFails(t *testing.T) {
	root, _ := ownedUninstallFixture(t)
	receipt := `{"schemaVersion":3,"owner":"BROray","interfaceName":"Proxy0","protocol":"socks5","upstream":{"host":"192.168.1.1","port":2080}}`
	if err := os.WriteFile(filepath.Join(root.Receipts(), "interface.json"), []byte(receipt), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Uninstall(context.Background(), root, false)
	if err == nil || !strings.Contains(err.Error(), "UNINSTALL_KEENETIC_CLEANUP_INCOMPLETE") {
		t.Fatalf("want UNINSTALL_KEENETIC_CLEANUP_INCOMPLETE, got %v", err)
	}
	for _, p := range []string{filepath.Join(root.Receipts(), "interface.json"), filepath.Join(root.Data(), "owner-data.txt"), InitScript(), BinLink()} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed although Keenetic cleanup failed", p)
		}
	}
}

func TestUninstallWithoutKeepData(t *testing.T) {
	root, opt := ownedUninstallFixture(t)
	if _, err := Uninstall(context.Background(), root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root.Dir); !os.IsNotExist(err) {
		t.Fatal("root still exists")
	}
	if _, err := os.Stat(filepath.Join(opt, "broray-preserved")); !os.IsNotExist(err) {
		t.Fatal("preserved dir created without --keep-data")
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

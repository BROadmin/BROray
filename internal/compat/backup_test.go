package compat

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func writeT(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestBackupStageAndSwap(t *testing.T) {
	live := &Deps{Root: platform.Root{Dir: t.TempDir()}, Log: log.New(io.Discard, "", 0)}
	writeT(t, live.Path("servers", "a.json"), "A")
	writeT(t, live.Path("config", "active-server"), "a\n")
	st, err := NewBackupStage(live, filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Take(DataRel("servers")); err != nil {
		t.Fatal(err)
	}
	if err := st.Take(DataRel("config", "new.json")); err != nil { // missing live path
		t.Fatal(err)
	}
	if err := st.Copy(DataRel("config", "active-server")); err != nil {
		t.Fatal(err)
	}
	if got := readT(t, st.St.Path("servers", "a.json")); got != "A" {
		t.Fatalf("stage copy %q", got)
	}
	writeT(t, st.St.Path("servers", "b.json"), "B")
	writeT(t, st.St.Path("config", "new.json"), "N")
	if p := st.Paths(); len(p) != 2 || p[0] != "data/config/new.json" || p[1] != "data/servers" {
		t.Fatalf("paths %v", p)
	}
	for _, bad := range []string{"../x", "/etc/passwd", "data/../../x", ""} {
		if err := st.Take(bad); err == nil {
			t.Fatalf("Take(%q) accepted", bad)
		}
	}
	pre := filepath.Join(t.TempDir(), "pre")
	entries, err := SwapBackupPaths(live.Root.Dir, st.Dir, pre, st.Paths())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Existed || !entries[1].Existed {
		t.Fatalf("entries %+v", entries)
	}
	if readT(t, live.Path("servers", "b.json")) != "B" || readT(t, live.Path("config", "new.json")) != "N" {
		t.Fatal("live not swapped")
	}
	if readT(t, filepath.Join(pre, "data", "servers", "a.json")) != "A" || readT(t, filepath.Join(pre, "data", "servers", "b.json")) != "<missing>" {
		t.Fatal("pre copy must hold the original tree")
	}
	// undo: the pre copy is the stage; a path that did not exist is removed
	undoPre := filepath.Join(t.TempDir(), "undo")
	paths := []string{entries[0].Path, entries[1].Path}
	if _, err := SwapBackupPaths(live.Root.Dir, pre, undoPre, paths); err != nil {
		t.Fatal(err)
	}
	if readT(t, live.Path("servers", "b.json")) != "<missing>" || readT(t, live.Path("servers", "a.json")) != "A" ||
		readT(t, live.Path("config", "new.json")) != "<missing>" {
		t.Fatal("undo did not restore the original state")
	}
	if readT(t, filepath.Join(undoPre, "data", "config", "new.json")) != "N" {
		t.Fatal("undo must keep what it removed")
	}
}

func TestBackupStageRefusesSymlinks(t *testing.T) {
	live := &Deps{Root: platform.Root{Dir: t.TempDir()}, Log: log.New(io.Discard, "", 0)}
	writeT(t, live.Path("servers", "a.json"), "A")
	if err := os.Symlink("/etc/passwd", live.Path("servers", "evil.json")); err != nil {
		t.Fatal(err)
	}
	st, err := NewBackupStage(live, filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Take(DataRel("servers")); err == nil {
		t.Fatal("a symlink in a staged tree must be refused")
	}
}

func TestSwapRollsBackOnFailure(t *testing.T) {
	liveRoot, stage, pre := t.TempDir(), t.TempDir(), t.TempDir()
	writeT(t, filepath.Join(liveRoot, "data", "a"), "old-a")
	writeT(t, filepath.Join(stage, "data", "a"), "new-a")
	writeT(t, filepath.Join(liveRoot, "other", "b"), "old-b")
	writeT(t, filepath.Join(stage, "other", "b"), "new-b")
	// the second path cannot be backed up: a file where its pre parent dir should be
	writeT(t, filepath.Join(pre, "other"), "not a dir")
	if _, err := SwapBackupPaths(liveRoot, stage, pre, []string{"data/a", "other/b"}); err == nil {
		t.Fatal("expected failure")
	}
	if readT(t, filepath.Join(liveRoot, "data", "a")) != "old-a" || readT(t, filepath.Join(liveRoot, "other", "b")) != "old-b" {
		t.Fatal("live state must be unchanged after a failed swap")
	}
}

func TestBackupPartsOrder(t *testing.T) {
	RegisterBackupPart(BackupPart{Name: "zz-test-b", Order: -100})
	RegisterBackupPart(BackupPart{Name: "zz-test-a", Order: -100})
	t.Cleanup(func() {
		backupMu.Lock()
		delete(backupParts, "zz-test-a")
		delete(backupParts, "zz-test-b")
		backupMu.Unlock()
	})
	p := BackupParts()
	if len(p) < 2 || p[0].Name != "zz-test-a" || p[1].Name != "zz-test-b" {
		t.Fatalf("%v", p)
	}
}

func TestMergeBackupFieldsAndDigest(t *testing.T) {
	live, src := t.TempDir(), t.TempDir()
	rel := "data/x/config.json"
	writeT(t, filepath.Join(live, rel), `{"sel":["a"],"managed":[{"id":"live"}],"n":1}`)
	writeT(t, filepath.Join(src, rel), `{"sel":["b"],"managed":[{"id":"stale"}],"gone":true}`)
	d0, err := BackupDigest(live, rel, []string{"sel", "gone"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MergeBackupFields(live, src, rel, []string{"sel", "gone", "absent"}); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := ReadJSON(filepath.Join(src, rel), &m); err != nil {
		t.Fatal(err)
	}
	if b, _ := Canonical(m); string(b) != `{"gone":true,"managed":[{"id":"live"}],"n":1,"sel":["b"]}`+"\n" {
		t.Fatalf("merged %s", b)
	}
	// only the fields count for a merged file
	writeT(t, filepath.Join(live, rel), `{"sel":["a"],"managed":[],"n":2}`)
	if d1, _ := BackupDigest(live, rel, []string{"sel", "gone"}); d1 != d0 {
		t.Fatal("digest depends on fields outside the backup")
	}
	writeT(t, filepath.Join(live, rel), `{"sel":["c"]}`)
	if d2, _ := BackupDigest(live, rel, []string{"sel", "gone"}); d2 == d0 {
		t.Fatal("digest ignores a changed field")
	}
	// a missing source removes the fields from the live object
	_ = os.Remove(filepath.Join(src, rel))
	if err := MergeBackupFields(live, src, rel, []string{"sel"}); err != nil || readT(t, filepath.Join(src, rel)) != "{}\n" {
		t.Fatalf("%v %q", err, readT(t, filepath.Join(src, rel)))
	}
	// whole trees: content and names count
	writeT(t, filepath.Join(live, "data/d/a.json"), "1")
	t1, _ := BackupDigest(live, "data/d", nil)
	writeT(t, filepath.Join(live, "data/d/b.json"), "1")
	t2, _ := BackupDigest(live, "data/d", nil)
	_ = os.Remove(filepath.Join(live, "data/d/b.json"))
	t3, _ := BackupDigest(live, "data/d", nil)
	a1, _ := BackupDigest(live, "data/none", nil)
	a2, _ := BackupDigest(live, "data/none2", nil)
	if t1 == t2 || t1 != t3 || a1 != a2 || a1 == t1 {
		t.Fatal("tree digest")
	}
}

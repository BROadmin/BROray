package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/installer"
)

func TestCleanupOwnedVersionsAndImportCopies(t *testing.T) {
	a := testArea(t)
	root := a.d.Root
	addVersion := func(v string) installer.OwnedVersion {
		t.Helper()
		data := []byte("version fixture " + v)
		dir := filepath.Join(root.Dir, "versions", v)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "broray"), data, 0700); err != nil {
			t.Fatal(err)
		}
		return installer.OwnedVersion{Version: v, SHA256: compat.SHA256Hex(data)}
	}
	obsolete := addVersion("3.5.7")
	fallback := addVersion("3.5.8")
	active := addVersion("3.5.9")
	foreign := addVersion("3.5.6")
	if err := os.Symlink("versions/"+active.Version, root.Current()); err != nil {
		t.Fatal(err)
	}
	journal := installer.Journal{SchemaVersion: 1, Mode: "switch", State: installer.JCommitted,
		To: active.Version, From: fallback.Version, NewTarget: "versions/" + active.Version,
		PreviousTarget: "versions/" + fallback.Version, OwnedVersions: []installer.OwnedVersion{obsolete, fallback, active}}
	if err := os.MkdirAll(filepath.Dir(root.UpdateJournal()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := compat.WriteJSON(root.UpdateJournal(), journal); err != nil {
		t.Fatal(err)
	}
	addCopy := func(i int) string {
		t.Helper()
		id := fmt.Sprintf("settings-before-import-20261120-12000%d-%08x", i, i)
		copyRoot := a.backupDir(id, backupCopyRoot)
		const path = "data/test.json"
		if err := os.MkdirAll(filepath.Join(copyRoot, "data"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(copyRoot, path), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		sum, err := compat.BackupDigest(copyRoot, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		m := copyManifest{SchemaVersion: 1, Complete: true, Kind: "pre-import",
			CreatedAt: time.Date(2026, 11, 20, 12, 0, i, 0, time.UTC).Format(time.RFC3339),
			Paths:     []compat.SwapEntry{{Path: path, Existed: true, OriginalSHA256: sum, SHA256: sum}}}
		if err := compat.WriteJSON(a.backupDir(id, "manifest.json"), m); err != nil {
			t.Fatal(err)
		}
		return id
	}
	oldCopy, newest, corrupt := addCopy(1), addCopy(2), addCopy(3)
	if err := os.WriteFile(a.backupDir(corrupt, backupCopyRoot, "data", "test.json"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	plan := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
	if plan.Code != 200 || data(plan)["candidateCount"] != float64(2) ||
		!strings.Contains(plan.Raw, oldCopy) || !strings.Contains(plan.Raw, "versions/"+obsolete.Version) {
		t.Fatalf("incorrect cleanup plan: %s", plan.Raw)
	}
	run := call(t, "POST", "/api/broray/cleanup.cgi", "{\"token\":\""+data(plan)["token"].(string)+"\"}", brorayHdr)
	if run.Code != 200 || data(run)["deletedCount"] != float64(2) {
		t.Fatalf("cleanup failed: %s", run.Raw)
	}
	for _, v := range []string{active.Version, fallback.Version, foreign.Version} {
		if _, err := os.Stat(filepath.Join(root.Dir, "versions", v, "broray")); err != nil {
			t.Fatalf("protected version %s: %v", v, err)
		}
	}
	for _, id := range []string{newest, corrupt} {
		if _, err := os.Stat(a.backupDir(id)); err != nil {
			t.Fatalf("protected copy %s: %v", id, err)
		}
	}
	for _, path := range []string{a.backupDir(oldCopy), filepath.Join(root.Dir, "versions", obsolete.Version)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("obsolete path %s remains: %v", path, err)
		}
	}
	journal.Mode, journal.From, journal.PreviousTarget = "replace", active.Version, journal.NewTarget
	if err := compat.WriteJSON(root.UpdateJournal(), journal); err != nil {
		t.Fatal(err)
	}
	replacePlan := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
	if replacePlan.Code != 200 || data(replacePlan)["candidateCount"] != float64(1) ||
		!strings.Contains(replacePlan.Raw, "versions/"+fallback.Version) {
		t.Fatalf("obsolete generation after reinstall is missing: %s", replacePlan.Raw)
	}
}

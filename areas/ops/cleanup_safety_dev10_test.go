package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const cleanupTempOnly = `{"temp":true,"backups":false,"routeBackups":false,"logs":false}`

func TestCleanupDev10ParentSymlink(t *testing.T) {
	a := testArea(t)
	foreign := t.TempDir()
	marker := filepath.Join(foreign, "broray-foreign")
	writeFile(t, marker, []byte("foreign fixture"), 48*time.Hour)
	if err := os.Remove(a.d.Root.Tmp()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, a.d.Root.Tmp()); err != nil {
		t.Fatal(err)
	}
	r := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
	if r.Code == 200 && data(r)["candidateCount"] != float64(0) {
		t.Fatalf("foreign ancestor exposed as cleanup candidate: %s", r.Raw)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("foreign fixture changed", err)
	}
}

func TestCleanupDev10SameSizeReplacement(t *testing.T) {
	a := testArea(t)
	target := filepath.Join(a.d.Root.Tmp(), "broray-old")
	writeFile(t, target, []byte("owned"), 48*time.Hour)
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	r := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
	if r.Code != 200 || data(r)["candidateCount"] != float64(1) {
		t.Fatal(r.Raw)
	}
	token := data(r)["token"].(string)
	// Hold the old inode under a different, ineligible name; prevents inode reuse.
	if err := os.Rename(target, filepath.Join(a.d.Root.Tmp(), "held-original")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, []byte("other"), 0)
	if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	r = call(t, "POST", "/api/broray/cleanup.cgi", `{"token":"`+token+`"}`, brorayHdr)
	if r.Code != 409 {
		t.Errorf("stale identity accepted: %d %s", r.Code, r.Raw)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "other" {
		t.Errorf("replacement must survive: %q %v", b, err)
	}
}

func TestCleanupDev10PlanStoreSymlink(t *testing.T) {
	a := testArea(t)
	foreign := t.TempDir()
	dir := a.cleanupPlanDir()
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, dir); err != nil {
		t.Fatal(err)
	}
	r := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
	if r.Code == 200 {
		t.Errorf("symlinked plan-store accepted: %s", r.Raw)
	}
	entries, err := os.ReadDir(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("wrote %d plan files through foreign ancestor", len(entries))
	}
}

func TestCleanupDev10ParentChangedAfterPlan(t *testing.T) {
	a := testArea(t)
	path := filepath.Join(a.d.Root.Tmp(), "broray-old")
	writeFile(t, path, []byte("owned"), 48*time.Hour)
	r := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
	if r.Code != 200 || data(r)["candidateCount"] != float64(1) {
		t.Fatal(r.Raw)
	}
	token := data(r)["token"].(string)
	foreign := t.TempDir()
	marker := filepath.Join(foreign, "broray-old")
	writeFile(t, marker, []byte("other"), 48*time.Hour)
	held := filepath.Join(a.d.Root.Dir, "held-tmp")
	if err := os.Rename(a.d.Root.Tmp(), held); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, a.d.Root.Tmp()); err != nil {
		t.Fatal(err)
	}
	r = call(t, "POST", "/api/broray/cleanup.cgi", `{"token":"`+token+`"}`, brorayHdr)
	if r.Code != 409 {
		t.Fatal(r.Raw)
	}
	for _, p := range []string{marker, filepath.Join(held, "broray-old")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("fixture must survive: %s: %v", p, err)
		}
	}
}

func TestCleanupDev10DirectoryDoesNotFollowChild(t *testing.T) {
	a := testArea(t)
	foreign := t.TempDir()
	marker := filepath.Join(foreign, "keep")
	writeFile(t, marker, []byte("foreign"), 0)
	target := filepath.Join(a.d.Root.Dir, "data/routes/tmp/owned-old")
	writeFile(t, filepath.Join(target, "owned"), []byte("remove"), 0)
	if err := os.Symlink(foreign, filepath.Join(target, "foreign-link")); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(target, old, old); err != nil {
		t.Fatal(err)
	}
	r := call(t, "POST", "/api/broray/cleanup-plan.cgi", cleanupTempOnly, brorayHdr)
	if r.Code != 200 || data(r)["candidateCount"] != float64(1) {
		t.Fatal(r.Raw)
	}
	token := data(r)["token"].(string)
	r = call(t, "POST", "/api/broray/cleanup.cgi", `{"token":"`+token+`"}`, brorayHdr)
	if r.Code != 200 {
		t.Fatal(r.Raw)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("owned directory remains: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "foreign" {
		t.Fatalf("foreign child target changed: %q %v", data, err)
	}
}

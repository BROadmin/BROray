package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRouteUncheckedBackupRejectsForeignAncestor(t *testing.T) {
	base := t.TempDir()
	foreign := t.TempDir()
	root := filepath.Join(base, "routes")
	if err := os.Symlink(foreign, root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "state.json"), []byte("foreign state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b := backupFiles(filepath.Join(root, "state.json")); b != nil {
		t.Error("backup accepted data through a foreign ancestor")
	}
}

func TestRouteUncheckedBackupRejectsTargetSymlink(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	foreignFile := filepath.Join(foreign, "state.json")
	if err := os.WriteFile(foreignFile, []byte("foreign state"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "state.json")
	if err := os.Symlink(foreignFile, target); err != nil {
		t.Fatal(err)
	}
	if b := backupFiles(target); b != nil {
		t.Error("backup accepted data through a target symlink")
	}
}

func TestRouteUncheckedBackupPreservesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	b := backupFiles(path)
	if b == nil {
		t.Fatal("missing file incorrectly rejected")
	}
	data, ok := b.files[path]
	if !ok || data != nil {
		t.Errorf("missing file not recorded for rollback: present=%t data=%q", ok, data)
	}
}

func TestRouteDeleteRejectsSymlinkedBackupInputBeforeRouterMutation(t *testing.T) {
	env := newEnv(t)
	env.installBundle("telegram")
	token := env.preflightToken("telegram", "delete")
	before := env.r.routeCount("Proxy0")

	foreignFile := filepath.Join(t.TempDir(), "foreign-delete-result.json")
	const foreignContent = "foreign delete result"
	if err := os.WriteFile(foreignFile, []byte(foreignContent), 0o600); err != nil {
		t.Fatal(err)
	}
	resultFile := filepath.Join(env.e.catalogDir("telegram"), "router-delete-result.json")
	if _, err := os.Lstat(resultFile); !os.IsNotExist(err) {
		t.Fatalf("expected absent optional delete result before injection: %v", err)
	}
	if err := os.Symlink(foreignFile, resultFile); err != nil {
		t.Fatal(err)
	}

	env.wantErr(env.post("/api/routes/delete.cgi?bundleId=telegram&preflightToken="+token), 502, "ROUTES_DELETE_FAILED")
	if got := env.r.routeCount("Proxy0"); got != before {
		t.Errorf("delete changed router routes after backup input failure: before=%d after=%d", before, got)
	}
	if got, err := os.ReadFile(foreignFile); err != nil || string(got) != foreignContent {
		t.Errorf("delete changed foreign file: content=%q error=%v", got, err)
	}
	if link, err := os.Readlink(resultFile); err != nil || link != foreignFile {
		t.Errorf("delete replaced foreign symlink: link=%q error=%v", link, err)
	}
}

func TestRouteExportRejectsSymlinkedResumeBackupBeforeRouterMutation(t *testing.T) {
	env := newEnv(t)
	env.mustOK(env.post("/api/routes/check.cgi?bundleId=telegram"))
	env.mustOK(env.post("/api/routes/download.cgi?bundleId=telegram"))
	token := env.preflightToken("telegram", "export")
	before := env.r.routeCount("Proxy0")

	foreignFile := filepath.Join(t.TempDir(), "foreign-counter")
	const foreignContent = "foreign counter"
	if err := os.WriteFile(foreignFile, []byte(foreignContent), 0o600); err != nil {
		t.Fatal(err)
	}
	counterFile := env.e.counterFile("telegram")
	if _, err := os.Lstat(counterFile); !os.IsNotExist(err) {
		t.Fatalf("expected absent counter before injection: %v", err)
	}
	if err := os.Symlink(foreignFile, counterFile); err != nil {
		t.Fatal(err)
	}

	env.wantErr(env.post("/api/routes/export.cgi?bundleId=telegram&preflightToken="+token), 502, "ROUTES_EXPORT_FAILED")
	if got := env.r.routeCount("Proxy0"); got != before {
		t.Errorf("export changed router routes after backup input failure: before=%d after=%d", before, got)
	}
	if got, err := os.ReadFile(foreignFile); err != nil || string(got) != foreignContent {
		t.Errorf("export changed foreign file: content=%q error=%v", got, err)
	}
	if link, err := os.Readlink(counterFile); err != nil || link != foreignFile {
		t.Errorf("export replaced foreign symlink: link=%q error=%v", link, err)
	}
}

func TestRouteCustomCommitRejectsSymlinkedBackupInputBeforeLocalMutation(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Fault", [2]string{"fault.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	beforeBundles, err := os.ReadFile(env.e.bundlesFile())
	if err != nil {
		t.Fatal(err)
	}
	beforeCustom, err := os.ReadFile(env.e.customFile())
	if err != nil {
		t.Fatal(err)
	}

	foreignFile := filepath.Join(t.TempDir(), "foreign-state.json")
	const foreignContent = "foreign state"
	if err := os.WriteFile(foreignFile, []byte(foreignContent), 0o600); err != nil {
		t.Fatal(err)
	}
	stateFile := env.e.stateFile(id)
	if _, err := os.Lstat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("expected absent state before injection: %v", err)
	}
	if err := os.Symlink(foreignFile, stateFile); err != nil {
		t.Fatal(err)
	}

	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "Fault", "bundleId": nil}), 400, "COMMIT_FAILED")
	if env.e.bundleInList(id) || env.e.userBundleExists(id) || fileExists(env.e.catalogDir(id)) {
		t.Error("custom commit published bundle after backup input failure")
	}
	if got, err := os.ReadFile(env.e.bundlesFile()); err != nil || string(got) != string(beforeBundles) {
		t.Errorf("custom commit changed bundle index: error=%v", err)
	}
	if got, err := os.ReadFile(env.e.customFile()); err != nil || string(got) != string(beforeCustom) {
		t.Errorf("custom commit changed custom index: error=%v", err)
	}
	if got, err := os.ReadFile(foreignFile); err != nil || string(got) != foreignContent {
		t.Errorf("custom commit changed foreign file: content=%q error=%v", got, err)
	}
	if link, err := os.Readlink(stateFile); err != nil || link != foreignFile {
		t.Errorf("custom commit replaced foreign symlink: link=%q error=%v", link, err)
	}
}

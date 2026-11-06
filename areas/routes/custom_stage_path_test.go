package routes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func TestRouteCustomCommitPreservesForeignStageOnCatalogSwap(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Stage", [2]string{"stage.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	catalog := filepath.Dir(env.e.catalogDir(id))
	held := catalog + ".held"
	foreign := t.TempDir()
	foreignStage := filepath.Join(foreign, id+".new."+itoa(os.Getpid()))
	if err := os.Mkdir(foreignStage, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(foreignStage, "foreign.txt")
	const content = "foreign stage must remain"
	if err := os.WriteFile(sentinel, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(catalog, held); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, catalog); err != nil {
		t.Fatal(err)
	}

	// The nofollow runtime check refuses the swapped catalog before commit starts.
	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "Stage", "bundleId": nil}), 500, "RUNTIME_PREPARE_FAILED")
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != content {
		t.Errorf("commit deleted foreign stage: content=%q error=%v", got, err)
	}
	if env.e.bundleInList(id) || env.e.userBundleExists(id) {
		t.Error("commit published bundle after catalog parent swap")
	}
}

func TestRouteOwnedStageCleanupRejectsSymlinkTarget(t *testing.T) {
	parent := t.TempDir()
	foreign := t.TempDir()
	sentinel := filepath.Join(foreign, "keep.txt")
	const content = "foreign data"
	if err := os.WriteFile(sentinel, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, "stage")
	if err := os.Symlink(foreign, stage); err != nil {
		t.Fatal(err)
	}
	directory, err := platform.OpenDirNoFollow(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := platform.RemoveDirTreeAtNoFollow(directory, "stage"); err == nil {
		t.Error("cleanup accepted symlinked stage")
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != content {
		t.Errorf("cleanup changed foreign bytes: content=%q error=%v", got, err)
	}
	if link, err := os.Readlink(stage); err != nil || link != foreign {
		t.Errorf("cleanup changed stage link: link=%q error=%v", link, err)
	}
}

func TestRouteOwnedStageCleanupRemovesNestedTree(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	source := filepath.Join(stage, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "01.bat"), []byte("route add 1.2.3.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := platform.OpenDirNoFollow(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := platform.RemoveDirTreeAtNoFollow(directory, "stage"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Errorf("owned stage remains after cleanup: %v", err)
	}
}

func TestRouteCustomCommitPreservesOccupiedLegacyBackup(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Backup", [2]string{"backup.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	legacyBackup := env.e.catalogDir(id) + ".bak." + itoa(os.Getpid())
	if err := os.Mkdir(legacyBackup, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(legacyBackup, "foreign.txt")
	const content = "unrelated backup must remain"
	if err := os.WriteFile(sentinel, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": token, "name": "Backup", "bundleId": nil}), 400, "COMMIT_FAILED")
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != content {
		t.Errorf("commit deleted occupied legacy backup: content=%q error=%v", got, err)
	}
	if env.e.bundleInList(id) {
		t.Error("commit published bundle despite occupied legacy backup")
	}
}

func TestRouteCustomReplacementRejectsOccupiedLegacyBackup(t *testing.T) {
	env := newEnv(t)
	first := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Original", [2]string{"original.bat", discordBat})))
	created := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": first["token"], "name": "Original", "bundleId": nil}))
	id := created["bundleId"].(string)
	original, err := os.ReadFile(env.e.manifestFile(id))
	if err != nil {
		t.Fatal(err)
	}
	second := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Replacement", [2]string{"replacement.bat", "route add 5.5.5.0 mask 255.255.255.0 0.0.0.0"})))
	legacyBackup := env.e.catalogDir(id) + ".bak." + itoa(os.Getpid())
	if err := os.Mkdir(legacyBackup, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(legacyBackup, "foreign.txt")
	const content = "occupied backup"
	if err := os.WriteFile(sentinel, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	env.wantErr(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": second["token"], "name": "Replacement", "bundleId": id}), 400, "COMMIT_FAILED")
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != content {
		t.Errorf("replacement deleted occupied backup: content=%q error=%v", got, err)
	}
	if got, err := os.ReadFile(env.e.manifestFile(id)); err != nil || string(got) != string(original) {
		t.Errorf("replacement changed previous bundle manifest: error=%v", err)
	}
}

func TestRouteCustomReplacementRestoresAfterLocalWriteFailure(t *testing.T) {
	env := newEnv(t)
	first := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Original", [2]string{"original.bat", discordBat})))
	created := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": first["token"], "name": "Original", "bundleId": nil}))
	id := created["bundleId"].(string)
	manifestBefore, err := os.ReadFile(env.e.manifestFile(id))
	if err != nil {
		t.Fatal(err)
	}
	catalogBefore, err := os.ReadFile(filepath.Join(env.e.catalogDir(id), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	second := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Replacement", [2]string{"replacement.bat", "route add 5.5.5.0 mask 255.255.255.0 0.0.0.0"})))
	writer := func(path string, value any) error {
		if path == env.e.stateFile(id) {
			return errors.New("injected local write failure")
		}
		return writeObj(path, value)
	}
	_, aerr := env.e.userCommitAsWithWriter(obj{"token": second["token"], "name": "Replacement", "bundleId": id}, "", writer)
	if aerr == nil || aerr.Code != "COMMIT_FAILED" {
		t.Fatalf("expected commit failure after injected local write: %v", aerr)
	}
	if got, err := os.ReadFile(env.e.manifestFile(id)); err != nil || string(got) != string(manifestBefore) {
		t.Errorf("rollback did not restore local manifest: error=%v", err)
	}
	if got, err := os.ReadFile(filepath.Join(env.e.catalogDir(id), "manifest.json")); err != nil || string(got) != string(catalogBefore) {
		t.Errorf("rollback did not restore catalog manifest: error=%v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(env.e.catalogDir(id)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), id+".bak.") || strings.HasPrefix(entry.Name(), id+".new.") {
			t.Errorf("rollback left temporary catalog entry: %s", entry.Name())
		}
	}
}

func TestRouteCustomReplacementRejectsSwappedBackupDuringRollback(t *testing.T) {
	env := newEnv(t)
	first := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Original", [2]string{"original.bat", discordBat})))
	created := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": first["token"], "name": "Original", "bundleId": nil}))
	id := created["bundleId"].(string)
	second := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Replacement", [2]string{"replacement.bat", "route add 5.5.5.0 mask 255.255.255.0 0.0.0.0"})))
	foreign := t.TempDir()
	sentinel := filepath.Join(foreign, "keep.txt")
	const content = "foreign backup target"
	if err := os.WriteFile(sentinel, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	heldOriginal := filepath.Join(t.TempDir(), "held-original")
	swapped := false
	writer := func(path string, value any) error {
		if path != env.e.stateFile(id) {
			return writeObj(path, value)
		}
		catalog := filepath.Dir(env.e.catalogDir(id))
		entries, err := os.ReadDir(catalog)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), id+".bak.") {
				continue
			}
			backup := filepath.Join(catalog, entry.Name())
			if err := os.Rename(backup, heldOriginal); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(foreign, backup); err != nil {
				t.Fatal(err)
			}
			swapped = true
			break
		}
		if !swapped {
			t.Fatal("replacement backup was not found for fault injection")
		}
		return errors.New("injected local write failure after backup swap")
	}
	_, aerr := env.e.userCommitAsWithWriter(obj{"token": second["token"], "name": "Replacement", "bundleId": id}, "", writer)
	if aerr == nil || aerr.Code != "COMMIT_FAILED" || !strings.Contains(aerr.Message, "не подтверждено") {
		t.Fatalf("swapped backup must leave recovery unconfirmed: %v", aerr)
	}
	if info, err := os.Lstat(env.e.catalogDir(id)); err == nil && info.Mode()&os.ModeSymlink != 0 {
		t.Error("rollback installed foreign backup symlink as active catalog")
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != content {
		t.Errorf("rollback changed foreign bytes: content=%q error=%v", got, err)
	}
	if info, err := os.Lstat(heldOriginal); err != nil || !info.IsDir() {
		t.Errorf("original catalog not preserved for recovery: info=%v error=%v", info, err)
	}
}

func TestRouteCustomCommitRejectsRealCatalogParentSwapAfterStageOpen(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Parent swap", [2]string{"routes.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	catalog := filepath.Dir(env.e.catalogDir(id))
	held := catalog + ".held"
	foreignStage := ""
	const marker = "foreign stage is not owned by BROray"
	hook := func(stageName string) {
		if err := os.Rename(catalog, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(catalog, 0o700); err != nil {
			t.Fatal(err)
		}
		foreignStage = filepath.Join(catalog, stageName)
		if err := os.Mkdir(foreignStage, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(foreignStage, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, aerr := env.e.userCommitAsWithWriter(obj{"token": token, "name": "Parent swap", "bundleId": nil}, "", writeObj, hook)
	if aerr == nil || aerr.Code != "COMMIT_FAILED" {
		t.Errorf("commit must refuse changed catalog parent: %v", aerr)
	}
	if foreignStage == "" {
		t.Fatal("stage hook was not called")
	}
	entries, err := os.ReadDir(foreignStage)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "foreign.txt" {
		t.Errorf("commit wrote into foreign stage: entries=%v", entries)
	}
	if got, err := os.ReadFile(filepath.Join(foreignStage, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("foreign marker changed: content=%q error=%v", got, err)
	}
	if env.e.bundleInList(id) || env.e.userBundleExists(id) {
		t.Error("commit published bundle after catalog parent replacement")
	}
}

func TestRouteCustomCommitRejectsRealCatalogParentSwapDuringLocalWrite(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Write swap", [2]string{"routes.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	catalog := filepath.Dir(env.e.catalogDir(id))
	held := catalog + ".held"
	swapped := false
	writer := func(path string, value any) error {
		if !swapped {
			if err := os.Rename(catalog, held); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(catalog, 0o700); err != nil {
				t.Fatal(err)
			}
			swapped = true
		}
		return writeObj(path, value)
	}
	_, aerr := env.e.userCommitAsWithWriter(obj{"token": token, "name": "Write swap", "bundleId": nil}, "", writer)
	if !swapped {
		t.Fatal("local writer did not run")
	}
	if aerr == nil || aerr.Code != "COMMIT_FAILED" {
		t.Errorf("commit must refuse catalog parent swap during local write: %v", aerr)
	}
	if env.e.bundleInList(id) || env.e.userBundleExists(id) {
		t.Error("commit published local metadata for a catalog that is no longer at its configured path")
	}
	entries, err := os.ReadDir(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("commit wrote into replacement catalog parent: entries=%v", entries)
	}
}

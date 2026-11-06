package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomCommitPreservesForeignStageEntryOnSwap(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Entry swap", [2]string{"routes.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	catalog := filepath.Dir(env.e.catalogDir(id))
	var foreignStage, heldStage string
	const marker = "foreign stage must survive failed commit"
	hook := func(stageName string) {
		foreignStage = filepath.Join(catalog, stageName)
		heldStage = foreignStage + ".held"
		if err := os.Rename(foreignStage, heldStage); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(foreignStage, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(foreignStage, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": token, "name": "Entry swap", "bundleId": nil}, "", writeObj, hook)
	if apiErr == nil || apiErr.Code != "COMMIT_FAILED" {
		t.Errorf("commit must reject stage entry swap: %v", apiErr)
	}
	if foreignStage == "" {
		t.Fatal("stage hook did not run")
	}
	if got, err := os.ReadFile(filepath.Join(foreignStage, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("commit touched foreign stage entry: content=%q error=%v", got, err)
	}
	if info, err := os.Stat(heldStage); err != nil || !info.IsDir() {
		t.Errorf("owned stage missing for recovery: info=%v error=%v", info, err)
	}
	if env.e.bundleInList(id) || env.e.userBundleExists(id) {
		t.Error("commit published bundle after stage entry swap")
	}
}

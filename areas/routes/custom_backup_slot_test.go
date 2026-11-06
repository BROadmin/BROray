package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomReplacementPreservesLateForeignBackupSlot(t *testing.T) {
	env := newEnv(t)
	first := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Original", [2]string{"original.bat", discordBat})))
	created := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": first["token"], "name": "Original", "bundleId": nil}))
	id := created["bundleId"].(string)
	target := env.e.catalogDir(id)
	originalInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	second := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Replacement", [2]string{"replacement.bat", "route add 5.5.5.0 mask 255.255.255.0 0.0.0.0"})))
	var backup string
	var foreignInfo os.FileInfo
	hook := func(backupName string) {
		backup = filepath.Join(filepath.Dir(target), backupName)
		if err := os.Rename(backup, backup+".held"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(backup, 0o700); err != nil {
			t.Fatal(err)
		}
		foreignInfo, err = os.Stat(backup)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": second["token"], "name": "Replacement", "bundleId": id}, "", writeObj, nil, nil, nil, nil, nil, hook)
	if foreignInfo == nil {
		t.Fatal("backup move hook did not run")
	}
	if apiErr == nil || apiErr.Code != "COMMIT_FAILED" {
		t.Errorf("replacement must reject changed backup slot: %v", apiErr)
	}
	if info, err := os.Stat(backup); err != nil || !os.SameFile(foreignInfo, info) {
		t.Errorf("replacement overwrote foreign backup slot: old=%v current=%v error=%v", foreignInfo, info, err)
	}
	if info, err := os.Stat(target); err != nil || !os.SameFile(originalInfo, info) {
		t.Errorf("replacement moved original target after backup slot swap: old=%v current=%v error=%v", originalInfo, info, err)
	}
}

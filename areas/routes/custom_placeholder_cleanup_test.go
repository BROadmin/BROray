package routes

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomFailedCommitCleanupPreservesForeignPlaceholder(t *testing.T) {
	env := newEnv(t)
	first := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Original", [2]string{"original.bat", discordBat})))
	created := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": first["token"], "name": "Original", "bundleId": nil}))
	id := created["bundleId"].(string)
	target := env.e.catalogDir(id)
	catalog := filepath.Dir(target)
	original, err := os.ReadFile(filepath.Join(target, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	second := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Replacement", [2]string{"replacement.bat", "route add 5.5.5.0 mask 255.255.255.0 0.0.0.0"})))
	heldTarget := target + ".held"
	var backup string
	const marker = "foreign placeholder must survive failed commit cleanup"
	targetHookRan := false
	targetHook := func(string) {
		targetHookRan = true
		if err := os.Rename(target, heldTarget); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cleanupHook := func(backupName string) {
		backup = filepath.Join(catalog, backupName)
		if err := os.Rename(backup, backup+".held"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(backup, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backup, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": second["token"], "name": "Replacement", "bundleId": id}, "", writeObj, nil, nil, nil, nil, nil, nil, targetHook, cleanupHook)
	if !targetHookRan {
		t.Fatal("pre-apply target hook did not run")
	}
	if backup == "" {
		t.Fatal("placeholder cleanup hook did not run")
	}
	if apiErr == nil || apiErr.Code != "COMMIT_FAILED" {
		t.Errorf("replacement must reject changed target: %v", apiErr)
	}
	if got, err := os.ReadFile(filepath.Join(backup, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("failed commit cleanup changed foreign placeholder: content=%q error=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(heldTarget, "manifest.json")); err != nil || !bytes.Equal(got, original) {
		t.Errorf("original target was not retained: content=%q error=%v", got, err)
	}
}

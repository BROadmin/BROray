package routes

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomReplacementCleanupPreservesForeignBackup(t *testing.T) {
	env := newEnv(t)
	first := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Original", [2]string{"original.bat", discordBat})))
	created := env.mustOK(env.call("POST", "/api/routes/custom-commit.cgi", map[string]any{"token": first["token"], "name": "Original", "bundleId": nil}))
	id := created["bundleId"].(string)
	target := env.e.catalogDir(id)
	original, err := os.ReadFile(filepath.Join(target, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	second := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Replacement", [2]string{"replacement.bat", "route add 5.5.5.0 mask 255.255.255.0 0.0.0.0"})))
	var backup, held string
	const marker = "foreign backup must survive cleanup"
	hook := func(backupName string) {
		backup = filepath.Join(filepath.Dir(target), backupName)
		held = backup + ".held"
		if err := os.Rename(backup, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(backup, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backup, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": second["token"], "name": "Replacement", "bundleId": id}, "", writeObj, nil, nil, nil, hook)
	if backup == "" {
		t.Fatal("backup cleanup hook did not run")
	}
	if apiErr != nil {
		t.Fatalf("replacement failed before backup cleanup: %v", apiErr)
	}
	if got, err := os.ReadFile(filepath.Join(backup, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("cleanup changed foreign backup: content=%q error=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(held, "manifest.json")); err != nil || !bytes.Equal(got, original) {
		t.Errorf("original backup was not retained: content=%q error=%v", got, err)
	}
}

package routes

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomStageOpenFailurePreservesForeignEntry(t *testing.T) {
	env := newEnv(t)
	prepared := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Stage", [2]string{"routes.bat", discordBat})))
	token := prepared["token"].(string)
	id := "user-" + token[:16]
	catalog := filepath.Dir(env.e.catalogDir(id))
	const marker = "foreign stage after failed open"
	var stage string
	hooks := make([]func(string), 10)
	hooks[8] = func(name string) {
		stage = filepath.Join(catalog, name)
		if err := os.Rename(stage, stage+".held"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stage, []byte("block open"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hooks[9] = func(string) {
		if err := os.Remove(stage); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(stage, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": token, "name": "Stage", "bundleId": nil}, "", writeObj, hooks...)
	if stage == "" {
		t.Fatal("stage pre-open hook did not run")
	}
	if apiErr == nil || apiErr.Code != "COMMIT_FAILED" {
		t.Errorf("stage open failure must reject commit: %v", apiErr)
	}
	if got, err := os.ReadFile(filepath.Join(stage, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("stage open failure cleanup removed foreign entry: content=%q error=%v", got, err)
	}
	if _, err := os.Stat(stage + ".held"); err != nil {
		t.Errorf("original stage not retained: %v", err)
	}
}

func TestRouteCustomBackupOpenFailurePreservesForeignEntry(t *testing.T) {
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
	const marker = "foreign backup after failed open"
	var backup string
	hooks := make([]func(string), 12)
	hooks[10] = func(name string) {
		backup = filepath.Join(catalog, name)
		if err := os.Rename(backup, backup+".held"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backup, []byte("block open"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hooks[11] = func(string) {
		if err := os.Remove(backup); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(backup, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backup, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": second["token"], "name": "Replacement", "bundleId": id}, "", writeObj, hooks...)
	if backup == "" {
		t.Fatal("backup pre-open hook did not run")
	}
	if apiErr == nil || apiErr.Code != "COMMIT_FAILED" {
		t.Errorf("backup open failure must reject commit: %v", apiErr)
	}
	if got, err := os.ReadFile(filepath.Join(backup, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("backup open failure cleanup removed foreign entry: content=%q error=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "manifest.json")); err != nil || !bytes.Equal(got, original) {
		t.Errorf("original target changed: content=%q error=%v", got, err)
	}
}

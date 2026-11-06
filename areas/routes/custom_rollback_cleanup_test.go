package routes

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouteCustomReplacementRollbackCleanupPreservesForeignTarget(t *testing.T) {
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
	const marker = "foreign target must survive rollback cleanup"
	held := target + ".held"
	var foreignInfo os.FileInfo
	hook := func(string) {
		if err := os.Rename(target, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
		foreignInfo, err = os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
	}
	failedWriter := func(string, any) error { return errors.New("forced local metadata failure") }
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": second["token"], "name": "Replacement", "bundleId": id}, "", failedWriter, nil, nil, nil, nil, hook)
	if foreignInfo == nil {
		t.Fatal("rollback cleanup hook did not run")
	}
	if apiErr == nil || apiErr.Code != "COMMIT_FAILED" {
		t.Errorf("replacement must report failed commit: %v", apiErr)
	}
	if info, err := os.Stat(target); err != nil || !os.SameFile(foreignInfo, info) {
		t.Errorf("rollback cleanup replaced foreign target: old=%v current=%v error=%v", foreignInfo, info, err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("rollback cleanup changed foreign marker: content=%q error=%v", got, err)
	}
	entries, err := os.ReadDir(catalog)
	if err != nil {
		t.Fatal(err)
	}
	backups := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), filepath.Base(target)+".bak.") {
			continue
		}
		backups++
		got, err := os.ReadFile(filepath.Join(catalog, entry.Name(), "manifest.json"))
		if err != nil || !bytes.Equal(got, original) {
			t.Errorf("original bundle not retained in backup: content=%q error=%v", got, err)
		}
	}
	if backups != 1 {
		t.Errorf("expected one retained original backup, got %d", backups)
	}
	if _, err := os.Stat(filepath.Join(held, "manifest.json")); err != nil {
		t.Errorf("published replacement was not retained for diagnosis: %v", err)
	}
}

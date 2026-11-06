package routes

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomCommitPreservesForeignPreviewAfterValidation(t *testing.T) {
	env := newEnv(t)
	prepared := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Snapshot", [2]string{"routes.bat", discordBat})))
	token := prepared["token"].(string)
	previewDir := filepath.Join(env.e.previewsDir(), token)
	held := previewDir + ".held"
	validated, err := os.ReadFile(filepath.Join(previewDir, "optimized.txt"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "foreign preview must survive committed cleanup"
	called := false
	afterValidation := func(string) {
		called = true
		if err := os.Rename(previewDir, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(previewDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(previewDir, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, apiErr := env.e.userCommitAsWithWriter(obj{"token": token, "name": "Snapshot", "bundleId": nil}, "", writeObj, nil, afterValidation)
	if !called {
		t.Fatal("post-validation preview hook did not run")
	}
	if apiErr != nil {
		t.Fatalf("commit of validated snapshot failed: %v", apiErr)
	}
	id := result["bundleId"].(string)
	committed, err := os.ReadFile(filepath.Join(env.e.catalogDir(id), "normalized.txt"))
	if err != nil || !bytes.Equal(committed, validated) {
		t.Errorf("commit did not use validated snapshot: bytes=%q error=%v", committed, err)
	}
	if got, err := os.ReadFile(filepath.Join(previewDir, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("commit cleanup removed foreign preview: content=%q error=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(held, "report.json")); err != nil {
		t.Errorf("owned preview was not retained for safe cleanup: %v", err)
	}
}

package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomPreviewRejectsSymlinkedSource(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			env := newEnv(t)
			preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Source link", [2]string{"routes.bat", discordBat})))
			token := preview["token"].(string)
			previewDir := filepath.Join(env.e.previewsDir(), token)
			sourceDir := filepath.Join(previewDir, "source")
			stored := filepath.Join(sourceDir, "01.bat")
			content, err := os.ReadFile(stored)
			if err != nil {
				t.Fatal(err)
			}
			foreignDir := t.TempDir()
			foreignFile := filepath.Join(foreignDir, "01.bat")
			if err := os.WriteFile(foreignFile, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(stored); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				if err := os.Symlink(foreignFile, stored); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(sourceDir); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreignDir, sourceDir); err != nil {
					t.Fatal(err)
				}
			}
			if env.e.previewValid(previewDir) {
				t.Error("preview accepted a source symlink to foreign bytes")
			}
			_, aerr := env.e.userCommitAs(obj{"token": token, "name": "Source link", "bundleId": nil}, "")
			if aerr == nil || aerr.Code != "PREVIEW_INVALID" {
				t.Errorf("commit must reject symlinked preview before staging: %v", aerr)
			}
			if got, err := os.ReadFile(foreignFile); err != nil || string(got) != string(content) {
				t.Errorf("foreign source changed: content=%q error=%v", got, err)
			}
		})
	}
}

func TestRoutePreviewValidRejectsTopFileSwapAfterPreflight(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Top file swap", [2]string{"routes.bat", discordBat})))
	dir := filepath.Join(env.e.previewsDir(), preview["token"].(string))
	target := filepath.Join(dir, "optimized.txt")
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "optimized.txt")
	if err := os.WriteFile(foreign, original, 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	hook := func() {
		called = true
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, target); err != nil {
			t.Fatal(err)
		}
	}
	if env.e.previewValidWithHook(dir, hook) {
		t.Error("preview accepted a symlink inserted after the regular-file preflight")
	}
	if !called {
		t.Fatal("preflight swap hook did not run")
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != string(original) {
		t.Errorf("foreign file changed: content=%q error=%v", got, err)
	}
}

func TestRouteCommitKeepsValidatedPreviewOnPostValidationChange(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Commit snapshot", [2]string{"routes.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	optimized := filepath.Join(env.e.previewsDir(), token, "optimized.txt")
	validated, err := os.ReadFile(optimized)
	if err != nil {
		t.Fatal(err)
	}
	const replacement = "198.51.100.0/24\n"
	if string(validated) == replacement {
		t.Fatal("test replacement matches validated preview")
	}
	called := false
	afterValidation := func(string) {
		called = true
		if err := os.WriteFile(optimized, []byte(replacement), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, aerr := env.e.userCommitAsWithWriter(obj{"token": token, "name": "Commit snapshot", "bundleId": nil}, "", writeObj, nil, afterValidation)
	if !called {
		t.Fatal("postvalidation change hook did not run")
	}
	if aerr != nil {
		t.Fatalf("commit of validated bytes failed: %v", aerr)
	}
	committed, err := os.ReadFile(filepath.Join(env.e.catalogDir(id), "normalized.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(committed) != string(validated) {
		t.Errorf("commit used changed preview bytes: got=%q validated=%q", committed, validated)
	}
}

func TestRouteCleanupPreviewsPreservesForeignSymlinkedParent(t *testing.T) {
	env := newEnv(t)
	env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Own preview", [2]string{"routes.bat", discordBat})))
	root := env.e.previewsDir()
	held := root + ".held"
	foreign := t.TempDir()
	stale := filepath.Join(foreign, "foreign-stale")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(stale, "keep.txt")
	if err := os.WriteFile(marker, []byte("foreign preview"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, held); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, root); err != nil {
		t.Fatal(err)
	}
	env.e.cleanupPreviews()
	if got, err := os.ReadFile(marker); err != nil || string(got) != "foreign preview" {
		t.Errorf("cleanup removed foreign preview: content=%q error=%v", got, err)
	}
}

func TestRouteCleanupPreviewsRemovesExpiredOwnedPreview(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Expired", [2]string{"routes.bat", discordBat})))
	dir := filepath.Join(env.e.previewsDir(), preview["token"].(string))
	reportPath := filepath.Join(dir, "report.json")
	report, ok := readObj(reportPath)
	if !ok {
		t.Fatal("own preview report missing")
	}
	report["createdEpoch"] = nowFn().Unix() - userPreviewTTL - 10
	if err := writeObj(reportPath, report); err != nil {
		t.Fatal(err)
	}
	env.e.cleanupPreviews()
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("expired owned preview remains: %v", err)
	}
}

func TestRouteCleanupPreviewsPreservesAmbiguousPreview(t *testing.T) {
	env := newEnv(t)
	env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Own preview", [2]string{"routes.bat", discordBat})))
	const unknownToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	unknown := filepath.Join(env.e.previewsDir(), unknownToken)
	if err := os.Mkdir(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(unknown, "keep.txt")
	if err := os.WriteFile(marker, []byte("ownership unknown"), 0o600); err != nil {
		t.Fatal(err)
	}
	env.e.cleanupPreviews()
	if got, err := os.ReadFile(marker); err != nil || string(got) != "ownership unknown" {
		t.Errorf("cleanup removed ambiguous preview: content=%q error=%v", got, err)
	}
}

func TestRouteUserPrepareRejectsSymlinkedTmp(t *testing.T) {
	env := newEnv(t)
	if aerr := env.e.userPrepareRuntime(); aerr != nil {
		t.Fatal(aerr)
	}
	tmp := env.e.p("tmp")
	held := tmp + ".held"
	foreign := t.TempDir()
	if err := os.Rename(tmp, held); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, tmp); err != nil {
		t.Fatal(err)
	}
	if aerr := env.e.userPrepareRuntime(); aerr == nil || aerr.Code != "RUNTIME_PREPARE_FAILED" {
		t.Errorf("runtime preparation must reject symlinked tmp: %v", aerr)
	}
	entries, err := os.ReadDir(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("runtime preparation created foreign entries: %v", entries)
	}
}

func TestRouteCustomPreviewPreservesForeignTokenOnRootSwap(t *testing.T) {
	env := newEnv(t)
	if aerr := env.e.userPrepareRuntime(); aerr != nil {
		t.Fatal(aerr)
	}
	root := env.e.previewsDir()
	held := root + ".held"
	foreign := t.TempDir()
	marker := ""
	hook := func(token string) {
		if err := os.Rename(root, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, root); err != nil {
			t.Fatal(err)
		}
		victim := filepath.Join(foreign, token)
		if err := os.Mkdir(victim, 0o700); err != nil {
			t.Fatal(err)
		}
		marker = filepath.Join(victim, "keep.txt")
		if err := os.WriteFile(marker, []byte("foreign token"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, aerr := env.e.userPreviewWithHook(previewBody("Root swap", [2]string{"routes.bat", discordBat}), hook)
	if marker == "" {
		t.Fatal("token hook did not run")
	}
	if aerr == nil {
		t.Error("preview accepted a replaced root")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "foreign token" {
		t.Errorf("preview changed foreign token directory: content=%q error=%v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(marker))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Errorf("preview wrote into foreign token directory: entries=%v", entries)
	}
}

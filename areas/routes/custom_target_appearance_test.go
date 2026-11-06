package routes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRouteCustomCommitPreservesLateForeignTarget(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Late target", [2]string{"routes.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	target := env.e.catalogDir(id)
	const marker = "foreign target must survive failed commit"
	created := false
	hook := func(string) {
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "foreign.txt"), []byte(marker), 0o600); err != nil {
			t.Fatal(err)
		}
		created = true
	}
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": token, "name": "Late target", "bundleId": nil}, "", writeObj, hook)
	if !created {
		t.Fatal("stage hook did not run")
	}
	if apiErr == nil || apiErr.Code != "COMMIT_FAILED" {
		t.Errorf("commit must refuse a target that appeared after new-bundle decision: %v", apiErr)
	}
	if got, err := os.ReadFile(filepath.Join(target, "foreign.txt")); err != nil || string(got) != marker {
		t.Errorf("commit changed foreign target: content=%q error=%v", got, err)
	}
	if env.e.bundleInList(id) || env.e.userBundleExists(id) {
		t.Error("commit published bundle over late foreign target")
	}
}

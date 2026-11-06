package routes

import (
	"os"
	"testing"
)

func TestRouteCustomCommitPreservesTargetAppearingAtRename(t *testing.T) {
	env := newEnv(t)
	preview := env.mustOK(env.call("POST", "/api/routes/custom-preview.cgi", previewBody("Rename target", [2]string{"routes.bat", discordBat})))
	token := preview["token"].(string)
	id := "user-" + token[:16]
	target := env.e.catalogDir(id)
	var foreignInfo os.FileInfo
	hook := func(string) {
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		var err error
		foreignInfo, err = os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, apiErr := env.e.userCommitAsWithWriter(obj{"token": token, "name": "Rename target", "bundleId": nil}, "", writeObj, nil, nil, hook)
	if foreignInfo == nil {
		t.Fatal("pre-rename hook did not run")
	}
	if apiErr == nil || apiErr.Code != "COMMIT_FAILED" {
		t.Errorf("commit must refuse a target that appeared at rename: %v", apiErr)
	}
	if info, err := os.Stat(target); err != nil || !os.SameFile(foreignInfo, info) {
		t.Errorf("commit replaced foreign target: old=%v current=%v error=%v", foreignInfo, info, err)
	}
	if env.e.bundleInList(id) || env.e.userBundleExists(id) {
		t.Error("commit published bundle over foreign target")
	}
}

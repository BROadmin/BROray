package ops

import (
	"testing"
)

func TestUninstallFullHandoffFailureKeepsPreservedCopies(t *testing.T) {
	a := testArea(t)
	f := &fakeUninstall{failAt: "handoff"}
	a.uninstall = f.hooks()
	r := call(t, "POST", "/api/broray/uninstall.cgi", `{"mode":"full","confirmation":"УДАЛИТЬ BROray ПОЛНОСТЬЮ"}`, brorayHdr)
	if r.Body["ok"] != true {
		t.Fatal(r.Raw)
	}
	st := pollStatus(t, func(s map[string]any) bool { return s["running"] == false })
	if st["state"] != "error" {
		t.Fatalf("want failed handoff, got %v", st)
	}
	if f.purged {
		t.Fatal("preserved copies were purged before the installer accepted handoff")
	}
}

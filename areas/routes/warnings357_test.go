package routes

import (
	"strings"
	"testing"
)

func TestWarnings357BroadPrefix(t *testing.T) {
	e := newEnv(t)
	r := e.mustOK(e.call("POST", "/api/routes/custom-preview.cgi", previewBody("synthetic broad", [2]string{"b.bat", "route add 64.0.0.0 mask 252.0.0.0 0.0.0.0\n"})))
	if r["broadRouteCount"] != float64(1) || !strings.Contains(r["warning"].(string), "/1–/8") {
		t.Fatal("broad network warning understates range", r["warning"])
	}
}

package dns

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

var settingsHeaders = map[string]string{"X-BROray-Request": "operations", "Origin": "http://192.168.1.1:8080", "Content-Type": "application/json"}

func TestEmptySelectionSavePreservesRouterAndAutomation(t *testing.T) {
	h := newHarness(t)
	h.k.dot = []string{"dns-proxy tls upstream 9.9.9.9 853 sni dns.quad9.net"}
	ids := map[string]any{"serverIds": []string{"google-primary"}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	if r := h.call("POST", "/api/routes/dot-apply.cgi", ids, nil); !r.Success {
		t.Fatal(r.Error)
	}
	if err := h.s.autoSave(true); err != nil {
		t.Fatal(err)
	}
	before, err := readMap(h.s.configPath())
	if err != nil {
		t.Fatal(err)
	}
	autoBefore, err := os.ReadFile(h.s.autoPath())
	if err != nil {
		t.Fatal(err)
	}
	live, commands := strings.Join(h.k.dot, "\n"), len(h.k.cmds())
	h.s.probe = func(_ context.Context, _ catEntry) probeResult {
		t.Fatal("empty save must not probe")
		return probeResult{}
	}
	r := h.call("POST", "/api/routes/dot-test.cgi", map[string]any{"serverIds": []string{}, "allowUntested": false}, nil)
	if !r.Success {
		t.Fatalf("empty choice must be saveable: %+v", r.Error)
	}
	reloaded := h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil)
	if !reloaded.Success || len(reloaded.Data["selectedIds"].([]any)) != 0 {
		t.Fatalf("empty choice lost on reload: %+v", reloaded)
	}
	after, err := readMap(h.s.configPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"managed", "effectiveIds"} {
		if !jsonEqual(before[key], after[key]) {
			t.Fatalf("empty choice changed %s", key)
		}
	}
	autoAfter, err := os.ReadFile(h.s.autoPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(autoBefore, autoAfter) || strings.Join(h.k.dot, "\n") != live || len(h.k.cmds()) != commands {
		t.Fatal("empty choice changed router or automation")
	}
	h.expectErr(h.call("POST", "/api/routes/dot-apply.cgi", map[string]any{"serverIds": []string{}}, nil), 400, "REQUEST_INVALID")
	if h.s.autoDue() {
		t.Fatal("empty saved selection scheduled probes")
	}
}

func TestAutoSettingsRevisionConflict(t *testing.T) {
	h := newHarness(t)
	if _, err := h.s.ensureFiles(); err != nil {
		t.Fatal(err)
	}
	view := h.s.autoView()
	rev, ok := view["revision"].(string)
	if !ok || len(rev) != 64 {
		t.Fatalf("missing confirmed settings revision: %v", view)
	}
	body := map[string]any{"enabled": true, "expectedRevision": rev}
	r := h.call("POST", "/api/routes/dot-auto-settings.cgi", body, settingsHeaders)
	if !r.Success || r.Data["enabled"] != true || r.Data["revision"] == rev {
		t.Fatalf("CAS save failed: %+v", r)
	}
	body["enabled"] = false
	h.expectErr(h.call("POST", "/api/routes/dot-auto-settings.cgi", body, settingsHeaders), 409, "DOT_SETTINGS_CHANGED")
	if enabled, valid := h.s.autoEnabled(); !enabled || !valid {
		t.Fatal("stale request changed saved toggle")
	}
	body["expectedRevision"] = r.Data["revision"]
	r = h.call("POST", "/api/routes/dot-auto-settings.cgi", body, settingsHeaders)
	if !r.Success || r.Data["enabled"] != false {
		t.Fatalf("disable failed: %+v", r)
	}
}

func TestAutoSettingsRejectsAmbiguousInput(t *testing.T) {
	for _, body := range []string{
		`{"enabled":true,"enabled":false}`, `{"Enabled":true}`, `{"enabled":null}`,
		`{"enabled":true,"expectedRevision":null}`, `{"enabled":true,"expectedrevision":"x"}`,
		`{"enabled":true,"extra":1}`, `{"enabled":true} {"enabled":false}`,
	} {
		t.Run(body, func(t *testing.T) {
			h := newHarness(t)
			if _, err := h.s.ensureFiles(); err != nil {
				t.Fatal(err)
			}
			r := h.call("POST", "/api/routes/dot-auto-settings.cgi", body, settingsHeaders)
			if r.Status != 400 || r.Success {
				t.Fatalf("ambiguous request accepted: %+v", r)
			}
			if enabled, valid := h.s.autoEnabled(); enabled || !valid {
				t.Fatal("invalid request changed setting")
			}
		})
	}
}

func TestAutoSettingsPreservesInvalidStoredFile(t *testing.T) {
	h := newHarness(t)
	if _, err := h.s.ensureFiles(); err != nil {
		t.Fatal(err)
	}
	invalid := []byte(`{"schemaVersion":1,"enabled":"broken"}`)
	if err := os.WriteFile(h.s.autoPath(), invalid, 0600); err != nil {
		t.Fatal(err)
	}
	r := h.call("POST", "/api/routes/dot-auto-settings.cgi", `{"enabled":false}`, settingsHeaders)
	if r.Success || r.Status != 500 {
		t.Fatalf("corrupt settings silently overwritten: %+v", r)
	}
	after, err := os.ReadFile(h.s.autoPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, invalid) {
		t.Fatal("corrupt original not preserved")
	}
}

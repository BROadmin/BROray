package dns

import (
	"context"
	"strings"
	"testing"
)

// Applying a selection is not permission to claim records created by somebody
// else. In particular, a later uninstall must leave selected external rows alone.
func TestApplyPreservesExternalDNSOnUninstall(t *testing.T) {
	h := newHarness(t)
	owned := map[string]any{"serverIds": []string{"google-primary", "cloudflare-primary"}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", owned, nil); !r.Success {
		t.Fatalf("test initial owned selection: %+v", r.Error)
	}
	if r := h.call("POST", "/api/routes/dot-apply.cgi", owned, nil); !r.Success {
		t.Fatalf("create initial owned records: %+v", r.Error)
	}

	// These two exact catalog matches are external, not BROray creations.
	foreign := []string{
		"dns-proxy tls upstream 94.140.14.14 853 sni dns.adguard-dns.com",
		"dns-proxy tls upstream 94.140.15.15 853 sni dns.adguard-dns.com",
	}
	h.k.mu.Lock()
	h.k.dot = append(h.k.dot, foreign...)
	h.k.mu.Unlock()
	selected := map[string]any{"serverIds": []string{
		"google-primary", "cloudflare-primary", "adguard-primary", "adguard-secondary",
	}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", selected, nil); !r.Success {
		t.Fatalf("test mixed selection: %+v", r.Error)
	}
	r := h.call("POST", "/api/routes/dot-apply.cgi", selected, nil)
	if !r.Success {
		t.Fatalf("apply mixed selection: %+v", r.Error)
	}
	managed := len(r.Data["managed"].([]any))
	if err := h.s.uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	h.k.mu.Lock()
	live := append([]string(nil), h.k.dot...)
	h.k.mu.Unlock()
	if strings.Join(live, "\n") != strings.Join(foreign, "\n") {
		t.Fatalf("apply claimed external DNS ownership: managed=%d; after uninstall got %v, want external rows %v", managed, live, foreign)
	}
	if managed != 2 {
		t.Fatalf("mixed selection claimed %d records; only the two BROray creations may be managed", managed)
	}
}

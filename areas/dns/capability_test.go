package dns

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type capabilityTransport struct {
	*fakeKeenetic
	version     string
	unavailable bool
	reads       int
	bounded     bool
}

func (k *capabilityTransport) Show(ctx context.Context, path string, out any) error {
	if path != "show/version" {
		return errors.New("unexpected show path")
	}
	k.reads++
	deadline, ok := ctx.Deadline()
	k.bounded = ok && time.Until(deadline) <= 3*time.Second
	if k.unavailable {
		return errors.New("unavailable")
	}
	return json.Unmarshal([]byte(k.version), out)
}

func capHarness(t *testing.T, version string, runtime bool) (*harness, *capabilityTransport) {
	h := newHarness(t)
	k := &capabilityTransport{fakeKeenetic: h.k, version: version}
	h.d.App.T = k
	if !runtime {
		h.k.runtimeFn = func([]string) []string { return []string{"proxy-status:", "    proxy-name: System"} }
	}
	return h, k
}

func TestDoTCapabilityInventory(t *testing.T) {
	for _, tc := range []struct {
		name, version, want string
		runtime             bool
	}{
		{"present", `{"release":"5.01.C.1","ndw":{"components":"base,dns-tls,ip6,ndns"}}`, "present", false},
		{"missing", `{"release":"5.01.C.1","ndw":{"components":"base,ip6,ndns,dns-https"}}`, "missing", false},
		{"firmware-is-not-proof", `{"release":"5.01.C.1"}`, "unknown", false},
		{"misplaced-components", `{"components":"base,dns-tls"}`, "unknown", false},
		{"empty", `{"ndw":{"components":""}}`, "unknown", false},
		{"null", `{"ndw":{"components":null}}`, "unknown", false},
		{"object", `{"ndw":{"components":{"dns-tls":true}}}`, "unknown", false},
		{"duplicate", `{"ndw":{"components":"base,base,dns-tls"}}`, "unknown", false},
		{"missing-base", `{"ndw":{"components":"ndns"}}`, "unknown", false},
		{"bad-token", `{"ndw":{"components":"base,dns-tls;reboot"}}`, "unknown", false},
		{"runtime-proof", `{}`, "present", true},
		{"conflict", `{"ndw":{"components":"base,ndns"}}`, "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, k := capHarness(t, tc.version, tc.runtime)
			r := h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil)
			if !r.Success {
				t.Fatal(r.Error)
			}
			cap, ok := r.Data["capabilities"].(map[string]any)
			if !ok || cap["dotComponent"] != tc.want {
				t.Fatalf("capabilities=%v want=%s", cap, tc.want)
			}
			if cap["ipv6DoT"] != "unknown" && tc.want != "missing" {
				t.Fatalf("no IPv6 proof: %v", cap)
			}
			if !k.bounded || k.reads != 1 || len(k.cmds()) != 0 {
				t.Fatalf("reads=%d bounded=%v cmds=%v", k.reads, k.bounded, k.cmds())
			}
			if tc.want == "missing" && r.Data["mutationAvailable"] != false {
				t.Fatal("missing component allows mutation")
			}
			if tc.want == "unknown" && r.Data["supported"] != nil {
				t.Fatalf("unknown falsely supported: %v", r.Data["supported"])
			}
		})
	}
}

func TestDoTCapabilityUnavailableAndIPv6Evidence(t *testing.T) {
	h, k := capHarness(t, `{}`, false)
	k.unavailable = true
	r := h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil)
	cap, _ := r.Data["capabilities"].(map[string]any)
	if cap["dotComponent"] != "unknown" {
		t.Fatalf("transport error is not missing: %v", cap)
	}
	h, k = capHarness(t, `{"release":"4.01.C.1","ndw":{"components":"base,dns-tls,ip6"}}`, true)
	k.dot = []string{"dns-proxy tls upstream 2001:4860:4860::8888 853 sni dns.google"}
	r = h.call("GET", "/api/routes/dot-status.cgi?force=1", nil, nil)
	cap, _ = r.Data["capabilities"].(map[string]any)
	if cap["ipv6DoT"] != "confirmed" || cap["firmware"] != "4.01.C.1" {
		t.Fatalf("runtime proof lost: %v", cap)
	}
}

func TestDoTMissingComponentPreventsApplyWithoutWrites(t *testing.T) {
	h, k := capHarness(t, `{"ndw":{"components":"base,ndns,ip6"}}`, false)
	req := map[string]any{"serverIds": []string{"google-primary"}}
	if r := h.call("POST", "/api/routes/dot-test.cgi", req, nil); !r.Success {
		t.Fatal(r.Error)
	}
	r := h.call("POST", "/api/routes/dot-apply.cgi", req, nil)
	if r.Success || r.Error.Code != "DOT_COMPONENT_MISSING" {
		t.Fatalf("wrong failure: %+v", r)
	}
	if len(k.cmds()) != 0 || exists(h.s.markerPath()) {
		t.Fatal("missing component caused mutation/transaction")
	}
}

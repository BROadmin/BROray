package xray

import (
	"strings"
	"testing"
)

// An accepted limitation must be visible without turning a failed protocol
// into an architecture-wide or physical-router compatibility claim.
func TestEvidence353Scope(t *testing.T) {
	for _, tc := range []struct {
		arch, sum string
		limited   bool
		hardware  bool
	}{
		{"arm64-v8a", "64ed6858982a7f5eb85d88b811089063bd4f17561f4c8f44e0f1dc8e6f6430be", false, true},
		{"arm32-v7a", "19c51e08a6562705fb1cb032a84fd87df21d7e27b194f035be7e754dbe3a97b3", false, false},
		{"mips32le", "793dc40029283eb9ccead188b05abf0d34b64aab16ad1f6eadd204963a54837b", true, false},
		{"mips32", "4ceb213ad7cd8c1fc9250bb886e730665cddb9dfe891291b09231a204f8ba6ef", true, false},
	} {
		t.Run(tc.arch, func(t *testing.T) {
			c := compatibilityForArch("v26.11.9", tc.sum, tc.arch)
			if tc.hardware && (c["status"] != "compatible" || c["label"] != "Совместима с BROray") {
				t.Fatalf("recorded KN-1811 TCP/UDP checks missing: %v", c)
			}
			if !tc.hardware && (c["status"] == "compatible" || c["label"] != "Проверка совместимости не завершена") {
				t.Fatalf("offline evidence must be recorded without a router compatibility claim: %v", c)
			}
			checks, _ := checksFor("v26.11.9", tc.arch, tc.sum)
			found := false
			for _, check := range checks {
				level, _ := check["level"].(string)
				if strings.HasPrefix(level, "router-") {
					if tc.hardware {
						if check["state"] != "passed" || !strings.Contains(check["where"].(string), "KN-1811") || !strings.Contains(check["receipt"].(string), "candidate1/artifacts/native-preflight/") {
							t.Fatalf("missing scoped hardware receipt: %v", check)
						}
					} else if check["state"] != "not-run" {
						t.Fatalf("unmeasured hardware evidence: %v", check)
					}
				}
				if tc.limited && level == "config" && check["state"] != "not-run" {
					t.Fatalf("MIPS must not imply complete configuration coverage: %v", check)
				}
				if level == "emulator" {
					found = true
					detail, _ := check["details"].(string)
					if !strings.Contains(detail, "Google Drive/WARP") || !strings.Contains(detail, "не проверены") {
						t.Fatalf("real-cloud limit missing: %v", check)
					}
					if tc.limited && (!strings.Contains(detail, "WireGuard") || !strings.Contains(detail, "сбой") || !strings.Contains(detail, "роутере")) {
						t.Fatalf("known MIPS limitation missing: %v", check)
					}
				}
			}
			if !found {
				t.Fatal("missing measured emulator scope")
			}
			if hasEvidence("v26.11.9", tc.arch, "00"+tc.sum[2:]) {
				t.Fatal("evidence leaked to changed archive")
			}
			if changed := compatibilityForArch("v26.11.9", "00"+tc.sum[2:], tc.arch); changed["status"] != "untested" {
				t.Fatal("changed archive inherited compatibility")
			}
		})
	}
}

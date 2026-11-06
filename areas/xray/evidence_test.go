package xray

import "testing"

// Owner decision 2026-11-02: per version and architecture BROray shows what
// was checked; «Совместима» only with config + TCP + UDP on that arch.
func TestEvidenceLevels(t *testing.T) {
	state := func(c map[string]any) map[string]string {
		out := map[string]string{}
		for _, ch := range c["checks"].([]map[string]any) {
			out[ch["level"].(string)] = ch["state"].(string)
		}
		return out
	}
	c := compatibilityForArch("v26.10.31", arm64Xray261031, "arm64-v8a")
	if c["status"] != "compatible" || c["label"] != "Совместима с BROray" {
		t.Fatalf("26.10.31 arm64: %v", c)
	}
	if s := state(c); s["config"] != "passed" || s["router-tcp"] != "passed" || s["router-udp"] != "passed" {
		t.Fatalf("26.10.31 checks: %v", s)
	}
	c = compatibilityForArch("v26.10.21", arm64Xray261021, "arm64-v8a")
	if c["status"] != "compatible" || c["label"] != "Совместима с BROray" {
		t.Fatalf("26.10.21 arm64 has a recorded KN-1811 UDP check: %v", c)
	}
	if s := state(c); s["config"] != "passed" || s["router-tcp"] != "passed" || s["router-udp"] != "passed" {
		t.Fatalf("26.10.21 checks: %v", s)
	}
	for _, trial := range []struct{ hash, arch string }{
		{"00" + arm64Xray261021[2:], "arm64-v8a"},
		{arm64Xray261021, "arm32-v7a"},
	} {
		other := compatibilityForArch("v26.10.21", trial.hash, trial.arch)
		if other["status"] == "compatible" || state(other)["router-udp"] == "passed" {
			t.Fatalf("26.10.21 router evidence escaped its archive/architecture: %v", other)
		}
	}
	c = compatibilityForArch("v26.9.30", "9886f077f9fd8e6713b84c377c1c7db4e53b9bfa8c276a5bd12561139522b473", "arm64-v8a")
	if c["label"] != "Проверка совместимости не завершена" || state(c)["router-tcp"] != "not-run" {
		t.Fatalf("pre-release VM-only: %v", c)
	}
	// ARM64 results are not carried to other architectures; armv7 has only
	// the informational emulator level (CODEX-REPORT-EMU)
	c = compatibilityForArch("v26.10.31", "2387f55239ee7799fd67d5382e79f589c1da07a97d993e86b058f2290b8e456b", "arm32-v7a")
	if s := state(c); c["status"] != "untested" || c["label"] != "Проверка совместимости не завершена" ||
		s["emulator"] != "passed" || s["config"] != "not-run" || s["router-tcp"] != "not-run" || s["router-udp"] != "not-run" {
		t.Fatalf("armv7: %v", c)
	}
	// a version/arch with no record at all
	c = compatibilityForArch("v27.1.1", "ab"+arm64Xray261031[2:], "mips32")
	if c["label"] != "Совместимость не проверялась" {
		t.Fatalf("unknown: %v", c)
	}
	// a different archive under the same tag is not covered
	c = compatibilityForArch("v26.10.31", "00"+arm64Xray261031[2:], "arm64-v8a")
	if c["status"] != "untested" || c["label"] != "Совместимость не проверялась" {
		t.Fatalf("other archive: %v", c)
	}
}

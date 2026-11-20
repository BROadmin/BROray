package servers

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestManagedDPICoreGate(t *testing.T) {
	rec := dpiContractRecord(t)
	dpiContractManaged(rec)
	if RequiredCore(rec) != "26.11.9" {
		t.Error("managed DPI core requirement missing")
	}
	for _, version := range []string{"", "25.1.1", "26.11.10", "27.1.1", "invalid"} {
		if err := CheckCoreSupport(rec, version); err == nil {
			t.Error("untested core accepted for managed DPI", version)
		}
	}
	if err := CheckCoreSupport(rec, "26.11.9"); err != nil {
		t.Fatal(err)
	}
}

func TestManagedDPICoreActivationAndPresentation(t *testing.T) {
	for _, version := range []string{"25.1.1", "26.11.10", "unknown"} {
		t.Run(version, func(t *testing.T) {
			e := newEnv(t)
			if err := os.WriteFile(e.d.Root.XrayBinary(), []byte("#!/bin/sh\necho 'Xray "+version+" (test core)'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			rec := dpiContractRecord(t)
			dpiContractManaged(rec)
			if err := WriteRecord(e.d, rec); err != nil {
				t.Fatal(err)
			}
			r := call(t, "POST", "/api/servers/activate.cgi", `{"id":"dpi-record"}`, nil)
			if r.Code != 400 || errCode(r) != "XRAY_DPI_CORE_UNVERIFIED" || len(e.applied) != 0 || ActiveID(e.d) != "" {
				t.Fatal("core gate bypassed before apply", r.Raw)
			}
			view := e.s.supportViewChecker(context.Background())(rec)
			if view["supported"] != false || view["code"] != "XRAY_DPI_CORE_UNVERIFIED" || !strings.Contains(asString(view["message"]), "26.11.9") || strings.Contains(asString(view["message"]), "новее") {
				t.Fatal("misleading minimum-version presentation", view)
			}
			called := false
			e.s.measure = func(context.Context, Record, []byte) probeResult { called = true; return probeResult{Success: true} }
			if _, err := e.s.checkServer(context.Background(), "dpi-record", "manual", ""); err == nil || called {
				t.Fatal("quality probe ignored exact core gate", err, called)
			}
		})
	}
}
func TestManagedDPICoreDoesNotChangeOrdinaryPolicy(t *testing.T) {
	rec := dpiContractRecord(t)
	for _, version := range []string{"", "25.1.1", "27.1.1"} {
		if err := CheckCoreSupport(rec, version); err != nil {
			t.Fatal("ordinary profile policy changed", err)
		}
	}
	dpiContractManaged(rec)
	if err := CheckCoreSupport(rec, "v26.11.9"); err != nil {
		t.Fatal(err)
	}
	rec["dpi"] = nil
	if err := CheckCoreSupport(rec, "26.11.9"); err == nil {
		t.Fatal("invalid marker accepted")
	}
}

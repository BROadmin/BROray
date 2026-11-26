package ops

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/keenetic"
)

type diagnosticVersionTransport struct {
	keenetic.Transport
	show func(context.Context, string, any) error
}

func (t diagnosticVersionTransport) Show(ctx context.Context, path string, out any) error {
	return t.show(ctx, path, out)
}

func TestReportDeviceAndRunningBuild(t *testing.T) {
	a := testArea(t)
	fixtures := []struct {
		name                  string
		response              map[string]any
		failure               error
		model, code, firmware any
	}{
		{"identified", map[string]any{"model": "Keenetic Peak (KN-2710)", "hw_id": "KN-2710",
			"release": "5.02.B.4.0-0", "title": "5.2", "serial": "SERIAL-FIXTURE-PRIVATE", "device": "private-device-tag"},
			nil, "Keenetic Peak (KN-2710)", "KN-2710", "5.02.B.4.0-0"},
		{"partial", map[string]any{"model": "Keenetic Giga (KN-1010)", "title": "4.2.3"}, nil, "Keenetic Giga (KN-1010)", "KN-1010", "4.2.3"},
		{"netcraze-hardware", map[string]any{"model": "Ultra", "hw_id": "NC-1812", "release": "5.02.B.4.0-0"}, nil, "Ultra", "NC-1812", "5.02.B.4.0-0"},
		{"netcraze-suffix", map[string]any{"model": "Ultra (NC-1812)", "release": "5.02.B.4.0-0"}, nil, "Ultra (NC-1812)", "NC-1812", "5.02.B.4.0-0"},
		{"model-family-conflict", map[string]any{"model": "Ultra (NC-1812)", "hw_id": "KN-1812"}, nil, nil, nil, nil},
		{"conflict", map[string]any{"hw_id": "KN-2710", "model": "Keenetic Giga (KN-1010)", "release": "bad://private-router-value"}, nil, nil, nil, nil},
		{"unavailable", nil, errors.New("https://private-upstream.example/diagnostic?password=hidden"), nil, nil, nil},
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			calls := 0
			tr := diagnosticVersionTransport{show: func(ctx context.Context, path string, out any) error {
				calls++
				deadline, bounded := ctx.Deadline()
				if path != "show/version" || !bounded || time.Until(deadline) > 3*time.Second {
					t.Fatal("unbounded or unexpected device read", path, bounded)
				}
				*out.(*map[string]any) = f.response
				return f.failure
			}}
			a.d.App = &app.App{K: keenetic.NewAdapter(tr, a.d.Root.Receipts(), 0)}
			result := call(t, "GET", "/api/operations/report.cgi", "", nil)
			platform, ok := result.Body["platform"].(map[string]any)
			if result.Code != 200 || !ok || calls != 1 {
				t.Fatal("report did not read router once", result.Code, calls)
			}
			for key, want := range map[string]any{"routerModel": f.model, "routerModelCode": f.code, "keeneticOS": f.firmware} {
				got, exists := platform[key]
				if !exists || got != want {
					t.Fatalf("%s: got %v (%v), want %v", key, got, exists, want)
				}
			}
			if platform["architecture"] == nil {
				t.Fatal("architecture missing")
			}
			build, ok := result.Body["build"].(map[string]any)
			expected := os.Getenv("BRORAY_REPORT_EXPECT_REVISION")
			revision, present := build["appRevision"]
			if !ok || !present || build["appVersion"] != app.Version ||
				(expected == "" && revision != nil) || (expected != "" && revision != expected) {
				t.Fatalf("wrong running build facts: %+v, expected revision %q", build, expected)
			}
			for _, private := range []string{"SERIAL-FIXTURE-PRIVATE", "private-device-tag", "private-router-value", "private-upstream.example", "password=hidden"} {
				if strings.Contains(result.Raw, private) {
					t.Fatal("private router data reached diagnostics")
				}
			}
		})
	}
}

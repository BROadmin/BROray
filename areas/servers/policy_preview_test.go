package servers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
	"golang.org/x/sys/unix"
)

func TestPolicyPreviewDoesNotApplyAndValidatesCore(t *testing.T) {
	e := newEnv(t)
	ctx := compat.WithFence(context.Background())
	if cfg, err := e.d.Svc.PreviewActiveConfig(ctx); err != nil || cfg != nil {
		t.Fatalf("inactive preview: %s %v", cfg, err)
	}
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	if _, err := e.s.activate(id); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(e.d.Root.XrayConfig())
	if err != nil {
		t.Fatal(err)
	}
	transformed := false
	e.d.Svc.RoutesConfigureXray = func(ctx context.Context, b []byte) ([]byte, error) {
		transformed = compat.FenceHeld(ctx)
		return b, nil
	}
	cfg, err := e.d.Svc.PreviewActiveConfig(ctx)
	if err != nil || len(cfg) == 0 || !transformed || len(e.applied) != 1 {
		t.Fatalf("preview: %s %v calls%d", cfg, err, len(e.applied))
	}
	e.s.xrayTest = func(context.Context, string) error { return errors.New("core rejected") }
	if _, err := e.d.Svc.PreviewActiveConfig(ctx); err == nil {
		t.Fatal("core rejection ignored")
	}
	after, err := os.ReadFile(e.d.Root.XrayConfig())
	if err != nil || !bytes.Equal(before, after) || len(e.applied) != 1 || ActiveID(e.d) != id {
		t.Fatal("preview mutated active connection")
	}
}

func TestPolicyPreviewRefusesAmbiguousActiveRecord(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "directory", "oversize", "invalid-id", "running-without-id"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			path := ActiveFile(e.d)
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink("missing-target", path)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "oversize":
				err = os.WriteFile(path, []byte(strings.Repeat("a", 257)), 0600)
			case "invalid-id":
				err = os.WriteFile(path, []byte("../foreign\n"), 0600)
			case "running-without-id":
				e.running.Store(true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.d.Svc.PreviewActiveConfig(compat.WithFence(context.Background())); err == nil {
				t.Fatal("ambiguous active state treated as inactive")
			}
			if len(e.applied) != 0 {
				t.Fatal("preview applied config")
			}
		})
	}
}

func TestPolicyPreviewWithOfficialCore(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("official core validation requires BRORAY_XRAY_BIN")
	}
	e := newEnv(t)
	id := e.addServer(t, "trojan://pw@a.example:443#A")
	if _, err := e.s.activate(id); err != nil {
		t.Fatal(err)
	}
	e.s.xrayTest = func(ctx context.Context, path string) error {
		out, err := exec.CommandContext(ctx, bin, "run", "-test", "-c", path).CombinedOutput()
		if err != nil {
			return errors.New(string(out))
		}
		return nil
	}
	e.d.Svc.RoutesConfigureXray = func(ctx context.Context, base []byte) ([]byte, error) {
		return routepolicy.Render(base, []routepolicy.Rule{{ID: "deny", Enabled: true, Action: routepolicy.Deny, Domains: []string{"ads.example"}}}, geodata.Snapshot{}, geodata.AssetDirectory, routepolicy.OutboundTags{VPN: "proxy", Direct: "broray-direct", Deny: "broray-deny"}, []string{"socks"}, "")
	}
	ctx := compat.WithFence(context.Background())
	if cfg, err := e.d.Svc.PreviewActiveConfig(ctx); err != nil || !bytes.Contains(cfg, []byte("broray-deny")) {
		t.Fatalf("official core preview failed: %v", err)
	}
	e.d.Svc.RoutesConfigureXray = func(context.Context, []byte) ([]byte, error) {
		return []byte(`{"inbounds":[{"protocol":"not-a-protocol"}]}`), nil
	}
	if _, err := e.d.Svc.PreviewActiveConfig(ctx); err == nil {
		t.Fatal("official core rejection ignored")
	}
	if len(e.applied) != 1 {
		t.Fatal("preview applied candidate config")
	}
}

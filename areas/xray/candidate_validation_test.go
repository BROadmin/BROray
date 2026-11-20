package xray

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
)

func TestCandidateValidationDoesNotApplyOrStart(t *testing.T) {
	h := newHarness(t, true)
	ctx := compat.WithFence(context.Background())
	if err := h.s.ValidateConfig(ctx, []byte(goodConfig)); err != nil {
		t.Fatal(err)
	}
	if err := h.s.ValidateConfig(ctx, []byte(`{"INVALID":true}`)); err == nil {
		t.Fatal("core failure ignored")
	}
	if err := h.s.ValidateConfig(context.Background(), []byte(goodConfig)); err == nil {
		t.Fatal("unfenced validation accepted")
	}
	got, err := os.ReadFile(h.d.Root.XrayConfig())
	if err != nil || string(got) != goodConfig || h.d.App.Xray.Running() {
		t.Fatal("validation changed active runtime")
	}
	entries, err := os.ReadDir(h.d.Root.Tmp())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) >= 10 && entry.Name()[:10] == "policy-cfg" {
			t.Fatal("validation scratch left behind")
		}
	}
}

// Diagnostic uses only the public synthetic fixture, never a user's profile.
func probeCandidateDescriptor(t *testing.T, bin string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	go func() { _, _ = w.Write([]byte(goodConfig)); w.Close() }()
	cmd := exec.Command(bin, "run", "-test", "-c", "/proc/self/fd/3", "-format", "json")
	cmd.ExtraFiles = []*os.File{r}
	out, err := cmd.CombinedOutput()
	r.Close()
	if len(out) > 8192 {
		out = out[:8192]
	}
	t.Logf("synthetic descriptor probe: %v %s", err, out)
}

func TestCandidateValidationOfficialCoreThroughOwnedDescriptor(t *testing.T) {
	bin := os.Getenv("BRORAY_XRAY_BIN")
	if bin == "" {
		t.Skip("official core fixture required")
	}
	h := newHarness(t, true)
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.d.Root.XrayBinary(), data, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := compat.WithFence(context.Background())
	if err := h.s.ValidateConfig(ctx, []byte(goodConfig)); err != nil {
		probeCandidateDescriptor(t, h.d.Root.XrayBinary())
		t.Fatal(err)
	}
	if err := h.s.ValidateConfig(ctx, []byte(`{"inbounds":[{"protocol":"not-a-protocol"}]}`)); err == nil {
		t.Fatal("real core rejection ignored")
	}
	got, err := os.ReadFile(h.d.Root.XrayConfig())
	if err != nil || string(got) != goodConfig || h.d.App.Xray.Running() {
		t.Fatal("validation applied/started config")
	}
}

func TestCandidateValidationDoesNotUseScratchRoot(t *testing.T) {
	h := newHarness(t, true)
	foreign := t.TempDir()
	if err := os.Rename(h.d.Root.Tmp(), filepath.Join(h.d.Root.Dir, "tmp-held")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, h.d.Root.Tmp()); err != nil {
		t.Fatal(err)
	}
	if err := h.s.ValidateConfig(compat.WithFence(context.Background()), []byte(goodConfig)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(foreign)
	if err != nil || len(entries) != 0 {
		t.Fatal("foreign directory modified")
	}
}

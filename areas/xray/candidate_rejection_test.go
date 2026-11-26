package xray

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
)

func TestCandidateRejectionClassification(t *testing.T) {
	for _, kind := range []string{"official-rejected", "official-valid", "cancelled", "signal", "launch", "short-input", "symlink", "core-drift", "other-exit"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t, true)
			ctx, cancel := context.WithTimeout(compat.WithFence(context.Background()), 10*time.Second)
			defer cancel()
			binary := h.d.Root.XrayBinary()
			config := []byte(goodConfig)
			if strings.HasPrefix(kind, "official-") {
				fixture := os.Getenv("BRORAY_XRAY_BIN")
				if fixture == "" {
					t.Fatal("official core fixture required")
				}
				raw, err := os.ReadFile(fixture)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(binary, raw, 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "official-rejected" {
					config = []byte(`{"inbounds":[{"protocol":"not-a-protocol"}]}`)
				}
			} else {
				script := "#!/bin/sh\ncat >/dev/null\nexit 23\n"
				switch kind {
				case "cancelled":
					cancel()
				case "signal":
					script = "#!/bin/sh\ncat >/dev/null\nkill -TERM $$\n"
				case "launch":
					script = "#!/missing-interpreter\n"
				case "short-input":
					script = "#!/bin/sh\nexit 23\n"
					config = []byte(strings.Repeat("x", 1<<20))
				case "other-exit":
					script = "#!/bin/sh\ncat >/dev/null\nexit 2\n"
				case "core-drift":
					ready := filepath.Join(h.d.Root.Dir, "validation-ready")
					script = "#!/bin/sh\ncat >/dev/null\necho ready > '" + ready + "'\nsleep 1\nexit 23\n"
					done := make(chan error, 1)
					go func() {
						for {
							if _, err := os.Stat(ready); err == nil {
								done <- os.Rename(binary, binary+".held")
								return
							}
							select {
							case <-ctx.Done():
								done <- ctx.Err()
								return
							case <-time.After(10 * time.Millisecond):
							}
						}
					}()
					defer func() {
						if err := <-done; err != nil {
							t.Error(err)
						}
					}()
				}
				if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					target := binary + ".target"
					if err := os.Rename(binary, target); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, binary); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := h.s.ValidateConfig(ctx, config)
			rejected := errors.Is(err, compat.ErrXrayCandidateRejected)
			if rejected != (kind == "official-rejected") {
				t.Fatalf("wrong rejection classification kind=%s error=%v", kind, err)
			}
			if kind == "official-valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("operational failure/rejection lost")
			}
			raw, e := os.ReadFile(h.d.Root.XrayConfig())
			if e != nil || string(raw) != goodConfig || h.d.App.Xray.Running() {
				t.Fatal("validation changed active runtime", e)
			}
		})
	}
}

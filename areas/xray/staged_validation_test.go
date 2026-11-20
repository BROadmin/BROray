package xray

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

type stagedConfigValidator interface {
	ValidateStagedConfig(context.Context, []byte, *os.File) error
}

func stagedValidator(t *testing.T, s *service) stagedConfigValidator {
	t.Helper()
	v, ok := any(s).(stagedConfigValidator)
	if !ok {
		t.Fatal("protected staged-asset validator missing")
	}
	return v
}

const stagedGeoConfig = `{"outbounds":[{"tag":"deny","protocol":"blackhole"}],"routing":{"rules":[{"type":"field","ip":["ext:fi.dat:fi"],"outboundTag":"deny"}]}}`

func TestStagedAssetValidationOfficialCore(t *testing.T) {
	h := newHarness(t, true)
	core := os.Getenv("BRORAY_XRAY_BIN")
	if core == "" {
		t.Fatal("pinned core required")
	}
	raw, err := os.ReadFile(core)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(h.d.Root.XrayBinary(), raw, 0755); err != nil {
		t.Fatal(err)
	}
	assets := t.TempDir()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(core), "fi.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(assets, "fi.dat"), data, 0600); err != nil {
		t.Fatal(err)
	}
	dir, err := platform.OpenDirNoFollow(assets)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	ctx := compat.WithFence(context.Background())
	if err = h.s.ValidateConfig(ctx, []byte(stagedGeoConfig)); err == nil {
		t.Fatal("control unexpectedly found data in live runtime")
	}
	if err = stagedValidator(t, h.s).ValidateStagedConfig(ctx, []byte(stagedGeoConfig), dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(h.d.Root.XrayConfig())
	if err != nil || string(got) != goodConfig || h.d.App.Xray.Running() {
		t.Fatal("validator mutated main", err)
	}
	if _, err = os.Stat(filepath.Join(h.d.Root.Runtime(), "fi.dat")); !os.IsNotExist(err) {
		t.Fatal("validator copied live asset", err)
	}
	if _, err = dir.Stat(); err != nil {
		t.Fatal("validator closed caller descriptor", err)
	}
	after, err := os.ReadFile(filepath.Join(assets, "fi.dat"))
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("validator changed staged data", err)
	}
}
func TestStagedAssetValidationRefusesAmbiguousDirectory(t *testing.T) {
	h := newHarness(t, true)
	api := stagedValidator(t, h.s)
	for _, kind := range []string{"nil", "file", "closed", "replaced", "linked", "unfenced", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir, err := platform.OpenDirNoFollow(root)
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			ctx := compat.WithFence(context.Background())
			switch kind {
			case "nil":
				dir = nil
			case "file":
				p := filepath.Join(root, "file")
				if err = os.WriteFile(p, []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
				f, e := os.Open(p)
				if e != nil {
					t.Fatal(e)
				}
				defer f.Close()
				dir = f
			case "closed":
				dir.Close()
			case "replaced", "linked":
				if err = os.Rename(root, root+"-old"); err != nil {
					t.Fatal(err)
				}
				if kind == "linked" {
					err = os.Symlink(root+"-old", root)
				} else {
					err = os.Mkdir(root, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "unfenced":
				ctx = context.Background()
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if err = api.ValidateStagedConfig(ctx, []byte(goodConfig), dir); err == nil {
				t.Fatal("ambiguous/unfenced staged assets accepted")
			}
		})
	}
}
func TestStagedAssetValidationDetectsRebindDuringChild(t *testing.T) {
	h := newHarness(t, true)
	api := stagedValidator(t, h.s)
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.Mkdir(assets, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := platform.OpenDirNoFollow(assets)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	started, release := filepath.Join(root, "started"), filepath.Join(root, "release")
	script := "#!/bin/sh\ncat /proc/self/fd/3 >/dev/null\n: > '" + started + "'\nwhile [ ! -f '" + release + "' ]; do sleep 1; done\nexit 0\n"
	if err = os.WriteFile(h.d.Root.XrayBinary(), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(compat.WithFence(context.Background()), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- api.ValidateStagedConfig(ctx, []byte(goodConfig), dir) }()
	deadline := time.Now().Add(4 * time.Second)
	for {
		if _, err = os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned validation child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = os.Rename(assets, assets+"-old"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(assets, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(assets, "foreign")
	if err = os.WriteFile(sentinel, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("successful child accepted rebound asset root")
		}
	case <-ctx.Done():
		t.Fatal("child did not finish")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "untouched" {
		t.Fatal("foreign directory changed", err)
	}
}

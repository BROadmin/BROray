package geodata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPublishedFilesWithPinnedCore(t *testing.T) {
	core := os.Getenv("BRORAY_XRAY_BIN")
	if core == "" {
		t.Skip("BRORAY_XRAY_BIN and adjacent pinned data fixtures required")
	}
	root := filepath.Dir(core)
	for _, tc := range []struct {
		name, sha string
		kind      Kind
		tags      []string
	}{
		{"fi.dat", "f74e17b0c99a84a916bf8dd97afc00ab01b58dbf9a703e5741e8aed9e9eb6fe6", GeoIP, []string{"fi"}},
		{"dlc.dat", "c449116a9ad4cf406a31c23ff254b94f4d341db0c2d5ad2924410daa62b4277b", GeoSite, []string{"youtube", "category-ads", "category-telemetry"}},
	} {
		data, err := os.ReadFile(filepath.Join(root, tc.name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != tc.sha {
			t.Fatal("fixture identity mismatch")
		}
		if err := Validate(tc.kind, data, tc.tags); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	cfg := map[string]any{"log": map[string]any{"loglevel": "warning"}, "outbounds": []any{map[string]any{"tag": "deny", "protocol": "blackhole"}}, "routing": map[string]any{"rules": []any{
		map[string]any{"type": "field", "ip": []string{"ext:fi.dat:fi"}, "outboundTag": "deny"},
		map[string]any{"type": "field", "domain": []string{"ext:dlc.dat:youtube", "ext:dlc.dat:category-ads", "ext:dlc.dat:category-telemetry"}, "outboundTag": "deny"},
	}}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, core, "run", "-test", "-config", path)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+root)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("core config: %v: %s", err, output)
	}
	t.Logf("native ext data accepted by pinned core: %s", output)

	store, managedRoot := testStore(t)
	selection := Selection{CountryCodes: []string{"fi"}, GeoSiteTags: []string{"youtube", "category-ads", "category-telemetry"}}
	revisions := Revisions{GeoIP: "07b6131c86aeafbc6d9742b9baa3f8d7b7c78b95", GeoSite: "925d1d5de5427626382bad46b55d21be1b748a75"}
	candidate, err := store.Prepare(ctx, selection, revisions, func(_ context.Context, source Source) ([]byte, error) {
		name := "dlc.dat"
		if source.Kind == GeoIP {
			name = "fi.dat"
		}
		return os.ReadFile(filepath.Join(root, name))
	}, func(ctx context.Context, snapshot Snapshot) error {
		rules := []any{}
		for _, asset := range snapshot.Assets {
			refs := []string{}
			for _, tag := range asset.Tags {
				refs = append(refs, "ext:"+asset.Path+":"+tag)
			}
			key := "domain"
			if asset.Kind == GeoIP {
				key = "ip"
			}
			rules = append(rules, map[string]any{"type": "field", key: refs, "outboundTag": "deny"})
		}
		cfg["routing"] = map[string]any{"rules": rules}
		data, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, core, "run", "-test", "-config", path)
		cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+managedRoot)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("staged core: %w: %s", err, out)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := store.Commit(ctx, candidate, "")
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(managedRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.Load(ctx)
	if err != nil || loaded.Revision != committed.Revision {
		t.Fatalf("real data offline load: %v", err)
	}
	t.Log("staged generation accepted by core; committed data reopened offline")
}

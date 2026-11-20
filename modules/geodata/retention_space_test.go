package geodata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func retentionNativeInputs(t *testing.T) (string, Revisions, Fetch) {
	t.Helper()
	core := os.Getenv("BRORAY_XRAY_BIN")
	if core == "" {
		t.Fatal("pinned native core required")
	}
	rev := Revisions{GeoIP: "07b6131c86aeafbc6d9742b9baa3f8d7b7c78b95", GeoSite: "925d1d5de5427626382bad46b55d21be1b748a75"}
	fetch := func(_ context.Context, source Source) ([]byte, error) {
		name := "dlc.dat"
		if source.Kind == GeoIP {
			name = "fi.dat"
		}
		return os.ReadFile(filepath.Join(filepath.Dir(core), name))
	}
	return core, rev, fetch
}

func retentionNativeCheck(t *testing.T, core, root, assetPath string) error {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{
		"outbounds": []any{map[string]any{"tag": "deny", "protocol": "blackhole"}},
		"routing":   map[string]any{"rules": []any{map[string]any{"type": "field", "ip": []string{"ext:" + assetPath + ":fi"}, "outboundTag": "deny"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(testContext, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, core, "run", "-test", "-config", "stdin:", "-format", "json")
	cmd.Stdin = bytes.NewReader(cfg)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+root)
	return cmd.Run()
}

func TestGeodataRetainsNativeHistoricalReferences(t *testing.T) {
	core, revisions, fetch := retentionNativeInputs(t)
	store, root := testStore(t)
	var first, current Snapshot
	for i := 0; i < 3; i++ {
		c, err := store.Prepare(testContext, testSelection, revisions, fetch, fixtureCore)
		if err != nil {
			t.Fatal(err)
		}
		current, err = store.Commit(testContext, c, current.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = current
		}
	}
	if first.Revision == current.Revision {
		t.Fatal("did not exercise distinct generations")
	}
	offline, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	again, err := offline.Load(testContext)
	if err != nil || !reflect.DeepEqual(again, current) {
		t.Fatalf("offline current changed: %v", err)
	}
	if err = retentionNativeCheck(t, core, root, first.Assets[0].Path); err != nil {
		t.Fatalf("old configuration lost its native asset: %v", err)
	}
	if err = retentionNativeCheck(t, core, root, "missing-generation/fi.dat"); err == nil {
		t.Fatal("negative control did not load native asset")
	}
	for _, name := range []string{"snapshot.json", "previous.json"} {
		if _, err = os.Stat(filepath.Join(root, filepath.Dir(first.Assets[0].Path), name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("three committed generations; offline reopen and native oldest-config PASS; missing-path negative control rejected")
}

func TestGeodataRealENOSPCPreservesWorkingGeneration(t *testing.T) {
	core, revisions, fetch := retentionNativeInputs(t)
	mount := t.TempDir()
	if err := unix.Mount("tmpfs", mount, "tmpfs", 0, "size=8m"); err != nil {
		t.Fatalf("real isolated tmpfs required: %v", err)
	}
	defer func() {
		if err := unix.Unmount(mount, 0); err != nil {
			t.Errorf("unmount owned test tmpfs: %v", err)
		}
	}()
	root := filepath.Join(mount, "managed")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c, err := store.Prepare(testContext, testSelection, revisions, fetch, fixtureCore)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Commit(testContext, c, "")
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) []byte {
		t.Helper()
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	pointer := read(filepath.Join(root, "current.json"))
	payload := read(filepath.Join(root, current.Assets[0].Path))
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(mount, "owned-padding"), make([]byte, 7<<20), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	failed, err := store.Prepare(testContext, Selection{CountryCodes: []string{"fi"}, GeoSiteTags: []string{"youtube"}}, revisions, fetch, func(context.Context, Snapshot) error { called = true; return nil })
	if !errors.Is(err, unix.ENOSPC) || failed != nil || called {
		t.Fatalf("expected actual write ENOSPC before core: candidate=%v core=%v err=%v", failed, called, err)
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := func(entries []os.DirEntry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.Name())
		}
		return out
	}
	if !reflect.DeepEqual(names(before), names(after)) {
		t.Fatalf("failed candidate leaked or removed existing data: %v -> %v", names(before), names(after))
	}
	loaded, err := store.Load(testContext)
	if err != nil || !reflect.DeepEqual(loaded, current) {
		t.Fatalf("working generation changed after ENOSPC: %v", err)
	}
	if !bytes.Equal(pointer, read(filepath.Join(root, "current.json"))) || !bytes.Equal(payload, read(filepath.Join(root, current.Assets[0].Path))) {
		t.Fatal("working bytes changed after ENOSPC")
	}
	if err = retentionNativeCheck(t, core, root, current.Assets[0].Path); err != nil {
		t.Fatalf("working native config failed after ENOSPC: %v", err)
	}
	t.Log("real 8MiB tmpfs with 7MiB padding: candidate ENOSPC, no leaked generation, working pointer/payload exact, native config PASS")
}

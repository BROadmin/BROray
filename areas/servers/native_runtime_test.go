package servers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
)

func TestLocalXDriveRefusesOutsideRoot(t *testing.T) {
	e := newEnv(t)
	r := parseNativeFixture(t, nativeBackendFixture(t, "local"))
	cfg, err := XrayConfig(r, GenSettings{Listen: "127.0.0.1", SocksPort: 2080}, "test")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	e.s.xrayTest = func(context.Context, string) error { called = true; return nil }
	if err := e.s.testBytes(context.Background(), cfg); err == nil || called {
		t.Fatal("local storage outside actual installation root reached core")
	}
}

func TestLocalStorageOwnership(t *testing.T) {
	for _, kind := range []string{"new", "unmarked", "symlink", "foreign-marker"} {
		t.Run(kind, func(t *testing.T) {
			root := platform.Root{Dir: t.TempDir()}
			if err := os.Mkdir(root.Run(), 0700); err != nil {
				t.Fatal(err)
			}
			folder := filepath.Join(root.Run(), "xdrive-local")
			if kind == "symlink" {
				if err := os.Symlink(t.TempDir(), folder); err != nil {
					t.Fatal(err)
				}
			} else if kind != "new" {
				if err := os.Mkdir(folder, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "foreign-marker" {
					if err := os.WriteFile(filepath.Join(folder, ".broray-owned"), []byte("other"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := prepareLocalStorage(root, folder)
			if kind != "new" {
				if err == nil {
					t.Fatal("unowned local directory accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := prepareLocalStorage(root, folder); err != nil {
				t.Fatal("owned reuse", err)
			}
		})
	}
}

func TestNativeFlatVLESS(t *testing.T) {
	out := nativeTestOutbound("xdrive")
	ep := get(out, "settings", "vnext").([]any)[0].(map[string]any)
	user := ep["users"].([]any)[0].(map[string]any)
	want := parseNativeFixture(t, out)
	flat := map[string]any{"address": ep["address"], "port": ep["port"], "id": user["id"], "encryption": user["encryption"]}
	out["settings"] = flat
	got := parseNativeFixture(t, out)
	a, _ := Outbound(got)
	b, _ := Outbound(want)
	if !jsonValueEqual(a, b) {
		t.Fatal("official flat VLESS form changed connection")
	}
	flat["vnext"] = []any{ep}
	if _, err := ParseNativeOutbound(out, "test", "native-test", nil); err == nil {
		t.Fatal("ambiguous flat/vnext accepted")
	}
}

func TestLegacyRecordReadCompatibility(t *testing.T) {
	e := newEnv(t)
	id := e.addServer(t, "vless://00000000-0000-0000-0000-000000000001@example.test:443?security=none&type=raw#legacy")
	r, err := ReadRecord(e.d, id)
	if err != nil {
		t.Fatal(err)
	}
	// A formerly accepted parameter context must not trigger a URI reparse
	// when reading or generating an existing 3.5.x record.
	r["uri"] = "vless://00000000-0000-0000-0000-000000000001@example.test:443?security=none&type=raw&authority=legacy.example.test"
	r["legacyMetadata"] = map[string]any{"zero": 0, "disabled": false}
	data, _ := json.Marshal(r)
	if err := os.WriteFile(RecordPath(e.d, id), data, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := ReadRecord(e.d, id)
	if err != nil || !jsonValueEqual(restored, r) {
		t.Fatal("read performed strict import migration", err)
	}
	if _, err := XrayConfig(restored, GenSettings{}, "legacy"); err != nil {
		t.Fatal(err)
	}
}

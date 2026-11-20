package servers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceLegacyGuardOptionalParent(t *testing.T) {
	for _, kind := range []string{"missing-file", "missing-parent", "foreign-parent"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			id := e.addServer(t, "trojan://pw@127.0.0.1:9#Legacy")
			parent := filepath.Dir(reservePreferencesFile(e.d))
			foreign := t.TempDir()
			sentinel := filepath.Join(foreign, "untouched")
			if err := os.WriteFile(sentinel, []byte("foreign"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind != "missing-file" {
				if err := os.Remove(parent); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "foreign-parent" {
				if err := os.Symlink(foreign, parent); err != nil {
					t.Fatal(err)
				}
			}
			err := CheckSourceRemovalLegacy(e.d.Ctx, e.d, []string{id})
			if (err != nil) != (kind == "foreign-parent") {
				t.Fatal("optional parent classification", kind, err)
			}
			got, e2 := os.ReadFile(sentinel)
			if e2 != nil || string(got) != "foreign" {
				t.Fatal("read-only guard changed foreign data", e2)
			}
			if _, e2 := os.Stat(RecordPath(e.d, id)); e2 != nil {
				t.Fatal("read-only guard removed server", e2)
			}
		})
	}
}

func TestSourceStateOptionalParent(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		root := t.TempDir()
		parent := filepath.Join(root, "optional")
		if foreign {
			if err := os.Symlink(t.TempDir(), parent); err != nil {
				t.Fatal(err)
			}
		}
		raw, object, err := sourceStateObject(filepath.Join(parent, "state.json"))
		if foreign {
			if err == nil {
				t.Fatal("foreign optional parent accepted")
			}
		} else if err != nil || raw != nil || len(object) != 0 {
			t.Fatal("missing optional state parent refused", err)
		}
	}
}

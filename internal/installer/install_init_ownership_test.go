package installer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallInitOwnership(t *testing.T) {
	physicalDev5 := strings.NewReplacer("@ROOT@", "/opt/broray", "@OPT@", "/opt").Replace(dev5InitTemplateFixture)
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(physicalDev5))); got != "c0f1c863e43eaa447d9da27a232d2dc2e1b3ebf365fb33779aa2c050d30f8d69" {
		t.Fatalf("fixture differs from the physical dev5 init: %s", got)
	}
	for _, tc := range []struct {
		name   string
		kind   string
		wantOK bool
	}{
		{"exact_dev5", "legacy", true},
		{"exact_current", "current", true},
		{"modified_current", "modified_current", false},
		{"modified_dev5", "modified_legacy", false},
		{"symlink", "symlink", false},
		{"nonexecutable", "nonexecutable", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, opt := ownedUninstallFixture(t)
			body := renderInitScript(root)
			if tc.kind == "legacy" || tc.kind == "modified_legacy" {
				body = strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev5InitTemplateFixture)
			}
			if strings.HasPrefix(tc.kind, "modified_") {
				body += "\n# External modification must remain untouched.\n"
			}
			if err := os.WriteFile(InitScript(), []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.kind == "nonexecutable" {
				if err := os.Chmod(InitScript(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			external := filepath.Join(opt, "external-init")
			if tc.kind == "symlink" {
				if err := os.WriteFile(external, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(InitScript()); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, InitScript()); err != nil {
					t.Fatal(err)
				}
			}
			oldMode, err := os.Lstat(InitScript())
			if err != nil {
				t.Fatal(err)
			}
			_, err = Install(root, "3.5.0-test2", false)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("exact owned init refused: %v", err)
				}
				if target, err := os.Readlink(root.Current()); err != nil || target != filepath.Join("versions", "3.5.0-test2") {
					t.Fatalf("install did not switch to staged version: %q %v", target, err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "INSTALL_INIT_UNOWNED") {
					t.Fatalf("unowned init was not refused before installation: %v", err)
				}
				if got, err := os.ReadFile(InitScript()); err != nil || string(got) != body {
					t.Fatalf("unowned init bytes changed: %v", err)
				}
				if mode, err := os.Lstat(InitScript()); err != nil || mode.Mode() != oldMode.Mode() {
					t.Fatalf("unowned init type/mode changed: %v", err)
				}
				if tc.kind == "symlink" {
					if target, err := os.Readlink(InitScript()); err != nil || target != external {
						t.Fatalf("foreign link changed: %q %v", target, err)
					}
					if got, err := os.ReadFile(external); err != nil || string(got) != body {
						t.Fatalf("foreign target changed: %v", err)
					}
				}
				if target, err := os.Readlink(root.Current()); err != nil || target != filepath.Join("versions", "3.5.0-test") {
					t.Fatalf("current changed after refusal: %q %v", target, err)
				}
				if _, err := os.Lstat(filepath.Join(root.Versions(), "3.5.0-test2")); !os.IsNotExist(err) {
					t.Fatalf("refused installation created a version directory: %v", err)
				}
			}
			if got, err := os.ReadFile(filepath.Join(root.Data(), "owner-data.txt")); err != nil || string(got) != "SENTINEL" {
				t.Fatalf("data changed: %v", err)
			}
		})
	}
}

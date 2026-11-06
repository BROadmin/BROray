package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalInitBackupTemplates(t *testing.T) {
	for _, name := range []string{"current", "dev5", "dev7", "modified-dev7", "symlink-dev7", "nonexec-dev7", "outside-dev7"} {
		t.Run(name, func(t *testing.T) {
			root, _ := fixture(t)
			body := strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev7InitTemplateFixture)
			switch name {
			case "current":
				body = renderInitScript(root)
			case "dev5":
				body = strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev5InitScriptTemplate)
			case "modified-dev7":
				body += "\n# foreign modification\n"
			}
			backup := filepath.Join(root.UpdateState(), "S24broray.prev")
			if name == "outside-dev7" {
				backup = filepath.Join(t.TempDir(), "S24broray.prev")
			}
			if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
				t.Fatal(err)
			}
			mode := os.FileMode(0o755)
			if name == "nonexec-dev7" {
				mode = 0o600
			}
			if name == "symlink-dev7" {
				outside := filepath.Join(t.TempDir(), "foreign-init")
				if err := os.WriteFile(outside, []byte(body), mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, backup); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(backup, []byte(body), mode); err != nil {
				t.Fatal(err)
			}
			present, err := checkJournalInitBackup(root, &Journal{InitBackup: backup})
			want := name == "current" || name == "dev5" || name == "dev7"
			if want && (err != nil || !present) {
				t.Fatalf("exact owned %s backup refused: present=%v err=%v", name, present, err)
			}
			if !want && (err == nil || present) {
				t.Fatalf("unsafe %s backup accepted: present=%v err=%v", name, present, err)
			}
			if got, err := os.ReadFile(backup); err != nil || string(got) != body {
				t.Fatalf("backup changed during validation: %v", err)
			}
		})
	}
}

func TestCommitWithDev7InitBackup(t *testing.T) {
	for _, modified := range []bool{false, true} {
		name := "exact"
		if modified {
			name = "modified"
		}
		t.Run(name, func(t *testing.T) {
			root, _ := fixture(t)
			body := strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev7InitTemplateFixture)
			if modified {
				body += "\n# foreign modification\n"
			}
			backup := filepath.Join(root.UpdateState(), "S24broray.prev")
			if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backup, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			j := &Journal{Mode: "switch", From: "3.5.0-dev7", To: "3.5.0-dev8", InitBackup: backup}
			if err := j.set(root, JStarted); err != nil {
				t.Fatal(err)
			}
			err := commit(root, j)
			got, loadErr := LoadJournal(root)
			if loadErr != nil || got == nil {
				t.Fatalf("journal missing: %v", loadErr)
			}
			if modified {
				if err == nil || got.State != JStarted {
					t.Fatalf("modified backup did not block durable commit: state=%s err=%v", got.State, err)
				}
				if data, readErr := os.ReadFile(backup); readErr != nil || string(data) != body {
					t.Fatalf("foreign backup changed: %v", readErr)
				}
				return
			}
			if err != nil || got.State != JCommitted {
				t.Fatalf("exact dev7 backup blocked commit: state=%s err=%v", got.State, err)
			}
			if _, err := os.Lstat(backup); !os.IsNotExist(err) {
				t.Fatalf("owned backup not cleaned after durable commit: %v", err)
			}
		})
	}
}

func TestSwitchBackWithDev7InitBackup(t *testing.T) {
	root, _ := fixture(t)
	for _, version := range []string{"3.5.0-dev7", "3.5.0-dev8"} {
		dir := filepath.Join(root.Versions(), version)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "broray"), []byte(version), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("versions/3.5.0-dev8", root.Current()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(InitScript(), []byte(renderInitScript(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReplacer("@ROOT@", root.Dir, "@OPT@", Opt).Replace(dev7InitTemplateFixture)
	backup := filepath.Join(root.UpdateState(), "S24broray.prev")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	oldSHA, err := fileSHA256(filepath.Join(root.Versions(), "3.5.0-dev7", "broray"))
	if err != nil {
		t.Fatal(err)
	}
	newSHA, err := fileSHA256(filepath.Join(root.Versions(), "3.5.0-dev8", "broray"))
	if err != nil {
		t.Fatal(err)
	}
	j := &Journal{OperationID: "legacy7-rollback-test", Mode: "switch", From: "3.5.0-dev7", To: "3.5.0-dev8",
		PreviousTarget: "versions/3.5.0-dev7", NewTarget: "versions/3.5.0-dev8",
		PreviousSHA256: oldSHA, StagedSHA256: newSHA, InitBackup: backup}
	if err := switchBack(root, j); err != nil {
		t.Fatalf("exact dev7 backup blocked rollback: %v", err)
	}
	if got, err := os.Readlink(root.Current()); err != nil || got != j.PreviousTarget {
		t.Fatalf("previous link not restored: %q %v", got, err)
	}
	if got, err := os.ReadFile(InitScript()); err != nil || string(got) != body {
		t.Fatalf("exact previous init not restored: %v", err)
	}
	if _, err := os.Lstat(backup); !os.IsNotExist(err) {
		t.Fatalf("restored backup remains: %v", err)
	}
}

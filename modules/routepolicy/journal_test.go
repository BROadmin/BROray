package routepolicy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/geodata"
	"golang.org/x/sys/unix"
)

var journalContext = context.Background()
var journalOldConfig = []byte(`{"old":true}`)
var journalNextConfig = []byte(`{"next":true}`)

func journalEnv(t *testing.T, previous bool) (platform.Root, *Journal, *platform.Lock) {
	t.Helper()
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if previous {
		if err := os.WriteFile(root.XrayConfig(), journalOldConfig, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root.Routes(), StateFile), []byte(`{"schemaVersion":1,"rules":[]}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	j, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	guard, err := platform.TryLock(root.DaemonLock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guard.Release() })
	return root, j, guard
}
func journalNextState() State {
	return State{SchemaVersion: 1, Rules: []Rule{{ID: "deny", Enabled: true, Action: Deny, Domains: []string{"example.com"}}}}
}
func journalSetConfig(t *testing.T, root platform.Root) {
	t.Helper()
	if err := platform.WriteFileAtomicNoFollow(root.XrayConfig(), journalNextConfig, 0600); err != nil {
		t.Fatal(err)
	}
}
func assertJournalGone(t *testing.T, root platform.Root) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root.Routes(), JournalFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still exists: %v", err)
	}
}

func TestPolicyJournalColdRecoveryAtEachTransition(t *testing.T) {
	for _, step := range []string{"prepared", "config", "state"} {
		t.Run(step, func(t *testing.T) {
			root, j, guard := journalEnv(t, true)
			before, err := os.ReadFile(filepath.Join(root.Routes(), StateFile))
			if err != nil {
				t.Fatal(err)
			}
			if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
				t.Fatal(err)
			}
			if step != "prepared" {
				journalSetConfig(t, root)
			}
			if step == "state" {
				if err := j.InstallState(journalContext); err != nil {
					t.Fatal(err)
				}
			}
			if err := j.Close(); err != nil {
				t.Fatal(err)
			}
			if err := RecoverPolicy(journalContext, root, guard); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(root.XrayConfig())
			if err != nil || string(got) != string(journalOldConfig) {
				t.Fatalf("old config not restored: %s %v", got, err)
			}
			got, err = os.ReadFile(filepath.Join(root.Routes(), StateFile))
			if err != nil || string(got) != string(before) {
				t.Fatalf("old rules not restored: %s %v", got, err)
			}
			assertJournalGone(t, root)
		})
	}
}

func TestPolicyJournalConfirmedAndFirstActivation(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback-absent", true: "confirmed"}[confirm], func(t *testing.T) {
			root, j, guard := journalEnv(t, false)
			if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
				t.Fatal(err)
			}
			journalSetConfig(t, root)
			if err := j.InstallState(journalContext); err != nil {
				t.Fatal(err)
			}
			if confirm {
				if err := j.Confirm(journalContext); err != nil {
					t.Fatal(err)
				}
			}
			if err := RecoverPolicy(journalContext, root, guard); err != nil {
				t.Fatal(err)
			}
			if confirm {
				got, err := os.ReadFile(root.XrayConfig())
				if err != nil || string(got) != string(journalNextConfig) {
					t.Fatalf("confirmed config changed: %v", err)
				}
				state, err := ReadState(journalContext, root.Routes())
				if err != nil || len(state.Rules) != 1 || !state.Rules[0].Enabled {
					t.Fatal("confirmed rules changed")
				}
			} else {
				for _, p := range []string{root.XrayConfig(), filepath.Join(root.Routes(), StateFile)} {
					if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("first activation rollback left %s", p)
					}
				}
			}
			assertJournalGone(t, root)
		})
	}
}

func TestPolicyJournalRefusesForeignAndLiveOwners(t *testing.T) {
	for _, kind := range []string{"config", "state", "symlink", "child", "installer", "launcher", "no-daemon-guard"} {
		t.Run(kind, func(t *testing.T) {
			root, j, guard := journalEnv(t, true)
			if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
				t.Fatal(err)
			}
			foreign := []byte(`{"foreign":true}`)
			switch kind {
			case "config":
				if err := os.WriteFile(root.XrayConfig(), foreign, 0600); err != nil {
					t.Fatal(err)
				}
			case "state":
				if err := os.WriteFile(filepath.Join(root.Routes(), StateFile), foreign, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(root.XrayConfig(), root.XrayConfig()+".held"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(root.XrayConfig()+".held", root.XrayConfig()); err != nil {
					t.Fatal(err)
				}
			case "child", "installer", "launcher":
				p := filepath.Join(root.Run(), "xray-child.lock")
				if kind == "installer" {
					p = root.InstallerLock()
				}
				if kind == "launcher" {
					p = root.LauncherLock()
				}
				lock, err := platform.TryLock(p)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Release()
			case "no-daemon-guard":
				guard = nil
			}
			before, err := os.ReadFile(filepath.Join(root.Routes(), JournalFile))
			if err != nil {
				t.Fatal(err)
			}
			if err := RecoverPolicy(journalContext, root, guard); err == nil {
				t.Fatal("uncertain/live state accepted")
			}
			after, err := os.ReadFile(filepath.Join(root.Routes(), JournalFile))
			if err != nil || string(after) != string(before) {
				t.Fatal("failed recovery changed journal")
			}
			if kind == "config" {
				got, _ := os.ReadFile(root.XrayConfig())
				if string(got) != string(foreign) {
					t.Fatal("foreign config overwritten")
				}
			}
		})
	}
}

func TestPolicyJournalRuntimeRollbackMustBeConfirmed(t *testing.T) {
	root, j, guard := journalEnv(t, true)
	if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
		t.Fatal(err)
	}
	journalSetConfig(t, root)
	if err := j.InstallState(journalContext); err != nil {
		t.Fatal(err)
	}
	if err := j.Rollback(journalContext, func(context.Context, []byte, bool) error { return errors.New("live restore unknown") }); err == nil {
		t.Fatal("unknown restore confirmed")
	}
	if _, err := os.Stat(filepath.Join(root.Routes(), JournalFile)); err != nil {
		t.Fatal("journal lost after restore failure")
	}
	if err := j.Rollback(journalContext, func(context.Context, []byte, bool) error { return nil }); err == nil {
		t.Fatal("false success without config readback accepted")
	}
	if err := RecoverPolicy(journalContext, root, guard); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyJournalRejectsSecondWriterAndRootReplacement(t *testing.T) {
	root, j, _ := journalEnv(t, true)
	if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
		t.Fatal(err)
	}
	if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err == nil {
		t.Fatal("pending journal overwritten")
	}
	if err := os.Rename(root.Routes(), root.Routes()+"-held"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root.Routes(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := j.InstallState(journalContext); err == nil {
		t.Fatal("replaced root accepted")
	}
	entries, err := os.ReadDir(root.Routes())
	if err != nil || len(entries) != 0 {
		t.Fatal("foreign root changed")
	}
}

func TestPolicyJournalGeoRollbackAfterRestart(t *testing.T) {
	for _, damage := range []string{"none", "current", "previous"} {
		t.Run(damage, func(t *testing.T) { testPolicyGeoRecovery(t, damage) })
	}
}

func testPolicyGeoRecovery(t *testing.T, damage string) {
	root, j, guard := journalEnv(t, true)
	geoRoot := filepath.Join(root.Runtime(), geodata.AssetDirectory)
	if err := os.Mkdir(geoRoot, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := geodata.Open(geoRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prepare := func(rev string) *geodata.Candidate {
		candidate, err := store.Prepare(journalContext, geodata.Selection{CountryCodes: []string{"fi"}}, geodata.Revisions{GeoIP: strings.Repeat(rev, 40)}, func(context.Context, geodata.Source) ([]byte, error) {
			return []byte{10, 14, 10, 2, 'F', 'I', 18, 8, 10, 4, 1, 2, 3, 0, 16, 24}, nil
		}, func(context.Context, geodata.Snapshot) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		return candidate
	}
	first := prepare("a")
	old, err := store.Commit(journalContext, first, "")
	if err != nil {
		t.Fatal(err)
	}
	second := prepare("b")
	next := second.Snapshot()
	state := journalNextState()
	state.GeoRevision = next.Revision
	if err := j.Begin(journalContext, state, journalNextConfig, next.Revision); err != nil {
		t.Fatal(err)
	}
	journalSetConfig(t, root)
	if _, err := store.Commit(journalContext, second, old.Revision); err != nil {
		t.Fatal(err)
	}
	if err := j.InstallState(journalContext); err != nil {
		t.Fatal(err)
	}
	if damage != "none" {
		asset := next.Assets[0]
		if damage == "previous" {
			asset = old.Assets[0]
		}
		if err := os.WriteFile(filepath.Join(geoRoot, asset.Path), []byte("truncated"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if damage == "previous" {
		if err := RecoverPolicy(journalContext, root, guard); err == nil {
			t.Fatal("damaged predecessor restored")
		}
		got, err := store.Load(journalContext)
		if err != nil || got.Revision != next.Revision {
			t.Fatal("valid current generation changed")
		}
		if _, err := os.Stat(filepath.Join(root.Routes(), JournalFile)); err != nil {
			t.Fatal("failed rollback lost journal")
		}
		return
	}
	if err := RecoverPolicy(journalContext, root, guard); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(journalContext)
	if err != nil || got.Revision != old.Revision {
		t.Fatalf("geo predecessor not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(geoRoot, next.Assets[0].Path)); err != nil {
		t.Fatal("new generation was removed")
	}
}

func TestPolicyJournalCommittedMarkerSurvivesCrash(t *testing.T) {
	root, j, guard := journalEnv(t, true)
	if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
		t.Fatal(err)
	}
	journalSetConfig(t, root)
	if err := j.InstallState(journalContext); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.Routes(), JournalFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["phase"] = "committed"
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := platform.WriteFileAtomicNoFollow(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPolicy(journalContext, root, guard); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(root.XrayConfig())
	if err != nil || string(got) != string(journalNextConfig) {
		t.Fatal("confirmed state rolled back")
	}
	assertJournalGone(t, root)
}

func TestPolicyJournalRejectsMissingRecoveryFields(t *testing.T) {
	for _, field := range []string{"previousState", "previousConfig", "previousGeo", "nextGeo"} {
		t.Run(field, func(t *testing.T) {
			root, j, guard := journalEnv(t, true)
			if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
				t.Fatal(err)
			}
			journalSetConfig(t, root)
			if err := j.InstallState(journalContext); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root.Routes(), JournalFile)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]json.RawMessage
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			delete(record, field)
			data, err = json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := platform.WriteFileAtomicNoFollow(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := RecoverPolicy(journalContext, root, guard); err == nil {
				t.Fatal("incomplete recovery metadata accepted")
			}
			got, err := os.ReadFile(root.XrayConfig())
			if err != nil || string(got) != string(journalNextConfig) {
				t.Fatal("untrusted recovery changed config")
			}
			got, err = os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Fatal("failed recovery changed journal")
			}
		})
	}
}

func TestPolicyJournalRequiresAppliedStateAndIndependentLock(t *testing.T) {
	root, j, _ := journalEnv(t, true)
	other, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := unix.Flock(int(j.dirs[root.Routes()].Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := other.Begin(journalContext, journalNextState(), journalNextConfig, ""); err == nil {
		t.Fatal("independent journal lock bypassed")
	}
	if err := unix.Flock(int(j.dirs[root.Routes()].Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
		t.Fatal(err)
	}
	if err := j.InstallState(journalContext); err == nil {
		t.Fatal("rules stored before config applied")
	}
	if err := j.Confirm(journalContext); err == nil {
		t.Fatal("incomplete transaction confirmed")
	}
	journalSetConfig(t, root)
	if err := j.Confirm(journalContext); err == nil {
		t.Fatal("uncommitted rules confirmed")
	}
	if err := j.InstallState(journalContext); err != nil {
		t.Fatal(err)
	}
	if err := j.Confirm(journalContext); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyJournalRejectsDamagedOrUnsafeJournal(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "oversize", "hash"} {
		t.Run(kind, func(t *testing.T) {
			root, j, guard := journalEnv(t, true)
			if err := j.Begin(journalContext, journalNextState(), journalNextConfig, ""); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root.Routes(), JournalFile)
			if kind == "symlink" || kind == "fifo" {
				if err := os.Rename(path, path+".held"); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "symlink" {
					err = os.Symlink(path+".held", path)
				} else {
					err = unix.Mkfifo(path, 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			} else if kind == "oversize" {
				if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxJournalBytes+1)), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var record map[string]any
				if err := json.Unmarshal(data, &record); err != nil {
					t.Fatal(err)
				}
				record["previousConfig"].(map[string]any)["sha256"] = strings.Repeat("0", 64)
				data, err = json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := RecoverPolicy(journalContext, root, guard); err == nil {
				t.Fatal("unsafe journal accepted")
			}
			got, err := os.ReadFile(root.XrayConfig())
			if err != nil || string(got) != string(journalOldConfig) {
				t.Fatal("old config changed")
			}
		})
	}
}

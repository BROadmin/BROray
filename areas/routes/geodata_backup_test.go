package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/geodata"
	"github.com/BROadmin/broray/modules/routepolicy"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRouteSettingsBackupIncludesPolicyAndGeodata(t *testing.T) {
	env, _ := geoAPIEnv(t)
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	geoMustComplete(t, geoSubmit(env, "geo-upsert", "video", `,"name":"Video","kind":"geosite","tags":["youtube"]`))
	before := geoLoad(t, env)
	if len(before.Assets) != 2 {
		t.Fatal("fixture must contain both selected native assets", before)
	}
	exported, err := env.e.backupExport()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	policy, ok := document["policy"]
	if !ok || string(policy) == "null" {
		t.Fatal("settings backup silently omits saved routing policy and selected offline geodata")
	}
	var part map[string]json.RawMessage
	if err = json.Unmarshal(policy, &part); err != nil {
		t.Fatal(err)
	}
	if len(part["geodata"]) == 0 || string(part["geodata"]) == "null" {
		t.Fatal("backup omits selected OFF geodata")
	}
	after := geoLoad(t, env)
	if after.Revision != before.Revision {
		t.Fatal("read-only export changed live generation")
	}
}

func TestRouteSettingsBackupOfflineStageAndUndo(t *testing.T) {
	source, _ := geoAPIEnv(t)
	geoMustComplete(t, geoSubmit(source, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	geoMustComplete(t, geoSubmit(source, "geo-upsert", "video", `,"name":"Video","kind":"geosite","tags":["youtube"]`))
	saved, err := source.e.backupExport()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	target := newEnv(t)
	target.d.Svc.ServersPreviewBackupBaseConfig = func(context.Context, *compat.BackupStage) ([]byte, error) { return nil, nil }
	stage, err := compat.NewBackupStage(target.d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativeChecks := 0
	target.d.Svc.XrayValidateStagedConfig = func(ctx context.Context, cfg []byte, assets *os.File) error {
		if !compat.FenceHeld(ctx) || assets.Name() == target.d.Root.Runtime() {
			t.Fatal("validation escaped detached stage")
		}
		nativeChecks++
		core := os.Getenv("BRORAY_XRAY_BIN")
		if core == "" {
			t.Fatal("pinned native core required")
		}
		cmd := exec.CommandContext(ctx, core, "run", "-test", "-c", "stdin:", "-format", "json")
		cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+assets.Name())
		cmd.Stdin = bytes.NewReader(cfg)
		return cmd.Run()
	}
	ctx := compat.WithFence(context.Background())
	before, err := routepolicy.ReadState(ctx, target.e.root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := target.e.backupStage(ctx, raw, stage)
	if err != nil {
		t.Fatal(err)
	}
	if nativeChecks == 0 || len(plan.Changes) == 0 {
		t.Fatal("policy import silently ignored", nativeChecks, plan)
	}
	live, err := routepolicy.ReadState(ctx, target.e.root)
	if err != nil || !reflect.DeepEqual(before, live) {
		t.Fatal("preview changed live policy", err)
	}
	next, err := routepolicy.ReadState(ctx, stage.St.Root.Routes())
	if err != nil || len(next.Rules) != 2 {
		t.Fatal("policy not staged", err, next)
	}
	for _, rule := range next.Rules {
		if rule.Enabled {
			t.Fatal("OFF rule enabled")
		}
	}
	store, err := geodata.Open(filepath.Join(stage.St.Root.Runtime(), geodata.AssetDirectory))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load(ctx)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != next.GeoRevision || snapshot.Revision == geoLoad(t, source).Revision || len(snapshot.Assets) != 2 {
		t.Fatal("stale or incomplete imported generation", snapshot)
	}
	// Persist the test's recovery plan before invoking the shared swap primitive.
	pre := t.TempDir()
	manifest, _ := json.Marshal(stage.Paths())
	if err = compat.WriteJSON(filepath.Join(pre, "planned.json"), json.RawMessage(manifest)); err != nil {
		t.Fatal(err)
	}
	if _, err = compat.SwapBackupPaths(target.d.Root.Dir, stage.Dir, filepath.Join(pre, "original"), stage.Paths()); err != nil {
		t.Fatal(err)
	}
	applied, err := routepolicy.ReadState(ctx, target.e.root)
	if err != nil || !reflect.DeepEqual(next, applied) {
		t.Fatal("policy apply", err)
	}
	undo := t.TempDir()
	if err = compat.WriteJSON(filepath.Join(undo, "planned.json"), json.RawMessage(manifest)); err != nil {
		t.Fatal(err)
	}
	if _, err = compat.SwapBackupPaths(target.d.Root.Dir, filepath.Join(pre, "original"), filepath.Join(undo, "original"), stage.Paths()); err != nil {
		t.Fatal(err)
	}
	restored, err := routepolicy.ReadState(ctx, target.e.root)
	if err != nil || !reflect.DeepEqual(before, restored) {
		t.Fatal("policy undo", err)
	}
}

func TestRouteSettingsBackupStrictNewFieldAndLegacy(t *testing.T) {
	valid := `{"state":{"schemaVersion":1,"rules":[]},"autoUpdate":false,"geodata":null}`
	for _, raw := range []string{
		`{"policy":null}`, `{"Policy":` + valid + `}`, `{"policy":` + valid + `,"policy":` + valid + `}`,
		`{"policy":{"state":{"schemaVersion":1,"rules":[]},"autoUpdate":null,"geodata":null}}`,
		`{"policy":{"state":{"schemaVersion":1,"rules":[]},"autoUpdate":false,"geodata":null,"extra":true}}`,
		`{"policy":{"state":{"schemaVersion":1,"rules":[]},"autoUpdate":false,"AutoUpdate":true,"geodata":null}}`,
		`{"policy":{"state":{"schemaVersion":1,"rules":[]},"autoUpdate":false,"autoUpdate":true,"geodata":null}}`,
		`{"policy":{"state":{"schemaVersion":1,"rules":[]},"autoUpdate":false}}`,
		`{"policy":{"state":{"schemaVersion":1,"schemaVersion":1,"rules":[]},"autoUpdate":false,"geodata":null}}`,
	} {
		if _, err := policyBackupInput([]byte(raw)); err == nil {
			t.Fatal("ambiguous policy backup accepted", raw)
		}
	}
	if v, err := policyBackupInput([]byte(`{"custom":[],"builtinInstalled":[]}`)); err != nil || v != nil {
		t.Fatal("legacy backup changed policy semantics", err)
	}
	env := newEnv(t)
	stage, err := compat.NewBackupStage(env.d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.e.backupStage(context.Background(), []byte(`{"policy":`+valid+`}`), stage); err == nil {
		t.Fatal("unfenced new policy accepted")
	}
	if len(stage.Paths()) != 0 {
		t.Fatal("unfenced policy staged swap paths")
	}
}

func TestRouteSettingsBackupRejectsBadDataWithoutLiveWrites(t *testing.T) {
	source, _ := geoAPIEnv(t)
	geoMustComplete(t, geoSubmit(source, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	exported, err := source.e.backupExport()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing-core", "rejected-core", "missing-archive", "mismatched-revision", "foreign-stage", "pending-policy"} {
		t.Run(kind, func(t *testing.T) {
			target := newEnv(t)
			target.d.Svc.ServersPreviewBackupBaseConfig = func(context.Context, *compat.BackupStage) ([]byte, error) { return nil, nil }
			stage, err := compat.NewBackupStage(target.d, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]json.RawMessage
			json.Unmarshal(raw, &doc)
			var part map[string]json.RawMessage
			json.Unmarshal(doc["policy"], &part)
			foreign := ""
			switch kind {
			case "rejected-core":
				target.d.Svc.XrayValidateStagedConfig = func(context.Context, []byte, *os.File) error { return errors.New("native reject") }
			case "missing-archive":
				part["geodata"] = json.RawMessage("null")
			case "mismatched-revision":
				state, err := routepolicy.DecodeState(part["state"])
				if err != nil {
					t.Fatal(err)
				}
				state.GeoRevision = strings.Repeat("f", 64)
				part["state"], _ = json.Marshal(state)
			case "foreign-stage":
				foreign = t.TempDir()
				if err = os.WriteFile(filepath.Join(foreign, "sentinel"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(foreign, stage.St.Root.Runtime()); err != nil {
					t.Fatal(err)
				}
			case "pending-policy":
				if err = os.WriteFile(filepath.Join(target.e.root, routepolicy.JournalFile), []byte("pending-owner"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			doc["policy"], _ = json.Marshal(part)
			input, _ := json.Marshal(doc)
			before, err := compat.BackupDigest(target.d.Root.Dir, "data/routes", nil)
			if err != nil {
				t.Fatal(err)
			}
			runtimeBefore, err := compat.BackupDigest(target.d.Root.Dir, "runtime", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = target.e.backupStage(compat.WithFence(context.Background()), input, stage); err == nil {
				t.Fatal("invalid backup accepted", kind)
			}
			after, err := compat.BackupDigest(target.d.Root.Dir, "data/routes", nil)
			if err != nil || before != after {
				t.Fatal("failed preview wrote live policy", err)
			}
			runtimeAfter, err := compat.BackupDigest(target.d.Root.Dir, "runtime", nil)
			if err != nil || runtimeBefore != runtimeAfter {
				t.Fatal("failed preview wrote live runtime", err)
			}
			if foreign != "" {
				entries, err := os.ReadDir(foreign)
				if err != nil || len(entries) != 1 {
					t.Fatal("foreign staging root modified", err)
				}
			}
		})
	}
}

func TestRouteSettingsBackupRefusesLateStagedAssetChange(t *testing.T) {
	source, _ := geoAPIEnv(t)
	geoMustComplete(t, geoSubmit(source, "geo-upsert", "fi", `,"name":"Finland","kind":"geoip","tags":["fi"]`))
	exported, err := source.e.backupExport()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"payload", "directory"} {
		t.Run(kind, func(t *testing.T) {
			target := newEnv(t)
			target.d.Svc.ServersPreviewBackupBaseConfig = func(context.Context, *compat.BackupStage) ([]byte, error) { return nil, nil }
			changed := false
			target.d.Svc.XrayValidateStagedConfig = func(ctx context.Context, cfg []byte, assets *os.File) error {
				managed := filepath.Join(assets.Name(), geodata.AssetDirectory)
				// Before the first native check there is no committed staging pointer.
				pointer, err := os.ReadFile(filepath.Join(managed, "current.json"))
				if os.IsNotExist(err) {
					return nil
				}
				if err != nil {
					return err
				}
				if changed {
					return nil
				}
				changed = true
				if kind == "directory" {
					if err = os.Rename(managed, managed+"-original"); err != nil {
						return err
					}
					if err = os.Mkdir(managed, 0700); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(managed, "foreign"), []byte("keep"), 0600)
				}
				var snapshot geodata.Snapshot
				if err = json.Unmarshal(pointer, &snapshot); err != nil {
					return err
				}
				if len(snapshot.Assets) == 0 {
					t.Fatal("control has no payload")
				}
				return os.WriteFile(filepath.Join(managed, snapshot.Assets[0].Path), []byte("late corruption"), 0600)
			}
			stage, err := compat.NewBackupStage(target.d, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			before, err := compat.BackupDigest(target.d.Root.Dir, "data/routes", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = target.e.backupStage(compat.WithFence(context.Background()), raw, stage); err == nil {
				t.Fatal("late staged asset change accepted", kind)
			}
			if !changed {
				t.Fatal("late-mutation control did not run")
			}
			after, err := compat.BackupDigest(target.d.Root.Dir, "data/routes", nil)
			if err != nil || before != after {
				t.Fatal("failed preview changed live settings", err)
			}
		})
	}
}

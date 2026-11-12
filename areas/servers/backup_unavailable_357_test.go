package servers

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

func backupUnavailable357Active(t *testing.T) (*fakeEnv, Record, []byte) {
	t.Helper()
	env := newEnv(t)
	id := env.addServer(t, "vless://00000000-0000-4000-8000-000000000357@active.example.test:443#Active")
	if err := setActiveID(env.d, id); err != nil {
		t.Fatal("could not seed active pointer")
	}
	rec, err := ReadRecord(env.d, id)
	if err != nil {
		t.Fatal("could not read active fixture")
	}
	cfg, err := XrayConfig(rec, GenSettings{}, "backup-active")
	if err != nil || platform.WriteFileAtomic(env.d.Root.XrayConfig(), cfg, 0o600) != nil {
		t.Fatal("could not seed synthetic active config")
	}
	env.running.Store(true)
	return env, rec, cfg
}

func backupUnavailable357Payload(t *testing.T, rec Record, active *string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal("could not encode private unavailable fixture")
	}
	body, err := json.Marshal(serversBackup{Records: []json.RawMessage{raw}, ActiveServerID: active})
	if err != nil {
		t.Fatal("could not encode synthetic backup")
	}
	return body
}

func backupUnavailable357AssertActive(t *testing.T, env *fakeEnv, active Record, cfg []byte) {
	t.Helper()
	id := active["id"].(string)
	current, err := ReadRecord(env.d, id)
	if err != nil || !sameRecord(current, active) || ActiveID(env.d) != id {
		t.Fatal("backup changed the live active record or pointer")
	}
	currentConfig, err := os.ReadFile(env.d.Root.XrayConfig())
	if err != nil || !bytes.Equal(currentConfig, cfg) || len(env.applied) != 0 || !env.running.Load() {
		t.Fatal("backup changed or reapplied the active runtime")
	}
}

func TestBackupUnavailable357RejectsActiveDuringStage(t *testing.T) {
	for _, selecting := range []bool{false, true} {
		name := "replace-current-active"
		if selecting {
			name = "select-new-unavailable"
		}
		t.Run(name, func(t *testing.T) {
			env, active, cfg := backupUnavailable357Active(t)
			replacement := unavailableClashRecord(t, "tuic")
			var activeID *string
			if selecting {
				id := replacement["id"].(string)
				activeID = &id
			} else {
				replacement["id"] = active["id"]
			}
			stage, err := compat.NewBackupStage(env.d, t.TempDir())
			if err != nil {
				t.Fatal("could not create backup stage")
			}
			_, err = env.s.backupStage(env.d.Ctx, backupUnavailable357Payload(t, replacement, activeID), stage)
			if err == nil {
				t.Fatal("backup preflight accepted an unavailable final active record")
			}
			if strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("backup preflight exposed private source fields")
			}
			backupUnavailable357AssertActive(t, env, active, cfg)
		})
	}
}

func TestBackupUnavailable357RestoresInactiveSource(t *testing.T) {
	env, active, cfg := backupUnavailable357Active(t)
	inactive := unavailableClashRecord(t, "tuic")
	stage, err := compat.NewBackupStage(env.d, t.TempDir())
	if err != nil {
		t.Fatal("could not create backup stage")
	}
	if _, err := env.s.backupStage(env.d.Ctx, backupUnavailable357Payload(t, inactive, nil), stage); err != nil {
		t.Fatalf("inactive unavailable backup rejected: %v", err)
	}
	before := env.s.backupBefore()
	if _, err := compat.SwapBackupPaths(env.d.Root.Dir, stage.Dir, t.TempDir(), stage.Paths()); err != nil {
		t.Fatalf("inactive unavailable backup swap failed: %v", err)
	}
	after := env.s.backupAfter(env.d.Ctx, before)
	if len(after.FollowUps) != 0 {
		t.Fatal("restoring an inactive source disturbed the active server")
	}
	backupUnavailable357AssertActive(t, env, active, cfg)
	restored, err := ReadRecord(env.d, inactive["id"].(string))
	if err != nil || Validate(restored, "inactive-backup") != nil || !jsonValueEqual(restored["importProfile"], inactive["importProfile"]) {
		t.Fatal("inactive unavailable private source was not restored intact")
	}
	if connectionSupport(restored)["supported"] != false {
		t.Fatal("backup restoration made unavailable source executable")
	}
	masked, _ := json.Marshal(MaskRecord(restored))
	if strings.Contains(string(masked), "synthetic-private") || strings.Contains(string(masked), "importProfile") {
		t.Fatal("restored private source escaped public mask")
	}
}

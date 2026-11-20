package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/modules/xray"
)

type sourceOwnerService interface {
	PrepareSourceRemoval(context.Context, string, []string, []string) (string, error)
}

func TestSourceOwnerServiceReconcilesState(t *testing.T) {
	for _, kind := range []string{"replace", "manual-replace", "withdraw-auto", "withdraw-manual", "absent-auto", "absent-state", "bad-auto", "foreign-cache", "bad-state", "foreign-state", "auto-drift", "foreign-state-after"} {
		t.Run(kind, func(t *testing.T) {
			f, description := reserveDisableFixture(t)
			owner, ok := any(f.e.d.Svc).(sourceOwnerService)
			if !ok {
				t.Fatal("SOURCE_OWNER_SERVICE_ABSENT")
			}
			if err := f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision); err != nil {
				t.Fatal(err)
			}
			oldID, nextID := f.plan.node.ID, f.backup
			sourceID := "source-owner"
			withdraw := kind == "withdraw-auto" || kind == "withdraw-manual"
			removed, candidates := []string{oldID}, []string{nextID}
			if withdraw {
				removed = []string{oldID, nextID}
				candidates = nil
			}
			records := map[string][]byte{}
			for _, id := range []string{oldID, nextID} {
				p := RecordPath(f.e.d, id)
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				var rec Record
				if err = json.Unmarshal(b, &rec); err != nil {
					t.Fatal(err)
				}
				if id == oldID || withdraw {
					rec["source"] = map[string]any{"type": "subscription", "subscriptionId": sourceID}
				}
				b, err = json.Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(p, b, 0600); err != nil {
					t.Fatal(err)
				}
				records[p] = b
			}
			policy := []byte(`{"schemaVersion":3,"enabled":true,"preserve":"user-policy"}`)
			if kind == "bad-auto" {
				policy = []byte("broken policy")
			}
			if kind == "absent-auto" {
				if err := os.Remove(autoSwitchFile(f.e.d)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(autoSwitchFile(f.e.d), policy, 0600); err != nil {
				t.Fatal(err)
			}
			state := []byte(`{"schemaVersion":3,"activeHealth":{"serverId":"old","status":"healthy"},"lastProxyContext":"stale","consecutiveFailures":5,"withdrawal":{"pending":true},"preserve":"history"}`)
			foreign := filepath.Join(t.TempDir(), "foreign-state")
			if err := os.WriteFile(foreign, []byte("foreign-owned"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "bad-state" {
				state = []byte("broken state")
			}
			if err := os.WriteFile(stateFile(f.e.d), state, 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "absent-state" {
				if err := os.Remove(stateFile(f.e.d)); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "foreign-state" {
				if err := os.Remove(stateFile(f.e.d)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, stateFile(f.e.d)); err != nil {
					t.Fatal(err)
				}
			}
			cache := f.e.d.RunPath("interface-status.json")
			if err := os.WriteFile(cache, []byte(`{"old":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "foreign-cache" {
				if err := os.Remove(cache); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, cache); err != nil {
					t.Fatal(err)
				}
			}
			manual := kind == "manual-replace" || kind == "withdraw-manual"
			if manual {
				if err := f.process.Stop(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			before := f.process.RuntimeSnapshot()
			prefsBefore, e := readReservePreferences(f.e.d)
			if e != nil {
				t.Fatal(e)
			}
			f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = f.e.d.Svc.KeeneticVerifyInterfaceTarget
			f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) {
				if ActiveID(f.e.d) == nextID {
					return "BROray - B", nil
				}
				return "BROray - A", nil
			}
			f.e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) { return "BROray - " + name, nil }
			f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
				*description = "BROray - B"
				if kind == "foreign-state-after" {
					if err := os.Remove(stateFile(f.e.d)); err != nil {
						return err
					}
					return os.Symlink(foreign, stateFile(f.e.d))
				}
				return nil
			}
			f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, cfg []byte) error {
				if kind == "auto-drift" {
					if err := os.WriteFile(autoSwitchFile(f.e.d), []byte(`{"enabled":false}`), 0600); err != nil {
						return err
					}
				}
				p := filepath.Join(t.TempDir(), "candidate.json")
				if err := os.WriteFile(p, cfg, 0600); err != nil {
					return err
				}
				return xray.Test(ctx, f.e.d.Root.XrayBinary(), p)
			}
			chosen, err := owner.PrepareSourceRemoval(f.ctx, sourceID, removed, candidates)
			failure := kind == "bad-auto" || kind == "foreign-cache" || kind == "bad-state" || kind == "foreign-state" || kind == "auto-drift" || kind == "foreign-state-after"
			if (err != nil) != failure {
				t.Fatal("owner result", kind, chosen, err)
			}
			if !failure {
				raw, e := os.ReadFile(stateFile(f.e.d))
				if e != nil {
					t.Fatal(e)
				}
				var st map[string]any
				if e = json.Unmarshal(raw, &st); e != nil {
					t.Fatal(e)
				}
				if st["activeHealth"] != nil || st["lastProxyContext"] != nil || st["consecutiveFailures"] != float64(0) || (kind != "absent-state" && st["preserve"] != "history") {
					t.Fatal("stale health/history publication", st)
				}
				if _, e = os.Lstat(cache); !os.IsNotExist(e) {
					t.Fatal("old interface cache retained", e)
				}
				after := f.process.RuntimeSnapshot()
				if withdraw {
					w, ok := st["withdrawal"].(map[string]any)
					if !ok || w["pending"] != (!manual) || st["activeServerId"] != nil || chosen != "" {
						t.Fatal("withdrawal policy", st)
					}
					if manual {
						if _, e := f.e.s.replacementContext(); e == nil {
							t.Fatal("manual withdrawal permits auto replacement")
						}
					}
					if after.Running || after.Wanted {
						t.Fatal("withdrawal restarted")
					}
				} else {
					if chosen != nextID || st["activeServerId"] != nextID || st["withdrawal"] != nil {
						t.Fatal("replacement state", st)
					}
				}
				if manual && (after.Running || after.Wanted || after.Generation != before.Generation) {
					t.Fatal("manual stop lost")
				}
				if st["enabled"] != (kind != "absent-auto") {
					t.Fatal("auto policy changed", st)
				}
			} else if kind == "bad-state" || kind == "foreign-state" || kind == "bad-auto" || kind == "foreign-cache" {
				prefs, e := readReservePreferences(f.e.d)
				if e != nil || prefs.Revision != prefsBefore.Revision || f.process.RuntimeSnapshot() != before || ActiveID(f.e.d) != oldID {
					t.Fatal("preflight failure changed runtime/roster", e)
				}
			}
			for p, want := range records {
				got, e := os.ReadFile(p)
				if e != nil || !bytes.Equal(got, want) {
					t.Fatal("owner service deleted catalog", p, e)
				}
			}
			if kind == "absent-auto" {
				if _, e := os.Lstat(autoSwitchFile(f.e.d)); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("absent auto config seeded")
				}
			} else if kind != "auto-drift" {
				got, e := os.ReadFile(autoSwitchFile(f.e.d))
				if e != nil || !bytes.Equal(got, policy) {
					t.Fatal("user auto policy overwritten", e)
				}
			}
			got, e := os.ReadFile(foreign)
			if e != nil || string(got) != "foreign-owned" {
				t.Fatal("foreign state modified", e)
			}
		})
	}
}

func TestSourceOwnerServiceMissingProvider(t *testing.T) {
	svc := &compat.Services{}
	owner, ok := any(svc).(sourceOwnerService)
	if !ok {
		t.Fatal("SOURCE_OWNER_SERVICE_ABSENT")
	}
	if _, err := owner.PrepareSourceRemoval(context.Background(), "source", nil, nil); !errors.Is(err, compat.ErrServiceUnavailable) {
		t.Fatal("missing owner accepted", err)
	}
}

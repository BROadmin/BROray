package servers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"github.com/BROadmin/broray/modules/xray"
)

func TestColdStoppedSourceOperations(t *testing.T) {
	for _, kind := range []string{"replace", "refresh", "same-id", "withdraw", "disable-reserve"} {
		for _, fault := range []string{"none", "missing-hold", "foreign-child", "no-daemon", "hold-drift", "start-attempt", "intent-drift"} {
			t.Run(kind+"/"+fault, func(t *testing.T) { testColdStoppedSourceOperation(t, kind, fault) })
		}
	}
}
func TestColdStoppedSourceOwner(t *testing.T) {
	for _, kind := range []string{"replace", "refresh", "same-id", "withdraw"} {
		for _, fault := range []string{"none", "start-attempt", "hold-drift", "intent-drift"} {
			t.Run(kind+"/"+fault, func(t *testing.T) { testColdStoppedSourceOperation(t, kind, "owner-"+fault) })
		}
	}
}
func testColdStoppedSourceOperation(t *testing.T, kind, fault string) {
	owner := strings.HasPrefix(fault, "owner-")
	fault = strings.TrimPrefix(fault, "owner-")
	f, description := reserveDisableFixture(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if kind != "disable-reserve" {
		must(f.e.s.disableReserve(f.ctx, f.binding, f.plan.revision))
	}
	oldID, nextID, sourceID := f.plan.node.ID, f.backup, "cold-subscription"
	if kind == "same-id" {
		nextID = oldID
	}
	if kind != "disable-reserve" {
		for _, id := range []string{oldID, nextID} {
			r, err := ReadRecord(f.e.d, id)
			must(err)
			if id == oldID || kind == "refresh" {
				r["source"] = map[string]any{"type": "subscription", "subscriptionId": sourceID}
			}
			if kind == "same-id" {
				r["name"] = "B"
			}
			must(WriteRecord(f.e.d, r))
		}
	}
	must(f.process.Stop(f.ctx))
	p := xray.NewProcess(f.e.d.Root)
	f.e.d.App.Xray = p
	before := p.RuntimeSnapshot()
	if before.Running || before.Wanted || before.ExitConfirmed || before.Generation != 0 || before.Intent != 0 || before.LastPID != 0 {
		t.Fatal("not a cold supervisor", before)
	}
	guard, err := platform.TryLock(f.e.d.Root.DaemonLock())
	must(err)
	defer guard.Release()
	f.e.d.DaemonGuard = guard
	// The marker represents the already-verified stopped recovery decision;
	// the separate actual-daemon matrix proves creation across restart.
	config, err := os.ReadFile(f.e.d.Root.XrayConfig())
	must(err)
	record, err := os.ReadFile(RecordPath(f.e.d, oldID))
	must(err)
	hold, err := json.Marshal(map[string]any{"schemaVersion": 1, "operation": strings.Repeat("a", 64), "serverId": oldID, "recordSHA256": compat.SHA256Hex(record), "configSHA256": compat.SHA256Hex(config)})
	must(err)
	holdPath := filepath.Join(f.e.d.Root.Config(), reserve.ProfileBootHoldFile)
	must(os.WriteFile(holdPath, hold, 0600))
	verify := f.e.d.Svc.KeeneticVerifyInterfaceTarget
	f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = verify
	f.e.d.Svc.KeeneticVerifyInterfaceTarget = func(context.Context, string) (compat.OwnedInterfaceTarget, error) {
		return compat.OwnedInterfaceTarget{}, errors.New("cold supervisor has no live listener")
	}
	f.e.d.Svc.KeeneticOrdinaryDescription = func() (string, error) {
		if ActiveID(f.e.d) == nextID {
			return "BROray - B", nil
		}
		return "BROray - A", nil
	}
	f.e.d.Svc.KeeneticPreviewOrdinaryDescription = func(name string) (string, error) {
		if name == "A" {
			return "BROray - A", nil
		}
		if name != "B" {
			return "", errors.New("wrong name")
		}
		return "BROray - B", nil
	}
	f.e.d.Svc.KeeneticSyncOwnedDescription = func(context.Context) error {
		if ActiveID(f.e.d) == nextID {
			*description = "BROray - B"
		} else {
			*description = "BROray - A"
		}
		return nil
	}
	f.e.d.Svc.XrayValidateConfig = func(ctx context.Context, raw []byte) error {
		path := filepath.Join(t.TempDir(), "candidate.json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			return err
		}
		return xray.Test(ctx, f.e.d.Root.XrayBinary(), path)
	}

	originalActive, err := os.ReadFile(ActiveFile(f.e.d))
	must(err)
	originalPrefs, err := os.ReadFile(reservePreferencesFile(f.e.d))
	must(err)
	var stopDone chan error
	switch fault {
	case "missing-hold":
		must(os.Remove(holdPath))
	case "foreign-child":
		foreign, err := platform.TryLock(filepath.Join(f.e.d.Root.Run(), "xray-child.lock"))
		must(err)
		defer foreign.Release()
	case "no-daemon":
		f.e.d.DaemonGuard = nil
	case "hold-drift", "start-attempt", "intent-drift":
		first := true
		f.e.d.Svc.KeeneticVerifyInterfaceConfiguration = func(ctx context.Context, want string) (compat.OwnedInterfaceTarget, error) {
			target, err := verify(ctx, want)
			if first {
				first = false
				switch fault {
				case "hold-drift":
					hold = []byte("foreign stopped decision")
					must(os.WriteFile(holdPath, hold, 0600))
				case "intent-drift":
					stopDone = make(chan error, 1)
					go func() { stopDone <- p.Stop(f.ctx) }()
					deadline := time.Now().Add(2 * time.Second)
					for p.RuntimeSnapshot().Intent == before.Intent {
						if time.Now().After(deadline) {
							t.Fatal("manual stop intent not observed")
						}
						time.Sleep(time.Millisecond)
					}
				case "start-attempt":
					for _, start := range []func(context.Context) error{p.Start, p.StartAtBoot} {
						if err := start(ctx); err == nil {
							t.Fatal("launch admitted during cold metadata operation")
						}
					}
					if p.RuntimeSnapshot() != before {
						t.Fatal("rejected launch changed lifecycle intent")
					}
				}
			}
			return target, err
		}
	}
	if owner {
		switch kind {
		case "replace":
			_, err = f.e.s.PrepareSourceRemoval(f.ctx, sourceID, []string{oldID}, []string{nextID})
		case "same-id":
			_, err = f.e.s.ReapplySourceRefresh(f.ctx, sourceID, oldID, "A")
		case "refresh":
			_, err = f.e.s.PrepareSourceRefresh(f.ctx, sourceID, []string{oldID}, []string{nextID})
		case "withdraw":
			_, err = f.e.s.PrepareSourceRemoval(f.ctx, sourceID, []string{oldID}, nil)
		}
	} else {
		switch kind {
		case "replace":
			err = f.e.s.replaceRemovedSource(f.ctx, sourceID, oldID, nextID)
		case "same-id":
			err = f.e.s.replaceSourceCandidateDescription(f.ctx, sourceID, oldID, nextID, true, "BROray - A", false, nil)
		case "refresh":
			err = f.e.s.replaceSourceCandidate(f.ctx, sourceID, oldID, nextID, true)
		case "withdraw":
			err = f.e.s.withdrawRemovedSource(f.ctx, sourceID, oldID)
		case "disable-reserve":
			err = f.e.s.disableReserveStopped(f.ctx, f.plan, f.plan.revision)
		}

	}
	if stopDone != nil {
		select {
		case stopErr := <-stopDone:
			must(stopErr)
		case <-time.After(2 * time.Second):
			t.Fatal("manual stop not confirmed after operation release")
		}
	}
	if fault != "none" && fault != "start-attempt" {
		if err == nil {
			t.Fatal("unsafe cold operation accepted", fault)
		}
		expected := before
		if fault == "intent-drift" {
			expected.Intent++
		}
		if p.RuntimeSnapshot() != expected {
			t.Fatal("unsafe operation mutated supervisor")
		}
		for path, want := range map[string][]byte{f.e.d.Root.XrayConfig(): config, ActiveFile(f.e.d): originalActive, reservePreferencesFile(f.e.d): originalPrefs} {
			got, err := os.ReadFile(path)
			must(err)
			if !bytes.Equal(got, want) {
				t.Fatal("rejected operation changed files", path)
			}
		}
		pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root)
		must(err)
		if pending {
			t.Fatal("admission refusal created journal")
		}
		if fault != "missing-hold" {
			got, err := os.ReadFile(holdPath)
			must(err)
			if !bytes.Equal(got, hold) {
				t.Fatal("foreign hold changed")
			}
		}
		return
	}
	if err != nil {
		t.Fatal("cold stopped operation rejected", err)
	}
	if p.RuntimeSnapshot() != before {
		t.Fatal("cold supervisor intent or generation changed")
	}
	actual, err := os.ReadFile(holdPath)
	must(err)
	if !bytes.Equal(actual, hold) {
		t.Fatal("stopped intent changed")
	}
	pending, err := reserve.TransactionPending(f.ctx, f.e.d.Root)
	must(err)
	if pending {
		t.Fatal("successful operation retained journal")
	}
	switch kind {
	case "replace", "refresh", "same-id":
		if ActiveID(f.e.d) != nextID {
			t.Fatal("replacement not selected")
		}
	case "withdraw":
		if ActiveID(f.e.d) != "" {
			t.Fatal("withdrawal retained selection")
		}
		if _, err := os.Lstat(f.e.d.Root.XrayConfig()); !os.IsNotExist(err) {
			t.Fatal("withdrawal retained config", err)
		}
	case "disable-reserve":
		prefs, err := readReservePreferences(f.e.d)
		must(err)
		if prefs.Enabled || ActiveID(f.e.d) != oldID {
			t.Fatal("disable changed selection or remained enabled")
		}
		actual, err := os.ReadFile(f.e.d.Root.XrayConfig())
		must(err)
		if !bytes.Equal(actual, config) {
			t.Fatal("disable changed config")
		}
	}
	if err := p.StartAtBoot(f.ctx); !errors.Is(err, reserve.ErrProfileBootHeld) {
		t.Fatal("operation lost stopped boot decision", err)
	}
}

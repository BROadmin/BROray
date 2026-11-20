package routepolicy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyInactiveSavePreservesConfig(t *testing.T) {
	for _, exists := range []bool{false, true} {
		root, j, _ := journalEnv(t, exists)
		if err := j.Begin(journalContext, journalNextState(), nil, ""); err != nil {
			t.Fatal(err)
		}
		if err := j.InstallState(journalContext); err != nil {
			t.Fatal(err)
		}
		if err := j.Confirm(journalContext); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(root.XrayConfig())
		if exists && (err != nil || string(got) != string(journalOldConfig)) {
			t.Fatalf("inactive config changed: %s %v", got, err)
		}
		if !exists && !os.IsNotExist(err) {
			t.Fatalf("inactive config created: %v", err)
		}
		assertJournalGone(t, root)
	}
}

func TestPolicyBaselineRejectsChangesBeforeJournal(t *testing.T) {
	for _, change := range []string{"state", "config", "removed-config"} {
		t.Run(change, func(t *testing.T) {
			root, j, _ := journalEnv(t, true)
			state, baseline, err := j.Baseline(journalContext)
			if err != nil {
				t.Fatal(err)
			}
			if state.SchemaVersion != 1 || len(baseline.StateRevision) != 64 || len(baseline.ConfigSHA256) != 64 {
				t.Fatalf("bad baseline: %+v", baseline)
			}
			switch change {
			case "state":
				err = os.WriteFile(filepath.Join(root.Routes(), StateFile), []byte(`{"schemaVersion":1,"rules":[{"id":"foreign","enabled":false,"action":"deny"}]}`), 0600)
			case "config":
				err = os.WriteFile(root.XrayConfig(), []byte(`{"foreign":true}`), 0600)
			case "removed-config":
				err = os.Remove(root.XrayConfig())
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := j.BeginChecked(journalContext, journalNextState(), journalNextConfig, "", baseline); err == nil {
				t.Fatal("stale preview accepted")
			}
			assertJournalGone(t, root)
		})
	}
}

func TestPolicyBaselineRoundTripAndPendingRefusal(t *testing.T) {
	root, j, _ := journalEnv(t, true)
	_, baseline, err := j.Baseline(journalContext)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.BeginChecked(journalContext, journalNextState(), nil, "", baseline); err != nil {
		t.Fatal(err)
	}
	if _, _, err := j.Baseline(journalContext); err == nil {
		t.Fatal("pending transaction offered writable baseline")
	}
	if err := j.InstallState(journalContext); err != nil {
		t.Fatal(err)
	}
	if err := j.Confirm(journalContext); err != nil {
		t.Fatal(err)
	}
	state, after, err := j.Baseline(journalContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Rules) != 1 || after.StateRevision == baseline.StateRevision || after.ConfigSHA256 != baseline.ConfigSHA256 {
		t.Fatalf("unexpected result: %+v", after)
	}
	assertJournalGone(t, root)
}

package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/internal/update"
)

func TestPrunePreservesAmbiguousVersionDirectory(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := os.Mkdir(root.Versions(), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root.Versions(), "3.4.0")
	if err := os.Mkdir(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(foreign, "broray")
	original := []byte("#!/bin/sh\necho foreign\n")
	if err := os.WriteFile(bin, original, 0o755); err != nil {
		t.Fatal(err)
	}

	prune(root, update.NewEmitter(io.Discard, "test"), nil, "3.5.0-dev5", "3.5.0-dev6")

	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("unverified version directory was removed: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("unverified binary changed: %q", got)
	}
}

func TestPruneDeletesOnlyPreviouslyCommittedVersion(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	makeVersion := func(name string, content []byte) string {
		t.Helper()
		dir := filepath.Join(root.Versions(), name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "broray")
		if err := os.WriteFile(bin, content, 0o755); err != nil {
			t.Fatal(err)
		}
		return bin
	}
	old := []byte("#!/bin/sh\necho old\n")
	oldBin := makeVersion("3.4.0", old)
	foreign := []byte("#!/bin/sh\necho foreign\n")
	foreignBin := makeVersion("3.3.0", foreign)
	makeVersion("3.5.0-dev5", []byte("current-old"))
	makeVersion("3.5.0-dev6", []byte("current-new"))
	if err := os.Symlink(filepath.Join("versions", "3.5.0-dev6"), root.Current()); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(old)
	prior := &Journal{SchemaVersion: 1, State: JCommitted, Mode: "switch", From: "3.4.0", To: "3.5.0-dev5",
		PreviousTarget: filepath.Join("versions", "3.4.0"), NewTarget: filepath.Join("versions", "3.5.0-dev5"),
		PreviousSHA256: hex.EncodeToString(sum[:])}
	if err := platform.WriteJSONAtomic(root.UpdateJournal(), prior, 0o600); err != nil {
		t.Fatal(err)
	}

	prune(root, update.NewEmitter(io.Discard, "test"), prior, "3.5.0-dev5", "3.5.0-dev6")

	if _, err := os.Stat(oldBin); !os.IsNotExist(err) {
		t.Fatalf("confirmed prior version was not removed: %v", err)
	}
	got, err := os.ReadFile(foreignBin)
	if err != nil || string(got) != string(foreign) {
		t.Fatalf("unverified version was changed: %q, %v", got, err)
	}
}

func TestPruneKeepsProofAcrossReinstall(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	makeVersion := func(name string, content []byte) string {
		t.Helper()
		dir := filepath.Join(root.Versions(), name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "broray")
		if err := os.WriteFile(bin, content, 0o755); err != nil {
			t.Fatal(err)
		}
		return bin
	}
	old := []byte("#!/bin/sh\necho old\n")
	oldBin := makeVersion("3.4.0", old)
	makeVersion("3.5.0-dev5", []byte("current-old"))
	makeVersion("3.5.0-dev6", []byte("current-new"))
	foreign := []byte("#!/bin/sh\necho foreign\n")
	foreignBin := makeVersion("3.3.0", foreign)
	if err := os.Symlink(filepath.Join("versions", "3.5.0-dev6"), root.Current()); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(old)
	// The committed B reinstall must carry the ownership proof from A→B.
	// Decode JSON so this test fails on behavior before the schema is added.
	raw, err := json.Marshal(map[string]any{
		"state": "committed", "mode": "replace", "from": "3.5.0-dev5", "to": "3.5.0-dev5",
		"previousTarget": filepath.Join("versions", "3.5.0-dev5"),
		"newTarget":      filepath.Join("versions", "3.5.0-dev5"),
		"ownedVersions":  []map[string]string{{"version": "3.4.0", "sha256": hex.EncodeToString(sum[:])}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var prior Journal
	if err := json.Unmarshal(raw, &prior); err != nil {
		t.Fatal(err)
	}
	prune(root, update.NewEmitter(io.Discard, "test"), &prior, "3.5.0-dev5", "3.5.0-dev6")
	if _, err := os.Stat(oldBin); !os.IsNotExist(err) {
		t.Fatalf("confirmed A remained after B reinstall and C commit: %v", err)
	}
	got, err := os.ReadFile(foreignBin)
	if err != nil || string(got) != string(foreign) {
		t.Fatalf("unverified directory changed: %q, %v", got, err)
	}
}

func TestOwnedVersionProofSurvivesReinstallAndRecovery(t *testing.T) {
	oldSum := sha256.Sum256([]byte("old A"))
	beforeSum := sha256.Sum256([]byte("B before reinstall"))
	afterSum := sha256.Sum256([]byte("B after reinstall"))
	aHash := hex.EncodeToString(oldSum[:])
	bBeforeHash := hex.EncodeToString(beforeSum[:])
	bAfterHash := hex.EncodeToString(afterSum[:])
	a, b, c := "3.4.0", "3.5.0-dev5", "3.5.0-dev6"
	legacy := &Journal{State: JCommitted, Mode: "switch", From: a, To: b,
		PreviousTarget: filepath.Join("versions", a), NewTarget: filepath.Join("versions", b), PreviousSHA256: aHash}
	reinstall := &Journal{State: JCommitted, Mode: "replace", From: b, To: b,
		PreviousTarget: filepath.Join("versions", b), NewTarget: filepath.Join("versions", b),
		OwnedVersions: ownedVersionsForOperation(legacy, b, bBeforeHash, false)}
	if len(reinstall.OwnedVersions) != 1 || reinstall.OwnedVersions[0] != (OwnedVersion{Version: a, SHA256: aHash}) {
		t.Fatalf("reinstall lost legacy A proof: %+v", reinstall.OwnedVersions)
	}
	raw, err := json.Marshal(reinstall)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Journal
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	toC := ownedVersionsForOperation(&persisted, b, bAfterHash, true)
	if len(toC) != 2 || toC[0] != (OwnedVersion{Version: a, SHA256: aHash}) ||
		toC[1] != (OwnedVersion{Version: b, SHA256: bAfterHash}) {
		t.Fatalf("B→C lost version proof: %+v", toC)
	}
	for _, state := range []string{JFailed, JRolledBack} {
		interrupted := &Journal{State: state, Mode: "switch", From: b, To: c,
			PreviousTarget: filepath.Join("versions", b), NewTarget: filepath.Join("versions", c), OwnedVersions: toC}
		retry := ownedVersionsForOperation(interrupted, b, bAfterHash, true)
		if len(retry) != 2 || retry[0] != toC[0] || retry[1] != toC[1] {
			t.Fatalf("%s recovery lost version proof: %+v", state, retry)
		}
	}
}

func TestPrunePreservesChangedPreviouslyOwnedBinary(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	a, b, c := "3.4.0", "3.5.0-dev5", "3.5.0-dev6"
	if err := os.Mkdir(filepath.Join(root.Versions(), a), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root.Versions(), a, "broray")
	changed := []byte("#!/bin/sh\necho changed\n")
	if err := os.WriteFile(bin, changed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("versions", c), root.Current()); err != nil {
		t.Fatal(err)
	}
	oldSum := sha256.Sum256([]byte("#!/bin/sh\necho original\n"))
	prior := &Journal{State: JCommitted, Mode: "replace", From: b, To: b,
		PreviousTarget: filepath.Join("versions", b), NewTarget: filepath.Join("versions", b),
		OwnedVersions: []OwnedVersion{{Version: a, SHA256: hex.EncodeToString(oldSum[:])}}}
	prune(root, update.NewEmitter(io.Discard, "test"), prior, b, c)
	got, err := os.ReadFile(bin)
	if err != nil || string(got) != string(changed) {
		t.Fatalf("changed binary was removed or modified: %q, %v", got, err)
	}
}

func TestRecoverCommittedSwitchFinishesVersionPrune(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	makeVersion := func(name string, data []byte) string {
		t.Helper()
		dir := filepath.Join(root.Versions(), name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "broray")
		if err := os.WriteFile(bin, data, 0o755); err != nil {
			t.Fatal(err)
		}
		return bin
	}
	a, b, c, foreign := "3.4.0", "3.5.0-dev5", "3.5.0-dev6", "3.3.0"
	aData, bData, cData, foreignData := []byte("owned A"), []byte("previous B"), []byte("current C"), []byte("foreign")
	aBin := makeVersion(a, aData)
	bBin := makeVersion(b, bData)
	cBin := makeVersion(c, cData)
	foreignBin := makeVersion(foreign, foreignData)
	if err := os.Symlink(filepath.Join("versions", c), root.Current()); err != nil {
		t.Fatal(err)
	}
	aSum, bSum := sha256.Sum256(aData), sha256.Sum256(bData)
	j := &Journal{Mode: "switch", From: b, To: c, PreviousTarget: filepath.Join("versions", b),
		NewTarget: filepath.Join("versions", c), PreviousSHA256: hex.EncodeToString(bSum[:]),
		OwnedVersions: []OwnedVersion{{Version: a, SHA256: hex.EncodeToString(aSum[:])},
			{Version: b, SHA256: hex.EncodeToString(bSum[:])}}}
	if err := j.set(root, JCommitted); err != nil {
		t.Fatal(err)
	}
	outcome, err := Recover(ApplyOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(aBin); !os.IsNotExist(err) {
		t.Fatalf("committed recovery left obsolete A: %v", err)
	}
	if outcome != RecoverCommitted {
		t.Fatalf("recovery did not report completed pruning: %s", outcome)
	}
	for path, want := range map[string][]byte{bBin: bData, cBin: cData, foreignBin: foreignData} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatalf("recovery changed retained binary %s: %q, %v", path, got, err)
		}
	}
	if again, err := Recover(ApplyOptions{Root: root}); err != nil || again != RecoverNothing {
		t.Fatalf("repeat recovery changed committed state: %s, %v", again, err)
	}
}

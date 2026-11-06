package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommitKeepsRollbackBinaryWhenJournalWriteFails(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-old")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(verDir, "broray.old")
	if err := os.WriteFile(old, []byte("KNOWN_PREVIOUS_BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA256(old)
	if err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "replace", From: "3.5.0-old", To: "3.5.0-old", PreviousSHA256: sha}
	if err := j.set(root, JStarted); err != nil {
		t.Fatal(err)
	}

	// Preserve the existing durable journal separately, then make the terminal
	// atomic rename fail. This models a journal-write failure after readiness.
	journal := root.UpdateJournal()
	saved := journal + ".saved"
	if err := os.Rename(journal, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(journal, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := commit(root, j); err == nil {
		t.Fatal("commit succeeded despite blocked journal write")
	}
	if b, err := os.ReadFile(old); err != nil || string(b) != "KNOWN_PREVIOUS_BINARY" {
		t.Fatalf("failed commit removed rollback binary: %q, %v", b, err)
	}
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, journal); err != nil {
		t.Fatal(err)
	}
	got, err := LoadJournal(root)
	if err != nil || got == nil || got.State != JStarted {
		t.Fatalf("durable journal did not remain recoverable: %+v, %v", got, err)
	}
}

func TestCommitKeepsInitBackupWhenJournalWriteFails(t *testing.T) {
	root, _ := fixture(t)
	backup := filepath.Join(root.UpdateState(), "S24broray.prev")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte(renderInitScript(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "switch", From: "3.5.0-old", To: "3.5.0-new", InitBackup: backup}
	if err := j.set(root, JStarted); err != nil {
		t.Fatal(err)
	}
	journal := root.UpdateJournal()
	saved := journal + ".saved"
	if err := os.Rename(journal, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(journal, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := commit(root, j); err == nil {
		t.Fatal("commit succeeded despite blocked journal write")
	}
	if b, err := os.ReadFile(backup); err != nil || string(b) != renderInitScript(root) {
		t.Fatalf("failed commit removed init backup: %q, %v", b, err)
	}
}

func TestRecoverCleansOwnedCopiesAfterDurableCommit(t *testing.T) {
	root, _ := fixture(t)
	verDir := filepath.Join(root.Versions(), "3.5.0-old")
	if err := os.Mkdir(verDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(verDir, "broray.old")
	if err := os.WriteFile(old, []byte("KNOWN_PREVIOUS_BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA256(old)
	if err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "replace", From: "3.5.0-old", To: "3.5.0-old", PreviousSHA256: sha}
	if err := j.set(root, JCommitted); err != nil {
		t.Fatal(err)
	}
	outcome, err := Recover(ApplyOptions{Root: root})
	if err != nil || outcome != RecoverCommitted {
		t.Fatalf("recover did not clean committed rollback copy: %s, %v", outcome, err)
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatalf("owned rollback copy remains after recovery: %v", err)
	}
	if outcome, err := Recover(ApplyOptions{Root: root}); err != nil || outcome != RecoverNothing {
		t.Fatalf("repeat recovery changed terminal state: %s, %v", outcome, err)
	}
}

func TestRecoverRejectsEscapingCommittedTarget(t *testing.T) {
	root, _ := fixture(t)
	outside := filepath.Join(root.Dir, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(outside, "broray.old")
	if err := os.WriteFile(foreign, []byte("FOREIGN_BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA256(foreign)
	if err != nil {
		t.Fatal(err)
	}
	j := &Journal{Mode: "replace", From: "3.5.0-old", To: "../outside", PreviousSHA256: sha}
	if err := j.set(root, JCommitted); err != nil {
		t.Fatal(err)
	}
	if outcome, err := Recover(ApplyOptions{Root: root}); err == nil {
		t.Fatalf("escaping committed target accepted: %s", outcome)
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "FOREIGN_BINARY" {
		t.Fatalf("recovery removed foreign binary: %q, %v", b, err)
	}
}

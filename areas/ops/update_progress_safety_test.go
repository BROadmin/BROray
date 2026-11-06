package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpawnDetachedPreservesPreexistingOutputLinks(t *testing.T) {
	for _, linkedOutput := range []string{"progress", "log"} {
		t.Run(linkedOutput, func(t *testing.T) {
			a := testArea(t)
			dir := a.d.Path("state", "update")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			foreign := filepath.Join(t.TempDir(), "foreign")
			if err := os.WriteFile(foreign, []byte("preserve"), 0o600); err != nil {
				t.Fatal(err)
			}
			progress := filepath.Join(dir, "update-20261103000000-123.jsonl")
			log := filepath.Join(dir, "update-20261103000000-123.log")
			linked := progress
			if linkedOutput == "log" {
				linked = log
			}
			if err := os.Symlink(foreign, linked); err != nil {
				t.Fatal(err)
			}

			err := a.spawnDetached([]string{"-test.run=^$", "-test.v"}, progress, log)
			if err == nil {
				t.Error("accepted a preexisting output symlink")
			}
			data, readErr := os.ReadFile(foreign)
			if readErr != nil || string(data) != "preserve" {
				t.Errorf("foreign output changed: %q, %v", data, readErr)
			}
		})
	}
}

func TestPageOperationIDsDifferWithinOneSecond(t *testing.T) {
	a := testArea(t)
	a.now = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	first := a.pageOperationID("update")
	second := a.pageOperationID("update")
	if first == second {
		t.Fatalf("two operations reused %q", first)
	}
	if !strings.HasPrefix(first, "update-20261103120000-") ||
		!strings.HasPrefix(second, "update-20261103120000-") {
		t.Fatalf("unexpected operation IDs: %q, %q", first, second)
	}
}

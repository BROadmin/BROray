package xray

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BROadmin/broray/areas/servers"
)

func writeActiveForCoreTest(h *harness, id string) error {
	p := servers.ActiveFile(h.d)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(id+"\n"), 0o600)
}

func TestCoreDowngradeActiveProfile(t *testing.T) {
	h := newHarness(t, true)
	writeBinary(t, h.d.Root.XrayBinary(), "26.11.9")
	rec, err := servers.ParseLink("trojan://synthetic@example.test:443?fp=hellochrome_155", "minimum-core", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := servers.WriteRecord(h.d, rec); err != nil {
		t.Fatal(err)
	}
	// Only an active profile blocks the downgrade; inactive profiles are retained.
	if err := writeActiveForCoreTest(h, "minimum-core"); err != nil {
		t.Fatal(err)
	}
	archive := zipWith(t, "26.10.31")
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	fakeGitHub(t, []fakeRel{{tag: "v26.10.31", published: "2026-10-31T00:00:00Z", archive: archive}})
	req := installRequest{Tag: "v26.10.31", CurrentVersion: "26.11.9", ArchiveSHA256: digest, AllowDowngrade: true, AllowUntested: true, AllowPartial: true}
	_, _, err = h.s.selectTarget(context.Background(), "install", req)
	if err == nil || !strings.Contains(err.Error(), "26.11.9") {
		t.Fatalf("unsupported downgrade selected: %v", err)
	}
	if err := writeActiveForCoreTest(h, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.selectTarget(context.Background(), "install", req); err != nil {
		t.Fatalf("inactive profile blocks downgrade: %v", err)
	}
}

package xray

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDPIRecoveredStoppedProfileBlocksAreaBoot(t *testing.T) {
	h := newHarness(t, true)
	active := h.d.Path("config", "active-server")
	if err := os.MkdirAll(filepath.Dir(active), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, []byte("stopped"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.d.Root.Config(), "server-dpi-boot-hold.v1.json")
	raw := `{"schemaVersion":1,"operation":"` + strings.Repeat("a", 64) + `","serverId":"stopped","recordSHA256":"` + strings.Repeat("b", 64) + `","configSHA256":"` + strings.Repeat("c", 64) + `"}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	h.s.bootStart(context.Background())
	if s := h.d.App.Xray.RuntimeSnapshot(); s.Wanted || s.Running {
		t.Fatal("area boot ignored recovered stopped intent", s)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("boot removed durable stopped intent", err)
	}
	if _, err := h.s.lifecycle(context.Background(), "start"); err != nil {
		t.Fatal("explicit lifecycle start", err)
	}
	if s := h.d.App.Xray.RuntimeSnapshot(); !s.Wanted || !s.Running {
		t.Fatal("explicit lifecycle did not start", s)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("explicit lifecycle retained stop hold", err)
	}
}

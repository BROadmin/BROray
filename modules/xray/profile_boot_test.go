package xray

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func profileBootFixture(t *testing.T, p *Process) string {
	t.Helper()
	path := filepath.Join(p.root.Config(), "server-dpi-boot-hold.v1.json")
	raw := `{"schemaVersion":1,"operation":"` + strings.Repeat("a", 64) + `","serverId":"stopped","recordSHA256":"` + strings.Repeat("b", 64) + `","configSHA256":"` + strings.Repeat("c", 64) + `"}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDPIProfileManualStartRetiresStoppedIntent(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	path := profileBootFixture(t, p)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := p.RuntimeSnapshot(); !s.Wanted || !s.Running || s.Generation != 1 {
		t.Fatal("manual launch not owned", s)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("manual launch left stopped hold", err)
	}
}

func TestDPIProfileAutomaticStartWithoutHold(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	if err := p.StartAtBoot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := p.RuntimeSnapshot(); !s.Wanted || !s.Running {
		t.Fatal("ordinary boot blocked", s)
	}
}

func TestDPIProfileFailedManualStartRetainsRequestedIntent(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	path := profileBootFixture(t, p)
	p.Binary = filepath.Join(t.TempDir(), "missing-core")
	if err := p.Start(context.Background()); err == nil {
		t.Fatal("missing core started")
	}
	if s := p.RuntimeSnapshot(); !s.Wanted || s.Running {
		t.Fatal("manual request intent lost", s)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("failed manual launch retained superseded stop", err)
	}
}

func TestDPIProfileRepeatedBootKeepsHold(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	path := profileBootFixture(t, p)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		other := NewProcess(p.root)
		stopObservedProcess(t, other)
		if err := other.StartAtBoot(context.Background()); err == nil {
			t.Fatal("new process ignored hold")
		}
		if s := other.RuntimeSnapshot(); s.Wanted || s.Running || s.Generation != 0 {
			t.Fatal(s)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("boot consumed hold", err)
	}
}

func TestDPIProfileAutomaticStartPreservesStoppedIntent(t *testing.T) {
	p := testProcess(t, "exec sleep 60")
	stopObservedProcess(t, p)
	path := profileBootFixture(t, p)
	boot, ok := any(p).(interface{ StartAtBoot(context.Context) error })
	if !ok {
		t.Fatal("atomic automatic-start guard absent")
	}
	if err := boot.StartAtBoot(context.Background()); err == nil {
		t.Fatal("automatic start accepted held profile")
	}
	if s := p.RuntimeSnapshot(); s.Wanted || s.Running || s.Generation != 0 {
		t.Fatal("automatic start changed stopped intent", s)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("automatic start removed hold", err)
	}
}

package xray

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// DEV1_DEFAULT_XRAY_VERSION_NOT_PINNED: a cold install (no config file) must
// resolve to a pinned tag with a digest for this architecture.
func TestDefaultConfigResolvesToPinnedDigest(t *testing.T) {
	cfg := platform.DefaultConfig()
	tag, err := ResolveTag(cfg.Xray.Version)
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	inst := NewInstaller(platform.Root{Dir: t.TempDir()}, cfg.Xray.Repo, tag)
	for _, arch := range []string{"Xray-linux-arm64-v8a.zip", "Xray-linux-arm32-v7a.zip", "Xray-linux-mips32le.zip", "Xray-linux-mips32.zip"} {
		if Pinned[tag][arch] == "" {
			t.Errorf("%s: no digest for %s", tag, arch)
		}
	}
	if _, _, err := inst.Expected(); err != nil && CurrentAssetErr() == nil {
		t.Fatalf("expected: %v", err)
	}
}

func TestResolveTagRefusesUnpinned(t *testing.T) {
	if _, err := ResolveTag("v25.10.15"); err == nil {
		t.Fatal("unpinned explicit tag accepted")
	}
	if tag, err := ResolveTag("v26.10.31"); err != nil || tag != "v26.10.31" {
		t.Fatalf("pinned tag: %q %v", tag, err)
	}
}

func TestCompareTags(t *testing.T) {
	if compareTags("v26.10.31", "v26.9.30") <= 0 || compareTags("v26.9.9", "v26.10.1") >= 0 {
		t.Fatal("numeric tag compare")
	}
}

func CurrentAssetErr() error { _, err := CurrentAsset(); return err }

func testProcess(t *testing.T, script string) *Process {
	t.Helper()
	dir := t.TempDir()
	root := platform.Root{Dir: dir}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	p := NewProcess(root)
	p.Binary = "/bin/sh"
	p.Args = []string{"-c", script}
	return p
}

// Stop on a cooperative child: TERM, reaped, nil.
func TestStopTermReaped(t *testing.T) {
	p := testProcess(t, "trap 'exit 0' TERM; while :; do sleep 0.05; done")
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	pid := p.Status(context.Background()).PID
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.Running() {
		t.Fatal("still running")
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("pid %d still exists after confirmed stop", pid)
	}
}

// A child that ignores TERM is killed after the grace period and reaped.
func TestStopEscalatesToKill(t *testing.T) {
	old := StopTimeouts
	StopTimeouts = [2]time.Duration{300 * time.Millisecond, 2 * time.Second}
	defer func() { StopTimeouts = old }()
	p := testProcess(t, "trap '' TERM; while :; do sleep 0.05; done")
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.Running() {
		t.Fatal("still running")
	}
}

// After the child exits by itself and is reaped, Stop must not signal its
// (possibly reused) PID: signalLocked refuses once exited is set.
func TestNoSignalAfterExit(t *testing.T) {
	p := testProcess(t, "exit 0")
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	c := p.cur
	p.wanted = false
	p.mu.Unlock()
	<-c.done
	if !c.exited {
		t.Fatal("exited flag not set before reap completed")
	}
	// Stop with nothing to stop.
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

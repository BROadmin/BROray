package xray

import (
	"context"
	"fmt"
	"github.com/BROadmin/broray/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOwnedCoreParentHelper(t *testing.T) {
	dir := os.Getenv("XRAY_PARENT_HELPER_ROOT")
	if dir == "" {
		return
	}
	p := NewProcess(platform.Root{Dir: dir})
	p.Binary = "/bin/sleep"
	p.Args = []string{"5"}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ready"), []byte(strconv.Itoa(p.PID())), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
}

func TestOwnedCoreCannotOutliveParent(t *testing.T) {
	root := platform.Root{Dir: t.TempDir()}
	if err := root.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedCoreParentHelper$")
	cmd.Env = append(os.Environ(), "XRAY_PARENT_HELPER_ROOT="+root.Dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(filepath.Join(root.Dir, "ready"))
		pid, _ = strconv.Atoi(string(b))
		if pid > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("helper did not launch core")
	}
	// No signal is ever sent to the orphan PID; the bounded fixture self-exits.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	time.Sleep(200 * time.Millisecond)
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err == nil {
		rest := string(b[strings.LastIndexByte(string(b), ')')+1:])
		fields := strings.Fields(rest)
		if len(fields) > 0 && fields[0] != "Z" {
			t.Errorf("owned Xray survived its daemon: state=%s", fields[0])
			time.Sleep(5 * time.Second)
		}
	}
}

func TestFreshProcessCannotReportAnExistingChildStopped(t *testing.T) {
	p := testProcess(t, "exec sleep 5")
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer p.Stop(context.Background())
	fresh := NewProcess(p.root)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := fresh.Stop(ctx); err == nil {
		t.Fatal("new process object falsely confirmed existing child stopped")
	}
	if err := fresh.Start(context.Background()); err == nil {
		t.Fatal("second core started over live child guard")
	}
	if !p.Running() {
		t.Fatal("fresh object stopped a child it does not own")
	}
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// runStart launches "broray daemon" detached from the caller (own session,
// stdio on /dev/null) and waits until it answers on the control socket.
// It replaces setsid(1), which the BusyBox of KeeneticOS/Entware may lack.
func runStart(root platform.Root) int {
	if code, _, err := ctlCall(root, "GET", "/api/v1/daemon/ping", nil); err == nil && code == 200 {
		fmt.Println("BROray already running")
		return 0
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "broray start: "+err.Error())
		return 1
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "broray start: "+err.Error())
		return 1
	}
	defer devnull.Close()
	cmd := exec.Command(self, "daemon")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "broray start: "+err.Error())
		return 1
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if code, _, err := ctlCall(root, "GET", "/api/v1/daemon/ping", nil); err == nil && code == 200 {
			fmt.Println("BROray started")
			return 0
		}
		select {
		case err := <-exited:
			fmt.Fprintf(os.Stderr, "BROray daemon exited during start (%v); see %s/brorayd.log\n", err, root.Logs())
			return 1
		case <-time.After(500 * time.Millisecond):
		}
	}
	fmt.Fprintf(os.Stderr, "BROray did not start in 30 s; see %s/brorayd.log\n", root.Logs())
	return 1
}

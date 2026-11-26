package xray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

var ErrReserveSelectorUnconfirmed = errors.New("XRAY_RESERVE_SELECTOR_UNCONFIRMED")

const reserveSelector = "broray-reserve"

// ReserveControlSocket is the only endpoint the supervisor accepts for reserve selection.
func ReserveControlSocket(root platform.Root) string {
	return filepath.Join(root.Run(), "reserve-control", "api.sock")
}

// SelectReserveOutboundIfCurrent serializes a bounded local API operation with
// child lifecycle changes. Callers still need their own transaction, roster and
// traffic proof. An error after a call starts can mean the override was applied.
func (p *Process) SelectReserveOutboundIfCurrent(ctx context.Context, expected RuntimeSnapshot, tag string) error {
	return p.reserveOutboundIfCurrent(ctx, expected, tag, true)
}

// VerifyReserveOutboundIfCurrent observes the current hot slot without
// changing the override or allowing the default blocked route to substitute.
func (p *Process) VerifyReserveOutboundIfCurrent(ctx context.Context, expected RuntimeSnapshot, tag string) error {
	return p.reserveOutboundIfCurrent(ctx, expected, tag, false)
}

func (p *Process) reserveOutboundIfCurrent(ctx context.Context, expected RuntimeSnapshot, tag string, change bool) error {
	switch tag {
	case "broray-reserve-slot-1", "broray-reserve-slot-2", "broray-reserve-slot-3", "broray-reserve-blocked":
	default:
		return ErrReserveSelectorUnconfirmed
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if !p.mu.TryLock() {
		return ErrRuntimeChanged
	}
	defer p.mu.Unlock()
	if ctx.Err() != nil || p.runtimeSnapshotLocked() != expected || !expected.Running || !expected.Wanted || expected.ExitConfirmed ||
		expected.PID <= 0 || expected.PID != expected.LastPID || expected.Generation == 0 || expected.Generation == ^uint64(0) ||
		expected.Intent == 0 || expected.Intent == ^uint64(0) {
		return ErrRuntimeChanged
	}
	path := ReserveControlSocket(p.root)
	dir, info, err := reserveControlDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	command := p.cur.cmd.Path
	if !filepath.IsAbs(command) {
		return ErrReserveSelectorUnconfirmed
	}
	currentPeer := func() error {
		live, err := os.Stat(fmt.Sprintf("/proc/%d/exe", expected.PID))
		if err != nil {
			return ErrRuntimeChanged
		}
		installed, err := os.Stat(command)
		if err != nil || !os.SameFile(live, installed) {
			return ErrRuntimeChanged
		}
		current, currentInfo, err := reserveControlDirectory(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer current.Close()
		if !os.SameFile(info, currentInfo) {
			return ErrReserveSelectorUnconfirmed
		}
		return reserveControlPeer(ctx, path, expected.PID)
	}
	if err := currentPeer(); err != nil {
		return err
	}
	address := "--server=unix://" + path
	if change {
		if _, err := reserveAPICommand(ctx, command, "bo", address, "-b", reserveSelector, tag); err != nil {
			return err
		}
		if err := currentPeer(); err != nil {
			return errors.Join(ErrReserveSelectorUnconfirmed, err)
		}
	}
	response, err := reserveAPICommand(ctx, command, "bi", address, reserveSelector)
	if err != nil {
		return err
	}
	if !reserveReadbackMatches(response, tag) {
		return ErrReserveSelectorUnconfirmed
	}
	if err := currentPeer(); err != nil {
		return errors.Join(ErrReserveSelectorUnconfirmed, err)
	}
	return nil
}

// The tagged CLI renders a single named balancer as a text report. Require
// both the selected override and the single fail-closed default to match.
func reserveReadbackMatches(raw []byte, tag string) bool {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 4 || strings.TrimSpace(lines[0]) != "- Selecting Override:" || strings.TrimSpace(lines[2]) != "- Selects:" {
		return false
	}
	override, fallback := strings.Fields(lines[1]), strings.Fields(lines[3])
	return len(override) == 2 && override[0] == "1" && override[1] == tag &&
		len(fallback) == 2 && fallback[0] == "1" && fallback[1] == "broray-reserve-blocked"
}

func reserveControlDirectory(path string) (*os.File, os.FileInfo, error) {
	f, err := platform.OpenDirNoFollow(path)
	if err != nil {
		return nil, nil, ErrReserveSelectorUnconfirmed
	}
	info, err := f.Stat()
	if err == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && info.Mode().Perm() == 0700 && stat.Uid == uint32(os.Geteuid()) {
			return f, info, nil
		}
	}
	_ = f.Close()
	return nil, nil, ErrReserveSelectorUnconfirmed
}

func reserveControlPeer(ctx context.Context, path string, pid int) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return ErrReserveSelectorUnconfirmed
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return ErrReserveSelectorUnconfirmed
	}
	defer conn.Close()
	raw, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		return ErrReserveSelectorUnconfirmed
	}
	match := false
	if err := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		match = err == nil && int(cred.Pid) == pid && cred.Uid == uint32(os.Geteuid())
	}); err != nil || !match {
		return ErrReserveSelectorUnconfirmed
	}
	return nil
}

type reserveAPIOutput struct{ buffer bytes.Buffer }

func (out *reserveAPIOutput) Write(data []byte) (int, error) {
	if out.buffer.Len()+len(data) > 8<<10 {
		return 0, ErrReserveSelectorUnconfirmed
	}
	return out.buffer.Write(data)
}

func reserveAPICommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, append([]string{"api"}, args...)...)
	cmd.Env = childEnv()
	var output reserveAPIOutput
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s command failed", ErrReserveSelectorUnconfirmed, args[0])
	}
	return output.buffer.Bytes(), nil
}

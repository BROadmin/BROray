package xray

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"golang.org/x/sys/unix"
)

func (s *service) ValidateConfig(ctx context.Context, config []byte) error {
	return s.validateConfig(ctx, config, nil)
}

// ValidateStagedConfig validates detached assets without changing the live
// runtime. The caller retains ownership of the borrowed directory descriptor.
func (s *service) ValidateStagedConfig(ctx context.Context, config []byte, assets *os.File) error {
	if assets == nil {
		return errors.New("XRAY_VALIDATION_ASSETS_INVALID")
	}
	return s.validateConfig(ctx, config, assets)
}

func (s *service) validateConfig(ctx context.Context, config []byte, assets *os.File) error {
	if !compat.FenceHeld(ctx) {
		return errors.New("candidate validation requires global fence")
	}
	if len(config) == 0 || len(config) > 4<<20 {
		return errors.New("candidate configuration size invalid")
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.maintenanceBlocksAuto() {
		return errors.New("XRAY_MAINTENANCE_REQUIRED: Завершите восстановление ядра.")
	}
	if !executable(s.d.Root.XrayBinary()) {
		return errors.New("XRAY_NOT_INSTALLED: Исполняемый файл Xray не найден.")
	}
	core, err := openCandidateCore(s.d.Root.XrayBinary())
	if err != nil {
		return err
	}
	defer core.Close()
	before, err := core.Stat()
	if err != nil {
		return err
	}
	coreUnchanged := func() bool {
		visible, e := openCandidateCore(s.d.Root.XrayBinary())
		if e != nil {
			return false
		}
		defer visible.Close()
		now, e := visible.Stat()
		return e == nil && os.SameFile(before, now) && before.Size() == now.Size() && before.ModTime().Equal(now.ModTime()) && before.Mode() == now.Mode()
	}
	var ownedAssets *os.File
	assetsUnchanged := func() bool { return true }
	if assets != nil {
		borrowedInfo, e := assets.Stat()
		if e != nil || !borrowedInfo.IsDir() {
			return errors.New("XRAY_VALIDATION_ASSETS_INVALID")
		}
		ownedAssets, e = platform.OpenDirNoFollow(assets.Name())
		if e != nil {
			return errors.New("XRAY_VALIDATION_ASSETS_INVALID")
		}
		defer ownedAssets.Close()
		ownedInfo, e := ownedAssets.Stat()
		if e != nil || !os.SameFile(borrowedInfo, ownedInfo) {
			return errors.New("XRAY_VALIDATION_ASSETS_CHANGED")
		}
		assetsUnchanged = func() bool {
			visible, e := platform.OpenDirNoFollow(ownedAssets.Name())
			if e != nil {
				return false
			}
			defer visible.Close()
			now, e := visible.Stat()
			return e == nil && os.SameFile(ownedInfo, now)
		}
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	written := make(chan error, 1)
	go func() {
		n, err := writer.Write(config)
		if err == nil && n != len(config) {
			err = io.ErrShortWrite
		}
		writer.Close()
		written <- err
	}()
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "/proc/self/fd/4", "run", "-test", "-c", "/proc/self/fd/3", "-format", "json")
	cmd.ExtraFiles = []*os.File{reader, core}
	cmd.Env = childEnv(s.d.Root.Runtime())
	if ownedAssets != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, ownedAssets)
		cmd.Env = childEnv("/proc/self/fd/5")
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err = cmd.Run()
	reader.Close() // Release a blocked writer even if exec failed before reading.
	writeErr := <-written
	if cctx.Err() != nil {
		return cctx.Err()
	}
	if !assetsUnchanged() {
		return errors.New("XRAY_VALIDATION_ASSETS_CHANGED")
	}
	if !coreUnchanged() {
		return errors.New("XRAY_VALIDATION_CORE_CHANGED")
	}
	// The pinned official core's run -test rejection status is 23. A signal,
	// another exit status or incomplete pipe write does not prove bad input.
	var rejected *exec.ExitError
	if writeErr == nil && errors.As(err, &rejected) && rejected.ExitCode() == 23 {
		return compat.ErrXrayCandidateRejected
	}
	if err != nil || writeErr != nil {
		// Candidates can contain private transport credentials. Do not expose
		// arbitrary core output through the service or its callers' logs.
		return fmt.Errorf("XRAY_CONFIG_INVALID: Конфигурация Xray не прошла проверку: %w", errors.Join(err, writeErr))
	}
	return nil
}

// Keep the exact executable open until validation completes. Passing the
// descriptor prevents a pathname replacement from selecting a different core.
func openCandidateCore(path string) (*os.File, error) {
	parent, err := platform.OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	core := os.NewFile(uintptr(fd), path)
	info, err := core.Stat()
	if err != nil {
		core.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		core.Close()
		return nil, errors.New("XRAY_VALIDATION_CORE_INVALID")
	}
	return core, nil
}

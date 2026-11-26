package servers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
	"github.com/BROadmin/broray/modules/reserve"
	"golang.org/x/sys/unix"
)

func (s *svc) PreviewActiveConfig(ctx context.Context) ([]byte, error) {
	if !compat.FenceHeld(ctx) {
		return nil, errors.New("policy preview requires mutation fence")
	}
	id, err := s.policyActiveID()
	if err != nil {
		return nil, err
	}
	if id == "" {
		if s.d.Svc.IsXrayRunning() {
			return nil, errors.New("running Xray has no confirmed active server")
		}
		return nil, nil
	}
	rec, err := ReadRecord(s.d, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireRuntimeSupport(ctx, rec); err != nil {
		return nil, err
	}
	cfg, err := XrayConfig(rec, s.genSettings(ctx), RecordPath(s.d, id))
	if err != nil {
		return nil, err
	}
	cfg, err = s.d.Svc.ConfigureRouting(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ROUTING_POLICY_INVALID: %w", err)
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return nil, err
	}
	if prefs.Enabled {
		current, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
		if err != nil {
			return nil, err
		}
		endpoint, ok := reserve.DiagnosticEndpoint(current)
		if !ok {
			return nil, errors.New("RESERVE_RUNTIME_UNCONFIRMED: Сначала восстановите резервирование VPN на странице «Серверы».")
		}
		cfg, err = reserve.AttachHealthInbound(cfg, endpoint)
		if err != nil {
			return nil, fmt.Errorf("RESERVE_HEALTH_CONFIG_UNCONFIRMED: %w", err)
		}
	}
	if err := s.testBytes(ctx, cfg); err != nil {
		return nil, errors.New("XRAY_CONFIG_REJECTED: Xray отклонил конфигурацию сервера")
	}
	return cfg, nil
}

// Unlike legacy ActiveID, a failed read must not mean permission to save
// inactive rules while an existing connection still uses the old policy.
func (s *svc) policyActiveID() (string, error) {
	path := ActiveFile(s.d)
	dir, err := platform.OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	var id string
	if !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return "", err
		}
		f := os.NewFile(uintptr(fd), path)
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() > 256 {
			return "", errors.New("invalid active server record")
		}
		data, err := io.ReadAll(io.LimitReader(f, 257))
		if err != nil {
			return "", err
		}
		if len(data) > 256 {
			return "", errors.New("active server record too large")
		}
		id = strings.TrimSuffix(string(data), "\n")
		if id != "" && !ValidID(id) {
			return "", errors.New("invalid active server ID")
		}
	}
	visible, err := platform.OpenDirNoFollow(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	defer visible.Close()
	a, err := dir.Stat()
	if err != nil {
		return "", err
	}
	b, err := visible.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(a, b) {
		return "", errors.New("active server directory changed")
	}
	return id, nil
}

// PreviewBackupBaseConfig never tests against the live asset directory or
// changes the active pointer. The caller validates the fully decorated config.
func (s *svc) PreviewBackupBaseConfig(ctx context.Context, stage *compat.BackupStage) ([]byte, error) {
	if !compat.FenceHeld(ctx) || stage == nil || stage.Live != s.d {
		return nil, errors.New("backup preview requires owned stage and fence")
	}
	for _, rel := range []string{compat.DataRel("servers"), compat.DataRel("config", "active-server")} {
		if err := stage.Copy(rel); err != nil {
			return nil, err
		}
	}
	if err := platform.MkdirAllDurable(filepath.Dir(ActiveFile(stage.St)), 0700); err != nil {
		return nil, err
	}
	staged := &svc{d: stage.St}
	id, err := staged.policyActiveID()
	if err != nil {
		return nil, err
	}
	if id == "" {
		if s.d.Svc.IsXrayRunning() {
			return nil, errors.New("running Xray has no confirmed active server")
		}
		return nil, nil
	}
	rec, err := ReadRecord(stage.St, id)
	if err != nil {
		return nil, err
	}
	if err = s.requireRuntimeSupport(ctx, rec); err != nil {
		return nil, err
	}
	return XrayConfig(rec, s.genSettings(ctx), RecordPath(stage.St, id))
}

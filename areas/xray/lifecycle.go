package xray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/platform"
)

// Waits of xray-control.sh (start/restart: verified PID within 180 s;
// stop: 12 s) and of the config apply (SOCKS inbound listening).
var (
	startWait   = 180 * time.Second
	startSettle = 1500 * time.Millisecond
	stopWait    = 12 * time.Second
	socksWait   = 20 * time.Second
	pollEvery   = 250 * time.Millisecond
)

// waitRunning waits until the child runs and is still alive after the
// settle period (an invalid config exits immediately).
func (s *service) waitRunning(ctx context.Context) bool {
	deadline := time.Now().Add(startWait)
	for time.Now().Before(deadline) {
		if s.d.App.Xray.Running() {
			pid := s.d.App.Xray.PID()
			if sleep(ctx, startSettle) != nil {
				return false
			}
			if s.d.App.Xray.Running() && s.d.App.Xray.PID() == pid {
				return true
			}
			continue
		}
		if sleep(ctx, pollEvery) != nil {
			return false
		}
	}
	return false
}

func (s *service) waitStopped(ctx context.Context) bool {
	deadline := time.Now().Add(stopWait)
	for time.Now().Before(deadline) {
		if !s.d.App.Xray.Running() {
			return true
		}
		if sleep(ctx, pollEvery) != nil {
			return false
		}
	}
	return !s.d.App.Xray.Running()
}

// waitSocks waits for the SOCKS inbound of the active config to listen.
func (s *service) waitSocks(ctx context.Context, wait time.Duration) bool {
	listen, port, ok := SocksInbound(s.d.Root.XrayConfig())
	if !ok {
		return s.d.App.Xray.Running()
	}
	if listen == "" {
		listen = "0.0.0.0"
	}
	deadline := time.Now().Add(wait)
	for {
		if s.d.App.Xray.Running() && SocksListening(listen, port) {
			return true
		}
		if !time.Now().Before(deadline) || sleep(ctx, pollEvery) != nil {
			return false
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// lifecycleResult is broray_xray_action_result + completedAt.
func (s *service) lifecycleResult(action string, success bool) map[string]any {
	var pid any
	running := s.d.App.Xray.Running()
	if n := s.d.App.Xray.PID(); running && n > 0 {
		pid = n
	}
	return map[string]any{"success": success, "action": action, "running": running, "pid": pid, "completedAt": compat.StampUTC(time.Now())}
}

var lifecycleTexts = map[string][2]string{
	"start":   {"XRAY_START_FAILED", "Xray не был запущен."},
	"stop":    {"XRAY_STOP_FAILED", "Xray не был остановлен."},
	"restart": {"XRAY_RESTART_FAILED", "Xray не был перезапущен."},
}

// handleLifecycle implements X2–X4: global lock xray:<action>, then the
// `broray xray <action>` semantics on the supervised process.
func (s *service) handleLifecycle(c *cgi.Req, action string) (any, error) {
	release, ok := s.d.Lock.TryAcquire("xray", "xray:"+action, "xray")
	if !ok {
		return nil, cgi.E(409, "OPERATION_BUSY", "Другая конфликтующая операция BROray уже выполняется.")
	}
	defer release()
	op := s.d.Queue.StartOperation("", "xray_maintenance", "xray:"+action, "USER", "protected", "")
	op.Phase("working")
	ctx := context.WithoutCancel(c.R.Context())
	details, err := s.lifecycle(ctx, action)
	s.invalidateStatus()
	if err != nil {
		op.Finish(errors.New(lifecycleTexts[action][0] + ": " + err.Error()))
		t := lifecycleTexts[action]
		return nil, cgi.Ed(500, t[0], t[1], details)
	}
	op.Finish(nil)
	return s.lifecycleResult(action, true), nil
}

func (s *service) lifecycle(ctx context.Context, action string) (string, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	switch action {
	case "start", "restart":
		v := s.validate(ctx)
		if !v.Success {
			out, _ := v.Output.(string)
			return tailLines(out, 30), errors.New("validate failed")
		}
		var err error
		if action == "restart" && s.d.App.Xray.Running() {
			err = s.d.App.Xray.Restart(ctx)
		} else {
			err = s.d.App.Xray.Start(ctx)
		}
		if err != nil {
			return err.Error(), err
		}
		if !s.waitRunning(ctx) {
			return "Процесс Xray не подтверждён после запуска.", errors.New("not running")
		}
		return "", nil
	case "stop":
		if err := s.d.App.Xray.Stop(ctx); err != nil {
			return err.Error(), err
		}
		if !s.waitStopped(ctx) {
			return "Процесс Xray продолжает работать.", errors.New("still running")
		}
		return "", nil
	}
	return "", fmt.Errorf("unknown action %s", action)
}

// handleCheckConfig is X5.
func (s *service) handleCheckConfig(c *cgi.Req) (any, error) {
	v := s.validate(c.R.Context())
	if !v.Success {
		return nil, cgi.Ed(422, "XRAY_CONFIG_INVALID", "Конфигурация Xray не прошла проверку.", v.Output)
	}
	return map[string]any{"valid": true, "output": v.Output, "configPath": v.Config, "checkedAt": compat.StampUTC(time.Now())}, nil
}

// ApplyConfig implements Services.XrayApplyConfig: test the bytes with
// `xray run -test`, install them atomically, (re)start Xray, wait for the
// SOCKS inbound and roll back to the previous config on failure.
func (s *service) ApplyConfig(ctx context.Context, config []byte) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	defer s.invalidateStatus()
	bin, path := s.d.Root.XrayBinary(), s.d.Root.XrayConfig()
	if !executable(bin) {
		return errors.New("XRAY_NOT_INSTALLED: Исполняемый файл Xray не найден.")
	}
	if err := os.MkdirAll(s.d.Root.Tmp(), 0o700); err != nil {
		return err
	}
	candidate := filepath.Join(s.d.Root.Tmp(), "xray-config.candidate.json")
	if err := platform.WriteFileAtomic(candidate, config, 0o600); err != nil {
		return err
	}
	defer os.Remove(candidate)
	if out, ok := s.runTest(ctx, bin, candidate); !ok {
		return fmt.Errorf("XRAY_CONFIG_INVALID: Конфигурация Xray не прошла проверку. %s", tailLines(out, 20))
	}
	previous, prevErr := os.ReadFile(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := platform.WriteFileAtomic(path, config, 0o600); err != nil {
		return err
	}
	restart := func() error {
		if s.d.App.Xray.Running() {
			return s.d.App.Xray.Restart(ctx)
		}
		return s.d.App.Xray.Start(ctx)
	}
	err := restart()
	if err == nil && !s.waitSocks(ctx, socksWait) {
		err = errors.New("SOCKS inbound did not start listening")
	}
	if err == nil {
		return nil
	}
	// Roll back to the previous configuration.
	if prevErr == nil {
		if werr := platform.WriteFileAtomic(path, previous, 0o600); werr == nil {
			if rerr := restart(); rerr == nil && s.waitSocks(ctx, socksWait) {
				return fmt.Errorf("XRAY_APPLY_FAILED: Новая конфигурация Xray не запустилась; восстановлена предыдущая. %v", err)
			}
		}
		return fmt.Errorf("XRAY_APPLY_FAILED: Новая конфигурация Xray не запустилась; восстановление не подтверждено. %v", err)
	}
	_ = s.d.App.Xray.Stop(ctx)
	_ = os.Remove(path)
	return fmt.Errorf("XRAY_APPLY_FAILED: Конфигурация Xray не запустилась. %v", err)
}

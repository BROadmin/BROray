package servers

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

// WithdrawActive is called with the subscription transaction's global fence.
// It stops use of the removed node without changing the owner's auto policy.
// After commit the normal loop queues replacement when that policy is enabled.
func (s *svc) WithdrawActive(ctx context.Context) (func(context.Context) error, error) {
	previous := ActiveID(s.d)
	if previous == "" {
		return nil, errors.New("ACTIVE_SERVER_MISSING")
	}
	ac, ok := s.readAutoConfig()
	if !ok {
		return nil, errors.New("AUTO_CONFIG_INVALID")
	}
	type snapshot struct {
		path   string
		data   []byte
		exists bool
	}
	var saved []snapshot
	for _, path := range []string{ActiveFile(s.d), stateFile(s.d), s.d.RunPath("interface-status.json"), s.d.Root.XrayConfig()} {
		st, err := os.Lstat(path)
		if os.IsNotExist(err) {
			saved = append(saved, snapshot{path: path})
			continue
		}
		if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
			return nil, errors.New("ACTIVE_STATE_UNREADABLE")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		saved = append(saved, snapshot{path, b, true})
	}
	wasRunning := s.xrayRunning()
	undo := func(recovery context.Context) error {
		for _, f := range saved {
			if f.exists {
				if err := platform.WriteFileAtomic(f.path, f.data, 0o600); err != nil {
					return err
				}
			} else if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if wasRunning && !s.xrayRunning() {
			for _, f := range saved {
				if f.path == s.d.Root.XrayConfig() && f.exists {
					return s.d.Svc.ApplyXrayConfig(recovery, f.data)
				}
			}
			return errors.New("XRAY_PREVIOUS_CONFIG_MISSING")
		}
		return nil
	}
	if wasRunning {
		if err := s.stopXray(ctx); err != nil {
			return undo, err
		}
	}
	for attempt := 0; s.xrayRunning(); attempt++ {
		if attempt >= 9 {
			return undo, errors.New("XRAY_STOP_TIMEOUT")
		}
		select {
		case <-ctx.Done():
			return undo, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	for _, path := range []string{s.d.Root.XrayConfig(), ActiveFile(s.d), s.d.RunPath("interface-status.json")} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return undo, err
		}
	}
	err := s.updateState(func(st map[string]any) bool {
		st["activeServerId"], st["activeHealth"], st["lastProxyContext"] = nil, nil, nil
		st["consecutiveFailures"], st["enabled"] = 0, ac.enabled
		st["withdrawal"] = map[string]any{"serverId": previous, "pending": ac.enabled, "nextAttemptEpoch": 0}
		st["status"], st["lastReason"] = "disabled", "Активный сервер удалён из подписки. Выберите другой сервер."
		if ac.enabled {
			st["status"], st["lastReason"] = "waiting-replacement", "Активный сервер удалён из подписки. Автовыбор ищет замену."
		}
		return true
	})
	return undo, err
}

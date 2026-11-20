package servers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/strictjson"
	"github.com/BROadmin/broray/modules/reserve"
)

type reserveModeContextKey struct{}

func (s *svc) defineReserveActions() {
	s.d.Queue.Define(compat.ActionSpec{Name: compat.ReserveModeAction, Type: "server_operation", Priority: map[string]int{"*": 1}, Stages: []compat.Stage{{Name: "mode", Phase: "switching", Global: true, Protected: true, Run: s.reserveModeStep}}})
	s.d.Queue.Define(compat.ActionSpec{Name: compat.ReserveSwitchAction, Type: "server_operation", Priority: map[string]int{"*": 1}, Stages: []compat.Stage{{Name: "monitor", Phase: "checking", Global: true, Protected: true, Run: s.reserveMonitorStep}}})
}

func (s *svc) handleReserveMode(c *cgi.Req) {
	if _, ok := compat.OpsBody(c); !ok {
		return
	}
	v, err := strictjson.Decode(c.Body())
	m, ok := v.(map[string]any)
	bad := func() {
		cgi.WriteErr(c.W, cgi.E(400, "INVALID_REQUEST", "Некорректные параметры резервирования."))
	}
	if err != nil || !ok || len(m) != 3 {
		bad()
		return
	}
	enabled, eok := m["enabled"].(bool)
	revision, rok := m["expectedRevision"].(string)
	nonce, nok := m["nonce"].(string)
	if !eok || !rok || !nok || len(revision) != 64 || len(nonce) != 32 {
		bad()
		return
	}
	if _, err := hex.DecodeString(revision); err != nil {
		bad()
		return
	}
	mode := "off:"
	if enabled {
		mode = "on:"
	}
	r, coalesced, err := s.d.Queue.Submit(compat.ReserveModeAction, compat.ReserveOperationTarget, "USER", mode+revision, nonce)
	if err != nil {
		compat.WriteOpsError(c.W, err)
		return
	}
	receipt := r.Receipt(coalesced)
	receipt["accepted"] = true
	cgi.WriteJSON(c.W, 202, receipt)
}

func (s *svc) reserveModeStep(t *compat.Task) error {
	parts := strings.Split(t.Context, ":")
	if len(parts) != 2 || (parts[0] != "on" && parts[0] != "off") {
		return stepError(t, "INVALID_REQUEST", "Некорректный режим резервирования.")
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil || prefs.Revision != parts[1] {
		return stepError(t, "SETTINGS_CHANGED", "Настройки резервирования изменились. Обновите состояние.")
	}
	want := parts[0] == "on"
	if want == prefs.Enabled {
		if !want {
			t.Result("Режим резервирования уже установлен.", "")
			return nil
		}
		config, err := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
		if err != nil {
			return s.reserveQueueError(t, err)
		}
		if _, present := reserve.DiagnosticEndpoint(config); present {
			t.Result("Режим резервирования уже установлен.", "")
			return nil
		}
	}
	ctx := context.WithValue(t.Ctx, reserveModeContextKey{}, true)
	if want {
		err = s.enableReserve(ctx, prefs.Revision)
	} else {
		var config []byte
		config, err = reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
		if err == nil {
			endpoint, ok := reserve.DiagnosticEndpoint(config)
			if !ok {
				err = errReserveRuntime
			} else {
				var plan reserveRuntimePlan
				plan, err = s.prepareReserveRuntime(ctx, ActiveID(s.d), endpoint)
				if err == nil {
					if s.d.App != nil && s.d.App.Xray != nil && !s.d.App.Xray.RuntimeSnapshot().Running {
						err = s.disableReserveStopped(ctx, plan, prefs.Revision)
					} else {
						var binding reserveActiveBinding
						binding, err = s.bindReserveRuntime(ctx, plan, nil)
						if err == nil {
							err = s.disableReserve(ctx, binding, prefs.Revision)
						}
					}
				}
			}
		}
	}
	if err != nil {
		return s.reserveQueueError(t, err)
	}
	s.resetReserveMonitor()
	t.Result("Режим резервирования изменён.", "")
	return nil
}

func (s *svc) resetReserveMonitor() {
	m := s.reserveMon
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeEvidence.Store(nil)
	// Cleanup uncertainty is never cleared by changing preferences.
	if m.blocked {
		return
	}
	m.state = reserve.State{}
	m.authority = ""
	m.binding = nil
	m.lastFailure = nil
	m.retryRevision = ""
	m.retryAfter = time.Time{}
}

func (s *svc) reserveQueueError(t *compat.Task, err error) error {
	if errors.Is(err, errReserveProbeCleanup) || errors.Is(err, reserve.ErrProbeWorkspaceUnconfirmed) {
		return errors.Join(errReserveTransactionPending, err)
	}
	if errors.Is(err, errReserveTransactionPending) {
		return err
	}
	if errors.Is(err, errReserveModeRolledBack) {
		t.Result("Режим не изменён. Восстановлены предыдущие настройки резервирования.", "RESERVE_MODE_ROLLED_BACK")
		return err
	}
	if errors.Is(err, errReserveSwitchRolledBack) {
		t.Result("Переключение не удалось. Восстановлена предыдущая конфигурация подключения.", "RESERVE_SWITCH_ROLLED_BACK")
		return err
	}
	t.Result("Состояние подключения не подтверждено. Проверьте серверы и повторите действие.", "RESERVE_RUNTIME_UNCONFIRMED")
	return err
}

func (s *svc) reserveMonitorStep(t *compat.Task) error {
	prefs, err := readReservePreferences(s.d)
	if err != nil || !prefs.Enabled || prefs.Revision != t.Context {
		return stepError(t, "SETTINGS_CHANGED", "Настройки резервирования изменились.")
	}
	result, err := s.monitorReserve(t.Ctx, s.reserveMon)
	if err == nil && result.Intent != nil {
		err = s.switchReserve(t.Ctx, s.reserveMon, result)
		if err != nil && !errors.Is(err, errReserveTransactionPending) && !errors.Is(err, errReserveSwitchRolledBack) {
			s.resetReserveMonitor()
		}
	}
	if err != nil {
		m := s.reserveMon
		m.mu.Lock()
		m.retryRevision = prefs.Revision
		m.retryAfter = s.now().Add(reserve.DefaultPolicy().ActiveInterval)
		m.mu.Unlock()
		return s.reserveQueueError(t, err)
	}
	t.Result("Проверка резервирования завершена.", "")
	return nil
}

func (s *svc) enqueueReserveDue() error {
	if s.d.Queue.Paused() || s.coreMaintenanceBlocked() || s.d.Lock.Busy() {
		return nil
	}
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return err
	}
	if !prefs.Enabled {
		return nil
	}
	m := s.reserveMon
	if !m.mu.TryLock() {
		return nil
	}
	now := s.now()
	backoff := m.retryRevision == prefs.Revision && now.Before(m.retryAfter)
	due := !m.blocked && !backoff && (m.authority == "" || m.binding == nil || m.binding.plan.revision != prefs.Revision || m.scheduler.Due(m.state, now, reserve.DefaultPolicy()))
	m.mu.Unlock()
	if !due {
		return nil
	}
	_, err = s.d.Queue.Enqueue(compat.ReserveSwitchAction, compat.ReserveOperationTarget, "RESERVE_MONITOR", prefs.Revision)
	return err
}

func (s *svc) reserveLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.enqueueReserveDue(); err != nil {
			s.logf("RESERVE_MONITOR_ENQUEUE=UNCONFIRMED")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *svc) reserveStatus() (any, error) {
	prefs, err := readReservePreferences(s.d)
	if err != nil {
		return nil, err
	}
	active := ActiveID(s.d)
	repairRequired := false
	if prefs.Enabled {
		config, e := reserveRuntimeFile(s.d.Root.XrayConfig(), 2<<20)
		if e == nil {
			_, present := reserve.DiagnosticEndpoint(config)
			repairRequired = !present
		}
	}
	m := s.reserveMon
	if !m.mu.TryLock() {
		return map[string]any{"settings": prefs, "activeServerId": active, "paused": s.d.Queue.Paused(), "recoveryRequired": s.d.Lock.Ambiguous(), "runtimeRepairRequired": repairRequired, "checking": true, "state": nil}, nil
	}
	state, err := json.Marshal(m.state)
	blocked := m.blocked
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return map[string]any{"settings": prefs, "activeServerId": active, "paused": s.d.Queue.Paused(), "recoveryRequired": blocked || s.d.Lock.Ambiguous(), "runtimeRepairRequired": repairRequired, "checking": false, "state": json.RawMessage(state)}, nil
}

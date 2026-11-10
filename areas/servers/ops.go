package servers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	"github.com/BROadmin/broray/internal/compat"
)

var errBusy = cgi.E(409, "OPERATION_BUSY", "Другая конфликтующая операция BROray уже выполняется.")

// job is broray_job_begin system "servers:<action>" servers <source> cooperative:
// it takes the global operation fence (busy → 409 OPERATION_BUSY) and shows
// the operation in the operations panel.
func (s *svc) job(action, source string, fn func(ctx context.Context, op *compat.Operation) (any, error)) (any, error) {
	release, ok := s.d.Lock.TryAcquire("servers", "servers:"+action, "")
	if !ok {
		return nil, errBusy
	}
	defer release()
	op := s.d.Queue.StartOperation("", "server_operation", "servers:"+action, source, "cooperative", "")
	ctx, cancel := context.WithCancel(s.d.Ctx)
	defer cancel()
	op.SetCancel(cancel)
	data, err := fn(ctx, op)
	if err == nil && ctx.Err() != nil {
		err = context.Canceled
	}
	if errors.Is(err, context.Canceled) {
		err = cgi.E(409, "OPERATION_CANCELLED", "Операция остановлена. Прежняя оценка сервера сохранена.")
	}
	op.Finish(err)
	return data, err
}

// ---------- activation (broray_server_activate) ----------

func (s *svc) activate(id string) (any, error) {
	if err := validateID(id); err != nil {
		return nil, opFailed(err.Text())
	}
	if !RecordExists(s.d, id) {
		return nil, opFailed("Ошибка: сервер " + id + " не найден")
	}
	return s.job("activate", "USER", func(ctx context.Context, op *compat.Operation) (any, error) {
		return s.activateUnderFence(ctx, op, id)
	})
}

// Both synchronous CLI callers and the queued WebUI hold the global fence.
func (s *svc) activateUnderFence(ctx context.Context, op *compat.Operation, id string) (any, error) {
	if s.coreMaintenanceBlocked() {
		return nil, cgi.E(409, "XRAY_MAINTENANCE_REQUIRED", "Установите Xray или завершите восстановление ядра на странице Xray.")
	}
	op.Phase("checking")
	rec, err := ReadRecord(s.d, id)
	if err != nil {
		return nil, opFailed("Ошибка: неправильный JSON: " + RecordPath(s.d, id))
	}
	if err := s.requireRuntimeSupport(ctx, rec); err != nil {
		return nil, err
	}
	cfg, gerr := XrayConfig(rec, s.genSettings(ctx), RecordPath(s.d, id))
	if gerr != nil {
		return nil, opFailed(gerr.Error())
	}
	if terr := s.testBytes(ctx, cfg); terr != nil {
		return nil, opFailed(strings.TrimSpace(terr.Error() + "\nОшибка: Xray отклонил конфигурацию сервера"))
	}
	op.SetCancelability("protected")
	op.Phase("committing")
	if err := s.applyServer(ctx, id, cfg); err != nil {
		return nil, err
	}
	sum, err := s.summary(ctx)
	if err != nil {
		return nil, err
	}
	pretty, _ := json.MarshalIndent(sum, "", "  ")
	text := fmt.Sprintf("\nАктивирован сервер:\nНазвание: %s\nID сервера: %s\nПротокол: %s\nАдрес: %s:%s\n%s\n",
		jqRaw(alt(rec["name"], rec["id"])), id, jqRaw(rec["protocol"]), jqRaw(rec["address"]), jqRaw(rec["port"]), pretty)
	return map[string]any{"message": text}, nil
}

// applyServer is broray_xray_apply_prepared_server: install + restart through
// the xray area (which rolls back itself), then switch the active pointer
// and resync the Keenetic interface (warning only).
func (s *svc) applyServer(ctx context.Context, id string, cfg []byte) error {
	if err := s.prepareNativeConfig(cfg); err != nil {
		return err
	}
	rec, err := ReadRecord(s.d, id)
	if err != nil {
		return err
	}
	if err := s.requireRuntimeSupport(ctx, rec); err != nil {
		return err
	}
	if err := s.d.Svc.ApplyXrayConfig(ctx, cfg); err != nil {
		s.logf("SERVER_ACTIVATE=FAIL SERVER=%s ERROR=%v", id, err)
		return opFailed("Перезапуск завершился ошибкой. Выполняется откат.\nОшибка: не удалось применить сервер")
	}
	if err := setActiveID(s.d, id); err != nil {
		return opFailed("Ошибка: не удалось записать активный сервер")
	}
	_ = s.ensureInterface(ctx)
	return nil
}

// ReapplyActive regenerates and applies the Xray config of the active server
// (Services.ServersReapplyActive). The caller holds the global fence.
func (s *svc) ReapplyActive(ctx context.Context, reason string) error {
	id := ActiveID(s.d)
	if id == "" {
		return errors.New("ACTIVE_SERVER_MISSING: Активный сервер не выбран.")
	}
	rec, err := ReadRecord(s.d, id)
	if err != nil {
		return errors.New("SERVER_NOT_FOUND: Активный сервер " + id + " не найден.")
	}
	if err := s.requireRuntimeSupport(ctx, rec); err != nil {
		return err
	}
	cfg, err := XrayConfig(rec, s.genSettings(ctx), RecordPath(s.d, id))
	if err != nil {
		return errors.New("SERVER_CONFIG_INVALID: " + strings.ReplaceAll(err.Error(), "\n", " "))
	}
	if err := s.testBytes(ctx, cfg); err != nil {
		return errors.New("XRAY_CONFIG_REJECTED: Xray отклонил конфигурацию сервера")
	}
	if err := s.d.Svc.ApplyXrayConfig(ctx, cfg); err != nil {
		return errors.New("XRAY_APPLY_FAILED: не удалось применить сервер")
	}
	s.logf("SERVER_REAPPLY=OK SERVER=%s REASON=%s", id, reason)
	_ = s.ensureInterface(ctx)
	return nil
}

// ---------- deactivation (broray_server_deactivate) ----------

func (s *svc) deactivate() (any, error) {
	return s.job("deactivate", "USER", func(ctx context.Context, op *compat.Operation) (any, error) {
		op.SetCancelability("protected")
		op.Phase("committing")
		previous := ActiveID(s.d)
		if s.xrayRunning() {
			if err := s.stopXray(ctx); err != nil {
				return nil, opFailed("Ошибка: не удалось остановить Xray")
			}
		}
		for attempt := 0; s.xrayRunning(); attempt++ {
			if attempt+1 >= 10 {
				return nil, opFailed("Ошибка: процесс Xray не остановился")
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		s.removeRoute(ctx)
		if err := s.disableAutoSwitch(); err != nil {
			return nil, opFailed("Ошибка: не удалось выключить автоматическое переключение")
		}
		if err := s.clearWithdrawal(); err != nil {
			return nil, opFailed("Ошибка: не удалось отменить поиск замены")
		}
		for _, p := range []string{ActiveFile(s.d), s.d.RunPath("interface-status.json")} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return nil, opFailed("")
			}
		}
		var prev any
		if previous != "" {
			prev = previous
		}
		return map[string]any{"deactivated": true, "previousActiveServerId": prev, "connectionState": "disabled",
			"autoSwitch": map[string]any{"enabled": false}, "updatedAt": compat.Stamp(s.now())}, nil
	})
}

func (s *svc) removeRoute(ctx context.Context) {
	path := s.d.RunPath("xray-server-route")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	f := strings.Fields(string(b))
	if len(f) >= 2 {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = exec.CommandContext(cctx, "ip", "route", "del", f[0]+"/32", "dev", f[1]).Run()
		cancel()
	}
	_ = os.Remove(path)
}

// disableAutoSwitch is broray_server_disable_auto_switch.
func (s *svc) disableAutoSwitch() error {
	if err := os.MkdirAll(s.d.Path("config", "system"), 0o700); err != nil {
		return err
	}
	now := compat.Stamp(s.now())
	cfg, ok := readObjectFile(autoSwitchFile(s.d))
	if ok {
		cfg["enabled"], cfg["updatedAt"] = false, now
	} else {
		cfg = map[string]any{"schemaVersion": 1, "enabled": false, "failureThreshold": 3, "cooldownMinutes": 10,
			"minimumRating": "acceptable", "selectionRule": "manual", "preferredServerId": nil, "updatedAt": now}
	}
	return compat.WriteJSON(autoSwitchFile(s.d), cfg)
}

// ---------- delete (broray_server_delete_safe) ----------

func (s *svc) deleteServer(id string) (any, error) {
	if err := validateID(id); err != nil {
		return nil, opFailed(err.Text())
	}
	if !RecordExists(s.d, id) {
		return nil, opFailed("Ошибка: сервер " + id + " не найден")
	}
	if id == ActiveID(s.d) {
		return nil, opFailed("Ошибка: активный сервер сначала необходимо отключить или заменить")
	}
	return s.job("delete", "USER", func(ctx context.Context, op *compat.Operation) (any, error) {
		op.SetCancelability("protected")
		op.Phase("committing")
		if err := DeleteRecord(s.d, id); err != nil {
			return nil, opFailed("Ошибка: не удалось удалить сервер " + id)
		}
		return map[string]any{"deleted": true, "id": id, "updatedAt": compat.Stamp(s.now())}, nil
	})
}

// ---------- import (preview: import.cgi:26-69, commit: broray_server_import_job) ----------

func (s *svc) preview(uri string) (map[string]any, error) {
	rec, err := ParseLink(uri, "manual-preview", nil)
	if err != nil {
		msg := ""
		if pe, ok := err.(*ParseError); ok {
			lines := pe.Lines
			if len(lines) > 6 {
				lines = lines[len(lines)-6:]
			}
			msg = strings.Join(lines, "\n")
		}
		return nil, cgi.E(400, "SERVER_PREVIEW_FAILED", orStr(msg, "Конфигурация не прошла проверку."))
	}
	key, err := CanonicalKey(rec)
	if err != nil {
		return nil, cgi.E(400, "SERVER_PREVIEW_FAILED", "Конфигурация не прошла проверку.")
	}
	var duplicate any
	ids, _ := RecordIDs(s.d)
	for _, id := range ids {
		st, err := os.Lstat(RecordPath(s.d, id))
		if err != nil || !st.Mode().IsRegular() {
			return nil, cgi.E(400, "SERVER_PREVIEW_FAILED", "Конфигурация не прошла проверку.")
		}
		old, err := readRecordFile(RecordPath(s.d, id))
		if err != nil {
			return nil, cgi.E(400, "SERVER_PREVIEW_FAILED", "Конфигурация не прошла проверку.")
		}
		if k, err := CanonicalKey(old); err == nil && k == key {
			duplicate = "Такая конфигурация уже сохранена: " + jqRaw(alt(old["name"], old["id"])) + "."
			break
		}
	}
	return map[string]any{"protocol": rec["protocol"], "name": rec["name"], "host": rec["address"], "port": rec["port"],
		"transport": rec["network"], "security": rec["security"], "canonical": key, "duplicate": duplicate, "connectionSupport": s.supportViewChecker(s.d.Ctx)(rec), "importWarnings": rec["importWarnings"]}, nil
}

func (s *svc) importCommit(uri string) (any, error) {
	return s.job("import", "USER", func(ctx context.Context, op *compat.Operation) (any, error) {
		op.Phase("parsing")
		id, err := NewManualID(s.d)
		if err != nil {
			return nil, opFailed(err.(*ParseError).Text())
		}
		rec, err := ParseLink(uri, id, nil)
		if err != nil {
			return nil, opFailed(err.(*ParseError).Text())
		}
		if _, err := os.Lstat(RecordPath(s.d, id)); !os.IsNotExist(err) {
			return nil, opFailed("")
		}
		op.SetCancelability("protected")
		op.Phase("committing")
		if err := WriteRecord(s.d, rec); err != nil {
			return nil, cgi.E(503, "OPERATION_UNRESOLVED", "Завершение операции ещё не подтверждено. Блокировка сохранена.")
		}
		s.logf("SERVER_IMPORT=OK SERVER=%s", id)
		return map[string]any{"imported": true, "id": id}, nil
	})
}

// directCheck is the non-queue check.cgi branch (broray_server_check manual).
func (s *svc) directCheck(id string) (any, error) {
	return s.job("check", "USER", func(ctx context.Context, op *compat.Operation) (any, error) {
		op.Phase("checking")
		res, err := s.checkServer(ctx, id, "manual", "")
		if err != nil {
			if errors.Is(err, errMeasureTimeout) {
				return nil, cgi.E(504, "SERVER_CHECK_TIMEOUT", "Время проверки истекло. Прежняя оценка сервера сохранена.")
			}
			return nil, err
		}
		if !res.Success {
			pretty, _ := json.MarshalIndent(res.Object, "", "  ")
			return nil, opFailed(string(pretty))
		}
		return res.Object, nil
	})
}

func (s *svc) publishSnapshot() (any, error) {
	return s.job("quality-batch-complete", "USER", func(ctx context.Context, op *compat.Operation) (any, error) {
		op.SetCancelability("protected")
		op.Phase("committing")
		sum, err := s.summary(ctx)
		if err != nil {
			return nil, err
		}
		n := 0
		if rows, ok := sum["servers"].([]any); ok {
			n = len(rows)
		}
		return map[string]any{"published": true, "totalServers": n, "capturedAt": compat.StampUTC(s.now())}, nil
	})
}

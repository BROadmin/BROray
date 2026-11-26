package compat

import (
	"context"
	"errors"
	"strings"
)

// Only these fixed messages are stored by class and exposed in the diagnostic
// report. An operation can return a server response, URI or credential in its
// error text, even if the error has a familiar prefix.
var diagnosticErrorText = map[string]string{
	"XRAY_APPLY_FAILED":                     "Новая конфигурация Xray не запустилась.",
	"XRAY_CONFIG_INVALID":                   "Конфигурация Xray не прошла проверку.",
	"POLICY_APPLY_FAILED":                   "Не удалось применить изменения маршрутизации.",
	"GEODATA_VPN_CHECK_FAILED":              "Проверка VPN после применения геоданных завершилась ошибкой.",
	"POLICY_RECOVERY_REQUIRED":              "Изменения маршрутизации требуют восстановления.",
	"OPERATION_FAILED":                      "Подробности исходной ошибки недоступны.",
	"OPERATION_TIMEOUT":                     "Истекло время выполнения операции.",
	"XRAY_CANDIDATE_REJECTED":               "Xray отклонил новую конфигурацию.",
	"XRAY_CONFIG_REJECTED":                  "Xray отклонил конфигурацию сервера.",
	"XRAY_RUNTIME_CHANGED":                  "Рабочий процесс Xray изменился во время операции.",
	"XRAY_STOP_UNCONFIRMED":                 "Остановка рабочего процесса Xray не подтверждена.",
	"RESERVE_RUNTIME_UNCONFIRMED":           "Не удалось подтвердить состояние подключения.",
	"RESERVE_NO_REACHABLE_SERVER":           "Ни один доступный для выбора сервер не подтвердил соединение.",
	"RESERVE_TRANSACTION_RECOVERY_REQUIRED": "Требуется восстановление незавершённого изменения резервирования.",
	"SETTINGS_CHANGED":                      "Настройки изменились во время операции.",
	"SERVER_CATALOG_CHANGED":                "Список серверов изменился во время операции.",
	"SERVER_SOURCE_REMOVE_FAILED":           "Не удалось подтвердить удаление серверов источника.",
	"SUBSCRIPTION_PROFILE_CHANGED":          "Ожидающие параметры или активный сервер изменились.",
	"SUBSCRIPTION_PROFILE_APPLY_FAILED":     "Не удалось применить ожидающие параметры подписки.",
	"SUBSCRIPTION_PROFILE_VPN_ROLLED_BACK":  "Связь с новыми параметрами подписки не подтверждена.",
	"SUBSCRIPTION_FETCH_FAILED":             "Не удалось получить данные подписки.",
	"SUBSCRIPTION_DEVICE_LIMIT_REACHED":     "Достигнут лимит устройств подписки.",
	"SUBSCRIPTION_DEVICE_ID_REJECTED":       "Провайдер отклонил идентификатор клиента подписки.",
	"ROUTING_POLICY_INVALID":                "Не удалось сформировать действующую политику маршрутизации.",
	"GEODATA_SCHEDULE_UNCONFIRMED":          "Не удалось подтвердить настройки обновления геоданных.",
}

// XrayConfigRollback carries previous-config evidence separately from VPN
// traffic proof. Err retains the original error handling contract.
type XrayConfigRollback struct {
	Err      error
	Restored bool
}

func (e *XrayConfigRollback) Error() string { return e.Err.Error() }
func (e *XrayConfigRollback) Unwrap() error { return e.Err }

// PreserveXrayRollback keeps structured evidence when an area substitutes its
// existing user-visible error for the lower-level diagnostic.
func PreserveXrayRollback(original, public error) error {
	var rollback *XrayConfigRollback
	if errors.As(original, &rollback) {
		return &XrayConfigRollback{Err: public, Restored: rollback.Restored}
	}
	return public
}

func diagnosticCause(err error, budget *int) string {
	if err == nil || *budget <= 0 {
		return ""
	}
	*budget = *budget - 1
	switch nested := err.(type) {
	case *XrayConfigRollback:
		return "XRAY_APPLY_FAILED"
	case interface{ Unwrap() []error }:
		fallback := ""
		for _, cause := range nested.Unwrap() {
			if code := diagnosticCause(cause, budget); code != "" {
				if code != "RESERVE_TRANSACTION_RECOVERY_REQUIRED" {
					return code
				}
				fallback = code
			}
			if *budget <= 0 {
				break
			}
		}
		return fallback
	case interface{ Unwrap() error }:
		if code := diagnosticCause(nested.Unwrap(), budget); code != "" {
			return code
		}
	}
	if err == context.DeadlineExceeded {
		return "OPERATION_TIMEOUT"
	}
	code, _, _ := strings.Cut(err.Error(), ":")
	if code != "OPERATION_FAILED" && diagnosticErrorText[code] != "" {
		return code
	}
	return ""
}

func diagnosticAction(action string) any {
	switch action {
	case ReserveModeAction, ReserveSelectAction, ReserveSwitchAction, SourceRefreshAction, SourceRemovalAction, SourceProfileApplyAction,
		"servers:activate", "servers:deactivate", "servers:active-health", "servers:check", "servers:failover",
		"servers:quality", "routes:geodata", "routes:policy":
		return action
	default:
		return nil
	}
}

func failureReport(ev map[string]any) map[string]any {
	if ev["event"] != "failed" {
		return nil
	}
	id, _ := ev["operationId"].(string)
	if !ValidOperationID(id) {
		return nil
	}
	code, _ := ev["failureCode"].(string)
	if diagnosticErrorText[code] == "" {
		code, _ = ev["errorCode"].(string)
		if diagnosticErrorText[code] == "" {
			code = "OPERATION_FAILED"
		}
	}
	action, _ := ev["operationAction"].(string)
	phase, _ := ev["failedPhase"].(string)
	var phaseValue, config, vpn any
	if enumHas(phase, publicPhases...) && phase != "finished" {
		phaseValue = phase
	}
	if ev["configurationRollback"] == "restored" {
		config = "restored"
		vpn = "unconfirmed"
		if ev["vpnRecovery"] == "verified" {
			vpn = "verified"
		}
	} else if ev["configurationRollback"] == "unconfirmed" {
		config, vpn = "unconfirmed", "unconfirmed"
	}
	typ, _ := ev["operationType"].(string)
	publicType := PublicType(typ)
	publicAction := diagnosticAction(action)
	if publicType == "unknown" && publicAction != nil {
		publicType = PublicType(action)
	}
	var configText, vpnText any
	switch config {
	case "restored":
		configText = "Предыдущая конфигурация восстановлена."
	case "unconfirmed":
		configText = "Откат конфигурации не подтверждён."
	}
	switch vpn {
	case "verified":
		vpnText = "Восстановление VPN подтверждено."
	case "unconfirmed":
		vpnText = "Восстановление не подтверждено."
	}
	return map[string]any{"operationId": id, "operationType": publicType, "action": publicAction,
		"failedAt": PublicTimestamp(ev["timestamp"]), "phase": phaseValue, "errorCode": code, "errorText": diagnosticErrorText[code],
		"configurationRollback": config, "configurationRollbackText": configText, "vpnRecovery": vpn, "vpnRecoveryText": vpnText}
}

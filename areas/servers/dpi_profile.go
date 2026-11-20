package servers

import (
	"bytes"
	"encoding/json"
)

const dpiPreset = "tlshello-v1"
const dpiRequiredCoreVersion = "26.11.9"

// This preset is measured by the native TLS-record and real proxy tests.
// The existing transport.finalMask remains the only generator input.
func dpiMask() map[string]any {
	return map[string]any{"tcp": []any{map[string]any{"type": "fragment", "settings": map[string]any{"packets": "tlshello", "length": "100-200", "delay": "1-2", "maxSplit": 64}}}}
}

func dpiJSONEqual(a, b any) bool {
	x, e := json.Marshal(a)
	y, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}

// No merging with imported or externally edited masks is authorized.
// Absence is distinct from an explicitly empty mask. Present null is invalid
// in the existing profile schema and must not be made valid by this feature.
func dpiMetadata(rec Record) (map[string]any, bool, *ParseError) {
	value, present := rec["dpi"]
	if !present {
		return nil, false, nil
	}
	bad := func() (map[string]any, bool, *ParseError) {
		return nil, true, die("состояние DPI не подтверждено: параметры профиля изменены")
	}
	m, ok := value.(map[string]any)
	if !ok || len(m) != 4 || !keysSubset(m, "schema", "preset", "originalMaskPresent", "originalMask") {
		return bad()
	}
	schema, ok := freshUnsigned(m["schema"], 32)
	if !ok || schema != 1 || m["preset"] != dpiPreset {
		return bad()
	}
	originalPresent, ok := m["originalMaskPresent"].(bool)
	if !ok {
		return bad()
	}
	original, hasOriginal := m["originalMask"]
	if !hasOriginal {
		return bad()
	}
	if originalPresent {
		empty, ok := original.(map[string]any)
		if !ok || len(empty) != 0 {
			return bad()
		}
	} else if original != nil {
		return bad()
	}
	transport, ok := rec["transport"].(map[string]any)
	if !ok || !dpiJSONEqual(transport["finalMask"], dpiMask()) {
		return bad()
	}
	return m, true, nil
}

func dpiProfileReason(rec Record) string {
	if has(rec, "importProfile") || !in(asString(rec["protocol"]), "vless", "vmess", "trojan") {
		return "Этот протокол не проверен для DPI."
	}
	network, security := asString(rec["network"]), asString(rec["security"])
	if security == "tls" {
		if !in(network, "raw", "ws", "grpc", "httpupgrade") {
			return "Этот транспорт не проверен для DPI."
		}
	} else if security != "reality" || rec["protocol"] != "vless" || network != "raw" {
		return "Для DPI требуется проверенное сочетание TCP и TLS/REALITY."
	}
	flow := asString(rec["flow"])
	if flow != "" && (flow != "xtls-rprx-vision" || network != "raw") {
		return "Этот режим соединения не проверен для DPI."
	}
	if rec["protocol"] == "vless" && asString(rec["encryption"]) != "none" {
		return "Это шифрование VLESS не проверено для DPI."
	}
	if header := asString(get(rec, "transport", "headerType")); header != "" && header != "none" {
		return "Пользовательский заголовок транспорта несовместим с пресетом DPI."
	}
	return ""
}

func validateDPIRecord(rec Record) *ParseError {
	_, enabled, err := dpiMetadata(rec)
	if err != nil {
		return err
	}
	if enabled {
		if reason := dpiProfileReason(rec); reason != "" {
			return die(reason)
		}
	}
	return nil
}

// prepareDPIRecord only constructs a detached proposal. It does not save a
// profile, start a process, check the installed core, or confirm activation.
// The operation owner must validate the actual core and proxied connection
// before committing the proposal under its transaction/fence.
func prepareDPIRecord(rec Record, enabled bool) (Record, error) {
	raw, err := Marshal(rec)
	if err != nil {
		return nil, err
	}
	next, ok := decodeObject(raw)
	if !ok {
		return nil, die("неправильный профиль DPI")
	}
	meta, current, stateErr := dpiMetadata(next)
	if stateErr != nil {
		return nil, stateErr
	}
	if !enabled && current {
		transport := next["transport"].(map[string]any)
		if meta["originalMaskPresent"] == true {
			transport["finalMask"] = meta["originalMask"]
		} else {
			delete(transport, "finalMask")
		}
		delete(next, "dpi")
	}
	if err := Validate(next, "DPI"); err != nil {
		return nil, err
	}
	if enabled && !current {
		if reason := dpiProfileReason(next); reason != "" {
			return nil, die(reason)
		}
		transport, ok := next["transport"].(map[string]any)
		if !ok {
			return nil, die("отсутствует транспорт профиля DPI")
		}
		original, present := transport["finalMask"]
		if present {
			empty, ok := original.(map[string]any)
			if !ok || len(empty) != 0 {
				return nil, die("пользовательский FinalMask сохранён: автоматический пресет DPI недоступен")
			}
		}
		next["dpi"] = map[string]any{"schema": 1, "preset": dpiPreset, "originalMaskPresent": present, "originalMask": original}
		transport["finalMask"] = dpiMask()
	}
	if err := Validate(next, "DPI"); err != nil {
		return nil, err
	}
	return next, nil
}

func dpiPresentation(rec Record) map[string]any {
	_, enabled, err := dpiMetadata(rec)
	result := map[string]any{"enabled": enabled, "profileCompatible": false, "reason": "", "requiredCoreVersion": dpiRequiredCoreVersion}
	if err != nil {
		result["enabled"] = nil
		result["reason"] = "Состояние DPI не подтверждено."
		return result
	}
	if Validate(rec, "DPI") != nil {
		result["reason"] = "Профиль требует проверки."
		return result
	}
	if reason := dpiProfileReason(rec); reason != "" {
		result["reason"] = reason
		return result
	}
	if !enabled {
		transport, ok := rec["transport"].(map[string]any)
		if !ok {
			result["reason"] = "Отсутствует транспорт профиля."
			return result
		}
		if mask, present := transport["finalMask"]; present {
			empty, ok := mask.(map[string]any)
			if !ok || len(empty) != 0 {
				result["reason"] = "Сохранён пользовательский FinalMask."
				return result
			}
		}
	}
	result["profileCompatible"] = true
	return result
}

// ManagedDPI reports a structurally valid managed choice, not proof that the
// installed core or a running process has accepted that choice.
func ManagedDPI(rec Record) (bool, error) {
	if err := Validate(rec, "DPI"); err != nil {
		return false, err
	}
	_, enabled, err := dpiMetadata(rec)
	if err != nil {
		return false, err
	}
	return enabled, nil
}

// DPIOriginalRecord returns the original connection for subscription matching.
// Callers must retain the actual saved identity separately for catalog ownership.
// It never removes an unrecognized or externally modified mask.
func DPIOriginalRecord(rec Record) (Record, error) {
	return prepareDPIRecord(rec, false)
}

// RefreshDPIRecord transfers only the selected local choice. The caller must
// first prove that both records identify the same server in the same source,
// then validate changed connection parameters through the actual core/proxy
// before persistence. A remote source cannot supply local ownership metadata.
func RefreshDPIRecord(previous, incoming Record) (Record, error) {
	if _, present := incoming["dpi"]; present {
		return nil, die("источник не может задавать локальное состояние DPI")
	}
	enabled, err := ManagedDPI(previous)
	if err != nil {
		return nil, err
	}
	return prepareDPIRecord(incoming, enabled)
}

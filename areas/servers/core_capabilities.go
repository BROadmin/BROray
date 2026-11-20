package servers

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/BROadmin/broray/internal/cgi"
	mxray "github.com/BROadmin/broray/modules/xray"
)

// RequiredCore identifies the tested core requirement, not a stored property.
// Ordinary features use a minimum; managed DPI requires the exact tested
// version in CheckCoreSupport. Old profiles retain their existing policy.
func RequiredCore(rec Record) string {
	if _, present := rec["dpi"]; present {
		return dpiRequiredCoreVersion
	}
	if rec["network"] == "xdrive" || rec["network"] == "masque" || rec["protocol"] == "masque" {
		return "26.11.9"
	}
	var fingerprint any
	switch rec["security"] {
	case "tls":
		fingerprint = tlsSettings(rec)["fingerprint"]
	case "reality":
		fingerprint = realitySettings(rec)["fingerprint"]
	}
	if needsChrome155(fingerprint) {
		return "26.11.9"
	}
	if rec["network"] == "xhttp" {
		block := "transport"
		if rec["protocol"] == "vless" {
			block = "xhttp"
		}
		download := get(rec, block, "extra", "downloadSettings")
		security := asString(get(download, "security"))
		if in(security, "tls", "reality") && needsChrome155(get(download, security+"Settings", "fingerprint")) {
			return "26.11.9"
		}
	}
	return ""
}

func needsChrome155(v any) bool {
	return strings.EqualFold(asString(v), "hellochrome_155")
}

func coreVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if !allDigits(p) || len(p) > 4 {
			return out, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// CheckCoreSupport never echoes input credentials or raw version output.
func CheckCoreSupport(rec Record, installed string) error {
	if _, present := rec["dpi"]; present {
		if err := validateDPIRecord(rec); err != nil {
			return cgi.E(400, "XRAY_DPI_PROFILE_INVALID", "Состояние DPI не подтверждено. Проверьте профиль сервера.")
		}
		if strings.TrimPrefix(installed, "v") != dpiRequiredCoreVersion {
			return cgi.E(400, "XRAY_DPI_CORE_UNVERIFIED", "Для пресета DPI проверен Xray "+dpiRequiredCoreVersion+". Отключите DPI или установите проверенную версию ядра.")
		}
		return nil
	}
	minimum := RequiredCore(rec)
	if minimum == "" {
		return nil
	}
	want, _ := coreVersion(minimum)
	have, ok := coreVersion(installed)
	if !ok {
		return cgi.E(400, "XRAY_CORE_VERSION_UNKNOWN", "Не удалось определить версию Xray. Для этой конфигурации требуется "+minimum+" или новее. Проверьте состояние ядра на странице Xray.")
	}
	if ok {
		for i := range want {
			if have[i] > want[i] {
				return nil
			}
			if have[i] < want[i] {
				ok = false
				break
			}
		}
	}
	if !ok {
		return cgi.E(400, "XRAY_CORE_UNSUPPORTED", "Для этой конфигурации требуется Xray "+minimum+" или новее. Обновите ядро на странице Xray.")
	}
	return nil
}

// A batch resolves the version at most once, only when a profile needs it.
// An unavailable version command must not hold the WebUI for its full timeout.
func (s *svc) runtimeSupportChecker(ctx context.Context) func(Record) error {
	version, loaded := "", false
	return func(rec Record) error {
		if err := requireConnectionSupport(rec); err != nil {
			return err
		}
		if RequiredCore(rec) == "" {
			return nil
		}
		if !loaded {
			loaded = true
			cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			var err error
			version, err = mxray.Version(cctx, s.d.Root.XrayBinary())
			if err != nil {
				version = ""
			}
			cancel()
		}
		return CheckCoreSupport(rec, version)
	}
}

func (s *svc) requireRuntimeSupport(ctx context.Context, rec Record) error {
	return s.runtimeSupportChecker(ctx)(rec)
}

func (s *svc) supportViewChecker(ctx context.Context) func(Record) map[string]any {
	check := s.runtimeSupportChecker(ctx)
	return func(rec Record) map[string]any {
		view := connectionSupport(rec)
		if view["supported"] == false {
			return view
		}
		minimum := RequiredCore(rec)
		if minimum != "" {
			view["minimumCore"] = minimum
			view["mode"] = "supported"
			if err := check(rec); err != nil {
				view["supported"], view["mode"], view["code"] = false, "requires-core", "XRAY_CORE_UNSUPPORTED"
				view["message"] = "Конфигурация сохранена. Для подключения обновите Xray до " + minimum + " или новее."
				if ae, ok := err.(*cgi.APIError); ok && (ae.Code == "XRAY_CORE_VERSION_UNKNOWN" || ae.Code == "XRAY_DPI_CORE_UNVERIFIED" || ae.Code == "XRAY_DPI_PROFILE_INVALID") {
					view["mode"], view["code"], view["message"] = "core-unverified", ae.Code, ae.Message
				}
			}
		}
		if rec["network"] == "xdrive" {
			view["experimental"] = true
			view["notice"] = "XDRIVE проходит публичное тестирование в Xray. Версии ядра на клиенте и сервере должны совпадать. Подробные логи ядра отключены для защиты реквизитов хранилища."
		} else if rec["protocol"] == "masque" {
			view["notice"] = "Подробные логи ядра отключены для защиты реквизитов MASQUE. Результаты операций доступны в BROray."
		}
		return view
	}
}

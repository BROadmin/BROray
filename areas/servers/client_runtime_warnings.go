package servers

import "strings"

const clientAccountLevelWarning = "Уровень пользователя level сохранён; применяется локальная политика Xray в BROray. Политика профиля не применяется."
const clientUTLSTuningWarning = "Настройки версий TLS, шифров, кривых и возобновления сессии сохранены. Xray 26.11.9 не передаёт эти настройки в uTLS при использовании fingerprint; фактические параметры определяет выбранный fingerprint."
const clientRootEndpointWarning = "Адрес и порт верхнего streamSettings сохранены. Xray 26.11.9 использует адрес и порт сервера из settings; верхние streamSettings.address и streamSettings.port не переопределяют назначение. Отдельное назначение downloadSettings XHTTP применяется к загрузочному соединению."

// The pinned uTLS copyConfig omits these fields. The plain Go TLS path
// (fingerprint=unsafe) applies them. This warning never contains source data.
func clientUTLSTuningIgnored(network string, value any) bool {
	m, _ := value.(map[string]any)
	if m == nil || strings.EqualFold(asString(m["fingerprint"]), "unsafe") {
		return false
	}
	// XHTTP and XDRIVE select QUIC only for the exact ALPN=[h3] list.
	if in(network, "xhttp", "xdrive") && jsonValueEqual(m["alpn"], []any{"h3"}) {
		return false
	}
	if network == "masque" {
		// MASQUE uses HTTP/2 iff h2 is present and h3 is absent. All other
		// ALPN combinations choose QUIC with Go TLS, not uTLS.
		alpn, _ := clientStringList(m["alpn"])
		if !in("h2", alpn...) || in("h3", alpn...) {
			return false
		}
	}
	for _, field := range []string{"minVersion", "maxVersion", "cipherSuites", "curvePreferences", "enableSessionResumption"} {
		if has(m, field) {
			return true
		}
	}
	return false
}

func addClientRuntimeWarnings(rec Record) {
	if has(rec, "hysteriaMasquerade") {
		addNativeImportWarning(rec, "Hysteria2: поле masquerade сохранено; оно относится к серверу и не применяется в клиентском подключении.")
	}
	if clientSS2022MetadataIgnored(rec) && (has(rec, "accountLevel") || has(rec, "accountEmail")) {
		addNativeImportWarning(rec, "Поля Shadowsocks 2022 level и email сохранены. Xray 26.11.9 не переносит эти метаданные в клиентскую конфигурацию Shadowsocks 2022; на соединение они не влияют.")
	} else if has(rec, "accountLevel") {
		if in(asString(rec["protocol"]), "http", "socks") && asString(rec["username"]) == "" && rec["proxyAccount"] != true {
			addNativeImportWarning(rec, "Поле level сохранено. HTTP/SOCKS без имени пользователя не создаёт учётную запись: Xray использует локальную политику уровня 0.")
		} else {
			addNativeImportWarning(rec, clientAccountLevelWarning)
		}
	}
	if get(rec, "reality", "show") == true {
		addNativeImportWarning(rec, "REALITY show=true сохранён и включает диагностический вывод Xray.")
	}
	// KCP and Hysteria use Go TLS directly, not the fingerprint uTLS path.
	network := asString(rec["network"])
	ignored := in(network, "raw", "ws", "httpupgrade", "grpc", "xhttp", "masque", "xdrive") && rec["security"] == "tls" && clientUTLSTuningIgnored(network, rec["tls"])
	for _, block := range []string{"transport", "xhttp"} {
		download, _ := get(rec, block, "extra", "downloadSettings").(map[string]any)
		if download != nil && strings.EqualFold(asString(download["security"]), "tls") && clientUTLSTuningIgnored("xhttp", download["tlsSettings"]) {
			ignored = true
		}
	}
	if ignored {
		addNativeImportWarning(rec, clientUTLSTuningWarning)
	}
	// XDRIVE is intentionally absent: its storage-service client consumes
	// streamEndpoint as the fronting address and optional nonzero port.
	if in(network, "raw", "ws", "httpupgrade", "grpc", "xhttp", "kcp", "hysteria", "masque") && has(rec, "streamEndpoint") {
		addNativeImportWarning(rec, clientRootEndpointWarning)
	}
}

// The pinned SS2022 builder copies only address, port, method and key.
func clientSS2022MetadataIgnored(rec Record) bool {
	return rec["protocol"] == "shadowsocks" && strings.HasPrefix(asString(rec["method"]), "2022-")
}

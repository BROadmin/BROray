package servers

import "strings"

const clientTLSCertificateUnavailable = "Конфигурация сохранена. Xray 26.11.9 не отправляет клиентский сертификат TLS (mTLS); подключение с этим параметром пока недоступно."

// This is derived from the private fields each time, including after backup
// restore. A stored capability flag cannot turn an unsupported client TLS
// credential into a runnable profile. The pinned core only installs the
// server-side GetCertificate callback; verify certificates remain usable CAs.
func clientTLSUnavailable(rec Record) string {
	if rec["security"] == "tls" && clientTLSRequiresCertificate(rec["tls"]) {
		return clientTLSCertificateUnavailable
	}
	for _, block := range []string{"transport", "hysteria"} {
		if clientMaskRequiresCertificate(get(rec, block, "finalMask")) {
			return clientTLSCertificateUnavailable
		}
	}
	for _, block := range []string{"transport", "xhttp"} {
		download, _ := get(rec, block, "extra", "downloadSettings").(map[string]any)
		if download == nil {
			continue
		}
		if strings.EqualFold(asString(download["security"]), "tls") && clientTLSRequiresCertificate(download["tlsSettings"]) ||
			clientMaskRequiresCertificate(download["finalmask"]) {
			return clientTLSCertificateUnavailable
		}
	}
	return ""
}

func clientTLSRequiresCertificate(value any) bool {
	certificates, _ := get(value, "certificates").([]any)
	for _, value := range certificates {
		certificate, _ := value.(map[string]any)
		usage := strings.ToLower(asString(certificate["usage"]))
		if !in(usage, "", "encipherment") {
			continue
		}
		cert, _ := certificate["certificate"].([]any)
		key, _ := certificate["key"].([]any)
		if len(cert) > 0 && len(key) > 0 {
			return true
		}
	}
	return false
}

func clientMaskRequiresCertificate(value any) bool {
	items, _ := get(value, "udp").([]any)
	for _, item := range items {
		switch get(item, "type") {
		case "realm":
			if clientTLSRequiresCertificate(get(item, "settings", "tlsConfig")) {
				return true
			}
		case "xdns":
			resolvers, _ := get(item, "settings", "resolvers").([]any)
			for _, resolver := range resolvers {
				if clientTLSRequiresCertificate(get(resolver, "tlsConfig")) {
					return true
				}
			}
		}
	}
	return false
}

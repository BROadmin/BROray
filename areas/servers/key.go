package servers

import (
	"encoding/json"

	"github.com/BROadmin/broray/internal/compat"
)

func lower(v any) any {
	if s, ok := v.(string); ok {
		return asciiOnlyLower(s)
	}
	return v
}

// importKeyObject is the object of broray_server_subscription_import_key
// (server-subscription-service.sh:66-102).
func importKeyObject(s Record) map[string]any {
	var serverName, fingerprint any
	if s["security"] == "reality" {
		serverName = alt(get(s, "reality", "serverName"), "")
		fingerprint = alt(get(s, "reality", "fingerprint"), "chrome")
	} else {
		serverName = alt(get(s, "tls", "serverName"), get(s, "reality", "serverName"), s["sni"], "")
		fingerprint = alt(get(s, "tls", "fingerprint"), get(s, "reality", "fingerprint"), "chrome")
	}
	return map[string]any{
		"protocol":             alt(s["protocol"], ""),
		"address":              lower(alt(s["address"], "")),
		"port":                 alt(s["port"], json.Number("0")),
		"network":              alt(s["network"], get(s, "transport", "type"), ""),
		"security":             alt(s["security"], "none"),
		"uuid":                 alt(s["uuid"], ""),
		"encryption":           alt(s["encryption"], ""),
		"flow":                 alt(s["flow"], ""),
		"alterId":              alt(s["alterId"], json.Number("0")),
		"password":             alt(s["password"], ""),
		"method":               alt(s["method"], ""),
		"auth":                 alt(s["auth"], ""),
		"serverName":           serverName,
		"fingerprint":          fingerprint,
		"alpn":                 alt(get(s, "tls", "alpn"), []any{}),
		"allowInsecure":        alt(get(s, "tls", "allowInsecure"), false),
		"publicKey":            alt(get(s, "reality", "publicKey"), ""),
		"shortId":              alt(get(s, "reality", "shortId"), ""),
		"spiderX":              alt(get(s, "reality", "spiderX"), ""),
		"pinnedPeerCertSha256": alt(get(s, "tls", "pinnedPeerCertSha256"), ""),
		"host":                 alt(get(s, "transport", "host"), get(s, "grpc", "authority"), get(s, "ws", "host"), get(s, "httpupgrade", "host"), ""),
		"path":                 alt(get(s, "xhttp", "path"), get(s, "ws", "path"), get(s, "httpupgrade", "path"), get(s, "transport", "path"), ""),
		"serviceName":          alt(get(s, "grpc", "serviceName"), get(s, "transport", "serviceName"), ""),
		"mode":                 alt(get(s, "xhttp", "mode"), get(s, "transport", "mode"), ""),
		"headerType":           alt(get(s, "transport", "headerType"), "none"),
		"header":               alt(get(s, "transport", "header"), map[string]any{}),
		"extra":                alt(get(s, "xhttp", "extra"), get(s, "transport", "extra"), map[string]any{}),
		"ports":                alt(get(s, "hysteria", "ports"), ""),
		"obfs":                 alt(get(s, "hysteria", "obfs"), ""),
		"obfsPassword":         alt(get(s, "hysteria", "obfsPassword"), ""),
		"finalMask":            alt(get(s, "hysteria", "finalMask"), get(s, "transport", "finalMask"), map[string]any{}),
		"echConfigList":        alt(get(s, "tls", "echConfigList"), ""),
		"verifyPeerCertByName": alt(get(s, "tls", "verifyPeerCertByName"), ""),
		"mldsa65Verify":        alt(get(s, "reality", "mldsa65Verify"), ""),
		"kcp":                  alt(get(s, "transport", "kcp"), map[string]any{}),
		"kcpSeed":              alt(get(s, "transport", "kcpSeed"), ""),
		"kcpLegacy":            alt(get(s, "transport", "kcpLegacy"), false),
	}
}

// CanonicalKey is broray_server_subscription_import_key: the sha256 (64
// lowercase hex) of the canonical (sorted, compact) connection identity of a
// record; the name, id, uri and source are excluded. Equal keys = duplicate.
// The bytes are produced by compat.Canonical (Go is producer and consumer).
func CanonicalKey(rec Record) (string, error) {
	return compat.CanonicalSHA256(importKeyObject(normalize(rec)))
}

// ContinuityKey is broray_server_subscription_continuity_key
// (server-subscription-service.sh:104-119): the endpoint identity used to keep
// a subscription server id across credential rotation.
func ContinuityKey(rec Record) (string, error) {
	s := normalize(rec)
	var serverName any
	if s["security"] == "reality" {
		serverName = alt(get(s, "reality", "serverName"), "")
	} else {
		serverName = alt(get(s, "tls", "serverName"), get(s, "reality", "serverName"), s["sni"], "")
	}
	return compat.CanonicalSHA256(map[string]any{
		"protocol":    alt(s["protocol"], ""),
		"address":     lower(alt(s["address"], "")),
		"port":        alt(s["port"], json.Number("0")),
		"network":     alt(s["network"], get(s, "transport", "type"), ""),
		"security":    alt(s["security"], "none"),
		"serverName":  serverName,
		"host":        alt(get(s, "transport", "host"), get(s, "grpc", "authority"), get(s, "ws", "host"), get(s, "httpupgrade", "host"), ""),
		"path":        alt(get(s, "xhttp", "path"), get(s, "ws", "path"), get(s, "httpupgrade", "path"), get(s, "transport", "path"), ""),
		"serviceName": alt(get(s, "grpc", "serviceName"), get(s, "transport", "serviceName"), ""),
		"mode":        alt(get(s, "xhttp", "mode"), get(s, "transport", "mode"), ""),
	})
}
